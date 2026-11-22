package maps

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"
)

// Boundary is an administrative outline in WGS84, as stored in PostGIS.
type Boundary struct {
	Name   string
	Adcode string
	Center location.Point
	Rings  [][]location.Point
}

// RoadWay is one OSM way of the local road network.
type RoadWay struct {
	ID      int64
	Name    string
	Highway string
	OneWay  int8 // 0 both directions, 1 forward only, -1 backward only
	Points  []location.Point
}

// LocalSource is the read-only lookup surface backed by the Guilin PostGIS
// tables. All points crossing this boundary are WGS84.
type LocalSource interface {
	SearchPlaces(ctx context.Context, keyword string, center location.Point, limit int) ([]location.Place, error)
	NearbyPlaces(ctx context.Context, center location.Point, radiusMeters float64, limit int) ([]location.Place, error)
	Place(ctx context.Context, source string, osmID int64) (location.Place, bool, error)
	Boundary(ctx context.Context, adcode string) (*Boundary, error)
	City(ctx context.Context, p location.Point) (name string, adcode string, center *location.Point, err error)
	ReverseAddress(ctx context.Context, p location.Point, radiusMeters float64) (string, error)
	RoadWays(ctx context.Context) ([]RoadWay, error)
}

// TrustedPlaceRecorder is the trusted-place write port used by the local
// provider. It is deliberately an interface so the shared maps package does
// not import any service's application layer.
type TrustedPlaceRecorder interface {
	RecordAndGet(context.Context, []location.Place) ([]location.Place, error)
}

// Local implements the map service interfaces on top of the local PostGIS
// data. It speaks GCJ02 on the outside because the browser converts every
// coordinate with the GCJ02 helpers; storage and the road graph stay WGS84.
type Local struct {
	Source   LocalSource
	Places   TrustedPlaceRecorder
	Adcode   string
	CityName string
	Center   location.Point // preferences for ranking, WGS84

	searchLimit  int
	nearbyRadius float64

	graphMu sync.Mutex
	graph   *roadGraph
}

func NewLocal(source LocalSource, places TrustedPlaceRecorder, city, adcode string) *Local {
	return &Local{Source: source, Places: places, CityName: city, Adcode: adcode, searchLimit: 10, nearbyRadius: 500}
}

// Warm builds the road graph ahead of the first route request.
func (l *Local) Warm(ctx context.Context) {
	l.graphMu.Lock()
	defer l.graphMu.Unlock()
	_, _ = l.roadGraph(ctx)
}

func (l *Local) roadGraph(ctx context.Context) (*roadGraph, error) {
	if l.graph != nil {
		return l.graph, nil
	}
	if l.Source == nil {
		return nil, fmt.Errorf("本地地图数据未配置")
	}
	ways, err := l.Source.RoadWays(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取本地路网失败")
	}
	g := buildRoadGraph(ways)
	if g == nil {
		return nil, fmt.Errorf("本地路网为空")
	}
	l.graph = g
	return g, nil
}

func (l *Local) Search(ctx context.Context, keywords, _ string) (*SearchResult, error) {
	keywords = strings.Join(strings.Fields(keywords), " ")
	if keywords == "" {
		return nil, fmt.Errorf("%w: 请输入地点名称", domain.ErrInvalidRequest)
	}
	places, err := l.Source.SearchPlaces(ctx, keywords, l.defaultCenter(), l.searchLimit)
	if err != nil {
		return nil, fmt.Errorf("%w: 本地地点检索失败", domain.ErrUnavailable)
	}
	result := &SearchResult{Provider: "local", CoordinateSystem: "GCJ02", Data: toGCJPlaces(places)}
	return l.record(ctx, result)
}

func (l *Local) Nearby(ctx context.Context, p location.Point) (*SearchResult, error) {
	center := GCJ02ToWGS84(p)
	places, err := l.Source.NearbyPlaces(ctx, center, l.nearbyRadius, 20)
	if err != nil {
		return nil, fmt.Errorf("%w: 本地附近地点查询失败", domain.ErrUnavailable)
	}
	result := &SearchResult{Provider: "local", CoordinateSystem: "GCJ02", Data: toGCJPlaces(places)}
	return l.record(ctx, result)
}

// VerifyPlace re-checks a local POI id for the trusted-place flow.
func (l *Local) VerifyPlace(ctx context.Context, id string) error {
	source, osmID, ok := parseLocalPlaceID(id)
	if !ok {
		return domain.ErrNotFound
	}
	place, found, err := l.Source.Place(ctx, source, osmID)
	if err != nil {
		return fmt.Errorf("%w: 本地地点查询失败", domain.ErrUnavailable)
	}
	if !found || strings.TrimSpace(place.Name) == "" {
		return domain.ErrNotFound
	}
	_, err = l.Places.RecordAndGet(ctx, []location.Place{place})
	return err
}

func (l *Local) Riding(ctx context.Context, origin, destination location.Point) (*RidingRoute, error) {
	return l.RidingMode(ctx, "bicycling", origin, destination)
}

