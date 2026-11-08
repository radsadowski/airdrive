// Package index owns the SQLite database: schema migrations, the single writer
// goroutine, and all read queries used by the bridge services.
package index

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // register "sqlite" driver

	"github.com/radosny/airdrive/internal/provider"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// gooseMu serializes Open: goose's global config is not concurrency-safe.
var gooseMu sync.Mutex

// DB wraps the SQLite connection with a single-writer channel to serialize all
// writes (modernc/sqlite is unsafe for concurrent writers in WAL mode).
type DB struct {
	db     *sql.DB
	logger *slog.Logger
}

// Open creates (or opens) the database at path, runs pending migrations, and
// enables WAL mode for concurrent reads. The index is disposable: when a
// preexisting database's schema predates what the current build queries
// (a migration edited in place during development), Open drops the file and
// rebuilds it: the lost entries are repopulated by the next crawl.
func Open(path string, logger *slog.Logger) (*DB, error) {
	if logger == nil {
		logger = slog.Default()
	}

	db, err := openMigrated(path)
	if err != nil {
		return nil, err
	}
	drifted := false
	for _, probe := range []string{"SELECT md5 FROM files LIMIT 0", "SELECT actions FROM sync_pairs LIMIT 0"} {
		if _, probeErr := db.Exec(probe); probeErr != nil {
			drifted = true
			break
		}
	}
	if drifted {
		logger.Warn("index: schema drift detected, rebuilding index", "path", path)
		_ = db.Close()
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if rmErr := os.Remove(path + suffix); rmErr != nil && !os.IsNotExist(rmErr) {
				logger.Warn("index: drop stale database", "path", path+suffix, "err", rmErr)
			}
		}
		db, err = openMigrated(path)
		if err != nil {
			return nil, err
		}
	}

	return &DB{db: db, logger: logger}, nil
}

// openMigrated opens the SQLite database at path and applies pending
// migrations with a single-writer connection.
func openMigrated(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("index: mkdir: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("index: open: %w", err)
	}
	// Single writer: one connection suffices for writes.
	db.SetMaxOpenConns(1)

	gooseMu.Lock()
	defer gooseMu.Unlock()
	goose.SetBaseFS(migrationFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return nil, fmt.Errorf("index: goose dialect: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return nil, fmt.Errorf("index: migrate: %w", err)
	}

	return db, nil
}

// Close closes the underlying database.
func (d *DB) Close() error { return d.db.Close() }

// ---- Locations ----------------------------------------------------------- //

// Location mirrors the locations table row.
type Location struct {
	ID        string
	Kind      provider.Kind
	Name      string
	Root      string
	Config    string // JSON
	CreatedAt time.Time
}

// UpsertLocation inserts or replaces a location row.
func (d *DB) UpsertLocation(ctx context.Context, loc Location) error {
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO locations(id, kind, name, root, config)
		 VALUES (?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET kind=excluded.kind, name=excluded.name,
		   root=excluded.root, config=excluded.config`,
		loc.ID, string(loc.Kind), loc.Name, loc.Root, loc.Config,
	)
	return err
}

// DeleteLocation removes a location and all its files (CASCADE).
func (d *DB) DeleteLocation(ctx context.Context, locationID string) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM locations WHERE id=?`, locationID)
	return err
}

// AllLocations returns every location row.
func (d *DB) AllLocations(ctx context.Context) ([]Location, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT id,kind,name,root,config,created_at FROM locations ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Location
	for rows.Next() {
		var l Location
		var kind, createdAt string
		if err := rows.Scan(&l.ID, &kind, &l.Name, &l.Root, &l.Config, &createdAt); err != nil {
			return nil, err
		}
		l.Kind = provider.Kind(kind)
		l.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		out = append(out, l)
	}
	return out, rows.Err()
}

// LocationByID returns a single location or sql.ErrNoRows.
func (d *DB) LocationByID(ctx context.Context, id string) (Location, error) {
	var l Location
	var kind, createdAt string
	err := d.db.QueryRowContext(ctx,
		`SELECT id,kind,name,root,config,created_at FROM locations WHERE id=?`, id,
	).Scan(&l.ID, &kind, &l.Name, &l.Root, &l.Config, &createdAt)
	if err != nil {
		return Location{}, err
	}
	l.Kind = provider.Kind(kind)
	l.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return l, nil
}

// GetSyncCursor returns the stored incremental-sync cursor ("" = full walk).
func (d *DB) GetSyncCursor(ctx context.Context, locationID string) (string, error) {
	return d.GetConfigValue(ctx, locationID, "syncCursor")
}

// SetSyncCursor stores the cursor, preserving sibling config keys.
func (d *DB) SetSyncCursor(ctx context.Context, locationID, cursor string) error {
	return d.SetConfigValues(ctx, locationID, map[string]string{"syncCursor": cursor})
}

// GetGdriveRootID returns the location's picked Drive folder ID ("" = whole
// Drive).
func (d *DB) GetGdriveRootID(ctx context.Context, locationID string) (string, error) {
	return d.GetConfigValue(ctx, locationID, "gdriveRootId")
}

// SetGdriveRootID stores the picked Drive folder ID.
func (d *DB) SetGdriveRootID(ctx context.Context, locationID, rootID string) error {
	return d.SetConfigValues(ctx, locationID, map[string]string{"gdriveRootId": rootID})
}

// GetConfigValue reads one location config key ("" when unset).
func (d *DB) GetConfigValue(ctx context.Context, locationID, key string) (string, error) {
	cfg, err := d.locationConfig(ctx, locationID)
	if err != nil {
		return "", fmt.Errorf("get %s: %w", key, err)
	}
	v, _ := cfg[key].(string)
	return v, nil
}

// GetConfigValues reads several config keys at once.
func (d *DB) GetConfigValues(ctx context.Context, locationID string, keys ...string) (map[string]string, error) {
	cfg, err := d.locationConfig(ctx, locationID)
	if err != nil {
		return nil, fmt.Errorf("get config values: %w", err)
	}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		v, _ := cfg[k].(string)
		out[k] = v
	}
	return out, nil
}

