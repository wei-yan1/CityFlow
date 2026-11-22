-- Discovery owns public place facts: merchant profiles, merchant media,
-- place reviews/likes and the trusted-place snapshot used by Task. Identity
-- accounts stay in cityflow_identity; no foreign keys or joins cross the
-- service boundary.
CREATE TABLE IF NOT EXISTS merchant_profiles (
    id                  CHAR(36) NOT NULL,
    user_id             CHAR(36) NOT NULL,
    store_name          VARCHAR(120) NOT NULL,
    description         TEXT NULL,
    contact_phone       VARCHAR(32) NULL,
    address             TEXT NOT NULL,
    latitude            DECIMAL(10,7) NOT NULL,
    longitude           DECIMAL(10,7) NOT NULL,
    amap_poi_id         VARCHAR(128) NULL,
    business_status     VARCHAR(20) NOT NULL DEFAULT 'CLOSED',
    service_types       JSON NOT NULL,
    capacity_per_hour   INT UNSIGNED NOT NULL DEFAULT 1,
    preparation_minutes SMALLINT UNSIGNED NOT NULL DEFAULT 0,
    business_hours      JSON NOT NULL,
    is_archived         TINYINT(1) NOT NULL DEFAULT 0,
    created_at          DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at          DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY merchant_profiles_user_unique (user_id),
    UNIQUE KEY merchant_amap_poi_unique (amap_poi_id),
    KEY merchant_profiles_archive_created_idx (is_archived, created_at, id),
    KEY merchant_profiles_archive_status_created_idx (is_archived, business_status, created_at, id),
    KEY merchant_map_location_idx (business_status, is_archived, longitude, latitude),
    CONSTRAINT merchant_business_status_check CHECK (business_status IN ('OPEN', 'CLOSED', 'PAUSED')),
    CONSTRAINT merchant_latitude_check CHECK (latitude BETWEEN -90 AND 90),
    CONSTRAINT merchant_longitude_check CHECK (longitude BETWEEN -180 AND 180)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS merchant_media (
    merchant_id CHAR(36) NOT NULL,
    avatar_url  VARCHAR(2048) NOT NULL DEFAULT '',
    photos      JSON NOT NULL,
    PRIMARY KEY (merchant_id),
    CONSTRAINT merchant_media_merchant_fk FOREIGN KEY (merchant_id) REFERENCES merchant_profiles(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS place_reviews (
    id             CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    task_id        CHAR(36) NULL,
    place_source   VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    place_id       VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    user_id        CHAR(36) NOT NULL,
    rating         TINYINT UNSIGNED NOT NULL,
    comment        VARCHAR(500) NOT NULL DEFAULT '',
    created_at     DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY place_reviews_task_unique (task_id, place_source, place_id, user_id),
    KEY place_reviews_place (place_source, place_id, created_at, id),
    KEY place_reviews_user_created (user_id, created_at, id),
    CONSTRAINT place_reviews_rating CHECK (rating BETWEEN 1 AND 5)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS place_review_likes (
    review_id  CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    user_id    CHAR(36) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (review_id, user_id),
    CONSTRAINT place_review_likes_review_fk FOREIGN KEY (review_id) REFERENCES place_reviews(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS trusted_places (
    poi_id      VARCHAR(128) NOT NULL PRIMARY KEY,
    name        VARCHAR(255) NOT NULL,
    address     VARCHAR(1000) NOT NULL,
    latitude    DOUBLE NOT NULL,
    longitude   DOUBLE NOT NULL,
    verified_at DATETIME(3) NOT NULL,
    expires_at  DATETIME(3) NOT NULL,
    INDEX trusted_places_expiry_idx (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

