-- Batch redeem codes: one code can be redeemed by many users up to
-- max_redeem, and each user may redeem the same code only once. The unique key
-- on coupon_redeem_record is the final guard; code checks cannot replace it.
CREATE TABLE IF NOT EXISTS coupon_redeem_code (
    id          CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    coupon_id   CHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL,
    code        VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    max_redeem  INT NOT NULL,
    used_count  INT NOT NULL DEFAULT 0,
    status      VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    starts_at   DATETIME(3) NULL,
    ends_at     DATETIME(3) NULL,
    created_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_redeem_code (code),
    KEY idx_redeem_code_coupon (coupon_id),
    CONSTRAINT fk_redeem_code_coupon FOREIGN KEY (coupon_id) REFERENCES coupons (id),
    CONSTRAINT chk_redeem_code_status CHECK (status IN ('ACTIVE', 'DISABLED')),
    CONSTRAINT chk_redeem_code_limit CHECK (max_redeem > 0 AND used_count >= 0 AND used_count <= max_redeem)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS coupon_redeem_record (
    id          CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    code_id     CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    user_id     CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    coupon_id   CHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL,
    claim_id    CHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL,
    created_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_redeem_code_user (code_id, user_id),
    KEY idx_redeem_record_coupon_user (coupon_id, user_id),
    KEY idx_redeem_record_claim (claim_id),
    CONSTRAINT fk_redeem_record_code FOREIGN KEY (code_id) REFERENCES coupon_redeem_code (id),
    CONSTRAINT fk_redeem_record_coupon FOREIGN KEY (coupon_id) REFERENCES coupons (id),
    CONSTRAINT fk_redeem_record_claim FOREIGN KEY (claim_id) REFERENCES coupon_claims (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
