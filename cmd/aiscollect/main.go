// main: parses the flags and runs the collector, exiting with status 1 on error.
// parseCommandLineFlags: builds the Config from the defaults overridden by command-line flags.

package main

import (
	"flag"
	"fmt"
	"os"

	"radarpipeline/internal/aiscollect"
)

func main() {
	config := parseCommandLineFlags()
	if err := aiscollect.Run(config); err != nil {
		fmt.Fprintln(os.Stderr, "\nError:", err)
		os.Exit(1)
	}
}

func parseCommandLineFlags() aiscollect.Config {
	config := aiscollect.NewDefaultConfig()

	flag.StringVar(&config.StreamURL, "stream-url", config.StreamURL,
		"websocket to subscribe on; any aisstream.io-compatible /v0 endpoint")
	flag.StringVar(&config.APIKeyEnv, "key-env", config.APIKeyEnv,
		"environment variable holding the token for -stream-url")
	flag.DurationVar(&config.SampleInterval, "sample", config.SampleInterval,
		"shortest gap between two stored positions for the same vessel")
	flag.DurationVar(&config.RunFor, "run-for", config.RunFor,
		"stop after this long; unset means run until stopped")
	flag.IntVar(&config.PrintSample, "print", config.PrintSample,
		"echo this many stored records, for confirming a new installation")
	flag.StringVar(&config.MySQLEnvFile, "mysql-env", config.MySQLEnvFile,
		"file holding APP_DB_PASSWORD for the results database")

	flag.Parse()
	return config
}
