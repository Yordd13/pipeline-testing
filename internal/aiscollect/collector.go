// Run: checks the config, loads the key, then runs stream, recorder and database writer until stopped or timed out.
// printSummary: prints the received, kept, dropped and stored counts plus any losses and the first records kept.
// humanBytes: formats a byte count as B, kB, MB or GB.
// indent: indents every line of the text by two spaces.
// readAPIKey: reads the stream token from the named environment variable, failing if it is empty.
// loadEnvFile: loads the .env file when it exists, failing only if it exists but cannot be parsed.

package aiscollect

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/joho/godotenv"
)

func Run(config Config) error {
	if err := config.check(); err != nil {
		return err
	}
	if err := loadEnvFile(); err != nil {
		return err
	}
	apiKey, err := readAPIKey(config.APIKeyEnv)
	if err != nil {
		return err
	}

	tally := &counters{}
	queue := make(chan []byte, config.QueueSize)
	database := newDatabaseWriter(config.MySQLEnvFile, tally)
	writer := newRecorder(&config, tally, database)
	feed := &stream{config: &config, apiKey: apiKey, queue: queue, counters: tally}

	fmt.Printf("Endpoint:  %s\n", config.StreamURL)
	fmt.Println("Subscription sent on every connection:")
	fmt.Println(indent(feed.redactedSubscription()))
	fmt.Printf("\nSampling:  one position per MMSI per %s\n", config.SampleInterval)
	fmt.Printf("Stored in: MySQL only (%s); no files are written\n\n", config.MySQLEnvFile)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if config.RunFor > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.RunFor)
		defer cancel()
	}

	go database.run()

	writing := make(chan struct{})
	go func() {
		defer close(writing)
		writer.run(ctx, queue)
	}()

	feed.run(ctx)
	<-writing
	database.stop()

	printSummary(writer, tally)
	return nil
}

func printSummary(writer *recorder, tally *counters) {
	received := tally.messagesReceived.Load()
	written := tally.messagesWritten.Load()

	fmt.Println()
	fmt.Printf("Bytes received:      %s\n", humanBytes(tally.bytesReceived.Load()))
	fmt.Printf("Messages received:   %d\n", received)
	fmt.Printf("Messages kept:       %d\n", written)
	fmt.Printf("Dropped by sampling: %d\n", tally.messagesSampled.Load())
	fmt.Printf("Queue overflows:     %d\n", tally.overflows.Load())
	fmt.Printf("Stored in MySQL:     %d new\n", tally.databaseStored.Load())
	if failures := tally.databaseFailures.Load(); failures > 0 {
		fmt.Printf("MySQL write retries: %d\n", failures)
	}
	if full := tally.databaseQueueFull.Load(); full > 0 {
		fmt.Printf("LOST, MySQL queue full: %d\n", full)
	}
	if dropped := tally.databaseDropped.Load(); dropped > 0 {
		fmt.Printf("LOST while MySQL was away too long: %d\n", dropped)
	}
	if written > 0 {
		fmt.Printf("Received per kept:   %.1fx\n", float64(received)/float64(written))
	}
	if arrivals := tally.arrivalTimes.Load(); arrivals > 0 {
		fmt.Printf("\n%d message(s) carried no usable time_utc and were stamped with the\n"+
			"moment they arrived instead.\n", arrivals)
	}

	if len(writer.firstFew) > 0 {
		fmt.Println("\nFirst records kept:")
		for _, record := range writer.firstFew {
			fmt.Printf("  %s\n", record)
		}
	}
}

func humanBytes(count uint64) string {
	switch {
	case count >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(count)/(1<<30))
	case count >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(count)/(1<<20))
	case count >= 1<<10:
		return fmt.Sprintf("%.1f kB", float64(count)/(1<<10))
	default:
		return fmt.Sprintf("%d B", count)
	}
}

func indent(text string) string {
	return "  " + strings.ReplaceAll(text, "\n", "\n  ")
}

func readAPIKey(variable string) (string, error) {
	key := strings.TrimSpace(os.Getenv(variable))
	if key == "" {
		return "", fmt.Errorf("%s is not set; put it in %s or in the environment",
			variable, envFileName)
	}
	return key, nil
}

func loadEnvFile() error {
	if _, err := os.Stat(envFileName); err != nil {
		return nil
	}
	if err := godotenv.Load(); err != nil {
		return fmt.Errorf("could not read %s: %w", envFileName, err)
	}
	return nil
}