func (l *Local) RidingMode(ctx context.Context, mode string, origin, destination location.Point) (*RidingRoute, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "", "bicycling":
		mode = "bicycling"
	case "electrobike", "driving":
	default:
		return nil, fmt.Errorf("%w: 不支持的路线方式", domain.ErrInvalidRequest)
	}
	l.graphMu.Lock()
	graph, err := l.roadGraph(ctx)
	l.graphMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrUnavailable, err)
	}
	from, to := GCJ02ToWGS84(origin), GCJ02ToWGS84(destination)
	coords, meters, instructions := graph.route(from, to, mode)
	if len(coords) < 2 {
		return nil, fmt.Errorf("%w: 未找到可用路线，请调整起终点", domain.ErrInvalidRequest)
	}
	gcj := make([]location.Point, 0, len(coords))
	for _, p := range coords {
		gcj = append(gcj, wgs84ToGCJ(p))
	}
	duration := int(meters / routeSpeed(mode))
	if duration < 60 {
		duration = 60
	}
	path := RoutePath{Mode: mode, Index: 0, Label: routeLabel(mode) + "方案 1", Coordinates: gcj, DistanceMeters: int(meters), DurationSeconds: duration, Instructions: instructions}
	route := &RidingRoute{
		Provider: "local", CoordinateSystem: "GCJ02", Mode: mode,
		Coordinates: gcj, DistanceMeters: int(meters), DurationSeconds: duration, Instructions: instructions,
		Alternatives: []RoutePath{path},
	}
	return route, nil
}

func (l *Local) City(ctx context.Context, p location.Point) (*City, error) {
	name, adcode, center, err := l.Source.City(ctx, GCJ02ToWGS84(p))
	if err != nil {
		return nil, fmt.Errorf("%w: 本地城市识别失败", domain.ErrUnavailable)
	}
	if name == "" {
		return nil, fmt.Errorf("%w: 未能识别当前位置所在城市", domain.ErrNotFound)
	}
	result := &City{Name: name, Adcode: adcode, CoordinateSystem: "GCJ02"}
	if center != nil {
		converted := wgs84ToGCJ(*center)
		result.Center = &converted
	}
	return result, nil
}

func (l *Local) District(ctx context.Context, code string) (*District, error) {
	boundary, err := l.Source.Boundary(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("%w: 本地行政区边界读取失败", domain.ErrUnavailable)
	}
	if boundary == nil || len(boundary.Rings) == 0 {
		return nil, fmt.Errorf("%w: 未找到城市行政区边界", domain.ErrNotFound)
	}
	rings := make([][]location.Point, 0, len(boundary.Rings))
	for _, ring := range boundary.Rings {
		converted := make([]location.Point, 0, len(ring))
		for _, p := range ring {
			converted = append(converted, wgs84ToGCJ(p))
		}
		if len(converted) >= 3 {
			rings = append(rings, converted)
		}
	}
	if len(rings) == 0 {
		return nil, fmt.Errorf("%w: 行政区边界数据无效", domain.ErrUnavailable)
	}
	center := wgs84ToGCJ(boundary.Center)
	return &District{City: City{Name: boundary.Name, Adcode: boundary.Adcode, Center: &center, CoordinateSystem: "GCJ02"}, Boundaries: rings}, nil
}

func (l *Local) ReverseAddress(ctx context.Context, p location.Point) (string, error) {
	address, err := l.Source.ReverseAddress(ctx, GCJ02ToWGS84(p), 60)
	if err != nil || address == "" {
		return "", fmt.Errorf("%w: 未找到该地点的地址", domain.ErrNotFound)
	}
	return address, nil
}

func (l *Local) record(ctx context.Context, result *SearchResult) (*SearchResult, error) {
	if l.Places == nil {
		return result, nil
	}
	valid := make([]location.Place, 0, len(result.Data))
	for _, p := range result.Data {
		if p.ID != "" && strings.TrimSpace(p.Name) != "" && p.Location.Valid() {
			valid = append(valid, p)
		}
	}
	stored, err := l.Places.RecordAndGet(ctx, valid)
	if err != nil {
		return nil, err
	}
	result.Data = stored
	return result, nil
}

func (l *Local) defaultCenter() location.Point {
	if l.Center.Valid() {
		return l.Center
	}
	return location.Point{Latitude: 25.274, Longitude: 110.290}
}

func toGCJPlaces(places []location.Place) []location.Place {
	result := make([]location.Place, 0, len(places))
	for _, p := range places {
		p.Location = wgs84ToGCJ(p.Location)
		result = append(result, p)
	}
	return result
}

// Place ids are "osm:n<id>" for nodes and "osm:w<id>" for ways/polygons.
func parseLocalPlaceID(id string) (string, int64, bool) {
	rest, ok := strings.CutPrefix(id, "osm:")
	if !ok || len(rest) < 2 {
		return "", 0, false
	}
	source := "node"
	if rest[0] == 'w' {
		source = "way"
	} else if rest[0] != 'n' {
		return "", 0, false
	}
	number, err := strconv.ParseInt(rest[1:], 10, 64)
	if err != nil || number <= 0 {
		return "", 0, false
	}
	return source, number, true
}

// WGS84ToGCJ exposes the provider-side conversion for callers outside this
// package (tests and integration checks). Rendering code keeps using the
// browser helpers.
func WGS84ToGCJ(p location.Point) location.Point { return wgs84ToGCJ(p) }

func routeSpeed(mode string) float64 {
	switch mode {
	case "electrobike":
		return 6.9
	case "driving":
		return 8.3
	default:
		return 4.2
	}
}

func routeLabel(mode string) string {
	switch mode {
	case "electrobike":
		return "电动车"
	case "driving":
		return "驾车"
	default:
		return "骑行"
	}
}
