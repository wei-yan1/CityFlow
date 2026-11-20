-- Reconcile scans PENDING / REFUND_PENDING / REFUND_FAILED by updated_at.
ALTER TABLE payment_orders
    ADD KEY payment_orders_reconcile_idx (status, updated_at);
