-- Task owns the cancellation reason and terminal timestamps. Payment keeps a
-- separate refund_reason snapshot when a cancelled task is refunded.
ALTER TABLE tasks
    ADD COLUMN cancel_reason VARCHAR(255) NOT NULL DEFAULT '' AFTER status,
    ADD COLUMN cancelled_at DATETIME(3) NULL AFTER worker_id,
    ADD COLUMN completed_at DATETIME(3) NULL AFTER cancelled_at;
