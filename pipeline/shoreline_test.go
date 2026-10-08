// TestDistanceToSegmentMeasuresToTheNearerEnd: checks point-to-segment distance uses the nearer end when the foot falls outside.
// TestDistanceToShoreOnAKnownShape: checks shore distances north and east of a square coastline match hand-worked figures.
// TestLoadShorelineRefusesWhatIsNotAPolygonShapefile: checks short, wrong-code, point-type and missing shapefiles are refused.
// TestFormatShoreDistanceLeavesUnmeasuredBlank: checks an unmeasured shore distance is blank and a measured one is rounded.
// writeTestShapefile: writes a minimal shapefile holding one polygon record built from the given rings.
// squareRing: returns the four corners of the given box as a coastline ring.
// TestTouchesBoundsFindsCoastInsideTheSwath: checks coast is found inside, overlapping or crossing a swath and not in open sea.
// TestTouchesBoundsKeepsTheMaskForCoastOnTheEdge: checks a coastline lying exactly on the swath edge counts as touching it.
// TestAnUnanswerableCoastKeepsTheMaskAndMeasuresNothing: checks no mask, coastline or product extent keeps the configured graph and says why.

package main

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	writeTestShapefile(t, path, [][][2]float64{{
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
	want := degree * metresPerDegreeLatitude
	if math.Abs(got-want) > want*0.01 {
		t.Errorf("distance north = %.0f m, want about %.0f m", got, want)
	}

	got = shore.distanceMetres(lat, lon+2*degree)
	want = degree * metresPerDegreeLongitude * math.Cos(lat*math.Pi/180)
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
	if got := formatShoreDistance(Detection{}); got != "" {
		t.Errorf("unmeasured formatted as %q, want empty: a number would be believed", got)
	}
	measured := Detection{DistanceToShoreM: 412.6, ShoreMeasured: true}
	if got := formatShoreDistance(measured); got != "413" {
		t.Errorf("formatted as %q, want 413", got)
	}
}

func writeTestShapefile(t *testing.T, path string, rings [][][2]float64) {
	t.Helper()

	points := 0
	for _, ring := range rings {
		points += len(ring)
	}
	content := 44 + len(rings)*4 + points*16
	total := 100 + 8 + content

	raw := make([]byte, total)
	binary.BigEndian.PutUint32(raw[0:4], 9994)
	binary.BigEndian.PutUint32(raw[24:28], uint32(total/2))
	binary.LittleEndian.PutUint32(raw[28:32], 1000)
	binary.LittleEndian.PutUint32(raw[32:36], shapefilePolygonType)

	binary.BigEndian.PutUint32(raw[100:104], 1)
	binary.BigEndian.PutUint32(raw[104:108], uint32(content/2))
	body := raw[108:]

	binary.LittleEndian.PutUint32(body[0:4], shapefilePolygonType)
	binary.LittleEndian.PutUint32(body[36:40], uint32(len(rings)))
	binary.LittleEndian.PutUint32(body[40:44], uint32(points))

	at := 0
	for index, ring := range rings {
		binary.LittleEndian.PutUint32(body[44+index*4:48+index*4], uint32(at))
		at += len(ring)
	}

	base := 44 + len(rings)*4
	written := 0
	for _, ring := range rings {
		for _, point := range ring {
			offset := base + written*16
			binary.LittleEndian.PutUint64(body[offset:offset+8], math.Float64bits(point[0]))
			binary.LittleEndian.PutUint64(body[offset+8:offset+16], math.Float64bits(point[1]))
			written++
		}
	}

	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func squareRing(west, south, east, north float64) [][2]float64 {
	return [][2]float64{{west, south}, {east, south}, {east, north}, {west, north}}
}

func TestTouchesBoundsFindsCoastInsideTheSwath(t *testing.T) {
	shore := &shoreline{rings: [][][2]float64{squareRing(28.0, 43.0, 28.5, 43.5)}}

	cases := []struct {
		name string
		box  geoBounds
		want bool
	}{
		{"open sea to the east", geoBounds{South: 42.3, West: 28.83, North: 44.25, East: 32.42}, false},
		{"open sea to the north", geoBounds{South: 44.0, West: 27.0, North: 45.0, East: 29.0}, false},

		{"the coast is inside", geoBounds{South: 42.5, West: 27.5, North: 44.0, East: 29.0}, true},
		{"a corner of the coast is inside", geoBounds{South: 43.25, West: 28.25, North: 44.0, East: 29.0}, true},

		{"a swath cutting straight across", geoBounds{South: 43.2, West: 27.0, North: 43.3, East: 30.0}, true},
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
	shore := &shoreline{rings: [][][2]float64{squareRing(28.0, 43.0, 28.5, 43.5)}}

	grazing := geoBounds{South: 43.0, West: 28.5, North: 43.5, East: 29.0}
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
		{"the graph masks nothing", strings.Replace(maskingDetectionGraph, "<geometry>bg_coast</geometry>", "", 1),
			true, true, "masks no coastline", false},
		{"the coastline is missing", maskingDetectionGraph, false, true, "cannot read bg_coast.shp", false},
		{"the product is unreadable", maskingDetectionGraph, true, false, "extent is unreadable", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			config := newDefaultConfig()
			config.GraphsDir = dir
			writeText(t, config.DetectionGraphFilePath(), c.graph)
			if c.coast {
				writeTestShapefile(t, filepath.Join(dir, "bg_coast.shp"), [][][2]float64{coastFarAway})
			}
			slice := &SliceJob{Index: 1, PreprocessedPath: filepath.Join(dir, "absent.dim")}
			if c.product {
				slice.PreprocessedPath = writeSeaProduct(t, dir, "sea")
			}

			var run GraphRun
			var err error
			detections := []Detection{{Latitude: 43, Longitude: 28}}
			said := captureStdout(t, func() {
				run, err = detectionRunForSlice(&config, slice)
				measureDistanceToShore(&config, detections)
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
