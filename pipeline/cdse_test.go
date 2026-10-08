// catalogueEntry: builds one CDSE catalogue product entry with id, name, dates, online flag and footprint.
// catalogueResponse: returns a catalogue JSON body with two slices of the newest S1D pass and one older S1C slice.
// productZip: builds an in-memory product zip holding a single .SAFE folder with a manifest.
// scriptCDSE: sets test credentials and scripts working CDSE sign-in, catalogue and download responses.
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
// TestPlanFromAreaSearchDownloadsAndUnpacksEverySlice: checks an area search plan downloads and unpacks every slice.
// TestPlanFromProductIDFillsInFromTheName: checks a plan from a product id takes its metadata from the product name.
// TestPlanFromSearchStopsOnAFailedDownload: checks area and product plans fail on a refused download, as does an unknown area.
// TestPlanStopsOnAnArchiveThatWillNotUnpack: checks a downloaded archive that is not a zip fails the plan at unpacking.
// TestReportSearchDetailsOnePassOrSurveysSeveral: checks the search report needs -area, then details one pass or surveys several.

package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	newestSliceSouth = "S1D_IW_GRDH_1SDV_20260907T155920_20260907T155945_004474_0084CD_ED7F.SAFE"
	newestSliceNorth = "S1D_IW_GRDH_1SDV_20260907T155945_20260907T160010_004474_0084CD_AB12.SAFE"
	olderSlice       = "S1C_IW_GRDH_1SDV_20260905T041309_20260905T041334_009300_0125AA_1111.SAFE"
)

func catalogueEntry(id, name, start, end string, online bool, footprint string) map[string]any {
	return map[string]any{
		"Id": id, "Name": name, "Online": online, "Footprint": footprint,
		"ContentDate": map[string]string{"Start": start, "End": end},
	}
}

func catalogueResponse() []byte {
	body, _ := json.Marshal(map[string]any{"value": []any{
		catalogueEntry("id-north", newestSliceNorth, "2026-09-07T15:59:45.000Z", "2026-09-07T16:00:10.000Z", true,
			"geography'SRID=4326;POLYGON ((27.0 42.9, 30.0 42.9, 30.0 44.0, 27.0 44.0, 27.0 42.9))'"),
		catalogueEntry("id-south", newestSliceSouth, "2026-09-07T15:59:20.000Z", "2026-09-07T15:59:45.000Z", false,
			"geography'SRID=4326;POLYGON ((27.0 41.8, 30.0 41.8, 30.0 43.0, 27.0 43.0, 27.0 41.8))'"),
		catalogueEntry("id-older", olderSlice, "2026-09-05T04:13:09.000Z", "2026-09-05T04:13:34.000Z", true,
			"geography'SRID=4326;POLYGON ((27.0 42.0, 28.0 42.0, 28.0 42.5, 27.0 42.5, 27.0 42.0))'"),
	}})
	return body
}

func productZip(t *testing.T, topDir string) []byte {
	t.Helper()
	buffer := &bytes.Buffer{}
	archive := zip.NewWriter(buffer)
	if _, err := archive.Create(topDir + "/"); err != nil {
		t.Fatal(err)
	}
	file, err := archive.Create(topDir + "/manifest.safe")
	if err != nil {
		t.Fatal(err)
	}
	file.Write([]byte("<manifest/>"))
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func scriptCDSE(t *testing.T) *cdseServer {
	t.Helper()
	t.Setenv("CDSE_USERNAME", "test-user")
	t.Setenv("CDSE_PASSWORD", "test-password")

	fake := newCDSEServer(t)
	fake.handle(cdseIdentityHost, func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil || request.PostForm.Get("username") != "test-user" ||
			request.PostForm.Get("client_id") != cdsePublicClientID {
			http.Error(writer, "bad form", http.StatusBadRequest)
			return
		}
		fmt.Fprint(writer, `{"access_token":"token-123"}`)
	})
	fake.handle(cdseCatalogueHost, func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "(") {
			if request.Header.Get("Authorization") != "Bearer token-123" {
				http.Error(writer, "no token", http.StatusUnauthorized)
				return
			}
			fmt.Fprintf(writer, `{"Name":%q}`, newestSliceSouth)
			return
		}
		writer.Write(catalogueResponse())
	})
	fake.handle(cdseDownloadHost, func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer token-123" {
			http.Error(writer, "no token", http.StatusUnauthorized)
			return
		}
		name := newestSliceSouth
		if strings.Contains(request.URL.Path, "id-north") {
			name = newestSliceNorth
		}
		writer.Write(productZip(t, name))
	})
	return fake
}

