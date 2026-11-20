-- 0003_file_op_groups.sql
-- +goose Up

-- group_id ties every item of one user action together, so a cancelled
-- operation can be traced in the ledger.
ALTER TABLE file_ops ADD COLUMN group_id TEXT NOT NULL DEFAULT '';

CREATE INDEX file_ops_group ON file_ops(group_id);

-- +goose Down
DROP INDEX IF EXISTS file_ops_group;
ALTER TABLE file_ops DROP COLUMN group_id;
