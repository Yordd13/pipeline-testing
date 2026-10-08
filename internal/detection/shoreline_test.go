// TestDistanceToSegmentMeasuresToTheNearerEnd: checks point-to-segment distance uses the nearer end when the foot falls outside.
// TestDistanceToShoreOnAKnownShape: checks shore distances north and east of a square coastline match hand-worked figures.
// TestLoadShorelineRefusesWhatIsNotAPolygonShapefile: checks short, wrong-code, point-type and missing shapefiles are refused.
// TestFormatShoreDistanceLeavesUnmeasuredBlank: checks an unmeasured shore distance is blank and a measured one is rounded.
// TestTouchesBoundsFindsCoastInsideTheSwath: checks coast is found inside, overlapping or crossing a swath and not in open sea.
// TestTouchesBoundsKeepsTheMaskForCoastOnTheEdge: checks a coastline lying exactly on the swath edge counts as touching it.
// TestAnUnanswerableCoastKeepsTheMaskAndMeasuresNothing: checks no mask, coastline or product extent keeps the configured graph and says why.

package detection

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"radarpipeline/internal/config"
	"radarpipeline/internal/pass"
	"radarpipeline/internal/raster"
	"radarpipeline/internal/snap"
	"radarpipeline/internal/testsupport"
)

func TestDistanceToSegmentMeasuresToTheNearerEnd(t *testing.T) {
	cases := []struct {
		name                 string
		ax, ay, bx, by, want float64
	}{
		{"beside the middle", -100, 50, 100, 50, 50},
		{"past the far end", 100, 0, 200, 0, 100},
		{"before the near end", -200, 0, -100, 0, 100},
		{"diagonal, past a corner", 30, 40, 60, 80, 50},
		{"a repeated vertex", 300, 400, 300, 400, 500},
		{"the point is on it", -10, 0, 10, 0, 0},
	}
	for _, sample := range cases {
		t.Run(sample.name, func(t *testing.T) {
			got := distanceToSegmentMetres(sample.ax, sample.ay, sample.bx, sample.by)
			if math.Abs(got-sample.want) > 0.001 {
				t.Errorf("distance = %.3f, want %.3f", got, sample.want)
			}
		})
	}
}

func TestDistanceToShoreOnAKnownShape(t *testing.T) {
	const lat, lon = 43.0, 28.0
	const degree = 0.01

	dir := t.TempDir()
	path := filepath.Join(dir, "coast.shp")
	testsupport.WriteTestShapefile(t, path, [][][2]float64{{
		{lon - degree, lat - degree},
		{lon + degree, lat - degree},
		{lon + degree, lat + degree},
		{lon - degree, lat + degree},
	}})

	shore, err := loadShoreline(path)
	if err != nil {
		t.Fatalf("loadShoreline: %v", err)
	}
	if len(shore.rings) != 1 || shore.vertices() != 4 {
		t.Fatalf("read %d rings and %d vertices, want 1 and 4",
			len(shore.rings), shore.vertices())
	}

	got := shore.distanceMetres(lat+2*degree, lon)
	want := degree * raster.MetresPerDegreeLatitude
	if math.Abs(got-want) > want*0.01 {
		t.Errorf("distance north = %.0f m, want about %.0f m", got, want)
	}

	got = shore.distanceMetres(lat, lon+2*degree)
	want = degree * raster.MetresPerDegreeLongitude * math.Cos(lat*math.Pi/180)
	if math.Abs(got-want) > want*0.01 {
		t.Errorf("distance east = %.0f m, want about %.0f m", got, want)
	}

	if got := shore.distanceMetres(lat, lon); got <= 0 {
		t.Errorf("distance from the middle = %.0f m, want the distance to an edge", got)
	}
}

