// SyncChanges — S3 has no changes feed, so every sync is a full recursive
// walk of the bucket; deletions are detected by the bridge's reconcile.
package s3compat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/radosny/airdrive/internal/provider"
)

// SyncChanges walks the whole bucket recursively, emitting ChangeCreated for
// every object and synthesizing explicit dir events for each key's ancestor
// prefixes (navigation needs dir rows). Trash-prefixed keys are invisible.
// The returned cursor is always "" — callers re-walk on every sync.
func (p *Provider) SyncChanges(ctx context.Context, locationID, cursor string, emit func([]provider.ChangeEvent)) (string, error) {
	seenDirs := map[string]bool{}
	batch := make([]provider.ChangeEvent, 0, 256)
	flush := func() {
		if len(batch) > 0 {
			emit(batch)
			batch = make([]provider.ChangeEvent, 0, 256)
		}
	}
	dirEvent := func(prefix string) {
		if seenDirs[prefix] {
			return
		}
		seenDirs[prefix] = true
		fi := p.toFileInfo(prefix, 0, "", time.Time{})
		batch = append(batch, provider.ChangeEvent{FileID: prefix, Kind: provider.ChangeCreated, File: &fi})
	}

	for obj := range p.client.ListObjects(ctx, p.bucket, minio.ListObjectsOptions{Prefix: "", Recursive: true}) {
		if obj.Err != nil {
			return "", fmt.Errorf("s3compat: walk bucket %q: %w", p.bucket, obj.Err)
		}
		key := obj.Key
		if isTrashKey(key) {
			continue
		}
		if key == "" {
			continue
		}
		for _, prefix := range ancestorPrefixes(key) {
			dirEvent(prefix)
		}
		fi := p.toFileInfo(key, obj.Size, obj.ETag, obj.LastModified)
		batch = append(batch, provider.ChangeEvent{FileID: key, Kind: provider.ChangeCreated, File: &fi})
		if len(batch) >= 500 {
			flush()
		}
	}
	flush()
	return "", nil
}

// isTrashKey reports whether a key lives under the in-bucket trash prefix.
func isTrashKey(key string) bool {
	return len(key) >= len(TrashPrefix) && key[:len(TrashPrefix)] == TrashPrefix
}

// ancestorPrefixes lists a key's ancestor dir prefixes, nearest first.
func ancestorPrefixes(key string) []string {
	var out []string
	rest := key
	for {
		i := strings.LastIndex(rest, "/")
		if i <= 0 {
			return out
		}
		rest = rest[:i+1]
		out = append(out, rest)
		rest = strings.TrimSuffix(rest, "/")
	}
}
