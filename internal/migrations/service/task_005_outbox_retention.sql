-- Outbox maintenance deletes PUBLISHED rows by published_at.
ALTER TABLE task_outbox
    ADD KEY task_outbox_retention_idx (status, published_at);