// SetConfigValues writes several keys at once, preserving siblings.
func (d *DB) SetConfigValues(ctx context.Context, locationID string, kv map[string]string) error {
	cfg, err := d.locationConfig(ctx, locationID)
	if err != nil {
		return fmt.Errorf("set config values: %w", err)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	for k, v := range kv {
		cfg[k] = v
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("set config values: marshal: %w", err)
	}
	if _, err := d.db.ExecContext(ctx, `UPDATE locations SET config=? WHERE id=?`, string(b), locationID); err != nil {
		return fmt.Errorf("set config values: %w", err)
	}
	return nil
}

// locationConfig loads the location's config JSON, tolerating empty config.
func (d *DB) locationConfig(ctx context.Context, locationID string) (map[string]any, error) {
	var config string
	if err := d.db.QueryRowContext(ctx, `SELECT config FROM locations WHERE id=?`, locationID).Scan(&config); err != nil {
		return nil, err
	}
	var cfg map[string]any
	if strings.TrimSpace(config) != "" {
		if err := json.Unmarshal([]byte(config), &cfg); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// DeleteFilesNotIn removes a location's files whose IDs are not in keep,
// returning the number removed: the reconcile pass that detects remote
// deletions for providers without a changes feed.
func (d *DB) DeleteFilesNotIn(ctx context.Context, locationID string, keep []string) (int64, error) {
	if _, err := d.db.ExecContext(ctx, `DROP TABLE IF EXISTS temp.keep_ids`); err != nil {
		return 0, fmt.Errorf("reconcile: drop temp: %w", err)
	}
	if _, err := d.db.ExecContext(ctx, `CREATE TEMP TABLE keep_ids (id TEXT PRIMARY KEY)`); err != nil {
		return 0, fmt.Errorf("reconcile: create temp: %w", err)
	}
	for i := 0; i < len(keep); i += 500 {
		end := min(i+500, len(keep))
		var b strings.Builder
		b.WriteString("INSERT OR IGNORE INTO keep_ids VALUES")
		for j := i; j < end; j++ {
			if j > i {
				b.WriteByte(',')
			}
			b.WriteString("(?)")
		}
		args := make([]any, 0, end-i)
		for _, k := range keep[i:end] {
			args = append(args, k)
		}
		if _, err := d.db.ExecContext(ctx, b.String(), args...); err != nil {
			return 0, fmt.Errorf("reconcile: seed temp: %w", err)
		}
	}
	res, err := d.db.ExecContext(ctx,
		`DELETE FROM files WHERE location_id=? AND id NOT IN (SELECT id FROM keep_ids)`,
		locationID)
	if err != nil {
		return 0, fmt.Errorf("reconcile: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("reconcile: affected: %w", err)
	}
	return n, nil
}

// ---- Files --------------------------------------------------------------- //

// UpsertFile inserts or replaces a file row and keeps FTS in sync.
func (d *DB) UpsertFile(ctx context.Context, f provider.FileInfo) error {
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO files(id,location_id,parent_id,name,kind,size,mime,etag,md5,preview_kind,modified_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(location_id, id) DO UPDATE SET
		   parent_id=excluded.parent_id, name=excluded.name,
		   kind=excluded.kind, size=excluded.size, mime=excluded.mime,
		   etag=excluded.etag, md5=excluded.md5, preview_kind=excluded.preview_kind,
		   modified_at=excluded.modified_at,
		   indexed_at=strftime('%Y-%m-%dT%H:%M:%SZ','now')`,
		f.ID, f.LocationID, f.ParentID, f.Name,
		string(f.Kind), f.Size, f.MIME, f.Etag, f.MD5, f.PreviewKind,
		f.ModifiedAt.UTC().Format(time.RFC3339),
	)
	return err
}

// DeleteFile removes a single file row, scoped to its location.
func (d *DB) DeleteFile(ctx context.Context, locationID, fileID string) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM files WHERE location_id=? AND id=?`, locationID, fileID)
	return err
}

// DeleteChildrenOf removes all files whose id starts with dirPath/: used when
// a directory is deleted from the filesystem so descendants are purged too.
func (d *DB) DeleteChildrenOf(ctx context.Context, locationID, dirPath string) error {
	prefix := dirPath + "/"
	_, err := d.db.ExecContext(ctx,
		`DELETE FROM files WHERE location_id=? AND (id LIKE ? OR parent_id LIKE ? OR parent_id=?)`,
		locationID, prefix+"%", prefix+"%", dirPath,
	)
	return err
}

// FileByID returns a single file of a location or sql.ErrNoRows.
func (d *DB) FileByID(ctx context.Context, locationID, fileID string) (provider.FileInfo, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT id,location_id,parent_id,name,kind,size,mime,etag,md5,preview_kind,modified_at
		 FROM files WHERE location_id=? AND id=?`, locationID, fileID)
	return scanFile(row)
}

// ListDir returns a paginated, sorted page of direct children of parentID
// within locationID. parentID="" means root.
func (d *DB) ListDir(ctx context.Context, locationID, parentID string, sort provider.SortSpec, cursor string, limit int, includeHidden bool) (provider.ListPage, error) {
	if limit <= 0 || limit > 2000 {
		limit = 400
	}

	var offset int
	if _, err := fmt.Sscanf(cursor, "%d", &offset); err != nil {
		offset = 0
	}

	orderCol := "name"
	switch sort.Field {
	case provider.SortBySize:
		orderCol = "size"
	case provider.SortByModifiedAt:
		orderCol = "modified_at"
	case provider.SortByKind:
		orderCol = "mime"
	}
	orderDir := "ASC"
	if !sort.Asc {
		orderDir = "DESC"
	}

	hiddenClause := ""
	if !includeHidden {
		hiddenClause = " AND name NOT LIKE '.%'"
	}

	q := "SELECT id,location_id,parent_id,name,kind,size,mime,etag,md5,preview_kind,modified_at" + //nolint:gosec // orderCol/orderDir/hiddenClause are enum-derived, never user input
		" FROM files" +
		" WHERE location_id=? AND parent_id=?" + hiddenClause +
		" ORDER BY CASE kind WHEN 'dir' THEN 0 ELSE 1 END, " + orderCol + " " + orderDir +
		" LIMIT ? OFFSET ?"

	rows, err := d.db.QueryContext(ctx, q, locationID, parentID, limit+1, offset)
	if err != nil {
		return provider.ListPage{}, err
	}
	defer rows.Close()

	var items []provider.FileInfo
	for rows.Next() {
		f, err := scanFileRow(rows)
		if err != nil {
			return provider.ListPage{}, err
		}
		items = append(items, f)
	}
	if err := rows.Err(); err != nil {
		return provider.ListPage{}, err
	}

	var nextCursor string
	if len(items) > limit {
		items = items[:limit]
		nextCursor = fmt.Sprintf("%d", offset+limit)
	}
	return provider.ListPage{Items: items, Cursor: nextCursor}, nil
}

// ftsMatch quotes each search term as an FTS5 prefix term; raw user text
// would let FTS5 operators (", -, AND/OR) behave as syntax and error out.
func ftsMatch(text string) string {
	var terms []string
	for _, field := range strings.Fields(text) {
		field = strings.Trim(field, `"`)
		if field != "" {
			terms = append(terms, `"`+field+`"*`)
		}
	}
	return strings.Join(terms, " ")
}

// CountFiles returns the number of non-directory files in a location.
func (d *DB) CountFiles(ctx context.Context, locationID string) (int64, int64, error) {
	var count, totalSize int64
	err := d.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(size),0) FROM files WHERE location_id=? AND kind='file'`,
		locationID,
	).Scan(&count, &totalSize)
	return count, totalSize, err
}

// Search performs FTS5 + filter queries. Returns a paginated page.
func (d *DB) Search(ctx context.Context, q SearchQuery) (provider.ListPage, error) {
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 150
	}
	var offset int
	if _, err := fmt.Sscanf(q.Cursor, "%d", &offset); err != nil {
		offset = 0
	}

	// Build WHERE from filters; FTS is a JOIN.
	var where []string
	var args []any

	if match := ftsMatch(q.Text); match != "" {
		where = append(where, "f.rowid IN (SELECT rowid FROM files_fts WHERE name MATCH ?)")
		args = append(args, match)
	}
	if len(q.LocationIDs) > 0 {
		placeholders := strings.Repeat("?,", len(q.LocationIDs))
		placeholders = placeholders[:len(placeholders)-1]
		where = append(where, "f.location_id IN ("+placeholders+")")
		for _, id := range q.LocationIDs {
			args = append(args, id)
		}
	}
	if q.SizeMin > 0 {
		where = append(where, "f.size >= ?")
		args = append(args, q.SizeMin)
	}
	if q.SizeMax > 0 {
		where = append(where, "f.size <= ?")
		args = append(args, q.SizeMax)
	}
	if q.ModifiedFrom != "" {
		where = append(where, "f.modified_at >= ?")
		args = append(args, q.ModifiedFrom)
	}
	if q.ModifiedTo != "" {
		where = append(where, "f.modified_at <= ?")
		args = append(args, q.ModifiedTo)
	}
	if q.Kind != "" {
		where = append(where, "f.kind = ?")
		args = append(args, q.Kind)
	}

	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}

	query := "SELECT f.id,f.location_id,f.parent_id,f.name,f.kind,f.size," + //nolint:gosec // whereSQL is built from validated filters, never raw user input
		"f.mime,f.etag,f.md5,f.preview_kind,f.modified_at" +
		" FROM files f " + whereSQL +
		" ORDER BY f.modified_at DESC" +
		" LIMIT ? OFFSET ?"

	args = append(args, q.Limit+1, offset)
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return provider.ListPage{}, err
	}
	defer rows.Close()

	var items []provider.FileInfo
	for rows.Next() {
		f, err := scanFileRow(rows)
		if err != nil {
			return provider.ListPage{}, err
		}
		items = append(items, f)
	}
	if err := rows.Err(); err != nil {
		return provider.ListPage{}, err
	}

	var nextCursor string
	if len(items) > q.Limit {
		items = items[:q.Limit]
		nextCursor = fmt.Sprintf("%d", offset+q.Limit)
	}
	return provider.ListPage{Items: items, Cursor: nextCursor}, nil
}

// SearchQuery parameterises a Search call.
type SearchQuery struct {
	Text         string
	LocationIDs  []string
	SizeMin      int64
	SizeMax      int64
	ModifiedFrom string
	ModifiedTo   string
	Kind         string // "file" | "dir" | ""
	Limit        int
	Cursor       string
}

// ---- Transfers ------------------------------------------------------------ //

// TransferBatch mirrors the transfer_batches row plus computed job counts.
type TransferBatch struct {
	ID                                     string
	Action                                 string // copy | move | upload
	Conflict                               string // smart | overwrite | skip
	DeleteSource                           bool
	SourceLocID                            string
	DestLocID                              string
	DestDirID                              string
	CreatedAt                              time.Time
	Total, Done, Failed, Skipped, Canceled int
}

// TransferJob mirrors the transfers row.
type TransferJob struct {
	ID           string
	BatchID      string
	SourceFileID string
	SourceItemID string // the selection item this job descended from
	SourceName   string
	SourceLocID  string
	DestLocID    string
	DestDirID    string
	DestPath     string
	State        string // pending|running|done|failed|skipped|canceled|paused
	BytesTotal   int64
	BytesDone    int64
	MD5Verified  bool
	Error        string
	UpdatedAt    time.Time
}

// CreateTransferBatch persists a batch and its jobs atomically.
func (d *DB) CreateTransferBatch(ctx context.Context, b TransferBatch, jobs []TransferJob) error {
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("create transfer batch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for i := range jobs {
		jobs[i].BatchID = b.ID
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO transfer_batches(id, action, conflict, delete_source, source_loc_id, dest_loc_id, dest_dir_id, created_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		b.ID, b.Action, b.Conflict, b.DeleteSource, b.SourceLocID, b.DestLocID, b.DestDirID, now,
	); err != nil {
		return fmt.Errorf("create transfer batch: %w", err)
	}
	for _, j := range jobs {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO transfers(id, batch_id, source_file_id, source_name,
				source_loc_id, dest_loc_id, dest_dir_id, dest_path, state, bytes_total,
				bytes_done, md5_verified, error, source_item_id, created_at, updated_at)
				VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			j.ID,
			j.BatchID,
			j.SourceFileID,
			j.SourceName,
			j.SourceLocID,
			j.DestLocID,
			j.DestDirID,
			j.DestPath,
			"pending",
			j.BytesTotal,
			0,
			0,
			"",
			j.SourceItemID,
			now,
			now,
		); err != nil {
			return fmt.Errorf("create transfer job %q: %w", j.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("create transfer batch: %w", err)
	}
	return nil
}

// NextPendingJobs returns up to limit pending jobs for dispatch.
func (d *DB) NextPendingJobs(ctx context.Context, limit int) ([]TransferJob, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id,batch_id,source_file_id,source_name,source_loc_id,dest_loc_id,dest_dir_id,dest_path,state,bytes_total,bytes_done,md5_verified,error,updated_at,source_item_id
		 FROM transfers WHERE state='pending' ORDER BY created_at LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("next pending jobs: %w", err)
	}
	defer rows.Close()
	return scanTransferJobs(rows)
}

// UpdateTransferJobState sets a job's terminal or intermediate state.
func (d *DB) UpdateTransferJobState(ctx context.Context, jobID, state string, md5Verified bool, errMsg string) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE transfers SET state=?, md5_verified=?, error=?, updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=?`,
		state, md5Verified, errMsg, jobID)
	if err != nil {
		return fmt.Errorf("update transfer job state: %w", err)
	}
	return nil
}

// UpdateTransferJobProgress advances the byte counter.
func (d *DB) UpdateTransferJobProgress(ctx context.Context, jobID string, bytesDone, bytesTotal int64) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE transfers SET bytes_done=?, bytes_total=?, updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=?`,
		bytesDone, bytesTotal, jobID)
	if err != nil {
		return fmt.Errorf("update transfer job progress: %w", err)
	}
	return nil
}

// JobsForBatches returns all jobs of the given batches.
func (d *DB) JobsForBatches(ctx context.Context, batchIDs []string) ([]TransferJob, error) {
	if len(batchIDs) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(batchIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(batchIDs))
	for _, id := range batchIDs {
		args = append(args, id)
	}
	//nolint:gosec // placeholders are count-derived, never user input (same pattern as ListDir)
	rows, err := d.db.QueryContext(ctx,
		`SELECT id,batch_id,source_file_id,source_name,source_loc_id,dest_loc_id,dest_dir_id,dest_path,state,bytes_total,bytes_done,md5_verified,error,updated_at,source_item_id
		 FROM transfers WHERE batch_id IN (`+placeholders+`) ORDER BY created_at`, args...)
	if err != nil {
		return nil, fmt.Errorf("jobs for batches: %w", err)
	}
	defer rows.Close()
	return scanTransferJobs(rows)
}

// ListTransferBatches returns all batches with computed job counts.
func (d *DB) ListTransferBatches(ctx context.Context) ([]TransferBatch, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT b.id, b.action, b.conflict, b.delete_source, b.source_loc_id, b.dest_loc_id, b.dest_dir_id, b.created_at,
		   COALESCE(SUM(t.state='done'),0), COALESCE(SUM(t.state='failed'),0), COALESCE(SUM(t.state='skipped'),0), COALESCE(SUM(t.state='canceled'),0), COUNT(t.id)
		 FROM transfer_batches b LEFT JOIN transfers t ON t.batch_id = b.id
		 GROUP BY b.id ORDER BY b.created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list transfer batches: %w", err)
	}
	defer rows.Close()
	var out []TransferBatch
	for rows.Next() {
		var b TransferBatch
		var createdAt string
		if err := rows.Scan(&b.ID, &b.Action, &b.Conflict, &b.DeleteSource, &b.SourceLocID, &b.DestLocID, &b.DestDirID, &createdAt,
			&b.Done, &b.Failed, &b.Skipped, &b.Canceled, &b.Total); err != nil {
			return nil, fmt.Errorf("list transfer batches: %w", err)
		}
		b.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		out = append(out, b)
	}
	return out, rows.Err()
}

// FailInterruptedTransfers flips running/paused jobs to failed at startup.
func (d *DB) FailInterruptedTransfers(ctx context.Context, reason string) (int64, error) {
	res, err := d.db.ExecContext(ctx,
		`UPDATE transfers SET state='failed', error=?, updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now')
		 WHERE state IN ('running','paused')`, reason)
	if err != nil {
		return 0, fmt.Errorf("fail interrupted transfers: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("fail interrupted transfers: %w", err)
	}
	return n, nil
}

func scanTransferJobs(rows *sql.Rows) ([]TransferJob, error) {
	var out []TransferJob
	for rows.Next() {
		var j TransferJob
		var updated string
		var md5 int
		if err := rows.Scan(
			&j.ID,
			&j.BatchID,
			&j.SourceFileID,
			&j.SourceName,
			&j.SourceLocID,
			&j.DestLocID,
			&j.DestDirID,
			&j.DestPath,
			&j.State,
			&j.BytesTotal,
			&j.BytesDone,
			&md5,
			&j.Error,
			&updated,
			&j.SourceItemID,
		); err != nil {
			return nil, err
		}
		j.MD5Verified = md5 != 0
		j.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, j)
	}
	return out, rows.Err()
}

// ---- helpers ------------------------------------------------------------- //

type scanner interface {
	Scan(dest ...any) error
}

func scanFile(s scanner) (provider.FileInfo, error) {
	return scanFileRow(s)
}

func scanFileRow(s scanner) (provider.FileInfo, error) {
	var f provider.FileInfo
	var kind, modAt string
	if err := s.Scan(
		&f.ID, &f.LocationID, &f.ParentID, &f.Name,
		&kind, &f.Size, &f.MIME, &f.Etag, &f.MD5, &f.PreviewKind, &modAt,
	); err != nil {
		return provider.FileInfo{}, err
	}
	f.Kind = provider.FileKind(kind)
	f.ModifiedAt, _ = time.Parse(time.RFC3339, modAt)
	return f, nil
}

// ---- Sync pairs ------------------------------------------------------------ //

// SyncPair is one sync policy row between two location folders.
type SyncPair struct {
	ID         string
	Name       string
	LocationA  string
	PathA      string
	LocationB  string
	PathB      string
	Direction  string // a_to_b | b_to_a | both
	Actions    int    // bitmask: 1=created, 2=modified, 4=deleted
	Paused     bool
	LastRunAt  string
	LastError  string
	LastCounts string // JSON summary of the last reconcile
	CreatedAt  string
}

const syncPairCols = "id,name,location_a,path_a,location_b,path_b,direction,actions,paused,last_run_at,last_error,last_counts,created_at"

// CreateSyncPair inserts a new pair row.
func (d *DB) CreateSyncPair(ctx context.Context, p SyncPair) error {
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO sync_pairs(id,name,location_a,path_a,location_b,path_b,direction,actions,paused)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.LocationA, p.PathA, p.LocationB, p.PathB, p.Direction, p.Actions, p.Paused,
	)
	return err
}

// scanPairs scans full sync_pairs rows ordered by creation.
func (d *DB) scanPairs(rows *sql.Rows) ([]SyncPair, error) {
	defer rows.Close()
	var out []SyncPair
	for rows.Next() {
		var p SyncPair
		var actions int
		var paused int
		if err := rows.Scan(&p.ID, &p.Name, &p.LocationA, &p.PathA, &p.LocationB, &p.PathB,
			&p.Direction, &actions, &paused, &p.LastRunAt, &p.LastError, &p.LastCounts, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Actions = actions
		p.Paused = paused != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListSyncPairs returns every pair ordered by creation.
func (d *DB) ListSyncPairs(ctx context.Context) ([]SyncPair, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT `+syncPairCols+` FROM sync_pairs ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	return d.scanPairs(rows)
}

// GetSyncPair returns one pair or sql.ErrNoRows.
func (d *DB) GetSyncPair(ctx context.Context, id string) (SyncPair, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT `+syncPairCols+` FROM sync_pairs WHERE id=?`, id)
	if err != nil {
		return SyncPair{}, err
	}
	pairs, err := d.scanPairs(rows)
	if err != nil {
		return SyncPair{}, err
	}
	if len(pairs) == 0 {
		return SyncPair{}, sql.ErrNoRows
	}
	return pairs[0], nil
}

