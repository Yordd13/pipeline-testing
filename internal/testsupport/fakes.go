// Main: acts as a fake tool when re-invoked, otherwise puts fake docker and explorer first on PATH and runs the tests.
// installFakeTools: copies the test binary into the folder as docker and explorer executables.
// exeSuffix: returns ".exe" on Windows and an empty string elsewhere.
// runFakeTool: logs the call and acts out docker version, info, kill or run, or a silent explorer.
// fakeDockerRun: imitates docker run for gdal_translate, gdal2tiles and SNAP, writing outputs into the mounted host folders.
// contains: reports whether the slice holds the wanted string.
// writeOrFail: writes the file, creating its folder, and returns a non-zero exit code on failure.
// copyTree: copies every file and folder under source into destination.
// LogFakeTools: points the fake tools at a fresh log file and returns a function reading the logged calls.

package testsupport

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"radarpipeline/internal/config"
)

const (
	fakeToolMarker = "PIPELINE_FAKE_TOOL"

	fakeToolLog        = "FAKE_TOOL_LOG"
	FakeDockerDown     = "FAKE_DOCKER_DOWN"
	FakeDockerMemTotal = "FAKE_DOCKER_MEMTOTAL"
	FakeSnapExitCode   = "FAKE_SNAP_EXIT"
	FakeSnapSleep      = "FAKE_SNAP_SLEEP_MS"
	FakeSnapProducts   = "FAKE_SNAP_PRODUCTS"
	FakeTilesBehaviour = "FAKE_TILES"
)

func Main(m *testing.M) {
	if os.Getenv(fakeToolMarker) == "1" {
		os.Exit(runFakeTool(os.Args))
	}

	fakeBin, err := os.MkdirTemp("", "pipeline-fake-tools-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot create the fake tool folder:", err)
		os.Exit(1)
	}
	if err := installFakeTools(fakeBin); err != nil {
		os.RemoveAll(fakeBin)
		fmt.Fprintln(os.Stderr, "cannot install the fake tools:", err)
		os.Exit(1)
	}
	os.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	os.Setenv(fakeToolMarker, "1")

	code := m.Run()
	os.RemoveAll(fakeBin)
	os.Exit(code)
}

func installFakeTools(dir string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	contents, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	for _, name := range []string{"docker", "explorer"} {
		if err := os.WriteFile(filepath.Join(dir, name+exeSuffix()), contents, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func exeSuffix() string {
	if filepath.Separator == '\\' {
		return ".exe"
	}
	return ""
}

func runFakeTool(args []string) int {
	tool := strings.ToLower(strings.TrimSuffix(filepath.Base(args[0]), filepath.Ext(args[0])))
	arguments := args[1:]

	if logPath := os.Getenv(fakeToolLog); logPath != "" {
		if file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(file, "%s %s\n", tool, strings.Join(arguments, " "))
			file.Close()
		}
	}

	if tool != "docker" || len(arguments) == 0 {
		return 0
	}

	switch arguments[0] {
	case "version":
		if os.Getenv(FakeDockerDown) == "1" {
			fmt.Println("Cannot connect to the Docker daemon")
			return 1
		}
		fmt.Println("27.0.0-fake")
		return 0
	case "info":
		total := os.Getenv(FakeDockerMemTotal)
		if total == "" {
			return 1
		}
		fmt.Println(total)
		return 0
	case "kill":
		return 0
	case "run":
		return fakeDockerRun(arguments[1:])
	}
	fmt.Println("fake docker: unexpected command", arguments[0])
	return 2
}

func fakeDockerRun(arguments []string) int {
	mounts := map[string]string{}
	entrypoint := ""
	for at := 0; at < len(arguments)-1; at++ {
		switch arguments[at] {
		case "-v":
			mount := arguments[at+1]
			split := strings.LastIndex(mount, ":/")
			mounts[mount[split+1:]] = mount[:split]
		case "--entrypoint":
			entrypoint = arguments[at+1]
		}
	}
	toHost := func(containerPath string) string {
		for container, host := range mounts {
			if strings.HasPrefix(containerPath, container+"/") {
				return filepath.Join(host, filepath.FromSlash(strings.TrimPrefix(containerPath, container+"/")))
			}
		}
		return ""
	}
	last := arguments[len(arguments)-1]

	switch {
	case entrypoint == "gdal_translate":
		if os.Getenv(FakeTilesBehaviour) == "fail-translate" {
			fmt.Println("ERROR 4: source.img not recognised")
			return 1
		}
		return writeOrFail(toHost(last), "II* fake tiff")

	case entrypoint == "gdal2tiles.py":
		root := toHost(last)
		switch os.Getenv(FakeTilesBehaviour) {
		case "fail":
			fmt.Println("gdal2tiles exploded")
			return 1
		case "empty":
			os.MkdirAll(root, 0o755)
			return 0
		}
		for _, tile := range []string{"7/80/50.webp", "8/160/100.webp", "12/2368/1509.webp"} {
			if code := writeOrFail(filepath.Join(root, filepath.FromSlash(tile)), "tile"); code != 0 {
				return code
			}
		}
		return 0

	case contains(arguments, config.SnapImageDigest):
		if wait, _ := strconv.Atoi(os.Getenv(FakeSnapSleep)); wait > 0 {
			time.Sleep(time.Duration(wait) * time.Millisecond)
		}
		if code, _ := strconv.Atoi(os.Getenv(FakeSnapExitCode)); code != 0 {
			fmt.Println("SNAP: something went wrong")
			return code
		}
		outDir := mounts[config.ContainerOutputDir]
		if products := os.Getenv(FakeSnapProducts); products != "" {
			if err := copyTree(products, outDir); err != nil {
				fmt.Println("fake SNAP:", err)
				return 3
			}
			return 0
		}
		for _, argument := range arguments {
			if output, ok := strings.CutPrefix(argument, "-Poutput="); ok {
				return writeOrFail(toHost(output), "fake product")
			}
		}
		return 0
	}
	fmt.Println("fake docker: unrecognised run", strings.Join(arguments, " "))
	return 2
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func writeOrFail(path, contents string) int {
	if path == "" {
		fmt.Println("fake docker: path outside every mount")
		return 3
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Println(err)
		return 3
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		fmt.Println(err)
		return 3
	}
	return 0
}

func copyTree(source, destination string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, 0o644)
	})
}

func LogFakeTools(t *testing.T) func() []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tools.log")
	t.Setenv(fakeToolLog, path)
	return func() []string {
		contents, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		return strings.Split(strings.TrimSpace(string(contents)), "\n")
	}
}
