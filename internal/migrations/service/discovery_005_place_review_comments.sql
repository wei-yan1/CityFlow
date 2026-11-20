-- Comments live beside the reviews they belong to. Every comment is owned by
-- Discovery; no other service writes this table.
SET @has_like_count = (
    SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'place_reviews' AND COLUMN_NAME = 'like_count'
);
SET @add_like_count = IF(@has_like_count = 0,
    'ALTER TABLE place_reviews ADD COLUMN like_count INT NOT NULL DEFAULT 0 AFTER comment',
    'SET @cityflow_noop = 1');
PREPARE cityflow_add_like_count FROM @add_like_count;
EXECUTE cityflow_add_like_count;
DEALLOCATE PREPARE cityflow_add_like_count;

SET @has_hot_index = (
    SELECT COUNT(*) FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'place_reviews' AND INDEX_NAME = 'place_reviews_hot'
);
SET @add_hot_index = IF(@has_hot_index = 0,
    'ALTER TABLE place_reviews ADD KEY place_reviews_hot (place_source, place_id, like_count DESC, id)',
    'SET @cityflow_noop = 1');
PREPARE cityflow_add_hot_index FROM @add_hot_index;
EXECUTE cityflow_add_hot_index;
DEALLOCATE PREPARE cityflow_add_hot_index;

CREATE TABLE IF NOT EXISTS place_review_comments (
    id               CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    review_id        CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    user_id          CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    anonymous        TINYINT(1) NOT NULL DEFAULT 0,
    author_label     VARCHAR(32) NOT NULL DEFAULT '',
    reply_to_user_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL,
    reply_to_label   VARCHAR(32) NULL,
    root_id          CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    parent_id        CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL,
    depth            TINYINT UNSIGNED NOT NULL DEFAULT 1,
    content          VARCHAR(500) NOT NULL DEFAULT '',
    hidden           TINYINT(1) NOT NULL DEFAULT 0,
    created_at       DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_review_top (review_id, parent_id, created_at, id),
    KEY idx_root (root_id, created_at, id),
    KEY idx_user (user_id, created_at),
    CONSTRAINT fk_comment_review FOREIGN KEY (review_id)
        REFERENCES place_reviews(id) ON DELETE CASCADE,
    CONSTRAINT depth_check CHECK (depth BETWEEN 1 AND 3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
