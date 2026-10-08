// warnIfAISWillBeMissing: warns when the database holds no AIS positions around the pass's acquisition time.

package main

import (
	"fmt"
	"time"

	"radarpipeline/internal/resultsdb"
)

const aisWindow = 15 * time.Minute

func warnIfAISWillBeMissing(config *Config, plan *PassPlan) {
	acquisition := plan.PassStart()
	if acquisition.IsZero() {
		return
	}

	db, err := resultsdb.Open(config.MySQLEnvFile)
	if err != nil {
		fmt.Printf("\nAIS: cannot ask the database: %v\n", err)
		return
	}
	defer db.Close()

	count, err := resultsdb.CountPositionsAround(db, acquisition, aisWindow)
	if err != nil {
		fmt.Printf("\nAIS: cannot ask the database: %v\n", err)
		return
	}
	if count == 0 {
		fmt.Printf("\nWARNING: this pass will have no AIS. The database holds no position within %s\n",
			aisWindow)
		fmt.Printf("  of %s, so the collector was not running then.\n",
			acquisition.UTC().Format("2006-01-02T15:04:05Z"))
		fmt.Println("  Every detection will therefore look like a vessel without a")
		fmt.Println("  transponder. Processing continues; read the result accordingly.")
	}
}
