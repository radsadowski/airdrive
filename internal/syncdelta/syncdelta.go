// Package syncdelta computes the file-level actions needed to converge the
// two folders of a sync pair. It is deliberately pure: callers bring file
// maps (from the index) and recorded state, and get back an ordered plan.
package syncdelta

import (
	"path"
	"sort"
	"strings"
	"time"
)

// Action mask values mirrored to sync_pairs.actions.
const (
	ActCreate = 1
	ActModify = 2
	ActDelete = 4
)

// Direction selects which edge of a pair may write.
type Direction string

// Valid directions: one-way A→B, one-way B→A, or two-way.
const (
	AToB Direction = "a_to_b"
	BToA Direction = "b_to_a"
	Both Direction = "both"
)

// File is the engine's view of one indexed file, keyed by its path
// relative to the paired root.
type File struct {
	Path  string
	ID    string
	Kind  string // "file" | "dir"
	Size  int64
	Etag  string
	Mtime time.Time
}

// Side is one location's subtree as a path-keyed map.
type Side map[string]File

// StateEntry is what the pair remembers from previous reconciles. Etags and
// mtimes are tracked per side: providers' etags are not comparable to each
// other, only to their own previous values.
type StateEntry struct {
	Path   string
	IDA    string
	IDB    string
	EtagA  string
	EtagB  string
	MtimeA time.Time
	MtimeB time.Time
}

// StepKind enumerates the apply actions the caller must execute.
type StepKind int

// Step kinds in execution order matter: copies parents-first within this
// package's sort, deletes children-first.
const (
	CopyAToB StepKind = iota
	CopyBToA
	DeleteOnA
	DeleteOnB
)

// Step is one action to execute. Src is set for copies, Dst for deletes.
type Step struct {
	Kind StepKind
	Path string
	Src  File
	Dst  File
}

// Plan computes the ordered actions for one reconcile pass. backfill makes
// the pass copy what the destination lacks without ever deleting, and
// ignores the actions mask for copies (a fresh pair exists to converge).
func Plan(dir Direction, actions int, a, b Side, state map[string]StateEntry, backfill bool) []Step {
	var steps []Step
	paths := map[string]bool{}
	for p := range a {
		paths[p] = true
	}
	for p := range b {
		paths[p] = true
	}
	for p := range state {
		paths[p] = true
	}

	for p := range paths {
		fa, onA := a[p]
		fb, onB := b[p]
		se, hasState := state[p]

		switch {
		case backfill:
			if onA && !onB && dir != BToA {
				steps = append(steps, copyStep(CopyAToB, fa))
			}
			if onB && !onA && dir != AToB {
				steps = append(steps, copyStep(CopyBToA, fb))
			}
			continue

		case hasState && !onA && onB:
			steps = deleteIf(steps, DeleteOnB, fb, dir != BToA, actions)
			continue
		case hasState && !onB && onA:
			steps = deleteIf(steps, DeleteOnA, fa, dir != AToB, actions)
			continue
		case hasState && !onA && !onB:
			continue // both vanished; the engine just drops the row

		case !onA && onB:
			if allowed(dir, CopyBToA) && actions&ActCreate != 0 {
				steps = append(steps, copyStep(CopyBToA, fb))
			}
			continue
		case onA && !onB:
			if allowed(dir, CopyAToB) && actions&ActCreate != 0 {
				steps = append(steps, copyStep(CopyAToB, fa))
			}
			continue
		case !onA || !onB:
			continue
		}

		chgA := changed(fa, se.EtagA, se.MtimeA, true)
		chgB := changed(fb, se.EtagB, se.MtimeB, false)
		switch {
		case chgA && chgB && actions&ActModify != 0:
			if fa.Mtime.After(fb.Mtime) || fa.Mtime.Equal(fb.Mtime) {
				if allowed(dir, CopyAToB) {
					steps = append(steps, copyStep(CopyAToB, fa))
				}
			} else if allowed(dir, CopyBToA) {
				steps = append(steps, copyStep(CopyBToA, fb))
			}
		case chgA && actions&ActModify != 0 && allowed(dir, CopyAToB):
			steps = append(steps, copyStep(CopyAToB, fa))
		case chgB && actions&ActModify != 0 && allowed(dir, CopyBToA):
			steps = append(steps, copyStep(CopyBToA, fb))
		}
	}

	sort.SliceStable(steps, func(i, j int) bool {
		if steps[i].Kind == DeleteOnA || steps[i].Kind == DeleteOnB ||
			steps[j].Kind == DeleteOnA || steps[j].Kind == DeleteOnB {
			iDel := steps[i].Kind == DeleteOnA || steps[i].Kind == DeleteOnB
			jDel := steps[j].Kind == DeleteOnA || steps[j].Kind == DeleteOnB
			if iDel != jDel {
				return !iDel // copies first
			}
			return depth(steps[i].Path) > depth(steps[j].Path) // deletes children-first
		}
		return depth(steps[i].Path) < depth(steps[j].Path) // copies parents-first
	})
	return steps
}

// allowed reports whether the direction permits a step of that kind.
func allowed(dir Direction, k StepKind) bool {
	if dir == Both {
		return true
	}
	if dir == AToB {
		return k == CopyAToB || k == DeleteOnB
	}
	return k == CopyBToA || k == DeleteOnA
}

// changed compares one side against its own recorded baseline: etag when
// both values exist, otherwise mtime drift, defaulting to unchanged when
// the side lacks a baseline at all.
func changed(f File, seEtag string, seMtime time.Time, _ bool) bool {
	if seEtag == "" && seMtime.IsZero() {
		return false // first-known baseline pairs treat present files as settled
	}
	if seEtag != "" && f.Etag != "" {
		return f.Etag != seEtag
	}
	return !f.Mtime.Equal(seMtime)
}

// copyStep wraps a copy action for a file or directory.
func copyStep(k StepKind, f File) Step {
	return Step{Kind: k, Path: f.Path, Src: f}
}

// deleteIf appends a delete step when its mask and direction allow it.
func deleteIf(steps []Step, k StepKind, f File, dirAllows bool, actions int) []Step {
	if dirAllows && actions&ActDelete != 0 {
		return append(steps, Step{Kind: k, Path: f.Path, Dst: f})
	}
	return steps
}

// depth counts path segments so parents order before their children.
func depth(p string) int {
	return strings.Count(path.Clean(p), "/") + 1
}
