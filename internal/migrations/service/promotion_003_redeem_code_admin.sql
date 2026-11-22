-- Redeem-code production fields. Existing development rows are not backfilled
-- with a real sequence because their codes were inserted manually; new rows
-- always carry seq, batch_no, operator and optional note.
ALTER TABLE coupon_redeem_code
    ADD COLUMN batch_no VARCHAR(64) NOT NULL DEFAULT '' AFTER coupon_id,
    ADD COLUMN seq BIGINT NULL AFTER code,
    ADD COLUMN created_by VARCHAR(64) NOT NULL DEFAULT 'system' AFTER status,
    ADD COLUMN note VARCHAR(255) NULL AFTER created_by,
    ADD UNIQUE KEY uk_redeem_seq (seq),
    ADD KEY idx_redeem_code_batch (coupon_id, batch_no, created_at);

UPDATE coupon_redeem_code SET batch_no = 'LEGACY' WHERE batch_no = '';

CREATE TABLE IF NOT EXISTS promotion_redeem_code_sequence (
    name       VARCHAR(64) NOT NULL PRIMARY KEY,
    next_value BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO promotion_redeem_code_sequence (name, next_value)
VALUES ('coupon_redeem_code', 1);
