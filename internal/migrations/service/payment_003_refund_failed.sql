ALTER TABLE payment_orders
    DROP CHECK payment_orders_status_check,
    ADD CONSTRAINT payment_orders_status_check CHECK (
        status IN ('PENDING','PAID','FAILED','CLOSED','REFUND_PENDING','REFUNDED','REFUND_FAILED')
    );
