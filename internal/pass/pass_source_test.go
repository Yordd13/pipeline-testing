// TestPlanFromAreaSearchDownloadsAndUnpacksEverySlice: checks an area search plan downloads and unpacks every slice.
// TestPlanFromProductIDFillsInFromTheName: checks a plan from a product id takes its metadata from the product name.
// TestPlanFromSearchStopsOnAFailedDownload: checks area and product plans fail on a refused download, as does an unknown area.
// TestPlanStopsOnAnArchiveThatWillNotUnpack: checks a downloaded archive that is not a zip fails the plan at unpacking.
// TestPlanFromASceneOnDisk: checks a local scene zip becomes a one-slice plan, is reported if missing and unpacked only once.
// TestPlanNeedsSomethingToDo: checks building a plan with no scene and no search reports there is nothing to do.
// TestNewSliceJobFromFileReadsTheName: checks a slice job takes mission, orbit and time from its product name, or leaves them empty.
// TestPassPlanAccounting: checks success counts, missing metadata and rounded coverage text of a pass plan.

package pass

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"radarpipeline/internal/config"
	"radarpipeline/internal/testsupport"
)

func TestPlanFromAreaSearchDownloadsAndUnpacksEverySlice(t *testing.T) {
	testsupport.ScriptCDSE(t)
	cfg := config.NewDefaultConfig()
	cfg.ScenesDir = t.TempDir()
	cfg.SearchAreaName = "bulgaria"

	var plan *PassPlan
	var err error
	output := testsupport.CaptureStdout(t, func() { plan, err = BuildPassPlan(&cfg) })
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Slices) != 2 || plan.AreaName != "bulgaria" {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.CoveragePercent < 99 {
		t.Errorf("coverage = %.1f, want the whole area", plan.CoveragePercent)
	}
	first := plan.Slices[0]
	if first.Index != 1 || first.SceneFileName != testsupport.NewestSliceSouth || first.ProductID != "id-south" ||
		first.ArchivePath != filepath.Join(cfg.ScenesDir, testsupport.NewestSliceSouth+".zip") {
		t.Errorf("first slice = %+v", first)
	}
	if _, err := os.Stat(filepath.Join(cfg.ScenesDir, testsupport.NewestSliceSouth, "manifest.safe")); err != nil {
		t.Errorf("the slice was not unpacked: %v", err)
	}
	if !strings.Contains(output, "archived rather than online") {
		t.Errorf("an offline slice was not pointed out:\n%s", output)
	}
}

func TestPlanFromProductIDFillsInFromTheName(t *testing.T) {
	testsupport.ScriptCDSE(t)
	cfg := config.NewDefaultConfig()
	cfg.ScenesDir = t.TempDir()
	cfg.CDSEProductID = "id-south"

	plan, err := BuildPassPlan(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	slice := plan.Slices[0]
	if slice.Mission != "S1D" || slice.AbsoluteOrbit != "004474" ||
		!slice.AcquiredAt.Equal(time.Date(2026, 9, 7, 15, 59, 20, 0, time.UTC)) {
		t.Errorf("slice = %+v", slice)
	}
	if plan.CoveragePercent != CoverageUnknown {
		t.Errorf("coverage = %v, want unknown", plan.CoveragePercent)
	}
}

func TestPlanFromSearchStopsOnAFailedDownload(t *testing.T) {
	fake := testsupport.ScriptCDSE(t)
	fake.Handle(testsupport.CDSEDownloadHost, func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "quota", http.StatusTooManyRequests)
	})
	cfg := config.NewDefaultConfig()
	cfg.ScenesDir = t.TempDir()

	cfg.SearchAreaName = "bulgaria"
	if _, err := BuildPassPlan(&cfg); err == nil {
		t.Error("an area plan survived a failed download")
	}
	cfg.SearchAreaName = ""
	cfg.CDSEProductID = "id-south"
	if _, err := BuildPassPlan(&cfg); err == nil {
		t.Error("a product plan survived a failed download")
	}
	cfg.CDSEProductID = ""
	cfg.SearchAreaName = "atlantis"
	if _, err := BuildPassPlan(&cfg); err == nil {
		t.Error("an unknown area was planned")
	}
}

