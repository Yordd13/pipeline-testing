// GeoBounds.union: returns the smallest box enclosing both bounds.
// rasterBand.Bounds: returns the band's geographic extent from its origin, pixel size and dimensions.
// decodeLatin1: converts an ISO-8859-1 input to UTF-8 for the XML decoder, rejecting any other charset.
// OpenRasterBand: reads a .dim header and returns the named float32 band's size, data file and geocoding.
// rasterBand.readGeocoding: parses the image-to-model transform, accepting only an unrotated, square, north-up grid.
// rasterBand.open: opens the band's .img file and returns a reader for its rows.
// bandReader.Close: closes the underlying image file.
// bandReader.readRow: reads one whole row of raw big-endian samples, seeking only when not already positioned there.
// sampleAt: decodes the big-endian float32 sample at a column of a raw row.

package raster

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type GeoBounds struct {
	South, West, North, East float64
}

func (b GeoBounds) union(other GeoBounds) GeoBounds {
	return GeoBounds{
		South: math.Min(b.South, other.South),
		West:  math.Min(b.West, other.West),
		North: math.Max(b.North, other.North),
		East:  math.Max(b.East, other.East),
	}
}

type rasterBand struct {
	imagePath string
	cols      int
	rows      int

	originLon float64
	originLat float64
	pixelDeg  float64

	noData float64
}

func (b *rasterBand) Bounds() GeoBounds {
	return GeoBounds{
		North: b.originLat,
		West:  b.originLon,
		South: b.originLat - float64(b.rows)*b.pixelDeg,
		East:  b.originLon + float64(b.cols)*b.pixelDeg,
	}
}

type dimapDocument struct {
	Geoposition struct {
		Transform string `xml:"IMAGE_TO_MODEL_TRANSFORM"`
	} `xml:"Geoposition"`
	Dimensions struct {
		Cols int `xml:"NCOLS"`
		Rows int `xml:"NROWS"`
	} `xml:"Raster_Dimensions"`
	DataAccess struct {
		Files []struct {
			Path struct {
				Href string `xml:"href,attr"`
			} `xml:"DATA_FILE_PATH"`
			Index int `xml:"BAND_INDEX"`
		} `xml:"Data_File"`
	} `xml:"Data_Access"`
	Interpretation struct {
		Bands []struct {
			Index    int    `xml:"BAND_INDEX"`
			Name     string `xml:"BAND_NAME"`
			DataType string `xml:"DATA_TYPE"`
			NoData   string `xml:"NO_DATA_VALUE"`
		} `xml:"Spectral_Band_Info"`
	} `xml:"Image_Interpretation"`
}

func decodeLatin1(charset string, input io.Reader) (io.Reader, error) {
	if normalised := strings.ToUpper(charset); normalised != "ISO-8859-1" &&
		normalised != "LATIN1" && normalised != "ISO8859-1" {
		return nil, fmt.Errorf("unexpected encoding %q", charset)
	}

	raw, err := io.ReadAll(input)
	if err != nil {
		return nil, err
	}
	widened := make([]rune, len(raw))
	for at, b := range raw {
		widened[at] = rune(b)
	}
	return strings.NewReader(string(widened)), nil
}

func OpenRasterBand(dimPath, bandName string) (*rasterBand, error) {
	raw, err := os.ReadFile(dimPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", filepath.Base(dimPath), err)
	}

	var document dimapDocument
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	decoder.CharsetReader = decodeLatin1
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("cannot parse %s: %w", filepath.Base(dimPath), err)
	}

	band := &rasterBand{cols: document.Dimensions.Cols, rows: document.Dimensions.Rows}
	if band.cols <= 0 || band.rows <= 0 {
		return nil, fmt.Errorf("%s declares no raster size", filepath.Base(dimPath))
	}

	index := -1
	for _, candidate := range document.Interpretation.Bands {
		if candidate.Name != bandName {
			continue
		}
		if candidate.DataType != "float32" {
			return nil, fmt.Errorf("%s is %s, and only float32 is handled",
				bandName, candidate.DataType)
		}
		band.noData, _ = strconv.ParseFloat(strings.TrimSpace(candidate.NoData), 64)
		index = candidate.Index
		break
	}
	if index < 0 {
		return nil, fmt.Errorf("%s holds no band called %s", filepath.Base(dimPath), bandName)
	}

	for _, file := range document.DataAccess.Files {
		if file.Index != index {
			continue
		}
		header := filepath.FromSlash(file.Path.Href)
		band.imagePath = filepath.Join(filepath.Dir(dimPath),
			strings.TrimSuffix(header, filepath.Ext(header))+".img")
		break
	}
	if band.imagePath == "" {
		return nil, fmt.Errorf("%s names no data file for %s", filepath.Base(dimPath), bandName)
	}

	if err := band.readGeocoding(document.Geoposition.Transform); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(dimPath), err)
	}
	return band, nil
}

func (b *rasterBand) readGeocoding(transform string) error {
	parts := strings.Split(strings.TrimSpace(transform), ",")
	if len(parts) != 6 {
		return fmt.Errorf("image-to-model transform has %d terms, want 6", len(parts))
	}

	term := make([]float64, 6)
	for at, text := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil {
			return fmt.Errorf("image-to-model transform term %d is not a number: %q", at+1, text)
		}
		term[at] = value
	}

	scaleX, shearY, shearX, scaleY := term[0], term[1], term[2], term[3]
	if shearX != 0 || shearY != 0 {
		return fmt.Errorf("the product is rotated (shear %g, %g), which is not handled",
			shearX, shearY)
	}
	if scaleX <= 0 || scaleY >= 0 || math.Abs(scaleX+scaleY) > 1e-12 {
		return fmt.Errorf("pixels are %g by %g degrees, want square and north-up", scaleX, scaleY)
	}

	b.pixelDeg = scaleX
	b.originLon = term[4]
	b.originLat = term[5]
	return nil
}

type bandReader struct {
	band *rasterBand
	file *os.File
	line []byte
	at   int64
}

func (b *rasterBand) open() (*bandReader, error) {
	file, err := os.Open(b.imagePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", filepath.Base(b.imagePath), err)
	}
	return &bandReader{band: b, file: file, line: make([]byte, b.cols*4), at: -1}, nil
}

func (r *bandReader) Close() error { return r.file.Close() }

func (r *bandReader) readRow(row int) ([]byte, error) {
	if row != int(r.at) {
		offset := int64(row) * int64(r.band.cols) * 4
		if _, err := r.file.Seek(offset, io.SeekStart); err != nil {
			return nil, err
		}
	}
	if _, err := io.ReadFull(r.file, r.line); err != nil {
		return nil, err
	}
	r.at = int64(row) + 1
	return r.line, nil
}

func sampleAt(line []byte, col int) float64 {
	return float64(math.Float32frombits(binary.BigEndian.Uint32(line[col*4:])))
}
