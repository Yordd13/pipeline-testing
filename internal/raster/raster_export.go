// geographicGrid.size: returns the grid's width and height in pixels.
// geographicGrid.lonAtColumn: returns the longitude at a column edge of the degree grid.
// geographicGrid.latAtRow: returns the latitude at a row edge of the degree grid.
// geographicGrid.bounds: returns the degree grid's own extent from its corner, pixel size and dimensions.
// buildGeographicGrid: builds a degree grid covering the area at the given pixel size, rounding outwards.
// coveredArea: returns the smallest box enclosing every band's bounds.
// buildRasterMosaic: resamples every band onto one output grid, the first band winning where they overlap.
// pixelSpan.empty: reports whether the span covers no pixels.
// rasterMosaic.paint: averages a band's valid samples into each still-unfilled mosaic pixel they cover.
// rasterMosaic.columnSpans: maps each mosaic column to the run of band columns it covers.
// rasterMosaic.rowEdges: returns each mosaic row edge as a fractional band row.
// clampSpan: rounds a fractional pixel range outwards to at least one whole pixel, clamped to the product.
// decibels: converts a linear reflectance to decibels.
// dbStretch.greyLevel: maps a decibel value linearly onto a grey level from 0 to 254, clamping at the ends.
// measureStretch: picks the decibel stretch from the mosaic's 2nd and 99.8th percentiles, bounded in width.
// percentileFrom: reads the decibel value at a percentile from a histogram.
// ExportPassRaster: draws the pass as XYZ tiles and returns their description, or nil when no tiles can be made.
// exportPassTiles: cuts the tile pyramid, prints a summary, prunes old runs and returns the tile description.

package raster

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"radarpipeline/internal/config"
	"radarpipeline/internal/pass"
)

const (
	RasterBandName = "Sigma0_VH"

	rasterWidestStretchDB    = 60.0
	rasterNarrowestStretchDB = 12.0

	rasterLowPercentile  = 2.0
	rasterHighPercentile = 99.8

	rasterMinValidFraction = 0.5
)

type geographicGrid struct {
	area     GeoBounds
	pixelDeg float64
	width    int
	height   int
}

func (g geographicGrid) size() (int, int) { return g.width, g.height }

func (g geographicGrid) lonAtColumn(column float64) float64 {
	return g.area.West + column*g.pixelDeg
}

func (g geographicGrid) latAtRow(row float64) float64 {
	return g.area.North - row*g.pixelDeg
}

func (g geographicGrid) bounds() GeoBounds {
	return GeoBounds{
		South: g.area.North - float64(g.height)*g.pixelDeg,
		West:  g.area.West,
		North: g.area.North,
		East:  g.area.West + float64(g.width)*g.pixelDeg,
	}
}

func buildGeographicGrid(area GeoBounds, pixelDeg float64) geographicGrid {
	return geographicGrid{
		area:     area,
		pixelDeg: pixelDeg,
		width:    max(int(math.Ceil((area.East-area.West)/pixelDeg)), 1),
		height:   max(int(math.Ceil((area.North-area.South)/pixelDeg)), 1),
	}
}

type rasterMosaic struct {
	grid   geographicGrid
	width  int
	height int
	value  []float32
	filled []bool
}

func coveredArea(bands []*rasterBand) GeoBounds {
	area := bands[0].Bounds()
	for _, band := range bands[1:] {
		area = area.union(band.Bounds())
	}
	return area
}

func buildRasterMosaic(bands []*rasterBand, grid geographicGrid) (*rasterMosaic, error) {
	if len(bands) == 0 {
		return nil, fmt.Errorf("no bands to draw")
	}

	width, height := grid.size()
	mosaic := &rasterMosaic{
		grid:   grid,
		width:  width,
		height: height,
		value:  make([]float32, width*height),
		filled: make([]bool, width*height),
	}

	for _, band := range bands {
		if err := mosaic.paint(band); err != nil {
			return nil, err
		}
	}
	return mosaic, nil
}

type pixelSpan struct{ from, to int }

func (s pixelSpan) empty() bool { return s.from >= s.to }

func (m *rasterMosaic) paint(band *rasterBand) error {
	columns := m.columnSpans(band)
	rowEdges := m.rowEdges(band)

	reader, err := band.open()
	if err != nil {
		return err
	}
	defer reader.Close()

	sum := make([]float64, m.width)
	valid := make([]int, m.width)
	seen := make([]int, m.width)

	for row := 0; row < m.height; row++ {
		rows := clampSpan(rowEdges[row], rowEdges[row+1], band.rows)
		if rows.empty() {
			continue
		}

		for at := range sum {
			sum[at], valid[at], seen[at] = 0, 0, 0
		}

		for source := rows.from; source < rows.to; source++ {
			line, err := reader.readRow(source)
			if err != nil {
				return fmt.Errorf("reading %s row %d: %w",
					filepath.Base(band.imagePath), source, err)
			}
			for column, span := range columns {
				if span.empty() {
					continue
				}
				for at := span.from; at < span.to; at++ {
					seen[column]++
					if sample := sampleAt(line, at); sample > 0 && sample != band.noData {
						sum[column] += sample
						valid[column]++
					}
				}
			}
		}

		base := row * m.width
		for column := range columns {
			if seen[column] == 0 || m.filled[base+column] {
				continue
			}
			if float64(valid[column]) < rasterMinValidFraction*float64(seen[column]) {
				continue
			}
			m.value[base+column] = float32(sum[column] / float64(valid[column]))
			m.filled[base+column] = true
		}
	}
	return nil
}

