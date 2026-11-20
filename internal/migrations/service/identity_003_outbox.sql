-- Identity outbox for cross-service rider provisioning. Approval commits the
-- account, registration status and this event together; Task consumes it.
CREATE TABLE IF NOT EXISTS identity_outbox (
    id            CHAR(36) NOT NULL,
    event_type    VARCHAR(96) NOT NULL,
    aggregate_id  CHAR(36) NOT NULL,
    payload       JSON NOT NULL,
    status        VARCHAR(16) NOT NULL DEFAULT 'PENDING',
    attempts      INT UNSIGNED NOT NULL DEFAULT 0,
    available_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    locked_until  DATETIME(3) NULL,
    published_at  DATETIME(3) NULL,
    last_error    VARCHAR(255) NULL,
    created_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY identity_outbox_dispatch_idx (status, available_at, locked_until, created_at),
    KEY identity_outbox_aggregate_idx (aggregate_id, created_at),
    CONSTRAINT identity_outbox_status_check CHECK (status IN ('PENDING', 'PUBLISHING', 'PUBLISHED', 'FAILED'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
