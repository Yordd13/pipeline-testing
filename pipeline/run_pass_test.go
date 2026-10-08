// newRunFixture: lays out a temporary project with graphs, coastline, an unpacked scene and fake SNAP products.
// writeSeaProduct: writes a preprocessed product of dark sea with one bright target over the fixture swath.
// writeShipsProduct: writes a detection product with a mask in its header, one band and the given detection rows.
// runFixture.toolCalls: returns the logged fake tool calls that contain the given text.
// TestProcessPassRunsEveryStepAndSaysWhenTheDatabaseIsMissing: checks a full pass runs, reports, tiles and cleans up without MySQL.
// TestProcessPassKeepsTheMaskWhereThereIsCoast: checks a swath holding coast runs the masked graph and measures shore distance.
// TestProcessPassFailsWhenEverySliceFails: checks each kind of slice failure is recorded and fails the pass.
// TestProcessPassRejectsADetectionProductWithoutBands: checks a detection product with no image bands fails its slice.
// TestProcessPassNeedsARunFolder: checks the pass fails when the run folder cannot be created.
// TestProcessPassStopsWhenTheMergeFails: checks an unreadable detection list stops the pass before any cleanup.
// TestProcessPassKeepsEverythingWhenASliceFails: checks one failed slice leaves the run folder and scenes in place.
// TestPrintPassSummaryTellsAQuietSeaFromAFilteredOne: checks the summary separates all-filtered targets from an empty sea.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	runSceneName = "S1C_IW_GRDH_1SDV_20260831T041309_20260831T041334_009234_01258F_67D4.SAFE"
	runStem      = "S1C_20260831T041309"
)

const maskingDetectionGraph = `<graph id="Graph">
  <version>1.0</version>
  <node id="Read"><operator>Read</operator><sources/></node>
  <node id="Import-Vector"><operator>Import-Vector</operator>
    <sources><sourceProduct refid="Read"/></sources></node>
  <node id="Land-Sea-Mask"><operator>Land-Sea-Mask</operator>
    <sources><sourceProduct refid="Import-Vector"/></sources>
    <parameters class="x"><useSRTM>false</useSRTM><geometry>bg_coast</geometry></parameters></node>
  <node id="AdaptiveThresholding"><operator>AdaptiveThresholding</operator>
    <sources><sourceProduct refid="Land-Sea-Mask"/></sources></node>
  <node id="Write"><operator>Write</operator>
    <sources><sourceProduct refid="AdaptiveThresholding"/></sources>
    <parameters class="x"><formatName>BEAM-DIMAP</formatName></parameters></node>
</graph>`

const preprocessGraph = `<graph id="Graph"><node id="Calibration"><operator>Calibration</operator></node>
<node id="Write"><operator>Write</operator><parameters><formatName>BEAM-DIMAP</formatName></parameters></node></graph>`

const (
	swathCols, swathRows        = 40, 2
	swathLon, swathLat, swathPx = 28.0, 43.0, 0.005
)

var (
	coastFarAway    = squareRing(27.5, 42.0, 27.6, 42.1)
	coastInTheSwath = squareRing(28.05, 42.9, 28.1, 43.1)
)

type runFixture struct {
	root   string
	config Config
	plan   *PassPlan
	tools  func() []string
}

