// TestMain: acts as a fake tool when re-invoked, otherwise puts fake docker and explorer first on PATH and runs the tests.
// installFakeTools: copies the test binary into the folder as docker and explorer executables.
// exeSuffix: returns ".exe" on Windows and an empty string elsewhere.
// runFakeTool: logs the call and acts out docker version, info, kill or run, or a silent explorer.
// fakeDockerRun: imitates docker run for gdal_translate, gdal2tiles and SNAP, writing outputs into the mounted host folders.
// contains: reports whether the slice holds the wanted string.
// writeOrFail: writes the file, creating its folder, and returns a non-zero exit code on failure.
// copyTree: copies every file and folder under source into destination.
// logFakeTools: points the fake tools at a fresh log file and returns a function reading the logged calls.
// TestTheFakeToolsAreWhatRuns: checks the docker and explorer that run during tests are the fakes.
// newCDSEServer: starts a test server and routes default HTTP transport requests for CDSE hosts to it, failing others.
// cdseServer.handle: registers the handler for requests meant for the given CDSE host.
// cdseServer.serve: records each request and dispatches it to its host's handler, failing on unscripted hosts.
// roundTripFunc.RoundTrip: calls the function to perform the HTTP round trip.
// writeText: writes a small text file, creating its folder first.
// captureStdout: runs the function and returns what it printed to standard output.

package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	fakeToolMarker = "PIPELINE_FAKE_TOOL"

	fakeToolLog        = "FAKE_TOOL_LOG"
	fakeDockerDown     = "FAKE_DOCKER_DOWN"
	fakeDockerMemTotal = "FAKE_DOCKER_MEMTOTAL"
	fakeSnapExitCode   = "FAKE_SNAP_EXIT"
	fakeSnapSleep      = "FAKE_SNAP_SLEEP_MS"
	fakeSnapProducts   = "FAKE_SNAP_PRODUCTS"
	fakeTilesBehaviour = "FAKE_TILES"
)

func TestMain(m *testing.M) {
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
		if os.Getenv(fakeDockerDown) == "1" {
			fmt.Println("Cannot connect to the Docker daemon")
			return 1
		}
		fmt.Println("27.0.0-fake")
		return 0
	case "info":
		total := os.Getenv(fakeDockerMemTotal)
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
		if os.Getenv(fakeTilesBehaviour) == "fail-translate" {
			fmt.Println("ERROR 4: source.img not recognised")
			return 1
		}
		return writeOrFail(toHost(last), "II* fake tiff")

	case entrypoint == "gdal2tiles.py":
		root := toHost(last)
		switch os.Getenv(fakeTilesBehaviour) {
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

	case contains(arguments, snapImageDigest):
		if wait, _ := strconv.Atoi(os.Getenv(fakeSnapSleep)); wait > 0 {
			time.Sleep(time.Duration(wait) * time.Millisecond)
		}
		if code, _ := strconv.Atoi(os.Getenv(fakeSnapExitCode)); code != 0 {
			fmt.Println("SNAP: something went wrong")
			return code
		}
		outDir := mounts[containerOutputDir]
		if products := os.Getenv(fakeSnapProducts); products != "" {
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

func logFakeTools(t *testing.T) func() []string {
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

func TestTheFakeToolsAreWhatRuns(t *testing.T) {
	calls := logFakeTools(t)

	if err := ensureDockerIsRunning(); err != nil {
		t.Fatalf("the fake docker said it was not running: %v", err)
	}
	config := newDefaultConfig()
	config.OpenResultFolderWhenDone = true
	openResultFolderIfRequested(&config, filepath.Join(t.TempDir(), "x.dim"))

	got := calls()
	if len(got) != 2 || !strings.HasPrefix(got[0], "docker version") {
		t.Fatalf("the fake docker was not the one that ran; it logged %q", got)
	}
	if !strings.HasPrefix(got[1], "explorer /select,") {
		t.Errorf("the fake explorer was not the one that ran; it logged %q", got[1])
	}
}

type cdseServer struct {
	t        *testing.T
	server   *httptest.Server
	handlers map[string]http.HandlerFunc
	requests []string
}

const (
	cdseIdentityHost  = "identity.dataspace.copernicus.eu"
	cdseCatalogueHost = "catalogue.dataspace.copernicus.eu"
	cdseDownloadHost  = "download.dataspace.copernicus.eu"
)

func newCDSEServer(t *testing.T) *cdseServer {
	t.Helper()
	fake := &cdseServer{t: t, handlers: map[string]http.HandlerFunc{}}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.server.Close)

	target, err := url.Parse(fake.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	inner := &http.Transport{}
	t.Cleanup(inner.CloseIdleConnections)

	original := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case cdseIdentityHost, cdseCatalogueHost, cdseDownloadHost:
		default:
			t.Errorf("a request left for %s, which is not a CDSE host", request.URL.Host)
			return nil, fmt.Errorf("blocked request to %s", request.URL.Host)
		}
		rewritten := request.Clone(request.Context())
		rewritten.Header.Set("X-Original-Host", request.URL.Host)
		rewritten.URL.Scheme = target.Scheme
		rewritten.URL.Host = target.Host
		rewritten.Host = target.Host
		return inner.RoundTrip(rewritten)
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	return fake
}

func (f *cdseServer) handle(host string, handler http.HandlerFunc) {
	f.handlers[host] = handler
}

func (f *cdseServer) serve(writer http.ResponseWriter, request *http.Request) {
	host := request.Header.Get("X-Original-Host")
	f.requests = append(f.requests, host+request.URL.Path)
	handler, scripted := f.handlers[host]
	if !scripted {
		f.t.Errorf("unexpected CDSE request to %s%s", host, request.URL.Path)
		http.Error(writer, "not scripted", http.StatusTeapot)
		return
	}
	handler(writer, request)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func writeText(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	done := make(chan string)
	go func() {
		contents, _ := io.ReadAll(reader)
		done <- string(contents)
	}()
	defer func() { os.Stdout = original }()
	fn()
	writer.Close()
	os.Stdout = original
	return <-done
}
