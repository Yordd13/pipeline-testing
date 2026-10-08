// thinSwath: builds a two-slice plan with a one-row product holding a bright target and one unreadable slice.
// TestExportPassRasterCutsTilesWhenItCan: checks the raster is cut into XYZ WebP tiles and older tile runs are pruned.
// TestExportPassRasterGivesUpWhenNoTilesCanBeMade: checks each tiling failure yields no sidecar, says why and leaves no tile folder.
// TestExportPassRasterWithNothingToDraw: checks no sidecar is produced when no slice product can be read.
// TestOpenRasterBandRefusesWhatItCannotRead: checks each kind of broken DIMAP header or missing samples is refused with a reason.
// TestBoundsUnionHoldsBoth: checks the union of two bounds spans both.
// TestMosaicLetsTheFirstSliceWinWhereTwoOverlap: checks the first slice's values win where two slices overlap in the mosaic.

package raster

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"radarpipeline/internal/config"
	"radarpipeline/internal/pass"
	"radarpipeline/internal/testsupport"
)

func thinSwath(t *testing.T) *pass.PassPlan {
	t.Helper()
	dir := t.TempDir()
	samples := make([]float32, testsupport.SwathCols)
	for at := range samples {
		samples[at] = 5e-4
	}
	samples[testsupport.SwathCols/2] = 3.0
	dimPath := testsupport.WriteNamedTestProduct(t, dir, "slice_1", testsupport.SwathCols, 1, testsupport.SwathLon, testsupport.SwathLat, testsupport.SwathPx, samples)
	return &pass.PassPlan{Slices: []*pass.SliceJob{
		{Index: 1, PreprocessedPath: dimPath, DetectionProductPath: "ships.dim"},
		{Index: 2, PreprocessedPath: filepath.Join(dir, "absent.dim"), DetectionProductPath: "ships.dim"},
	}}
}

func TestExportPassRasterCutsTilesWhenItCan(t *testing.T) {
	plan := thinSwath(t)
	cfg := config.NewDefaultConfig()
	cfg.TilesDir = t.TempDir()
	cfg.TileRetentionRuns = 1
	writeFakeTileTree(t, filepath.Join(cfg.TilesDir, "2026-01-01T000000Z_000001"), time.Now().Add(-time.Hour))
	testsupport.WriteText(t, filepath.Join(cfg.TilesDir, "readme.txt"), "not a run")
	calls := testsupport.LogFakeTools(t)

	var sidecar *RasterSidecar
	output := testsupport.CaptureStdout(t, func() { sidecar = ExportPassRaster(&cfg, plan, "RUN") })

	if sidecar == nil || sidecar.Type != "tiles" || sidecar.File != "RUN/{z}/{x}/{y}.webp" ||
		sidecar.MaxNativeZoom != tileMaxZoom || sidecar.MaxZoom != tileViewerMaxZoom {
		t.Fatalf("sidecar = %+v\n%s", sidecar, output)
	}
	if sidecar.Bounds.West > testsupport.SwathLon || sidecar.Bounds.North < testsupport.SwathLat {
		t.Errorf("bounds %+v do not hold the swath", sidecar.Bounds)
	}
	for _, want := range []string{"no image for slice_2", "3 tiles", "pruned 1 run(s) beyond the newest 1"} {
		if !strings.Contains(output, want) {
			t.Errorf("output lacks %q:\n%s", want, output)
		}
	}
	if _, err := os.Stat(filepath.Join(cfg.TilesDir, "2026-01-01T000000Z_000001")); !os.IsNotExist(err) {
		t.Error("the old run was not pruned")
	}

	cut := strings.Join(calls(), "\n")
	for _, want := range []string{"--xyz", "--zoom 7-12", "-a_srs EPSG:4326", "--tiledriver WEBP"} {
		if !strings.Contains(cut, want) {
			t.Errorf("GDAL was not asked for %q:\n%s", want, cut)
		}
	}
}

func TestExportPassRasterGivesUpWhenNoTilesCanBeMade(t *testing.T) {
	cases := []struct {
		name      string
		tilesDir  bool
		tiles     string
		wantNotes []string
	}{
		{"no tiles folder", false, "", []string{"no tiles directory is configured"}},
		{"gdal2tiles fails", true, "fail", []string{"gdal2tiles.py failed", "gdal2tiles exploded"}},
		{"gdal_translate fails", true, "fail-translate", []string{"gdal_translate failed", "source.img not recognised"}},
		{"no tiles produced", true, "empty", []string{"gdal2tiles produced no tiles"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan := thinSwath(t)
			cfg := config.NewDefaultConfig()
			cfg.TilesDir = ""
			if c.tilesDir {
				cfg.TilesDir = t.TempDir()
			}
			t.Setenv(testsupport.FakeTilesBehaviour, c.tiles)

			var sidecar *RasterSidecar
			output := testsupport.CaptureStdout(t, func() { sidecar = ExportPassRaster(&cfg, plan, "RUN") })
			if sidecar != nil {
				t.Fatalf("sidecar = %+v, want none\n%s", sidecar, output)
			}
			for _, want := range append(c.wantNotes, "this run has no raster layer") {
				if !strings.Contains(output, want) {
					t.Errorf("output lacks %q:\n%s", want, output)
				}
			}
			if c.tilesDir && c.tiles != "empty" {
				if _, err := os.Stat(filepath.Join(cfg.TilesDir, "RUN")); !os.IsNotExist(err) {
					t.Error("a failed cut left its folder behind")
				}
			}
			if c.tilesDir {
				if _, err := os.Stat(filepath.Join(cfg.TilesDir, TileWorkDirName, "RUN")); !os.IsNotExist(err) {
					t.Error("the work folder was left behind")
				}
			}
		})
	}
}

