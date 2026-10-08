// TestIsInBulgarianWaters: checks points off the Bulgarian coast are inside the AOI and neighbouring waters are not.
// TestIsInBulgarianWatersOnTheBoundary: pins down the half-open ray-casting result for points on the AOI edges and vertices.
// TestIsInsideAOIRejectsDegeneratePolygons: checks a polygon with fewer than three vertices contains no point.
// TestBulgarianWatersAOIIsWellFormed: checks the AOI has at least three valid vertices and does not repeat its first one.

package main

import "testing"

func TestIsInBulgarianWaters(t *testing.T) {
	cases := []struct {
		what      string
		latitude  float64
		longitude float64
		want      bool
	}{
		{"open sea east of Varna, clearly inside", 43.10, 28.60, true},
		{"just off Burgas, the case the filter exists to keep", 42.48, 27.60, true},
		{"Sozopol approaches", 42.40, 27.75, true},
		{"the far south-east corner of the envelope", 42.00, 29.95, true},

		{"Romanian waters off Constanta", 44.10, 28.80, false},
		{"the Bosphorus anchorage, where 45 of 56 detections landed", 41.15, 29.10, false},
		{"the Sea of Marmara", 40.70, 28.20, false},
		{"east of the seaward limit", 43.00, 30.40, false},
		{"west of the inland edge, deep in Bulgaria", 42.50, 26.90, false},
		{"just north of the Romanian border", 43.80, 28.50, false},
		{"just south of the Turkish border", 41.90, 28.10, false},
	}

	for _, c := range cases {
		got := isInBulgarianWaters(c.latitude, c.longitude)
		if got != c.want {
			t.Errorf("%s (%.2f, %.2f): got %v, want %v",
				c.what, c.latitude, c.longitude, got, c.want)
		}
	}
}

func TestIsInBulgarianWatersOnTheBoundary(t *testing.T) {
	cases := []struct {
		what      string
		latitude  float64
		longitude float64
		want      bool
	}{
		{"on the western edge", 42.50, 27.40, true},
		{"on the eastern edge", 42.50, 30.00, false},
		{"on the southern edge", 41.98, 28.50, true},
		{"on the northern edge", 43.75, 28.50, false},

		{"on the south-west vertex", 41.98, 27.40, true},
		{"on the north-west vertex", 43.75, 27.40, false},
		{"on the south-east vertex", 41.98, 30.00, false},
		{"on the north-east vertex", 43.75, 30.00, false},
	}

	for _, c := range cases {
		got := isInBulgarianWaters(c.latitude, c.longitude)
		if got != c.want {
			t.Errorf("%s (%.2f, %.2f): got %v, want %v",
				c.what, c.latitude, c.longitude, got, c.want)
		}
	}
}

func TestIsInsideAOIRejectsDegeneratePolygons(t *testing.T) {
	for _, polygon := range [][]AOIVertex{
		nil,
		{{Latitude: 42, Longitude: 28}},
		{{Latitude: 42, Longitude: 28}, {Latitude: 43, Longitude: 29}},
	} {
		if isInsideAOI(42.5, 28.5, polygon) {
			t.Errorf("a %d-vertex polygon accepted a point", len(polygon))
		}
	}
}

func TestBulgarianWatersAOIIsWellFormed(t *testing.T) {
	if len(BulgarianWatersAOI) < 3 {
		t.Fatalf("the AOI has %d vertices, which cannot enclose an area",
			len(BulgarianWatersAOI))
	}
	first, last := BulgarianWatersAOI[0], BulgarianWatersAOI[len(BulgarianWatersAOI)-1]
	if first == last {
		t.Error("the AOI repeats its first vertex at the end; isInsideAOI closes " +
			"the ring itself, so the duplicate makes a zero-length edge")
	}
	for index, vertex := range BulgarianWatersAOI {
		if vertex.Latitude < -90 || vertex.Latitude > 90 {
			t.Errorf("vertex %d has latitude %.4f", index, vertex.Latitude)
		}
		if vertex.Longitude < -180 || vertex.Longitude > 180 {
			t.Errorf("vertex %d has longitude %.4f", index, vertex.Longitude)
		}
	}
}
