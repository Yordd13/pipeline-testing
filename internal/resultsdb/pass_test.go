// newMock: creates a regexp-matching sqlmock database that is closed when the test ends.
// checkExpectations: fails the test if any expected mock statement was not executed.
// q: quotes a SQL string so it matches literally as a regular expression.
// sameMoment.Match: reports whether the argument is a time at the same instant, whatever its location.
// testPass: builds a consistent pass with one slice, two detections, a raster and a fractional start time.
// expectInserts: expects every INSERT that testPass needs, with the pass_run row given the passID.
// TestSavePassWritesANewPassInOneTransaction: checks a new pass is looked up by second, inserted and committed.
// TestSavePassSkipsAStoredPassWithoutWriting: checks an existing pass with SkipExisting is rolled back unwritten.
// TestSavePassReplacesAStoredPass: checks ReplaceExisting deletes the stored pass's rows and inserts it again.
// TestSavePassRollsBackOnEveryFailure: checks every failing step rolls back and returns an error naming the step.
// TestSavePassReportsAFailedBeginAndCommit: checks errors from beginning and committing the transaction are returned.
// TestValidate: checks Validate accepts a consistent pass and rejects missing start, slices or mismatched counts.
// TestToSecondCutsToTheUTCSecond: checks ToSecond truncates rather than rounds and returns the time in UTC.
// TestCheckSchema: checks CheckSchema accepts current or newer versions and rejects older, missing or unreadable.

package resultsdb

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

const (
	selectExisting = "SELECT id FROM pass_run WHERE area = ? AND pass_start = ? FOR UPDATE"
	insertRun      = "INSERT INTO pass_run "
	insertSlice    = "INSERT INTO pass_slice "
	insertDetect   = "INSERT INTO detection "
	insertRaster   = "INSERT INTO raster_layer "
	testProduct    = "S1D_IW_GRDH_1SDV_20260926T155130_20260926T155155_004751_008E53_DDA3"
)

var errBoom = errors.New("boom")

func newMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
	})
	return db, mock
}

func checkExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func q(sql string) string { return regexp.QuoteMeta(sql) }

type sameMoment time.Time

func (m sameMoment) Match(v driver.Value) bool {
	actual, ok := v.(time.Time)
	return ok && actual.Equal(time.Time(m))
}

func testPass() Pass {
	start := time.Date(2026, 9, 26, 15, 51, 30, 640_000_000, time.UTC)
	distance := "106327"
	zoom := 12
	coverage := 21
	return Pass{
		Start: start, Orbit: 4751, Area: "bulgaria", CoveragePercent: &coverage,
		SlicesProcessed: 1, RawCFAR: 26, DroppedOutsideAOI: 24, Written: 2, AOIVertices: 4,
		Slices: []Slice{{
			Index: 1, ProductName: testProduct, ProductID: "2b91b54d", Mission: "S1D", Start: start,
			Detections: []Detection{
				{ID: "slice_1_target_001", Latitude: "43.52", Longitude: "29.92", WidthM: "210.0",
					LengthM: "290.0", PixelX: "12214", PixelY: "8064", DistanceToShoreM: &distance},
				{ID: "slice_1_target_002", Latitude: "43.10", Longitude: "28.70", WidthM: "30.0",
					LengthM: "40.0", PixelX: "100", PixelY: "200"},
			},
		}},
		Raster: &Raster{Type: "tiles", File: "tiles/x", South: 42, West: 27, North: 44, East: 30, MaxNativeZoom: &zoom},
	}
}

var wantStart = time.Date(2026, 9, 26, 15, 51, 30, 0, time.UTC)

