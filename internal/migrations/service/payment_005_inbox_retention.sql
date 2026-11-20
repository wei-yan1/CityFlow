-- Purge scans PROCESSED inbox rows by processed_at. The existing
-- (status, updated_at) index cannot serve that range efficiently.
ALTER TABLE payment_inbox
    ADD KEY payment_inbox_retention_idx (status, processed_at);
