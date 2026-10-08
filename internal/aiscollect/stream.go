// stream.redactedSubscription: renders the subscription frame as indented JSON with the API key replaced by a placeholder.
// stream.subscriptionFrame: builds the subscription with the API key, Bulgarian waters box and kept message types.
// stream.run: connects and keeps reconnecting with jittered backoff until the context ends.
// stream.connectOnce: dials, subscribes and queues every message read until the connection fails, dropping on overflow.
// keepAlive: pings the connection on a timer and closes it when the context ends or a ping fails.
// backoffFor: returns the doubling reconnect wait for an attempt, capped at the ceiling and jittered by +/-25%.
// logf: prints a formatted log line prefixed with the current UTC timestamp.

package aiscollect

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	pingEvery = 60 * time.Second

	silenceBeforeReconnect = 5 * time.Minute

	firstBackoff = time.Second

	settledAfter = 60 * time.Second

	writeTimeout = 10 * time.Second
)

type stream struct {
	config   *Config
	apiKey   string
	queue    chan []byte
	counters *counters
}

type subscription struct {
	APIKey             string        `json:"APIKey"`
	BoundingBoxes      [][][]float64 `json:"BoundingBoxes"`
	FilterMessageTypes []string      `json:"FilterMessageTypes,omitempty"`
}

func (s *stream) redactedSubscription() string {
	frame := s.subscriptionFrame()
	frame.APIKey = "<" + s.config.APIKeyEnv + ", not shown>"

	var rendered strings.Builder
	encoder := json.NewEncoder(&rendered)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(frame); err != nil {
		return "(the subscription frame could not be rendered)"
	}
	return strings.TrimRight(rendered.String(), "\n")
}

func (s *stream) subscriptionFrame() subscription {
	return subscription{
		APIKey:             s.apiKey,
		BoundingBoxes:      bulgarianWatersBox,
		FilterMessageTypes: keptMessageTypes,
	}
}

func (s *stream) run(ctx context.Context) {
	attempt := 0
	for ctx.Err() == nil {
		startedAt := time.Now()
		err := s.connectOnce(ctx)
		lived := time.Since(startedAt)

		if ctx.Err() != nil {
			return
		}
		logf("disconnected after %s: %v", lived.Round(time.Second), err)

		if lived >= settledAfter {
			attempt = 0
		}
		wait := backoffFor(attempt, s.config.MaxBackoff)
		attempt++

		logf("reconnecting in %s", wait.Round(100*time.Millisecond))
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (s *stream) connectOnce(ctx context.Context) error {
	dialer := *websocket.DefaultDialer

	dialer.EnableCompression = true
	dialer.HandshakeTimeout = 30 * time.Second

	connection, _, err := dialer.DialContext(ctx, s.config.StreamURL, nil)
	if err != nil {
		return fmt.Errorf("cannot connect: %w", err)
	}
	defer connection.Close()

	frame, err := json.Marshal(s.subscriptionFrame())
	if err != nil {
		return err
	}
	connection.SetWriteDeadline(time.Now().Add(subscribeWithin))
	if err := connection.WriteMessage(websocket.TextMessage, frame); err != nil {
		return fmt.Errorf("cannot subscribe: %w", err)
	}

	logf("connected and subscribed")

	connection.SetReadDeadline(time.Now().Add(silenceBeforeReconnect))
	connection.SetPongHandler(func(string) error {
		return connection.SetReadDeadline(time.Now().Add(silenceBeforeReconnect))
	})

	pinging, stopPinging := context.WithCancel(ctx)
	defer stopPinging()
	go keepAlive(pinging, connection)

	for {
		_, payload, err := connection.ReadMessage()
		if err != nil {
			return err
		}
		connection.SetReadDeadline(time.Now().Add(silenceBeforeReconnect))

		s.counters.bytesReceived.Add(uint64(len(payload)))
		s.counters.messagesReceived.Add(1)

		select {
		case s.queue <- payload:
		default:
			s.counters.overflows.Add(1)
		}
	}
}

func keepAlive(ctx context.Context, connection *websocket.Conn) {
	ticker := time.NewTicker(pingEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			connection.Close()
			return
		case <-ticker.C:
			deadline := time.Now().Add(writeTimeout)
			if err := connection.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
				connection.Close()
				return
			}
		}
	}
}

func backoffFor(attempt int, ceiling time.Duration) time.Duration {
	wait := firstBackoff << attempt
	if wait > ceiling || wait <= 0 {
		wait = ceiling
	}
	jitter := 0.75 + rand.Float64()/2
	return time.Duration(float64(wait) * jitter)
}

func logf(format string, arguments ...any) {
	fmt.Printf("%s  %s\n", time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		fmt.Sprintf(format, arguments...))
}
