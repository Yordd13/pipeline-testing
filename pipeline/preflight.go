// runPreflightChecks: runs every check that must pass before slow work starts, then prints the advisory warnings.
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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runPreflightChecks(config *Config) error {
	if err := checkHeapSizeIsSensible(config); err != nil {
		return err
	}
	if err := checkSceneSourceIsUsable(config); err != nil {
		return err
	}
	if err := checkCredentialsBeforeDownloading(config); err != nil {
		return err
	}
	if err := checkGraphFileExists(config); err != nil {
		return err
	}
	if err := checkLandSeaMaskHasAMaskSource(config); err != nil {
		return err
	}
	if err := checkResultNameMatchesGraphFormat(config); err != nil {
		return err
	}
	if err := checkCalibrationHappensOnce(config); err != nil {
		return err
	}
	if err := createOutputDir(config); err != nil {
		return err
	}
	if err := checkResultsDatabaseIsReady(config); err != nil {
		return err
	}
	if err := ensureDockerIsRunning(); err != nil {
		return err
	}
	if err := checkContainerMemoryFitsDocker(config); err != nil {
		return err
	}

	warnIfBothSceneSourcesGiven(config)
	warnIfNothingCalibrates(config)
	warnIfAuxDataCacheIsEmpty(config)
	return nil
}

func checkSceneSourceIsUsable(config *Config) error {
	noSourceGiven := config.SceneFileName == "" &&
		config.SearchAreaName == "" &&
		config.CDSEProductID == ""
	if noSourceGiven {
		return fmt.Errorf("nothing to do: pass -area bulgaria to fetch the newest pass, " +
			"-scene for a file you already have, or -product with a CDSE product id")
	}

	if config.SceneFileName == "" && config.SearchAreaName != "" {
		if _, isKnownArea := footprintsByAreaName[config.SearchAreaName]; !isKnownArea {
			return fmt.Errorf("unknown area %q (known: %s)",
				config.SearchAreaName, knownAreaNames())
		}
	}
	return nil
}

func runWillDownload(config *Config) bool {
	return config.SceneFileName == "" &&
		(config.SearchAreaName != "" || config.CDSEProductID != "")
}

func checkCredentialsBeforeDownloading(config *Config) error {
	if !runWillDownload(config) {
		return nil
	}

	if _, err := readCDSECredentials(); err == nil {
		return nil
	}

	if _, err := os.Stat(envFileName); err != nil {
		return fmt.Errorf("downloading needs CDSE credentials and there is no %s — "+
			"copy .env.example to %s and fill in CDSE_USERNAME and CDSE_PASSWORD",
			envFileName, envFileName)
	}
	return fmt.Errorf("%s does not set both CDSE_USERNAME and CDSE_PASSWORD, "+
		"which downloading needs", envFileName)
}

func checkHeapSizeIsSensible(config *Config) error {
	if config.SnapHeapGB <= 0 {
		return fmt.Errorf("-heap must be a positive number of GB, got %d", config.SnapHeapGB)
	}
	return nil
}

func checkContainerMemoryFitsDocker(config *Config) error {
	availableGiB, err := dockerMemoryTotalGiB()
	if err != nil {
		return nil
	}

	requestedGiB := float64(config.ContainerMemoryLimitGB())
	if requestedGiB <= availableGiB {
		return nil
	}
	return fmt.Errorf(
		"-heap %d GB needs a %s container, but Docker only has %.1f GiB.\n"+
			"       Lower -heap to %d or less, or raise Docker Desktop's memory limit",
		config.SnapHeapGB, config.ContainerMemoryLimit(), availableGiB,
		int(availableGiB/containerMemoryHeadroomFactor))
}

func checkGraphFileExists(config *Config) error {
	if _, err := os.Stat(config.GraphFilePath()); err != nil {
		return fmt.Errorf("graph not found: %s", config.GraphFilePath())
	}

	if !config.SkipDetection {
		detectionGraphPath := config.DetectionGraphFilePath()
		if _, err := os.Stat(detectionGraphPath); err != nil {
			return fmt.Errorf("detection graph not found: %s "+
				"(pass -no-detect to stop after preprocessing)", detectionGraphPath)
		}
	}
	return nil
}

