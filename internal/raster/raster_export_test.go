// TestReadGeocodingAcceptsANorthUpProduct: checks a north-up transform yields the expected origin and pixel size.
// TestReadGeocodingRefusesWhatItCannotSample: checks rotated, south-up, non-square, short or malformed transforms are refused.
// TestGreyLevelClipsRatherThanWrapping: checks dB values outside the stretch clip to black or white instead of wrapping.
// TestMeasureStretchFollowsTheData: checks the stretch starts at the sea level and reaches above land to keep targets visible.
// TestMeasureStretchRefusesToAmplifyNoise: checks a uniform scene still gets at least the narrowest allowed stretch.
// TestClampSpanKeepsAtLeastOnePixelInsideTheProduct: checks pixel spans are rounded outward, kept non-empty and clipped to the limit.
// TestMosaicDrawsABrightTargetWhereTheGridPutsIt: checks a bright target lands where the degree grid predicts, with no-data left unfilled.
// writeTestProduct: writes a minimal BEAM-DIMAP product called product with the given geocoding and samples.

package raster

import (
	"math"
	"testing"

	"radarpipeline/internal/testsupport"
)

const northUpTransform = "8.983152841195215E-5,0.0,0.0,-8.983152841195215E-5," +
	"27.155265144961415,42.8466400049629"

func TestReadGeocodingAcceptsANorthUpProduct(t *testing.T) {
	band := &rasterBand{}
	if err := band.readGeocoding(northUpTransform); err != nil {
		t.Fatalf("readGeocoding = %v, want nil", err)
	}
	if band.originLon != 27.155265144961415 || band.originLat != 42.8466400049629 {
		t.Errorf("origin = %v, %v, want the transform's translation terms",
			band.originLon, band.originLat)
	}
	if band.pixelDeg != 8.983152841195215e-5 {
		t.Errorf("pixelDeg = %v, want the transform's scale", band.pixelDeg)
	}
}

func TestReadGeocodingRefusesWhatItCannotSample(t *testing.T) {
	cases := map[string]string{
		"rotated":      "1E-4,2E-5,3E-5,-1E-4,27.0,42.0",
		"south-up":     "1E-4,0.0,0.0,1E-4,27.0,42.0",
		"non-square":   "1E-4,0.0,0.0,-2E-4,27.0,42.0",
		"wrong arity":  "1E-4,0.0,0.0,-1E-4",
		"not a number": "1E-4,0.0,0.0,-1E-4,west,42.0",
		"empty":        "",
	}
	for name, transform := range cases {
		t.Run(name, func(t *testing.T) {
			if err := (&rasterBand{}).readGeocoding(transform); err == nil {
				t.Errorf("readGeocoding(%q) = nil, want an error", transform)
			}
		})
	}
}

func TestGreyLevelClipsRatherThanWrapping(t *testing.T) {
	stretch := dbStretch{low: -40, high: -10}
	cases := []struct {
		db   float64
		want uint8
	}{
		{stretch.low - 30, 0},
		{stretch.low, 0},
		{(stretch.low + stretch.high) / 2, 127},
		{stretch.high, 254},
		{stretch.high + 30, 254},
	}
	for _, sample := range cases {
		if got := stretch.greyLevel(sample.db); got != sample.want {
			t.Errorf("greyLevel(%.0f dB) = %d, want %d", sample.db, got, sample.want)
		}
	}
}

func TestMeasureStretchFollowsTheData(t *testing.T) {
	mosaic := &rasterMosaic{
		grid:   geographicGrid{width: 1000, height: 10},
		value:  make([]float32, 10000),
		filled: make([]bool, 10000),
	}
	for at := range mosaic.value {
		switch {
		case at < 9000:
			mosaic.value[at] = 1e-3
		case at < 9950:
			mosaic.value[at] = 10e-3
		default:
			mosaic.value[at] = 1.0
		}
		mosaic.filled[at] = true
	}

	stretch := measureStretch(mosaic)
	if math.Abs(stretch.low-(-30)) > 0.2 {
		t.Errorf("low = %.2f dB, want the sea at -30: the 2nd percentile is sea",
			stretch.low)
	}
	if stretch.high < -10 {
		t.Errorf("high = %.2f dB, want it above the land at -20 so targets stay "+
			"distinguishable from land", stretch.high)
	}
}

