// Package entities holds all cross-layer types exposed to the Wails bridge
// (mirroring TypeScript contract types) and shared between internal packages.
//
// Rules (see also CLAUDE.md):
//   - Every struct exposed to the Wails bridge lives here.
//   - Converter functions (Go domain model → entity type) live in converters.go.
package entities

// File mirrors the TypeScript FileInfo contract.
type File struct {
	ID          string `json:"id"`
	LocationID  string `json:"locationId"`
	ParentID    string `json:"parentId"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Size        int64  `json:"size"`
	MIME        string `json:"mime"`
	ModifiedAt  string `json:"modifiedAt"`
	Etag        string `json:"etag"`
	PreviewKind string `json:"previewKind,omitempty"`
}

// Page is a paginated result with an opaque cursor.
type Page struct {
	Items  []File `json:"items"`
	Cursor string `json:"cursor"`
}

// ListDirOpts mirrors the frontend ListDirOptions.
type ListDirOpts struct {
	SortField     string `json:"sortField,omitempty"`
	SortAsc       bool   `json:"sortAsc"`
	Cursor        string `json:"cursor,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	IncludeHidden bool   `json:"includeHidden,omitempty"`
}

// Location mirrors the TypeScript Location contract.
type Location struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Root         string `json:"root"`
	Status       string `json:"status"`
	FileCount    int64  `json:"fileCount"`
	TotalSize    int64  `json:"totalSize"`
	LastSyncedAt string `json:"lastSyncedAt"`
	Error        string `json:"error,omitempty"`
}

// GdriveSession is the pre-location sign-in state of the Drive connect flow:
// browsing and confirmation reference it until the location is created.
type GdriveSession struct {
	SessionID string `json:"sessionId"`
	Email     string `json:"email"`
	Name      string `json:"name"`
}

// S3Config carries a bucket connection from the wizard; the secret is used
// once to reach the Keychain and never persisted elsewhere.
type S3Config struct {
	Preset          string `json:"preset"`
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	PathStyle       bool   `json:"pathStyle"`
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
}

// S3TestResult answers the wizard's Test connection button.
type S3TestResult struct {
	OK      bool     `json:"ok"`
	Buckets []string `json:"buckets"`
	Error   string   `json:"error,omitempty"`
}
