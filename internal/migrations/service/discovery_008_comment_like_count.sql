-- Comments keep a like_count projection so the hot list can be ordered
-- without aggregating reaction_records on every read. reaction_records stays
-- the source of truth; this column is maintained in the same transaction as
-- the reaction write.
SET @has_comment_like_count = (
    SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'place_review_comments' AND COLUMN_NAME = 'like_count'
);
SET @add_comment_like_count = IF(@has_comment_like_count = 0,
    'ALTER TABLE place_review_comments ADD COLUMN like_count INT NOT NULL DEFAULT 0 AFTER hidden',
    'SET @cityflow_noop = 1');
PREPARE cityflow_add_comment_like_count FROM @add_comment_like_count;
EXECUTE cityflow_add_comment_like_count;
DEALLOCATE PREPARE cityflow_add_comment_like_count;

UPDATE place_review_comments c
LEFT JOIN (
    SELECT target_id, COUNT(*) AS like_total
    FROM reaction_records
    WHERE target_type = 'PLACE_COMMENT' AND kind = 'LIKE'
    GROUP BY target_id
) r ON r.target_id = c.id
SET c.like_count = COALESCE(r.like_total, 0);

SET @has_review_hot_index = (
    SELECT COUNT(*) FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'place_review_comments' AND INDEX_NAME = 'idx_review_hot'
);
SET @add_review_hot_index = IF(@has_review_hot_index = 0,
    'ALTER TABLE place_review_comments ADD KEY idx_review_hot (review_id, parent_id, hidden, like_count DESC, created_at DESC, id DESC)',
    'SET @cityflow_noop = 1');
PREPARE cityflow_add_review_hot_index FROM @add_review_hot_index;
EXECUTE cityflow_add_review_hot_index;
DEALLOCATE PREPARE cityflow_add_review_hot_index;
