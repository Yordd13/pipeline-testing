// FindNewestPassOverArea: searches the catalogue for a named area and returns every slice of its newest pass.
// FindRecentPassesOverArea: returns up to the wanted number of recent passes over a named area, newest first.
// groupIntoPasses: groups scenes by pass key, keeping passes newest first and each pass's slices in time order.
// TrackDirectionOf: reports whether a pass is ascending or descending from how its slices' latitudes progress.
// slicesOfNewestPass: keeps the scenes that share the newest scene's pass key, sorted by acquisition time.
// AbsoluteOrbitFromProductName: extracts the absolute orbit field from a Sentinel-1 product name, or "" if absent.
// KnownAreaNames: returns the configured area names, sorted and comma-separated.
// queryCatalogueForRecentScenes: asks the CDSE catalogue for the newest scenes over a footprint, returning the raw body.
// buildSceneFilterExpression: builds the OData filter for non-COG Sentinel-1 IW GRDH products over a footprint.
// parseScenesFromResponse: parses a catalogue search response into CatalogueScene values, failing if it is empty.
// DescribeSceneAge: describes how long ago a scene was acquired in hours or days.
// CatalogueScene.passKey: returns the mission prefix and absolute orbit joined, identifying the scene's pass.
// MissionPrefixFromProductName: returns the mission field (such as S1A) at the start of a product name.

package cdse

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	burgasCoastlineFootprint = "POLYGON((27.2 42.3, 27.9 42.3, 27.9 42.8, 27.2 42.8, 27.2 42.3))"

	bulgarianWatersFootprint = "POLYGON((27.4 41.95, 29.2 41.95, 29.2 43.8, 27.4 43.8, 27.4 41.95))"
)

var FootprintsByAreaName = map[string]string{
	"burgas":   burgasCoastlineFootprint,
	"bulgaria": bulgarianWatersFootprint,
}

const catalogueSearchTimeout = 90 * time.Second

const cataloguePageSize = 20

type CatalogueScene struct {
	ProductID   string
	ProductName string
	AcquiredAt  time.Time
	EndsAt      time.Time
	IsOnline    bool

	MissionPrefix string
	AbsoluteOrbit string

	Footprint string
	Extent    GeoExtent
}

func FindNewestPassOverArea(areaName string) ([]CatalogueScene, error) {
	footprint, isKnownArea := FootprintsByAreaName[areaName]
	if !isKnownArea {
		return nil, fmt.Errorf("unknown area %q (known: %s)", areaName, KnownAreaNames())
	}

	responseBody, err := queryCatalogueForRecentScenes(footprint, areaName, cataloguePageSize)
	if err != nil {
		return nil, err
	}

	scenes, err := parseScenesFromResponse(responseBody, areaName)
	if err != nil {
		return nil, err
	}
	return slicesOfNewestPass(scenes), nil
}

func FindRecentPassesOverArea(areaName string, wanted int) ([][]CatalogueScene, error) {
	footprint, isKnownArea := FootprintsByAreaName[areaName]
	if !isKnownArea {
		return nil, fmt.Errorf("unknown area %q (known: %s)", areaName, KnownAreaNames())
	}

	responseBody, err := queryCatalogueForRecentScenes(footprint, areaName, wanted*catalogueSlicesPerPassGuess)
	if err != nil {
		return nil, err
	}
	scenes, err := parseScenesFromResponse(responseBody, areaName)
	if err != nil {
		return nil, err
	}
	return groupIntoPasses(scenes, wanted), nil
}

func groupIntoPasses(scenes []CatalogueScene, wanted int) [][]CatalogueScene {
	order := make([]string, 0, len(scenes))
	byKey := map[string][]CatalogueScene{}
	for _, scene := range scenes {
		key := scene.passKey()
		if _, seen := byKey[key]; !seen {
			order = append(order, key)
		}
		byKey[key] = append(byKey[key], scene)
	}

	passes := make([][]CatalogueScene, 0, len(order))
	for _, key := range order {
		if len(passes) == wanted {
			break
		}
		pass := byKey[key]
		sort.Slice(pass, func(i, j int) bool {
			return pass[i].AcquiredAt.Before(pass[j].AcquiredAt)
		})
		passes = append(passes, pass)
	}
	return passes
}

func TrackDirectionOf(pass []CatalogueScene) string {
	if len(pass) < 2 || pass[0].Extent.IsZero() || pass[len(pass)-1].Extent.IsZero() {
		return "unknown"
	}
	if pass[len(pass)-1].Extent.LatMin > pass[0].Extent.LatMin {
		return "ascending"
	}
	return "descending"
}

