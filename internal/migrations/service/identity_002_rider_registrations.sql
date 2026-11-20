-- Rider applications are owned by Identity until an administrator approves
-- them. Approval creates the user account here and asks Task to provision the
-- rider performance profile; Task never writes this table.
CREATE TABLE IF NOT EXISTS rider_registrations (
    id                    CHAR(36) NOT NULL,
    email                 VARCHAR(254) NOT NULL,
    phone                 VARCHAR(32) NULL,
    password_hash         VARCHAR(255) NOT NULL,
    display_name          VARCHAR(80) NOT NULL,
    vehicle_type          VARCHAR(24) NOT NULL,
    service_types         JSON NOT NULL,
    service_area          VARCHAR(160) NULL,
    max_distance_meters   INT UNSIGNED NOT NULL,
    max_concurrent_tasks  SMALLINT UNSIGNED NOT NULL,
    paid_amount_fen       BIGINT NOT NULL,
    payment_reference     VARCHAR(128) NOT NULL,
    payment_note          VARCHAR(255) NULL,
    status                VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    review_note           VARCHAR(255) NULL,
    reviewed_by           CHAR(36) NULL,
    reviewed_at           DATETIME(3) NULL,
    user_id               CHAR(36) NULL,
    created_at            DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at            DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY rider_registrations_payment_reference (payment_reference),
    KEY rider_registrations_status (status, created_at),
    KEY rider_registrations_email (email),
    KEY rider_registrations_phone (phone),
    CONSTRAINT rider_registrations_amount_check CHECK (paid_amount_fen >= 1),
    CONSTRAINT rider_registrations_status_check CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED')),
    CONSTRAINT rider_registrations_tasks_check CHECK (max_concurrent_tasks BETWEEN 1 AND 20),
    CONSTRAINT rider_registrations_vehicle_check CHECK (vehicle_type IN ('WALK', 'BICYCLE', 'E_BIKE', 'CAR', 'OTHER'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
