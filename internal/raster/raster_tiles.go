// tileSourcePixelDegrees: returns the source pixel size in degrees matching a tile pixel's ground size at a zoom.
// buildPassTiles: resamples the bands to a degree grid, writes it out and has GDAL cut an XYZ tile pyramid.
// runTileTools: georeferences the source with gdal_translate and cuts WebP XYZ tiles with gdal2tiles in Docker.
// runContainer: runs a docker command with a timeout and returns an error including its output on failure.
// indent: trims command output and indents each line for an error message, or returns "" if empty.
// trimFloat: formats a coordinate with nine decimal places and no exponent.
// writeTileSource: writes the stretched mosaic as a raw two-band grey-and-mask image with an ENVI header.
// writeENVIHeader: writes the ENVI header describing the raw two-band 8-bit source image.
// measureTileTree: counts the tile files under a directory and sums their sizes.
// pruneTileDirectories: removes all but the most recently written tile runs and returns the removed names.
// looksLikeTileTree: reports whether a directory contains a zoom-level folder produced by gdal2tiles.

package raster

import (
	"context"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"radarpipeline/internal/config"
)

const (
	tileMaxZoom = 12

	tileMinZoom = 7

	tileViewerMaxZoom = 16

	tilePixels = 256

	tileFormat    = "WEBP"
	tileExtension = "webp"

	tileQuality = 85

	gdalTilesImageDigest = "ghcr.io/osgeo/gdal" +
		"@sha256:87d788f58ff8273d4c027cb587c852490952a1919c4841112c35cb13313d0f05"

	containerTilesDir = "/tiles"

	tileTimeout = 30 * time.Minute

	TileWorkDirName = ".work"

	mercatorRadiusMetres = 6378137.0
)

func tileSourcePixelDegrees(zoom int, area GeoBounds) float64 {
	mercatorMetres := 2 * math.Pi * mercatorRadiusMetres /
		(tilePixels * math.Pow(2, float64(zoom)))

	middle := (area.South + area.North) / 2
	groundMetres := mercatorMetres * math.Cos(middle*math.Pi/180)
	return groundMetres / MetresPerDegreeLatitude
}

type tileOutcome struct {
	Directory string
	Bounds    GeoBounds
	Tiles     int
	Bytes     int64
	Took      time.Duration
}

func buildPassTiles(cfg *config.Config, bands []*rasterBand, runID string) (tileOutcome, error) {
	started := time.Now()
	outcome := tileOutcome{}

	tilesDir := config.AbsolutePath(cfg.TilesDir)
	workDir := filepath.Join(tilesDir, TileWorkDirName, runID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return outcome, fmt.Errorf("cannot create %s: %w", workDir, err)
	}
	defer os.RemoveAll(workDir)

	area := coveredArea(bands)
	grid := buildGeographicGrid(area, tileSourcePixelDegrees(tileMaxZoom, area))
	width, height := grid.size()
	fmt.Printf("Tiles:          resampling to %d x %d at %.1f m a pixel\n",
		width, height, grid.pixelDeg*MetresPerDegreeLatitude)

	mosaic, err := buildRasterMosaic(bands, grid)
	if err != nil {
		return outcome, err
	}
	outcome.Bounds = grid.bounds()

	sourcePath := filepath.Join(workDir, "source.img")
	if err := writeTileSource(sourcePath, mosaic, measureStretch(mosaic)); err != nil {
		return outcome, err
	}
	mosaic = nil

	target := filepath.Join(tilesDir, runID)
	if err := os.RemoveAll(target); err != nil {
		return outcome, fmt.Errorf("cannot clear %s: %w", target, err)
	}
	if err := runTileTools(tilesDir, runID, outcome.Bounds); err != nil {
		os.RemoveAll(target)
		return outcome, err
	}

	outcome.Directory = target
	outcome.Tiles, outcome.Bytes, err = measureTileTree(target)
	if err != nil {
		return outcome, err
	}
	if outcome.Tiles == 0 {
		return outcome, fmt.Errorf("gdal2tiles produced no tiles")
	}
	outcome.Took = time.Since(started)
	return outcome, nil
}

