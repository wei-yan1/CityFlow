-- Rider position is owned by Tracking (Redis GEO + trajectory ZSET). Keeping
-- coordinates in the Task-owned profile table would create a second source of
-- truth and let dispatch read stale data. Drop the dead columns and their
-- validation/index from the task schema.
ALTER TABLE rider_profiles
    DROP CHECK rider_latitude_check,
    DROP CHECK rider_longitude_check;

ALTER TABLE rider_profiles
    DROP INDEX rider_profiles_location_idx;

ALTER TABLE rider_profiles
    DROP COLUMN current_latitude,
    DROP COLUMN current_longitude,
    DROP COLUMN location_updated_at;
