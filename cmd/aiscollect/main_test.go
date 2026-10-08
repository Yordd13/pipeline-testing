// parseArguments: parses the given command line with a fresh flag set and restores the real one afterwards.
// TestParseCommandLineFlagsKeepsTheDefaults: checks that no flags leave the default collector configuration untouched.
// TestParseCommandLineFlagsOverridesTheDefaults: checks every collector flag lands in the Config.

package main

import (
	"flag"
	"os"
	"testing"
	"time"

	"radarpipeline/internal/aiscollect"
)

func parseArguments(t *testing.T, arguments ...string) aiscollect.Config {
	t.Helper()
	originalArgs, originalFlags := os.Args, flag.CommandLine
	t.Cleanup(func() { os.Args, flag.CommandLine = originalArgs, originalFlags })
	os.Args = append([]string{"aiscollect"}, arguments...)
	flag.CommandLine = flag.NewFlagSet("aiscollect", flag.ContinueOnError)
	return parseCommandLineFlags()
}

func TestParseCommandLineFlagsKeepsTheDefaults(t *testing.T) {
	got := parseArguments(t)
	if want := aiscollect.NewDefaultConfig(); got != want {
		t.Errorf("parsed %+v, want the defaults %+v", got, want)
	}
}

func TestParseCommandLineFlagsOverridesTheDefaults(t *testing.T) {
	got := parseArguments(t, "-stream-url", "wss://example.invalid/v0/stream", "-key-env", "OTHER_KEY",
		"-sample", "30s", "-run-for", "2m", "-print", "4", "-mysql-env", "other.env")
	if got.StreamURL != "wss://example.invalid/v0/stream" || got.APIKeyEnv != "OTHER_KEY" || got.MySQLEnvFile != "other.env" {
		t.Errorf("strings not read: %+v", got)
	}
	if got.SampleInterval != 30*time.Second || got.RunFor != 2*time.Minute || got.PrintSample != 4 {
		t.Errorf("durations or count not read: %+v", got)
	}
}
