-- Promotion owns coupon campaigns and claims. Task/payment IDs are opaque
-- references and never foreign keys.
CREATE TABLE IF NOT EXISTS coupons (
    id                  CHAR(36) NOT NULL PRIMARY KEY,
    code                VARCHAR(32) NOT NULL,
    title               VARCHAR(120) NOT NULL,
    subtitle            VARCHAR(180) NULL,
    description         TEXT NULL,
    type                VARCHAR(16) NOT NULL,
    value_fen           BIGINT NOT NULL DEFAULT 0,
    discount_percent    INT NOT NULL DEFAULT 0,
    max_discount_fen    BIGINT NOT NULL DEFAULT 0,
    min_spend_fen       BIGINT NOT NULL DEFAULT 0,
    total_quantity      INT NOT NULL,
    claimed_count       INT NOT NULL DEFAULT 0,
    per_user_limit      INT NOT NULL DEFAULT 1,
    status              VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    applicable_task_types JSON NULL,
    starts_at           DATETIME(3) NULL,
    ends_at             DATETIME(3) NULL,
    created_at          DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at          DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY coupons_code (code),
    KEY coupons_active (status, starts_at, ends_at),
    CONSTRAINT coupons_type_check CHECK (type IN ('AMOUNT','PERCENT')),
    CONSTRAINT coupons_status_check CHECK (status IN ('DRAFT','ACTIVE','PAUSED','ENDED')),
    CONSTRAINT coupons_quantity_check CHECK (total_quantity > 0 AND per_user_limit > 0),
    CONSTRAINT coupons_value_check CHECK (value_fen >= 0 AND discount_percent >= 0 AND discount_percent <= 100 AND max_discount_fen >= 0 AND min_spend_fen >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS coupon_claims (
    id                  CHAR(36) NOT NULL PRIMARY KEY,
    coupon_id           CHAR(36) NOT NULL,
    user_id             CHAR(36) NOT NULL,
    claim_no            INT NOT NULL,
    status              VARCHAR(16) NOT NULL DEFAULT 'UNUSED',
    task_id             CHAR(36) NULL,
    payment_order_id    CHAR(36) NULL,
    discount_fen        BIGINT NOT NULL DEFAULT 0,
    claimed_at          DATETIME(3) NOT NULL,
    used_at             DATETIME(3) NULL,
    created_at          DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at          DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY coupon_claims_slot (coupon_id, user_id, claim_no),
    KEY coupon_claims_user (user_id, status, claimed_at),
    KEY coupon_claims_task (task_id),
    CONSTRAINT coupon_claims_coupon_fk FOREIGN KEY (coupon_id) REFERENCES coupons(id),
    CONSTRAINT coupon_claims_status_check CHECK (status IN ('UNUSED','LOCKED','USED','EXPIRED')),
    CONSTRAINT coupon_claims_claim_no_check CHECK (claim_no > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT IGNORE INTO coupons
    (id, code, title, subtitle, description, type, value_fen, discount_percent, max_discount_fen, min_spend_fen, total_quantity, per_user_limit, status, applicable_task_types, starts_at, ends_at)
VALUES
    ('00000000-0000-4000-8000-000000000101','WELCOME5','新客立减 5 元','第一单配送，平台替你省一点','限 CityFlow 用户领取，单笔任务满 10 元可用。','AMOUNT',500,0,0,1000,500,1,'ACTIVE','[]','2026-01-01 00:00:00','2027-12-31 23:59:59'),
    ('00000000-0000-4000-8000-000000000102','RIDE20','配送费 8 折','适合校园、社区内的短途配送','按任务金额的 20% 抵扣，最高优惠 8 元，单笔满 20 元可用。','PERCENT',0,20,800,2000,300,1,'ACTIVE','[]','2026-01-01 00:00:00','2027-12-31 23:59:59'),
    ('00000000-0000-4000-8000-000000000103','CAMPUS3','校园配送立减 3 元','取件或送达在校园内时使用','限配送类任务，单笔任务满 8 元可用。','AMOUNT',300,0,0,800,1000,1,'ACTIVE','["DELIVERY"]','2026-01-01 00:00:00','2027-12-31 23:59:59');


