-- Guilin map layers derived from an osm2pgsql import (OpenStreetMap, ODbL).
-- Prerequisite: osm2pgsql has populated planet_osm_point / planet_osm_line /
-- planet_osm_polygon in the same database, imported with --latlong and --hstore.
-- Run against the map database (cityflow_map), not the CityFlow business DB.

DROP TABLE IF EXISTS guilin_poi, guilin_road, guilin_admin, guilin_boundary;

-- Guilin city boundary. OSM tags Chinese prefecture-level cities as admin_level=5.
CREATE TABLE guilin_boundary AS
SELECT osm_id, name, admin_level, way AS geom
FROM planet_osm_polygon
WHERE boundary = 'administrative' AND admin_level = '5' AND name = '桂林市';
ALTER TABLE guilin_boundary
  ALTER COLUMN geom TYPE geometry(MultiPolygon, 4326) USING ST_Multi(geom);
CREATE INDEX guilin_boundary_geom_idx ON guilin_boundary USING GIST (geom);

-- POIs: nodes first, then polygon features (schools, hospitals, malls, parks)
-- reduced to a representative point so both share one query surface.
CREATE TABLE guilin_poi AS
SELECT p.osm_id,
       p.name,
       coalesce(p.amenity, p.shop, p.tourism, p.office, p.tags->'healthcare') AS category,
       p.amenity, p.shop, p.tourism, p.office,
       p.tags->'addr:street' AS street,
       p.tags->'addr:housenumber' AS housenumber,
       p.tags->'phone' AS phone,
       p.tags->'opening_hours' AS opening_hours,
       p.tags AS tags,
       p.way AS geom,
       'node' AS source
FROM planet_osm_point p, guilin_boundary b
WHERE ST_Within(p.way, b.geom)
  AND (p.amenity IS NOT NULL OR p.shop IS NOT NULL OR p.tourism IS NOT NULL
       OR p.office IS NOT NULL OR p.tags ? 'healthcare');
CREATE INDEX guilin_poi_geom_idx ON guilin_poi USING GIST (geom);
CREATE INDEX guilin_poi_geog_idx ON guilin_poi USING GIST ((geom::geography));
CREATE INDEX guilin_poi_category_idx ON guilin_poi (category);

INSERT INTO guilin_poi
SELECT p.osm_id,
       p.name,
       coalesce(p.amenity, p.shop, p.tourism, p.office, p.tags->'healthcare'),
       p.amenity, p.shop, p.tourism, p.office,
       p.tags->'addr:street', p.tags->'addr:housenumber',
       p.tags->'phone', p.tags->'opening_hours',
       p.tags, ST_PointOnSurface(p.way), 'way'
FROM planet_osm_polygon p, guilin_boundary b
WHERE ST_Within(p.way, b.geom)
  AND (p.amenity IS NOT NULL OR p.shop IS NOT NULL OR p.tourism IS NOT NULL
       OR p.office IS NOT NULL OR p.tags ? 'healthcare');

-- Road network, clipped to the city boundary.
CREATE TABLE guilin_road AS
SELECT l.osm_id, l.name, l.highway,
       l.tags->'surface' AS surface, l.tags->'lanes' AS lanes,
       l.tags AS tags, l.way AS geom
FROM planet_osm_line l, guilin_boundary b
WHERE l.highway IS NOT NULL AND ST_Intersects(l.way, b.geom);
CREATE INDEX guilin_road_geom_idx ON guilin_road USING GIST (geom);
CREATE INDEX guilin_road_geog_idx ON guilin_road USING GIST ((geom::geography));

-- City, county/district and township boundaries in one table.
CREATE TABLE guilin_admin AS
SELECT osm_id, name, admin_level, way AS geom
FROM planet_osm_polygon
WHERE boundary = 'administrative' AND admin_level IN ('5', '6', '8')
  AND ST_Intersects(way, (SELECT geom FROM guilin_boundary));
CREATE INDEX guilin_admin_geom_idx ON guilin_admin USING GIST (geom);

ANALYZE guilin_poi;
ANALYZE guilin_road;
ANALYZE guilin_admin;

-- Example: POIs within 2 km of the city centre, nearest first.
-- SELECT name, category,
--        round(ST_Distance(geom::geography, ST_SetSRID(ST_MakePoint(110.290, 25.274), 4326)::geography)) AS meters
-- FROM guilin_poi
-- WHERE ST_DWithin(geom::geography, ST_SetSRID(ST_MakePoint(110.290, 25.274), 4326)::geography, 2000)
-- ORDER BY meters LIMIT 20;
