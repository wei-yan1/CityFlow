package maps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cityflow/internal/common/location"
)

// Amap accepts and returns GCJ-02. Domain task/rider coordinates remain WGS84;
// conversion belongs to the map boundary, never to position storage.
type Amap struct {
	key, baseURL string
	client       *http.Client
}
type SearchResult struct {
	Provider         string           `json:"provider"`
	CoordinateSystem string           `json:"coordinate_system"`
	Data             []location.Place `json:"data"`
}
type RidingRoute struct {
	Provider         string           `json:"provider"`
	CoordinateSystem string           `json:"coordinate_system"`
	Mode             string           `json:"mode"`
	Coordinates      []location.Point `json:"coordinates"`
	DistanceMeters   int              `json:"distance_meters"`
	DurationSeconds  int              `json:"duration_seconds"`
	Instructions     []string         `json:"instructions"`
	Alternatives     []RoutePath      `json:"alternatives,omitempty"`
}

// RoutePath is one selectable route returned by the provider. The top-level
// RidingRoute fields mirror the first path for existing clients.
type RoutePath struct {
	Mode            string           `json:"mode"`
	Index           int              `json:"index"`
	Label           string           `json:"label"`
	Coordinates     []location.Point `json:"coordinates"`
	DistanceMeters  int              `json:"distance_meters"`
	DurationSeconds int              `json:"duration_seconds"`
	Instructions    []string         `json:"instructions"`
}

func NewAmap(key string) *Amap {
	return &Amap{key: key, baseURL: "https://restapi.amap.com", client: &http.Client{Timeout: 10 * time.Second}}
}

