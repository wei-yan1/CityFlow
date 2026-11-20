// Package postgis serves the Guilin map layers that were imported from
// OpenStreetMap into PostgreSQL/PostGIS. It is read-only: the business
// database stays on MySQL and this store never writes.
package postgis

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"cityflow/internal/domain/location"
	"cityflow/internal/infrastructure/maps"
)

// Store queries the guilin_poi / guilin_road / guilin_admin / guilin_boundary
// tables created by scripts/map/guilin_schema.sql.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds the connection pool without a round trip, so callers that
// treat PostGIS as a fallback can finish startup immediately and Ping in the
// background.
func NewStore(dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(strings.TrimSpace(dsn))
	if err != nil {
		return nil, fmt.Errorf("解析地图库连接失败")
	}
	if cfg.MaxConns < 2 || cfg.MaxConns > 8 {
		cfg.MaxConns = 4
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("连接地图库失败")
	}
	return &Store{pool: pool}, nil
}

// Ping verifies the database is reachable; it is safe to call from a
// background goroutine after NewStore.
func (s *Store) Ping(ctx context.Context, timeout time.Duration) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("地图库未初始化")
	}
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := s.pool.Ping(pingCtx); err != nil {
		return fmt.Errorf("地图库不可用: %w", err)
	}
	return nil
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	store, err := NewStore(dsn)
	if err != nil {
		return nil, err
	}
	if err := store.Ping(ctx, 5*time.Second); err != nil {
		store.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) query(ctx context.Context, sql string, args ...any) ([]location.Place, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	places := make([]location.Place, 0, 20)
	for rows.Next() {
		var source string
		var osmID int64
		var place location.Place
		if err := rows.Scan(&source, &osmID, &place.Name, &place.Category, &place.Address, &place.Location.Latitude, &place.Location.Longitude); err != nil {
			return nil, err
		}
		place.ID = placeID(source, osmID)
		if place.Name == "" || !place.Location.Valid() {
			continue
		}
		places = append(places, place)
	}
	return places, rows.Err()
}

// placeSelect is shared by keyword search and radius search. Columns:
// source, osm_id, name, category, address, latitude, longitude.
const placeSelect = `
SELECT p.source, p.osm_id, coalesce(p.name, ''), coalesce(p.category, ''),
       trim(coalesce(p.street, '') || ' ' || coalesce(p.housenumber, '')),
       ST_Y(p.geom), ST_X(p.geom)
FROM guilin_poi p`

func (s *Store) SearchPlaces(ctx context.Context, keyword string, center location.Point, limit int) ([]location.Place, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	pattern := "%" + keyword + "%"
	sql := placeSelect + `
WHERE p.name IS NOT NULL AND p.name <> ''
  AND (p.name ILIKE $1 OR coalesce(p.street, '') ILIKE $1)
ORDER BY CASE WHEN p.name = $2 THEN 0 WHEN p.name ILIKE $3 THEN 1 ELSE 2 END,
         length(p.name),
         ST_Distance(p.geom::geography, ST_SetSRID(ST_MakePoint($5, $4), 4326)::geography)
LIMIT $6`
	return s.query(ctx, sql, pattern, keyword, keyword+"%", center.Latitude, center.Longitude, limit)
}

