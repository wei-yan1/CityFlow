-- Reaction is now a standalone service: content databases keep their
-- like_count projections and must not retain a second reaction ledger.
DROP TABLE IF EXISTS reaction_records;
