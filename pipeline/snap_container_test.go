// TestDockerRunArgsMountEverythingSNAPNeeds: checks docker run gets every mount, the memory limit, heap and GPT parameters.
// TestDetectionRunReadsStageOnesProduct: checks stage 2 reads stage 1's result from /out with the ship detection graph.
// TestInterpretSnapExitError: checks success stays nil and a launch failure is wrapped as an error running SNAP.
// TestWaitingForSNAPGivesUpOnTimeoutAndInterrupt: checks a timeout or interrupt abandons the run and kills its container once.
// TestDockerMemoryTotal: checks Docker's reported memory total in bytes is converted to GiB.
// TestRunDirIsTimestamped: checks the run folder is named after the time and container names start with snap-.

package main

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDockerRunArgsMountEverythingSNAPNeeds(t *testing.T) {
	config := newDefaultConfig()
	config.SnapHeapGB = 6
	run := preprocessRun(&config, "scene.SAFE")
	args := strings.Join(buildDockerRunArgs(&config, run, "out/run", "snap-1"), " ")

	for _, want := range []string{
		"run --rm --name snap-1",
		absolutePath("scenes") + ":/scenes",
		absolutePath("graphs") + ":/graphs",
		absolutePath("out/run") + ":/out",
		absolutePath("auxdata") + ":/root/.snap",
		"--memory 8g",
		snapImageDigest + " " + gptExecutablePath + " /graphs/preprocess_full.xml",
		"-J-Xmx6G",
		"-Pinput=/scenes/scene.SAFE",
		"-Poutput=/out/result.dim",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("docker run lacks %q:\n%s", want, args)
		}
	}
}

func TestDetectionRunReadsStageOnesProduct(t *testing.T) {
	config := newDefaultConfig()
	run := detectionRun(&config)
	if run.InputContainerPath != "/out/result.dim" || run.GraphContainerPath != "/graphs/ShipDetection.xml" ||
		run.OutputFileName != "ships.dim" {
		t.Errorf("stage 2 = %+v", run)
	}
}

func TestInterpretSnapExitError(t *testing.T) {
	if interpretSnapExitError(nil) != nil {
		t.Error("success became an error")
	}
	if err := interpretSnapExitError(errors.New("no such file")); err == nil ||
		!strings.Contains(err.Error(), "running SNAP: no such file") {
		t.Errorf("err = %v", err)
	}
}

func TestWaitingForSNAPGivesUpOnTimeoutAndInterrupt(t *testing.T) {
	calls := logFakeTools(t)
	t.Setenv(fakeSnapSleep, "300")

	cases := []struct {
		name   string
		cancel func() (context.Context, context.CancelFunc)
		want   string
	}{
		{"timeout", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), time.Millisecond)
		}, "gave up after 7 minutes"},
		{"interrupt", func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, cancel
		}, "interrupted"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			config := newDefaultConfig()
			command := exec.Command("docker", buildDockerRunArgs(&config, detectionRun(&config), t.TempDir(), "snap-x")...)
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := c.cancel()
			defer cancel()
			err := waitForContainerToFinish(ctx, command, "snap-x", 7)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want %q", err, c.want)
			}
		})
	}

	killed := 0
	for _, call := range calls() {
		if call == "docker kill snap-x" {
			killed++
		}
	}
	if killed != 2 {
		t.Errorf("the container was killed %d times, want once per abandoned run", killed)
	}
}

func TestDockerMemoryTotal(t *testing.T) {
	t.Setenv(fakeDockerMemTotal, "3221225472")
	if got, err := dockerMemoryTotalGiB(); err != nil || got != 3 {
		t.Errorf("total = %v GiB, %v; want 3", got, err)
	}
}

func TestRunDirIsTimestamped(t *testing.T) {
	config := newDefaultConfig()
	config.OutputDir = t.TempDir()
	runDir, err := createTimestampedRunDir(&config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := time.Parse(runDirTimestampLayout, filepath.Base(runDir)); err != nil {
		t.Errorf("run folder %s is not named after the time: %v", runDir, err)
	}
	if !strings.HasPrefix(newUniqueContainerName(), "snap-") {
		t.Error("container names lost their prefix")
	}
}
