-- Rider review comments mirror the Discovery comment projection: like_count is
-- maintained with the reaction write and the hot index serves the same
-- like_count DESC, created_at DESC ordering.
SET @has_rider_comment_like_count = (
    SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'rider_feedback_comments' AND COLUMN_NAME = 'like_count'
);
SET @add_rider_comment_like_count = IF(@has_rider_comment_like_count = 0,
    'ALTER TABLE rider_feedback_comments ADD COLUMN like_count INT NOT NULL DEFAULT 0 AFTER hidden',
    'SET @cityflow_noop = 1');
PREPARE cityflow_add_rider_comment_like_count FROM @add_rider_comment_like_count;
EXECUTE cityflow_add_rider_comment_like_count;
DEALLOCATE PREPARE cityflow_add_rider_comment_like_count;

UPDATE rider_feedback_comments c
LEFT JOIN (
    SELECT target_id, COUNT(*) AS like_total
    FROM reaction_records
    WHERE target_type = 'RIDER_COMMENT' AND kind = 'LIKE'
    GROUP BY target_id
) r ON r.target_id = c.id
SET c.like_count = COALESCE(r.like_total, 0);

SET @has_feedback_hot_index = (
    SELECT COUNT(*) FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'rider_feedback_comments' AND INDEX_NAME = 'idx_feedback_hot'
);
SET @add_feedback_hot_index = IF(@has_feedback_hot_index = 0,
    'ALTER TABLE rider_feedback_comments ADD KEY idx_feedback_hot (feedback_id, parent_id, hidden, like_count DESC, created_at DESC, id DESC)',
    'SET @cityflow_noop = 1');
PREPARE cityflow_add_feedback_hot_index FROM @add_feedback_hot_index;
EXECUTE cityflow_add_feedback_hot_index;
DEALLOCATE PREPARE cityflow_add_feedback_hot_index;
