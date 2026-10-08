// TestReadCDSECredentialsNeedsBoth: checks CDSE credentials are refused unless both username and password are set.
// TestSignInReadsTheToken: checks a successful sign-in returns the access token.
// TestSignInFailures: checks a refused sign-in, a non-JSON reply and an empty token are each reported.
// TestFetchProductName: checks a product name is looked up with the token and a bad token makes the lookup fail.
// TestFetchProductNameWithoutAName: checks a catalogue reply with no Name field is reported as an error.
// TestGetWithBearerTokenHonoursATimeout: checks a GET with a timeout returns the body and a malformed URL is refused.
// TestDownloadWritesThroughAPartFile: checks a download yields the served archive with no leftover .part file, and 401 fails.
// failingReader.Read: always fails with a connection reset error.
// TestADroppedDownloadIsReported: checks a body cut short of its Content-Length is reported and not kept under the final name.
// TestAnInterruptedDownloadLeavesNothing: checks a failed body read leaves neither the file nor its .part behind.
// TestDownloadSceneByProductID: checks a scene is downloaded by id, not fetched again if present, and needs credentials.
// TestDownloadSceneStopsWhenSignInOrLookupFails: checks a failed sign-in, name lookup or download stops the scene download.
// TestFindNewestPassOverAreaKeepsOnlyThatPassInTrackOrder: checks only the newest pass's slices are returned, south to north.
// TestCatalogueSearchFailures: checks server errors, bad JSON, empty results and unknown areas fail the searches.
// TestTheSearchAsksForWhatThePipelineCanUse: checks the catalogue query filters, limit and newest-first order.
// TestGroupIntoPassesNewestFirst: checks catalogue slices are grouped into passes newest first and zero passes yields none.

package cdse

import (
	"archive/zip"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"radarpipeline/internal/config"
	"radarpipeline/internal/testsupport"
)

func TestReadCDSECredentialsNeedsBoth(t *testing.T) {
	t.Setenv("CDSE_USERNAME", "someone")
	t.Setenv("CDSE_PASSWORD", "")
	if _, err := ReadCDSECredentials(); err == nil {
		t.Error("a username alone was accepted")
	}
	t.Setenv("CDSE_PASSWORD", "secret")
	credentials, err := ReadCDSECredentials()
	if err != nil || credentials.Username != "someone" || credentials.Password != "secret" {
		t.Errorf("credentials = %+v, %v", credentials, err)
	}
}

func TestSignInReadsTheToken(t *testing.T) {
	testsupport.ScriptCDSE(t)
	token, err := requestCDSEAccessToken(CDSECredentials{Username: "test-user", Password: "test-password"})
	if err != nil || token != "token-123" {
		t.Errorf("token = %q, %v", token, err)
	}
}

func TestSignInFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"refused", http.StatusUnauthorized, `{"error":"invalid_grant"}`, "sign-in returned 401"},
		{"not JSON", http.StatusOK, `<html>`, "could not read the sign-in response"},
		{"no token", http.StatusOK, `{"access_token":""}`, "returned no token"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := testsupport.NewCDSEServer(t)
			fake.Handle(testsupport.CDSEIdentityHost, func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(c.status)
				fmt.Fprint(writer, c.body)
			})
			_, err := requestCDSEAccessToken(CDSECredentials{Username: "u", Password: "p"})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestFetchProductName(t *testing.T) {
	testsupport.ScriptCDSE(t)
	name, err := fetchProductName("token-123", "id-south")
	if err != nil || name != testsupport.NewestSliceSouth {
		t.Errorf("name = %q, %v", name, err)
	}
	if _, err := fetchProductName("wrong-token", "id-south"); err == nil ||
		!strings.Contains(err.Error(), "looking up product id-south failed") {
		t.Errorf("err = %v, want the lookup to fail on a bad token", err)
	}
}

func TestFetchProductNameWithoutAName(t *testing.T) {
	fake := testsupport.NewCDSEServer(t)
	fake.Handle(testsupport.CDSECatalogueHost, func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `{"Id":"x"}`)
	})
	if _, err := fetchProductName("t", "x"); err == nil || !strings.Contains(err.Error(), "could not find a name") {
		t.Errorf("err = %v", err)
	}
}

