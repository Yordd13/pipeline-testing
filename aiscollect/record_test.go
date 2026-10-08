// TestOnePositionPerVesselPerSamplingInterval: checks that two minutes of 20 s reports keep two and sample out four.
// TestSamplingIsPerVesselNotPerStream: checks that three vessels reporting at once are all kept with the live-store source.
// TestANameLearnedFromAStaticMessageReachesLaterPositions: checks that static name and type fill in a later position.
// TestFramesThatAreNotPositionsAreNotKept: checks that a confirmation frame and invalid JSON keep nothing.
// TestArrivalStampedPositionsAreCounted: checks that a position without time_utc is counted as arrival-stamped and kept.
// TestPrintSampleCapsTheRecordsKeptForInspection: checks that firstFew holds no more than PrintSample records.
// TestRecorderDrainsTheQueueWhenStopped: checks that messages queued when the context ends are still handled.
// newTestRecorder: builds a recorder with a database writer whose goroutine is not started, plus its counters.
// queuedPositions: drains and returns every position waiting in the database writer's queue.
// positionFrame: builds a position report JSON frame for an MMSI at an offset in seconds from a fixed base time.
// waitFor: polls a condition until it holds, failing the test after five seconds.

package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"radarpipeline/internal/resultsdb"
)

func TestOnePositionPerVesselPerSamplingInterval(t *testing.T) {
	writer, tally, database := newTestRecorder(t)

	for offset := 0; offset < 120; offset += 20 {
		writer.handle(positionFrame(t, "207400000", offset))
	}

	if written := tally.messagesWritten.Load(); written != 2 {
		t.Fatalf("kept %d position(s) from two minutes of reports, want 2", written)
	}
	if dropped := tally.messagesSampled.Load(); dropped != 4 {
		t.Errorf("counted %d position(s) dropped by sampling, want 4", dropped)
	}
	if queued := queuedPositions(database); len(queued) != 2 {
		t.Errorf("handed %d position(s) to the database writer, want 2", len(queued))
	}
}

func TestSamplingIsPerVesselNotPerStream(t *testing.T) {
	writer, tally, database := newTestRecorder(t)

	for _, mmsi := range []string{"111111111", "222222222", "333333333"} {
		writer.handle(positionFrame(t, mmsi, 0))
	}

	if written := tally.messagesWritten.Load(); written != 3 {
		t.Fatalf("kept %d of 3 vessels reporting at the same moment", written)
	}
	queued := queuedPositions(database)
	if len(queued) != 3 {
		t.Fatalf("handed %d position(s) to the database writer, want 3", len(queued))
	}
	for _, position := range queued {
		if position.Source != positionSource {
			t.Errorf("position source is %q, want %q", position.Source, positionSource)
		}
	}
}

func TestANameLearnedFromAStaticMessageReachesLaterPositions(t *testing.T) {
	writer, tally, database := newTestRecorder(t)

	static := []byte(`{"MessageType":"ShipStaticData","MetaData":{"MMSI":207400000},
	  "Message":{"ShipStaticData":{"UserID":207400000,"Valid":true,
	  "Name":"EXAMPLE VESSEL","Type":70}}}`)
	writer.handle(static)
	writer.handle([]byte(`{"MessageType":"StaticDataReport","MetaData":{"MMSI":207400000},
	  "Message":{"StaticDataReport":{"ReportA":{"Valid":true,"Name":"EXAMPLE VESSEL"}}}}`))
	writer.handle(positionFrame(t, "207400000", 0))

	if tally.messagesWritten.Load() != 1 {
		t.Fatal("the position was not kept")
	}
	if len(writer.firstFew) != 1 {
		t.Fatal("nothing was captured for inspection")
	}
	stored := writer.firstFew[0]
	if stored.Name != "EXAMPLE VESSEL" {
		t.Errorf("name is %q; position reports do not carry one, so it has to come "+
			"from the static message heard earlier", stored.Name)
	}
	if stored.ShipType == nil || *stored.ShipType != 70 {
		t.Error("ship type was not carried over from the static message")
	}

	queued := queuedPositions(database)
	if len(queued) != 1 || queued[0].Name == nil || *queued[0].Name != "EXAMPLE VESSEL" {
		t.Errorf("the database writer did not receive the named position: %+v", queued)
	}
}

