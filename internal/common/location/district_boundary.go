package location

import "math"

// DistrictContains checks a point against the separate rings returned by the
// Amap district API. Both the point and rings must use the same coordinate
// system. Ring boundaries count as inside. An invalid ring is rejected in
// full rather than being silently shortened into a different polygon.
//
// Amap's district polyline represents separate regions as separate rings;
// this function does not interpret nested rings as GeoJSON holes.
func DistrictContains(rings [][]Point, point Point) bool {
	if !point.Valid() {
		return false
	}
	for _, ring := range rings {
		if ringContains(ring, point) {
			return true
		}
	}
	return false
}

func ringContains(ring []Point, point Point) bool {
	if len(ring) < 3 {
		return false
	}
	for _, vertex := range ring {
		if !vertex.Valid() {
			return false
		}
	}
	var twiceArea float64
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		twiceArea += ring[j].Longitude*ring[i].Latitude - ring[i].Longitude*ring[j].Latitude
	}
	if math.Abs(twiceArea) < 1e-14 {
		return false
	}
	inside := false
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		a, b := ring[j], ring[i]
		if pointOnSegment(point, a, b) {
			return true
		}
		if (a.Latitude > point.Latitude) != (b.Latitude > point.Latitude) {
			intersection := a.Longitude + (point.Latitude-a.Latitude)*(b.Longitude-a.Longitude)/(b.Latitude-a.Latitude)
			if point.Longitude < intersection {
				inside = !inside
			}
		}
	}
	return inside
}

func pointOnSegment(p, a, b Point) bool {
	const tolerance = 1e-10
	dx, dy := b.Longitude-a.Longitude, b.Latitude-a.Latitude
	length := math.Hypot(dx, dy)
	if length == 0 {
		return math.Hypot(p.Longitude-a.Longitude, p.Latitude-a.Latitude) <= tolerance
	}
	cross := (p.Longitude-a.Longitude)*dy - (p.Latitude-a.Latitude)*dx
	if math.Abs(cross) > tolerance*length {
		return false
	}
	return p.Longitude >= math.Min(a.Longitude, b.Longitude)-tolerance &&
		p.Longitude <= math.Max(a.Longitude, b.Longitude)+tolerance &&
		p.Latitude >= math.Min(a.Latitude, b.Latitude)-tolerance &&
		p.Latitude <= math.Max(a.Latitude, b.Latitude)+tolerance
}
