// geoExtent.IsZero: reports whether the extent is unset, with every bound zero.
// geoExtent.LatSpan: returns the extent's height in degrees of latitude.
// geoExtent.String: formats the extent's latitude and longitude ranges, or "unknown" when unset.
// combinedExtent: returns the box enclosing all the given extents, skipping unset ones.
// parseFootprintExtent: scans the coordinate pairs out of a WKT footprint and returns their bounding box.
// coverageOf.IsComplete: reports whether coverage reaches at least 99% on both axes with no gaps between slices.
// measureCoverage: measures how much of the target extent a pass's slices cover, and where coverage falls short.
// gapsBetweenSlices: lists latitude bands inside the target that no slice reaches.
// truncate: shortens a string to at most max bytes, appending "..." when it cuts.

package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

type geoExtent struct {
	LonMin, LonMax float64
	LatMin, LatMax float64
}

func (e geoExtent) IsZero() bool {
	return e == geoExtent{}
}

func (e geoExtent) LatSpan() float64 { return e.LatMax - e.LatMin }

func (e geoExtent) String() string {
	if e.IsZero() {
		return "unknown"
	}
	return fmt.Sprintf("lat %.3f..%.3f  lon %.3f..%.3f", e.LatMin, e.LatMax, e.LonMin, e.LonMax)
}

func combinedExtent(extents []geoExtent) geoExtent {
	combined := geoExtent{}
	for _, extent := range extents {
		if extent.IsZero() {
			continue
		}
		if combined.IsZero() {
			combined = extent
			continue
		}
		combined.LonMin = math.Min(combined.LonMin, extent.LonMin)
		combined.LonMax = math.Max(combined.LonMax, extent.LonMax)
		combined.LatMin = math.Min(combined.LatMin, extent.LatMin)
		combined.LatMax = math.Max(combined.LatMax, extent.LatMax)
	}
	return combined
}

func parseFootprintExtent(footprint string) (geoExtent, error) {
	opening := strings.Index(footprint, "((")
	if opening < 0 {
		return geoExtent{}, fmt.Errorf("no polygon in %q", truncate(footprint, 60))
	}

	body := footprint[opening+2:]
	if closing := strings.Index(body, "))"); closing >= 0 {
		body = body[:closing]
	}

	extent := geoExtent{}
	first := true
	for _, pair := range strings.Split(body, ",") {
		numbers := strings.Fields(strings.TrimSpace(pair))
		if len(numbers) < 2 {
			continue
		}
		longitude, lonErr := strconv.ParseFloat(numbers[0], 64)
		latitude, latErr := strconv.ParseFloat(numbers[1], 64)
		if lonErr != nil || latErr != nil {
			continue
		}

		if first {
			extent = geoExtent{LonMin: longitude, LonMax: longitude,
				LatMin: latitude, LatMax: latitude}
			first = false
			continue
		}
		extent.LonMin = math.Min(extent.LonMin, longitude)
		extent.LonMax = math.Max(extent.LonMax, longitude)
		extent.LatMin = math.Min(extent.LatMin, latitude)
		extent.LatMax = math.Max(extent.LatMax, latitude)
	}

	if first {
		return geoExtent{}, fmt.Errorf("no coordinates in %q", truncate(footprint, 60))
	}
	return extent, nil
}

type coverageOf struct {
	LatitudeFraction  float64
	LongitudeFraction float64
	MissingSouthOf    float64
	MissingNorthOf    float64
	Gaps              []string
}

func (c coverageOf) IsComplete() bool {
	return c.LatitudeFraction >= 0.99 && c.LongitudeFraction >= 0.99 && len(c.Gaps) == 0
}

func measureCoverage(slices []CatalogueScene, target geoExtent) coverageOf {
	extents := make([]geoExtent, 0, len(slices))
	for _, slice := range slices {
		extents = append(extents, slice.Extent)
	}
	covered := combinedExtent(extents)

	coverage := coverageOf{}
	if covered.IsZero() || target.LatSpan() <= 0 {
		return coverage
	}

	overlapLatMin := math.Max(target.LatMin, covered.LatMin)
	overlapLatMax := math.Min(target.LatMax, covered.LatMax)
	if overlapLatMax > overlapLatMin {
		coverage.LatitudeFraction = (overlapLatMax - overlapLatMin) / target.LatSpan()
	}

	overlapLonMin := math.Max(target.LonMin, covered.LonMin)
	overlapLonMax := math.Min(target.LonMax, covered.LonMax)
	if overlapLonMax > overlapLonMin && target.LonMax > target.LonMin {
		coverage.LongitudeFraction =
			(overlapLonMax - overlapLonMin) / (target.LonMax - target.LonMin)
	}

	if covered.LatMin > target.LatMin {
		coverage.MissingSouthOf = covered.LatMin
	}
	if covered.LatMax < target.LatMax {
		coverage.MissingNorthOf = covered.LatMax
	}
	coverage.Gaps = gapsBetweenSlices(extents, target)
	return coverage
}

func gapsBetweenSlices(extents []geoExtent, target geoExtent) []string {
	usable := make([]geoExtent, 0, len(extents))
	for _, extent := range extents {
		if !extent.IsZero() {
			usable = append(usable, extent)
		}
	}
	if len(usable) < 2 {
		return nil
	}
	sort.Slice(usable, func(i, j int) bool { return usable[i].LatMin < usable[j].LatMin })

	gaps := []string{}
	reachedUpTo := usable[0].LatMax
	for _, extent := range usable[1:] {
		if extent.LatMin > reachedUpTo && extent.LatMin < target.LatMax &&
			reachedUpTo > target.LatMin {
			gaps = append(gaps, fmt.Sprintf("%.3f..%.3f", reachedUpTo, extent.LatMin))
		}
		reachedUpTo = math.Max(reachedUpTo, extent.LatMax)
	}
	return gaps
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
