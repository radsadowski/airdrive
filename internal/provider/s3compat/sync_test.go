package s3compat

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/radosny/airdrive/internal/provider"
)

func TestWhenTheBucketIsWalkedThenFilesAndSynthesizedDirsAreEmitted(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	p, _ := newTestProvider(t, map[string]string{
		"photos/2024/a.png": "PNGDATA",
		"readme.txt":        "hi",
	})

	var events []provider.ChangeEvent
	cursor, err := p.SyncChanges(context.Background(), "L1", "", func(batch []provider.ChangeEvent) {
		events = append(events, batch...)
	})
	r.NoError(err)
	r.Empty(cursor, "S3 has no incremental cursor — refresh always re-walks")

	byID := map[string]provider.ChangeKind{}
	for _, e := range events {
		byID[e.FileID] = e.Kind
	}
	r.Equal(map[string]provider.ChangeKind{
		"readme.txt":        provider.ChangeCreated,
		"photos/":           provider.ChangeCreated,
		"photos/2024/":      provider.ChangeCreated,
		"photos/2024/a.png": provider.ChangeCreated,
	}, byID, "every key's ancestor prefixes become explicit dir events")

	for _, e := range events {
		if e.File == nil {
			continue
		}
		r.Equal(e.FileID, e.File.ID)
		if e.File.Kind == provider.FileKindDir {
			r.Empty(e.File.Size)
		}
	}
}

func TestWhenTheWalkRunsThenTrashIsNeverEmitted(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	p, _ := newTestProvider(t, map[string]string{
		"readme.txt":                       "hi",
		".airdrive-trash/123/readme.txt":   "deleted",
		".airdrive-trash/123/sub/":         "",
		".airdrive-trash/123/sub/olda.png": "old",
	})

	var events []provider.ChangeEvent
	_, err := p.SyncChanges(context.Background(), "L1", "ANYCURSOR", func(batch []provider.ChangeEvent) {
		events = append(events, batch...)
	})
	r.NoError(err)

	for _, e := range events {
		r.NotContains(e.FileID, ".airdrive-trash/", "trash must stay invisible to the index")
	}
	r.Len(events, 1)
	r.Equal("readme.txt", events[0].FileID)
}
