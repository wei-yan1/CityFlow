package location

import (
	"math"
	"time"
)

type Point struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Place struct {
	ID               string `json:"id"`
	Source           string `json:"source,omitempty"`
	Name             string `json:"name"`
	Category         string `json:"category"`
	Address          string `json:"address"`
	Location         Point  `json:"location"`
	CoordinateSystem string `json:"coordinate_system,omitempty"`
	AMAPPOIID        string `json:"amap_poi_id,omitempty"`
}

func (p Point) Valid() bool {
	return !math.IsNaN(p.Latitude) && !math.IsNaN(p.Longitude) && !math.IsInf(p.Latitude, 0) && !math.IsInf(p.Longitude, 0) && p.Latitude >= -90 && p.Latitude <= 90 && p.Longitude >= -180 && p.Longitude <= 180
}

type Position struct {
	Source  string `json:"source"`
	RiderID string `json:"rider_id"`
	TaskID  string `json:"task_id,omitempty"`
	Point
	Accuracy   float64   `json:"accuracy"`
	ObservedAt time.Time `json:"observed_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Version    int64     `json:"version"`
}

type Report struct {
	Source string `json:"source,omitempty"`
	TaskID string `json:"task_id,omitempty"`
	Point
	Accuracy   float64   `json:"accuracy"`
	ObservedAt time.Time `json:"observed_at"`
}