func TestReadCDSECredentialsNeedsBoth(t *testing.T) {
	t.Setenv("CDSE_USERNAME", "someone")
	t.Setenv("CDSE_PASSWORD", "")
	if _, err := readCDSECredentials(); err == nil {
		t.Error("a username alone was accepted")
	}
	t.Setenv("CDSE_PASSWORD", "secret")
	credentials, err := readCDSECredentials()
	if err != nil || credentials.Username != "someone" || credentials.Password != "secret" {
		t.Errorf("credentials = %+v, %v", credentials, err)
	}
}

func TestSignInReadsTheToken(t *testing.T) {
	scriptCDSE(t)
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
			fake := newCDSEServer(t)
			fake.handle(cdseIdentityHost, func(writer http.ResponseWriter, _ *http.Request) {
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
	scriptCDSE(t)
	name, err := fetchProductName("token-123", "id-south")
	if err != nil || name != newestSliceSouth {
		t.Errorf("name = %q, %v", name, err)
	}
	if _, err := fetchProductName("wrong-token", "id-south"); err == nil ||
		!strings.Contains(err.Error(), "looking up product id-south failed") {
		t.Errorf("err = %v, want the lookup to fail on a bad token", err)
	}
}

func TestFetchProductNameWithoutAName(t *testing.T) {
	fake := newCDSEServer(t)
	fake.handle(cdseCatalogueHost, func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `{"Id":"x"}`)
	})
	if _, err := fetchProductName("t", "x"); err == nil || !strings.Contains(err.Error(), "could not find a name") {
		t.Errorf("err = %v", err)
	}
}

func TestGetWithBearerTokenHonoursATimeout(t *testing.T) {
	fake := newCDSEServer(t)
	fake.handle(cdseCatalogueHost, func(writer http.ResponseWriter, _ *http.Request) {
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
	scriptCDSE(t)
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
	fake := newCDSEServer(t)
	fake.handle(cdseDownloadHost, func(writer http.ResponseWriter, _ *http.Request) {
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
	fake := scriptCDSE(t)
	config := newDefaultConfig()
	config.ScenesDir = t.TempDir()

	name, err := downloadSceneByProductID(&config, "id-south", "")
	if err != nil || name != newestSliceSouth+".zip" {
		t.Fatalf("name = %q, %v", name, err)
	}
	if _, err := os.Stat(filepath.Join(config.ScenesDir, name)); err != nil {
		t.Errorf("nothing was downloaded: %v", err)
	}

	before := len(fake.requests)
	if _, err := downloadSceneByProductID(&config, "id-south", newestSliceSouth); err != nil {
		t.Fatal(err)
	}
	for _, request := range fake.requests[before:] {
		if strings.HasPrefix(request, cdseDownloadHost) {
			t.Errorf("a scene already on disk was downloaded again: %s", request)
		}
	}

	t.Setenv("CDSE_PASSWORD", "")
	if _, err := downloadSceneByProductID(&config, "id-south", ""); err == nil {
		t.Error("a download without credentials went ahead")
	}
}

func TestDownloadSceneStopsWhenSignInOrLookupFails(t *testing.T) {
	t.Setenv("CDSE_USERNAME", "u")
	t.Setenv("CDSE_PASSWORD", "p")
	config := newDefaultConfig()
	config.ScenesDir = t.TempDir()

	fake := newCDSEServer(t)
	fake.handle(cdseIdentityHost, func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "down", http.StatusServiceUnavailable)
	})
	if _, err := downloadSceneByProductID(&config, "id", ""); err == nil {
		t.Error("a failed sign-in was not reported")
	}

	fake.handle(cdseIdentityHost, func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `{"access_token":"t"}`)
	})
	fake.handle(cdseCatalogueHost, func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "gone", http.StatusNotFound)
	})
	if _, err := downloadSceneByProductID(&config, "id", ""); err == nil {
		t.Error("a failed name lookup was not reported")
	}

	fake.handle(cdseDownloadHost, func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "gone", http.StatusNotFound)
	})
	if _, err := downloadSceneByProductID(&config, "id", "known.SAFE"); err == nil {
		t.Error("a failed download was not reported")
	}
}

