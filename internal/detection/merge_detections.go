// MergeDetections: gathers all slices' detections, keeps those in the AOI, drops duplicates, measures shore distance.
// readSliceDetections: reads one slice's SNAP detection file and stamps each row with its slice and product details.
// parseSNAPDetectionFile: parses SNAP's tab-separated detection file into detections, mapping columns by header name.
// mapDetectionColumns: maps lowercased header column names, without any ":Type" suffix, to their indexes.
// detectionFromFields: builds a Detection from one row's fields, failing if latitude or longitude does not parse.
// dropSeamDuplicates: removes detections within 150 m of an earlier kept one and returns the duplicate count.
// metresBetween: returns the great-circle distance in metres between two latitude/longitude points.
// detectionsFileStem: returns the pass's run ID, built from its start time and absolute orbit.
// FormatCoordinate: formats a latitude or longitude with six decimal places.

package detection

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"radarpipeline/internal/config"
	"radarpipeline/internal/pass"
	"radarpipeline/internal/snap"
)

const (
	SeamDuplicateRadiusMetres = 150.0

	earthRadiusMetres = 6371000.0
)

type Detection struct {
	ID        string
	Latitude  float64
	Longitude float64
	WidthM    string
	LengthM   string
	PixelX    string
	PixelY    string

	SourceSlice     string
	ProductID       string
	ProductName     string
	AcquisitionTime string
	Mission         string
	AbsoluteOrbit   string
	CoveragePercent string

	DistanceToShoreM float64
	ShoreMeasured    bool
}

type MergeOutcome struct {
	RunID             string
	RawCFAR           int
	DroppedOutsideAOI int
	SeamDuplicates    int
	Written           int
	SlicesRead        int
	SlicesEmpty       int

	Kept []Detection
}

func MergeDetections(cfg *config.Config, plan *pass.PassPlan) (MergeOutcome, error) {
	outcome := MergeOutcome{}

	collected := make([]Detection, 0, 64)
	for _, slice := range plan.SucceededSlices() {
		detections, err := readSliceDetections(slice, plan)
		if err != nil {
			return outcome, err
		}
		outcome.SlicesRead++
		if len(detections) == 0 {
			outcome.SlicesEmpty++
		}
		collected = append(collected, detections...)
	}

	outcome.RawCFAR = len(collected)

	inArea := make([]Detection, 0, len(collected))
	for _, detection := range collected {
		if isInBulgarianWaters(detection.Latitude, detection.Longitude) {
			inArea = append(inArea, detection)
		}
	}
	outcome.DroppedOutsideAOI = len(collected) - len(inArea)

	kept, duplicates := dropSeamDuplicates(inArea)
	outcome.SeamDuplicates = duplicates
	outcome.Written = len(kept)

	measureDistanceToShore(cfg, kept)

	sort.Slice(kept, func(i, j int) bool {
		if kept[i].SourceSlice != kept[j].SourceSlice {
			return kept[i].SourceSlice < kept[j].SourceSlice
		}
		return kept[i].ID < kept[j].ID
	})
	outcome.Kept = kept
	outcome.RunID = detectionsFileStem(plan)
	return outcome, nil
}

func readSliceDetections(slice *pass.SliceJob, plan *pass.PassPlan) ([]Detection, error) {
	vectorDir := filepath.Join(
		strings.TrimSuffix(slice.DetectionProductPath, ".dim")+".data", snap.VectorDataDirName)

	entries, err := os.ReadDir(vectorDir)
	if err != nil {
		return nil, nil
	}
	detectionFile, found := snap.FindDetectionFile(entries)
	if !found {
		return nil, nil
	}

	rows, err := parseSNAPDetectionFile(filepath.Join(vectorDir, detectionFile))
	if err != nil {
		return nil, fmt.Errorf("reading %s of %s: %w", detectionFile, slice.SliceName(), err)
	}

	acquired := ""
	if !slice.AcquiredAt.IsZero() {
		acquired = slice.AcquiredAt.UTC().Format(time.RFC3339)
	}
	for index := range rows {
		rows[index].ID = slice.SliceName() + "_" + rows[index].ID
		rows[index].SourceSlice = slice.SliceName()
		rows[index].ProductID = slice.ProductID
		rows[index].ProductName = pass.TrimProductExtensions(slice.ProductName)
		rows[index].AcquisitionTime = acquired
		rows[index].Mission = slice.Mission
		rows[index].AbsoluteOrbit = slice.AbsoluteOrbit
		rows[index].CoveragePercent = plan.CoverageText()
	}
	return rows, nil
}

func parseSNAPDetectionFile(path string) ([]Detection, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var columns map[string]int
	detections := make([]Detection, 0, 32)

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")

		if columns == nil {
			columns = mapDetectionColumns(fields)
			continue
		}

		detection, ok := detectionFromFields(fields, columns)
		if ok {
			detections = append(detections, detection)
		}
	}
	return detections, scanner.Err()
}

func mapDetectionColumns(fields []string) map[string]int {
	columns := map[string]int{}
	for index, field := range fields {
		name := strings.ToLower(strings.TrimSpace(field))
		if colon := strings.Index(name, ":"); colon >= 0 {
			name = name[:colon]
		}
		columns[name] = index
	}
	return columns
}

func detectionFromFields(fields []string, columns map[string]int) (Detection, bool) {
	at := func(name string) string {
		index, known := columns[name]
		if !known || index >= len(fields) {
			return ""
		}
		return strings.TrimSpace(fields[index])
	}

	latitude, latErr := strconv.ParseFloat(at("detected_lat"), 64)
	longitude, lonErr := strconv.ParseFloat(at("detected_lon"), 64)
	if latErr != nil || lonErr != nil {
		return Detection{}, false
	}

	return Detection{
		ID:        at("shipdetections"),
		Latitude:  latitude,
		Longitude: longitude,
		WidthM:    at("detected_width"),
		LengthM:   at("detected_length"),
		PixelX:    at("detected_x"),
		PixelY:    at("detected_y"),
	}, true
}

func dropSeamDuplicates(detections []Detection) (kept []Detection, duplicates int) {
	kept = make([]Detection, 0, len(detections))
	for _, candidate := range detections {
		isDuplicate := false
		for _, existing := range kept {
			if metresBetween(candidate.Latitude, candidate.Longitude,
				existing.Latitude, existing.Longitude) <= SeamDuplicateRadiusMetres {
				isDuplicate = true
				break
			}
		}
		if isDuplicate {
			duplicates++
			continue
		}
		kept = append(kept, candidate)
	}
	return kept, duplicates
}

func metresBetween(lat1, lon1, lat2, lon2 float64) float64 {
	radians := math.Pi / 180
	deltaLat := (lat2 - lat1) * radians
	deltaLon := (lon2 - lon1) * radians
	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(lat1*radians)*math.Cos(lat2*radians)*
			math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	return 2 * earthRadiusMetres * math.Asin(math.Min(1, math.Sqrt(a)))
}

func detectionsFileStem(plan *pass.PassPlan) string {
	stamp := "unknown-time"
	if start := plan.PassStart(); !start.IsZero() {
		stamp = start.UTC().Format("2006-01-02T150405Z")
	}
	orbit := plan.AbsoluteOrbit()
	if orbit == "" {
		orbit = "unknown-orbit"
	}
	return stamp + "_" + orbit
}

func FormatCoordinate(degrees float64) string {
	return strconv.FormatFloat(degrees, 'f', 6, 64)
}
