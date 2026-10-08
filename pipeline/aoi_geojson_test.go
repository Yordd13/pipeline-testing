// TestWriteAOIGeoJSONWritesAClosedLongitudeFirstRing: checks the AOI file is a one-polygon FeatureCollection with a closed, lon-first ring.
// TestWriteAOIGeoJSONReportsWhereItCannotWrite: checks writing the AOI under a file or over a folder returns an error.

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAOIGeoJSONWritesAClosedLongitudeFirstRing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web", "aoi.geojson")
	captureStdout(t, func() {
		if err := writeAOIGeoJSON(path); err != nil {
			t.Error(err)
		}
	})

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Type     string `json:"type"`
		Features []struct {
			Geometry struct {
				Type        string         `json:"type"`
				Coordinates [][][2]float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(contents, &document); err != nil {
		t.Fatalf("not GeoJSON: %v", err)
	}
	if document.Type != "FeatureCollection" || len(document.Features) != 1 ||
		document.Features[0].Geometry.Type != "Polygon" {
		t.Fatalf("document = %+v", document)
	}

	ring := document.Features[0].Geometry.Coordinates[0]
	if len(ring) != len(BulgarianWatersAOI)+1 || ring[0] != ring[len(ring)-1] {
		t.Errorf("ring %v is not closed", ring)
	}
	first := BulgarianWatersAOI[0]
	if ring[0] != [2]float64{first.Longitude, first.Latitude} {
		t.Errorf("first position %v, want longitude %v then latitude %v", ring[0], first.Longitude, first.Latitude)
	}
}

func TestWriteAOIGeoJSONReportsWhereItCannotWrite(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "file")
	writeText(t, blocked, "x")
	if err := writeAOIGeoJSON(filepath.Join(blocked, "aoi.geojson")); err == nil {
		t.Error("writing under a file succeeded")
	}
	if err := writeAOIGeoJSON(t.TempDir()); err == nil {
		t.Error("writing over a folder succeeded")
	}
}