func newRunFixture(t *testing.T, coast [][2]float64) *runFixture {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)

	config := newDefaultConfig()
	config.GraphsDir = filepath.Join(root, "graphs")
	config.ScenesDir = filepath.Join(root, "scenes")
	config.OutputDir = filepath.Join(root, "out")
	config.AuxDataDir = filepath.Join(root, "auxdata")
	config.TilesDir = filepath.Join(root, "tiles")
	config.MySQLEnvFile = filepath.Join(root, "mysql", "absent.env")

	writeText(t, config.GraphFilePath(), preprocessGraph)
	writeText(t, config.DetectionGraphFilePath(), maskingDetectionGraph)
	writeTestShapefile(t, filepath.Join(config.GraphsDir, "bg_coast.shp"), [][][2]float64{coast})

	writeText(t, filepath.Join(config.ScenesDir, runSceneName, "manifest.safe"), "<manifest/>")
	writeText(t, filepath.Join(config.ScenesDir, runSceneName+".zip"), "archive")

	products := filepath.Join(root, "products")
	writeSeaProduct(t, products, runStem+"_preprocessed")
	writeShipsProduct(t, products, runStem+"_ships", []string{
		"target_000\t10\t1\t42.995\t28.1\t20.0\t60.0",
		"target_001\t11\t1\t42.9952\t28.1\t20.0\t60.0",
		"target_002\t30\t1\t41.0\t29.0\t20.0\t60.0",
	})
	t.Setenv(fakeSnapProducts, products)

	plan := &PassPlan{
		AreaName:        "bulgaria",
		CoveragePercent: 64,
		Slices: []*SliceJob{{
			Index: 1, SceneFileName: runSceneName, ProductID: "id-1",
			ProductName: runSceneName, Mission: "S1C", AbsoluteOrbit: "009234",
			AcquiredAt: time.Date(2026, 8, 31, 4, 13, 9, 0, time.UTC),
		}},
	}
	return &runFixture{root: root, config: config, plan: plan, tools: logFakeTools(t)}
}

func writeSeaProduct(t *testing.T, dir, name string) string {
	t.Helper()
	samples := make([]float32, swathCols*swathRows)
	for at := range samples {
		samples[at] = 5e-4
	}
	samples[20] = 3.0
	return writeNamedTestProduct(t, dir, name, swathCols, swathRows, swathLon, swathLat, swathPx, samples)
}

func writeShipsProduct(t *testing.T, dir, name string, rows []string) string {
	t.Helper()
	dimPath := filepath.Join(dir, name+".dim")
	writeText(t, dimPath, "<Dimap_Document>\n<Masks><Mask>ShipDetections</Mask></Masks>\n</Dimap_Document>\n")
	writeText(t, filepath.Join(dir, name+".data", "ship_bit_msk.img"), "band bytes")
	header := "ShipDetections\tDetected_x:Integer\tDetected_y:Integer\tDetected_lat:Double\t" +
		"Detected_lon:Double\tDetected_width:Double\tDetected_length:Double"
	writeText(t, filepath.Join(dir, name+".data", vectorDataDirName, "ShipDetections.csv"),
		"#defaultCSS=fill:#ff0000\n"+header+"\n"+strings.Join(rows, "\n")+"\n")
	writeText(t, filepath.Join(dir, name+".data", vectorDataDirName, "pins.csv"), "placeholder\n")
	return dimPath
}

func (f *runFixture) toolCalls(containing string) []string {
	found := []string{}
	for _, call := range f.tools() {
		if strings.Contains(call, containing) {
			found = append(found, call)
		}
	}
	return found
}

func TestProcessPassRunsEveryStepAndSaysWhenTheDatabaseIsMissing(t *testing.T) {
	fixture := newRunFixture(t, coastFarAway)

	var err error
	output := captureStdout(t, func() { err = processPass(&fixture.config, fixture.plan) })
	if err == nil || !strings.Contains(err.Error(), "not stored in MySQL") {
		t.Fatalf("err = %v, want the run reported as not stored\n%s", err, output)
	}

	for _, want := range []string{
		"Processed 1 of 1 slice(s)",
		"CFAR found:     3 across 1 slice(s)",
		"outside the area: 1 dropped",
		"on the seam:      1 dropped",
		"Detections:     1 in Bulgarian waters",
		"Area covered:   64% of bulgaria waters — PARTIAL",
		"Pass:           orbit 009234, acquired 2026-08-31T04:13:09Z",
		"Run:            2026-08-31T041309Z_009234",
		"No coast in slice_1",
		"Tiles:",
		"Bands:  ship_bit_msk.img",
		"this run's results could not be stored",
		"Freed",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("the run did not say %q:\n%s", want, output)
		}
	}

	snapRuns := fixture.toolCalls(snapImageDigest)
	if len(snapRuns) != 2 {
		t.Fatalf("SNAP ran %d times, want both stages:\n%v", len(snapRuns), fixture.tools())
	}
	if !strings.Contains(snapRuns[0], "-Pinput=/scenes/"+runSceneName) ||
		!strings.Contains(snapRuns[0], "-Poutput=/out/"+runStem+"_preprocessed.dim") {
		t.Errorf("stage 1 was run as %s", snapRuns[0])
	}
	if !strings.Contains(snapRuns[1], "/out/"+maskFreeGraphFileName) ||
		!strings.Contains(snapRuns[1], "-Pinput=/out/"+runStem+"_preprocessed.dim") {
		t.Errorf("stage 2 was run as %s", snapRuns[1])
	}

	tiles := filepath.Join(fixture.config.TilesDir, "2026-08-31T041309Z_009234", "12", "2368", "1509.webp")
	if _, err := os.Stat(tiles); err != nil {
		t.Errorf("no tiles were left behind: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fixture.config.TilesDir, tileWorkDirName, "2026-08-31T041309Z_009234")); !os.IsNotExist(err) {
		t.Error("the tile work folder was left behind")
	}

	if _, err := os.Stat(fixture.plan.RunDir); !os.IsNotExist(err) {
		t.Errorf("the run folder %s survived the cleanup", fixture.plan.RunDir)
	}
	for _, scene := range []string{runSceneName, runSceneName + ".zip"} {
		if _, err := os.Stat(filepath.Join(fixture.config.ScenesDir, scene)); !os.IsNotExist(err) {
			t.Errorf("%s survived the cleanup", scene)
		}
	}
}

