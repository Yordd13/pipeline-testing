// TestReportSingleFileResultOpensTheFolderWhenAsked: checks a single-file result is reported and selected in Explorer when asked.
// TestCheckFileWasWritten: checks folders, empty, missing and incomplete outputs are each reported as not written.

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestReportSingleFileResultOpensTheFolderWhenAsked(t *testing.T) {
	calls := logFakeTools(t)
	config := newDefaultConfig()
	config.OpenResultFolderWhenDone = true
	path := filepath.Join(t.TempDir(), "result.tif")
	writeText(t, path, strings.Repeat("x", 2048))

	said := captureStdout(t, func() {
		if err := reportResult(&config, path); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(said, "Output: "+path) || !strings.Contains(said, "min/max") {
		t.Errorf("report = %q", said)
	}
	got := calls()
	if len(got) != 1 || got[0] != "explorer /select,"+path {
		t.Errorf("explorer was asked %q", got)
	}
}

func TestCheckFileWasWritten(t *testing.T) {
	dir := t.TempDir()
	writeText(t, filepath.Join(dir, "empty.dim"), "")
	writeText(t, filepath.Join(dir, "b.txt"), "b")
	writeText(t, filepath.Join(dir, "a.txt"), "a")

	cases := []struct {
		path string
		want string
	}{
		{dir, "is a folder"},
		{filepath.Join(dir, "empty.dim"), "is empty"},
		{filepath.Join(dir, "absent.dim"), "the folder contains: a.txt, b.txt, empty.dim"},
		{filepath.Join(dir, "nowhere", "absent.dim"), "nothing was written to"},
	}
	for _, c := range cases {
		err := checkFileWasWritten(c.path)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", filepath.Base(c.path), err, c.want)
		}
	}

	config := newDefaultConfig()
	if err := reportResult(&config, filepath.Join(dir, "absent.tif")); err == nil {
		t.Error("a missing single-file result was reported as written")
	}
	if err := reportResult(&config, filepath.Join(dir, "empty.dim")); err == nil {
		t.Error("an empty product was reported as written")
	}
	writeText(t, filepath.Join(dir, "nodata.dim"), "header")
	if err := reportResult(&config, filepath.Join(dir, "nodata.dim")); err == nil ||
		!strings.Contains(err.Error(), "product is incomplete") {
		t.Errorf("err = %v, want the missing .data folder reported", err)
	}
}
