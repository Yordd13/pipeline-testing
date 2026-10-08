// TestPositionFromRecord: checks conversion to UTC, an empty name becoming NULL and a half-written record refused.
// TestRecordString: checks a record prints as its MMSI, timestamp and coordinates.

package aiscollect

import (
	"testing"
	"time"
)

func TestPositionFromRecord(t *testing.T) {
	sog := 11.4
	position, ok := positionFromRecord(Record{
		MMSI: "207400000", Timestamp: "2026-09-22T15:59:20.5+03:00", Latitude: 43.1, Longitude: 28.5,
		SOG: &sog, Name: "EXAMPLE",
	})
	if !ok {
		t.Fatal("a readable record was refused")
	}
	if !position.Time.Equal(time.Date(2026, 9, 22, 12, 59, 20, 500_000_000, time.UTC)) ||
		position.SOG != &sog || position.Name == nil || *position.Name != "EXAMPLE" || position.Source != "aiscast" {
		t.Errorf("got %+v", position)
	}
	if position, _ := positionFromRecord(Record{MMSI: "1", Timestamp: "2026-09-22T15:59:20Z"}); position.Name != nil {
		t.Error("an empty name was not stored as NULL")
	}
	if _, ok := positionFromRecord(Record{MMSI: "1", Timestamp: "2026-09-22T15:5"}); ok {
		t.Error("a record with a half-written time was accepted")
	}
}

func TestRecordString(t *testing.T) {
	record := Record{MMSI: "207400000", Timestamp: "2026-09-22T12:59:20Z", Latitude: 43.1, Longitude: 28.5}
	if got, want := record.String(), "207400000 at 2026-09-22T12:59:20Z (43.10000, 28.50000)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