func slicesOfNewestPass(scenes []CatalogueScene) []CatalogueScene {
	newestPass := scenes[0].passKey()

	pass := make([]CatalogueScene, 0, 4)
	for _, scene := range scenes {
		if scene.passKey() == newestPass {
			pass = append(pass, scene)
		}
	}

	sort.Slice(pass, func(i, j int) bool {
		return pass[i].AcquiredAt.Before(pass[j].AcquiredAt)
	})
	return pass
}

func AbsoluteOrbitFromProductName(productName string) string {
	fields := strings.Split(strings.TrimSuffix(productName, ".SAFE"), "_")
	const orbitField = 6
	if len(fields) <= orbitField {
		return ""
	}
	return fields[orbitField]
}

func KnownAreaNames() string {
	names := make([]string, 0, len(FootprintsByAreaName))
	for name := range FootprintsByAreaName {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func queryCatalogueForRecentScenes(footprint, areaName string, wanted int) ([]byte, error) {
	queryParams := url.Values{}
	queryParams.Set("$filter", buildSceneFilterExpression(footprint))
	queryParams.Set("$orderby", "ContentDate/Start desc")
	queryParams.Set("$top", fmt.Sprintf("%d", wanted))
	queryParams.Set("$select", "Id,Name,ContentDate,Online,Footprint")

	client := &http.Client{Timeout: catalogueSearchTimeout}
	response, err := client.Get(cdseCatalogueURL + "?" + queryParams.Encode())
	if err != nil {
		return nil, fmt.Errorf("searching the catalogue for %s failed "+
			"(a wrong footprint can make this time out): %w", areaName, err)
	}
	defer response.Body.Close()

	responseBody, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalogue search returned %d: %s", response.StatusCode, responseBody)
	}
	return responseBody, nil
}

func buildSceneFilterExpression(footprint string) string {
	return fmt.Sprintf(
		"Collection/Name eq 'SENTINEL-1'"+
			" and contains(Name,'IW_GRDH')"+
			" and not contains(Name,'_COG')"+
			" and OData.CSC.Intersects(area=geography'SRID=4326;%s')",
		footprint,
	)
}

func parseScenesFromResponse(responseBody []byte, areaName string) ([]CatalogueScene, error) {
	var searchResults struct {
		Value []struct {
			ID          string `json:"Id"`
			Name        string `json:"Name"`
			Online      bool   `json:"Online"`
			Footprint   string `json:"Footprint"`
			ContentDate struct {
				Start string `json:"Start"`
				End   string `json:"End"`
			} `json:"ContentDate"`
		} `json:"value"`
	}
	if err := json.Unmarshal(responseBody, &searchResults); err != nil {
		return nil, fmt.Errorf("could not read the search results: %w", err)
	}
	if len(searchResults.Value) == 0 {
		return nil, fmt.Errorf(
			"no Sentinel-1 IW GRDH scene has ever been found over %s; "+
				"the footprint is probably wrong", areaName)
	}

	scenes := make([]CatalogueScene, 0, len(searchResults.Value))
	for _, found := range searchResults.Value {
		scene := CatalogueScene{
			ProductID:     found.ID,
			ProductName:   found.Name,
			IsOnline:      found.Online,
			Footprint:     found.Footprint,
			MissionPrefix: MissionPrefixFromProductName(found.Name),
			AbsoluteOrbit: AbsoluteOrbitFromProductName(found.Name),
		}
		if acquiredAt, err := time.Parse(time.RFC3339, found.ContentDate.Start); err == nil {
			scene.AcquiredAt = acquiredAt
		}
		if endsAt, err := time.Parse(time.RFC3339, found.ContentDate.End); err == nil {
			scene.EndsAt = endsAt
		}
		if extent, err := ParseFootprintExtent(found.Footprint); err == nil {
			scene.Extent = extent
		}
		scenes = append(scenes, scene)
	}
	return scenes, nil
}

func DescribeSceneAge(acquiredAt time.Time) string {
	elapsed := time.Since(acquiredAt)
	switch {
	case elapsed < time.Hour:
		return "under an hour ago"
	case elapsed < 48*time.Hour:
		return fmt.Sprintf("%d hours ago", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(elapsed.Hours()/24))
	}
}

func (s CatalogueScene) passKey() string {
	return s.MissionPrefix + "_" + s.AbsoluteOrbit
}

func MissionPrefixFromProductName(productName string) string {
	fields := strings.Split(productName, "_")
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

const catalogueSlicesPerPassGuess = 4
