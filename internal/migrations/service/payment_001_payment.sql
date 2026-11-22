-- Payment owns charges, settlements and payout accounts. Task and
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
