-- Reaction owns like state. Task stores the absolute rider-feedback like count
-- pushed by Reaction's MQ batch so the public profile never counts its own
-- reaction rows.
SET @has_like_count = (
    SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'rider_feedback' AND COLUMN_NAME = 'like_count'
);
SET @add_like_count = IF(@has_like_count = 0,
    'ALTER TABLE rider_feedback ADD COLUMN like_count INT NOT NULL DEFAULT 0 AFTER comment',
    'SELECT 1'
);
PREPARE cityflow_add_feedback_like_count FROM @add_like_count;
EXECUTE cityflow_add_feedback_like_count;
DEALLOCATE PREPARE cityflow_add_feedback_like_count;
