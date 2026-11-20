package location

import (
	"fmt"
	"math"
)

type Bounds struct {
	West  float64 `json:"west"`
	South float64 `json:"south"`
	East  float64 `json:"east"`
	North float64 `json:"north"`
}

func (b Bounds) Valid() bool {
	return (Point{b.South, b.West}).Valid() && (Point{b.North, b.East}).Valid() && b.West < b.East && b.South < b.North && b.North-b.South <= 8 && b.East-b.West <= 8
}
func (b Bounds) Contains(p Point) bool {
	return p.Longitude >= b.West && p.Longitude <= b.East && p.Latitude >= b.South && p.Latitude <= b.North
}

const MapGridSize = 0.1

func GridID(p Point) string {
	return gridName(int(math.Floor(p.Longitude/MapGridSize)), int(math.Floor(p.Latitude/MapGridSize)))
}
func gridName(x, y int) string { return fmt.Sprintf("%d:%d", x, y) }

type MapCell struct {
	ID     string
	Bounds Bounds
}

func (b Bounds) Cells() []MapCell {
	result := []MapCell{}
	for x := int(math.Floor(b.West / MapGridSize)); x <= int(math.Floor(b.East/MapGridSize)); x++ {
		for y := int(math.Floor(b.South / MapGridSize)); y <= int(math.Floor(b.North/MapGridSize)); y++ {
			result = append(result, MapCell{gridName(x, y), Bounds{float64(x) * MapGridSize, float64(y) * MapGridSize, float64(x+1) * MapGridSize, float64(y+1) * MapGridSize}})
		}
	}
	return result
}

type MapMerchant struct {
	Place
	Count  int     `json:"count,omitempty"`
	Bounds *Bounds `json:"bounds,omitempty"`
}
type MerchantMap struct {
	Data             []MapMerchant `json:"data"`
	Truncated        bool          `json:"truncated"`
	CoordinateSystem string        `json:"coordinate_system"`
}
