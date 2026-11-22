package maps

import "cityflow/internal/common/location"

// GCJ02ToWGS84 reverses the same transformation used for the service boundary.
func GCJ02ToWGS84(p location.Point) location.Point {
	result := p
	for i := 0; i < 8; i++ {
		converted := wgs84ToGCJ(result)
		result.Latitude -= converted.Latitude - p.Latitude
		result.Longitude -= converted.Longitude - p.Longitude
	}
	return result
}
