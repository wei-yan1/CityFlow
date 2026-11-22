-- Reaction is now a standalone service: Discovery only keeps the like_count
-- projections on reviews and comments, never its own reaction ledger.
DROP TABLE IF EXISTS reaction_records;
