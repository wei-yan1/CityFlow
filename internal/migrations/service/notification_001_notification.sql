-- Notification is the only owner of in-app messages and read state.
CREATE TABLE IF NOT EXISTS notifications (
    id            CHAR(36) NOT NULL PRIMARY KEY,
    user_id       CHAR(36) NOT NULL,
    kind          VARCHAR(48) NOT NULL,
    title         VARCHAR(160) NOT NULL,
    body          TEXT NOT NULL,
    task_id       CHAR(36) NULL,
    event_id      CHAR(36) NULL,
    read_at       DATETIME(3) NULL,
    created_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    UNIQUE KEY notifications_event_user (event_id, user_id),
    KEY notifications_user_created (user_id, created_at, id),
    KEY notifications_user_unread (user_id, read_at, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS notification_templates (
    kind          VARCHAR(48) NOT NULL PRIMARY KEY,
    title         VARCHAR(160) NOT NULL,
    body          TEXT NOT NULL,
    updated_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