func TestPlanStopsOnAnArchiveThatWillNotUnpack(t *testing.T) {
	fake := testsupport.ScriptCDSE(t)
	fake.Handle(testsupport.CDSEDownloadHost, func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, "this is not a zip")
	})
	cfg := config.NewDefaultConfig()
	cfg.ScenesDir = t.TempDir()
	cfg.CDSEProductID = "id-south"
	if _, err := BuildPassPlan(&cfg); err == nil || !strings.Contains(err.Error(), "unpacking") {
		t.Errorf("err = %v, want the unpacking failure", err)
	}
}

func TestPlanFromASceneOnDisk(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.ScenesDir = t.TempDir()
	cfg.SceneFileName = testsupport.NewestSliceSouth + ".zip"

	if _, err := BuildPassPlan(&cfg); err == nil || !strings.Contains(err.Error(), "scene not found") {
		t.Errorf("err = %v, want the missing scene reported", err)
	}

	if err := os.WriteFile(cfg.SceneFilePath(), testsupport.ProductZip(t, testsupport.NewestSliceSouth), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPassPlan(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	slice := plan.Slices[0]
	if plan.CoveragePercent != CoverageUnknown || slice.SceneFileName != testsupport.NewestSliceSouth ||
		slice.Mission != "S1D" || slice.AbsoluteOrbit != "004474" ||
		!slice.AcquiredAt.Equal(time.Date(2026, 9, 7, 15, 59, 20, 0, time.UTC)) {
		t.Errorf("plan = %+v, slice = %+v", plan, slice)
	}

	said := testsupport.CaptureStdout(t, func() { BuildPassPlan(&cfg) })
	if !strings.Contains(said, "already there") {
		t.Errorf("output = %q", said)
	}
}

func TestPlanNeedsSomethingToDo(t *testing.T) {
	cfg := config.NewDefaultConfig()
	if _, err := BuildPassPlan(&cfg); err == nil || !strings.Contains(err.Error(), "nothing to do") {
		t.Errorf("err = %v", err)
	}
}

func TestNewSliceJobFromFileReadsTheName(t *testing.T) {
	slice := newSliceJobFromFile(2, testsupport.NewestSliceNorth)
	if slice.Index != 2 || slice.SliceName() != "slice_2" || slice.Mission != "S1D" || slice.AbsoluteOrbit != "004474" {
		t.Errorf("slice = %+v", slice)
	}
	unknown := newSliceJobFromFile(1, "mystery")
	if !unknown.AcquiredAt.IsZero() || unknown.AbsoluteOrbit != "" {
		t.Errorf("a name that says nothing produced %+v", unknown)
	}
	if !AcquisitionTimeFromProductName("S1C_IW_GRDH_1SDV_notatime_x").IsZero() {
		t.Error("an unreadable time was parsed")
	}
}

func TestPassPlanAccounting(t *testing.T) {
	plan := &PassPlan{}
	if plan.AllSucceeded() {
		t.Error("an empty plan succeeded")
	}
	plan.Slices = []*SliceJob{
		{Index: 1, DetectionProductPath: "a.dim"},
		{Index: 2},
	}
	if plan.AllSucceeded() || len(plan.SucceededSlices()) != 1 || len(plan.FailedSlices()) != 1 {
		t.Error("a slice without a product counted as done")
	}
	if plan.AbsoluteOrbit() != "" || !plan.PassStart().IsZero() {
		t.Error("a plan without metadata invented some")
	}
	plan.CoveragePercent = 99.6
	if plan.CoverageText() != "100" {
		t.Errorf("coverage text = %q", plan.CoverageText())
	}
}