func (a *Amap) get(ctx context.Context, path string, params url.Values, target any) error {
	params.Set("key", a.key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+path+"?"+params.Encode(), nil)
	if err != nil {
		return fmt.Errorf("高德请求无法创建")
	}
	response, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("高德地图服务连接失败或超时")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("高德地图服务暂时不可用 (%d)", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(target); err != nil {
		return fmt.Errorf("高德地图返回数据无法解析")
	}
	return nil
}

func (a *Amap) Search(ctx context.Context, keywords, city string) (*SearchResult, error) {
	var body struct {
		Status string `json:"status"`
		Info   string `json:"info"`
		POIs   []struct {
			ID       string          `json:"id"`
			Name     string          `json:"name"`
			Address  json.RawMessage `json:"address"`
			Location string          `json:"location"`
		} `json:"pois"`
	}
	if err := a.get(ctx, "/v3/place/text", url.Values{"keywords": {keywords}, "city": {city}, "citylimit": {"true"}, "offset": {"10"}, "page": {"1"}, "extensions": {"base"}}, &body); err != nil {
		return nil, err
	}
	if body.Status != "1" {
		return nil, providerError(body.Info)
	}
	result := &SearchResult{Provider: "amap", CoordinateSystem: "GCJ02", Data: make([]location.Place, 0)}
	for _, poi := range body.POIs {
		p, ok := parsePoint(poi.Location)
		if !ok {
			continue
		}
		var address string
		_ = json.Unmarshal(poi.Address, &address)
		result.Data = append(result.Data, location.Place{ID: poi.ID, Name: poi.Name, Address: address, Category: "POI", Location: p})
	}
	return result, nil
}

func (a *Amap) PlaceByID(ctx context.Context, id string) (*SearchResult, error) {
	var body struct {
		Status string `json:"status"`
		Info   string `json:"info"`
		POIs   []struct {
			ID       string          `json:"id"`
			Name     string          `json:"name"`
			Address  json.RawMessage `json:"address"`
			Location string          `json:"location"`
		} `json:"pois"`
	}
	if err := a.get(ctx, "/v3/place/detail", url.Values{"id": {id}, "extensions": {"base"}}, &body); err != nil {
		return nil, err
	}
	if body.Status != "1" {
		return nil, providerError(body.Info)
	}
	result := &SearchResult{Provider: "amap", CoordinateSystem: "GCJ02", Data: make([]location.Place, 0)}
	for _, poi := range body.POIs {
		p, ok := parsePoint(poi.Location)
		if !ok || poi.ID != id {
			continue
		}
		var address string
		_ = json.Unmarshal(poi.Address, &address)
		result.Data = append(result.Data, location.Place{ID: poi.ID, Name: poi.Name, Address: address, Category: "POI", Location: p})
	}
	return result, nil
}

func (a *Amap) Nearby(ctx context.Context, p location.Point) (*SearchResult, error) {
	var body struct {
		Status string `json:"status"`
		Info   string `json:"info"`
		POIs   []struct {
			ID       string          `json:"id"`
			Name     string          `json:"name"`
			Address  json.RawMessage `json:"address"`
			Location string          `json:"location"`
		} `json:"pois"`
	}
	q := url.Values{"location": {strconv.FormatFloat(p.Longitude, 'f', 6, 64) + "," + strconv.FormatFloat(p.Latitude, 'f', 6, 64)}, "radius": {"500"}, "sortrule": {"distance"}, "offset": {"20"}, "page": {"1"}, "extensions": {"base"}}
	if err := a.get(ctx, "/v3/place/around", q, &body); err != nil {
		return nil, err
	}
	if body.Status != "1" {
		return nil, providerError(body.Info)
	}
	result := &SearchResult{Provider: "amap", CoordinateSystem: "GCJ02", Data: make([]location.Place, 0, len(body.POIs))}
	for _, poi := range body.POIs {
		point, ok := parsePoint(poi.Location)
		if !ok {
			continue
		}
		var address string
		_ = json.Unmarshal(poi.Address, &address)
		result.Data = append(result.Data, location.Place{ID: poi.ID, Name: poi.Name, Address: address, Category: "POI", Location: point})
	}
	return result, nil
}

func (a *Amap) Riding(ctx context.Context, origin, destination location.Point) (*RidingRoute, error) {
	return a.RidingMode(ctx, "bicycling", origin, destination)
}

func (a *Amap) RidingMode(ctx context.Context, mode string, origin, destination location.Point) (*RidingRoute, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "bicycling"
	}
	encode := func(p location.Point) string {
		return strconv.FormatFloat(p.Longitude, 'f', 6, 64) + "," + strconv.FormatFloat(p.Latitude, 'f', 6, 64)
	}
	var paths []RoutePath
	switch mode {
	case "bicycling":
		var body struct {
			ErrCode int    `json:"errcode"`
			ErrMsg  string `json:"errmsg"`
			Info    string `json:"info"`
			Data    struct {
				Paths []struct {
					Distance int `json:"distance"`
					Duration int `json:"duration"`
					Steps    []struct {
						Polyline    string `json:"polyline"`
						Instruction string `json:"instruction"`
					} `json:"steps"`
				} `json:"paths"`
			} `json:"data"`
		}
		if err := a.get(ctx, "/v4/direction/bicycling", url.Values{"origin": {encode(origin)}, "destination": {encode(destination)}}, &body); err != nil {
			return nil, err
		}
		if body.ErrCode != 0 || body.Info != "" {
			return nil, providerError(body.ErrMsg + body.Info)
		}
		for index, path := range body.Data.Paths {
			var coords []location.Point
			var instructions []string
			for _, step := range path.Steps {
				if step.Instruction != "" {
					instructions = append(instructions, step.Instruction)
				}
				coords = appendPolyline(coords, step.Polyline)
			}
			if len(coords) < 2 {
				continue
			}
			paths = append(paths, makePath(mode, index, path.Distance, path.Duration, coords, instructions))
		}
	case "electrobike", "driving":
		path := "/v5/direction/electrobike"
		if mode == "driving" {
			path = "/v5/direction/driving"
		}
		var body struct {
			Status string `json:"status"`
			Info   string `json:"info"`
			Route  struct {
				Paths []struct {
					Distance string `json:"distance"`
					Duration string `json:"duration"`
					Cost     struct {
						Duration string `json:"duration"`
					} `json:"cost"`
					Steps []struct {
						Polyline     string          `json:"polyline"`
						Instruction  string          `json:"instruction"`
						StepDistance json.RawMessage `json:"step_distance"`
						Navi         struct {
							Action string `json:"action"`
						} `json:"navi"`
					} `json:"steps"`
				} `json:"paths"`
			} `json:"route"`
		}
		if err := a.get(ctx, path, url.Values{
			"origin": {encode(origin)}, "destination": {encode(destination)},
			"alternative_route": {"3"}, "show_fields": {"cost,polyline,navi"},
		}, &body); err != nil {
			return nil, err
		}
		if body.Status != "1" {
			return nil, providerError(body.Info)
		}
		for index, path := range body.Route.Paths {
			distance, _ := strconv.Atoi(path.Distance)
			duration, _ := strconv.Atoi(path.Duration)
			if duration == 0 {
				duration, _ = strconv.Atoi(path.Cost.Duration)
			}
			var coords []location.Point
			var instructions []string
			for _, step := range path.Steps {
				if step.Instruction != "" {
					instructions = append(instructions, step.Instruction)
				} else if step.Navi.Action != "" {
					instructions = append(instructions, step.Navi.Action)
				}
				if distance == 0 {
					distance += flexibleInt(step.StepDistance)
				}
				coords = appendPolyline(coords, step.Polyline)
			}
			if len(coords) < 2 {
				continue
			}
			paths = append(paths, makePath(mode, index, distance, duration, coords, instructions))
		}
	default:
		return nil, fmt.Errorf("不支持的路线方式")
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("高德未找到可用路线，请调整起终点")
	}
	first := paths[0]
	route := &RidingRoute{Provider: "amap", CoordinateSystem: "GCJ02", Mode: mode, Coordinates: first.Coordinates, DistanceMeters: first.DistanceMeters, DurationSeconds: first.DurationSeconds, Instructions: first.Instructions, Alternatives: paths}
	return route, nil
}

