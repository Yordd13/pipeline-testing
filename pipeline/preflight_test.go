// preflightProject: lays out a temporary project whose graphs and coastline pass every preflight check.
// TestPreflightStopsAtTheFirstProblem: checks each broken setting stops preflight with its own message, in check order.
// TestPreflightIgnoresTheDetectionGraphWithNoDetect: checks -no-detect skips the detection graph checks entirely.
// TestDownloadingWithCredentialsSet: checks a download passes the credential check when both CDSE variables are set.
// TestContainerMemoryHasToFitDocker: checks a container limit above Docker's memory is refused unless Docker cannot say.
// TestDockerThatIsNotRunningIsReported: checks a stopped Docker daemon is reported with Docker's own message.
// TestPreflightNotes: checks the warnings for two scene sources, no calibration and an empty orbit cache, and their absence.
// TestLandSeaMaskWithoutAMaskIsAllowed: checks graphs without Land-Sea-Mask, or masking without SRTM or geometry, are accepted.

package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func preflightProject(t *testing.T) Config {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("CDSE_USERNAME", "")
	t.Setenv("CDSE_PASSWORD", "")

	config := newDefaultConfig()
	config.GraphsDir = filepath.Join(root, "graphs")
	config.ScenesDir = filepath.Join(root, "scenes")
	config.OutputDir = filepath.Join(root, "out")
	config.AuxDataDir = filepath.Join(root, "auxdata")
	config.MySQLEnvFile = filepath.Join(root, "mysql", "absent.env")
	config.SceneFileName = runSceneName

	writeText(t, config.GraphFilePath(), preprocessGraph)
	writeText(t, config.DetectionGraphFilePath(), maskingDetectionGraph)
	for _, extension := range shapefileSidecars {
		writeText(t, filepath.Join(config.GraphsDir, "bg_coast"+extension), "part")
	}
	return config
}

