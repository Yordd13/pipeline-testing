// newDefaultConfig: returns a Config with the default stream URL, key variable, 60 s sampling and queue sizes.
// Config.check: rejects an empty stream URL or key variable and a sample interval that is non-positive or over 2 min.

package main

import (
	"fmt"
	"time"

	"radarpipeline/internal/resultsdb"
)

const (
	envFileName = ".env"

	subscribeWithin = 3 * time.Second
)

const (
	defaultStreamURL = "wss://ais.openwaters.io/v0/stream"
	defaultKeyEnv    = "AISCAST_KEY"
)

var keptMessageTypes = []string{
	"PositionReport",
	"StandardClassBPositionReport",
	"ExtendedClassBPositionReport",
	"ShipStaticData",
	"StaticDataReport",
}

var bulgarianWatersBox = [][][]float64{{
	{41.8, 27.2},
	{44.0, 30.5},
}}

type Config struct {
	StreamURL string
	APIKeyEnv string

	SampleInterval time.Duration

	QueueSize int

	MaxBackoff time.Duration

	RunFor time.Duration

	PrintSample int

	MySQLEnvFile string
}

func newDefaultConfig() Config {
	return Config{
		StreamURL:      defaultStreamURL,
		APIKeyEnv:      defaultKeyEnv,
		SampleInterval: 60 * time.Second,
		QueueSize:      8192,
		MaxBackoff:     60 * time.Second,
		MySQLEnvFile:   resultsdb.DefaultEnvFile,
	}
}

func (c *Config) check() error {
	if c.StreamURL == "" {
		return fmt.Errorf("-stream-url is empty; there is nothing to connect to")
	}
	if c.APIKeyEnv == "" {
		return fmt.Errorf("-key-env is empty; there is no variable to read the token from")
	}
	if c.SampleInterval <= 0 {
		return fmt.Errorf("-sample must be positive")
	}
	if c.SampleInterval > 2*time.Minute {
		return fmt.Errorf(
			"-sample is %s; beyond about a minute a vessel underway moves further "+
				"between stored positions than the pipeline's 1 km match radius, so "+
				"raising it silently breaks matching for moving ships", c.SampleInterval)
	}
	return nil
}
