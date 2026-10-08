// catalogueEntry: builds one CDSE catalogue product entry with id, name, dates, online flag and footprint.
// CatalogueResponse: returns a catalogue JSON body with two slices of the newest S1D pass and one older S1C slice.
// ProductZip: builds an in-memory product zip holding a single .SAFE folder with a manifest.
// ScriptCDSE: sets test credentials and scripts working CDSE sign-in, catalogue and download responses.
// NewCDSEServer: starts a test server and routes default HTTP transport requests for CDSE hosts to it, failing others.
// CDSEServer.Handle: registers the handler for requests meant for the given CDSE host.
// CDSEServer.serve: records each request and dispatches it to its host's handler, failing on unscripted hosts.
// roundTripFunc.RoundTrip: calls the function to perform the HTTP round trip.

package testsupport

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const (
	NewestSliceSouth = "S1D_IW_GRDH_1SDV_20260907T155920_20260907T155945_004474_0084CD_ED7F.SAFE"
	NewestSliceNorth = "S1D_IW_GRDH_1SDV_20260907T155945_20260907T160010_004474_0084CD_AB12.SAFE"
	olderSlice       = "S1C_IW_GRDH_1SDV_20260905T041309_20260905T041334_009300_0125AA_1111.SAFE"
)

func catalogueEntry(id, name, start, end string, online bool, footprint string) map[string]any {
	return map[string]any{
		"Id": id, "Name": name, "Online": online, "Footprint": footprint,
		"ContentDate": map[string]string{"Start": start, "End": end},
	}
}

func CatalogueResponse() []byte {
	body, _ := json.Marshal(map[string]any{"value": []any{
		catalogueEntry("id-north", NewestSliceNorth, "2026-09-07T15:59:45.000Z", "2026-09-07T16:00:10.000Z", true,
			"geography'SRID=4326;POLYGON ((27.0 42.9, 30.0 42.9, 30.0 44.0, 27.0 44.0, 27.0 42.9))'"),
		catalogueEntry("id-south", NewestSliceSouth, "2026-09-07T15:59:20.000Z", "2026-09-07T15:59:45.000Z", false,
			"geography'SRID=4326;POLYGON ((27.0 41.8, 30.0 41.8, 30.0 43.0, 27.0 43.0, 27.0 41.8))'"),
		catalogueEntry("id-older", olderSlice, "2026-09-05T04:13:09.000Z", "2026-09-05T04:13:34.000Z", true,
			"geography'SRID=4326;POLYGON ((27.0 42.0, 28.0 42.0, 28.0 42.5, 27.0 42.5, 27.0 42.0))'"),
	}})
	return body
}

func ProductZip(t *testing.T, topDir string) []byte {
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

func ScriptCDSE(t *testing.T) *CDSEServer {
	t.Helper()
	t.Setenv("CDSE_USERNAME", "test-user")
	t.Setenv("CDSE_PASSWORD", "test-password")

	fake := NewCDSEServer(t)
	fake.Handle(CDSEIdentityHost, func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil || request.PostForm.Get("username") != "test-user" ||
			request.PostForm.Get("client_id") != "cdse-public" {
			http.Error(writer, "bad form", http.StatusBadRequest)
			return
		}
		fmt.Fprint(writer, `{"access_token":"token-123"}`)
	})
	fake.Handle(CDSECatalogueHost, func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "(") {
			if request.Header.Get("Authorization") != "Bearer token-123" {
				http.Error(writer, "no token", http.StatusUnauthorized)
				return
			}
			fmt.Fprintf(writer, `{"Name":%q}`, NewestSliceSouth)
			return
		}
		writer.Write(CatalogueResponse())
	})
	fake.Handle(CDSEDownloadHost, func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer token-123" {
			http.Error(writer, "no token", http.StatusUnauthorized)
			return
		}
		name := NewestSliceSouth
		if strings.Contains(request.URL.Path, "id-north") {
			name = NewestSliceNorth
		}
		writer.Write(ProductZip(t, name))
	})
	return fake
}

type CDSEServer struct {
	t        *testing.T
	server   *httptest.Server
	handlers map[string]http.HandlerFunc
	Requests []string
}

const (
	CDSEIdentityHost  = "identity.dataspace.copernicus.eu"
	CDSECatalogueHost = "catalogue.dataspace.copernicus.eu"
	CDSEDownloadHost  = "download.dataspace.copernicus.eu"
)

func NewCDSEServer(t *testing.T) *CDSEServer {
	t.Helper()
	fake := &CDSEServer{t: t, handlers: map[string]http.HandlerFunc{}}
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
		case CDSEIdentityHost, CDSECatalogueHost, CDSEDownloadHost:
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

func (f *CDSEServer) Handle(host string, handler http.HandlerFunc) {
	f.handlers[host] = handler
}

func (f *CDSEServer) serve(writer http.ResponseWriter, request *http.Request) {
	host := request.Header.Get("X-Original-Host")
	f.Requests = append(f.Requests, host+request.URL.Path)
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