func TestFindNewestPassOverAreaKeepsOnlyThatPassInTrackOrder(t *testing.T) {
	scriptCDSE(t)
	pass, err := findNewestPassOverArea("bulgaria")
	if err != nil {
		t.Fatal(err)
	}
	if len(pass) != 2 {
		t.Fatalf("%d slices, want the two of the newest pass", len(pass))
	}
	if pass[0].ProductName != newestSliceSouth || pass[1].ProductName != newestSliceNorth {
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
			fake := newCDSEServer(t)
			fake.handle(cdseCatalogueHost, func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(c.status)
				fmt.Fprint(writer, c.body)
			})
			if _, err := findNewestPassOverArea("burgas"); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("newest: err = %v, want %q", err, c.want)
			}
			if _, err := findRecentPassesOverArea("burgas", 2); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("recent: err = %v, want %q", err, c.want)
			}
		})
	}

	newCDSEServer(t)
	if _, err := findNewestPassOverArea("atlantis"); err == nil || !strings.Contains(err.Error(), "bulgaria, burgas") {
		t.Errorf("err = %v, want the known areas listed", err)
	}
	if _, err := findRecentPassesOverArea("atlantis", 3); err == nil {
		t.Error("an unknown area was searched")
	}
}

func TestTheSearchAsksForWhatThePipelineCanUse(t *testing.T) {
	fake := newCDSEServer(t)
	var query map[string][]string
	fake.handle(cdseCatalogueHost, func(writer http.ResponseWriter, request *http.Request) {
		query = request.URL.Query()
		writer.Write(catalogueResponse())
	})
	if _, err := findRecentPassesOverArea("burgas", 3); err != nil {
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
	scriptCDSE(t)
	passes, err := findRecentPassesOverArea("bulgaria", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(passes) != 2 || len(passes[0]) != 2 || len(passes[1]) != 1 {
		t.Fatalf("passes = %d (%v)", len(passes), passes)
	}
	if passes[0][0].ProductName != newestSliceSouth {
		t.Errorf("the newest pass is not first in track order: %s", passes[0][0].ProductName)
	}

	if got := groupIntoPasses(passes[0], 0); len(got) != 0 {
		t.Errorf("asking for no passes returned %d", len(got))
	}
}

func TestPlanFromAreaSearchDownloadsAndUnpacksEverySlice(t *testing.T) {
	scriptCDSE(t)
	config := newDefaultConfig()
	config.ScenesDir = t.TempDir()
	config.SearchAreaName = "bulgaria"

	var plan *PassPlan
	var err error
	output := captureStdout(t, func() { plan, err = buildPassPlan(&config) })
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Slices) != 2 || plan.AreaName != "bulgaria" {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.CoveragePercent < 99 {
		t.Errorf("coverage = %.1f, want the whole area", plan.CoveragePercent)
	}
	first := plan.Slices[0]
	if first.Index != 1 || first.SceneFileName != newestSliceSouth || first.ProductID != "id-south" ||
		first.ArchivePath != filepath.Join(config.ScenesDir, newestSliceSouth+".zip") {
		t.Errorf("first slice = %+v", first)
	}
	if _, err := os.Stat(filepath.Join(config.ScenesDir, newestSliceSouth, "manifest.safe")); err != nil {
		t.Errorf("the slice was not unpacked: %v", err)
	}
	if !strings.Contains(output, "archived rather than online") {
		t.Errorf("an offline slice was not pointed out:\n%s", output)
	}
}

func TestPlanFromProductIDFillsInFromTheName(t *testing.T) {
	scriptCDSE(t)
	config := newDefaultConfig()
	config.ScenesDir = t.TempDir()
	config.CDSEProductID = "id-south"

	plan, err := buildPassPlan(&config)
	if err != nil {
		t.Fatal(err)
	}
	slice := plan.Slices[0]
	if slice.Mission != "S1D" || slice.AbsoluteOrbit != "004474" ||
		!slice.AcquiredAt.Equal(time.Date(2026, 9, 7, 15, 59, 20, 0, time.UTC)) {
		t.Errorf("slice = %+v", slice)
	}
	if plan.CoveragePercent != coverageUnknown {
		t.Errorf("coverage = %v, want unknown", plan.CoveragePercent)
	}
}

func TestPlanFromSearchStopsOnAFailedDownload(t *testing.T) {
	fake := scriptCDSE(t)
	fake.handle(cdseDownloadHost, func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "quota", http.StatusTooManyRequests)
	})
	config := newDefaultConfig()
	config.ScenesDir = t.TempDir()

	config.SearchAreaName = "bulgaria"
	if _, err := buildPassPlan(&config); err == nil {
		t.Error("an area plan survived a failed download")
	}
	config.SearchAreaName = ""
	config.CDSEProductID = "id-south"
	if _, err := buildPassPlan(&config); err == nil {
		t.Error("a product plan survived a failed download")
	}
	config.CDSEProductID = ""
	config.SearchAreaName = "atlantis"
	if _, err := buildPassPlan(&config); err == nil {
		t.Error("an unknown area was planned")
	}
}

