package entities

import (
	"time"

	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
)

// ToFile converts a provider.FileInfo domain model to its frontend entity type.
func ToFile(f provider.FileInfo) File {
	return File{
		ID:          f.ID,
		LocationID:  f.LocationID,
		ParentID:    f.ParentID,
		Name:        f.Name,
		Kind:        string(f.Kind),
		Size:        f.Size,
		MIME:        f.MIME,
		ModifiedAt:  f.ModifiedAt.UTC().Format(time.RFC3339),
		Etag:        f.Etag,
		PreviewKind: f.PreviewKind,
	}
}

// ToPage converts a provider.ListPage to its frontend entity type.
func ToPage(page provider.ListPage) Page {
	items := make([]File, 0, len(page.Items))
	for _, f := range page.Items {
		items = append(items, ToFile(f))
	}
	return Page{Items: items, Cursor: page.Cursor}
}

// ToEntitySyncPair converts an index.SyncPair row to its frontend entity type.
func ToEntitySyncPair(p index.SyncPair) SyncPair {
	return SyncPair{
		ID:         p.ID,
		Name:       p.Name,
		LocationA:  p.LocationA,
		PathA:      p.PathA,
		LocationB:  p.LocationB,
		PathB:      p.PathB,
		Direction:  p.Direction,
		Actions:    p.Actions,
		Paused:     p.Paused,
		LastRunAt:  p.LastRunAt,
		LastError:  p.LastError,
		LastCounts: p.LastCounts,
		CreatedAt:  p.CreatedAt,
	}
}

// FromEntitySyncPair converts a frontend SyncPair to its index row form.
func FromEntitySyncPair(p SyncPair) index.SyncPair {
	return index.SyncPair{
		ID:        p.ID,
		Name:      p.Name,
		LocationA: p.LocationA,
		PathA:     p.PathA,
		LocationB: p.LocationB,
		PathB:     p.PathB,
		Direction: p.Direction,
		Actions:   p.Actions,
		Paused:    p.Paused,
	}
}
