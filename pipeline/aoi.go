// isInsideAOI: reports whether a latitude/longitude lies inside a polygon, using an eastward ray-casting test.
// isInBulgarianWaters: reports whether a position falls inside the BulgarianWatersAOI polygon.

package main

type AOIVertex struct {
	Latitude  float64
	Longitude float64
}

var BulgarianWatersAOI = []AOIVertex{
	{Latitude: 41.98, Longitude: 27.40},
	{Latitude: 41.98, Longitude: 30.00},
	{Latitude: 43.75, Longitude: 30.00},
	{Latitude: 43.75, Longitude: 27.40},
}

func isInsideAOI(latitude, longitude float64, polygon []AOIVertex) bool {
	if len(polygon) < 3 {
		return false
	}

	inside := false
	for current := range polygon {
		previous := (current + len(polygon) - 1) % len(polygon)

		a, b := polygon[previous], polygon[current]
		if (a.Latitude > latitude) == (b.Latitude > latitude) {
			continue
		}

		crossingLongitude := a.Longitude +
			(latitude-a.Latitude)/(b.Latitude-a.Latitude)*(b.Longitude-a.Longitude)
		if longitude < crossingLongitude {
			inside = !inside
		}
	}
	return inside
}

func isInBulgarianWaters(latitude, longitude float64) bool {
	return isInsideAOI(latitude, longitude, BulgarianWatersAOI)
}
