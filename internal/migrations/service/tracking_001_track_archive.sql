-- Tracking keeps Redis for the live snapshot/GEO index and a short pending
-- trajectory ZSET. MQ consumers copy each reported point into this permanent
-- archive before removing the corresponding Redis ZSET member.
--
-- The old task-scoped table used (task_id, rider_id, version) as its identity.
-- Keep it as a legacy table instead of deleting it; the new archive is rider
-- scoped and keyed by (rider_id, version).
SET @legacy_unique = (
  SELECT COUNT(*) FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'rider_track_points'
    AND INDEX_NAME = 'uq_task_rider_version'
);
SET @sql = IF(@legacy_unique > 0,
  'RENAME TABLE rider_track_points TO rider_track_points_legacy_20261122',
  'SELECT 1');
PREPARE cityflow_track_legacy FROM @sql;
EXECUTE cityflow_track_legacy;
DEALLOCATE PREPARE cityflow_track_legacy;

CREATE TABLE IF NOT EXISTS rider_track_points (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    rider_id     VARCHAR(64) NOT NULL,
    task_id      VARCHAR(64) NULL,
    latitude     DOUBLE NOT NULL,
    longitude    DOUBLE NOT NULL,
    accuracy     DOUBLE NOT NULL,
    source       VARCHAR(16) NOT NULL DEFAULT 'device',
    observed_at  DATETIME(3) NOT NULL,
    updated_at   DATETIME(3) NOT NULL,
    version      BIGINT NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY rider_track_version (rider_id, version),
    KEY rider_track_observed (rider_id, observed_at, version),
    KEY rider_track_task (task_id, observed_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