func TestFramesThatAreNotPositionsAreNotKept(t *testing.T) {
	writer, tally, database := newTestRecorder(t)

	writer.handle([]byte(`{"MessageType":"SubscriptionConfirmation"}`))
	writer.handle([]byte(`not json`))

	if tally.messagesWritten.Load() != 0 || len(queuedPositions(database)) != 0 {
		t.Fatal("a frame that is not a position was kept")
	}
}

func TestArrivalStampedPositionsAreCounted(t *testing.T) {
	writer, tally, database := newTestRecorder(t)

	writer.handle([]byte(`{"MessageType":"PositionReport","MetaData":{"MMSI":111111111},
	  "Message":{"PositionReport":{"Valid":true,"Latitude":43.1,"Longitude":28.5}}}`))

	if tally.arrivalTimes.Load() != 1 {
		t.Errorf("counted %d arrival-stamped position(s), want 1", tally.arrivalTimes.Load())
	}
	if tally.messagesWritten.Load() != 1 || len(queuedPositions(database)) != 1 {
		t.Error("a position stamped on arrival should still be kept")
	}
}

func TestPrintSampleCapsTheRecordsKeptForInspection(t *testing.T) {
	writer, _, _ := newTestRecorder(t)
	writer.config.PrintSample = 2

	for _, mmsi := range []string{"111111111", "222222222", "333333333"} {
		writer.handle(positionFrame(t, mmsi, 0))
	}
	if len(writer.firstFew) != 2 {
		t.Errorf("kept %d record(s) for inspection, want 2", len(writer.firstFew))
	}
}

func TestRecorderDrainsTheQueueWhenStopped(t *testing.T) {
	writer, tally, _ := newTestRecorder(t)

	queue := make(chan []byte, 4)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		writer.run(ctx, queue)
	}()

	queue <- positionFrame(t, "111111111", 0)
	waitFor(t, "the first message to be handled", func() bool {
		return tally.messagesWritten.Load() == 1
	})

	cancel()
	queue <- positionFrame(t, "222222222", 0)
	queue <- positionFrame(t, "333333333", 0)
	<-done
	writer.drain(queue)

	if written := tally.messagesWritten.Load(); written != 3 {
		t.Errorf("kept %d position(s), want 3: the queue was not drained", written)
	}
}

func newTestRecorder(t *testing.T) (*recorder, *counters, *databaseWriter) {
	t.Helper()
	config := newDefaultConfig()
	config.PrintSample = 4
	tally := &counters{}
	database := newDatabaseWriter(fakeEnvFile(t, ""), tally)
	return newRecorder(&config, tally, database), tally, database
}

func queuedPositions(database *databaseWriter) []resultsdb.Position {
	var positions []resultsdb.Position
	for {
		select {
		case position := <-database.in:
			positions = append(positions, position)
		default:
			return positions
		}
	}
}

func positionFrame(t *testing.T, mmsi string, offsetSeconds int) []byte {
	t.Helper()
	base := time.Date(2026, 9, 22, 15, 59, 0, 0, time.UTC)
	stamp := base.Add(time.Duration(offsetSeconds) * time.Second).
		Format("2006-01-02 15:04:05 -0700 MST")

	return []byte(fmt.Sprintf(`{
	  "MessageType": "PositionReport",
	  "MetaData": {"MMSI": %s, "time_utc": %q},
	  "Message": {"PositionReport": {
	    "UserID": %s, "Valid": true, "Latitude": 43.1, "Longitude": 28.5
	  }}
	}`, mmsi, stamp, mmsi))
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
