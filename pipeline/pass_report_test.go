// sceneOver: builds a catalogue scene spanning longitudes 27 to 30 between the given latitudes.
// TestDescribeCoverageNamesEveryHole: checks a partial pass names its missing south, missing north and the gap between slices.
// TestCoverageOfAPassWithNoUsableOutline: checks a slice without an outline gives zero coverage and an unknown track direction.
// TestReportPassCoverageForASliceWithoutMetadata: checks the pass report copes with a slice whose product name cannot be parsed.
// TestParseFootprintExtentRefusesWhatHasNoCoordinates: checks footprints without usable coordinates give an error, not an extent.
// TestDescribeSceneAge: checks scene ages are phrased in minutes, hours or days as appropriate.

package main

import (
	"strings"
	"testing"
	"time"
)

var bulgarianTarget = geoExtent{LonMin: 27.4, LonMax: 29.2, LatMin: 41.95, LatMax: 43.8}

func sceneOver(latMin, latMax float64) CatalogueScene {
	return CatalogueScene{Extent: geoExtent{LonMin: 27.0, LonMax: 30.0, LatMin: latMin, LatMax: latMax}}
}

func TestDescribeCoverageNamesEveryHole(t *testing.T) {
	coverage := measureCoverage([]CatalogueScene{sceneOver(42.2, 42.6), sceneOver(42.9, 43.5)}, bulgarianTarget)
	if coverage.IsComplete() || len(coverage.Gaps) != 1 {
		t.Fatalf("coverage = %+v", coverage)
	}

	said := captureStdout(t, func() { describeCoverage(coverage, bulgarianTarget) })
	for _, want := range []string{"PARTIAL", "nothing south of 42.200", "nothing north of 43.500",
		"gap between slices at lat 42.600..42.900", "not evidence"} {
		if !strings.Contains(said, want) {
			t.Errorf("description lacks %q:\n%s", want, said)
		}
	}
}

func TestCoverageOfAPassWithNoUsableOutline(t *testing.T) {
	coverage := measureCoverage([]CatalogueScene{{}}, bulgarianTarget)
	if coverage.LatitudeFraction != 0 || coverage.LongitudeFraction != 0 || coverage.IsComplete() {
		t.Errorf("an unknown outline produced %+v", coverage)
	}
	if got := trackDirectionOf([]CatalogueScene{{}}); got != "unknown" {
		t.Errorf("direction of one slice = %q", got)
	}
	descending := []CatalogueScene{sceneOver(43, 44), sceneOver(42, 43)}
	if got := trackDirectionOf(descending); got != "descending" {
		t.Errorf("direction = %q", got)
	}
}

func TestReportPassCoverageForASliceWithoutMetadata(t *testing.T) {
	slice := sceneOver(41.9, 43.9)
	slice.ProductName = "mystery"
	said := captureStdout(t, func() { reportPassCoverage("bulgaria", []CatalogueScene{slice}, bulgarianTarget) })
	for _, want := range []string{"unparsed from the product name", "(0s)", "archived rather than online", "COMPLETE"} {
		if !strings.Contains(said, want) {
			t.Errorf("report lacks %q:\n%s", want, said)
		}
	}
	if orbitOf(nil) != "unknown" {
		t.Error("no slices should have an unknown orbit")
	}
}

func TestParseFootprintExtentRefusesWhatHasNoCoordinates(t *testing.T) {
	for _, footprint := range []string{"POINT (27 42)", "POLYGON ((a b, c d))", strings.Repeat("x", 100)} {
		if _, err := parseFootprintExtent(footprint); err == nil {
			t.Errorf("%q gave an extent", footprint)
		}
	}
	extent, err := parseFootprintExtent("POLYGON ((27 42, 28 43, 27.5))")
	if err != nil || extent != (geoExtent{LonMin: 27, LonMax: 28, LatMin: 42, LatMax: 43}) {
		t.Errorf("extent = %+v, %v; a pair with one number should be skipped", extent, err)
	}
	if (geoExtent{}).String() != "unknown" {
		t.Error("an empty extent should read as unknown")
	}
}

func TestDescribeSceneAge(t *testing.T) {
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{10 * time.Minute, "under an hour ago"},
		{5*time.Hour + time.Minute, "5 hours ago"},
		{72*time.Hour + time.Minute, "3 days ago"},
	}
	for _, c := range cases {
		if got := describeSceneAge(time.Now().Add(-c.ago)); got != c.want {
			t.Errorf("%s ago read as %q, want %q", c.ago, got, c.want)
		}
	}
}
