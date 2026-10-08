// buildPassPlan: builds the pass plan from a scene on disk, an area search or a product ID, whichever was given.
// planFromSceneOnDisk: builds a single-slice plan with unknown coverage from a scene already in the scenes folder.
// planFromAreaSearch: finds the newest pass over the area, prints it and downloads every slice into a plan.
// planFromProductID: builds a single-slice plan with unknown coverage by downloading one CDSE product.
// fetchSliceForPlan: downloads and unpacks a product, fills missing metadata from its name, returns its SliceJob.

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func buildPassPlan(config *Config) (*PassPlan, error) {
	switch {
	case config.SceneFileName != "":
		return planFromSceneOnDisk(config)
	case config.SearchAreaName != "":
		return planFromAreaSearch(config)
	case config.CDSEProductID != "":
		return planFromProductID(config)
	default:
		return nil, fmt.Errorf("nothing to do: pass -area bulgaria to fetch the newest pass, " +
			"-scene for a file you already have, or -product with a CDSE product id")
	}
}

func planFromSceneOnDisk(config *Config) (*PassPlan, error) {
	if _, err := os.Stat(config.SceneFilePath()); err != nil {
		return nil, fmt.Errorf("scene not found: %s", config.SceneFilePath())
	}
	fmt.Printf("Scene: %s (already downloaded)\n", config.SceneFilePath())

	unpackedName, err := ensureSceneIsUnpacked(config, config.SceneFileName)
	if err != nil {
		return nil, err
	}

	return &PassPlan{
		AreaName:        "",
		Slices:          []*SliceJob{newSliceJobFromFile(1, unpackedName)},
		CoveragePercent: coverageUnknown,
	}, nil
}

func planFromAreaSearch(config *Config) (*PassPlan, error) {
	pass, err := findNewestPassOverArea(config.SearchAreaName)
	if err != nil {
		return nil, err
	}

	target, err := targetExtentForArea(config.SearchAreaName)
	if err != nil {
		return nil, err
	}
	coverage := 100 * areaCoverageFraction(measureCoverage(pass, target))

	fmt.Printf("Newest pass over %s: %d slice(s), orbit %s, %.0f%% of the area\n",
		config.SearchAreaName, len(pass), pass[0].AbsoluteOrbit, coverage)
	for _, scene := range pass {
		fmt.Printf("  %s\n", scene.ProductName)
		fmt.Printf("    acquired %s (%s)\n",
			scene.AcquiredAt.Format("2006-01-02 15:04 UTC"), describeSceneAge(scene.AcquiredAt))
		if !scene.IsOnline {
			fmt.Println("    note: archived rather than online, so the download may be slow to start")
		}
	}
	fmt.Println()

	plan := &PassPlan{AreaName: config.SearchAreaName, CoveragePercent: coverage}
	for index, scene := range pass {
		slice, err := fetchSliceForPlan(config, index+1, scene)
		if err != nil {
			return nil, err
		}
		plan.Slices = append(plan.Slices, slice)
	}
	return plan, nil
}

func planFromProductID(config *Config) (*PassPlan, error) {
	scene := CatalogueScene{ProductID: config.CDSEProductID}
	slice, err := fetchSliceForPlan(config, 1, scene)
	if err != nil {
		return nil, err
	}
	return &PassPlan{
		Slices:          []*SliceJob{slice},
		CoveragePercent: coverageUnknown,
	}, nil
}

func fetchSliceForPlan(config *Config, index int, scene CatalogueScene) (*SliceJob, error) {
	archiveName, err := downloadSceneByProductID(config, scene.ProductID, scene.ProductName)
	if err != nil {
		return nil, err
	}

	unpackedName, err := ensureSceneIsUnpacked(config, archiveName)
	if err != nil {
		return nil, err
	}

	if scene.ProductName == "" {
		scene.ProductName = archiveName
	}
	if scene.AbsoluteOrbit == "" {
		scene.AbsoluteOrbit = absoluteOrbitFromProductName(archiveName)
	}
	if scene.MissionPrefix == "" {
		scene.MissionPrefix = missionPrefixFromProductName(archiveName)
	}
	if scene.AcquiredAt.IsZero() {
		scene.AcquiredAt = acquisitionTimeFromProductName(archiveName)
	}

	return newSliceJobFromCatalogue(index, scene, unpackedName,
		filepath.Join(config.ScenesDir, archiveName)), nil
}