// UpdateSyncPair rewrites the user-editable pair fields; status and
// created_at are preserved.
func (d *DB) UpdateSyncPair(ctx context.Context, p SyncPair) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE sync_pairs SET name=?, path_a=?, path_b=?, direction=?, actions=?, paused=? WHERE id=?`,
		p.Name, p.PathA, p.PathB, p.Direction, p.Actions, p.Paused, p.ID,
	)
	return err
}

// DeleteSyncPair removes the pair and its sync_state rows.
func (d *DB) DeleteSyncPair(ctx context.Context, id string) error {
	if _, err := d.db.ExecContext(ctx, `DELETE FROM sync_state WHERE pair_id=?`, id); err != nil {
		return err
	}
	_, err := d.db.ExecContext(ctx, `DELETE FROM sync_pairs WHERE id=?`, id)
	return err
}

// UpdateSyncPairStatus records the outcome of the last reconcile. countsJSON
// and errText are mutually exclusive: an error clears the counts.
func (d *DB) UpdateSyncPairStatus(ctx context.Context, id, runAt, countsJSON, errText string) error {
	if errText != "" {
		countsJSON = ""
	}
	_, err := d.db.ExecContext(ctx,
		`UPDATE sync_pairs SET last_run_at=?, last_counts=?, last_error=? WHERE id=?`,
		runAt, countsJSON, errText, id,
	)
	return err
}

// SyncState is the per-path record of what a pair has synced so far.
type SyncState struct {
	PairID       string
	Path         string
	IDA          string
	IDB          string
	LastEtagA    string
	LastEtagB    string
	LastMtimeA   string
	LastMtimeB   string
	LastSyncedAt string
}

// UpsertSyncState records or replaces one path's state.
func (d *DB) UpsertSyncState(ctx context.Context, s SyncState) error {
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO sync_state(pair_id,path,id_a,id_b,last_etag_a,last_etag_b,last_mtime_a,last_mtime_b,last_synced_at)
		 VALUES (?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(pair_id,path) DO UPDATE SET id_a=excluded.id_a, id_b=excluded.id_b,
		   last_etag_a=excluded.last_etag_a, last_etag_b=excluded.last_etag_b,
		   last_mtime_a=excluded.last_mtime_a, last_mtime_b=excluded.last_mtime_b,
		   last_synced_at=excluded.last_synced_at`,
		s.PairID, s.Path, s.IDA, s.IDB, s.LastEtagA, s.LastEtagB, s.LastMtimeA, s.LastMtimeB, s.LastSyncedAt,
	)
	return err
}

// DeleteSyncState drops one path's record (e.g. after both sides are gone).
func (d *DB) DeleteSyncState(ctx context.Context, pairID, path string) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM sync_state WHERE pair_id=? AND path=?`, pairID, path)
	return err
}

// SyncStateRows returns every state row for a pair.
func (d *DB) SyncStateRows(ctx context.Context, pairID string) ([]SyncState, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT pair_id,path,id_a,id_b,last_etag_a,last_etag_b,last_mtime_a,last_mtime_b,last_synced_at
		 FROM sync_state WHERE pair_id=? ORDER BY path`, pairID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncState
	for rows.Next() {
		var s SyncState
		if err := rows.Scan(&s.PairID, &s.Path, &s.IDA, &s.IDB,
			&s.LastEtagA, &s.LastEtagB, &s.LastMtimeA, &s.LastMtimeB, &s.LastSyncedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