func TestGetWithBearerTokenHonoursATimeout(t *testing.T) {
	fake := testsupport.NewCDSEServer(t)
	fake.Handle(testsupport.CDSECatalogueHost, func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, "ok")
	})
	body, err := getWithBearerToken(cdseCatalogueURL, "t", time.Minute)
	if err != nil || string(body) != "ok" {
		t.Errorf("body = %q, %v", body, err)
	}
	if _, err := getWithBearerToken("://not a url", "t", 0); err == nil {
		t.Error("a malformed URL was requested")
	}
}

func TestDownloadWritesThroughAPartFile(t *testing.T) {
	testsupport.ScriptCDSE(t)
	destination := filepath.Join(t.TempDir(), "scene.zip")
	if err := downloadProductArchive("token-123", "id-south", destination); err != nil {
		t.Fatal(err)
	}
	if archive, err := zip.OpenReader(destination); err != nil {
		t.Errorf("the download is not the archive that was served: %v", err)
	} else {
		archive.Close()
	}
	if _, err := os.Stat(destination + ".part"); !os.IsNotExist(err) {
		t.Error("the .part file was left behind")
	}

	err := downloadProductArchive("wrong-token", "id-south", filepath.Join(t.TempDir(), "other.zip"))
	if err == nil || !strings.Contains(err.Error(), "download returned 401") {
		t.Errorf("err = %v, want the 401 reported", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestADroppedDownloadIsReported(t *testing.T) {
	fake := testsupport.NewCDSEServer(t)
	fake.Handle(testsupport.CDSEDownloadHost, func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", "1000000")
		writer.Write([]byte("only the start"))
	})
	destination := filepath.Join(t.TempDir(), "scene.zip")
	err := downloadProductArchive("t", "id", destination)
	if err == nil || !strings.Contains(err.Error(), "stopped partway") {
		t.Errorf("err = %v, want the partial download reported", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Error("a partial download was left under the final name")
	}
}

func TestAnInterruptedDownloadLeavesNothing(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "scene.zip")
	if _, err := writeBodyToPartFileThenRename(failingReader{}, destination); err == nil {
		t.Fatal("an interrupted download succeeded")
	}
	for _, path := range []string{destination, destination + ".part"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s exists after a failed download", filepath.Base(path))
		}
	}

	if _, err := writeBodyToPartFileThenRename(strings.NewReader("x"),
		filepath.Join(t.TempDir(), "missing", "scene.zip")); err == nil {
		t.Error("writing into a folder that does not exist succeeded")
	}
}

func TestDownloadSceneByProductID(t *testing.T) {
	fake := testsupport.ScriptCDSE(t)
	cfg := config.NewDefaultConfig()
	cfg.ScenesDir = t.TempDir()

	name, err := DownloadSceneByProductID(&cfg, "id-south", "")
	if err != nil || name != testsupport.NewestSliceSouth+".zip" {
		t.Fatalf("name = %q, %v", name, err)
	}
	if _, err := os.Stat(filepath.Join(cfg.ScenesDir, name)); err != nil {
		t.Errorf("nothing was downloaded: %v", err)
	}

	before := len(fake.Requests)
	if _, err := DownloadSceneByProductID(&cfg, "id-south", testsupport.NewestSliceSouth); err != nil {
		t.Fatal(err)
	}
	for _, request := range fake.Requests[before:] {
		if strings.HasPrefix(request, testsupport.CDSEDownloadHost) {
			t.Errorf("a scene already on disk was downloaded again: %s", request)
		}
	}

	t.Setenv("CDSE_PASSWORD", "")
	if _, err := DownloadSceneByProductID(&cfg, "id-south", ""); err == nil {
		t.Error("a download without credentials went ahead")
	}
}

func TestDownloadSceneStopsWhenSignInOrLookupFails(t *testing.T) {
	t.Setenv("CDSE_USERNAME", "u")
	t.Setenv("CDSE_PASSWORD", "p")
	cfg := config.NewDefaultConfig()
	cfg.ScenesDir = t.TempDir()

	fake := testsupport.NewCDSEServer(t)
	fake.Handle(testsupport.CDSEIdentityHost, func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "down", http.StatusServiceUnavailable)
	})
	if _, err := DownloadSceneByProductID(&cfg, "id", ""); err == nil {
		t.Error("a failed sign-in was not reported")
	}

	fake.Handle(testsupport.CDSEIdentityHost, func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `{"access_token":"t"}`)
	})
	fake.Handle(testsupport.CDSECatalogueHost, func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "gone", http.StatusNotFound)
	})
	if _, err := DownloadSceneByProductID(&cfg, "id", ""); err == nil {
		t.Error("a failed name lookup was not reported")
	}

	fake.Handle(testsupport.CDSEDownloadHost, func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "gone", http.StatusNotFound)
	})
	if _, err := DownloadSceneByProductID(&cfg, "id", "known.SAFE"); err == nil {
		t.Error("a failed download was not reported")
	}
}