func checkCalibrationHappensOnce(config *Config) error {
	if config.SkipDetection {
		return nil
	}
	if countCalibratingGraphs(config) < 2 {
		return nil
	}
	return fmt.Errorf(
		"%s and %s both calibrate, which SNAP refuses on an already-calibrated "+
			"product.\n"+
			"       Remove the Calibration node from one of them, or pass "+
			"-no-detect to stop after preprocessing",
		config.GraphFileName, config.DetectionGraphFileName)
}

func countCalibratingGraphs(config *Config) int {
	count := 0
	for _, graphFilePath := range []string{config.GraphFilePath(), config.DetectionGraphFilePath()} {
		if graphContains(graphFilePath, calibrationOperatorTag) {
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

func checkResultNameMatchesGraphFormat(config *Config) error {
	graphWritesDimap := graphWritesBeamDimap(config.GraphFilePath())
	resultNameIsDimap := strings.HasSuffix(config.ResultFileName, ".dim")

	switch {
	case graphWritesDimap && !resultNameIsDimap:
		return fmt.Errorf("the graph writes BEAM-DIMAP, so -output must end in .dim (got %q)",
			config.ResultFileName)
	case !graphWritesDimap && resultNameIsDimap:
		return fmt.Errorf("the graph does not write BEAM-DIMAP, so -output should not end in .dim (got %q)",
			config.ResultFileName)
	}
	return nil
}

func createOutputDir(config *Config) error {
	if err := os.MkdirAll(config.OutputDir, 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", config.OutputDir, err)
	}
	return nil
}

func warnIfBothSceneSourcesGiven(config *Config) {
	if config.SearchAreaName != "" && config.CDSEProductID != "" {
		fmt.Printf("Note: both -area and -product given; using -area %s and ignoring -product.\n",
			config.SearchAreaName)
	}
}

func warnIfAuxDataCacheIsEmpty(config *Config) {
	entries, err := os.ReadDir(config.AuxDataDir)
	if err != nil || len(entries) == 0 {
		fmt.Printf("Note: %s is empty, so SNAP will download orbit files and DEM tiles this run.\n",
			config.AuxDataDir)
	}
}

func graphWritesBeamDimap(graphFilePath string) bool {
	return graphContains(graphFilePath, beamDimapFormatTag)
}

func warnIfNothingCalibrates(config *Config) {
	if config.SkipDetection || countCalibratingGraphs(config) > 0 {
		return
	}
	fmt.Printf("Note: neither %s nor %s calibrates, so detection runs on raw DN values\n"+
		"      rather than sigma0, and the size limits will not mean what they say.\n",
		config.GraphFileName, config.DetectionGraphFileName)
}

func checkLandSeaMaskHasAMaskSource(config *Config) error {
	if config.SkipDetection {
		return nil
	}

	settings, err := readLandSeaMaskSettings(config.DetectionGraphFilePath())
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
			config.DetectionGraphFileName)
	}
	if settings.Geometry == "" {
		return nil
	}
	return checkShapefileIsComplete(config, settings.Geometry)
}

var shapefileSidecars = []string{".shp", ".shx", ".dbf", ".prj"}

func checkShapefileIsComplete(config *Config, geometryName string) error {
	stem := filepath.Join(config.GraphsDir, geometryName)

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
			config.DetectionGraphFileName, geometryName, config.GraphsDir)
	}
	return fmt.Errorf(
		"the vector %q in %s/ is incomplete: %s missing.\n"+
			"       A shapefile needs all of %s together, or Import-Vector imports "+
			"an empty vector and the mask stops masking without saying so",
		geometryName, config.GraphsDir, strings.Join(missing, ", "),
		strings.Join(shapefileSidecars, " "))
}
