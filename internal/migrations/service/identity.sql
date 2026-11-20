-- Identity-owned tables. They live in their own MySQL database and are never
-- qualified with a cross-service schema. Tasks reference user IDs without a
-- database-level foreign key because the two services own separate schemas.
CREATE TABLE IF NOT EXISTS users (
    id              CHAR(36) NOT NULL,
    email           VARCHAR(254) NOT NULL,
    phone           VARCHAR(32) NULL,
    password_hash   VARCHAR(255) NOT NULL,
    display_name    VARCHAR(80) NOT NULL,
    avatar_url      TEXT NULL,
    role            VARCHAR(20) NOT NULL,
    status          VARCHAR(20) NOT NULL DEFAULT 'ACTIVE',
    email_verified  BOOLEAN NOT NULL DEFAULT FALSE,
    phone_verified  BOOLEAN NOT NULL DEFAULT FALSE,
    last_login_at   DATETIME(3) NULL,
    created_at      DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at      DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY users_email_unique (email),
    UNIQUE KEY users_phone_unique (phone),
    KEY users_created_at_idx (created_at, id),
    KEY users_status_created_idx (status, created_at, id),
    CONSTRAINT users_role_check CHECK (role IN ('USER', 'MERCHANT', 'RIDER', 'ADMIN')),
    CONSTRAINT users_status_check CHECK (status IN ('ACTIVE', 'DISABLED', 'PENDING'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS password_reset_tokens (
    token_hash  CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    user_id     CHAR(36) NOT NULL,
    email       VARCHAR(254) NOT NULL,
    expires_at  DATETIME(3) NOT NULL,
    created_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (token_hash),
    KEY password_reset_user (user_id, expires_at),
    CONSTRAINT password_reset_user_fk FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