func TestProcessPassKeepsTheMaskWhereThereIsCoast(t *testing.T) {
	fixture := newRunFixture(t, coastInTheSwath)
	fixture.config.KeepIntermediate = true
	fixture.config.KeepVectorData = true

	output := captureStdout(t, func() { processPass(&fixture.config, fixture.plan) })

	snapRuns := fixture.toolCalls(snapImageDigest)
	if len(snapRuns) != 2 || !strings.Contains(snapRuns[1], "/graphs/ShipDetection.xml") {
		t.Fatalf("stage 2 did not run the configured graph: %v", snapRuns)
	}
	if !strings.Contains(output, "-keep-intermediate was given") {
		t.Errorf("the kept products were not explained:\n%s", output)
	}
	if !strings.Contains(output, "Shoreline:      1 detections measured") {
		t.Errorf("the distance to shore was not measured:\n%s", output)
	}
	ships := filepath.Join(fixture.plan.Slices[0].RunSubDir, runStem+"_ships.data", vectorDataDirName, "ShipDetections.csv")
	if _, err := os.Stat(ships); err != nil {
		t.Errorf("the detection geometry was removed despite -keep-vector-data: %v", err)
	}
}

func TestProcessPassFailsWhenEverySliceFails(t *testing.T) {
	cases := []struct {
		name      string
		setUp     func(*testing.T, *runFixture)
		wantSlice string
	}{
		{"SNAP runs out of memory", func(t *testing.T, f *runFixture) {
			t.Setenv(fakeSnapExitCode, "137")
		}, "SNAP ran out of memory"},
		{"SNAP fails", func(t *testing.T, f *runFixture) {
			t.Setenv(fakeSnapExitCode, "1")
		}, "SNAP failed with exit code 1"},
		{"SNAP writes nothing", func(t *testing.T, f *runFixture) {
			t.Setenv(fakeSnapProducts, filepath.Join(f.root, "graphs"))
		}, "no " + runStem + "_preprocessed.dim was written"},
		{"-no-detect", func(t *testing.T, f *runFixture) {
			f.config.SkipDetection = true
		}, "-no-detect was given"},
		{"the detection graph cannot be derived", func(t *testing.T, f *runFixture) {
			writeText(t, f.config.DetectionGraphFilePath(),
				strings.Replace(maskingDetectionGraph, `<sourceProduct refid="Read"/></sources></node>`,
					`<sourceProduct refid="Read"/><sourceProduct refid="Read"/></sources></node>`, 1))
		}, "sources"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fixture := newRunFixture(t, coastFarAway)
			c.setUp(t, fixture)

			var err error
			output := captureStdout(t, func() { err = processPass(&fixture.config, fixture.plan) })
			if err == nil || !strings.Contains(err.Error(), "every slice failed") {
				t.Errorf("err = %v, want every slice failed", err)
			}
			if failure := fixture.plan.Slices[0].Failure; failure == nil || !strings.Contains(failure.Error(), c.wantSlice) {
				t.Errorf("slice failure = %v, want %q\n%s", failure, c.wantSlice, output)
			}
		})
	}
}

