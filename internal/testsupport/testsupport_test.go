// TestMain: runs the package tests with the fake docker and explorer first on PATH.
// fakeTool: runs the fake tool in-process with the given arguments and returns what it printed and its exit code.
// TestTheFakesAreFirstOnPath: checks a docker and explorer started by name are the logging fakes.
// TestFakeDockerAnswersVersionInfoAndKill: checks the fake docker's version, info, kill and unknown commands.
// TestFakeDockerRunsTheGDALTools: checks the fake gdal_translate and gdal2tiles write, fail or stay empty as told.
// TestFakeDockerRunsSNAP: checks the fake SNAP writes its output, copies scripted products, sleeps and fails as told.
// TestScriptCDSEAnswersLikeTheService: checks the scripted CDSE sign-in, lookup, search and download, with and without a token.
// TestFileHelpersAndFixtures: checks the text, stdout, product, shapefile and error helpers write what they promise.

package testsupport

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"radarpipeline/internal/config"
)

func TestMain(m *testing.M) { Main(m) }

func fakeTool(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var code int
	output := CaptureStdout(t, func() { code = runFakeTool(args) })
	return output, code
}

func TestTheFakesAreFirstOnPath(t *testing.T) {
	calls := LogFakeTools(t)
	if output, err := exec.Command("docker", "version").Output(); err != nil || !strings.Contains(string(output), "fake") {
		t.Fatalf("docker version = %q, %v; want the fake", output, err)
	}
	if err := exec.Command("explorer", "somewhere").Run(); err != nil {
		t.Fatalf("the fake explorer failed: %v", err)
	}
	got := calls()
	if len(got) != 2 || got[0] != "docker version" || got[1] != "explorer somewhere" {
		t.Errorf("logged calls = %q", got)
	}
}

func TestFakeDockerAnswersVersionInfoAndKill(t *testing.T) {
	if output, code := fakeTool(t, "docker", "version"); code != 0 || !strings.Contains(output, "27.0.0-fake") {
		t.Errorf("version = %q, %d", output, code)
	}
	t.Setenv(FakeDockerDown, "1")
	if _, code := fakeTool(t, "docker", "version"); code != 1 {
		t.Errorf("a down docker answered version with %d", code)
	}
	if _, code := fakeTool(t, "docker", "info"); code != 1 {
		t.Errorf("info without a memory total exited %d", code)
	}
	t.Setenv(FakeDockerMemTotal, "17179869184")
	if output, code := fakeTool(t, "docker", "info"); code != 0 || strings.TrimSpace(output) != "17179869184" {
		t.Errorf("info = %q, %d", output, code)
	}
	if _, code := fakeTool(t, "docker", "kill", "container"); code != 0 {
		t.Errorf("kill exited %d", code)
	}
	if _, code := fakeTool(t, "docker", "ps"); code != 2 {
		t.Errorf("an unknown command exited %d", code)
	}
	if _, code := fakeTool(t, "docker"); code != 0 {
		t.Errorf("docker with no arguments exited %d", code)
	}
	if _, code := fakeTool(t, "explorer.exe", "/select,x"); code != 0 {
		t.Errorf("explorer exited %d", code)
	}
}

func TestFakeDockerRunsTheGDALTools(t *testing.T) {
	host := t.TempDir()
	mount := host + ":/work"

	if _, code := fakeTool(t, "docker", "run", "-v", mount, "--entrypoint", "gdal_translate", "image", "in", "/work/out.tif"); code != 0 {
		t.Fatalf("gdal_translate exited %d", code)
	}
	if _, err := os.Stat(filepath.Join(host, "out.tif")); err != nil {
		t.Errorf("gdal_translate wrote nothing: %v", err)
	}
	if _, code := fakeTool(t, "docker", "run", "-v", mount, "--entrypoint", "gdal_translate", "image", "in", "/elsewhere/out.tif"); code != 3 {
		t.Errorf("a path outside every mount exited %d", code)
	}

	if _, code := fakeTool(t, "docker", "run", "-v", mount, "--entrypoint", "gdal2tiles.py", "image", "in", "/work/tiles"); code != 0 {
		t.Fatalf("gdal2tiles exited %d", code)
	}
	if _, err := os.Stat(filepath.Join(host, "tiles", "12", "2368", "1509.webp")); err != nil {
		t.Errorf("gdal2tiles wrote no tiles: %v", err)
	}

	t.Setenv(FakeTilesBehaviour, "empty")
	if _, code := fakeTool(t, "docker", "run", "-v", mount, "--entrypoint", "gdal2tiles.py", "image", "in", "/work/empty"); code != 0 {
		t.Errorf("an empty gdal2tiles exited %d", code)
	}
	if entries, err := os.ReadDir(filepath.Join(host, "empty")); err != nil || len(entries) != 0 {
		t.Errorf("an empty run left %d entries, %v", len(entries), err)
	}
	t.Setenv(FakeTilesBehaviour, "fail")
	if _, code := fakeTool(t, "docker", "run", "-v", mount, "--entrypoint", "gdal2tiles.py", "image", "in", "/work/x"); code != 1 {
		t.Errorf("a failing gdal2tiles exited %d", code)
	}
	t.Setenv(FakeTilesBehaviour, "fail-translate")
	if _, code := fakeTool(t, "docker", "run", "-v", mount, "--entrypoint", "gdal_translate", "image", "in", "/work/x.tif"); code != 1 {
		t.Errorf("a failing gdal_translate exited %d", code)
	}
}

