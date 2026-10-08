// TestDropSeamDuplicates: checks a vessel detected 40 m apart by two slices is kept once while a 1.4 km neighbour stays.
// TestDropSeamDuplicatesKeepsDistinctVessels: checks two detections 200 m apart are kept as separate vessels.
// TestMetresBetween: checks a tenth of a degree of latitude measures about 11.1 km.
// TestParseSNAPDetectionFile: checks a tab-separated SNAP detection list is parsed into ids, positions and measurements.
// TestParseSNAPDetectionFileToleratesReorderedColumns: checks detection columns are mapped by name, not by position.
// TestDetectionsFileStem: checks the detections file is named after the earliest slice time and the orbit.
// TestDetectionsFileStemWithoutMetadata: checks a plan without time or orbit gets an unknown-time_unknown-orbit stem.
// TestAcquisitionTimeFromProductName: checks the start time is parsed from Sentinel-1 product names and empty otherwise.
// TestMergeDetectionsStampsAndCountsAcrossSlices: checks merging filters to the AOI, prefixes ids, stamps provenance and counts.
// TestMergeDetectionsStopsOnAnUnreadableList: checks an unreadable detection list fails the merge and names the slice.

package detection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"radarpipeline/internal/config"
	"radarpipeline/internal/pass"
	"radarpipeline/internal/snap"
	"radarpipeline/internal/testsupport"
)

func TestDropSeamDuplicates(t *testing.T) {
	detections := []Detection{
		{ID: "target_000", Latitude: 43.20000, Longitude: 27.92000, SourceSlice: "slice_1"},
		{ID: "target_001", Latitude: 43.20036, Longitude: 27.92000, SourceSlice: "slice_2"},
		{ID: "target_002", Latitude: 43.21260, Longitude: 27.92000, SourceSlice: "slice_2"},
	}

	kept, duplicates := dropSeamDuplicates(detections)
	if duplicates != 1 {
		t.Errorf("duplicates = %d, want 1", duplicates)
	}
	if len(kept) != 2 {
		t.Fatalf("kept %d, want 2", len(kept))
	}
	if kept[0].ID != "target_000" || kept[1].ID != "target_002" {
		t.Errorf("kept the wrong pair: %s and %s", kept[0].ID, kept[1].ID)
	}
}

func TestDropSeamDuplicatesKeepsDistinctVessels(t *testing.T) {
	detections := []Detection{
		{ID: "a", Latitude: 42.50000, Longitude: 27.50000},
		{ID: "b", Latitude: 42.50180, Longitude: 27.50000},
	}
	kept, duplicates := dropSeamDuplicates(detections)
	if duplicates != 0 || len(kept) != 2 {
		t.Errorf("200 m apart collapsed: kept %d, dropped %d", len(kept), duplicates)
	}
}

func TestMetresBetween(t *testing.T) {
	got := metresBetween(43.0, 27.9, 43.1, 27.9)
	if got < 11000 || got > 11200 {
		t.Errorf("0.1 degree of latitude = %.0f m, want about 11100", got)
	}
}

func TestParseSNAPDetectionFile(t *testing.T) {
	content := strings.Join([]string{
		"#defaultCSS=fill:#ff0000; symbol:cross",
		strings.Join([]string{"ShipDetections", "geometry:Point", "Detected_x:Integer",
			"Detected_y:Integer", "Detected_lat:Double", "Detected_lon:Double",
			"Detected_width:Double", "Detected_length:Double", "style_css:String"}, "\t"),
		strings.Join([]string{"target_000", "POINT (5958 158)", "5958", "158",
			"43.4895", "29.2545", "120.0", "230.0", "fill:#ff0000"}, "\t"),
	}, "\n")

	path := filepath.Join(t.TempDir(), "ShipDetections.csv")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	detections, err := parseSNAPDetectionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(detections) != 1 {
		t.Fatalf("parsed %d rows, want 1", len(detections))
	}

	got := detections[0]
	if got.ID != "target_000" || got.Latitude != 43.4895 || got.Longitude != 29.2545 {
		t.Errorf("wrong values: %+v", got)
	}
	if got.WidthM != "120.0" || got.LengthM != "230.0" || got.PixelX != "5958" {
		t.Errorf("wrong measurements: %+v", got)
	}
}

func TestParseSNAPDetectionFileToleratesReorderedColumns(t *testing.T) {
	content := strings.Join([]string{
		"#defaultCSS=whatever",
		strings.Join([]string{"ShipDetections", "Detected_lon:Double",
			"Detected_lat:Double", "Detected_length:Double"}, "\t"),
		strings.Join([]string{"target_007", "29.2545", "43.4895", "230.0"}, "\t"),
	}, "\n")

	path := filepath.Join(t.TempDir(), "ShipDetections.csv")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	detections, err := parseSNAPDetectionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(detections) != 1 {
		t.Fatalf("parsed %d rows, want 1", len(detections))
	}
	if detections[0].Latitude != 43.4895 || detections[0].Longitude != 29.2545 {
		t.Errorf("columns were read positionally: %+v", detections[0])
	}
}

