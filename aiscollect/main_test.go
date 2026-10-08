// TestConfigCheck: checks that Config.check accepts valid settings and names the problem for each invalid one.
// TestDefaultsKeepTheProviderConfigurable: checks the default stream URL, key variable and one-minute sampling.
// TestReadAPIKeyNamesTheVariableButNeverTheKey: checks that the key is trimmed and a blank one errors naming the variable.
// TestRunRefusesABadConfigurationOrAMissingKey: checks that run fails on a zero sample interval or a missing key.
// TestRunCollectsUntilRunForEnds: checks a full run against a fake stream prints the expected summary and never the key.

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestConfigCheck(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*Config)
		problem string
	}{
		{"defaults", func(*Config) {}, ""},
		{"no stream", func(c *Config) { c.StreamURL = "" }, "-stream-url"},
		{"no key variable", func(c *Config) { c.APIKeyEnv = "" }, "-key-env"},
		{"zero sampling", func(c *Config) { c.SampleInterval = 0 }, "positive"},
		{"sampling wider than the match radius", func(c *Config) { c.SampleInterval = 5 * time.Minute }, "match radius"},
		{"sampling at the limit", func(c *Config) { c.SampleInterval = 2 * time.Minute }, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := newDefaultConfig()
			test.change(&config)
			err := config.check()
			switch {
			case test.problem == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case test.problem != "" && (err == nil || !strings.Contains(err.Error(), test.problem)):
				t.Fatalf("got %v, want an error mentioning %q", err, test.problem)
			}
		})
	}
}

func TestDefaultsKeepTheProviderConfigurable(t *testing.T) {
	config := newDefaultConfig()
	if config.StreamURL != defaultStreamURL || config.APIKeyEnv != defaultKeyEnv {
		t.Errorf("defaults are %q / %q", config.StreamURL, config.APIKeyEnv)
	}
	if config.SampleInterval != time.Minute {
		t.Errorf("sampling is %s, want one minute", config.SampleInterval)
	}
}

func TestReadAPIKeyNamesTheVariableButNeverTheKey(t *testing.T) {
	t.Setenv("AISCOLLECT_TEST_KEY", "  secret-value  ")
	key, err := readAPIKey("AISCOLLECT_TEST_KEY")
	if err != nil || key != "secret-value" {
		t.Fatalf("got %q, %v; want the trimmed key", key, err)
	}

	t.Setenv("AISCOLLECT_TEST_KEY", "   ")
	_, err = readAPIKey("AISCOLLECT_TEST_KEY")
	if err == nil || !strings.Contains(err.Error(), "AISCOLLECT_TEST_KEY is not set") {
		t.Fatalf("got %v, want an error naming the variable", err)
	}
}

func TestRunRefusesABadConfigurationOrAMissingKey(t *testing.T) {
	t.Chdir(t.TempDir())

	config := newDefaultConfig()
	config.SampleInterval = 0
	if err := run(config); err == nil {
		t.Error("run accepted a zero sampling interval")
	}

	config = newDefaultConfig()
	config.APIKeyEnv = "AISCOLLECT_TEST_ABSENT_KEY"
	t.Setenv(config.APIKeyEnv, "")
	if err := run(config); err == nil || !strings.Contains(err.Error(), config.APIKeyEnv) {
		t.Errorf("got %v, want an error naming the missing variable", err)
	}
}

func TestRunCollectsUntilRunForEnds(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("AISCOLLECT_TEST_KEY", "test-key")

	served := make(chan struct{})
	url := fakeStream(t, func(connection *websocket.Conn) {
		readSubscription(t, connection)
		connection.WriteMessage(websocket.TextMessage, positionFrame(t, "111111111", 0))
		connection.WriteMessage(websocket.TextMessage, positionFrame(t, "222222222", 0))
		close(served)
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
		}
	})

	config := newDefaultConfig()
	config.StreamURL = url
	config.APIKeyEnv = "AISCOLLECT_TEST_KEY"
	config.QueueSize = 16
	config.PrintSample = 1
	config.RunFor = 500 * time.Millisecond
	config.MySQLEnvFile = fakeEnvFile(t, "")

	var err error
	output := captureStdout(t, func() { err = run(config) })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-served:
	default:
		t.Fatal("the collector never subscribed to the fake stream")
	}

	for _, want := range []string{
		"Endpoint:  " + url,
		"<AISCOLLECT_TEST_KEY, not shown>",
		"Messages received:   2",
		"Messages kept:       2",
		"2 position(s) could not be written before stopping and are LOST",
		"First records kept:",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output lacks %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "test-key") {
		t.Error("the key was printed")
	}
}
