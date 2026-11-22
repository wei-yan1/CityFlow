-- Like/favorite records owned by discovery (places, merchants, place reviews
-- and place comments). Shared table shape with task-owned rider reactions.
CREATE TABLE IF NOT EXISTS reaction_records (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    target_type VARCHAR(24) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    target_id   VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    user_id     CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    kind        VARCHAR(12) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_reaction (target_type, target_id, kind, user_id),
    KEY idx_user_kind (user_id, kind, target_type, created_at, id),
    CONSTRAINT reaction_kind_check CHECK (kind IN ('LIKE','FAVORITE'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;