func (s *Store) NearbyPlaces(ctx context.Context, center location.Point, radiusMeters float64, limit int) ([]location.Place, error) {
	if radiusMeters <= 0 || radiusMeters > 5000 {
		radiusMeters = 500
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	sql := placeSelect + `
WHERE p.name IS NOT NULL AND p.name <> ''
  AND ST_DWithin(p.geom::geography, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography, $3)
ORDER BY ST_Distance(p.geom::geography, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography)
LIMIT $4`
	return s.query(ctx, sql, center.Latitude, center.Longitude, radiusMeters, limit)
}

// Place looks up a single stored feature by its layer and OSM id.
func (s *Store) Place(ctx context.Context, source string, osmID int64) (location.Place, bool, error) {
	if source != "node" && source != "way" {
		return location.Place{}, false, nil
	}
	sql := placeSelect + `
WHERE p.source = $1 AND p.osm_id = $2
LIMIT 1`
	places, err := s.query(ctx, sql, source, osmID)
	if err != nil || len(places) == 0 {
		return location.Place{}, false, err
	}
	return places[0], true, nil
}

// Boundary returns the outer rings of the requested administrative area.
// 450300 is the Guilin city boundary; district codes resolve by name.
func (s *Store) Boundary(ctx context.Context, adcode string) (*maps.Boundary, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if strings.TrimSpace(adcode) == "450300" {
		var name, geoJSON string
		var centerLat, centerLon float64
		err := s.pool.QueryRow(ctx, `
SELECT name, ST_AsGeoJSON(geom), ST_Y(ST_Centroid(geom)), ST_X(ST_Centroid(geom))
FROM guilin_boundary LIMIT 1`).Scan(&name, &geoJSON, &centerLat, &centerLon)
		if err != nil {
			return nil, err
		}
		rings, err := ringsFromGeoJSON(geoJSON)
		if err != nil {
			return nil, err
		}
		return &maps.Boundary{Name: name, Adcode: adcode, Center: location.Point{Latitude: centerLat, Longitude: centerLon}, Rings: rings}, nil
	}
	name, ok := adcodeNames[adcode]
	if !ok {
		return nil, nil
	}
	var areaName, geoJSON string
	var centerLat, centerLon float64
	err := s.pool.QueryRow(ctx, `
SELECT name, ST_AsGeoJSON(geom), ST_Y(ST_Centroid(geom)), ST_X(ST_Centroid(geom))
FROM guilin_admin
WHERE name = $1 AND admin_level = '6'
ORDER BY ST_Area(geom) DESC
LIMIT 1`, name).Scan(&areaName, &geoJSON, &centerLat, &centerLon)
	if err != nil {
		return nil, err
	}
	rings, err := ringsFromGeoJSON(geoJSON)
	if err != nil {
		return nil, err
	}
	return &maps.Boundary{Name: areaName, Adcode: adcode, Center: location.Point{Latitude: centerLat, Longitude: centerLon}, Rings: rings}, nil
}

// City resolves the containing prefecture-level city for a WGS84 point.
func (s *Store) City(ctx context.Context, p location.Point) (string, string, *location.Point, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pt := "ST_SetSRID(ST_MakePoint($1, $2), 4326)"
	var name string
	var centerLat, centerLon float64
	err := s.pool.QueryRow(ctx, `
SELECT name, ST_Y(ST_Centroid(geom)), ST_X(ST_Centroid(geom))
FROM guilin_boundary WHERE ST_Contains(geom, `+pt+`) LIMIT 1`, p.Longitude, p.Latitude).Scan(&name, &centerLat, &centerLon)
	if err == nil {
		center := location.Point{Latitude: centerLat, Longitude: centerLon}
		return name, "450300", &center, nil
	}
	err = s.pool.QueryRow(ctx, `
SELECT name FROM planet_osm_polygon
WHERE boundary = 'administrative' AND admin_level = '5' AND ST_Contains(way, `+pt+`)
LIMIT 1`, p.Longitude, p.Latitude).Scan(&name)
	if err != nil {
		return "", "", nil, nil
	}
	return name, "", nil, nil
}

// ReverseAddress prefers a nearby named street, then a nearby POI.
func (s *Store) ReverseAddress(ctx context.Context, p location.Point, radiusMeters float64) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pt := "ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography"
	var name string
	err := s.pool.QueryRow(ctx, `
SELECT name FROM guilin_road
WHERE name IS NOT NULL AND name <> '' AND ST_DWithin(geom::geography, `+pt+`, $3)
ORDER BY ST_Distance(geom::geography, `+pt+`)
LIMIT 1`, p.Longitude, p.Latitude, radiusMeters).Scan(&name)
	if err == nil && name != "" {
		return name + "附近", nil
	}
	err = s.pool.QueryRow(ctx, `
SELECT name FROM guilin_poi
WHERE name IS NOT NULL AND name <> '' AND ST_DWithin(geom::geography, `+pt+`, 150)
ORDER BY ST_Distance(geom::geography, `+pt+`)
LIMIT 1`, p.Longitude, p.Latitude).Scan(&name)
	if err == nil && name != "" {
		return name + "附近", nil
	}
	return "桂林市", nil
}

