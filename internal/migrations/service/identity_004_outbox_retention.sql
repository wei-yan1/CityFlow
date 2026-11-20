-- Outbox maintenance deletes PUBLISHED rows by published_at.
ALTER TABLE identity_outbox
    ADD KEY identity_outbox_retention_idx (status, published_at);
