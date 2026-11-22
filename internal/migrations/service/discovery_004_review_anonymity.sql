-- Anonymous is a property of one review row, never of the account.
-- user_id and task_id remain the authoritative internal identity/relationship.
SET @has_review_anonymous = (
    SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'place_reviews' AND COLUMN_NAME = 'anonymous'
);
SET @add_review_anonymous = IF(@has_review_anonymous = 0,
    'ALTER TABLE place_reviews ADD COLUMN anonymous TINYINT(1) NOT NULL DEFAULT 0',
    'SET @cityflow_noop = 1');
PREPARE cityflow_add_review_anonymous FROM @add_review_anonymous;
EXECUTE cityflow_add_review_anonymous;
DEALLOCATE PREPARE cityflow_add_review_anonymous;

SET @has_review_author_label = (
    SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'place_reviews' AND COLUMN_NAME = 'author_label'
);
SET @add_review_author_label = IF(@has_review_author_label = 0,
    CONCAT('ALTER TABLE place_reviews ADD COLUMN author_label VARCHAR(32) NOT NULL DEFAULT ', QUOTE('')),
    'SET @cityflow_noop = 1');
PREPARE cityflow_add_review_author_label FROM @add_review_author_label;
EXECUTE cityflow_add_review_author_label;
DEALLOCATE PREPARE cityflow_add_review_author_label;

-- Backfill only legacy rows. New rows always receive a label from the service.
UPDATE place_reviews
SET author_label = CONCAT('用户·', LEFT(SHA2(user_id, 256), 6))
WHERE author_label = '';

-- Audit every privileged reveal. The review_id is deliberately not a foreign
-- key: deleting a review must not erase the fact that an administrator opened it.
CREATE TABLE IF NOT EXISTS review_identity_audit (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    review_id   CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    admin_id    CHAR(36) NOT NULL,
    action      VARCHAR(32) NOT NULL,
    reason      VARCHAR(500) NOT NULL,
    created_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY review_identity_audit_review (review_id, created_at),
    KEY review_identity_audit_admin (admin_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
