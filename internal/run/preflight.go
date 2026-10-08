// RunPreflightChecks: runs every check that must pass before slow work starts, then prints the advisory warnings.
// checkSceneSourceIsUsable: fails when no scene source was given or the named search area is unknown.
// runWillDownload: reports whether this run will download from CDSE rather than use a scene on disk.
// checkCredentialsBeforeDownloading: fails when a download is needed but CDSE credentials are not available.
// checkHeapSizeIsSensible: fails when the configured SNAP heap size is not positive.
// checkContainerMemoryFitsDocker: fails when the container memory limit exceeds the memory Docker has.
// checkGraphFileExists: fails when the preprocessing graph, or the detection graph if needed, is missing.
// checkCalibrationHappensOnce: fails when both the preprocessing and detection graphs contain a Calibration node.
// countCalibratingGraphs: counts how many of the two stage graphs contain a Calibration operator.
// graphContains: reports whether a graph file's text contains the given phrase, false if it cannot be read.
// checkResultNameMatchesGraphFormat: fails when the result name's .dim suffix disagrees with the graph's format.
// createOutputDir: creates the output directory if it does not exist.
// warnIfBothSceneSourcesGiven: notes that -product is ignored when both -area and -product are given.
// warnIfAuxDataCacheIsEmpty: notes that SNAP will download orbits and DEM tiles when auxdata is empty.
// graphWritesBeamDimap: reports whether a graph file writes BEAM-DIMAP output.
// warnIfNothingCalibrates: notes when neither graph calibrates, so detection will run on raw DN values.
// checkLandSeaMaskHasAMaskSource: fails if Land-Sea-Mask uses SRTM with no geometry; checks any named shapefile.
// checkShapefileIsComplete: fails when any of the .shp, .shx, .dbf or .prj files of the named vector is missing.

package run

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"radarpipeline/internal/cdse"
	"radarpipeline/internal/config"
	"radarpipeline/internal/snap"
)

func RunPreflightChecks(cfg *config.Config) error {
	if err := checkHeapSizeIsSensible(cfg); err != nil {
		return err
	}
	if err := checkSceneSourceIsUsable(cfg); err != nil {
		return err
	}
	if err := checkCredentialsBeforeDownloading(cfg); err != nil {
		return err
	}
	if err := checkGraphFileExists(cfg); err != nil {
		return err
	}
	if err := checkLandSeaMaskHasAMaskSource(cfg); err != nil {
		return err
	}
	if err := checkResultNameMatchesGraphFormat(cfg); err != nil {
		return err
	}
	if err := checkCalibrationHappensOnce(cfg); err != nil {
		return err
	}
	if err := createOutputDir(cfg); err != nil {
		return err
	}
	if err := checkResultsDatabaseIsReady(cfg); err != nil {
		return err
	}
	if err := snap.EnsureDockerIsRunning(); err != nil {
		return err
	}
	if err := checkContainerMemoryFitsDocker(cfg); err != nil {
		return err
	}

	warnIfBothSceneSourcesGiven(cfg)
	warnIfNothingCalibrates(cfg)
	warnIfAuxDataCacheIsEmpty(cfg)
	return nil
}

func checkSceneSourceIsUsable(cfg *config.Config) error {
	noSourceGiven := cfg.SceneFileName == "" &&
		cfg.SearchAreaName == "" &&
		cfg.CDSEProductID == ""
	if noSourceGiven {
		return fmt.Errorf("nothing to do: pass -area bulgaria to fetch the newest pass, " +
			"-scene for a file you already have, or -product with a CDSE product id")
	}

	if cfg.SceneFileName == "" && cfg.SearchAreaName != "" {
		if _, isKnownArea := cdse.FootprintsByAreaName[cfg.SearchAreaName]; !isKnownArea {
			return fmt.Errorf("unknown area %q (known: %s)",
				cfg.SearchAreaName, cdse.KnownAreaNames())
		}
	}
	return nil
}

func runWillDownload(cfg *config.Config) bool {
	return cfg.SceneFileName == "" &&
		(cfg.SearchAreaName != "" || cfg.CDSEProductID != "")
}

func checkCredentialsBeforeDownloading(cfg *config.Config) error {
	if !runWillDownload(cfg) {
		return nil
	}

	if _, err := cdse.ReadCDSECredentials(); err == nil {
		return nil
	}

	if _, err := os.Stat(config.EnvFileName); err != nil {
		return fmt.Errorf("downloading needs CDSE credentials and there is no %s — "+
			"copy .env.example to %s and fill in CDSE_USERNAME and CDSE_PASSWORD",
			config.EnvFileName, config.EnvFileName)
	}
	return fmt.Errorf("%s does not set both CDSE_USERNAME and CDSE_PASSWORD, "+
		"which downloading needs", config.EnvFileName)
}

func checkHeapSizeIsSensible(cfg *config.Config) error {
	if cfg.SnapHeapGB <= 0 {
		return fmt.Errorf("-heap must be a positive number of GB, got %d", cfg.SnapHeapGB)
	}
	return nil
}

func checkContainerMemoryFitsDocker(cfg *config.Config) error {
	availableGiB, err := snap.DockerMemoryTotalGiB()
	if err != nil {
		return nil
	}

	requestedGiB := float64(cfg.ContainerMemoryLimitGB())
	if requestedGiB <= availableGiB {
		return nil
	}
	return fmt.Errorf(
		"-heap %d GB needs a %s container, but Docker only has %.1f GiB.\n"+
			"       Lower -heap to %d or less, or raise Docker Desktop's memory limit",
		cfg.SnapHeapGB, cfg.ContainerMemoryLimit(), availableGiB,
		int(availableGiB/config.ContainerMemoryHeadroomFactor))
}

