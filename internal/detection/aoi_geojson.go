// aoiRing: converts the AOI polygon to a closed GeoJSON ring of [longitude, latitude] pairs.
// WriteAOIGeoJSON: writes the AOI polygon to a file as a GeoJSON FeatureCollection, creating the directory if needed.

package detection

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func aoiRing() [][]float64 {
	ring := make([][]float64, 0, len(BulgarianWatersAOI)+1)
	for _, vertex := range BulgarianWatersAOI {
		ring = append(ring, []float64{vertex.Longitude, vertex.Latitude})
	}
	if len(ring) > 0 {
		ring = append(ring, ring[0])
	}
	return ring
}

func WriteAOIGeoJSON(path string) error {
	document := map[string]any{
		"type": "FeatureCollection",
		"features": []any{
			map[string]any{
				"type": "Feature",
				"properties": map[string]any{
					"name":   "Bulgarian waters AOI",
					"source": "BulgarianWatersAOI in pipeline/aoi.go",
				},
				"geometry": map[string]any{
					"type":        "Polygon",
					"coordinates": [][][]float64{aoiRing()},
				},
			},
		},
	}

	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode the AOI: %w", err)
	}
	encoded = append(encoded, '\n')

	if directory := filepath.Dir(path); directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("cannot create %s: %w", directory, err)
		}
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}

	fmt.Printf("Wrote %s (%d vertices)\n", path, len(BulgarianWatersAOI))
	return nil
}
