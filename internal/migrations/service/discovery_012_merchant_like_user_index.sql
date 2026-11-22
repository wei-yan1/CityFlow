-- The user's "my likes" summary filters by user_id across all months. The
-- unique key starts with merchant_id, so it cannot serve that range.
SET @has_merchant_like_user_index = (
    SELECT COUNT(*) FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'merchant_like'
      AND INDEX_NAME = 'merchant_like_user_month_idx'
);
SET @add_merchant_like_user_index = IF(@has_merchant_like_user_index = 0,
    'ALTER TABLE merchant_like ADD KEY merchant_like_user_month_idx (user_id, like_month)',
    'SET @cityflow_noop = 1');
PREPARE cityflow_add_merchant_like_user_index FROM @add_merchant_like_user_index;
EXECUTE cityflow_add_merchant_like_user_index;
DEALLOCATE PREPARE cityflow_add_merchant_like_user_index;
