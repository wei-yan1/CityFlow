package maps

import (
	"cityflow/internal/common/location"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type City struct {
	Name             string          `json:"name"`
	Adcode           string          `json:"adcode"`
	Center           *location.Point `json:"center,omitempty"`
	CoordinateSystem string          `json:"coordinate_system"`
}
type District struct {
	City
	Boundaries [][]location.Point `json:"boundaries"`
}

// ReverseAddress is an explicit user-selected location; it is not cached.
func (a *Amap) ReverseAddress(ctx context.Context, p location.Point) (string, error) {
	var body struct {
		Status    string `json:"status"`
		Info      string `json:"info"`
		Regeocode struct {
			Address string `json:"formatted_address"`
		} `json:"regeocode"`
	}
	q := url.Values{"location": {strconv.FormatFloat(p.Longitude, 'f', 6, 64) + "," + strconv.FormatFloat(p.Latitude, 'f', 6, 64)}, "extensions": {"base"}}
	if err := a.get(ctx, "/v3/geocode/regeo", q, &body); err != nil {
		return "", err
	}
	if body.Status != "1" {
		return "", providerError(body.Info)
	}
	if body.Regeocode.Address == "" {
		return "", fmt.Errorf("未找到该地点的地址")
	}
	return body.Regeocode.Address, nil
}

func (a *Amap) City(ctx context.Context, p location.Point) (*City, error) {
	var body struct {
		Status    string `json:"status"`
		Info      string `json:"info"`
		Regeocode struct {
			Address struct {
				City     json.RawMessage `json:"city"`
				Province string          `json:"province"`
				Adcode   string          `json:"adcode"`
				Citycode json.RawMessage `json:"citycode"`
			} `json:"addressComponent"`
		} `json:"regeocode"`
	}
	query := url.Values{"location": {strconv.FormatFloat(p.Longitude, 'f', 5, 64) + "," + strconv.FormatFloat(p.Latitude, 'f', 5, 64)}, "extensions": {"base"}, "radius": {"1000"}}
	if err := a.get(ctx, "/v3/geocode/regeo", query, &body); err != nil {
		return nil, err
	}
	if body.Status != "1" {
		return nil, providerError(body.Info)
	}
	name := ""
	_ = json.Unmarshal(body.Regeocode.Address.City, &name)
	if name == "" {
		name = body.Regeocode.Address.Province
	}
	code := body.Regeocode.Address.Adcode
	if len(code) != 6 || name == "" {
		return nil, fmt.Errorf("未能识别当前位置所在城市")
	}
	// Municipality code XX0000; ordinary prefecture city XXXX00.
	if strings.HasPrefix(code, "11") || strings.HasPrefix(code, "12") || strings.HasPrefix(code, "31") || strings.HasPrefix(code, "50") {
		code = code[:2] + "0000"
	} else {
		code = code[:4] + "00"
	}
	return &City{Name: name, Adcode: code, CoordinateSystem: "GCJ02"}, nil
}
func (a *Amap) District(ctx context.Context, code string) (*District, error) {
	var body struct {
		Status    string `json:"status"`
		Info      string `json:"info"`
		Districts []struct {
			Name     string `json:"name"`
			Adcode   string `json:"adcode"`
			Center   string `json:"center"`
			Polyline string `json:"polyline"`
		} `json:"districts"`
	}
	if err := a.get(ctx, "/v3/config/district", url.Values{"keywords": {code}, "subdistrict": {"0"}, "extensions": {"all"}}, &body); err != nil {
		return nil, err
	}
	if body.Status != "1" {
		return nil, providerError(body.Info)
	}
	if len(body.Districts) == 0 {
		return nil, fmt.Errorf("未找到城市行政区边界")
	}
	d := body.Districts[0]
	if d.Adcode != code {
		return nil, fmt.Errorf("行政区结果与请求不一致")
	}
	result := &District{City: City{Name: d.Name, Adcode: d.Adcode, CoordinateSystem: "GCJ02"}, Boundaries: make([][]location.Point, 0)}
	if p, ok := parsePoint(d.Center); ok {
		result.Center = &p
	}
	total := 0
	for _, ring := range strings.Split(d.Polyline, "|") {
		points := []location.Point{}
		for _, raw := range strings.Split(ring, ";") {
			if p, ok := parsePoint(raw); ok {
				points = append(points, p)
				total++
			}
		}
		if len(points) >= 3 {
			result.Boundaries = append(result.Boundaries, points)
		}
	}
	if len(result.Boundaries) == 0 || total > 100000 {
		return nil, fmt.Errorf("行政区边界数据无效或过大")
	}
	return result, nil
}