func checkGraphFileExists(cfg *config.Config) error {
	if _, err := os.Stat(cfg.GraphFilePath()); err != nil {
		return fmt.Errorf("graph not found: %s", cfg.GraphFilePath())
	}

	if !cfg.SkipDetection {
		detectionGraphPath := cfg.DetectionGraphFilePath()
		if _, err := os.Stat(detectionGraphPath); err != nil {
			return fmt.Errorf("detection graph not found: %s "+
				"(pass -no-detect to stop after preprocessing)", detectionGraphPath)
		}
	}
	return nil
}

func checkCalibrationHappensOnce(cfg *config.Config) error {
	if cfg.SkipDetection {
		return nil
	}
	if countCalibratingGraphs(cfg) < 2 {
		return nil
	}
	return fmt.Errorf(
		"%s and %s both calibrate, which SNAP refuses on an already-calibrated "+
			"product.\n"+
			"       Remove the Calibration node from one of them, or pass "+
			"-no-detect to stop after preprocessing",
		cfg.GraphFileName, cfg.DetectionGraphFileName)
}

func countCalibratingGraphs(cfg *config.Config) int {
	count := 0
	for _, graphFilePath := range []string{cfg.GraphFilePath(), cfg.DetectionGraphFilePath()} {
		if graphContains(graphFilePath, config.CalibrationOperatorTag) {
			count++
		}
	}
	return count
}

func graphContains(graphFilePath, phrase string) bool {
	contents, err := os.ReadFile(graphFilePath)
	if err != nil {
		return false
	}
	return strings.Contains(string(contents), phrase)
}

func checkResultNameMatchesGraphFormat(cfg *config.Config) error {
	graphWritesDimap := graphWritesBeamDimap(cfg.GraphFilePath())
	resultNameIsDimap := strings.HasSuffix(cfg.ResultFileName, ".dim")

	switch {
	case graphWritesDimap && !resultNameIsDimap:
		return fmt.Errorf("the graph writes BEAM-DIMAP, so -output must end in .dim (got %q)",
			cfg.ResultFileName)
	case !graphWritesDimap && resultNameIsDimap:
		return fmt.Errorf("the graph does not write BEAM-DIMAP, so -output should not end in .dim (got %q)",
			cfg.ResultFileName)
	}
	return nil
}

func createOutputDir(cfg *config.Config) error {
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", cfg.OutputDir, err)
	}
	return nil
}

func warnIfBothSceneSourcesGiven(cfg *config.Config) {
	if cfg.SearchAreaName != "" && cfg.CDSEProductID != "" {
		fmt.Printf("Note: both -area and -product given; using -area %s and ignoring -product.\n",
			cfg.SearchAreaName)
	}
}

func warnIfAuxDataCacheIsEmpty(cfg *config.Config) {
	entries, err := os.ReadDir(cfg.AuxDataDir)
	if err != nil || len(entries) == 0 {
		fmt.Printf("Note: %s is empty, so SNAP will download orbit files and DEM tiles this run.\n",
			cfg.AuxDataDir)
	}
}

func graphWritesBeamDimap(graphFilePath string) bool {
	return graphContains(graphFilePath, config.BeamDimapFormatTag)
}

func warnIfNothingCalibrates(cfg *config.Config) {
	if cfg.SkipDetection || countCalibratingGraphs(cfg) > 0 {
		return
	}
	fmt.Printf("Note: neither %s nor %s calibrates, so detection runs on raw DN values\n"+
		"      rather than sigma0, and the size limits will not mean what they say.\n",
		cfg.GraphFileName, cfg.DetectionGraphFileName)
}

func checkLandSeaMaskHasAMaskSource(cfg *config.Config) error {
	if cfg.SkipDetection {
		return nil
	}

	settings, err := snap.ReadLandSeaMaskSettings(cfg.DetectionGraphFilePath())
	if err != nil {
		return err
	}
	if !settings.Present {
		return nil
	}
	if settings.UseSRTM && settings.Geometry == "" {
		return fmt.Errorf(
			"%s masks with useSRTM=true and an empty <geometry/>, which discards "+
				"Bulgarian coastal water.\n"+
				"       SRTM returns 0 over this sea rather than no-data, so the SRTM "+
				"branch treats it as land.\n"+
				"       Either give Land-Sea-Mask a vector geometry with useSRTM=false, "+
				"or remove the node",
			cfg.DetectionGraphFileName)
	}
	if settings.Geometry == "" {
		return nil
	}
	return checkShapefileIsComplete(cfg, settings.Geometry)
}

var shapefileSidecars = []string{".shp", ".shx", ".dbf", ".prj"}

func checkShapefileIsComplete(cfg *config.Config, geometryName string) error {
	stem := filepath.Join(cfg.GraphsDir, geometryName)

	missing := make([]string, 0, len(shapefileSidecars))
	for _, extension := range shapefileSidecars {
		if _, err := os.Stat(stem + extension); err != nil {
			missing = append(missing, geometryName+extension)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	if len(missing) == len(shapefileSidecars) {
		return fmt.Errorf(
			"%s masks with the vector %q, and none of it is in %s/.\n"+
				"       Run scripts/get_coastline.sh once to fetch and clip it, or "+
				"pass -detection-graph with a graph that does not mask",
			cfg.DetectionGraphFileName, geometryName, cfg.GraphsDir)
	}
	return fmt.Errorf(
		"the vector %q in %s/ is incomplete: %s missing.\n"+
			"       A shapefile needs all of %s together, or Import-Vector imports "+
			"an empty vector and the mask stops masking without saying so",
		geometryName, cfg.GraphsDir, strings.Join(missing, ", "),
		strings.Join(shapefileSidecars, " "))
}
