// main: parses flags and either exports the AOI, runs a search only, or plans and processes a full pass.
// loadEnvFile: loads the .env file into the environment if it exists, failing only when it cannot be parsed.
// exitWithError: prints the error to standard error and exits with status 1.

package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

const envFileName = ".env"

func main() {
	config := parseCommandLineFlags()
	if err := loadEnvFile(); err != nil {
		exitWithError(err)
	}

	if config.ExportAOIPath != "" {
		if err := writeAOIGeoJSON(config.ExportAOIPath); err != nil {
			exitWithError(err)
		}
		return
	}

	if config.SearchOnly {
		if err := reportSearch(&config); err != nil {
			exitWithError(err)
		}
		return
	}

	if err := runPreflightChecks(&config); err != nil {
		exitWithError(err)
	}

	plan, err := buildPassPlan(&config)
	if err != nil {
		exitWithError(err)
	}

	warnIfAISWillBeMissing(&config, plan)

	if err := processPass(&config, plan); err != nil {
		exitWithError(err)
	}
}

func loadEnvFile() error {
	if _, err := os.Stat(envFileName); err != nil {
		return nil
	}
	if err := godotenv.Load(); err != nil {
		return fmt.Errorf("could not read .env: %w", err)
	}
	return nil
}

func exitWithError(err error) {
	fmt.Fprintln(os.Stderr, "\nError:", err)
	os.Exit(1)
}
