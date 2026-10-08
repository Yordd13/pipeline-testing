// TestAFullBatchIsWrittenWithoutWaitingForTheTimer: checks that reaching the batch size triggers a write before the timer.
// TestHeldPositionsAreWrittenWhenTheWriterStops: checks that stopping writes the remaining positions and reports no loss.
// TestAFailedWriteIsRetriedWithTheSamePositions: checks that failed writes keep positions, log once, and log recovery.
// TestPositionsStillHeldAtStopAreReportedLost: checks that positions unwritten at stop are reported as LOST.
// TestAnUnusableEnvFileDoesNotStopTheWriter: checks that a missing or passwordless env file only counts failures and losses.
// TestEnqueueNeverWaits: checks that a full queue counts the overflow and an unreadable record is not queued.
// newMockedWriter: returns a database writer already connected to a sqlmock database, with its mock and counters.
// fakeEnvFile: writes the given contents to a temporary env file and returns its path.
// testRecord: returns a fixed test AIS record whose MMSI is derived from the index.
// captureStdout: runs fn with standard output redirected and returns what it printed.

package aiscollect

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

const insertPositions = `(?s)INSERT INTO ais_position \(mmsi, ts, .*\) VALUES .* ON DUPLICATE KEY UPDATE mmsi = mmsi`

func TestAFullBatchIsWrittenWithoutWaitingForTheTimer(t *testing.T) {
	writer, mock, tally := newMockedWriter(t)
	mock.ExpectExec(insertPositions).WillReturnResult(sqlmock.NewResult(0, databaseBatch))
	mock.ExpectClose()

	go writer.run()
	for i := range databaseBatch {
		writer.enqueue(testRecord(i))
	}
	waitFor(t, "the batch to be written", func() bool {
		return tally.databaseStored.Load() == databaseBatch
	})
	writer.stop()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
	if failures := tally.databaseFailures.Load(); failures != 0 {
		t.Errorf("counted %d failure(s), want none", failures)
	}
}

func TestHeldPositionsAreWrittenWhenTheWriterStops(t *testing.T) {
	writer, mock, tally := newMockedWriter(t)
	mock.ExpectExec(insertPositions).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectClose()

	go writer.run()
	for i := range 3 {
		writer.enqueue(testRecord(i))
	}
	output := captureStdout(t, writer.stop)

	if stored := tally.databaseStored.Load(); stored != 3 {
		t.Errorf("stored %d position(s) on stopping, want 3", stored)
	}
	if strings.Contains(output, "LOST") {
		t.Errorf("reported a loss although everything was written:\n%s", output)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestAFailedWriteIsRetriedWithTheSamePositions(t *testing.T) {
	writer, mock, tally := newMockedWriter(t)
	for i := range 2 {
		position, _ := positionFromRecord(testRecord(i))
		writer.hold(position)
	}

	mock.ExpectExec(insertPositions).WillReturnError(errors.New("server has gone away"))
	mock.ExpectExec(insertPositions).WillReturnError(errors.New("server has gone away"))
	mock.ExpectExec(insertPositions).WillReturnResult(sqlmock.NewResult(0, 1))

	output := captureStdout(t, func() {
		writer.write()
		writer.write()
	})
	if !writer.failing || len(writer.held) != 2 {
		t.Fatalf("after failing: failing=%v held=%d, want true and 2", writer.failing, len(writer.held))
	}
	if failures := tally.databaseFailures.Load(); failures != 2 {
		t.Errorf("counted %d failure(s), want 2", failures)
	}
	if count := strings.Count(output, "cannot write"); count != 1 {
		t.Errorf("logged the failure %d time(s); only the first of a run should be:\n%s", count, output)
	}

	output = captureStdout(t, writer.write)
	if writer.failing || len(writer.held) != 0 {
		t.Errorf("after recovering: failing=%v held=%d, want false and 0", writer.failing, len(writer.held))
	}
	if stored := tally.databaseStored.Load(); stored != 1 {
		t.Errorf("counted %d stored, want 1 new row", stored)
	}
	if !strings.Contains(output, "reachable again") {
		t.Errorf("the recovery was not logged:\n%s", output)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestPositionsStillHeldAtStopAreReportedLost(t *testing.T) {
	writer, mock, tally := newMockedWriter(t)
	mock.ExpectExec(insertPositions).WillReturnError(errors.New("deadlock"))
	mock.ExpectClose()

	go writer.run()
	for i := range 3 {
		writer.enqueue(testRecord(i))
	}
	output := captureStdout(t, writer.stop)

	if !strings.Contains(output, "3 position(s) could not be written before stopping and are LOST") {
		t.Errorf("the loss was not reported:\n%s", output)
	}
	if tally.databaseStored.Load() != 0 || tally.databaseFailures.Load() != 1 {
		t.Errorf("stored=%d failures=%d, want 0 and 1",
			tally.databaseStored.Load(), tally.databaseFailures.Load())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestAnUnusableEnvFileDoesNotStopTheWriter(t *testing.T) {
	tests := map[string]string{
		"missing file":        filepath.Join(t.TempDir(), "absent.env"),
		"no password in file": fakeEnvFile(t, "OTHER_SETTING=1\n"),
	}
	for name, envFile := range tests {
		t.Run(name, func(t *testing.T) {
			tally := &counters{}
			writer := newDatabaseWriter(envFile, tally)

			output := captureStdout(t, func() {
				go writer.run()
				writer.enqueue(testRecord(0))
				writer.stop()
			})

			if writer.db != nil {
				t.Fatal("a database was opened from an env file with no password")
			}
			if failures := tally.databaseFailures.Load(); failures != 2 {
				t.Errorf("counted %d failure(s), want 2", failures)
			}
			if !strings.Contains(output, "1 position(s) could not be written") {
				t.Errorf("the lost position was not reported:\n%s", output)
			}
		})
	}
}

func TestEnqueueNeverWaits(t *testing.T) {
	tally := &counters{}
	writer := newDatabaseWriter(fakeEnvFile(t, ""), tally)

	for i := range cap(writer.in) {
		writer.enqueue(testRecord(i))
	}
	writer.enqueue(testRecord(0))
	if full := tally.databaseQueueFull.Load(); full != 1 {
		t.Errorf("counted %d position(s) lost to a full queue, want 1", full)
	}

	writer = newDatabaseWriter(fakeEnvFile(t, ""), tally)
	writer.enqueue(Record{MMSI: "1", Timestamp: "half a line"})
	if len(writer.in) != 0 {
		t.Error("a record with an unreadable timestamp was queued")
	}
}

func newMockedWriter(t *testing.T) (*databaseWriter, sqlmock.Sqlmock, *counters) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	tally := &counters{}
	writer := newDatabaseWriter(fakeEnvFile(t, ""), tally)
	writer.db = db
	return writer, mock, tally
}

func fakeEnvFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mysql.env")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testRecord(i int) Record {
	return Record{
		MMSI:      fmt.Sprintf("2074%05d", i),
		Timestamp: "2026-09-22T15:59:20Z",
		Latitude:  43.1,
		Longitude: 28.5,
		Name:      "TEST",
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	collected := make(chan string)
	go func() {
		data, _ := io.ReadAll(reader)
		collected <- string(data)
	}()

	fn()
	writer.Close()
	os.Stdout = original
	return <-collected
}
