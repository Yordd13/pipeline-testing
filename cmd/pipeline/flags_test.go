// parseArguments: parses the given command line with a fresh flag set and restores the real one afterwards.
// TestParseCommandLineFlagsKeepsTheDefaults: checks that no flags leave the default configuration untouched.
// TestParseCommandLineFlagsReadsEveryKindOfFlag: checks string, number and switch flags land in the Config and -output is noted.

package main

import (
	"flag"
	"os"
	"testing"

	"radarpipeline/internal/config"
)

func parseArguments(t *testing.T, arguments ...string) config.Config {
	t.Helper()
	originalArgs, originalFlags := os.Args, flag.CommandLine
	t.Cleanup(func() { os.Args, flag.CommandLine = originalArgs, originalFlags })
	os.Args = append([]string{"pipeline"}, arguments...)
	flag.CommandLine = flag.NewFlagSet("pipeline", flag.ContinueOnError)
	return parseCommandLineFlags()
}

func TestParseCommandLineFlagsKeepsTheDefaults(t *testing.T) {
	got := parseArguments(t)
	want := config.NewDefaultConfig()
	if got != want {
		t.Errorf("parsed %+v, want the defaults %+v", got, want)
	}
}

func TestParseCommandLineFlagsReadsEveryKindOfFlag(t *testing.T) {
	got := parseArguments(t, "-area", "burgas", "-output", "named.dim", "-heap", "12", "-no-detect", "-open",
		"-search-only", "-passes", "3", "-tile-retention", "5")
	if got.SearchAreaName != "burgas" || got.ResultFileName != "named.dim" || !got.ResultNameGivenExplicitly {
		t.Errorf("area or output not read: %+v", got)
	}
	if got.SnapHeapGB != 12 || got.PassesToSurvey != 3 || got.TileRetentionRuns != 5 {
		t.Errorf("numbers not read: %+v", got)
	}
	if !got.SkipDetection || !got.OpenResultFolderWhenDone || !got.SearchOnly {
		t.Errorf("switches not read: %+v", got)
	}
}
