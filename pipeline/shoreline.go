// shoreline.empty: reports whether the shoreline has no rings.
// shoreline.vertices: returns the total number of vertices across all rings.
// shorelineForGraph: loads the coastline shapefile the detection graph's Land-Sea-Mask uses, or nil if none.
// loadShoreline: reads every ring of an ESRI polygon shapefile into a shoreline.
// shoreline.distanceMetres: returns the distance in metres from a point to the nearest shoreline segment.
// measureDistanceToShore: sets each detection's distance to the nearest coast, skipping it if no coastline loads.
// formatShoreDistance: formats a detection's shore distance to the nearest metre, or "" if not measured.
// distanceToSegmentMetres: returns the distance from the origin to the segment between two points.
// shoreline.touchesBounds: reports whether any coastline vertex lies in the box or any segment crosses its edges.
// boundsHold: reports whether a longitude/latitude point lies inside the box, edges included.
// segmentsCross: reports whether two line segments properly intersect.
// turnsLeft: reports whether point p lies to the left of the line from a to b.

package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
)

const (
	metresPerDegreeLongitude = 111320.0
	metresPerDegreeLatitude  = 111132.0
)

const shapefilePolygonType = 5

type shoreline struct {
	rings [][][2]float64
}

func (s *shoreline) empty() bool { return len(s.rings) == 0 }

func (s *shoreline) vertices() int {
	total := 0
	for _, ring := range s.rings {
		total += len(ring)
	}
	return total
}

func shorelineForGraph(config *Config) (*shoreline, error) {
	settings, err := readLandSeaMaskSettings(config.DetectionGraphFilePath())
	if err != nil || !settings.Present || settings.Geometry == "" {
		return nil, err
	}
	return loadShoreline(filepath.Join(config.GraphsDir, settings.Geometry+".shp"))
}

func loadShoreline(path string) (*shoreline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", filepath.Base(path), err)
	}
	if len(raw) < 100 {
		return nil, fmt.Errorf("%s is too short to be a shapefile", filepath.Base(path))
	}
	if code := binary.BigEndian.Uint32(raw[0:4]); code != 9994 {
		return nil, fmt.Errorf("%s starts with %d, not a shapefile", filepath.Base(path), code)
	}
	if kind := binary.LittleEndian.Uint32(raw[32:36]); kind != shapefilePolygonType {
		return nil, fmt.Errorf("%s holds shape type %d, want polygons",
			filepath.Base(path), kind)
	}

	end := min(int(binary.BigEndian.Uint32(raw[24:28]))*2, len(raw))

	shore := &shoreline{}
	for at := 100; at+8 <= end; {
		content := int(binary.BigEndian.Uint32(raw[at+4:at+8])) * 2
		if content <= 0 || at+8+content > end {
			break
		}
		body := raw[at+8 : at+8+content]
		at += 8 + content

		if len(body) < 44 || binary.LittleEndian.Uint32(body[0:4]) != shapefilePolygonType {
			continue
		}
		parts := int(binary.LittleEndian.Uint32(body[36:40]))
		points := int(binary.LittleEndian.Uint32(body[40:44]))
		if parts <= 0 || points <= 0 || 44+parts*4+points*16 > len(body) {
			continue
		}

		starts := make([]int, parts)
		for part := range starts {
			starts[part] = int(binary.LittleEndian.Uint32(body[44+part*4 : 48+part*4]))
		}
		base := 44 + parts*4

		for part := 0; part < parts; part++ {
			stop := points
			if part+1 < parts {
				stop = starts[part+1]
			}
			if starts[part] < 0 || stop > points || stop <= starts[part] {
				continue
			}
			ring := make([][2]float64, 0, stop-starts[part])
			for index := starts[part]; index < stop; index++ {
				offset := base + index*16
				ring = append(ring, [2]float64{
					math.Float64frombits(binary.LittleEndian.Uint64(body[offset : offset+8])),
					math.Float64frombits(binary.LittleEndian.Uint64(body[offset+8 : offset+16])),
				})
			}
			shore.rings = append(shore.rings, ring)
		}
	}

	if shore.empty() {
		return nil, fmt.Errorf("%s holds no rings", filepath.Base(path))
	}
	return shore, nil
}

func (s *shoreline) distanceMetres(latitude, longitude float64) float64 {
	eastWest := math.Cos(latitude*math.Pi/180) * metresPerDegreeLongitude

	nearest := math.Inf(1)
	for _, ring := range s.rings {
		for index := range ring {
			from := ring[index]
			to := ring[(index+1)%len(ring)]

			distance := distanceToSegmentMetres(
				(from[0]-longitude)*eastWest, (from[1]-latitude)*metresPerDegreeLatitude,
				(to[0]-longitude)*eastWest, (to[1]-latitude)*metresPerDegreeLatitude)
			if distance < nearest {
				nearest = distance
			}
		}
	}
	return nearest
}

func measureDistanceToShore(config *Config, detections []Detection) {
	if len(detections) == 0 {
		return
	}

	shore, err := shorelineForGraph(config)
	if err != nil {
		fmt.Printf("Note: no distance to shore this run: %v\n", err)
		return
	}
	if shore == nil {
		fmt.Println("Note: the detection graph masks no coastline, " +
			"so distance to shore was not measured")
		return
	}

	for at := range detections {
		detections[at].DistanceToShoreM =
			shore.distanceMetres(detections[at].Latitude, detections[at].Longitude)
		detections[at].ShoreMeasured = true
	}
	fmt.Printf("Shoreline:      %d detections measured against %d coastline vertices\n",
		len(detections), shore.vertices())
}

func formatShoreDistance(detection Detection) string {
	if !detection.ShoreMeasured {
		return ""
	}
	return strconv.FormatFloat(detection.DistanceToShoreM, 'f', 0, 64)
}

func distanceToSegmentMetres(ax, ay, bx, by float64) float64 {
	alongX, alongY := bx-ax, by-ay

	lengthSquared := alongX*alongX + alongY*alongY
	if lengthSquared == 0 {
		return math.Hypot(ax, ay)
	}

	along := -(ax*alongX + ay*alongY) / lengthSquared
	along = math.Max(0, math.Min(1, along))

	return math.Hypot(ax+along*alongX, ay+along*alongY)
}

func (s *shoreline) touchesBounds(box geoBounds) bool {
	corners := [4][2]float64{
		{box.West, box.South}, {box.East, box.South},
		{box.East, box.North}, {box.West, box.North},
	}

	for _, ring := range s.rings {
		for index := range ring {
			from := ring[index]
			if boundsHold(box, from) {
				return true
			}

			to := ring[(index+1)%len(ring)]
			for corner := range corners {
				if segmentsCross(from, to, corners[corner], corners[(corner+1)%4]) {
					return true
				}
			}
		}
	}
	return false
}

func boundsHold(box geoBounds, point [2]float64) bool {
	return point[0] >= box.West && point[0] <= box.East &&
		point[1] >= box.South && point[1] <= box.North
}

func segmentsCross(p1, p2, p3, p4 [2]float64) bool {
	return turnsLeft(p3, p4, p1) != turnsLeft(p3, p4, p2) &&
		turnsLeft(p1, p2, p3) != turnsLeft(p1, p2, p4)
}

func turnsLeft(a, b, p [2]float64) bool {
	return (b[0]-a[0])*(p[1]-a[1])-(b[1]-a[1])*(p[0]-a[0]) > 0
}