func TestLoadShorelineRefusesWhatIsNotAPolygonShapefile(t *testing.T) {
	dir := t.TempDir()

	short := filepath.Join(dir, "short.shp")
	if err := os.WriteFile(short, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}

	wrongCode := filepath.Join(dir, "wrong.shp")
	header := make([]byte, 100)
	binary.BigEndian.PutUint32(header[0:4], 1234)
	if err := os.WriteFile(wrongCode, header, 0o644); err != nil {
		t.Fatal(err)
	}

	notPolygon := filepath.Join(dir, "points.shp")
	points := make([]byte, 100)
	binary.BigEndian.PutUint32(points[0:4], 9994)
	binary.BigEndian.PutUint32(points[24:28], 50)
	binary.LittleEndian.PutUint32(points[32:36], 1)
	if err := os.WriteFile(notPolygon, points, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{short, wrongCode, notPolygon, filepath.Join(dir, "absent.shp")} {
		if _, err := loadShoreline(path); err == nil {
			t.Errorf("loadShoreline(%s) = nil, want an error", filepath.Base(path))
		}
	}
}

func TestFormatShoreDistanceLeavesUnmeasuredBlank(t *testing.T) {
	if got := FormatShoreDistance(Detection{}); got != "" {
		t.Errorf("unmeasured formatted as %q, want empty: a number would be believed", got)
	}
	measured := Detection{DistanceToShoreM: 412.6, ShoreMeasured: true}
	if got := FormatShoreDistance(measured); got != "413" {
		t.Errorf("formatted as %q, want 413", got)
	}
}

func TestTouchesBoundsFindsCoastInsideTheSwath(t *testing.T) {
	shore := &shoreline{rings: [][][2]float64{testsupport.SquareRing(28.0, 43.0, 28.5, 43.5)}}

	cases := []struct {
		name string
		box  raster.GeoBounds
		want bool
	}{
		{"open sea to the east", raster.GeoBounds{South: 42.3, West: 28.83, North: 44.25, East: 32.42}, false},
		{"open sea to the north", raster.GeoBounds{South: 44.0, West: 27.0, North: 45.0, East: 29.0}, false},

		{"the coast is inside", raster.GeoBounds{South: 42.5, West: 27.5, North: 44.0, East: 29.0}, true},
		{"a corner of the coast is inside", raster.GeoBounds{South: 43.25, West: 28.25, North: 44.0, East: 29.0}, true},

		{"a swath cutting straight across", raster.GeoBounds{South: 43.2, West: 27.0, North: 43.3, East: 30.0}, true},
	}

	for _, sample := range cases {
		t.Run(sample.name, func(t *testing.T) {
			if got := shore.touchesBounds(sample.box); got != sample.want {
				t.Errorf("touchesBounds = %v, want %v", got, sample.want)
			}
		})
	}
}

func TestTouchesBoundsKeepsTheMaskForCoastOnTheEdge(t *testing.T) {
	shore := &shoreline{rings: [][][2]float64{testsupport.SquareRing(28.0, 43.0, 28.5, 43.5)}}

	grazing := raster.GeoBounds{South: 43.0, West: 28.5, North: 43.5, East: 29.0}
	if !shore.touchesBounds(grazing) {
		t.Error("coast on the swath edge read as open sea, want it treated as land")
	}
}

func TestAnUnanswerableCoastKeepsTheMaskAndMeasuresNothing(t *testing.T) {
	cases := []struct {
		name    string
		graph   string
		coast   bool
		product bool
		note    string
		measure bool
	}{
		{"the graph masks nothing", strings.Replace(testsupport.MaskingDetectionGraph, "<geometry>bg_coast</geometry>", "", 1),
			true, true, "masks no coastline", false},
		{"the coastline is missing", testsupport.MaskingDetectionGraph, false, true, "cannot read bg_coast.shp", false},
		{"the product is unreadable", testsupport.MaskingDetectionGraph, true, false, "extent is unreadable", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.NewDefaultConfig()
			cfg.GraphsDir = dir
			testsupport.WriteText(t, cfg.DetectionGraphFilePath(), c.graph)
			if c.coast {
				testsupport.WriteTestShapefile(t, filepath.Join(dir, "bg_coast.shp"), [][][2]float64{testsupport.CoastFarAway})
			}
			slice := &pass.SliceJob{Index: 1, PreprocessedPath: filepath.Join(dir, "absent.dim")}
			if c.product {
				slice.PreprocessedPath = testsupport.WriteSeaProduct(t, dir, "sea")
			}

			var run snap.GraphRun
			var err error
			detections := []Detection{{Latitude: 43, Longitude: 28}}
			said := testsupport.CaptureStdout(t, func() {
				run, err = DetectionRunForSlice(&cfg, slice)
				measureDistanceToShore(&cfg, detections)
			})
			if err != nil || run.GraphContainerPath != "/graphs/ShipDetection.xml" {
				t.Errorf("run = %+v, %v; want the configured graph", run, err)
			}
			if !strings.Contains(said, c.note) {
				t.Errorf("output lacks %q:\n%s", c.note, said)
			}
			if detections[0].ShoreMeasured != c.measure {
				t.Errorf("distance measured = %v, want %v", detections[0].ShoreMeasured, c.measure)
			}
		})
	}
	measureDistanceToShore(nil, nil)
}
