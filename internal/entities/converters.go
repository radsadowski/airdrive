package entities

import (
	"time"

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
