package maps

import (
	"cityflow/internal/domain"
	"cityflow/internal/domain/location"
	"context"
	"fmt"
	"math"
)

// ServiceArea checks WGS84 task coordinates against the provider's GCJ02 city polygon.
type ServiceArea struct {
	Districts interface {
		District(context.Context, string) (*District, error)
	}
	Adcode string
}

func (a ServiceArea) Validate(ctx context.Context, points []location.Point) error {
	if a.Districts == nil {
		return fmt.Errorf("%w: 桂林服务范围暂不可用", domain.ErrUnavailable)
	}
	district, err := a.Districts.District(ctx, a.Adcode)
	if err != nil {
		return fmt.Errorf("%w: 桂林服务范围查询失败", domain.ErrUnavailable)
	}
	if district == nil || len(district.Boundaries) == 0 {
		return fmt.Errorf("%w: 桂林服务范围数据无效", domain.ErrUnavailable)
	}
	for _, point := range points {
		if !containsRings(district.Boundaries, wgs84ToGCJ(point)) {
			return fmt.Errorf("%w: 取送地点必须位于桂林市服务范围内", domain.ErrInvalidRequest)
		}
	}
	return nil
}
func containsRings(rings [][]location.Point, p location.Point) bool {
	for _, ring := range rings {
		inside := false
		for i, j := 0, len(ring)-1; i < len(ring); i++ {
			a, b := ring[i], ring[j]
			if (a.Latitude > p.Latitude) != (b.Latitude > p.Latitude) && p.Longitude < (b.Longitude-a.Longitude)*(p.Latitude-a.Latitude)/(b.Latitude-a.Latitude)+a.Longitude {
				inside = !inside
			}
			j = i
		}
		if inside {
			return true
		}
	}
	return false
}

// Adapted from wandergis/coordtransform (MIT), already vendored in web/assets/vendor.
func wgs84ToGCJ(p location.Point) location.Point {
	lat, lng := p.Latitude, p.Longitude
	if !(lng > 73.66 && lng < 135.05 && lat > 3.86 && lat < 53.55) {
		return p
	}
	x, y := lng-105, lat-35
	dl := -100 + 2*x + 3*y + 0.2*y*y + 0.1*x*y + 0.2*math.Sqrt(math.Abs(x))
	dl += (20*math.Sin(6*x*math.Pi) + 20*math.Sin(2*x*math.Pi)) * 2 / 3
	dl += (20*math.Sin(y*math.Pi) + 40*math.Sin(y/3*math.Pi)) * 2 / 3
	dl += (160*math.Sin(y/12*math.Pi) + 320*math.Sin(y*math.Pi/30)) * 2 / 3
	dn := 300 + x + 2*y + 0.1*x*x + 0.1*x*y + 0.1*math.Sqrt(math.Abs(x))
	dn += (20*math.Sin(6*x*math.Pi) + 20*math.Sin(2*x*math.Pi)) * 2 / 3
	dn += (20*math.Sin(x*math.Pi) + 40*math.Sin(x/3*math.Pi)) * 2 / 3
	dn += (150*math.Sin(x/12*math.Pi) + 300*math.Sin(x/30*math.Pi)) * 2 / 3
	const axis = 6378245.0
	const eccentric = 0.00669342162296594323
	rad := lat / 180 * math.Pi
	magic := 1 - eccentric*math.Pow(math.Sin(rad), 2)
	root := math.Sqrt(magic)
	dl = dl * 180 / ((axis * (1 - eccentric)) / (magic * root) * math.Pi)
	dn = dn * 180 / (axis / root * math.Cos(rad) * math.Pi)
	return location.Point{Latitude: lat + dl, Longitude: lng + dn}
}