func runTileTools(tilesDir, runID string, area GeoBounds) error {
	ctx, giveUp := context.WithTimeout(context.Background(), tileTimeout)
	defer giveUp()

	work := containerTilesDir + "/" + TileWorkDirName + "/" + runID
	mount := tilesDir + ":" + containerTilesDir

	translate := []string{
		"run", "--rm", "-v", mount,
		"--entrypoint", "gdal_translate", gdalTilesImageDigest,
		"-q",
		"-of", "GTiff",
		"-a_srs", "EPSG:4326",
		"-a_ullr",
		trimFloat(area.West), trimFloat(area.North),
		trimFloat(area.East), trimFloat(area.South),
		"-b", "1", "-b", "1", "-b", "1", "-b", "2",
		"-colorinterp", "red,green,blue,alpha",
		"-co", "COMPRESS=DEFLATE",
		"-co", "TILED=YES",
		work + "/source.img", work + "/source.tif",
	}
	if err := runContainer(ctx, "gdal_translate", translate); err != nil {
		return err
	}

	tiles := []string{
		"run", "--rm", "-v", mount,
		"--entrypoint", "gdal2tiles.py", gdalTilesImageDigest,
		"--xyz",
		"--zoom", fmt.Sprintf("%d-%d", tileMinZoom, tileMaxZoom),
		"--resampling", "average",
		"--tiledriver", tileFormat,
		fmt.Sprintf("--webp-quality=%d", tileQuality),
		"--processes", "4",
		"--webviewer", "none",
		"-q",
		work + "/source.tif", containerTilesDir + "/" + runID,
	}
	return runContainer(ctx, "gdal2tiles.py", tiles)
}

func runContainer(ctx context.Context, what string, arguments []string) error {
	output, err := exec.CommandContext(ctx, "docker", arguments...).CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("%s did not finish within %s", what, tileTimeout)
	}
	if err != nil {
		return fmt.Errorf("%s failed: %w%s", what, err, indent(string(output)))
	}
	return nil
}

func indent(output string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return ""
	}
	return "\n       " + strings.ReplaceAll(trimmed, "\n", "\n       ")
}

func trimFloat(value float64) string {
	return fmt.Sprintf("%.9f", value)
}

func writeTileSource(path string, mosaic *rasterMosaic, stretch dbStretch) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	grey := make([]byte, mosaic.width)
	for row := 0; row < mosaic.height; row++ {
		base := row * mosaic.width
		for column := 0; column < mosaic.width; column++ {
			if mosaic.filled[base+column] {
				grey[column] = stretch.greyLevel(decibels(float64(mosaic.value[base+column])))
			} else {
				grey[column] = 0
			}
		}
		if _, err := file.Write(grey); err != nil {
			return err
		}
	}

	alpha := make([]byte, mosaic.width)
	for row := 0; row < mosaic.height; row++ {
		base := row * mosaic.width
		for column := 0; column < mosaic.width; column++ {
			if mosaic.filled[base+column] {
				alpha[column] = 0xff
			} else {
				alpha[column] = 0
			}
		}
		if _, err := file.Write(alpha); err != nil {
			return err
		}
	}

	if err := file.Close(); err != nil {
		return err
	}
	return writeENVIHeader(strings.TrimSuffix(path, filepath.Ext(path))+".hdr",
		mosaic.width, mosaic.height)
}

func writeENVIHeader(path string, width, height int) error {
	header := fmt.Sprintf(`ENVI
description = {ship detection pass, stretched to 8 bits with a mask}
samples = %d
lines = %d
bands = 2
header offset = 0
file type = ENVI Standard
data type = 1
interleave = bsq
byte order = 0
band names = {radar, mask}
`, width, height)
	return os.WriteFile(path, []byte(header), 0o644)
}

func measureTileTree(root string) (int, int64, error) {
	tiles := 0
	var bytes int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), "."+tileExtension) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		tiles++
		bytes += info.Size()
		return nil
	})
	return tiles, bytes, err
}

func pruneTileDirectories(tilesDir string, keep int) ([]string, error) {
	if keep <= 0 {
		return nil, nil
	}

	entries, err := os.ReadDir(tilesDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	type candidate struct {
		name    string
		written time.Time
	}
	found := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == TileWorkDirName {
			continue
		}
		if !looksLikeTileTree(filepath.Join(tilesDir, entry.Name())) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		found = append(found, candidate{entry.Name(), info.ModTime()})
	}

	sort.Slice(found, func(i, j int) bool {
		return found[i].written.After(found[j].written)
	})

	removed := make([]string, 0)
	for _, stale := range found[min(keep, len(found)):] {
		if err := os.RemoveAll(filepath.Join(tilesDir, stale.name)); err != nil {
			return removed, err
		}
		removed = append(removed, stale.name)
	}
	return removed, nil
}

func looksLikeTileTree(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		for zoom := tileMinZoom; zoom <= tileMaxZoom; zoom++ {
			if entry.Name() == fmt.Sprint(zoom) {
				return true
			}
		}
	}
	return false
}

const (
	MetresPerDegreeLongitude = 111320.0
	MetresPerDegreeLatitude  = 111132.0
)
