-- The city-center search cache moved to Redis with a one-minute TTL.
-- The former MySQL cache table is no longer part of the runtime path; drop it
-- for existing installations. Fresh installations never create it because the
-- creating migration was removed.
DROP TABLE IF EXISTS map_query_cache;
