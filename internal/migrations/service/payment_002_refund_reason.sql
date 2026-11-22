-- Payment owns the refund reason; it is no longer copied from the task.
ALTER TABLE payment_orders
  ADD COLUMN refund_reason VARCHAR(255) NOT NULL DEFAULT '' AFTER fail_reason,
  DROP CHECK payment_orders_status_check,
  ADD CONSTRAINT payment_orders_status_check CHECK(status IN ('PENDING','PAID','FAILED','CLOSED','REFUND_PENDING','REFUND_FAILED','REFUNDED'));
