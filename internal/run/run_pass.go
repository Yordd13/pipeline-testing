// ProcessPass: processes every slice under a timeout, then merges, draws, stores in MySQL and cleans up.
// processOneSlice: runs preprocessing and detection for one slice in its own subfolder, recording any failure.
// makeSurvivingProductsViewable: strips the detection geometry from every succeeded slice's detection product.
// printPassSummary: prints the detection counts at each filtering step, the area coverage and the pass details.

package run

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"radarpipeline/internal/config"
	"radarpipeline/internal/detection"
	"radarpipeline/internal/pass"
	"radarpipeline/internal/raster"
	"radarpipeline/internal/snap"
)

func ProcessPass(cfg *config.Config, plan *pass.PassPlan) error {
	ctx, stopListeningForInterrupt := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stopListeningForInterrupt()

	ctx, cancelTimeout := context.WithTimeout(ctx,
		time.Duration(cfg.ProcessingTimeoutMinutes)*time.Minute)
	defer cancelTimeout()

	runDir, err := snap.CreateTimestampedRunDir(cfg)
	if err != nil {
		return err
	}
	plan.RunDir = runDir

	startedAt := time.Now()
	for _, slice := range plan.Slices {
		processOneSlice(ctx, cfg, plan, slice)
	}

	fmt.Printf("\nProcessed %d of %d slice(s) in %s\n",
		len(plan.SucceededSlices()), len(plan.Slices), time.Since(startedAt).Round(time.Second))
	for _, slice := range plan.FailedSlices() {
		fmt.Printf("  %s failed: %v\n", slice.SliceName(), slice.Failure)
	}

	if len(plan.SucceededSlices()) == 0 {
		return fmt.Errorf("every slice failed, so there is nothing to merge")
	}

	outcome, err := detection.MergeDetections(cfg, plan)
	if err != nil {
		return err
	}
	printPassSummary(plan, outcome)

	raster := raster.ExportPassRaster(cfg, plan, outcome.RunID)

	databaseErr := saveRunToDatabase(cfg, plan, outcome, raster)
	if databaseErr != nil {
		fmt.Printf("\nERROR: this run's results could not be stored and are lost: %v\n", databaseErr)
	}

	makeSurvivingProductsViewable(cfg, plan)
	printCleanupOutcome(tidyAfterSuccess(cfg, plan))

	if databaseErr != nil {
		return fmt.Errorf("the run was processed but not stored in MySQL, so its results are lost: %w",
			databaseErr)
	}
	return nil
}

func processOneSlice(ctx context.Context, cfg *config.Config, plan *pass.PassPlan, slice *pass.SliceJob) {
	fmt.Printf("\n########## %s of %d: %s ##########\n",
		slice.SliceName(), len(plan.Slices), slice.SceneFileName)

	slice.RunSubDir = filepath.Join(plan.RunDir, slice.SliceName())
	if err := os.MkdirAll(slice.RunSubDir, 0o755); err != nil {
		slice.Failure = fmt.Errorf("cannot create %s: %w", slice.RunSubDir, err)
		return
	}

	snap.NameOutputsAfterScene(cfg, slice.SceneFileName)

	preprocessedPath, err := snap.RunGraph(ctx, cfg,
		snap.PreprocessRun(cfg, slice.SceneFileName), slice.RunSubDir)
	if err != nil {
		slice.Failure = err
		return
	}
	if err := snap.CheckFileWasWritten(preprocessedPath); err != nil {
		slice.Failure = err
		return
	}
	slice.PreprocessedPath = preprocessedPath

	if cfg.SkipDetection {
		slice.Failure = fmt.Errorf("-no-detect was given, so no detections were produced")
		return
	}

	detection, err := detection.DetectionRunForSlice(cfg, slice)
	if err != nil {
		slice.Failure = err
		return
	}

	detectionPath, err := snap.RunGraph(ctx, cfg, detection, slice.RunSubDir)
	if err != nil {
		slice.Failure = err
		return
	}
	if err := snap.CheckFileWasWritten(detectionPath); err != nil {
		slice.Failure = err
		return
	}
	slice.DetectionProductPath = detectionPath

	if err := snap.ReportResult(cfg, detectionPath); err != nil {
		slice.Failure = err
	}
}

func makeSurvivingProductsViewable(cfg *config.Config, plan *pass.PassPlan) {
	for _, slice := range plan.SucceededSlices() {
		if err := snap.MakeDetectionProductViewable(cfg, slice.DetectionProductPath); err != nil {
			fmt.Printf("Note: could not clear %s's detection geometry: %v\n",
				slice.SliceName(), err)
		}
	}
}

func printPassSummary(plan *pass.PassPlan, outcome detection.MergeOutcome) {
	fmt.Println()
	fmt.Printf("CFAR found:     %d across %d slice(s)\n", outcome.RawCFAR, outcome.SlicesRead)
	fmt.Printf("  outside the area: %d dropped\n", outcome.DroppedOutsideAOI)
	if outcome.SeamDuplicates > 0 {
		fmt.Printf("  on the seam:      %d dropped as the same vessel within %.0f m\n",
			outcome.SeamDuplicates, detection.SeamDuplicateRadiusMetres)
	}
	fmt.Printf("Detections:     %d in Bulgarian waters\n", outcome.Written)

	if outcome.Written == 0 && outcome.RawCFAR > 0 {
		fmt.Printf("                CFAR did find %d target(s), all outside the area.\n",
			outcome.RawCFAR)
	}
	if outcome.SlicesEmpty > 0 {
		fmt.Printf("  %d of %d slice(s) held none at all\n", outcome.SlicesEmpty, outcome.SlicesRead)
	}

	switch {
	case plan.CoveragePercent < 0:
		fmt.Println("Area covered:   unknown; this run started from a file, not a search,")
		fmt.Println("                so nothing measured how much of the coast it reaches")
	case plan.CoveragePercent >= 99:
		fmt.Printf("Area covered:   %.0f%% of %s waters\n", plan.CoveragePercent, plan.AreaName)
	default:
		fmt.Printf("Area covered:   %.0f%% of %s waters — PARTIAL\n",
			plan.CoveragePercent, plan.AreaName)
		fmt.Println("                Absence of detections outside that share is not")
		fmt.Println("                evidence of empty water.")
	}

	if orbit := plan.AbsoluteOrbit(); orbit != "" {
		fmt.Printf("Pass:           orbit %s, acquired %s\n",
			orbit, plan.PassStart().UTC().Format("2006-01-02T15:04:05Z"))
	}
	fmt.Printf("Run:            %s (stored in MySQL at the end of the run)\n", outcome.RunID)
}
