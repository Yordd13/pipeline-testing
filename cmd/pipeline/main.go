// main: parses flags and either exports the AOI, runs a search only, or plans and processes a full pass.
// exitWithError: prints the error to standard error and exits with status 1.

package main

import (
	"fmt"
	"os"

	"radarpipeline/internal/config"
	"radarpipeline/internal/detection"
	"radarpipeline/internal/pass"
	"radarpipeline/internal/run"
)

func main() {
	cfg := parseCommandLineFlags()
	if err := config.LoadEnvFile(); err != nil {
		exitWithError(err)
	}

	if cfg.ExportAOIPath != "" {
		if err := detection.WriteAOIGeoJSON(cfg.ExportAOIPath); err != nil {
			exitWithError(err)
		}
		return
	}

	if cfg.SearchOnly {
		if err := pass.ReportSearch(&cfg); err != nil {
			exitWithError(err)
		}
		return
	}

	if err := run.RunPreflightChecks(&cfg); err != nil {
		exitWithError(err)
	}

	plan, err := pass.BuildPassPlan(&cfg)
	if err != nil {
		exitWithError(err)
	}

	run.WarnIfAISWillBeMissing(&cfg, plan)

	if err := run.ProcessPass(&cfg, plan); err != nil {
		exitWithError(err)
	}
}

func exitWithError(err error) {
	fmt.Fprintln(os.Stderr, "\nError:", err)
	os.Exit(1)
}
