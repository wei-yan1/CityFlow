-- Rider review comments are owned by Task. feedback_id is rider_feedback.id
-- and the service additionally verifies kind='REVIEW'.
CREATE TABLE IF NOT EXISTS rider_feedback_comments (
    id               CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    feedback_id      CHAR(36) NOT NULL,
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
    KEY idx_feedback_top (feedback_id, parent_id, created_at, id),
    KEY idx_root (root_id, created_at, id),
    KEY idx_user (user_id, created_at),
    CONSTRAINT fk_rider_comment_feedback FOREIGN KEY (feedback_id)
        REFERENCES rider_feedback(id) ON DELETE CASCADE,
    CONSTRAINT rider_comment_depth_check CHECK (depth BETWEEN 1 AND 3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
