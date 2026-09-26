// Package provider defines the storage abstraction layer.
// Every storage backend (localfs, gdrive, s3compat) implements StorageProvider.
// Nothing outside this package knows which backend is in use.
package provider

import (
	"context"
	"io"
	"time"
)

//go:generate go run go.uber.org/mock/mockgen -source=provider.go -destination=mock/mock.go -package=mock

// Kind identifies the backend type stored in the locations table.
type Kind string

const (
	KindLocalFS Kind = "localfs"
	KindGDrive  Kind = "gdrive"
	KindS3      Kind = "s3"
)

// FileKind distinguishes files from directories.
type FileKind string

const (
	FileKindFile FileKind = "file"
	FileKindDir  FileKind = "dir"
)

// FileInfo is the location-agnostic file record returned by providers and
// stored in the SQLite index. It maps 1:1 to the TypeScript FileInfo contract.
type FileInfo struct {
	ID         string
	LocationID string
	ParentID   string // empty string = root of location
	Name       string
	Kind       FileKind
	Size       int64
	MIME       string
	ModifiedAt time.Time
	// Etag is the change-detection anchor:
	//   localfs → "<modtime_unix>:<size>"
	//   gdrive  → Drive revision ID
	//   s3      → ETag from S3 (may not be MD5 for multipart)
	Etag        string
	PreviewKind string // "image" | "text" | "pdf" | "video" | "audio" | "binary"
}

// SortField enumerates the sortable columns.
type SortField string

const (
	SortByName       SortField = "name"
	SortBySize       SortField = "size"
	SortByModifiedAt SortField = "modified_at"
	SortByKind       SortField = "kind"
)

// SortSpec encodes a column + direction.
type SortSpec struct {
	Field SortField
	Asc   bool
}

// ListPage is a paginated result set. Cursor is opaque and empty when exhausted.
type ListPage struct {
	Items  []FileInfo
	Cursor string
}

// ChangeEvent signals that a file was created, modified, or deleted.
type ChangeEvent struct {
	FileID string
	Kind   ChangeKind
	File   *FileInfo // nil on deletion
}

// ChangeKind classifies a ChangeEvent.
type ChangeKind string

// Reconciler marks providers that cannot detect deletions themselves
// (no incremental changes feed): the bridge detects them by diffing the
// walk against the index.
type Reconciler interface {
	Reconciles() bool
}

// Thumbnailer is the optional provider capability for rendering thumbnails
// (small, bounded image bytes). Providers that cannot produce them simply
// don't implement it.
type Thumbnailer interface {
	Thumbnail(ctx context.Context, locationID, fileID string) ([]byte, error)
}

const (
	ChangeCreated  ChangeKind = "created"
	ChangeModified ChangeKind = "modified"
	ChangeDeleted  ChangeKind = "deleted"
)

// PreviewImage is the PreviewKind value marking image files (thumbnails).
const PreviewImage = "image"

// StorageProvider is the interface every backend must implement.
// The index, transfer manager, and bridge never know which backend they talk to.
type StorageProvider interface {
	ID() string
	Kind() Kind

	// List returns direct children of dirID (empty = root).
	List(ctx context.Context, locationID, dirID string, sort SortSpec, cursor string, limit int) (ListPage, error)

	// Stat returns metadata for a single file or directory.
	Stat(ctx context.Context, locationID, fileID string) (FileInfo, error)

	// Mkdir creates a directory. Returns the created node.
	Mkdir(ctx context.Context, locationID, parentID, name string) (FileInfo, error)

	// Rename changes the name of a node (same parent).
	Rename(ctx context.Context, locationID, fileID, newName string) (FileInfo, error)

	// MoveWithin moves fileIDs under destDirID (same provider, same location).
	MoveWithin(ctx context.Context, locationID string, fileIDs []string, destDirID string) error

	// Delete moves nodes to the provider's trash (never permanent in Phase 2).
	Delete(ctx context.Context, locationID string, fileIDs []string) error

	// Download opens a readable stream for the file. Caller must close it.
	Download(ctx context.Context, locationID, fileID string) (io.ReadCloser, FileInfo, error)

	// Upload writes r into the location at parentID/name.
	Upload(ctx context.Context, locationID, parentID, name string, r io.Reader, size int64) (FileInfo, error)

	// SyncChanges fetches incremental changes since cursor, calling emit for each batch.
	// Returns the next cursor to persist. Cursor "" means "full walk".
	SyncChanges(ctx context.Context, locationID, cursor string, emit func([]ChangeEvent)) (string, error)
}
