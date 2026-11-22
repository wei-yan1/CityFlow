-- Outbox maintenance deletes PUBLISHED rows by published_at.
-- Conditional so renamed or partially applied development migrations resume.
SET @cityflow_index_exists = (
  SELECT COUNT(*) FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'task_outbox'
    AND INDEX_NAME = 'task_outbox_retention_idx'
);
SET @cityflow_sql = IF(@cityflow_index_exists = 0,
  'ALTER TABLE task_outbox ADD KEY task_outbox_retention_idx (status, published_at)',
  'SET @cityflow_noop = 1');
PREPARE cityflow_stmt FROM @cityflow_sql;
EXECUTE cityflow_stmt;
DEALLOCATE PREPARE cityflow_stmt;
