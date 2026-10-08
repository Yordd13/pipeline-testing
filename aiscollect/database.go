// newDatabaseWriter: creates a database writer with its buffered position queue for the given env file.
// databaseWriter.enqueue: converts a record to a position and queues it without blocking, counting it if the queue is full.
// databaseWriter.run: holds queued positions and writes them per full batch or timer tick until the queue is closed.
// databaseWriter.stop: closes the queue and waits for the writer's final write to finish.
// databaseWriter.hold: appends a position to the in-memory backlog, dropping and counting the oldest when it is full.
// databaseWriter.connect: opens the MySQL pool if not yet open, reporting success or recording the failure.
// databaseWriter.write: inserts all held positions, clearing them on success and logging recovery after failures.
// databaseWriter.fail: counts a write failure and logs only the first of a consecutive run of them.
// databaseWriter.close: reports any positions still held as lost and closes the database pool.

package main

import (
	"database/sql"
	"time"

	"radarpipeline/internal/resultsdb"
)

const (
	databaseQueueSize = 8192

	databaseBatch = 500

	databaseWriteEvery = 5 * time.Second

	maxHeld = 1_000_000
)

type databaseWriter struct {
	envFile string
	in      chan resultsdb.Position
	done    chan struct{}
	tally   *counters

	db      *sql.DB
	held    []resultsdb.Position
	failing bool
}

func newDatabaseWriter(envFile string, tally *counters) *databaseWriter {
	return &databaseWriter{
		envFile: envFile,
		in:      make(chan resultsdb.Position, databaseQueueSize),
		done:    make(chan struct{}),
		tally:   tally,
	}
}

func (w *databaseWriter) enqueue(record Record) {
	position, ok := positionFromRecord(record)
	if !ok {
		return
	}
	select {
	case w.in <- position:
	default:
		w.tally.databaseQueueFull.Add(1)
	}
}

func (w *databaseWriter) run() {
	defer close(w.done)
	w.connect()

	ticker := time.NewTicker(databaseWriteEvery)
	defer ticker.Stop()
	for {
		select {
		case position, open := <-w.in:
			if !open {
				w.write()
				w.close()
				return
			}
			w.hold(position)
			if len(w.held) >= databaseBatch {
				w.write()
			}
		case <-ticker.C:
			w.write()
		}
	}
}

func (w *databaseWriter) stop() {
	close(w.in)
	<-w.done
}

func (w *databaseWriter) hold(position resultsdb.Position) {
	if len(w.held) >= maxHeld {
		copy(w.held, w.held[1:])
		w.held = w.held[:len(w.held)-1]
		w.tally.databaseDropped.Add(1)
	}
	w.held = append(w.held, position)
}

func (w *databaseWriter) connect() bool {
	if w.db != nil {
		return true
	}
	db, err := resultsdb.Open(w.envFile)
	if err != nil {
		w.fail(err)
		return false
	}
	w.db = db
	logf("database: connected; positions are written every %s", databaseWriteEvery)
	return true
}

func (w *databaseWriter) write() {
	if len(w.held) == 0 || !w.connect() {
		return
	}
	inserted, err := resultsdb.InsertPositions(w.db, w.held)
	if err != nil {
		w.fail(err)
		return
	}
	w.tally.databaseStored.Add(uint64(inserted))
	if w.failing {
		logf("database: reachable again; wrote the %d position(s) held meanwhile", len(w.held))
		w.failing = false
	}
	w.held = w.held[:0]
}

func (w *databaseWriter) fail(err error) {
	w.tally.databaseFailures.Add(1)
	if !w.failing {
		logf("database: cannot write (%v); holding positions in memory and retrying every %s",
			err, databaseWriteEvery)
		w.failing = true
	}
}

func (w *databaseWriter) close() {
	if len(w.held) > 0 {
		logf("database: %d position(s) could not be written before stopping and are LOST "+
			"(MySQL is the only place they go)", len(w.held))
	}
	if w.db != nil {
		w.db.Close()
	}
}
