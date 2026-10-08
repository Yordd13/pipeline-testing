// TestTileSourceMatchesTheDeepestZoomOnTheGround: checks the tile source pixel matches a deepest-zoom tile pixel on the ground.
// TestTileSourceAccountsForLatitude: checks the tile source pixel in degrees shrinks by the cosine of the middle latitude.
// TestPruneKeepsTheNewestRunsAndRemovesTheRest: checks pruning keeps the newest tile trees by time, not name, and spares others.
// TestPruneLeavesEverythingWhenRetentionIsNotSet: checks a retention of zero removes no tile trees.
// writeFakeTileTree: creates a folder shaped like gdal2tiles output with one tile and sets its modification time.
// TestPruneWithoutATilesFolder: checks pruning a tiles folder that does not exist yet is not an error.
// TestLooksLikeTileTreeNeedsAZoomFolder: checks a folder needs a deep-zoom subfolder to count as a tile tree.
// TestWriteTileSourceWritesGreyThenMask: checks the tile source holds a dB-stretched grey band then a mask band, with a header.

package raster

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"radarpipeline/internal/testsupport"
)

func TestTileSourceMatchesTheDeepestZoomOnTheGround(t *testing.T) {
	area := GeoBounds{South: 40.937, West: 26.751, North: 44.346, East: 30.664}
	pixelDeg := tileSourcePixelDegrees(tileMaxZoom, area)

	middle := (area.South + area.North) / 2
	wantMetres := 156543.03392 * math.Cos(middle*math.Pi/180) / math.Pow(2, tileMaxZoom)

	gotMetres := pixelDeg * MetresPerDegreeLatitude
	if math.Abs(gotMetres-wantMetres) > 0.5 {
		t.Errorf("source pixel = %.1f m, want about %.1f m: the deepest tiles "+
			"would be resampled from something coarser", gotMetres, wantMetres)
	}
}

func TestTileSourceAccountsForLatitude(t *testing.T) {
	equator := GeoBounds{South: -1, West: 0, North: 1, East: 2}
	here := GeoBounds{South: 41, West: 27, North: 44, East: 30}

	atEquator := tileSourcePixelDegrees(tileMaxZoom, equator)
	atCoast := tileSourcePixelDegrees(tileMaxZoom, here)

	if atCoast >= atEquator {
		t.Errorf("pixel at 42 N is %.3e deg and at the equator %.3e: a grid in "+
			"degrees has to get finer with latitude, not coarser", atCoast, atEquator)
	}
	ratio := atCoast / atEquator
	if math.Abs(ratio-math.Cos(42.5*math.Pi/180)) > 0.02 {
		t.Errorf("ratio = %.3f, want the cosine of the middle latitude", ratio)
	}
}

func TestPruneKeepsTheNewestRunsAndRemovesTheRest(t *testing.T) {
	dir := t.TempDir()

	order := []string{"2026-09-01T000000Z_1", "2026-09-02T000000Z_1",
		"2026-09-03T000000Z_1", "2026-08-01T000000Z_1",
		"2026-09-05T000000Z_1", "2026-09-06T000000Z_1"}
	for at, name := range order {
		writeFakeTileTree(t, filepath.Join(dir, name),
			time.Now().Add(time.Duration(at-len(order))*time.Hour))
	}

	stranger := filepath.Join(dir, "notes")
	if err := os.MkdirAll(stranger, 0o755); err != nil {
		t.Fatal(err)
	}

	removed, err := pruneTileDirectories(dir, 3)
	if err != nil {
		t.Fatalf("pruneTileDirectories: %v", err)
	}
	if len(removed) != 3 {
		t.Fatalf("removed %v, want the three oldest", removed)
	}

	for _, kept := range order[3:] {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("%s was removed but is one of the newest three", kept)
		}
	}
	for _, gone := range order[:3] {
		if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
			t.Errorf("%s survived but is beyond the retention", gone)
		}
	}
	if _, err := os.Stat(stranger); err != nil {
		t.Errorf("a directory that is not a tile tree was deleted")
	}
}

func TestPruneLeavesEverythingWhenRetentionIsNotSet(t *testing.T) {
	dir := t.TempDir()
	writeFakeTileTree(t, filepath.Join(dir, "2026-09-01T000000Z_1"), time.Now())

	removed, err := pruneTileDirectories(dir, 0)
	if err != nil || len(removed) != 0 {
		t.Errorf("prune with retention 0 removed %v (err %v), want nothing",
			removed, err)
	}
}

func writeFakeTileTree(t *testing.T, path string, written time.Time) {
	t.Helper()
	tile := filepath.Join(path, "12", "2368")
	if err := os.MkdirAll(tile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tile, "1509.png"), []byte("not really"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, written, written); err != nil {
		t.Fatal(err)
	}
}

func TestPruneWithoutATilesFolder(t *testing.T) {
	removed, err := pruneTileDirectories(filepath.Join(t.TempDir(), "absent"), 3)
	if err != nil || len(removed) != 0 {
		t.Errorf("removed %v, err %v", removed, err)
	}
}

func TestLooksLikeTileTreeNeedsAZoomFolder(t *testing.T) {
	dir := t.TempDir()
	testsupport.WriteText(t, filepath.Join(dir, "run", "3", "x.webp"), "tile")
	testsupport.WriteText(t, filepath.Join(dir, "run", "12"), "a file, not a zoom folder")
	if looksLikeTileTree(filepath.Join(dir, "run")) {
		t.Error("zoom 3 and a file called 12 were taken for a tile tree")
	}
	if looksLikeTileTree(filepath.Join(dir, "absent")) {
		t.Error("a folder that does not exist was taken for a tile tree")
	}
}

func TestWriteTileSourceWritesGreyThenMask(t *testing.T) {
	dir := t.TempDir()
	mosaic := &rasterMosaic{width: 2, height: 1, value: []float32{1e-3, 0}, filled: []bool{true, false}}
	path := filepath.Join(dir, "source.img")
	if err := writeTileSource(path, mosaic, dbStretch{low: -40, high: -20}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{127, 0, 0xff, 0}; string(raw) != string(want) {
		t.Errorf("source = %v, want %v", raw, want)
	}
	header, err := os.ReadFile(filepath.Join(dir, "source.hdr"))
	if err != nil || !strings.Contains(string(header), "samples = 2") || !strings.Contains(string(header), "interleave = bsq") {
		t.Errorf("header = %q, %v", header, err)
	}

	if err := writeTileSource(filepath.Join(dir, "absent", "source.img"), mosaic, dbStretch{}); err == nil {
		t.Error("writing into a folder that does not exist succeeded")
	}
}
