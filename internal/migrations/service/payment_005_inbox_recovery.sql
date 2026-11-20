-- A consumer can crash after inserting the inbox row but before completing its
-- side effect. Keep the event payload so the Inbox recovery worker can replay it
-- after the PROCESSING lease expires, without relying on broker redelivery.
ALTER TABLE payment_inbox
    ADD COLUMN payload MEDIUMBLOB NULL AFTER event_type,
    ADD COLUMN attempts INT NOT NULL DEFAULT 0 AFTER payload;
