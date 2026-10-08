// checkResultsDatabaseIsReady: fails when the results database cannot be opened or its schema check fails.
// saveRunToDatabase: builds the pass record and saves it to MySQL, replacing any existing copy of the pass.
// passRecord: converts the plan, merge outcome and raster description into a resultsdb.Pass record.

package main

import (
	"fmt"
	"strconv"
	"time"

	"radarpipeline/internal/resultsdb"
)

func checkResultsDatabaseIsReady(config *Config) error {
	db, err := resultsdb.Open(config.MySQLEnvFile)
	if err != nil {
		return fmt.Errorf("the results database is not reachable, so this run could not store "+
			"what it finds: %w\n       Start it with: docker compose up -d   (in mysql/)", err)
	}
	defer db.Close()
	return resultsdb.CheckSchema(db)
}

func saveRunToDatabase(config *Config, plan *PassPlan, outcome mergeOutcome, raster *rasterSidecar) error {
	pass, err := passRecord(plan, outcome, raster)
	if err != nil {
		return err
	}
	db, err := resultsdb.Open(config.MySQLEnvFile)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := resultsdb.SavePass(db, pass, resultsdb.ReplaceExisting); err != nil {
		return err
	}
	fmt.Printf("Database:       stored %s, %d slice(s), %d detection(s)\n",
		pass.Start.Format(time.RFC3339), len(pass.Slices), outcome.Written)
	return nil
}

func passRecord(plan *PassPlan, outcome mergeOutcome, raster *rasterSidecar) (resultsdb.Pass, error) {
	orbit, err := strconv.Atoi(plan.AbsoluteOrbit())
	if err != nil {
		return resultsdb.Pass{}, fmt.Errorf("the pass has no absolute orbit (%q)", plan.AbsoluteOrbit())
	}
	pass := resultsdb.Pass{
		Start:             plan.PassStart(),
		Orbit:             orbit,
		Area:              plan.AreaName,
		SlicesProcessed:   len(plan.SucceededSlices()),
		SlicesFailed:      len(plan.FailedSlices()),
		RawCFAR:           outcome.RawCFAR,
		DroppedOutsideAOI: outcome.DroppedOutsideAOI,
		SeamDuplicates:    outcome.SeamDuplicates,
		Written:           outcome.Written,
		AOIVertices:       len(BulgarianWatersAOI),
	}

	if text := plan.CoverageText(); text != "" {
		coverage, err := strconv.Atoi(text)
		if err != nil {
			return resultsdb.Pass{}, fmt.Errorf("coverage %q is not a whole number", text)
		}
		pass.CoveragePercent = &coverage
	}

	indexOf := map[string]int{}
	for _, slice := range plan.Slices {
		indexOf[slice.SliceName()] = len(pass.Slices)
		pass.Slices = append(pass.Slices, resultsdb.Slice{
			Index:       slice.Index,
			ProductName: trimProductExtensions(slice.ProductName),
			ProductID:   slice.ProductID,
			Mission:     slice.Mission,
			Start:       slice.AcquiredAt,
		})
	}
	for _, detection := range outcome.Kept {
		at, known := indexOf[detection.SourceSlice]
		if !known {
			return resultsdb.Pass{}, fmt.Errorf("detection %s belongs to %s, which is not in the plan",
				detection.ID, detection.SourceSlice)
		}
		record := resultsdb.Detection{
			ID:        detection.ID,
			Latitude:  formatCoordinate(detection.Latitude),
			Longitude: formatCoordinate(detection.Longitude),
			WidthM:    detection.WidthM,
			LengthM:   detection.LengthM,
			PixelX:    detection.PixelX,
			PixelY:    detection.PixelY,
		}
		if distance := formatShoreDistance(detection); distance != "" {
			record.DistanceToShoreM = &distance
		}
		pass.Slices[at].Detections = append(pass.Slices[at].Detections, record)
	}

	if raster != nil {
		pass.Raster = &resultsdb.Raster{
			Type: raster.Type, File: raster.File,
			South: raster.Bounds.South, West: raster.Bounds.West,
			North: raster.Bounds.North, East: raster.Bounds.East,

			MaxNativeZoom: &raster.MaxNativeZoom,
			MaxZoom:       &raster.MaxZoom,
		}
	}
	return pass, nil
}
