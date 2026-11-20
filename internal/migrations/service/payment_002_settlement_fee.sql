-- Settlement stores the platform fee separately from the rider payout so
-- reporting can reconcile both sides. 001 intentionally created the funding
-- tables first; this incremental migration adds the fee column.
ALTER TABLE rider_settlements ADD COLUMN fee_fen BIGINT NOT NULL DEFAULT 0 AFTER amount_fen;
