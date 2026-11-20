-- 0002_file_ops.sql
-- +goose Up

-- Ledger of scheduled file operations (trash, in-place move). One row per
-- item; the partial unique index makes a second concurrent schedule of the
-- same item impossible, which is what stops double deletes.
CREATE TABLE file_ops (
    id          TEXT PRIMARY KEY,
    location_id TEXT NOT NULL,
    item_id     TEXT NOT NULL,
    item_path   TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL,            -- delete | move
    state       TEXT NOT NULL,            -- running | done | error | interrupted
    error       TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

CREATE UNIQUE INDEX file_ops_active ON file_ops(location_id, item_id) WHERE state = 'running';

-- +goose Down
DROP INDEX IF EXISTS file_ops_active;
DROP TABLE IF EXISTS file_ops;
