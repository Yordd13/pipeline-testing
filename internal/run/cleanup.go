// tidyAfterSuccess: deletes the run folder and scene archives after a fully successful run, unless that is unsafe.
// unexpectedEntriesInRunDir: lists entries in the run folder that are not one of the plan's slice folders.
// removeSceneArchives: deletes the downloaded and unpacked scenes of succeeded slices and returns the bytes freed.
// sceneArtefactsOf: returns the paths of a slice's unpacked scene and the zip archive it came from.
// pathSize: returns the size of a file, or the total size of a directory's files.
// directorySize: walks a directory tree and sums the sizes of all its files.
// printCleanupOutcome: prints what cleanup freed, or why it kept the working files.

package run

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"radarpipeline/internal/config"
	"radarpipeline/internal/pass"
)

type cleanupOutcome struct {
	BytesFreed    int64
	RunDirRemoved bool
	ArchivesFreed []string
	Unexpected    []string
	Skipped       string
}

func tidyAfterSuccess(cfg *config.Config, plan *pass.PassPlan) cleanupOutcome {
	outcome := cleanupOutcome{}

	if cfg.KeepIntermediate {
		outcome.Skipped = "-keep-intermediate was given, so the run folder and the scenes stay"
		return outcome
	}

	if !plan.AllSucceeded() {
		outcome.Skipped = fmt.Sprintf("%d of %d slices failed, so nothing was removed; "+
			"the products are the only way to retry without downloading again",
			len(plan.FailedSlices()), len(plan.Slices))
		return outcome
	}

	unexpected, err := unexpectedEntriesInRunDir(plan)
	if err != nil {
		outcome.Skipped = fmt.Sprintf("could not inspect %s, so it was left alone: %v",
			plan.RunDir, err)
		return outcome
	}
	if len(unexpected) > 0 {
		outcome.Unexpected = unexpected
		outcome.Skipped = fmt.Sprintf("%s holds entries this run did not create, "+
			"so it was left alone", plan.RunDir)
		return outcome
	}

	runDirBytes, err := directorySize(plan.RunDir)
	if err == nil {
		if removeErr := os.RemoveAll(plan.RunDir); removeErr == nil {
			outcome.BytesFreed += runDirBytes
			outcome.RunDirRemoved = true
		} else {
			outcome.Skipped = fmt.Sprintf("could not remove %s: %v", plan.RunDir, removeErr)
		}
	}

	outcome.BytesFreed += removeSceneArchives(plan, &outcome)
	return outcome
}

func unexpectedEntriesInRunDir(plan *pass.PassPlan) ([]string, error) {
	entries, err := os.ReadDir(plan.RunDir)
	if err != nil {
		return nil, err
	}

	known := map[string]bool{}
	for _, slice := range plan.Slices {
		known[slice.SliceName()] = true
	}

	unexpected := make([]string, 0)
	for _, entry := range entries {
		if !known[entry.Name()] {
			unexpected = append(unexpected, entry.Name())
		}
	}
	return unexpected, nil
}

func removeSceneArchives(plan *pass.PassPlan, outcome *cleanupOutcome) int64 {
	var freed int64
	for _, slice := range plan.Slices {
		if !slice.Succeeded() {
			continue
		}
		for _, path := range sceneArtefactsOf(slice) {
			size, err := pathSize(path)
			if err != nil {
				continue
			}
			if err := os.RemoveAll(path); err != nil {
				continue
			}
			freed += size
			outcome.ArchivesFreed = append(outcome.ArchivesFreed, filepath.Base(path))
		}
	}
	return freed
}

func sceneArtefactsOf(slice *pass.SliceJob) []string {
	paths := make([]string, 0, 2)
	if slice.SceneFileName != "" {
		paths = append(paths, filepath.Join("scenes", slice.SceneFileName))
	}
	if slice.ArchivePath != "" {
		paths = append(paths, slice.ArchivePath)
	} else if slice.SceneFileName != "" && !strings.HasSuffix(slice.SceneFileName, ".zip") {
		paths = append(paths, filepath.Join("scenes", slice.SceneFileName+".zip"))
	}
	return paths
}

func pathSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}
	return directorySize(path)
}

func directorySize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total, err
}

func printCleanupOutcome(outcome cleanupOutcome) {
	fmt.Println()
	if outcome.Skipped != "" {
		fmt.Println("Kept the working files:", outcome.Skipped)
		for _, name := range outcome.Unexpected {
			fmt.Printf("  unexpected: %s\n", name)
		}
		return
	}

	fmt.Printf("Freed %s\n", config.FormatBytes(outcome.BytesFreed))
	if outcome.RunDirRemoved {
		fmt.Println("  the run folder, including every slice's products")
	}
	if len(outcome.ArchivesFreed) > 0 {
		fmt.Printf("  scenes: %s\n", strings.Join(outcome.ArchivesFreed, ", "))
		fmt.Println("  Reprocessing these needs a fresh download from CDSE.")
	}
}
