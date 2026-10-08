// Record.Time: parses the position's RFC 3339 timestamp as UTC, reporting false if it cannot be parsed.
// Record.String: formats the record as its MMSI, timestamp and coordinates.
// positionFromRecord: converts a decoded record into a database Position row, returning false if its time is unreadable.

package aiscollect

import (
	"fmt"
	"time"

	"radarpipeline/internal/resultsdb"
)

const positionSource = "aiscast"

type Record struct {
	MMSI      string
	Timestamp string
	Latitude  float64
	Longitude float64
	SOG       *float64
	COG       *float64
	Heading   *int
	NavStatus *int
	Name      string
	ShipType  *int
}

func (r Record) Time() (time.Time, bool) {
	moment, err := time.Parse(time.RFC3339, r.Timestamp)
	if err != nil {
		return time.Time{}, false
	}
	return moment.UTC(), true
}

func (r Record) String() string {
	return fmt.Sprintf("%s at %s (%.5f, %.5f)", r.MMSI, r.Timestamp, r.Latitude, r.Longitude)
}

func positionFromRecord(record Record) (resultsdb.Position, bool) {
	moment, ok := record.Time()
	if !ok {
		return resultsdb.Position{}, false
	}
	position := resultsdb.Position{
		MMSI: record.MMSI, Time: moment, Latitude: record.Latitude, Longitude: record.Longitude,
		SOG: record.SOG, COG: record.COG, Heading: record.Heading, NavStatus: record.NavStatus,
		ShipType: record.ShipType, Source: positionSource,
	}
	if record.Name != "" {
		name := record.Name
		position.Name = &name
	}
	return position, true
}
