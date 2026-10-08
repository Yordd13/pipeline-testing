// TestTheFakeToolsAreWhatRuns: checks the docker and explorer that run during tests are the fakes.
// TestDockerRunArgsMountEverythingSNAPNeeds: checks docker run gets every mount, the memory limit, heap and GPT parameters.
// TestDetectionRunReadsStageOnesProduct: checks stage 2 reads stage 1's result from /out with the ship detection graph.
// TestInterpretSnapExitError: checks success stays nil and a launch failure is wrapped as an error running SNAP.
// TestWaitingForSNAPGivesUpOnTimeoutAndInterrupt: checks a timeout or interrupt abandons the run and kills its container once.
// TestDockerMemoryTotal: checks Docker's reported memory total in bytes is converted to GiB.
// TestRunDirIsTimestamped: checks the run folder is named after the time and container names start with snap-.

package snap

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"radarpipeline/internal/config"
	"radarpipeline/internal/testsupport"
)

func TestTheFakeToolsAreWhatRuns(t *testing.T) {
	calls := testsupport.LogFakeTools(t)

	if err := EnsureDockerIsRunning(); err != nil {
		t.Fatalf("the fake docker said it was not running: %v", err)
	}
	cfg := config.NewDefaultConfig()
	cfg.OpenResultFolderWhenDone = true
	openResultFolderIfRequested(&cfg, filepath.Join(t.TempDir(), "x.dim"))

	got := calls()
	if len(got) != 2 || !strings.HasPrefix(got[0], "docker version") {
		t.Fatalf("the fake docker was not the one that ran; it logged %q", got)
	}
	if !strings.HasPrefix(got[1], "explorer /select,") {
		t.Errorf("the fake explorer was not the one that ran; it logged %q", got[1])
	}
}

func TestDockerRunArgsMountEverythingSNAPNeeds(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.SnapHeapGB = 6
	run := PreprocessRun(&cfg, "scene.SAFE")
	args := strings.Join(buildDockerRunArgs(&cfg, run, "out/run", "snap-1"), " ")

	for _, want := range []string{
		"run --rm --name snap-1",
		config.AbsolutePath("scenes") + ":/scenes",
		config.AbsolutePath("graphs") + ":/graphs",
		config.AbsolutePath("out/run") + ":/out",
		config.AbsolutePath("auxdata") + ":/root/.snap",
		"--memory 8g",
		config.SnapImageDigest + " " + config.GPTExecutablePath + " /graphs/preprocess_full.xml",
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
	cfg := config.NewDefaultConfig()
	run := DetectionRun(&cfg)
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
	calls := testsupport.LogFakeTools(t)
	t.Setenv(testsupport.FakeSnapSleep, "300")

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
			cfg := config.NewDefaultConfig()
			command := exec.Command("docker", buildDockerRunArgs(&cfg, DetectionRun(&cfg), t.TempDir(), "snap-x")...)
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
	t.Setenv(testsupport.FakeDockerMemTotal, "3221225472")
	if got, err := DockerMemoryTotalGiB(); err != nil || got != 3 {
		t.Errorf("total = %v GiB, %v; want 3", got, err)
	}
}

func TestRunDirIsTimestamped(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.OutputDir = t.TempDir()
	runDir, err := CreateTimestampedRunDir(&cfg)
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
