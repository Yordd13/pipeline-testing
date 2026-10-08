// preflightProject: lays out a temporary project whose graphs and coastline pass every preflight check.
// TestPreflightStopsAtTheFirstProblem: checks each broken setting stops preflight with its own message, in check order.
// TestPreflightIgnoresTheDetectionGraphWithNoDetect: checks -no-detect skips the detection graph checks entirely.
// TestDownloadingWithCredentialsSet: checks a download passes the credential check when both CDSE variables are set.
// TestContainerMemoryHasToFitDocker: checks a container limit above Docker's memory is refused unless Docker cannot say.
// TestDockerThatIsNotRunningIsReported: checks a stopped Docker daemon is reported with Docker's own message.
// TestPreflightNotes: checks the warnings for two scene sources, no calibration and an empty orbit cache, and their absence.
// TestLandSeaMaskWithoutAMaskIsAllowed: checks graphs without Land-Sea-Mask, or masking without SRTM or geometry, are accepted.

package run

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"radarpipeline/internal/config"
	"radarpipeline/internal/snap"
	"radarpipeline/internal/testsupport"
)

func preflightProject(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("CDSE_USERNAME", "")
	t.Setenv("CDSE_PASSWORD", "")

	cfg := config.NewDefaultConfig()
	cfg.GraphsDir = filepath.Join(root, "graphs")
	cfg.ScenesDir = filepath.Join(root, "scenes")
	cfg.OutputDir = filepath.Join(root, "out")
	cfg.AuxDataDir = filepath.Join(root, "auxdata")
	cfg.MySQLEnvFile = filepath.Join(root, "mysql", "absent.env")
	cfg.SceneFileName = runSceneName

	testsupport.WriteText(t, cfg.GraphFilePath(), preprocessGraph)
	testsupport.WriteText(t, cfg.DetectionGraphFilePath(), testsupport.MaskingDetectionGraph)
	for _, extension := range shapefileSidecars {
		testsupport.WriteText(t, filepath.Join(cfg.GraphsDir, "bg_coast"+extension), "part")
	}
	return cfg
}

