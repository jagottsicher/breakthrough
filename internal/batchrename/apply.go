package batchrename

import (
	"fmt"
	"os"
	"path/filepath"
)

// Apply performs every one of changes with a plain os.Rename — always
// within the same directory (see Plan), so there's no cross-device
// EXDEV case to fall back on the way a Move would need to handle.
//
// Keeps going past one file's failure rather than stopping the whole
// batch — the same shape replace.Apply already uses for the same
// reason: one locked or permission-denied file shouldn't block
// renaming the rest, and Plan has already ruled out any collision
// between the renames themselves, so they're independent of each
// other. Returns every change that actually succeeded (in the order it
// happened) — both as a count for the caller to report, and as exactly
// what Undo needs to reverse — and the first error encountered, if any.
func Apply(changes []Change) (applied []Change, err error) {
	var firstErr error
	for _, c := range changes {
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

// Undo reverses exactly the changes Apply reported as applied, last
// one first — the usual undo-stack order, and the only order that
// still makes sense if two renames' paths ever brushed past each other
// along the way. Also keeps going past one failure, for the same
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
