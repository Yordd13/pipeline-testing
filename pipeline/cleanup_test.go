// cleanupFixture: builds a finished one-slice run with its scene folder and archive in a fresh working folder.
// TestTidyRemovesTheRunAndItsScenes: checks a successful run's folder and scenes are deleted and the freed space reported.
// TestTidyUsesTheArchiveTheRunKnows: checks cleanup deletes the archive path the slice recorded rather than guessing one.
// TestTidyLeavesThingsAlone: checks cleanup is skipped for -keep-intermediate, failed slices, foreign or unreadable run folders.
// TestPrintCleanupOutcomeListsTheStrangers: checks the cleanup summary lists unexpected entries found in the run folder.
// TestFormatBytes: checks byte counts are formatted as MB or GB with one decimal.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cleanupFixture(t *testing.T) (*Config, *PassPlan) {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)

	config := newDefaultConfig()
	plan := &PassPlan{RunDir: filepath.Join(root, "out", "20260831T050000Z")}
	plan.Slices = []*SliceJob{{
		Index: 1, SceneFileName: "S1C_x.SAFE",
		DetectionProductPath: filepath.Join(plan.RunDir, "slice_1", "ships.dim"),
	}}
	writeText(t, plan.Slices[0].DetectionProductPath, strings.Repeat("x", 1024))
	writeText(t, filepath.Join("scenes", "S1C_x.SAFE", "manifest.safe"), "m")
	writeText(t, filepath.Join("scenes", "S1C_x.SAFE.zip"), "zip")
	return &config, plan
}

func TestTidyRemovesTheRunAndItsScenes(t *testing.T) {
	config, plan := cleanupFixture(t)
	outcome := tidyAfterSuccess(config, plan)

	if outcome.Skipped != "" || !outcome.RunDirRemoved || outcome.BytesFreed != 1024+1+3 {
		t.Errorf("outcome = %+v", outcome)
	}
	if strings.Join(outcome.ArchivesFreed, ",") != "S1C_x.SAFE,S1C_x.SAFE.zip" {
		t.Errorf("archives freed = %v", outcome.ArchivesFreed)
	}
	for _, path := range []string{plan.RunDir, filepath.Join("scenes", "S1C_x.SAFE"), filepath.Join("scenes", "S1C_x.SAFE.zip")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s survived", path)
		}
	}

	said := captureStdout(t, func() { printCleanupOutcome(outcome) })
	for _, want := range []string{"Freed 0.0 MB", "the run folder", "scenes: S1C_x.SAFE, S1C_x.SAFE.zip", "fresh download"} {
		if !strings.Contains(said, want) {
			t.Errorf("summary lacks %q:\n%s", want, said)
		}
	}
}

func TestTidyUsesTheArchiveTheRunKnows(t *testing.T) {
	config, plan := cleanupFixture(t)
	archive := filepath.Join(t.TempDir(), "elsewhere.zip")
	writeText(t, archive, "zip")
	plan.Slices[0].ArchivePath = archive

	outcome := tidyAfterSuccess(config, plan)
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Error("the recorded archive survived")
	}
	if _, err := os.Stat(filepath.Join("scenes", "S1C_x.SAFE.zip")); err != nil {
		t.Errorf("an archive the run did not record was removed: %v", err)
	}
	if len(outcome.ArchivesFreed) != 2 {
		t.Errorf("archives freed = %v", outcome.ArchivesFreed)
	}
}

func TestTidyLeavesThingsAlone(t *testing.T) {
	cases := []struct {
		name  string
		setUp func(t *testing.T, config *Config, plan *PassPlan)
		want  string
	}{
		{"-keep-intermediate", func(_ *testing.T, c *Config, _ *PassPlan) {
			c.KeepIntermediate = true
		}, "-keep-intermediate was given"},
		{"a slice failed", func(_ *testing.T, _ *Config, p *PassPlan) {
			p.Slices = append(p.Slices, &SliceJob{Index: 2, Failure: errFake("no")})
		}, "1 of 2 slices failed"},
		{"something unexpected in the run folder", func(t *testing.T, _ *Config, p *PassPlan) {
			writeText(t, filepath.Join(p.RunDir, "notes.txt"), "mine")
		}, "entries this run did not create"},
		{"the run folder cannot be read", func(_ *testing.T, _ *Config, p *PassPlan) {
			p.RunDir = filepath.Join(p.RunDir, "absent")
		}, "could not inspect"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			config, plan := cleanupFixture(t)
			c.setUp(t, config, plan)
			outcome := tidyAfterSuccess(config, plan)
			if !strings.Contains(outcome.Skipped, c.want) || outcome.RunDirRemoved || outcome.BytesFreed != 0 {
				t.Errorf("outcome = %+v, want it skipped with %q", outcome, c.want)
			}
			if _, err := os.Stat(filepath.Join("scenes", "S1C_x.SAFE.zip")); err != nil {
				t.Errorf("a scene was removed: %v", err)
			}
			said := captureStdout(t, func() { printCleanupOutcome(outcome) })
			if !strings.Contains(said, "Kept the working files") {
				t.Errorf("summary = %q", said)
			}
		})
	}
}

func TestPrintCleanupOutcomeListsTheStrangers(t *testing.T) {
	said := captureStdout(t, func() {
		printCleanupOutcome(cleanupOutcome{Skipped: "left alone", Unexpected: []string{"notes.txt"}})
	})
	if !strings.Contains(said, "unexpected: notes.txt") {
		t.Errorf("summary = %q", said)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		0:                      "0.0 MB",
		5 * 1024 * 1024:        "5.0 MB",
		3 * 1024 * 1024 * 1024: "3.0 GB",
	}
	for count, want := range cases {
		if got := formatBytes(count); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", count, got, want)
		}
	}
}
