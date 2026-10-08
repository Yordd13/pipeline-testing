// BuildPassPlan: builds the pass plan from a scene on disk, an area search or a product ID, whichever was given.
// planFromSceneOnDisk: builds a single-slice plan with unknown coverage from a scene already in the scenes folder.
// planFromAreaSearch: finds the newest pass over the area, prints it and downloads every slice into a plan.
// planFromProductID: builds a single-slice plan with unknown coverage by downloading one CDSE product.
// fetchSliceForPlan: downloads and unpacks a product, fills missing metadata from its name, returns its SliceJob.

package pass

import (
	"fmt"
	"os"
	"path/filepath"

	"radarpipeline/internal/cdse"
	"radarpipeline/internal/config"
)

func BuildPassPlan(cfg *config.Config) (*PassPlan, error) {
	switch {
	case cfg.SceneFileName != "":
		return planFromSceneOnDisk(cfg)
	case cfg.SearchAreaName != "":
		return planFromAreaSearch(cfg)
	case cfg.CDSEProductID != "":
		return planFromProductID(cfg)
	default:
		return nil, fmt.Errorf("nothing to do: pass -area bulgaria to fetch the newest pass, " +
			"-scene for a file you already have, or -product with a CDSE product id")
	}
}

func planFromSceneOnDisk(cfg *config.Config) (*PassPlan, error) {
	if _, err := os.Stat(cfg.SceneFilePath()); err != nil {
		return nil, fmt.Errorf("scene not found: %s", cfg.SceneFilePath())
	}
	fmt.Printf("Scene: %s (already downloaded)\n", cfg.SceneFilePath())

	unpackedName, err := cdse.EnsureSceneIsUnpacked(cfg, cfg.SceneFileName)
	if err != nil {
		return nil, err
	}

	return &PassPlan{
		AreaName:        "",
		Slices:          []*SliceJob{newSliceJobFromFile(1, unpackedName)},
		CoveragePercent: CoverageUnknown,
	}, nil
}

func planFromAreaSearch(cfg *config.Config) (*PassPlan, error) {
	pass, err := cdse.FindNewestPassOverArea(cfg.SearchAreaName)
	if err != nil {
		return nil, err
	}

	target, err := targetExtentForArea(cfg.SearchAreaName)
	if err != nil {
		return nil, err
	}
	coverage := 100 * areaCoverageFraction(cdse.MeasureCoverage(pass, target))

	fmt.Printf("Newest pass over %s: %d slice(s), orbit %s, %.0f%% of the area\n",
		cfg.SearchAreaName, len(pass), pass[0].AbsoluteOrbit, coverage)
	for _, scene := range pass {
		fmt.Printf("  %s\n", scene.ProductName)
		fmt.Printf("    acquired %s (%s)\n",
			scene.AcquiredAt.Format("2006-01-02 15:04 UTC"), cdse.DescribeSceneAge(scene.AcquiredAt))
		if !scene.IsOnline {
			fmt.Println("    note: archived rather than online, so the download may be slow to start")
		}
	}
	fmt.Println()

	plan := &PassPlan{AreaName: cfg.SearchAreaName, CoveragePercent: coverage}
	for index, scene := range pass {
		slice, err := fetchSliceForPlan(cfg, index+1, scene)
		if err != nil {
			return nil, err
		}
		plan.Slices = append(plan.Slices, slice)
	}
	return plan, nil
}

func planFromProductID(cfg *config.Config) (*PassPlan, error) {
	scene := cdse.CatalogueScene{ProductID: cfg.CDSEProductID}
	slice, err := fetchSliceForPlan(cfg, 1, scene)
	if err != nil {
		return nil, err
	}
	return &PassPlan{
		Slices:          []*SliceJob{slice},
		CoveragePercent: CoverageUnknown,
	}, nil
}

func fetchSliceForPlan(cfg *config.Config, index int, scene cdse.CatalogueScene) (*SliceJob, error) {
	archiveName, err := cdse.DownloadSceneByProductID(cfg, scene.ProductID, scene.ProductName)
	if err != nil {
		return nil, err
	}

	unpackedName, err := cdse.EnsureSceneIsUnpacked(cfg, archiveName)
	if err != nil {
		return nil, err
	}

	if scene.ProductName == "" {
		scene.ProductName = archiveName
	}
	if scene.AbsoluteOrbit == "" {
		scene.AbsoluteOrbit = cdse.AbsoluteOrbitFromProductName(archiveName)
	}
	if scene.MissionPrefix == "" {
		scene.MissionPrefix = cdse.MissionPrefixFromProductName(archiveName)
	}
	if scene.AcquiredAt.IsZero() {
		scene.AcquiredAt = AcquisitionTimeFromProductName(archiveName)
	}

	return newSliceJobFromCatalogue(index, scene, unpackedName,
		filepath.Join(cfg.ScenesDir, archiveName)), nil
}