func TestMeasureStretchRefusesToAmplifyNoise(t *testing.T) {
	mosaic := &rasterMosaic{
		grid:   geographicGrid{width: 100, height: 10},
		value:  make([]float32, 1000),
		filled: make([]bool, 1000),
	}
	for at := range mosaic.value {
		mosaic.value[at] = 1e-3
		mosaic.filled[at] = true
	}

	stretch := measureStretch(mosaic)
	if span := stretch.high - stretch.low; span < rasterNarrowestStretchDB-0.01 {
		t.Errorf("span = %.2f dB, want at least %.0f", span, rasterNarrowestStretchDB)
	}
}

func TestClampSpanKeepsAtLeastOnePixelInsideTheProduct(t *testing.T) {
	cases := []struct {
		name             string
		from, to         float64
		limit            int
		wantFrom, wantTo int
	}{
		{"whole pixels", 3, 7, 100, 3, 7},
		{"fractional", 3.2, 6.8, 100, 3, 7},
		{"narrower than a pixel", 4.2, 4.3, 100, 4, 5},
		{"clipped at the end", 98.5, 120, 100, 98, 100},
		{"entirely past the end", 200, 260, 100, 200, 100},
	}
	for _, sample := range cases {
		t.Run(sample.name, func(t *testing.T) {
			got := clampSpan(sample.from, sample.to, sample.limit)
			if got.from != sample.wantFrom || got.to != sample.wantTo {
				t.Errorf("clampSpan(%v, %v, %d) = %+v, want {%d %d}",
					sample.from, sample.to, sample.limit, got,
					sample.wantFrom, sample.wantTo)
			}
		})
	}
}

func TestMosaicDrawsABrightTargetWhereTheGridPutsIt(t *testing.T) {
	const cols, rows = 400, 400
	const originLon, originLat, pixelDeg = 27.0, 43.0, 0.005

	const targetCol, targetRow = 300, 100
	samples := make([]float32, cols*rows)
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			if col < 40 {
				continue
			}
			samples[row*cols+col] = 5e-4
		}
	}
	for row := targetRow - 1; row <= targetRow+1; row++ {
		for col := targetCol - 1; col <= targetCol+1; col++ {
			samples[row*cols+col] = 3.0
		}
	}

	dimPath := writeTestProduct(t, t.TempDir(), cols, rows, originLon, originLat, pixelDeg, samples)
	band, err := OpenRasterBand(dimPath, RasterBandName)
	if err != nil {
		t.Fatalf("openRasterBand: %v", err)
	}

	grid := buildGeographicGrid(coveredArea([]*rasterBand{band}), 2*pixelDeg)
	mosaic, err := buildRasterMosaic([]*rasterBand{band}, grid)
	if err != nil {
		t.Fatalf("buildRasterMosaic: %v", err)
	}
	if mosaic.width != cols/2 || mosaic.height != rows/2 {
		t.Fatalf("mosaic is %d x %d, want %d x %d", mosaic.width, mosaic.height, cols/2, rows/2)
	}

	bounds := grid.bounds()
	targetLon := originLon + (targetCol+0.5)*pixelDeg
	targetLat := originLat - (targetRow+0.5)*pixelDeg
	wantX := int((targetLon - bounds.West) / (bounds.East - bounds.West) * float64(mosaic.width))
	wantY := int((bounds.North - targetLat) / (bounds.North - bounds.South) * float64(mosaic.height))

	stretch := measureStretch(mosaic)
	at := wantY*mosaic.width + wantX
	if !mosaic.filled[at] || stretch.greyLevel(decibels(float64(mosaic.value[at]))) < 200 {
		t.Errorf("pixel (%d,%d) is not a saturated target: the mosaic is offset from its grid", wantX, wantY)
	}
	mirrored := (mosaic.height-1-wantY)*mosaic.width + wantX
	if stretch.greyLevel(decibels(float64(mosaic.value[mirrored]))) > 200 {
		t.Error("a target also appears at the mirrored row")
	}
	if mosaic.filled[0] {
		t.Error("the top-left pixel lies in the no-data margin but was filled")
	}
}

func writeTestProduct(t *testing.T, dir string, cols, rows int,
	originLon, originLat, pixelDeg float64, samples []float32) string {
	t.Helper()
	return testsupport.WriteNamedTestProduct(t, dir, "product", cols, rows, originLon, originLat, pixelDeg, samples)
}
