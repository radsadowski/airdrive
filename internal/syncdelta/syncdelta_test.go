package syncdelta

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	actAll   = ActCreate | ActModify | ActDelete
	actNoDel = ActCreate | ActModify
)

var base = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func f(path, id, etag string, m int) File {
	return File{Path: path, ID: id, Kind: "file", Size: 10, Etag: etag, Mtime: base.Add(time.Duration(m) * time.Minute)}
}

func dir(path, id string) File {
	return File{Path: path, ID: id, Kind: "dir", Mtime: base}
}

// TestWhenTheDeltaIsComputedThenEachRuleApplies tables every spec rule.
func TestWhenTheDeltaIsComputedThenEachRuleApplies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		dir      Direction
		actions  int
		backfill bool
		a, b     Side
		state    map[string]StateEntry
		want     []Step
	}{
		{
			name: "when a file is new on the source then it copies A to B",
			dir:  AToB, actions: actAll,
			a:    Side{"a.jpg": f("a.jpg", "A1", "e1", 1)},
			b:    Side{},
			want: []Step{{Kind: CopyAToB, Path: "a.jpg", Src: f("a.jpg", "A1", "e1", 1)}},
		},
		{
			name: "when creations are masked out then new files are ignored",
			dir:  AToB, actions: actNoDel &^ ActCreate,
			a:    Side{"a.jpg": f("a.jpg", "A1", "e1", 1)},
			want: nil,
		},
		{
			name: "when one-way then new files on B never copy back",
			dir:  AToB, actions: actAll,
			b:    Side{"b.jpg": f("b.jpg", "B1", "e1", 1)},
			want: nil,
		},
		{
			name: "when two-way then files new on B copy to A",
			dir:  Both, actions: actAll,
			b:    Side{"b.jpg": f("b.jpg", "B1", "e1", 1)},
			want: []Step{{Kind: CopyBToA, Path: "b.jpg", Src: f("b.jpg", "B1", "e1", 1)}},
		},
		{
			name: "when b-to-a then the direction mirrors a-to-b",
			dir:  BToA, actions: actAll,
			b:    Side{"b.jpg": f("b.jpg", "B1", "e1", 1)},
			want: []Step{{Kind: CopyBToA, Path: "b.jpg", Src: f("b.jpg", "B1", "e1", 1)}},
		},
		{
			name: "when the etag drifts on A then the update propagates",
			dir:  AToB, actions: actAll,
			a:     Side{"a.jpg": f("a.jpg", "A1", "e2", 5)},
			b:     Side{"a.jpg": f("a.jpg", "B1", "e-b", 4)},
			state: map[string]StateEntry{"a.jpg": {Path: "a.jpg", IDA: "A1", IDB: "B1", EtagA: "e1", MtimeA: base.Add(4 * time.Minute)}},
			want:  []Step{{Kind: CopyAToB, Path: "a.jpg", Src: f("a.jpg", "A1", "e2", 5)}},
		},
		{
			name: "when nothing changed since the last state then the plan is empty",
			dir:  AToB, actions: actAll,
			a:     Side{"a.jpg": f("a.jpg", "A1", "e1", 1)},
			b:     Side{"a.jpg": f("a.jpg", "B1", "e-b", 9)},
			state: map[string]StateEntry{"a.jpg": {Path: "a.jpg", EtagA: "e1", EtagB: "e-b"}},
			want:  nil,
		},
		{
			name: "when etags are empty then mtime plus size detect the change",
			dir:  AToB, actions: actAll,
			a:     Side{"a.jpg": f("a.jpg", "A1", "", 9)},
			b:     Side{"a.jpg": f("a.jpg", "B1", "", 9)},
			state: map[string]StateEntry{"a.jpg": {Path: "a.jpg", MtimeA: base.Add(4 * time.Minute), MtimeB: base.Add(9 * time.Minute)}},
			want:  []Step{{Kind: CopyAToB, Path: "a.jpg", Src: f("a.jpg", "A1", "", 9)}},
		},
		{
			name: "when only B changed and direction is one-way then updates never write back",
			dir:  AToB, actions: actAll,
			a:     Side{"a.jpg": f("a.jpg", "A1", "e1", 1)},
			b:     Side{"a.jpg": f("a.jpg", "B1", "e-b2", 9)},
			state: map[string]StateEntry{"a.jpg": {Path: "a.jpg", EtagA: "e1", EtagB: "e-b"}},
			want:  nil,
		},
		{
			name: "when a synced file vanishes from A then B deletes it",
			dir:  AToB, actions: actAll,
			b:     Side{"a.jpg": f("a.jpg", "B1", "e-b", 9)},
			state: map[string]StateEntry{"a.jpg": {Path: "a.jpg", IDA: "A1", IDB: "B1", EtagB: "e-b"}},
			want:  []Step{{Kind: DeleteOnB, Path: "a.jpg", Dst: f("a.jpg", "B1", "e-b", 9)}},
		},
		{
			name: "when deletions are masked out then vanishing files stay",
			dir:  AToB, actions: actNoDel,
			b:     Side{"a.jpg": f("a.jpg", "B1", "e-b", 9)},
			state: map[string]StateEntry{"a.jpg": {Path: "a.jpg", IDA: "A1", IDB: "B1"}},
			want:  nil,
		},
		{
			name: "when a file was never synced then it is never deleted",
			dir:  AToB, actions: actAll,
			b:    Side{"stray.jpg": f("stray.jpg", "B9", "e9", 9)},
			want: nil,
		},
		{
			name: "when both sides changed then the newer mtime wins",
			dir:  Both, actions: actAll,
			a:     Side{"a.jpg": f("a.jpg", "A1", "e2", 10)},
			b:     Side{"a.jpg": f("a.jpg", "B1", "e-b3", 12)},
			state: map[string]StateEntry{"a.jpg": {Path: "a.jpg", EtagA: "e1", EtagB: "e-b"}},
			want:  []Step{{Kind: CopyBToA, Path: "a.jpg", Src: f("a.jpg", "B1", "e-b3", 12)}},
		},
		{
			name: "when A is deleted but B changed then the delete wins",
			dir:  Both, actions: actAll,
			b:     Side{"a.jpg": f("a.jpg", "B1", "e-b3", 12)},
			state: map[string]StateEntry{"a.jpg": {Path: "a.jpg", IDA: "A1", IDB: "B1", EtagB: "e-b"}},
			want:  []Step{{Kind: DeleteOnB, Path: "a.jpg", Dst: f("a.jpg", "B1", "e-b3", 12)}},
		},
		{
			name: "when backfilling then only missing destinations copy and nothing deletes",
			dir:  AToB, actions: actAll, backfill: true,
			a:    Side{"a.jpg": f("a.jpg", "A1", "e1", 1), "b.jpg": f("b.jpg", "A2", "e2", 1)},
			b:    Side{"b.jpg": f("b.jpg", "BX", "eX", 3)},
			want: []Step{{Kind: CopyAToB, Path: "a.jpg", Src: f("a.jpg", "A1", "e1", 1)}},
		},
		{
			name: "when backfilling two-way then both sides receive what they lack",
			dir:  Both, actions: actAll, backfill: true,
			a: Side{"a.jpg": f("a.jpg", "A1", "e1", 1)},
			b: Side{"b.jpg": f("b.jpg", "B1", "e1", 1)},
			want: []Step{
				{Kind: CopyAToB, Path: "a.jpg", Src: f("a.jpg", "A1", "e1", 1)},
				{Kind: CopyBToA, Path: "b.jpg", Src: f("b.jpg", "B1", "e1", 1)},
			},
		},
		{
			name: "when a directory is missing then it copies as a dir step",
			dir:  AToB, actions: actAll,
			a:    Side{"pics": dir("pics", "D1")},
			want: []Step{{Kind: CopyAToB, Path: "pics", Src: dir("pics", "D1")}},
		},
		{
			name: "when deletions emit then children leave before parents",
			dir:  AToB, actions: actAll,
			b: Side{"pics/x.jpg": f("pics/x.jpg", "B2", "e", 1), "pics": dir("pics", "D1")},
			state: map[string]StateEntry{
				"pics":       {IDA: "D1"},
				"pics/x.jpg": {IDA: "A2", IDB: "B2"},
			},
			want: []Step{
				{Kind: DeleteOnB, Path: "pics/x.jpg", Dst: f("pics/x.jpg", "B2", "e", 1)},
				{Kind: DeleteOnB, Path: "pics", Dst: dir("pics", "D1")},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			got := Plan(tt.dir, tt.actions, tt.a, tt.b, tt.state, tt.backfill)
			r.Equal(tt.want, got)
		})
	}
}