func TestDetectionsFileStem(t *testing.T) {
	plan := &pass.PassPlan{Slices: []*pass.SliceJob{
		{AcquiredAt: time.Date(2026, 9, 7, 15, 59, 45, 0, time.UTC), AbsoluteOrbit: "004474"},
		{AcquiredAt: time.Date(2026, 9, 7, 15, 59, 20, 0, time.UTC), AbsoluteOrbit: "004474"},
	}}

	if got, want := detectionsFileStem(plan), "2026-09-07T155920Z_004474"; got != want {
		t.Errorf("stem = %q, want %q", got, want)
	}
}

func TestDetectionsFileStemWithoutMetadata(t *testing.T) {
	plan := &pass.PassPlan{Slices: []*pass.SliceJob{{}}}
	if got := detectionsFileStem(plan); got != "unknown-time_unknown-orbit" {
		t.Errorf("stem = %q", got)
	}
}

func TestAcquisitionTimeFromProductName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"S1D_IW_GRDH_1SDV_20260907T155920_20260907T155945_004474_0084CD_ED7F.SAFE",
			"2026-09-07T15:59:20Z"},
		{"S1D_IW_GRDH_1SDV_20260907T155920_20260907T155945_004474_0084CD_ED7F.SAFE.zip",
			"2026-09-07T15:59:20Z"},
		{"not_a_product", ""},
	}

	for _, c := range cases {
		got := pass.AcquisitionTimeFromProductName(c.name)
		text := ""
		if !got.IsZero() {
			text = got.Format(time.RFC3339)
		}
		if text != c.want {
			t.Errorf("%q gave %q, want %q", c.name, text, c.want)
		}
	}
}

func TestMergeDetectionsStampsAndCountsAcrossSlices(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewDefaultConfig()
	cfg.GraphsDir = dir

	busy := testsupport.WriteShipsProduct(t, filepath.Join(dir, "slice_1"), "ships", []string{
		"target_001\t1\t1\t42.6\t28.0\t20.0\t60.0",
		"target_000\t2\t1\t42.5\t28.0\t20.0\t60.0",
		"target_002\t3\t1\t44.5\t28.0\t20.0\t60.0",
		"target_003\tx\tx\tnot-a-latitude\t28.0\t20.0\t60.0",
	})
	plan := &pass.PassPlan{AreaName: "bulgaria", CoveragePercent: 87.4, Slices: []*pass.SliceJob{
		{Index: 1, ProductID: "p1", ProductName: "S1C_A.SAFE.zip", Mission: "S1C", AbsoluteOrbit: "009234",
			AcquiredAt: time.Date(2026, 8, 31, 4, 13, 9, 0, time.UTC), DetectionProductPath: busy},
		{Index: 2, ProductName: "S1C_B.SAFE", AbsoluteOrbit: "009234",
			DetectionProductPath: filepath.Join(dir, "slice_2", "ships.dim")},
		{Index: 3, Failure: testsupport.ErrFake("never ran")},
	}}

	var outcome MergeOutcome
	var err error
	testsupport.CaptureStdout(t, func() { outcome, err = MergeDetections(&cfg, plan) })
	if err != nil {
		t.Fatal(err)
	}
	if outcome.RawCFAR != 3 || outcome.DroppedOutsideAOI != 1 || outcome.Written != 2 ||
		outcome.SlicesRead != 2 || outcome.SlicesEmpty != 1 || outcome.RunID != "2026-08-31T041309Z_009234" {
		t.Errorf("outcome = %+v", outcome)
	}
	if len(outcome.Kept) != 2 || outcome.Kept[0].ID != "slice_1_target_000" || outcome.Kept[1].ID != "slice_1_target_001" {
		t.Fatalf("kept = %+v, want both in id order", outcome.Kept)
	}
	got := outcome.Kept[0]
	if got.SourceSlice != "slice_1" || got.ProductID != "p1" || got.ProductName != "S1C_A" ||
		got.AcquisitionTime != "2026-08-31T04:13:09Z" || got.CoveragePercent != "87" || got.ShoreMeasured {
		t.Errorf("stamped detection = %+v", got)
	}
}

func TestMergeDetectionsStopsOnAnUnreadableList(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ships.data", snap.VectorDataDirName, "ShipDetections.csv"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.NewDefaultConfig()
	plan := &pass.PassPlan{Slices: []*pass.SliceJob{{Index: 1, DetectionProductPath: filepath.Join(dir, "ships.dim")}}}
	if _, err := MergeDetections(&cfg, plan); err == nil || !strings.Contains(err.Error(), "of slice_1") {
		t.Errorf("err = %v, want the unreadable list named", err)
	}
	if _, err := parseSNAPDetectionFile(filepath.Join(dir, "absent.csv")); err == nil {
		t.Error("a missing list was read")
	}
}
