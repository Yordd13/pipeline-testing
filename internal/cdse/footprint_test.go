// TestParseFootprintExtentRefusesWhatHasNoCoordinates: checks footprints without usable coordinates give an error, not an extent.

package cdse

import (
	"strings"
	"testing"
)

func TestParseFootprintExtentRefusesWhatHasNoCoordinates(t *testing.T) {
	for _, footprint := range []string{"POINT (27 42)", "POLYGON ((a b, c d))", strings.Repeat("x", 100)} {
		if _, err := ParseFootprintExtent(footprint); err == nil {
			t.Errorf("%q gave an extent", footprint)
		}
	}
	extent, err := ParseFootprintExtent("POLYGON ((27 42, 28 43, 27.5))")
	if err != nil || extent != (GeoExtent{LonMin: 27, LonMax: 28, LatMin: 42, LatMax: 43}) {
		t.Errorf("extent = %+v, %v; a pair with one number should be skipped", extent, err)
	}
	if (GeoExtent{}).String() != "unknown" {
		t.Error("an empty extent should read as unknown")
	}
}
