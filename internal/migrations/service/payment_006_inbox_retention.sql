-- Purge scans PROCESSED inbox rows by processed_at. The existing
-- (status, updated_at) index cannot serve that range efficiently.
-- Conditional so a partially migrated development database is safe to resume.
SET @cityflow_index_exists = (
  SELECT COUNT(*) FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'payment_inbox'
    AND INDEX_NAME = 'payment_inbox_retention_idx'
);
SET @cityflow_sql = IF(@cityflow_index_exists = 0,
  'ALTER TABLE payment_inbox ADD KEY payment_inbox_retention_idx (status, processed_at)',
  'SET @cityflow_noop = 1');
PREPARE cityflow_stmt FROM @cityflow_sql;
EXECUTE cityflow_stmt;
DEALLOCATE PREPARE cityflow_stmt;