// TestWhenAPlanIsAppliedThenTheNextPlanIsEmpty proves the loop-protection
// property for a round-trip of every copy/delete flavor.
func TestWhenAPlanIsAppliedThenTheNextPlanIsEmpty(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	a := Side{"a.jpg": f("a.jpg", "A1", "e1", 1), "old.jpg": f("old.jpg", "A9", "e9", 2)}
	b := Side{}
	steps := Plan(AToB, actAll, a, b, nil, false)
	r.Len(steps, 2, "both files must queue for copy")

	state := map[string]StateEntry{}
	for _, s := range steps {
		switch s.Kind {
		case CopyAToB:
			b[s.Path] = File{Path: s.Path, ID: s.Src.ID + "-dest", Kind: s.Src.Kind, Size: s.Src.Size, Etag: s.Src.Etag + "-b", Mtime: s.Src.Mtime}
			state[s.Path] = StateEntry{Path: s.Path, IDA: s.Src.ID, IDB: b[s.Path].ID, EtagA: s.Src.Etag, EtagB: b[s.Path].Etag, MtimeA: s.Src.Mtime, MtimeB: b[s.Path].Mtime}
		}
	}
	r.Empty(Plan(AToB, actAll, a, b, state, false), "replaying a recorded plan must produce no steps")
}
