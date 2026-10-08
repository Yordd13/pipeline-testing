// TestCountPositionsAroundAsksForTheWindowInUTC: checks the count query is given the window bounds as UTC instants.
// positions: builds n positions with distinct MMSIs and a time just short of the next second.
// valuesFor: returns a regexp matching an ais_position INSERT holding exactly the given number of rows.
// TestInsertPositionsBatchesAndCountsOnlyNewRows: checks 1201 positions go in batches of 500 and only new rows count.
// cutToMicrosecond.Match: reports whether the argument is the test time truncated to the microsecond.
// TestInsertPositionsCutsTimesAndPassesNULLs: checks times are cut to microseconds and unset fields are sent as NULL.
// TestInsertPositionsStopsAtAFailedBatch: checks a failed batch stops the insert and reports rows already stored.
// TestOpenRefusesAnUnusableEnvFile: checks Open fails on a missing env file or one without a password.

package resultsdb

import (
	"database/sql/driver"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestCountPositionsAroundAsksForTheWindowInUTC(t *testing.T) {
	db, mock := newMock(t)
	moment := time.Date(2026, 9, 22, 18, 59, 20, 0, time.FixedZone("EEST", 3*60*60))
	mock.ExpectQuery(q("SELECT COUNT(*) FROM ais_position WHERE ts BETWEEN ? AND ?")).
		WithArgs(sameMoment(moment.Add(-15*time.Minute)), sameMoment(moment.Add(15*time.Minute))).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(42))

	count, err := CountPositionsAround(db, moment, 15*time.Minute)
	if err != nil || count != 42 {
		t.Fatalf("count=%d err=%v, want 42", count, err)
	}
	checkExpectations(t, mock)
}

func positions(n int) []Position {
	list := make([]Position, n)
	for i := range list {
		list[i] = Position{
			MMSI:   fmt.Sprint(100000000 + i),
			Time:   time.Date(2026, 9, 22, 15, 59, 20, 999_999_900, time.UTC),
			Source: "aiscast",
		}
	}
	return list
}

func valuesFor(rows int) string {
	groups := strings.TrimSuffix(strings.Repeat("(?,?,?,?,?,?,?,?,?,?,?,?),", rows), ",")
	return `^INSERT INTO ais_position \(mmsi, ts, .*\) VALUES ` + regexp.QuoteMeta(groups) +
		` ON DUPLICATE KEY UPDATE mmsi = mmsi$`
}

func TestInsertPositionsBatchesAndCountsOnlyNewRows(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectExec(valuesFor(500)).WillReturnResult(sqlmock.NewResult(0, 500))
	mock.ExpectExec(valuesFor(500)).WillReturnResult(sqlmock.NewResult(0, 120))
	mock.ExpectExec(valuesFor(201)).WillReturnResult(sqlmock.NewResult(0, 0))

	inserted, err := InsertPositions(db, positions(1201))
	if err != nil || inserted != 620 {
		t.Fatalf("inserted=%d err=%v, want 620", inserted, err)
	}
	checkExpectations(t, mock)
}

type cutToMicrosecond struct{}

func (cutToMicrosecond) Match(v driver.Value) bool {
	moment, ok := v.(time.Time)
	return ok && moment.Equal(time.Date(2026, 9, 22, 15, 59, 20, 999_999_000, time.UTC))
}

func TestInsertPositionsCutsTimesAndPassesNULLs(t *testing.T) {
	db, mock := newMock(t)
	heading := 0
	position := positions(1)[0]
	position.Heading = &heading
	mock.ExpectExec(valuesFor(1)).
		WithArgs("100000000", cutToMicrosecond{}, 0.0, 0.0, nil, nil, 0, nil, nil, nil, nil, "aiscast").
		WillReturnResult(sqlmock.NewResult(1, 1))

	if inserted, err := InsertPositions(db, []Position{position}); err != nil || inserted != 1 {
		t.Fatalf("inserted=%d err=%v, want 1", inserted, err)
	}
	checkExpectations(t, mock)
}

func TestInsertPositionsStopsAtAFailedBatch(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectExec(valuesFor(500)).WillReturnResult(sqlmock.NewResult(0, 500))
	mock.ExpectExec(valuesFor(100)).WillReturnError(errBoom)

	inserted, err := InsertPositions(db, positions(600))
	if err != errBoom || inserted != 500 {
		t.Fatalf("inserted=%d err=%v, want 500 and boom", inserted, err)
	}

	mock.ExpectExec(valuesFor(1)).WillReturnResult(sqlmock.NewErrorResult(errBoom))
	if _, err := InsertPositions(db, positions(1)); err != errBoom {
		t.Errorf("err=%v, want the RowsAffected error", err)
	}
	checkExpectations(t, mock)
}

func TestOpenRefusesAnUnusableEnvFile(t *testing.T) {
	dir := t.TempDir()
	noPassword := filepath.Join(dir, "no-password.env")
	if err := os.WriteFile(noPassword, []byte("OTHER_KEY=fake\nAPP_DB_PASSWORD=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct{ path, want string }{
		"missing file": {filepath.Join(dir, "absent.env"), "reading"},
		"no password":  {noPassword, "has no APP_DB_PASSWORD"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			db, err := Open(c.path)
			if db != nil || err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("db=%v err=%v, want an error containing %q", db, err, c.want)
			}
		})
	}
}
