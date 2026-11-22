package location

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