// RoadWays loads the whole road network once for local path finding.
func (s *Store) RoadWays(ctx context.Context) ([]maps.RoadWay, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	rows, err := s.pool.Query(ctx, `
SELECT osm_id, coalesce(name, ''), coalesce(highway, ''),
       coalesce(tags->'oneway', ''), coalesce(tags->'junction', ''), ST_AsGeoJSON(geom)
FROM guilin_road
WHERE geom IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ways := make([]maps.RoadWay, 0, 40000)
	for rows.Next() {
		var way maps.RoadWay
		var oneway, junction, geoJSON string
		if err := rows.Scan(&way.ID, &way.Name, &way.Highway, &oneway, &junction, &geoJSON); err != nil {
			return nil, err
		}
		way.OneWay = parseOneWay(oneway, junction)
		lines, err := linesFromGeoJSON(geoJSON)
		if err != nil {
			continue
		}
		way.Points = lines
		ways = append(ways, way)
	}
	return ways, rows.Err()
}

func parseOneWay(oneway, junction string) int8 {
	switch strings.ToLower(strings.TrimSpace(oneway)) {
	case "yes", "true", "1":
		return 1
	case "-1", "reverse":
		return -1
	}
	if strings.EqualFold(strings.TrimSpace(junction), "roundabout") {
		return 1
	}
	return 0
}

func placeID(source string, osmID int64) string {
	if source == "way" {
		return "osm:w" + strconv.FormatInt(osmID, 10)
	}
	return "osm:n" + strconv.FormatInt(osmID, 10)
}

type geoJSONGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

func ringsFromGeoJSON(raw string) ([][]location.Point, error) {
	var g geoJSONGeometry
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return nil, fmt.Errorf("行政区边界数据无法解析")
	}
	var polygons [][][][]float64
	switch g.Type {
	case "Polygon":
		var polygon [][][]float64
		if err := json.Unmarshal(g.Coordinates, &polygon); err != nil {
			return nil, err
		}
		polygons = append(polygons, polygon)
	case "MultiPolygon":
		if err := json.Unmarshal(g.Coordinates, &polygons); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("行政区边界类型不支持")
	}
	rings := make([][]location.Point, 0, len(polygons))
	for _, polygon := range polygons {
		if len(polygon) == 0 {
			continue
		}
		// Only the outer ring: holes are rare here and the caller treats any
		// ring as inside, so a hole would invert its meaning.
		rings = append(rings, ringPoints(polygon[0]))
	}
	return rings, nil
}

func linesFromGeoJSON(raw string) ([]location.Point, error) {
	var g geoJSONGeometry
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return nil, err
	}
	switch g.Type {
	case "LineString":
		var line [][]float64
		if err := json.Unmarshal(g.Coordinates, &line); err != nil {
			return nil, err
		}
		return ringPoints(line), nil
	case "MultiLineString":
		var parts [][][]float64
		if err := json.Unmarshal(g.Coordinates, &parts); err != nil {
			return nil, err
		}
		var all []location.Point
		for _, part := range parts {
			all = append(all, ringPoints(part)...)
		}
		return all, nil
	default:
		return nil, fmt.Errorf("道路几何类型不支持")
	}
}

func ringPoints(coords [][]float64) []location.Point {
	points := make([]location.Point, 0, len(coords))
	for _, pair := range coords {
		if len(pair) != 2 {
			continue
		}
		p := location.Point{Longitude: pair[0], Latitude: pair[1]}
		if !p.Valid() {
			continue
		}
		if len(points) > 0 && points[len(points)-1] == p {
			continue
		}
		points = append(points, p)
	}
	return points
}

// Official adcodes for the counties and districts under Guilin. OSM stores the
// names without codes, so the mapping lives here rather than in the database.
var adcodeNames = map[string]string{
	"450302": "秀峰区", "450303": "叠彩区", "450304": "象山区", "450305": "七星区",
	"450311": "雁山区", "450312": "临桂区", "450321": "阳朔县", "450322": "临桂区",
	"450323": "灵川县", "450324": "全州县", "450325": "兴安县", "450326": "永福县",
	"450327": "灌阳县", "450328": "龙胜各族自治县", "450329": "资源县", "450330": "平乐县",
	"450331": "荔浦市", "450332": "恭城瑶族自治县",
}
