package batchrename

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// Order turns Plan's changes into the sequence of os.Rename calls that
// actually carries them out without any one of them clobbering
// another — Plan allows a rename *chain* ("a" to "b" while "b" goes to
// "c"; see its own doc comment), and a chain only works if "b" moves
// away before "a" arrives.
//
// Most of the time that's a plain dependency order: each change must
// run after whichever other change vacates its destination, resolved
// here as a topological sort. A chain that closes into a *cycle* ("a"
// to "b" and "b" to "a" — a swap) has no such order at all; one member
// of the cycle is staged through a temporary name in the same
// directory instead (see tempNameFor), which breaks the cycle, and its
// final rename is deferred until after everything else has landed.
//
// The returned steps are the physical renames in the order to run
// them — possibly more steps than there were changes, whenever a
// temporary name was needed. Every step is what Apply performs and
// records, so Undo reversing them last-first (see Undo) restores
// exactly the original names through the same temporaries, in the
// only order that's guaranteed not to bump into anything.
func Order(changes []Change) []Change {
	n := len(changes)
	if n == 0 {
		return nil
	}

	// dependents[i] lists every change that must wait for change i —
	// those whose destination is change i's own current name.
	bySource := make(map[string]int, n)
	for i, c := range changes {
		bySource[c.From] = i
	}
	dependents := make([][]int, n)
	indegree := make([]int, n)
	for i, c := range changes {
		if j, ok := bySource[c.To]; ok && j != i {
			dependents[j] = append(dependents[j], i)
			indegree[i]++
		}
	}

	var (
		steps  []Change
		finals []Change // deferred second halves of every temp-staged change
		queue  []int
	)
	for i := range changes {
		if indegree[i] == 0 {
			queue = append(queue, i)
		}
	}
	done := make([]bool, n)
	remaining := n
	release := func(i int) {
		done[i] = true
		remaining--
		for _, d := range dependents[i] {
			indegree[d]--
			if indegree[d] == 0 {
				queue = append(queue, d)
			}
		}
	}

	for remaining > 0 {
		if len(queue) == 0 {
			// Every change still pending is part of a cycle. Break it at
			// the lowest-numbered one: move it out of the way to a
			// temporary name now, which frees its current name for
			// whichever change was waiting on it, and finish it last.
			for i := range changes {
				if !done[i] {
					tmp := tempNameFor(changes[i].From)
					steps = append(steps, Change{From: changes[i].From, To: tmp})
					finals = append(finals, Change{From: tmp, To: changes[i].To})
					release(i)
					break
				}
			}
			continue
		}
		i := queue[0]
		queue = queue[1:]
		if done[i] {
			continue
		}
		steps = append(steps, changes[i])
		release(i)
	}
	return append(steps, finals...)
}

// tempNameFor picks a temporary path in the same directory as path,
// hidden (leading dot) and carrying a random suffix so it can't
// collide with anything a user would plausibly have there, nor with
// another temporary from the same batch.
func tempNameFor(path string) string {
	var buf [6]byte
	_, _ = rand.Read(buf[:]) // crypto/rand.Read only fails if the OS entropy source is gone entirely — a zero suffix is still a valid, merely less unique, temporary name
	return filepath.Join(filepath.Dir(path), ".breakthrough-rename-"+hex.EncodeToString(buf[:])+"-"+filepath.Base(path))
}

// Apply performs every one of changes with a plain os.Rename, in the
// order Order works out for them (see its own doc comment) — always
// within the same directory (see Plan), so there's no cross-device
// EXDEV case to fall back on the way a Move would need to handle.
//
// Keeps going past one file's failure rather than stopping the whole
// batch — the same shape replace.Apply already uses for the same
// reason: one locked or permission-denied file shouldn't block
// renaming the rest. A failed link of a chain naturally fails its
// dependents too (their destination is still occupied), each reported
// as its own step failing rather than silently skipped. Returns every
// physical step that actually succeeded (in the order it happened) —
// both as a count for the caller to report, and as exactly what Undo
// needs to reverse — and the first error encountered, if any.
func Apply(changes []Change) (applied []Change, err error) {
	var firstErr error
	for _, c := range Order(changes) {
		if renameErr := os.Rename(c.From, c.To); renameErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("renaming %s: %w", filepath.Base(c.From), renameErr)
			}
			continue
		}
		applied = append(applied, c)
	}
	return applied, firstErr
}

// Undo reverses exactly the steps Apply reported as applied, last one
// first — the usual undo-stack order, and the only order that still
// makes sense for a chain or a temp-staged cycle: each step's
// destination is only free again once every step that came after it
// has been reversed. Also keeps going past one failure, for the same
// reason Apply does; a path moved, deleted, or replaced by something
// outside this application since the rename simply fails at that one
// step rather than aborting the rest of the undo.
func Undo(applied []Change) (undone []Change, err error) {
	var firstErr error
	for i := len(applied) - 1; i >= 0; i-- {
		c := applied[i]
		if renameErr := os.Rename(c.To, c.From); renameErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("restoring %s: %w", filepath.Base(c.To), renameErr)
			}
			continue
		}
		undone = append(undone, c)
	}
	return undone, firstErr
}
