-- Payment-side inbox for integration events. event_id is the outbox event ID;
-- a duplicate delivery is acknowledged without repeating the side effect.
CREATE TABLE IF NOT EXISTS payment_inbox (
    event_id      CHAR(36) NOT NULL,
    event_type    VARCHAR(96) NOT NULL,
    status        VARCHAR(16) NOT NULL DEFAULT 'PROCESSING',
    received_at   DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    processed_at  DATETIME(3) NULL,
    last_error    VARCHAR(255) NULL,
    updated_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (event_id),
    KEY payment_inbox_status_idx (status, updated_at),
    CONSTRAINT payment_inbox_status_check CHECK (status IN ('PROCESSING', 'PROCESSED', 'FAILED'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
