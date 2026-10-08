// checkResultsDatabaseIsReady: fails when the results database cannot be opened or its schema check fails.
// saveRunToDatabase: builds the pass record and saves it to MySQL, replacing any existing copy of the pass.
// passRecord: converts the plan, merge outcome and raster description into a resultsdb.Pass record.

package run

import (
	"fmt"
	"strconv"
	"time"

	"radarpipeline/internal/config"
	"radarpipeline/internal/detection"
	"radarpipeline/internal/pass"
	"radarpipeline/internal/raster"
	"radarpipeline/internal/resultsdb"
)

func checkResultsDatabaseIsReady(cfg *config.Config) error {
	db, err := resultsdb.Open(cfg.MySQLEnvFile)
	if err != nil {
		return fmt.Errorf("the results database is not reachable, so this run could not store "+
			"what it finds: %w\n       Start it with: docker compose up -d   (in mysql/)", err)
	}
	defer db.Close()
	return resultsdb.CheckSchema(db)
}

func saveRunToDatabase(cfg *config.Config, plan *pass.PassPlan, outcome detection.MergeOutcome, raster *raster.RasterSidecar) error {
	pass, err := passRecord(plan, outcome, raster)
	if err != nil {
		return err
	}
	db, err := resultsdb.Open(cfg.MySQLEnvFile)
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

func passRecord(plan *pass.PassPlan, outcome detection.MergeOutcome, raster *raster.RasterSidecar) (resultsdb.Pass, error) {
	orbit, err := strconv.Atoi(plan.AbsoluteOrbit())
	if err != nil {
		return resultsdb.Pass{}, fmt.Errorf("the pass has no absolute orbit (%q)", plan.AbsoluteOrbit())
	}
	row := resultsdb.Pass{
		Start:             plan.PassStart(),
		Orbit:             orbit,
		Area:              plan.AreaName,
		SlicesProcessed:   len(plan.SucceededSlices()),
		SlicesFailed:      len(plan.FailedSlices()),
		RawCFAR:           outcome.RawCFAR,
		DroppedOutsideAOI: outcome.DroppedOutsideAOI,
		SeamDuplicates:    outcome.SeamDuplicates,
		Written:           outcome.Written,
		AOIVertices:       len(detection.BulgarianWatersAOI),
	}

	if text := plan.CoverageText(); text != "" {
		coverage, err := strconv.Atoi(text)
		if err != nil {
			return resultsdb.Pass{}, fmt.Errorf("coverage %q is not a whole number", text)
		}
		row.CoveragePercent = &coverage
	}

	indexOf := map[string]int{}
	for _, slice := range plan.Slices {
		indexOf[slice.SliceName()] = len(row.Slices)
		row.Slices = append(row.Slices, resultsdb.Slice{
			Index:       slice.Index,
			ProductName: pass.TrimProductExtensions(slice.ProductName),
			ProductID:   slice.ProductID,
			Mission:     slice.Mission,
			Start:       slice.AcquiredAt,
		})
	}
	for _, found := range outcome.Kept {
		at, known := indexOf[found.SourceSlice]
		if !known {
			return resultsdb.Pass{}, fmt.Errorf("detection %s belongs to %s, which is not in the plan",
				found.ID, found.SourceSlice)
		}
		record := resultsdb.Detection{
			ID:        found.ID,
			Latitude:  detection.FormatCoordinate(found.Latitude),
			Longitude: detection.FormatCoordinate(found.Longitude),
			WidthM:    found.WidthM,
			LengthM:   found.LengthM,
			PixelX:    found.PixelX,
			PixelY:    found.PixelY,
		}
		if distance := detection.FormatShoreDistance(found); distance != "" {
			record.DistanceToShoreM = &distance
		}
		row.Slices[at].Detections = append(row.Slices[at].Detections, record)
	}

	if raster != nil {
		row.Raster = &resultsdb.Raster{
			Type: raster.Type, File: raster.File,
			South: raster.Bounds.South, West: raster.Bounds.West,
			North: raster.Bounds.North, East: raster.Bounds.East,

			MaxNativeZoom: &raster.MaxNativeZoom,
			MaxZoom:       &raster.MaxZoom,
		}
	}
	return row, nil
}
