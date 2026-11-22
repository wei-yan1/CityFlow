-- Merchant likes are month-scoped facts. A user may like the same merchant
-- once per month; the unique key is the enforcement point, not Redis.
CREATE TABLE IF NOT EXISTS merchant_like (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    merchant_id  CHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL,
    user_id      CHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL,
    like_month   CHAR(6) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at   DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY merchant_like_user_month_unique (merchant_id, user_id, like_month),
    KEY merchant_like_month_merchant_idx (like_month, merchant_id),
    CONSTRAINT merchant_like_merchant_fk FOREIGN KEY (merchant_id) REFERENCES merchant_profiles(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- The monthly Top10 is persisted before the Redis ZSET is disposed. The
-- snapshot deliberately has no foreign key: it is historical data and must
-- survive merchant archival or deletion.
CREATE TABLE IF NOT EXISTS merchant_hot_snapshot (
    month       CHAR(6) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    category    VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    rank_no     TINYINT UNSIGNED NOT NULL,
    merchant_id CHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL,
    like_count  BIGINT UNSIGNED NOT NULL,
    created_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (month, category, rank_no),
    KEY merchant_hot_snapshot_merchant_idx (merchant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