func TestExportPassRasterWithNothingToDraw(t *testing.T) {
	plan := &pass.PassPlan{Slices: []*pass.SliceJob{
		{Index: 1, PreprocessedPath: filepath.Join(t.TempDir(), "absent.dim"), DetectionProductPath: "x.dim"},
	}}
	cfg := config.NewDefaultConfig()
	var sidecar *RasterSidecar
	output := testsupport.CaptureStdout(t, func() { sidecar = ExportPassRaster(&cfg, plan, "RUN") })
	if sidecar != nil {
		t.Errorf("sidecar = %+v, want none", sidecar)
	}
	if !strings.Contains(output, "this run has no image") {
		t.Errorf("output = %q", output)
	}

	if _, err := buildRasterMosaic(nil, geographicGrid{width: 1, height: 1}); err == nil {
		t.Error("a mosaic of nothing was drawn")
	}
}

func TestOpenRasterBandRefusesWhatItCannotRead(t *testing.T) {
	dir := t.TempDir()
	good := writeTestProduct(t, dir, 2, 2, 27, 43, 0.01, make([]float32, 4))
	original, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	header := string(original)

	cases := []struct {
		name, from, to, want string
	}{
		{"not XML", "</Dimap_Document>", "", "cannot parse"},
		{"an unknown encoding", "ISO-8859-1", "EBCDIC", "cannot parse"},
		{"no size", "<NCOLS>2</NCOLS>", "<NCOLS>0</NCOLS>", "declares no raster size"},
		{"not float32", "<DATA_TYPE>float32</DATA_TYPE>", "<DATA_TYPE>int16</DATA_TYPE>", "only float32"},
		{"no such band", "<BAND_NAME>" + RasterBandName, "<BAND_NAME>Sigma0_VV", "no band called"},
		{"no data file", ".hdr\" />\n            <BAND_INDEX>0", ".hdr\" />\n            <BAND_INDEX>3", "names no data file"},
		{"rotated", ",0.0,0.0,", ",0.1,0.0,", "rotated"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(header, c.from) {
				t.Fatalf("the fixture has no %q to change", c.from)
			}
			path := filepath.Join(t.TempDir(), "broken.dim")
			testsupport.WriteText(t, path, strings.Replace(header, c.from, c.to, 1))
			if _, err := OpenRasterBand(path, RasterBandName); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want %q", err, c.want)
			}
		})
	}

	if _, err := OpenRasterBand(filepath.Join(dir, "absent.dim"), RasterBandName); err == nil {
		t.Error("a missing header was read")
	}

	band, err := OpenRasterBand(good, RasterBandName)
	if err != nil {
		t.Fatal(err)
	}
	band.imagePath = filepath.Join(dir, "absent.img")
	if _, err := buildRasterMosaic([]*rasterBand{band}, buildGeographicGrid(band.Bounds(), 0.01)); err == nil {
		t.Error("a band without samples was drawn")
	}
}

func TestBoundsUnionHoldsBoth(t *testing.T) {
	a := GeoBounds{South: 42, West: 27, North: 43, East: 28}
	b := GeoBounds{South: 41.5, West: 27.5, North: 42.5, East: 29}
	if got := a.union(b); got != (GeoBounds{South: 41.5, West: 27, North: 43, East: 29}) {
		t.Errorf("union = %+v", got)
	}
}

func TestMosaicLetsTheFirstSliceWinWhereTwoOverlap(t *testing.T) {
	dir := t.TempDir()
	flat := func(value float32) []float32 {
		samples := make([]float32, 4*4)
		for at := range samples {
			samples[at] = value
		}
		return samples
	}
	first := testsupport.WriteNamedTestProduct(t, dir, "first", 4, 4, 28.00, 43.0, 0.01, flat(1e-3))
	second := testsupport.WriteNamedTestProduct(t, dir, "second", 4, 4, 28.02, 43.0, 0.01, flat(1e-1))
	bands := make([]*rasterBand, 0, 2)
	for _, path := range []string{first, second} {
		band, err := OpenRasterBand(path, RasterBandName)
		if err != nil {
			t.Fatal(err)
		}
		bands = append(bands, band)
	}

	area := coveredArea(bands)
	if math.Abs(area.West-28.0) > 1e-9 || math.Abs(area.East-28.06) > 1e-9 {
		t.Fatalf("covered area = %+v, want both slices", area)
	}
	mosaic, err := buildRasterMosaic(bands, buildGeographicGrid(area, 0.01))
	if err != nil {
		t.Fatal(err)
	}
	if mosaic.width != 6 {
		t.Fatalf("mosaic is %d wide, want 6", mosaic.width)
	}
	for column, want := range map[int]float32{0: 1e-3, 2: 1e-3, 3: 1e-3, 5: 1e-1} {
		if got := mosaic.value[column]; math.Abs(float64(got-want)) > 1e-6 {
			t.Errorf("column %d = %g, want %g", column, got, want)
		}
	}
}
