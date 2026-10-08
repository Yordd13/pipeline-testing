// ensureDockerIsRunning: fails when the Docker server does not answer a version query.
// runGraph: runs one SNAP graph in a container, waits for it and returns the host path of its output.
// preprocessRun: describes the stage 1 preprocessing run that reads the scene from the mounted scenes folder.
// detectionRun: describes the stage 2 detection run that reads stage 1's product from the output folder.
// createTimestampedRunDir: creates an output subfolder named after the current UTC time and returns its path.
// newUniqueContainerName: returns a container name built from the current time in nanoseconds.
// startSnapContainer: starts the SNAP docker run command with its output going to the console.
// waitForContainerToFinish: waits for the container, killing it on timeout or interrupt, and returns why it ended.
// interpretSnapExitError: turns a SNAP exit error into a readable message, treating exit 137 as out of memory.
// buildDockerRunArgs: builds the docker run arguments, with mounts, memory limit, heap and graph parameters.
// dockerMemoryTotalGiB: asks Docker for its total memory and returns it in GiB.

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	exitCodeKilledBySignal   = 137
	runDirTimestampLayout    = "20060102T150405Z"
	dockerVersionQueryFormat = "{{.Server.Version}}"
	dockerMemoryQueryFormat  = "{{.MemTotal}}"
	bytesPerGiB              = 1024 * 1024 * 1024
)

func ensureDockerIsRunning() error {
	output, err := exec.Command("docker", "version", "--format", dockerVersionQueryFormat).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Docker is not available - start Docker Desktop and try again\n%s",
			strings.TrimSpace(string(output)))
	}
	return nil
}

type GraphRun struct {
	Description string

	GraphHostPath      string
	GraphContainerPath string

	InputContainerPath string
	OutputFileName     string
}

func runGraph(ctx context.Context, config *Config, run GraphRun, runDir string) (resultPath string, err error) {
	fmt.Printf("\n== %s ==\n", run.Description)
	fmt.Printf("Graph: %s\n", run.GraphHostPath)

	containerName := newUniqueContainerName()
	dockerCommand, err := startSnapContainer(config, run, runDir, containerName)
	if err != nil {
		return "", err
	}

	if err := waitForContainerToFinish(ctx, dockerCommand, containerName, config.ProcessingTimeoutMinutes); err != nil {
		return "", err
	}
	return filepath.Join(runDir, run.OutputFileName), nil
}

func preprocessRun(config *Config, sceneFileName string) GraphRun {
	return GraphRun{
		Description:        "Stage 1 of 2: preprocessing",
		GraphHostPath:      config.GraphFilePath(),
		GraphContainerPath: containerGraphsDir + "/" + config.GraphFileName,
		InputContainerPath: containerScenesDir + "/" + sceneFileName,
		OutputFileName:     config.ResultFileName,
	}
}

func detectionRun(config *Config) GraphRun {
	return GraphRun{
		Description:        "Stage 2 of 2: ship detection",
		GraphHostPath:      config.DetectionGraphFilePath(),
		GraphContainerPath: containerGraphsDir + "/" + config.DetectionGraphFileName,
		InputContainerPath: containerOutputDir + "/" + config.ResultFileName,
		OutputFileName:     config.DetectionResultFileName,
	}
}

func createTimestampedRunDir(config *Config) (string, error) {
	runDir := filepath.Join(config.OutputDir, time.Now().UTC().Format(runDirTimestampLayout))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create the run folder: %w", err)
	}
	return runDir, nil
}

func newUniqueContainerName() string {
	return fmt.Sprintf("snap-%d", time.Now().UnixNano())
}

func startSnapContainer(config *Config, run GraphRun, runDir, containerName string) (*exec.Cmd, error) {
	dockerArgs := buildDockerRunArgs(config, run, runDir, containerName)

	dockerCommand := exec.Command("docker", dockerArgs...)
	dockerCommand.Stdout = os.Stdout
	dockerCommand.Stderr = os.Stderr

	if err := dockerCommand.Start(); err != nil {
		return nil, fmt.Errorf("could not start docker: %w", err)
	}
	return dockerCommand, nil
}

func waitForContainerToFinish(ctx context.Context, dockerCommand *exec.Cmd, containerName string, timeoutMinutes int) error {
	commandFinished := make(chan error, 1)
	go func() { commandFinished <- dockerCommand.Wait() }()

	select {
	case err := <-commandFinished:
		return interpretSnapExitError(err)

	case <-ctx.Done():
		exec.Command("docker", "kill", containerName).Run()
		<-commandFinished

		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("gave up after %d minutes and stopped the container; "+
				"raise -timeout if the scene simply needs longer", timeoutMinutes)
		}
		return errors.New("interrupted; the container was stopped")
	}
}

func interpretSnapExitError(err error) error {
	if err == nil {
		return nil
	}

	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		switch exitError.ExitCode() {
		case exitCodeKilledBySignal:
			return errors.New("SNAP ran out of memory: lower -heap, or give Docker " +
				"more memory in its settings")
		default:
			return fmt.Errorf("SNAP failed with exit code %d (see the messages above)",
				exitError.ExitCode())
		}
	}
	return fmt.Errorf("running SNAP: %w", err)
}

func buildDockerRunArgs(config *Config, run GraphRun, runDir, containerName string) []string {
	return []string{
		"run", "--rm", "--name", containerName,

		"-v", absolutePath(config.ScenesDir) + ":" + containerScenesDir,
		"-v", absolutePath(config.GraphsDir) + ":" + containerGraphsDir,
		"-v", absolutePath(runDir) + ":" + containerOutputDir,
		"-v", absolutePath(config.AuxDataDir) + ":" + containerSnapHomeDir,

		"--memory", config.ContainerMemoryLimit(),

		snapImageDigest,
		gptExecutablePath,
		run.GraphContainerPath,

		fmt.Sprintf("-J-Xmx%dG", config.SnapHeapGB),
		fmt.Sprintf("-P%s=%s",
			config.GraphInputParamName, run.InputContainerPath),
		fmt.Sprintf("-P%s=%s/%s",
			config.GraphOutputParamName, containerOutputDir, run.OutputFileName),
	}
}

func dockerMemoryTotalGiB() (float64, error) {
	output, err := exec.Command("docker", "info", "--format", dockerMemoryQueryFormat).Output()
	if err != nil {
		return 0, err
	}

	totalBytes, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		return 0, err
	}
	return float64(totalBytes) / bytesPerGiB, nil
}