func makePath(mode string, index, distance, duration int, coords []location.Point, instructions []string) RoutePath {
	label := map[string]string{"bicycling": "骑行", "electrobike": "电动车", "driving": "驾车"}[mode]
	if label == "" {
		label = mode
	}
	return RoutePath{Mode: mode, Index: index, Label: fmt.Sprintf("%s方案 %d", label, index+1), DistanceMeters: distance, DurationSeconds: duration, Coordinates: coords, Instructions: instructions}
}

func appendPolyline(target []location.Point, raw string) []location.Point {
	for _, piece := range strings.Split(raw, ";") {
		p, ok := parsePoint(strings.TrimSpace(piece))
		if !ok {
			continue
		}
		if len(target) == 0 || target[len(target)-1] != p {
			target = append(target, p)
		}
	}
	return target
}

// AMap v5 endpoints are inconsistent: driving returns step_distance as a JSON
// string while electrobike returns it as a number. Accept both without turning
// a valid route into a decode error.
func flexibleInt(raw json.RawMessage) int {
	value, _ := strconv.Atoi(strings.Trim(string(raw), `"`))
	return value
}

func parsePoint(raw string) (location.Point, bool) {
	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return location.Point{}, false
	}
	lon, e1 := strconv.ParseFloat(parts[0], 64)
	lat, e2 := strconv.ParseFloat(parts[1], 64)
	p := location.Point{Latitude: lat, Longitude: lon}
	return p, e1 == nil && e2 == nil && p.Valid()
}
func providerError(code string) error {
	// Return a bounded provider code, never the upstream request URL containing its key.
	if len(code) > 120 {
		code = code[:120]
	}
	return fmt.Errorf("高德服务拒绝请求，请检查 Key 类型、服务权限或配额 (%s)", code)
}
