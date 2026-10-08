// reportPassCoverage: prints each slice of a pass, their combined footprint and how much of the target they cover.
// describeCoverage: prints the latitude and longitude coverage and either "complete" or where coverage is missing.
// orbitOf: returns the absolute orbit of the first slice, or a placeholder when it is missing.
// sliceDuration: returns how long a slice's acquisition lasted, rounded to the second, or zero if unknown.
// reportNewestPass: finds the newest pass over the configured area and prints its coverage report.
// targetExtentForArea: returns the bounding box of a named area's search footprint.
// reportRecentPasses: prints a one-line coverage summary for each of several recent passes over the area.
// areaCoverageFraction: returns the smaller of the latitude and longitude coverage fractions.
// ReportSearch: checks that -area was given, then reports either the newest pass or a survey of recent ones.

package pass

import (
	"fmt"
	"time"

	"radarpipeline/internal/cdse"
	"radarpipeline/internal/config"
)

func reportPassCoverage(areaName string, slices []cdse.CatalogueScene, target cdse.GeoExtent) {
	fmt.Printf("Newest pass over %s: %d product(s)\n", areaName, len(slices))
	fmt.Printf("Absolute orbit:   %s\n\n", orbitOf(slices))

	for index, slice := range slices {
		fmt.Printf("slice_%d  %s\n", index+1, slice.ProductName)
		fmt.Printf("         acquired %s .. %s  (%s)\n",
			slice.AcquiredAt.UTC().Format("2006-01-02T15:04:05Z"),
			slice.EndsAt.UTC().Format("15:04:05Z"),
			sliceDuration(slice))
		fmt.Printf("         %s\n", slice.Extent)
		if !slice.IsOnline {
			fmt.Printf("         note: archived rather than online, so the download may be slow to start\n")
		}
		fmt.Println()
	}

	extents := make([]cdse.GeoExtent, 0, len(slices))
	for _, slice := range slices {
		extents = append(extents, slice.Extent)
	}
	combined := cdse.CombinedExtent(extents)

	fmt.Printf("Combined footprint: %s\n", combined)
	fmt.Printf("Bulgarian waters:   %s\n\n", target)

	describeCoverage(cdse.MeasureCoverage(slices, target), target)
}

func describeCoverage(coverage cdse.CoverageOf, target cdse.GeoExtent) {
	fmt.Printf("Latitude covered:   %.0f%% of %.2f..%.2f\n",
		100*coverage.LatitudeFraction, target.LatMin, target.LatMax)
	fmt.Printf("Longitude covered:  %.0f%% of %.2f..%.2f\n",
		100*coverage.LongitudeFraction, target.LonMin, target.LonMax)

	if coverage.IsComplete() {
		fmt.Println("\nCOMPLETE: this pass reaches the whole area.")
		return
	}

	fmt.Println("\nPARTIAL: this pass does not reach the whole area.")
	if coverage.MissingSouthOf > 0 {
		fmt.Printf("  nothing south of %.3f (the coast starts at %.2f)\n",
			coverage.MissingSouthOf, target.LatMin)
	}
	if coverage.MissingNorthOf > 0 {
		fmt.Printf("  nothing north of %.3f (the coast ends at %.2f)\n",
			coverage.MissingNorthOf, target.LatMax)
	}
	for _, gap := range coverage.Gaps {
		fmt.Printf("  gap between slices at lat %s\n", gap)
	}
	fmt.Println("  Detections will be missing there, and their absence is not evidence")
	fmt.Println("  of empty water.")
}

func orbitOf(slices []cdse.CatalogueScene) string {
	if len(slices) == 0 {
		return "unknown"
	}
	if slices[0].AbsoluteOrbit == "" {
		return "unparsed from the product name"
	}
	return slices[0].AbsoluteOrbit
}

func sliceDuration(slice cdse.CatalogueScene) time.Duration {
	if slice.EndsAt.IsZero() || slice.AcquiredAt.IsZero() {
		return 0
	}
	return slice.EndsAt.Sub(slice.AcquiredAt).Round(time.Second)
}

func reportNewestPass(cfg *config.Config) error {
	slices, err := cdse.FindNewestPassOverArea(cfg.SearchAreaName)
	if err != nil {
		return err
	}

	target, err := targetExtentForArea(cfg.SearchAreaName)
	if err != nil {
		return err
	}
	reportPassCoverage(cfg.SearchAreaName, slices, target)
	return nil
}

func targetExtentForArea(areaName string) (cdse.GeoExtent, error) {
	footprint, isKnownArea := cdse.FootprintsByAreaName[areaName]
	if !isKnownArea {
		return cdse.GeoExtent{}, fmt.Errorf("unknown area %q (known: %s)", areaName, cdse.KnownAreaNames())
	}
	return cdse.ParseFootprintExtent(footprint)
}

func reportRecentPasses(cfg *config.Config) error {
	target, err := targetExtentForArea(cfg.SearchAreaName)
	if err != nil {
		return err
	}

	passes, err := cdse.FindRecentPassesOverArea(cfg.SearchAreaName, cfg.PassesToSurvey)
	if err != nil {
		return err
	}

	fmt.Printf("Recent passes over %s, newest first\n", cfg.SearchAreaName)
	fmt.Printf("Area: %s\n\n", target)
	fmt.Printf("%-19s %-8s %-7s %-7s %-11s %-9s %s\n",
		"acquired (UTC)", "mission", "orbit", "slices", "direction", "coverage", "verdict")

	complete := 0
	for _, pass := range passes {
		coverage := cdse.MeasureCoverage(pass, target)
		verdict := "partial"
		if coverage.IsComplete() {
			verdict = "complete"
			complete++
		}
		fmt.Printf("%-19s %-8s %-7s %-7d %-11s %-9s %s\n",
			pass[0].AcquiredAt.UTC().Format("2006-01-02 15:04"),
			pass[0].MissionPrefix, pass[0].AbsoluteOrbit, len(pass),
			cdse.TrackDirectionOf(pass),
			fmt.Sprintf("%.0f%%", 100*areaCoverageFraction(coverage)),
			verdict)
	}

	fmt.Printf("\n%d of %d passes reach the whole area.\n", complete, len(passes))
	if complete < len(passes) {
		fmt.Println("Full coverage is therefore not guaranteed; a run has to report what it got.")
	}
	return nil
}

func areaCoverageFraction(coverage cdse.CoverageOf) float64 {
	if coverage.LatitudeFraction < coverage.LongitudeFraction {
		return coverage.LatitudeFraction
	}
	return coverage.LongitudeFraction
}

func ReportSearch(cfg *config.Config) error {
	if cfg.SearchAreaName == "" {
		return fmt.Errorf("-search-only needs -area, e.g. -area bulgaria (known: %s)",
			cdse.KnownAreaNames())
	}
	if cfg.PassesToSurvey > 1 {
		return reportRecentPasses(cfg)
	}
	return reportNewestPass(cfg)
}
