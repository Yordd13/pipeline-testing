// WriteNamedTestProduct: writes a minimal named BEAM-DIMAP header and big-endian float32 band for tests.
// ErrFake.Error: returns the fake error's text.
// WriteSeaProduct: writes a preprocessed product of dark sea with one bright target over the fixture swath.
// WriteShipsProduct: writes a detection product with a mask in its header, one band and the given detection rows.
// WriteTestShapefile: writes a minimal shapefile holding one polygon record built from the given rings.
// SquareRing: returns the four corners of the given box as a coastline ring.

package testsupport

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func WriteNamedTestProduct(t *testing.T, dir, name string, cols, rows int,
	originLon, originLat, pixelDeg float64, samples []float32) string {
	t.Helper()

	dataDir := filepath.Join(dir, name+".data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}

	raw := make([]byte, len(samples)*4)
	for at, sample := range samples {
		binary.BigEndian.PutUint32(raw[at*4:], math.Float32bits(sample))
	}
	if err := os.WriteFile(filepath.Join(dataDir, "Sigma0_VH.img"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	transform := fmt.Sprintf("%v,0.0,0.0,%v,%v,%v", pixelDeg, -pixelDeg, originLon, originLat)
	header := fmt.Sprintf(`<?xml version="1.0" encoding="ISO-8859-1"?>
<Dimap_Document name="%s.dim">
    <Geoposition>
        <IMAGE_TO_MODEL_TRANSFORM>%s</IMAGE_TO_MODEL_TRANSFORM>
    </Geoposition>
    <Raster_Dimensions>
        <NCOLS>%d</NCOLS>
        <NROWS>%d</NROWS>
        <NBANDS>1</NBANDS>
    </Raster_Dimensions>
    <Data_Access>
        <Data_File>
            <DATA_FILE_PATH href="%s.data/%s.hdr" />
            <BAND_INDEX>0</BAND_INDEX>
        </Data_File>
    </Data_Access>
    <Image_Interpretation>
        <Spectral_Band_Info>
            <BAND_INDEX>0</BAND_INDEX>
            <BAND_NAME>%s</BAND_NAME>
            <DATA_TYPE>float32</DATA_TYPE>
            <NO_DATA_VALUE_USED>true</NO_DATA_VALUE_USED>
            <NO_DATA_VALUE>0.0</NO_DATA_VALUE>
        </Spectral_Band_Info>
    </Image_Interpretation>
</Dimap_Document>
`, name, transform, cols, rows, name, "Sigma0_VH", "Sigma0_VH")

	dimPath := filepath.Join(dir, name+".dim")
	if err := os.WriteFile(dimPath, []byte(header), 0o644); err != nil {
		t.Fatal(err)
	}
	return dimPath
}

type ErrFake string

func (e ErrFake) Error() string { return string(e) }

const MaskingDetectionGraph = `<graph id="Graph">
  <version>1.0</version>
  <node id="Read"><operator>Read</operator><sources/></node>
  <node id="Import-Vector"><operator>Import-Vector</operator>
    <sources><sourceProduct refid="Read"/></sources></node>
  <node id="Land-Sea-Mask"><operator>Land-Sea-Mask</operator>
    <sources><sourceProduct refid="Import-Vector"/></sources>
    <parameters class="x"><useSRTM>false</useSRTM><geometry>bg_coast</geometry></parameters></node>
  <node id="AdaptiveThresholding"><operator>AdaptiveThresholding</operator>
    <sources><sourceProduct refid="Land-Sea-Mask"/></sources></node>
  <node id="Write"><operator>Write</operator>
    <sources><sourceProduct refid="AdaptiveThresholding"/></sources>
    <parameters class="x"><formatName>BEAM-DIMAP</formatName></parameters></node>
</graph>`

const (
	SwathCols, SwathRows        = 40, 2
	SwathLon, SwathLat, SwathPx = 28.0, 43.0, 0.005
)

var (
	CoastFarAway    = SquareRing(27.5, 42.0, 27.6, 42.1)
	CoastInTheSwath = SquareRing(28.05, 42.9, 28.1, 43.1)
)

func WriteSeaProduct(t *testing.T, dir, name string) string {
	t.Helper()
	samples := make([]float32, SwathCols*SwathRows)
	for at := range samples {
		samples[at] = 5e-4
	}
	samples[20] = 3.0
	return WriteNamedTestProduct(t, dir, name, SwathCols, SwathRows, SwathLon, SwathLat, SwathPx, samples)
}

func WriteShipsProduct(t *testing.T, dir, name string, rows []string) string {
	t.Helper()
	dimPath := filepath.Join(dir, name+".dim")
	WriteText(t, dimPath, "<Dimap_Document>\n<Masks><Mask>ShipDetections</Mask></Masks>\n</Dimap_Document>\n")
	WriteText(t, filepath.Join(dir, name+".data", "ship_bit_msk.img"), "band bytes")
	header := "ShipDetections\tDetected_x:Integer\tDetected_y:Integer\tDetected_lat:Double\t" +
		"Detected_lon:Double\tDetected_width:Double\tDetected_length:Double"
	WriteText(t, filepath.Join(dir, name+".data", "vector_data", "ShipDetections.csv"),
		"#defaultCSS=fill:#ff0000\n"+header+"\n"+strings.Join(rows, "\n")+"\n")
	WriteText(t, filepath.Join(dir, name+".data", "vector_data", "pins.csv"), "placeholder\n")
	return dimPath
}

func WriteTestShapefile(t *testing.T, path string, rings [][][2]float64) {
	t.Helper()

	points := 0
	for _, ring := range rings {
		points += len(ring)
	}
	content := 44 + len(rings)*4 + points*16
	total := 100 + 8 + content

	raw := make([]byte, total)
	binary.BigEndian.PutUint32(raw[0:4], 9994)
	binary.BigEndian.PutUint32(raw[24:28], uint32(total/2))
	binary.LittleEndian.PutUint32(raw[28:32], 1000)
	binary.LittleEndian.PutUint32(raw[32:36], 5)

	binary.BigEndian.PutUint32(raw[100:104], 1)
	binary.BigEndian.PutUint32(raw[104:108], uint32(content/2))
	body := raw[108:]

	binary.LittleEndian.PutUint32(body[0:4], 5)
	binary.LittleEndian.PutUint32(body[36:40], uint32(len(rings)))
	binary.LittleEndian.PutUint32(body[40:44], uint32(points))

	at := 0
	for index, ring := range rings {
		binary.LittleEndian.PutUint32(body[44+index*4:48+index*4], uint32(at))
		at += len(ring)
	}

	base := 44 + len(rings)*4
	written := 0
	for _, ring := range rings {
		for _, point := range ring {
			offset := base + written*16
			binary.LittleEndian.PutUint64(body[offset:offset+8], math.Float64bits(point[0]))
			binary.LittleEndian.PutUint64(body[offset+8:offset+16], math.Float64bits(point[1]))
			written++
		}
	}

	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func SquareRing(west, south, east, north float64) [][2]float64 {
	return [][2]float64{{west, south}, {east, south}, {east, north}, {west, north}}
}
