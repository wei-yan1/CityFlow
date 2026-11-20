-- Payment owns charges, settlements, payout accounts and coupons. Task and
-- Identity identifiers are copied as opaque keys; no cross-service foreign
-- keys or joins are allowed.
CREATE TABLE IF NOT EXISTS payment_orders (
 id CHAR(36) NOT NULL PRIMARY KEY,
 task_id CHAR(36) NOT NULL,
 payer_id CHAR(36) NOT NULL,
 original_amount_fen BIGINT NOT NULL,
 discount_fen BIGINT NOT NULL DEFAULT 0,
 amount_fen BIGINT NOT NULL,
 currency CHAR(3) NOT NULL DEFAULT 'CNY',
 status VARCHAR(24) NOT NULL DEFAULT 'PENDING',
 active_task_id CHAR(36) GENERATED ALWAYS AS (CASE WHEN status IN ('PENDING','PAID','REFUND_PENDING') THEN task_id ELSE NULL END) STORED,
 fail_reason VARCHAR(255) NULL,
 notify_count INT NOT NULL DEFAULT 0,
 last_notify_at DATETIME(3) NULL,
 provider VARCHAR(32) NULL,
 provider_order_no VARCHAR(64) NULL,
 provider_trade_id VARCHAR(128) NULL,
 pay_payload VARCHAR(1024) NULL,
 pay_url VARCHAR(1024) NULL,
 coupon_claim_id CHAR(36) NULL,
 coupon_title VARCHAR(120) NULL,
 idempotency_key VARCHAR(128) NOT NULL,
 paid_at DATETIME(3) NULL,
 expires_at DATETIME(3) NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 UNIQUE KEY payment_orders_idempotency (payer_id,idempotency_key),
 UNIQUE KEY payment_orders_active_task (active_task_id),
 UNIQUE KEY payment_orders_provider_trade (provider,provider_trade_id),
 UNIQUE KEY payment_orders_provider_order (provider,provider_order_no),
 KEY payment_orders_task (task_id,created_at),
 KEY payment_orders_coupon (coupon_claim_id),
 CONSTRAINT payment_orders_amount_check CHECK(amount_fen > 0),
 CONSTRAINT payment_orders_currency_check CHECK(currency = 'CNY'),
 CONSTRAINT payment_orders_status_check CHECK(status IN ('PENDING','PAID','FAILED','CLOSED','REFUND_PENDING','REFUNDED'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS rider_settlements (
 id CHAR(36) NOT NULL PRIMARY KEY,
 task_id CHAR(36) NOT NULL,
 payment_order_id CHAR(36) NOT NULL,
 rider_id CHAR(36) NOT NULL,
 amount_fen BIGINT NOT NULL,
 currency CHAR(3) NOT NULL DEFAULT 'CNY',
 status VARCHAR(24) NOT NULL DEFAULT 'PENDING',
 fail_reason VARCHAR(255) NULL,
 attempts INT NOT NULL DEFAULT 0,
 next_attempt_at DATETIME(3) NULL,
 locked_until DATETIME(3) NULL,
 provider VARCHAR(32) NULL,
 provider_bill_no VARCHAR(64) NULL,
 provider_payout_id VARCHAR(128) NULL,
 idempotency_key VARCHAR(128) NOT NULL,
 paid_at DATETIME(3) NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 UNIQUE KEY rider_settlements_task (task_id),
 UNIQUE KEY rider_settlements_idempotency (idempotency_key),
 UNIQUE KEY rider_settlements_provider_payout (provider,provider_payout_id),
 UNIQUE KEY rider_settlements_provider_bill (provider,provider_bill_no),
 KEY rider_settlements_rider (rider_id,created_at),
 CONSTRAINT rider_settlements_amount_check CHECK(amount_fen > 0),
 CONSTRAINT rider_settlements_currency_check CHECK(currency = 'CNY'),
 CONSTRAINT rider_settlements_status_check CHECK(status IN ('PENDING','PROCESSING','PAID','FAILED','UNKNOWN'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS payment_notifies (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 provider VARCHAR(32) NOT NULL,
 provider_order_no VARCHAR(64) NULL,
 provider_trade_id VARCHAR(128) NULL,
 signature_valid TINYINT(1) NOT NULL DEFAULT 0,
 status VARCHAR(24) NULL,
 result VARCHAR(64) NOT NULL,
 payload TEXT NULL,
 received_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 KEY payment_notifies_order (provider, provider_order_no, received_at),
 KEY payment_notifies_time (received_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS rider_payout_accounts (
 rider_id CHAR(36) NOT NULL PRIMARY KEY,
 provider VARCHAR(16) NOT NULL,
 account VARCHAR(128) NOT NULL,
 account_name VARCHAR(80) NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 CONSTRAINT rider_payout_accounts_provider_check CHECK(provider IN ('ALIPAY','WECHAT','BANK'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS coupons (
 id CHAR(36) NOT NULL PRIMARY KEY,
 code VARCHAR(32) NOT NULL,
 title VARCHAR(120) NOT NULL,
 subtitle VARCHAR(180) NULL,
 description TEXT NULL,
 type VARCHAR(16) NOT NULL,
 value_fen BIGINT NOT NULL DEFAULT 0,
 discount_percent INT NOT NULL DEFAULT 0,
 max_discount_fen BIGINT NOT NULL DEFAULT 0,
 min_spend_fen BIGINT NOT NULL DEFAULT 0,
 total_quantity INT NOT NULL,
 claimed_count INT NOT NULL DEFAULT 0,
 per_user_limit INT NOT NULL DEFAULT 1,
 status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
 applicable_task_types JSON NULL,
 starts_at DATETIME(3) NULL,
 ends_at DATETIME(3) NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 UNIQUE KEY coupons_code (code),
 KEY coupons_active (status,starts_at,ends_at),
 CONSTRAINT coupons_type_check CHECK(type IN ('AMOUNT','PERCENT')),
 CONSTRAINT coupons_status_check CHECK(status IN ('DRAFT','ACTIVE','PAUSED','ENDED')),
 CONSTRAINT coupons_quantity_check CHECK(total_quantity > 0 AND per_user_limit > 0),
 CONSTRAINT coupons_value_check CHECK(value_fen >= 0 AND discount_percent >= 0 AND discount_percent <= 100 AND max_discount_fen >= 0 AND min_spend_fen >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS coupon_claims (
 id CHAR(36) NOT NULL PRIMARY KEY,
 coupon_id CHAR(36) NOT NULL,
 user_id CHAR(36) NOT NULL,
 claim_no INT NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'UNUSED',
 task_id CHAR(36) NULL,
 payment_order_id CHAR(36) NULL,
 discount_fen BIGINT NOT NULL DEFAULT 0,
 claimed_at DATETIME(3) NOT NULL,
 used_at DATETIME(3) NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 UNIQUE KEY coupon_claims_slot (coupon_id,user_id,claim_no),
 KEY coupon_claims_user (user_id,status,claimed_at),
 KEY coupon_claims_task (task_id),
 CONSTRAINT coupon_claims_coupon_fk FOREIGN KEY(coupon_id) REFERENCES coupons(id),
 CONSTRAINT coupon_claims_status_check CHECK(status IN ('UNUSED','LOCKED','USED','EXPIRED')),
 CONSTRAINT coupon_claims_claim_no_check CHECK(claim_no > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT IGNORE INTO coupons
 (id,code,title,subtitle,description,type,value_fen,discount_percent,max_discount_fen,min_spend_fen,total_quantity,per_user_limit,status,applicable_task_types,starts_at,ends_at)
 VALUES
 ('00000000-0000-4000-8000-000000000101','WELCOME5','新客立减 5 元','第一单配送，平台替你省一点','限 CityFlow 用户领取，单笔订单满 10 元可用。','AMOUNT',500,0,0,1000,500,1,'ACTIVE','[]','2026-01-01 00:00:00','2027-12-31 23:59:59'),
 ('00000000-0000-4000-8000-000000000102','RIDE20','配送费 8 折','适合校园、社区内的短途配送','按订单金额的 20% 抵扣，最高优惠 8 元，单笔满 20 元可用。','PERCENT',0,20,800,2000,300,1,'ACTIVE','[]','2026-01-01 00:00:00','2027-12-31 23:59:59'),
 ('00000000-0000-4000-8000-000000000103','CAMPUS3','校园配送立减 3 元','取件或送达在校园内时使用','限配送类任务，单笔订单满 8 元可用。','AMOUNT',300,0,0,800,1000,1,'ACTIVE','["DELIVERY"]','2026-01-01 00:00:00','2027-12-31 23:59:59');