func TestFakeDockerRunsSNAP(t *testing.T) {
	out := t.TempDir()
	mount := out + ":" + config.ContainerOutputDir
	snap := []string{"docker", "run", "-v", mount, config.SnapImageDigest, "-Pinput=/scenes/x", "-Poutput=" + config.ContainerOutputDir + "/result.dim"}

	if _, code := fakeTool(t, snap...); code != 0 {
		t.Fatalf("SNAP exited %d", code)
	}
	if _, err := os.Stat(filepath.Join(out, "result.dim")); err != nil {
		t.Errorf("SNAP wrote no product: %v", err)
	}
	if _, code := fakeTool(t, "docker", "run", "-v", mount, config.SnapImageDigest); code != 0 {
		t.Errorf("SNAP without an output exited %d", code)
	}

	products := t.TempDir()
	WriteText(t, filepath.Join(products, "ships.data", "band.img"), "bytes")
	t.Setenv(FakeSnapProducts, products)
	if _, code := fakeTool(t, snap...); code != 0 {
		t.Fatalf("SNAP with scripted products exited %d", code)
	}
	if _, err := os.Stat(filepath.Join(out, "ships.data", "band.img")); err != nil {
		t.Errorf("the scripted products were not copied: %v", err)
	}
	t.Setenv(FakeSnapProducts, filepath.Join(products, "missing"))
	if _, code := fakeTool(t, snap...); code != 3 {
		t.Errorf("missing scripted products exited %d", code)
	}

	t.Setenv(FakeSnapSleep, "1")
	t.Setenv(FakeSnapExitCode, "4")
	if output, code := fakeTool(t, snap...); code != 4 || !strings.Contains(output, "went wrong") {
		t.Errorf("a failing SNAP = %q, %d", output, code)
	}
	if _, code := fakeTool(t, "docker", "run", "-v", mount, "other-image"); code != 2 {
		t.Errorf("an unrecognised run exited %d", code)
	}
}

func TestScriptCDSEAnswersLikeTheService(t *testing.T) {
	fake := ScriptCDSE(t)

	response, err := http.PostForm("https://"+CDSEIdentityHost+"/token",
		url.Values{"username": {"test-user"}, "client_id": {"cdse-public"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if !strings.Contains(string(body), "token-123") {
		t.Errorf("sign-in = %q", body)
	}
	response, err = http.PostForm("https://"+CDSEIdentityHost+"/token", url.Values{"username": {"someone"}})
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Errorf("a wrong user got %d", response.StatusCode)
	}

	get := func(address string, token bool) (int, []byte) {
		request, _ := http.NewRequest(http.MethodGet, address, nil)
		if token {
			request.Header.Set("Authorization", "Bearer token-123")
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return response.StatusCode, body
	}

	if status, body := get("https://"+CDSECatalogueHost+"/odata/v1/Products(id-south)", true); status != http.StatusOK ||
		!strings.Contains(string(body), NewestSliceSouth) {
		t.Errorf("lookup = %d %q", status, body)
	}
	if status, _ := get("https://"+CDSECatalogueHost+"/odata/v1/Products(id-south)", false); status != http.StatusUnauthorized {
		t.Errorf("a lookup without a token got %d", status)
	}
	if _, body := get("https://"+CDSECatalogueHost+"/odata/v1/Products", false); !bytes.Equal(body, CatalogueResponse()) {
		t.Errorf("search = %q", body)
	}
	status, archive := get("https://"+CDSEDownloadHost+"/odata/v1/Products(id-north)/$value", true)
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if status != http.StatusOK || err != nil || len(reader.File) != 2 || !strings.HasPrefix(reader.File[0].Name, NewestSliceNorth) {
		t.Errorf("download = %d, %v", status, err)
	}
	if status, _ := get("https://"+CDSEDownloadHost+"/odata/v1/Products(id-north)/$value", false); status != http.StatusUnauthorized {
		t.Errorf("a download without a token got %d", status)
	}
	if len(fake.Requests) != 7 || fake.Requests[0] != CDSEIdentityHost+"/token" {
		t.Errorf("requests = %q", fake.Requests)
	}
}

func TestFileHelpersAndFixtures(t *testing.T) {
	dir := t.TempDir()

	if said := CaptureStdout(t, func() { os.Stdout.WriteString("hello") }); said != "hello" {
		t.Errorf("captured %q", said)
	}

	sea := WriteSeaProduct(t, dir, "sea")
	if info, err := os.Stat(filepath.Join(dir, "sea.data", "Sigma0_VH.img")); err != nil || info.Size() != SwathCols*SwathRows*4 {
		t.Errorf("sea band: %v", err)
	}
	if header, err := os.ReadFile(sea); err != nil || !strings.Contains(string(header), "<NCOLS>40</NCOLS>") {
		t.Errorf("sea header: %v", err)
	}

	WriteShipsProduct(t, dir, "ships", []string{"target_000\t1\t1\t43\t28\t20\t60"})
	if list, err := os.ReadFile(filepath.Join(dir, "ships.data", "vector_data", "ShipDetections.csv")); err != nil ||
		!strings.Contains(string(list), "target_000") {
		t.Errorf("ships list: %v", err)
	}

	shapefile := filepath.Join(dir, "coast.shp")
	WriteTestShapefile(t, shapefile, [][][2]float64{SquareRing(1, 2, 3, 4)})
	if info, err := os.Stat(shapefile); err != nil || info.Size() != 100+8+44+4+4*16 {
		t.Errorf("shapefile: %v", err)
	}
	if ring := SquareRing(1, 2, 3, 4); ring[2] != [2]float64{3, 4} {
		t.Errorf("ring = %v", ring)
	}
	if CoastFarAway[0] != [2]float64{27.5, 42.0} || len(CoastInTheSwath) != 4 {
		t.Error("the coastline fixtures moved")
	}

	if ErrFake("boom").Error() != "boom" {
		t.Error("ErrFake lost its text")
	}
}