func (m *rasterMosaic) columnSpans(band *rasterBand) []pixelSpan {
	spans := make([]pixelSpan, m.width)
	previous := (m.grid.lonAtColumn(0) - band.originLon) / band.pixelDeg

	for column := 0; column < m.width; column++ {
		next := (m.grid.lonAtColumn(float64(column+1)) - band.originLon) / band.pixelDeg
		spans[column] = clampSpan(previous, next, band.cols)
		previous = next
	}
	return spans
}

func (m *rasterMosaic) rowEdges(band *rasterBand) []float64 {
	edges := make([]float64, m.height+1)
	for row := 0; row <= m.height; row++ {
		edges[row] = (band.originLat - m.grid.latAtRow(float64(row))) / band.pixelDeg
	}
	return edges
}

func clampSpan(from, to float64, limit int) pixelSpan {
	span := pixelSpan{from: int(math.Floor(from)), to: int(math.Ceil(to))}
	if span.to <= span.from {
		span.to = span.from + 1
	}
	span.from = max(span.from, 0)
	span.to = min(span.to, limit)
	return span
}

func decibels(reflectance float64) float64 { return 10 * math.Log10(reflectance) }

type dbStretch struct{ low, high float64 }

func (s dbStretch) greyLevel(db float64) uint8 {
	share := (db - s.low) / (s.high - s.low)
	share = math.Max(0, math.Min(1, share))
	return uint8(math.Round(share * 254))
}

const (
	stretchBinDB   = 0.05
	stretchFloorDB = -90.0
	stretchBins    = 2400
)

func measureStretch(mosaic *rasterMosaic) dbStretch {
	var histogram [stretchBins]int
	total := 0

	for at, filled := range mosaic.filled {
		if !filled {
			continue
		}
		bin := int((decibels(float64(mosaic.value[at])) - stretchFloorDB) / stretchBinDB)
		histogram[min(max(bin, 0), stretchBins-1)]++
		total++
	}

	if total == 0 {
		return dbStretch{low: -45, high: -5}
	}

	stretch := dbStretch{
		low:  percentileFrom(&histogram, total, rasterLowPercentile),
		high: percentileFrom(&histogram, total, rasterHighPercentile),
	}

	span := stretch.high - stretch.low
	if span < rasterNarrowestStretchDB {
		stretch.high = stretch.low + rasterNarrowestStretchDB
	} else if span > rasterWidestStretchDB {
		stretch.low = stretch.high - rasterWidestStretchDB
	}
	return stretch
}

func percentileFrom(histogram *[stretchBins]int, total int, percentile float64) float64 {
	wanted := int(percentile / 100 * float64(total))
	running := 0
	for bin, count := range histogram {
		running += count
		if running >= wanted {
			return stretchFloorDB + (float64(bin)+0.5)*stretchBinDB
		}
	}
	return stretchFloorDB + stretchBins*stretchBinDB
}

type RasterSidecar struct {
	Type   string
	File   string
	Bounds SidecarBounds

	MaxNativeZoom int
	MaxZoom       int
}

type SidecarBounds struct {
	South float64
	West  float64
	North float64
	East  float64
}

func ExportPassRaster(cfg *config.Config, plan *pass.PassPlan, runID string) *RasterSidecar {
	bands := make([]*rasterBand, 0, len(plan.Slices))
	for _, slice := range plan.SucceededSlices() {
		band, err := OpenRasterBand(slice.PreprocessedPath, RasterBandName)
		if err != nil {
			fmt.Printf("Note: no image for %s: %v\n", slice.SliceName(), err)
			continue
		}
		bands = append(bands, band)
	}
	if len(bands) == 0 {
		fmt.Println("Note: no product could be drawn, so this run has no image")
		return nil
	}

	sidecar := exportPassTiles(cfg, bands, runID)
	if sidecar == nil {
		fmt.Println("Note: no tiles could be cut, so this run has no raster layer")
	}
	return sidecar
}

func exportPassTiles(cfg *config.Config, bands []*rasterBand, runID string) *RasterSidecar {
	if cfg.TilesDir == "" {
		fmt.Println("Note: no tiles directory is configured")
		return nil
	}

	outcome, err := buildPassTiles(cfg, bands, runID)
	if err != nil {
		fmt.Printf("Note: could not cut tiles: %v\n", err)
		return nil
	}

	sidecar := RasterSidecar{
		Type: "tiles",
		File: runID + "/{z}/{x}/{y}." + tileExtension,
		Bounds: SidecarBounds{
			South: outcome.Bounds.South, West: outcome.Bounds.West,
			North: outcome.Bounds.North, East: outcome.Bounds.East,
		},
		MaxNativeZoom: tileMaxZoom,
		MaxZoom:       tileViewerMaxZoom,
	}

	fmt.Printf("Tiles:          %d tiles, %s, zoom %d-%d, cut in %s\n",
		outcome.Tiles, config.FormatBytes(outcome.Bytes), tileMinZoom, tileMaxZoom,
		outcome.Took.Round(time.Second))
	fmt.Printf("                %.3f to %.3f N, %.3f to %.3f E\n",
		outcome.Bounds.South, outcome.Bounds.North,
		outcome.Bounds.West, outcome.Bounds.East)
	fmt.Printf("                %s\n", outcome.Directory)

	removed, err := pruneTileDirectories(config.AbsolutePath(cfg.TilesDir), cfg.TileRetentionRuns)
	if err != nil {
		fmt.Printf("Note: could not prune old tiles: %v\n", err)
	} else if len(removed) > 0 {
		fmt.Printf("                pruned %d run(s) beyond the newest %d: %s\n",
			len(removed), cfg.TileRetentionRuns, strings.Join(removed, ", "))
	}
	return &sidecar
}
