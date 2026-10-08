// TestConnectOnceSubscribesAndQueuesWhatArrives: checks the subscription sent, frames queued and counters when the server ends the session.
// TestAFullQueueDropsAndCountsRatherThanWaits: checks that frames beyond the queue capacity are dropped and counted.
// TestRunReconnectsAndSubscribesAgain: checks run resubscribes after a dropped connection and closes the held session on cancel.
// TestRunStopsWhileWaitingToReconnect: checks cancelling during the reconnect backoff returns promptly without reconnecting.
// TestBackoffDoublesUpToTheCeilingWithJitter: checks that backoff doubles, caps at the ceiling and stays within jitter.
// fakeStream: serves a scripted websocket handler on a local test server and returns its ws URL.
// readSubscription: reads and decodes the first frame from the connection as a subscription.
// newTestStream: builds a stream with a test key and short backoff for the URL, returning it and its queue.

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestConnectOnceSubscribesAndQueuesWhatArrives(t *testing.T) {
	subscriptions := make(chan subscription, 1)
	url := fakeStream(t, func(connection *websocket.Conn) {
		subscriptions <- readSubscription(t, connection)
		for _, frame := range []string{
			`{"MessageType":"SubscriptionConfirmation"}`,
			string(positionFrame(t, "111111111", 0)),
			string(positionFrame(t, "222222222", 0)),
		} {
			if err := connection.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
				t.Error(err)
				return
			}
		}
	})

	feed, queue := newTestStream(url, 8)
	err := feed.connectOnce(context.Background())
	if err == nil {
		t.Fatal("connectOnce returned no reason for the connection ending")
	}

	sent := <-subscriptions
	if sent.APIKey != "test-key" {
		t.Errorf("the subscription carried key %q, want the configured one", sent.APIKey)
	}
	if len(sent.BoundingBoxes) != 1 || sent.BoundingBoxes[0][0][0] != 41.8 {
		t.Errorf("the subscription box is %v", sent.BoundingBoxes)
	}
	if len(sent.FilterMessageTypes) != len(keptMessageTypes) {
		t.Errorf("the subscription filters %v, want %v", sent.FilterMessageTypes, keptMessageTypes)
	}

	if len(queue) != 3 {
		t.Errorf("queued %d frame(s), want all 3 received", len(queue))
	}
	if received := feed.counters.messagesReceived.Load(); received != 3 {
		t.Errorf("counted %d message(s) received, want 3", received)
	}
	if feed.counters.bytesReceived.Load() == 0 {
		t.Error("no bytes were counted")
	}
}

func TestAFullQueueDropsAndCountsRatherThanWaits(t *testing.T) {
	url := fakeStream(t, func(connection *websocket.Conn) {
		readSubscription(t, connection)
		for range 5 {
			connection.WriteMessage(websocket.TextMessage, positionFrame(t, "111111111", 0))
		}
	})

	feed, queue := newTestStream(url, 2)
	feed.connectOnce(context.Background())

	if len(queue) != 2 {
		t.Errorf("queued %d frame(s), want the queue's capacity of 2", len(queue))
	}
	if overflows := feed.counters.overflows.Load(); overflows != 3 {
		t.Errorf("counted %d overflow(s), want 3", overflows)
	}
}

func TestRunReconnectsAndSubscribesAgain(t *testing.T) {
	var sessions, subscribed atomic.Int32
	var released atomic.Bool
	url := fakeStream(t, func(connection *websocket.Conn) {
		if readSubscription(t, connection).APIKey == "test-key" {
			subscribed.Add(1)
		}
		if sessions.Add(1) == 1 {
			return
		}
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				released.Store(true)
				return
			}
		}
	})

	feed, _ := newTestStream(url, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		feed.run(ctx)
	}()

	waitFor(t, "a second subscription", func() bool {
		return sessions.Load() >= 2
	})
	cancel()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after its context ended")
	}
	waitFor(t, "the second session to be closed by the client", released.Load)
	if got := sessions.Load(); got != 2 {
		t.Errorf("the server saw %d session(s), want 2: one dropped and one held until stopping", got)
	}
	if got := subscribed.Load(); got != 2 {
		t.Errorf("%d session(s) subscribed with the configured key, want both", got)
	}
}

func TestRunStopsWhileWaitingToReconnect(t *testing.T) {
	var sessions atomic.Int32
	var ended atomic.Bool
	url := fakeStream(t, func(connection *websocket.Conn) {
		readSubscription(t, connection)
		sessions.Add(1)
		connection.Close()
		ended.Store(true)
	})

	feed, _ := newTestStream(url, 1)
	feed.config.MaxBackoff = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		feed.run(ctx)
	}()

	waitFor(t, "the first session to end", func() bool {
		return ended.Load()
	})
	cancel()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("run kept waiting to reconnect after its context ended")
	}
	if got := sessions.Load(); got != 1 {
		t.Errorf("the server saw %d session(s), want 1: run should still have been waiting to reconnect", got)
	}
}

func TestBackoffDoublesUpToTheCeilingWithJitter(t *testing.T) {
	tests := []struct {
		attempt int
		ceiling time.Duration
		nominal time.Duration
	}{
		{0, time.Minute, time.Second},
		{1, time.Minute, 2 * time.Second},
		{3, time.Minute, 8 * time.Second},
		{6, time.Minute, time.Minute},
		{80, time.Minute, time.Minute},
		{0, 100 * time.Millisecond, 100 * time.Millisecond},
	}
	for _, test := range tests {
		for range 20 {
			wait := backoffFor(test.attempt, test.ceiling)
			low := time.Duration(float64(test.nominal) * 0.75)
			high := time.Duration(float64(test.nominal) * 1.25)
			if wait < low || wait > high {
				t.Errorf("backoffFor(%d, %s) = %s, want within [%s, %s]",
					test.attempt, test.ceiling, wait, low, high)
			}
		}
	}
}

func fakeStream(t *testing.T, handler func(*websocket.Conn)) string {
	t.Helper()
	upgrader := websocket.Upgrader{EnableCompression: true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		handler(connection)
	}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func readSubscription(t *testing.T, connection *websocket.Conn) subscription {
	t.Helper()
	var sent subscription
	_, frame, err := connection.ReadMessage()
	if err != nil {
		t.Errorf("no subscription arrived: %v", err)
		return sent
	}
	if err := json.Unmarshal(frame, &sent); err != nil {
		t.Errorf("the subscription is not JSON: %v", err)
	}
	return sent
}

func newTestStream(url string, queueSize int) (*stream, chan []byte) {
	config := newDefaultConfig()
	config.StreamURL = url
	config.MaxBackoff = 20 * time.Millisecond
	queue := make(chan []byte, queueSize)
	return &stream{config: &config, apiKey: "test-key", queue: queue, counters: &counters{}}, queue
}
