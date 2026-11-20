-- Rider repository records archive state. Older Task databases created the
-- profile table without this column, so add it once and add the archive lookup
-- indexes that the repository expects.
SET @has_rider_archived = (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'rider_profiles' AND COLUMN_NAME = 'is_archived'
);
SET @add_rider_archived = IF(@has_rider_archived = 0,
  'ALTER TABLE rider_profiles ADD COLUMN is_archived TINYINT(1) NOT NULL DEFAULT 0, ADD KEY rider_profiles_archive_created_idx (is_archived, created_at, id), ADD KEY rider_profiles_archive_status_created_idx (is_archived, status, created_at, id)',
  'SET @cityflow_noop = 1');
PREPARE cityflow_add_rider_archived FROM @add_rider_archived;
EXECUTE cityflow_add_rider_archived;
DEALLOCATE PREPARE cityflow_add_rider_archived;
