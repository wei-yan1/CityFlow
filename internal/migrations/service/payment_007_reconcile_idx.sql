-- Reconcile scans PENDING / REFUND_PENDING / REFUND_FAILED by updated_at.
-- Conditional so renamed or partially applied development migrations resume.
SET @cityflow_index_exists = (
  SELECT COUNT(*) FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'payment_orders'
    AND INDEX_NAME = 'payment_orders_reconcile_idx'
);
SET @cityflow_sql = IF(@cityflow_index_exists = 0,
  'ALTER TABLE payment_orders ADD KEY payment_orders_reconcile_idx (status, updated_at)',
  'SET @cityflow_noop = 1');
PREPARE cityflow_stmt FROM @cityflow_sql;
EXECUTE cityflow_stmt;
DEALLOCATE PREPARE cityflow_stmt;