func TestPlanStopsOnAnArchiveThatWillNotUnpack(t *testing.T) {
	fake := scriptCDSE(t)
	fake.handle(cdseDownloadHost, func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, "this is not a zip")
	})
	config := newDefaultConfig()
	config.ScenesDir = t.TempDir()
	config.CDSEProductID = "id-south"
	if _, err := buildPassPlan(&config); err == nil || !strings.Contains(err.Error(), "unpacking") {
		t.Errorf("err = %v, want the unpacking failure", err)
	}
}

func TestReportSearchDetailsOnePassOrSurveysSeveral(t *testing.T) {
	scriptCDSE(t)
	config := newDefaultConfig()

	if err := reportSearch(&config); err == nil || !strings.Contains(err.Error(), "needs -area") {
		t.Errorf("err = %v, want -area asked for", err)
	}

	config.SearchAreaName = "bulgaria"
	detail := captureStdout(t, func() {
		if err := reportSearch(&config); err != nil {
			t.Error(err)
		}
	})
	for _, want := range []string{"2 product(s)", "Absolute orbit:   004474", "slice_2", "COMPLETE"} {
		if !strings.Contains(detail, want) {
			t.Errorf("the pass report lacks %q:\n%s", want, detail)
		}
	}

	config.PassesToSurvey = 3
	survey := captureStdout(t, func() {
		if err := reportSearch(&config); err != nil {
			t.Error(err)
		}
	})
	for _, want := range []string{"Recent passes over bulgaria", "ascending", "complete", "partial",
		"1 of 2 passes reach the whole area", "not guaranteed"} {
		if !strings.Contains(survey, want) {
			t.Errorf("the survey lacks %q:\n%s", want, survey)
		}
	}

	config.SearchAreaName = "atlantis"
	if err := reportRecentPasses(&config); err == nil {
		t.Error("an unknown area was surveyed")
	}
	if err := reportNewestPass(&config); err == nil {
		t.Error("an unknown area was reported")
	}
}