func TestFindNewestPassOverAreaKeepsOnlyThatPassInTrackOrder(t *testing.T) {
	testsupport.ScriptCDSE(t)
	pass, err := FindNewestPassOverArea("bulgaria")
	if err != nil {
		t.Fatal(err)
	}
	if len(pass) != 2 {
		t.Fatalf("%d slices, want the two of the newest pass", len(pass))
	}
	if pass[0].ProductName != testsupport.NewestSliceSouth || pass[1].ProductName != testsupport.NewestSliceNorth {
		t.Errorf("order = %s, %s; want along track", pass[0].ProductName, pass[1].ProductName)
	}
	south := pass[0]
	if south.MissionPrefix != "S1D" || south.AbsoluteOrbit != "004474" || south.IsOnline {
		t.Errorf("parsed slice = %+v", south)
	}
	if south.Extent.LatMin != 41.8 || south.EndsAt.IsZero() {
		t.Errorf("extent %v, ends %s", south.Extent, south.EndsAt)
	}
}

func TestCatalogueSearchFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"server error", http.StatusInternalServerError, "boom", "catalogue search returned 500"},
		{"not JSON", http.StatusOK, "<html>", "could not read the search results"},
		{"nothing found", http.StatusOK, `{"value":[]}`, "the footprint is probably wrong"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := testsupport.NewCDSEServer(t)
			fake.Handle(testsupport.CDSECatalogueHost, func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(c.status)
				fmt.Fprint(writer, c.body)
			})
			if _, err := FindNewestPassOverArea("burgas"); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("newest: err = %v, want %q", err, c.want)
			}
			if _, err := FindRecentPassesOverArea("burgas", 2); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("recent: err = %v, want %q", err, c.want)
			}
		})
	}

	testsupport.NewCDSEServer(t)
	if _, err := FindNewestPassOverArea("atlantis"); err == nil || !strings.Contains(err.Error(), "bulgaria, burgas") {
		t.Errorf("err = %v, want the known areas listed", err)
	}
	if _, err := FindRecentPassesOverArea("atlantis", 3); err == nil {
		t.Error("an unknown area was searched")
	}
}

func TestTheSearchAsksForWhatThePipelineCanUse(t *testing.T) {
	fake := testsupport.NewCDSEServer(t)
	var query map[string][]string
	fake.Handle(testsupport.CDSECatalogueHost, func(writer http.ResponseWriter, request *http.Request) {
		query = request.URL.Query()
		writer.Write(testsupport.CatalogueResponse())
	})
	if _, err := FindRecentPassesOverArea("burgas", 3); err != nil {
		t.Fatal(err)
	}
	filter := strings.Join(query["$filter"], "")
	for _, want := range []string{"SENTINEL-1", "IW_GRDH", "not contains(Name,'_COG')", burgasCoastlineFootprint} {
		if !strings.Contains(filter, want) {
			t.Errorf("filter %q lacks %q", filter, want)
		}
	}
	if top := strings.Join(query["$top"], ""); top != fmt.Sprint(3*catalogueSlicesPerPassGuess) {
		t.Errorf("$top = %s", top)
	}
	if order := strings.Join(query["$orderby"], ""); order != "ContentDate/Start desc" {
		t.Errorf("$orderby = %s, want newest first", order)
	}
}

func TestGroupIntoPassesNewestFirst(t *testing.T) {
	testsupport.ScriptCDSE(t)
	passes, err := FindRecentPassesOverArea("bulgaria", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(passes) != 2 || len(passes[0]) != 2 || len(passes[1]) != 1 {
		t.Fatalf("passes = %d (%v)", len(passes), passes)
	}
	if passes[0][0].ProductName != testsupport.NewestSliceSouth {
		t.Errorf("the newest pass is not first in track order: %s", passes[0][0].ProductName)
	}

	if got := groupIntoPasses(passes[0], 0); len(got) != 0 {
		t.Errorf("asking for no passes returned %d", len(got))
	}
}
