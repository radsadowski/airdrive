-- 0001_initial.sql
-- +goose Up

CREATE TABLE locations (
    id          TEXT PRIMARY KEY,
    kind        TEXT NOT NULL,           -- localfs | gdrive | s3
    name        TEXT NOT NULL,
    root        TEXT NOT NULL,           -- folder path / bucket / "My Drive"
    config      TEXT NOT NULL DEFAULT '{}', -- JSON: non-secret settings
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

CREATE TABLE files (
    id           TEXT NOT NULL,
    location_id  TEXT NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
    parent_id    TEXT NOT NULL DEFAULT '', -- '' = root of location
    name         TEXT NOT NULL,
    kind         TEXT NOT NULL,           -- file | dir
    size         INTEGER NOT NULL DEFAULT 0,
    mime         TEXT NOT NULL DEFAULT '',
    etag         TEXT NOT NULL DEFAULT '',
    preview_kind TEXT NOT NULL DEFAULT '',
    modified_at  TEXT NOT NULL,
    indexed_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    -- composite key: file IDs are only unique within a location
    -- (S3 keys are per-bucket, localfs paths can overlap two roots)
    PRIMARY KEY (location_id, id)
);

CREATE INDEX files_location_parent ON files(location_id, parent_id);
CREATE INDEX files_location_name   ON files(location_id, name);
CREATE INDEX files_etag            ON files(etag);

CREATE VIRTUAL TABLE files_fts USING fts5(
    name,
    content=files,
    content_rowid=rowid
);

-- keep FTS in sync
-- +goose StatementBegin
CREATE TRIGGER files_ai AFTER INSERT ON files BEGIN
    INSERT INTO files_fts(rowid, name) VALUES (new.rowid, new.name);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER files_ad AFTER DELETE ON files BEGIN
    INSERT INTO files_fts(files_fts, rowid, name) VALUES ('delete', old.rowid, old.name);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER files_au AFTER UPDATE OF name ON files BEGIN
    INSERT INTO files_fts(files_fts, rowid, name) VALUES ('delete', old.rowid, old.name);
    INSERT INTO files_fts(rowid, name) VALUES (new.rowid, new.name);
END;
-- +goose StatementEnd

-- Transfer journal: the migration engine's source of truth.
CREATE TABLE transfers (
    id               TEXT PRIMARY KEY,
    batch_id         TEXT NOT NULL,
    source_file_id   TEXT NOT NULL,
    source_name      TEXT NOT NULL,
    source_loc_id    TEXT NOT NULL,
    dest_loc_id      TEXT NOT NULL,
    dest_dir_id      TEXT NOT NULL DEFAULT '',
    dest_path        TEXT NOT NULL DEFAULT '',
    state            TEXT NOT NULL DEFAULT 'pending', -- pending|running|done|failed|skipped|cancelled|paused
    bytes_total      INTEGER NOT NULL DEFAULT 0,
    bytes_done       INTEGER NOT NULL DEFAULT 0,
    md5_verified     INTEGER NOT NULL DEFAULT 0,
    error            TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

CREATE INDEX transfers_batch ON transfers(batch_id);
CREATE INDEX transfers_state ON transfers(state);

CREATE TABLE transfer_batches (
    id             TEXT PRIMARY KEY,
    action         TEXT NOT NULL,  -- copy | move | upload
    conflict       TEXT NOT NULL DEFAULT 'smart',
    delete_source  INTEGER NOT NULL DEFAULT 0,
    source_loc_id  TEXT NOT NULL DEFAULT '',
    dest_loc_id    TEXT NOT NULL DEFAULT '',
    dest_dir_id    TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

-- v2-ready sync tables (schema present from day one, unused until Phase v2).
CREATE TABLE sync_pairs (
    id          TEXT PRIMARY KEY,
    location_a  TEXT NOT NULL,
    path_a      TEXT NOT NULL DEFAULT '',
    location_b  TEXT NOT NULL,
    path_b      TEXT NOT NULL DEFAULT '',
    direction   TEXT NOT NULL DEFAULT 'both'
);

CREATE TABLE sync_state (
    pair_id           TEXT NOT NULL,
    file_id           TEXT NOT NULL,
    last_etag_a       TEXT NOT NULL DEFAULT '',
    last_etag_b       TEXT NOT NULL DEFAULT '',
    last_synced_at    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (pair_id, file_id)
);

-- +goose Down
DROP TABLE IF EXISTS sync_state;
DROP TABLE IF EXISTS sync_pairs;
DROP TABLE IF EXISTS transfer_batches;
DROP TABLE IF EXISTS transfers;
-- +goose StatementBegin
DROP TRIGGER IF EXISTS files_au;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TRIGGER IF EXISTS files_ad;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TRIGGER IF EXISTS files_ai;
-- +goose StatementEnd
DROP TABLE IF EXISTS files_fts;
DROP TABLE IF EXISTS files;
DROP TABLE IF EXISTS locations;