func TestPreflightStopsAtTheFirstProblem(t *testing.T) {
	cases := []struct {
		name    string
		breakIt func(t *testing.T, config *Config)
		want    string
	}{
		{"nothing wrong until the database", func(*testing.T, *Config) {}, "results database is not reachable"},
		{"no heap", func(_ *testing.T, c *Config) { c.SnapHeapGB = 0 }, "-heap must be a positive"},
		{"no source", func(_ *testing.T, c *Config) { c.SceneFileName = "" }, "nothing to do"},
		{"unknown area", func(_ *testing.T, c *Config) {
			c.SceneFileName, c.SearchAreaName = "", "atlantis"
		}, `unknown area "atlantis"`},
		{"download without .env", func(_ *testing.T, c *Config) {
			c.SceneFileName, c.SearchAreaName = "", "bulgaria"
		}, "there is no .env"},
		{"download with a .env lacking credentials", func(t *testing.T, c *Config) {
			c.SceneFileName, c.CDSEProductID = "", "some-id"
			writeText(t, envFileName, "OTHER=1\n")
		}, "does not set both CDSE_USERNAME and CDSE_PASSWORD"},
		{"stage 1 graph missing", func(_ *testing.T, c *Config) { c.GraphFileName = "absent.xml" }, "graph not found"},
		{"stage 2 graph missing", func(_ *testing.T, c *Config) {
			c.DetectionGraphFileName = "absent.xml"
		}, "detection graph not found"},
		{"stage 2 unreadable", func(t *testing.T, c *Config) {
			writeText(t, c.DetectionGraphFilePath(), "<graph><node>")
		}, "not readable as XML"},
		{"SRTM mask with no geometry", func(t *testing.T, c *Config) {
			writeText(t, c.DetectionGraphFilePath(), strings.Replace(maskingDetectionGraph,
				"<useSRTM>false</useSRTM><geometry>bg_coast</geometry>", "<useSRTM>true</useSRTM><geometry/>", 1))
		}, "discards Bulgarian coastal water"},
		{"coastline missing entirely", func(t *testing.T, c *Config) {
			writeText(t, c.DetectionGraphFilePath(), strings.Replace(maskingDetectionGraph, "bg_coast", "elsewhere", 1))
		}, "none of it is in"},
		{"coastline missing a part", func(t *testing.T, c *Config) {
			writeText(t, c.DetectionGraphFilePath(), strings.Replace(maskingDetectionGraph, "bg_coast", "partial", 1))
			writeText(t, filepath.Join(c.GraphsDir, "partial.shp"), "part")
		}, "partial.shx, partial.dbf, partial.prj missing"},
		{"DIMAP graph, other output", func(_ *testing.T, c *Config) { c.ResultFileName = "result.tif" }, "must end in .dim"},
		{"other graph, DIMAP output", func(t *testing.T, c *Config) {
			writeText(t, c.GraphFilePath(), "<graph><node><operator>Calibration</operator></node></graph>")
		}, "should not end in .dim"},
		{"both stages calibrate", func(t *testing.T, c *Config) {
			writeText(t, c.DetectionGraphFilePath(), strings.Replace(maskingDetectionGraph,
				"<operator>Read</operator>", "<operator>Calibration</operator>", 1))
		}, "both calibrate"},
		{"output folder blocked by a file", func(t *testing.T, c *Config) {
			writeText(t, c.OutputDir, "not a folder")
		}, "cannot create"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			config := preflightProject(t)
			c.breakIt(t, &config)
			err := runPreflightChecks(&config)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestPreflightIgnoresTheDetectionGraphWithNoDetect(t *testing.T) {
	config := preflightProject(t)
	config.SkipDetection = true
	config.DetectionGraphFileName = "absent.xml"
	err := runPreflightChecks(&config)
	if err == nil || !strings.Contains(err.Error(), "results database") {
		t.Errorf("err = %v, want to get as far as the database", err)
	}
}

func TestDownloadingWithCredentialsSet(t *testing.T) {
	config := preflightProject(t)
	config.SceneFileName, config.SearchAreaName = "", "burgas"
	t.Setenv("CDSE_USERNAME", "u")
	t.Setenv("CDSE_PASSWORD", "p")
	if err := checkCredentialsBeforeDownloading(&config); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestContainerMemoryHasToFitDocker(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	cases := []struct {
		name     string
		memTotal string
		heap     int
		want     string
	}{
		{"fits", fmt.Sprint(14 * gib), 8, ""},
		{"too big", fmt.Sprint(14 * gib), 12, "Lower -heap to 10 or less"},
		{"docker will not say", "", 64, ""},
		{"docker says nonsense", "lots", 64, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(fakeDockerMemTotal, c.memTotal)
			config := newDefaultConfig()
			config.SnapHeapGB = c.heap
			err := checkContainerMemoryFitsDocker(&config)
			switch {
			case c.want == "" && err != nil:
				t.Errorf("err = %v, want none", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Errorf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestDockerThatIsNotRunningIsReported(t *testing.T) {
	t.Setenv(fakeDockerDown, "1")
	err := ensureDockerIsRunning()
	if err == nil || !strings.Contains(err.Error(), "Cannot connect to the Docker daemon") {
		t.Errorf("err = %v, want docker's own message passed on", err)
	}
}

func TestPreflightNotes(t *testing.T) {
	config := preflightProject(t)
	config.SearchAreaName, config.CDSEProductID = "bulgaria", "id"
	writeText(t, config.GraphFilePath(), "<graph><formatName>BEAM-DIMAP</formatName></graph>")

	said := captureStdout(t, func() {
		warnIfBothSceneSourcesGiven(&config)
		warnIfNothingCalibrates(&config)
		warnIfAuxDataCacheIsEmpty(&config)
	})
	for _, want := range []string{"ignoring -product", "neither preprocess_full.xml nor ShipDetection.xml calibrates",
		"will download orbit files"} {
		if !strings.Contains(said, want) {
			t.Errorf("notes lack %q:\n%s", want, said)
		}
	}

	writeText(t, filepath.Join(config.AuxDataDir, "Orbits", "x.EOF"), "orbit")
	config.CDSEProductID = ""
	config.SkipDetection = true
	if said := captureStdout(t, func() {
		warnIfBothSceneSourcesGiven(&config)
		warnIfNothingCalibrates(&config)
		warnIfAuxDataCacheIsEmpty(&config)
	}); said != "" {
		t.Errorf("nothing to note, yet it said %q", said)
	}
}

func TestLandSeaMaskWithoutAMaskIsAllowed(t *testing.T) {
	config := preflightProject(t)
	writeText(t, config.DetectionGraphFilePath(), "<graph><node id=\"Read\"><operator>Read</operator></node></graph>")
	if err := checkLandSeaMaskHasAMaskSource(&config); err != nil {
		t.Errorf("a graph without Land-Sea-Mask was refused: %v", err)
	}
	writeText(t, config.DetectionGraphFilePath(), strings.Replace(maskingDetectionGraph, "<geometry>bg_coast</geometry>", "", 1))
	if err := checkLandSeaMaskHasAMaskSource(&config); err != nil {
		t.Errorf("a mask with useSRTM=false and no geometry was refused: %v", err)
	}
}
