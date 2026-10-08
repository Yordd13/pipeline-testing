// TestPlanFromASceneOnDisk: checks a local scene zip becomes a one-slice plan, is reported if missing and unpacked only once.
// TestPlanNeedsSomethingToDo: checks building a plan with no scene and no search reports there is nothing to do.
// TestUnpackingRefusesEntriesOutsideTheProduct: checks zip entries escaping the product folder abort unpacking and leave nothing.
// TestAFolderNeedsNoUnpacking: checks an unpacked .SAFE folder is used as is and a .zip name gets no second extension.
// TestNewSliceJobFromFileReadsTheName: checks a slice job takes mission, orbit and time from its product name, or leaves them empty.
// TestPassPlanAccounting: checks success counts, missing metadata and rounded coverage text of a pass plan.
// TestLoadEnvFile: checks a missing .env is fine, a valid one sets variables and a malformed one is an error.

package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlanFromASceneOnDisk(t *testing.T) {
	config := newDefaultConfig()
	config.ScenesDir = t.TempDir()
	config.SceneFileName = newestSliceSouth + ".zip"

	if _, err := buildPassPlan(&config); err == nil || !strings.Contains(err.Error(), "scene not found") {
		t.Errorf("err = %v, want the missing scene reported", err)
	}

	if err := os.WriteFile(config.SceneFilePath(), productZip(t, newestSliceSouth), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := buildPassPlan(&config)
	if err != nil {
		t.Fatal(err)
	}
	slice := plan.Slices[0]
	if plan.CoveragePercent != coverageUnknown || slice.SceneFileName != newestSliceSouth ||
		slice.Mission != "S1D" || slice.AbsoluteOrbit != "004474" ||
		!slice.AcquiredAt.Equal(time.Date(2026, 9, 7, 15, 59, 20, 0, time.UTC)) {
		t.Errorf("plan = %+v, slice = %+v", plan, slice)
	}

	said := captureStdout(t, func() { buildPassPlan(&config) })
	if !strings.Contains(said, "already there") {
		t.Errorf("output = %q", said)
	}
}

func TestPlanNeedsSomethingToDo(t *testing.T) {
	config := newDefaultConfig()
	if _, err := buildPassPlan(&config); err == nil || !strings.Contains(err.Error(), "nothing to do") {
		t.Errorf("err = %v", err)
	}
}

func TestUnpackingRefusesEntriesOutsideTheProduct(t *testing.T) {
	cases := map[string]string{
		"climbs out":        "../evil.txt",
		"absolute":          "/etc/evil.txt",
		"beside the folder": "other.SAFE/evil.txt",
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			scenes := t.TempDir()
			archive := filepath.Join(scenes, "product.SAFE.zip")
			file, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			writer := zip.NewWriter(file)
			w, _ := writer.Create("product.SAFE/manifest.safe")
			w.Write([]byte("m"))
			w, _ = writer.Create(entry)
			w.Write([]byte("evil"))
			writer.Close()
			file.Close()

			config := newDefaultConfig()
			config.ScenesDir = scenes
			if _, err := ensureSceneIsUnpacked(&config, "product.SAFE.zip"); err == nil {
				t.Fatalf("an archive with %q was unpacked", entry)
			}
			if _, err := os.Stat(filepath.Join(scenes, "product.SAFE")); !os.IsNotExist(err) {
				t.Error("a half-unpacked folder was left behind")
			}
		})
	}
}

func TestAFolderNeedsNoUnpacking(t *testing.T) {
	config := newDefaultConfig()
	if name, err := ensureSceneIsUnpacked(&config, "product.SAFE"); err != nil || name != "product.SAFE" {
		t.Errorf("name = %q, %v", name, err)
	}
	if got := ensureZipExtension("x.zip"); got != "x.zip" {
		t.Errorf("ensureZipExtension added a second .zip: %q", got)
	}
}

func TestNewSliceJobFromFileReadsTheName(t *testing.T) {
	slice := newSliceJobFromFile(2, newestSliceNorth)
	if slice.Index != 2 || slice.SliceName() != "slice_2" || slice.Mission != "S1D" || slice.AbsoluteOrbit != "004474" {
		t.Errorf("slice = %+v", slice)
	}
	unknown := newSliceJobFromFile(1, "mystery")
	if !unknown.AcquiredAt.IsZero() || unknown.AbsoluteOrbit != "" {
		t.Errorf("a name that says nothing produced %+v", unknown)
	}
	if !acquisitionTimeFromProductName("S1C_IW_GRDH_1SDV_notatime_x").IsZero() {
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

func TestLoadEnvFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := loadEnvFile(); err != nil {
		t.Errorf("no .env is fine, got %v", err)
	}

	writeText(t, envFileName, "PIPELINE_TEST_ONLY_VALUE=42\n")
	t.Cleanup(func() { os.Unsetenv("PIPELINE_TEST_ONLY_VALUE") })
	if err := loadEnvFile(); err != nil || os.Getenv("PIPELINE_TEST_ONLY_VALUE") != "42" {
		t.Errorf("err = %v, value = %q", err, os.Getenv("PIPELINE_TEST_ONLY_VALUE"))
	}

	writeText(t, envFileName, "BROKEN='never closed\n")
	if err := loadEnvFile(); err == nil || !strings.Contains(err.Error(), "could not read .env") {
		t.Errorf("err = %v, want a broken .env reported", err)
	}
}
