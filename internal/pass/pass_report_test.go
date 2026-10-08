// TestReportSearchDetailsOnePassOrSurveysSeveral: checks the search report needs -area, then details one pass or surveys several.
// sceneOver: builds a catalogue scene spanning longitudes 27 to 30 between the given latitudes.
// TestDescribeCoverageNamesEveryHole: checks a partial pass names its missing south, missing north and the gap between slices.
// TestCoverageOfAPassWithNoUsableOutline: checks a slice without an outline gives zero coverage and an unknown track direction.
// TestReportPassCoverageForASliceWithoutMetadata: checks the pass report copes with a slice whose product name cannot be parsed.

package pass

import (
	"strings"
	"testing"

	"radarpipeline/internal/cdse"
	"radarpipeline/internal/config"
	"radarpipeline/internal/testsupport"
)

func TestReportSearchDetailsOnePassOrSurveysSeveral(t *testing.T) {
	testsupport.ScriptCDSE(t)
	cfg := config.NewDefaultConfig()

	if err := ReportSearch(&cfg); err == nil || !strings.Contains(err.Error(), "needs -area") {
		t.Errorf("err = %v, want -area asked for", err)
	}

	cfg.SearchAreaName = "bulgaria"
	detail := testsupport.CaptureStdout(t, func() {
		if err := ReportSearch(&cfg); err != nil {
			t.Error(err)
		}
	})
	for _, want := range []string{"2 product(s)", "Absolute orbit:   004474", "slice_2", "COMPLETE"} {
		if !strings.Contains(detail, want) {
			t.Errorf("the pass report lacks %q:\n%s", want, detail)
		}
	}

	cfg.PassesToSurvey = 3
	survey := testsupport.CaptureStdout(t, func() {
		if err := ReportSearch(&cfg); err != nil {
			t.Error(err)
		}
	})
	for _, want := range []string{"Recent passes over bulgaria", "ascending", "complete", "partial",
		"1 of 2 passes reach the whole area", "not guaranteed"} {
		if !strings.Contains(survey, want) {
			t.Errorf("the survey lacks %q:\n%s", want, survey)
		}
	}

	cfg.SearchAreaName = "atlantis"
	if err := reportRecentPasses(&cfg); err == nil {
		t.Error("an unknown area was surveyed")
	}
	if err := reportNewestPass(&cfg); err == nil {
		t.Error("an unknown area was reported")
	}
}

var bulgarianTarget = cdse.GeoExtent{LonMin: 27.4, LonMax: 29.2, LatMin: 41.95, LatMax: 43.8}

func sceneOver(latMin, latMax float64) cdse.CatalogueScene {
	return cdse.CatalogueScene{Extent: cdse.GeoExtent{LonMin: 27.0, LonMax: 30.0, LatMin: latMin, LatMax: latMax}}
}

func TestDescribeCoverageNamesEveryHole(t *testing.T) {
	coverage := cdse.MeasureCoverage([]cdse.CatalogueScene{sceneOver(42.2, 42.6), sceneOver(42.9, 43.5)}, bulgarianTarget)
	if coverage.IsComplete() || len(coverage.Gaps) != 1 {
		t.Fatalf("coverage = %+v", coverage)
	}

	said := testsupport.CaptureStdout(t, func() { describeCoverage(coverage, bulgarianTarget) })
	for _, want := range []string{"PARTIAL", "nothing south of 42.200", "nothing north of 43.500",
		"gap between slices at lat 42.600..42.900", "not evidence"} {
		if !strings.Contains(said, want) {
			t.Errorf("description lacks %q:\n%s", want, said)
		}
	}
}

func TestCoverageOfAPassWithNoUsableOutline(t *testing.T) {
	coverage := cdse.MeasureCoverage([]cdse.CatalogueScene{{}}, bulgarianTarget)
	if coverage.LatitudeFraction != 0 || coverage.LongitudeFraction != 0 || coverage.IsComplete() {
		t.Errorf("an unknown outline produced %+v", coverage)
	}
	if got := cdse.TrackDirectionOf([]cdse.CatalogueScene{{}}); got != "unknown" {
		t.Errorf("direction of one slice = %q", got)
	}
	descending := []cdse.CatalogueScene{sceneOver(43, 44), sceneOver(42, 43)}
	if got := cdse.TrackDirectionOf(descending); got != "descending" {
		t.Errorf("direction = %q", got)
	}
}

func TestReportPassCoverageForASliceWithoutMetadata(t *testing.T) {
	slice := sceneOver(41.9, 43.9)
	slice.ProductName = "mystery"
	said := testsupport.CaptureStdout(t, func() { reportPassCoverage("bulgaria", []cdse.CatalogueScene{slice}, bulgarianTarget) })
	for _, want := range []string{"unparsed from the product name", "(0s)", "archived rather than online", "COMPLETE"} {
		if !strings.Contains(said, want) {
			t.Errorf("report lacks %q:\n%s", want, said)
		}
	}
	if orbitOf(nil) != "unknown" {
		t.Error("no slices should have an unknown orbit")
	}
}