func TestProcessPassRejectsADetectionProductWithoutBands(t *testing.T) {
	fixture := newRunFixture(t, coastFarAway)
	products := os.Getenv(fakeSnapProducts)
	os.Remove(filepath.Join(products, runStem+"_ships.data", "ship_bit_msk.img"))

	var err error
	captureStdout(t, func() { err = processPass(&fixture.config, fixture.plan) })
	if err == nil {
		t.Fatal("a product without bands passed")
	}
	if failure := fixture.plan.Slices[0].Failure; failure == nil || !strings.Contains(failure.Error(), "no image bands") {
		t.Errorf("slice failure = %v", failure)
	}
}

func TestProcessPassNeedsARunFolder(t *testing.T) {
	fixture := newRunFixture(t, coastFarAway)
	writeText(t, fixture.config.OutputDir, "a file where the folder should be")
	if err := processPass(&fixture.config, fixture.plan); err == nil ||
		!strings.Contains(err.Error(), "cannot create the run folder") {
		t.Errorf("err = %v", err)
	}
}

func TestProcessPassStopsWhenTheMergeFails(t *testing.T) {
	fixture := newRunFixture(t, coastFarAway)
	list := filepath.Join(os.Getenv(fakeSnapProducts), runStem+"_ships.data", vectorDataDirName, "ShipDetections.csv")
	os.Remove(list)
	if err := os.MkdirAll(list, 0o755); err != nil {
		t.Fatal(err)
	}

	var err error
	output := captureStdout(t, func() { err = processPass(&fixture.config, fixture.plan) })
	if err == nil || !strings.Contains(err.Error(), "ShipDetections.csv of slice_1") {
		t.Errorf("err = %v, want the unreadable list reported", err)
	}
	if strings.Contains(output, "Freed") {
		t.Error("a run whose merge failed was cleaned up")
	}
	if _, err := os.Stat(filepath.Join(fixture.config.ScenesDir, runSceneName)); err != nil {
		t.Errorf("the scene was removed: %v", err)
	}
}

func TestProcessPassKeepsEverythingWhenASliceFails(t *testing.T) {
	fixture := newRunFixture(t, coastFarAway)
	fixture.plan.Slices = append(fixture.plan.Slices, &SliceJob{
		Index: 2, SceneFileName: "S1C_IW_GRDH_1SDV_20260831T041334_20260831T041359_009234_01258F_AAAA.SAFE",
		AbsoluteOrbit: "009234",
	})

	output := captureStdout(t, func() { processPass(&fixture.config, fixture.plan) })
	if !strings.Contains(output, "slice_2 failed") || !strings.Contains(output, "1 of 2 slices failed") {
		t.Errorf("the partial run was not reported:\n%s", output)
	}
	if _, err := os.Stat(fixture.plan.RunDir); err != nil {
		t.Errorf("the run folder was removed after a failed slice: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fixture.config.ScenesDir, runSceneName)); err != nil {
		t.Errorf("a scene was removed after a failed slice: %v", err)
	}
}

func TestPrintPassSummaryTellsAQuietSeaFromAFilteredOne(t *testing.T) {
	plan := &PassPlan{AreaName: "burgas", CoveragePercent: coverageUnknown}
	filtered := captureStdout(t, func() {
		printPassSummary(plan, mergeOutcome{RawCFAR: 5, DroppedOutsideAOI: 5, SlicesRead: 2, SlicesEmpty: 1})
	})
	for _, want := range []string{"CFAR did find 5 target(s), all outside the area", "1 of 2 slice(s) held none",
		"Area covered:   unknown"} {
		if !strings.Contains(filtered, want) {
			t.Errorf("summary lacks %q:\n%s", want, filtered)
		}
	}

	plan.CoveragePercent = 100
	quiet := captureStdout(t, func() { printPassSummary(plan, mergeOutcome{SlicesRead: 1}) })
	if strings.Contains(quiet, "CFAR did find") || !strings.Contains(quiet, "100% of burgas waters\n") {
		t.Errorf("a quiet, complete pass read wrongly:\n%s", quiet)
	}
	if strings.Contains(quiet, "Pass:") {
		t.Error("a pass with no orbit printed one")
	}
}
