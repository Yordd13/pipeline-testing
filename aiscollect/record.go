// newRecorder: creates a recorder with empty per-vessel tables, wired to the counters and database writer.
// recorder.run: handles queued payloads until the context ends, then drains what is left in the queue.
// recorder.drain: handles every payload already in the queue without waiting for more.
// recorder.handle: decodes a payload, samples positions per vessel, fills in known identity and enqueues kept records.
// recorder.remember: merges a static message's name and ship type into what is known, never erasing earlier values.
// recorder.sweep: at most hourly, forgets vessels not heard from within forgetVesselAfter.

package main

import (
	"context"
	"time"

)

const sweepEvery = time.Hour

const forgetVesselAfter = time.Hour

type recorder struct {
	config   *Config
	counters *counters

	database *databaseWriter

	lastStored map[string]time.Time

	known map[string]vesselIdentity

	lastHeard map[string]time.Time
	sweptAt   time.Time

	firstFew []Record
}

func newRecorder(config *Config, tally *counters, database *databaseWriter) *recorder {
	return &recorder{
		config:     config,
		counters:   tally,
		database:   database,
		lastStored: map[string]time.Time{},
		known:      map[string]vesselIdentity{},
		lastHeard:  map[string]time.Time{},
	}
}

func (r *recorder) run(ctx context.Context, queue <-chan []byte) {
	for {
		select {
		case payload := <-queue:
			r.handle(payload)
		case <-ctx.Done():
			r.drain(queue)
			return
		}
	}
}

func (r *recorder) drain(queue <-chan []byte) {
	for {
		select {
		case payload := <-queue:
			r.handle(payload)
		default:
			return
		}
	}
}

func (r *recorder) handle(payload []byte) {
	arrived := time.Now().UTC()
	message, ok := decodeMessage(payload, arrived)
	if !ok {
		return
	}

	r.lastHeard[message.MMSI] = arrived
	r.sweep(arrived)

	if message.Identity != nil {
		r.remember(message.MMSI, *message.Identity)
		return
	}

	record := *message.Position
	if message.ClockFromArrival {
		r.counters.arrivalTimes.Add(1)
	}

	moment, ok := record.Time()
	if !ok {
		return
	}
	if last, seen := r.lastStored[record.MMSI]; seen &&
		moment.Sub(last) < r.config.SampleInterval {
		r.counters.messagesSampled.Add(1)
		return
	}

	identity := r.known[record.MMSI]
	if record.Name == "" {
		record.Name = identity.Name
	}
	if record.ShipType == nil {
		record.ShipType = identity.ShipType
	}

	r.lastStored[record.MMSI] = moment
	r.counters.messagesWritten.Add(1)

	r.database.enqueue(record)

	if len(r.firstFew) < r.config.PrintSample {
		r.firstFew = append(r.firstFew, record)
	}
}

func (r *recorder) remember(mmsi string, identity vesselIdentity) {
	existing := r.known[mmsi]
	if identity.Name != "" {
		existing.Name = identity.Name
	}
	if identity.ShipType != nil {
		existing.ShipType = identity.ShipType
	}
	r.known[mmsi] = existing
}

func (r *recorder) sweep(now time.Time) {
	if now.Sub(r.sweptAt) < sweepEvery {
		return
	}
	r.sweptAt = now

	for mmsi, heard := range r.lastHeard {
		if now.Sub(heard) < forgetVesselAfter {
			continue
		}
		delete(r.lastHeard, mmsi)
		delete(r.lastStored, mmsi)
		delete(r.known, mmsi)
	}
}