func TestPreflightStopsAtTheFirstProblem(t *testing.T) {
	cases := []struct {
		name    string
		breakIt func(t *testing.T, cfg *config.Config)
		want    string
	}{
		{"nothing wrong until the database", func(*testing.T, *config.Config) {}, "results database is not reachable"},
		{"no heap", func(_ *testing.T, c *config.Config) { c.SnapHeapGB = 0 }, "-heap must be a positive"},
		{"no source", func(_ *testing.T, c *config.Config) { c.SceneFileName = "" }, "nothing to do"},
		{"unknown area", func(_ *testing.T, c *config.Config) {
			c.SceneFileName, c.SearchAreaName = "", "atlantis"
		}, `unknown area "atlantis"`},
		{"download without .env", func(_ *testing.T, c *config.Config) {
			c.SceneFileName, c.SearchAreaName = "", "bulgaria"
		}, "there is no .env"},
		{"download with a .env lacking credentials", func(t *testing.T, c *config.Config) {
			c.SceneFileName, c.CDSEProductID = "", "some-id"
			testsupport.WriteText(t, config.EnvFileName, "OTHER=1\n")
		}, "does not set both CDSE_USERNAME and CDSE_PASSWORD"},
		{"stage 1 graph missing", func(_ *testing.T, c *config.Config) { c.GraphFileName = "absent.xml" }, "graph not found"},
		{"stage 2 graph missing", func(_ *testing.T, c *config.Config) {
			c.DetectionGraphFileName = "absent.xml"
		}, "detection graph not found"},
		{"stage 2 unreadable", func(t *testing.T, c *config.Config) {
			testsupport.WriteText(t, c.DetectionGraphFilePath(), "<graph><node>")
		}, "not readable as XML"},
		{"SRTM mask with no geometry", func(t *testing.T, c *config.Config) {
			testsupport.WriteText(t, c.DetectionGraphFilePath(), strings.Replace(testsupport.MaskingDetectionGraph,
				"<useSRTM>false</useSRTM><geometry>bg_coast</geometry>", "<useSRTM>true</useSRTM><geometry/>", 1))
		}, "discards Bulgarian coastal water"},
		{"coastline missing entirely", func(t *testing.T, c *config.Config) {
			testsupport.WriteText(t, c.DetectionGraphFilePath(), strings.Replace(testsupport.MaskingDetectionGraph, "bg_coast", "elsewhere", 1))
		}, "none of it is in"},
		{"coastline missing a part", func(t *testing.T, c *config.Config) {
			testsupport.WriteText(t, c.DetectionGraphFilePath(), strings.Replace(testsupport.MaskingDetectionGraph, "bg_coast", "partial", 1))
			testsupport.WriteText(t, filepath.Join(c.GraphsDir, "partial.shp"), "part")
		}, "partial.shx, partial.dbf, partial.prj missing"},
		{"DIMAP graph, other output", func(_ *testing.T, c *config.Config) { c.ResultFileName = "result.tif" }, "must end in .dim"},
		{"other graph, DIMAP output", func(t *testing.T, c *config.Config) {
			testsupport.WriteText(t, c.GraphFilePath(), "<graph><node><operator>Calibration</operator></node></graph>")
		}, "should not end in .dim"},
		{"both stages calibrate", func(t *testing.T, c *config.Config) {
			testsupport.WriteText(t, c.DetectionGraphFilePath(), strings.Replace(testsupport.MaskingDetectionGraph,
				"<operator>Read</operator>", "<operator>Calibration</operator>", 1))
		}, "both calibrate"},
		{"output folder blocked by a file", func(t *testing.T, c *config.Config) {
			testsupport.WriteText(t, c.OutputDir, "not a folder")
		}, "cannot create"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := preflightProject(t)
			c.breakIt(t, &cfg)
			err := RunPreflightChecks(&cfg)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestPreflightIgnoresTheDetectionGraphWithNoDetect(t *testing.T) {
	cfg := preflightProject(t)
	cfg.SkipDetection = true
	cfg.DetectionGraphFileName = "absent.xml"
	err := RunPreflightChecks(&cfg)
	if err == nil || !strings.Contains(err.Error(), "results database") {
		t.Errorf("err = %v, want to get as far as the database", err)
	}
}

func TestDownloadingWithCredentialsSet(t *testing.T) {
	cfg := preflightProject(t)
	cfg.SceneFileName, cfg.SearchAreaName = "", "burgas"
	t.Setenv("CDSE_USERNAME", "u")
	t.Setenv("CDSE_PASSWORD", "p")
	if err := checkCredentialsBeforeDownloading(&cfg); err != nil {
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
			t.Setenv(testsupport.FakeDockerMemTotal, c.memTotal)
			cfg := config.NewDefaultConfig()
			cfg.SnapHeapGB = c.heap
			err := checkContainerMemoryFitsDocker(&cfg)
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
	t.Setenv(testsupport.FakeDockerDown, "1")
	err := snap.EnsureDockerIsRunning()
	if err == nil || !strings.Contains(err.Error(), "Cannot connect to the Docker daemon") {
		t.Errorf("err = %v, want docker's own message passed on", err)
	}
}

func TestPreflightNotes(t *testing.T) {
	cfg := preflightProject(t)
	cfg.SearchAreaName, cfg.CDSEProductID = "bulgaria", "id"
	testsupport.WriteText(t, cfg.GraphFilePath(), "<graph><formatName>BEAM-DIMAP</formatName></graph>")

	said := testsupport.CaptureStdout(t, func() {
		warnIfBothSceneSourcesGiven(&cfg)
		warnIfNothingCalibrates(&cfg)
		warnIfAuxDataCacheIsEmpty(&cfg)
	})
	for _, want := range []string{"ignoring -product", "neither preprocess_full.xml nor ShipDetection.xml calibrates",
		"will download orbit files"} {
		if !strings.Contains(said, want) {
			t.Errorf("notes lack %q:\n%s", want, said)
		}
	}

	testsupport.WriteText(t, filepath.Join(cfg.AuxDataDir, "Orbits", "x.EOF"), "orbit")
	cfg.CDSEProductID = ""
	cfg.SkipDetection = true
	if said := testsupport.CaptureStdout(t, func() {
		warnIfBothSceneSourcesGiven(&cfg)
		warnIfNothingCalibrates(&cfg)
		warnIfAuxDataCacheIsEmpty(&cfg)
	}); said != "" {
		t.Errorf("nothing to note, yet it said %q", said)
	}
}

func TestLandSeaMaskWithoutAMaskIsAllowed(t *testing.T) {
	cfg := preflightProject(t)
	testsupport.WriteText(t, cfg.DetectionGraphFilePath(), "<graph><node id=\"Read\"><operator>Read</operator></node></graph>")
	if err := checkLandSeaMaskHasAMaskSource(&cfg); err != nil {
		t.Errorf("a graph without Land-Sea-Mask was refused: %v", err)
	}
	testsupport.WriteText(t, cfg.DetectionGraphFilePath(), strings.Replace(testsupport.MaskingDetectionGraph, "<geometry>bg_coast</geometry>", "", 1))
	if err := checkLandSeaMaskHasAMaskSource(&cfg); err != nil {
		t.Errorf("a mask with useSRTM=false and no geometry was refused: %v", err)
	}
}
