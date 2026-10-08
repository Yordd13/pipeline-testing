// counters: declares the atomic counters the collector keeps for received, queued, sampled and stored messages.

package aiscollect

import (
	"sync/atomic"
)

type counters struct {
	bytesReceived    atomic.Uint64
	messagesReceived atomic.Uint64
	messagesWritten  atomic.Uint64
	messagesSampled  atomic.Uint64
	overflows        atomic.Uint64
	arrivalTimes     atomic.Uint64

	databaseStored    atomic.Uint64
	databaseFailures  atomic.Uint64
	databaseQueueFull atomic.Uint64
	databaseDropped   atomic.Uint64
}
