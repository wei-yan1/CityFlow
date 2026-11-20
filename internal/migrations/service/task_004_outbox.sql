-- Transactional outbox for Task integration events. The relay publishes rows
-- after the task status change and task_events row have committed together.
CREATE TABLE IF NOT EXISTS task_outbox (
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
    KEY task_outbox_dispatch_idx (status, available_at, locked_until, created_at),
    KEY task_outbox_aggregate_idx (aggregate_id, created_at),
    CONSTRAINT task_outbox_status_check CHECK (status IN ('PENDING', 'PUBLISHING', 'PUBLISHED', 'FAILED'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
