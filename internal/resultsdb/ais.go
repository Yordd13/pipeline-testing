// CountPositionsAround: counts AIS positions from any source within the window either side of the moment in UTC.
// InsertPositions: stores positions in batches of 500, skipping any already stored, and returns how many were new.

package resultsdb

import (
	"database/sql"
	"strings"
	"time"
)

type Position struct {
	MMSI                string
	Time                time.Time
	Latitude, Longitude float64
	SOG, COG            *float64
	Heading, NavStatus  *int
	Name                *string
	ShipType, IMO       *int
	Source              string
}

func CountPositionsAround(db *sql.DB, moment time.Time, window time.Duration) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM ais_position WHERE ts BETWEEN ? AND ?`,
		moment.UTC().Add(-window), moment.UTC().Add(window)).Scan(&count)
	return count, err
}

const positionBatch = 500

func InsertPositions(db *sql.DB, positions []Position) (inserted int64, err error) {
	for start := 0; start < len(positions); start += positionBatch {
		batch := positions[start:min(start+positionBatch, len(positions))]

		var text strings.Builder
		text.WriteString(`INSERT INTO ais_position (mmsi, ts, latitude, longitude, sog, cog,
			heading, nav_status, name, ship_type, imo, source) VALUES `)
		args := make([]any, 0, len(batch)*12)
		for i, p := range batch {
			if i > 0 {
				text.WriteString(",")
			}
			text.WriteString("(?,?,?,?,?,?,?,?,?,?,?,?)")
			args = append(args, p.MMSI, p.Time.UTC().Truncate(time.Microsecond), p.Latitude, p.Longitude,
				p.SOG, p.COG, p.Heading, p.NavStatus, p.Name, p.ShipType, p.IMO, p.Source)
		}
		text.WriteString(" ON DUPLICATE KEY UPDATE mmsi = mmsi")

		result, err := db.Exec(text.String(), args...)
		if err != nil {
			return inserted, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return inserted, err
		}
		inserted += affected
	}
	return inserted, nil
}