func expectInserts(mock sqlmock.Sqlmock, passID int64) {
	mock.ExpectExec(q(insertRun)).
		WithArgs(sameMoment(wantStart), 4751, "bulgaria", 21, 1, 0, 26, 24, 0, 2, 4).
		WillReturnResult(sqlmock.NewResult(passID, 1))
	mock.ExpectExec(q(insertSlice)).
		WithArgs(passID, 1, testProduct, "2b91b54d", "S1D", sameMoment(wantStart)).
		WillReturnResult(sqlmock.NewResult(70, 1))
	mock.ExpectExec(q(insertDetect)).
		WithArgs(70, "slice_1_target_001", "43.52", "29.92", "210.0", "290.0", "12214", "8064", "106327").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(q(insertDetect)).
		WithArgs(70, "slice_1_target_002", "43.10", "28.70", "30.0", "40.0", "100", "200", nil).
		WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectExec(q(insertRaster)).
		WithArgs(passID, "tiles", "tiles/x", 42.0, 27.0, 44.0, 30.0, 12, nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
}

func TestSavePassWritesANewPassInOneTransaction(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(q(selectExisting)).WithArgs("bulgaria", sameMoment(wantStart)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	expectInserts(mock, 7)
	mock.ExpectCommit()

	written, err := SavePass(db, testPass(), SkipExisting)
	if err != nil || !written {
		t.Fatalf("written=%v err=%v, want a written pass", written, err)
	}
	checkExpectations(t, mock)
}

func TestSavePassSkipsAStoredPassWithoutWriting(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(q(selectExisting)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
	mock.ExpectRollback()

	written, err := SavePass(db, testPass(), SkipExisting)
	if err != nil || written {
		t.Fatalf("written=%v err=%v, want nothing written and no error", written, err)
	}
	checkExpectations(t, mock)
}

func TestSavePassReplacesAStoredPass(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(q(selectExisting)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
	for _, table := range []string{"DELETE d FROM detection d", "DELETE FROM pass_slice",
		"DELETE FROM raster_layer", "DELETE FROM pass_run WHERE id = ?"} {
		mock.ExpectExec(q(table)).WithArgs(3).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	expectInserts(mock, 8)
	mock.ExpectCommit()

	written, err := SavePass(db, testPass(), ReplaceExisting)
	if err != nil || !written {
		t.Fatalf("written=%v err=%v, want the pass replaced", written, err)
	}
	checkExpectations(t, mock)
}

func TestSavePassRollsBackOnEveryFailure(t *testing.T) {
	cases := []struct {
		name    string
		expect  func(sqlmock.Sqlmock)
		mutate  func(*Pass)
		replace bool
		want    string
	}{
		{"contradictory pass", func(sqlmock.Sqlmock) {}, func(p *Pass) { p.Written++ }, false, "were written"},
		{"lookup fails", func(m sqlmock.Sqlmock) {
			m.ExpectQuery(q(selectExisting)).WillReturnError(errBoom)
		}, nil, false, "boom"},
		{"delete fails", func(m sqlmock.Sqlmock) {
			m.ExpectQuery(q(selectExisting)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
			m.ExpectExec(q("DELETE d FROM detection d")).WillReturnError(errBoom)
		}, nil, true, "replacing the stored pass: boom"},
		{"pass insert fails", func(m sqlmock.Sqlmock) {
			m.ExpectQuery(q(selectExisting)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			m.ExpectExec(q(insertRun)).WillReturnError(errBoom)
		}, nil, false, "boom"},
		{"no insert id", func(m sqlmock.Sqlmock) {
			m.ExpectQuery(q(selectExisting)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			m.ExpectExec(q(insertRun)).WillReturnResult(sqlmock.NewErrorResult(errBoom))
		}, nil, false, "boom"},
		{"slice insert fails", func(m sqlmock.Sqlmock) {
			m.ExpectQuery(q(selectExisting)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			m.ExpectExec(q(insertRun)).WillReturnResult(sqlmock.NewResult(7, 1))
			m.ExpectExec(q(insertSlice)).WillReturnError(errBoom)
		}, nil, false, "slice_1: boom"},
		{"no slice id", func(m sqlmock.Sqlmock) {
			m.ExpectQuery(q(selectExisting)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			m.ExpectExec(q(insertRun)).WillReturnResult(sqlmock.NewResult(7, 1))
			m.ExpectExec(q(insertSlice)).WillReturnResult(sqlmock.NewErrorResult(errBoom))
		}, nil, false, "boom"},
		{"detection insert fails", func(m sqlmock.Sqlmock) {
			m.ExpectQuery(q(selectExisting)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			m.ExpectExec(q(insertRun)).WillReturnResult(sqlmock.NewResult(7, 1))
			m.ExpectExec(q(insertSlice)).WillReturnResult(sqlmock.NewResult(70, 1))
			m.ExpectExec(q(insertDetect)).WillReturnError(errBoom)
		}, nil, false, "detection slice_1_target_001: boom"},
		{"raster insert fails", func(m sqlmock.Sqlmock) {
			m.ExpectQuery(q(selectExisting)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			m.ExpectExec(q(insertRun)).WillReturnResult(sqlmock.NewResult(7, 1))
			m.ExpectExec(q(insertSlice)).WillReturnResult(sqlmock.NewResult(70, 1))
			m.ExpectExec(q(insertDetect)).WillReturnResult(sqlmock.NewResult(1, 1))
			m.ExpectExec(q(insertDetect)).WillReturnResult(sqlmock.NewResult(2, 1))
			m.ExpectExec(q(insertRaster)).WillReturnError(errBoom)
		}, nil, false, "raster: boom"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db, mock := newMock(t)
			mock.ExpectBegin()
			c.expect(mock)
			mock.ExpectRollback()

			pass := testPass()
			if c.mutate != nil {
				c.mutate(&pass)
			}
			onExisting := SkipExisting
			if c.replace {
				onExisting = ReplaceExisting
			}
			written, err := SavePass(db, pass, onExisting)
			if written || err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("written=%v err=%v, want an error containing %q", written, err, c.want)
			}
			checkExpectations(t, mock)
		})
	}
}

func TestSavePassReportsAFailedBeginAndCommit(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin().WillReturnError(errBoom)
	if _, err := SavePass(db, testPass(), SkipExisting); !errors.Is(err, errBoom) {
		t.Errorf("begin: err=%v, want boom", err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(q(selectExisting)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	expectInserts(mock, 7)
	mock.ExpectCommit().WillReturnError(errBoom)
	if _, err := SavePass(db, testPass(), SkipExisting); !errors.Is(err, errBoom) {
		t.Errorf("commit: err=%v, want boom", err)
	}
	checkExpectations(t, mock)
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Pass)
		want   string
	}{
		{"consistent", func(*Pass) {}, ""},
		{"no start", func(p *Pass) { p.Start = time.Time{} }, "no start time"},
		{"no slices", func(p *Pass) { p.Slices = nil }, "no slices"},
		{"detections disagree with written", func(p *Pass) { p.Written = 3; p.RawCFAR = 27 }, "2 detections, but the pass says 3"},
		{"raw does not add up", func(p *Pass) { p.SeamDuplicates = 1 }, "raw 26 is not outside 24 + seam 1 + written 2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pass := testPass()
			c.mutate(&pass)
			err := pass.Validate()
			switch {
			case c.want == "" && err != nil:
				t.Errorf("refused: %v", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Errorf("error %v, want one containing %q", err, c.want)
			}
		})
	}
}

func TestToSecondCutsToTheUTCSecond(t *testing.T) {
	sofia := time.FixedZone("EEST", 3*60*60)
	moment := time.Date(2026, 9, 26, 18, 51, 30, 999_999_999, sofia)
	got := ToSecond(moment)
	if !got.Equal(wantStart) || got.Location() != time.UTC {
		t.Errorf("ToSecond = %v, want %v in UTC", got, wantStart)
	}
}

func TestCheckSchema(t *testing.T) {
	cases := []struct {
		name    string
		version any
		err     error
		want    string
	}{
		{"required version", RequiredSchemaVersion, nil, ""},
		{"newer version", RequiredSchemaVersion + 1, nil, ""},
		{"older version", RequiredSchemaVersion - 1, nil, "schema is at version 6; this needs 7"},
		{"never migrated", nil, nil, "schema is at version 0"},
		{"no history table", nil, errBoom, "reading the schema version: boom"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db, mock := newMock(t)
			query := mock.ExpectQuery(q("SELECT MAX(CAST(version AS UNSIGNED)) FROM flyway_schema_history WHERE success = 1"))
			if c.err != nil {
				query.WillReturnError(c.err)
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(c.version))
			}
			err := CheckSchema(db)
			switch {
			case c.want == "" && err != nil:
				t.Errorf("refused: %v", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Errorf("error %v, want one containing %q", err, c.want)
			}
			checkExpectations(t, mock)
		})
	}
}
