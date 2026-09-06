package batchrename

import (
	"fmt"
	"os"
	"path/filepath"
)

// Change is one file Plan proposes to rename: its current full path
// and the full path it would become.
type Change struct {
	From, To string
}

// Problem is one input path Plan refuses to turn into a Change, and a
// human-readable reason why — shown next to that row in the live
// preview table rather than silently dropping it (see internal/ui's
// own batch-rename screen).
type Problem struct {
	Path   string
	Reason string
}

// PlanResult is Plan's own report: every rename it's prepared to make,
// plus every input it refused and why — kept as two separate slices
// rather than one combined list of "changed or not", so a caller can
// show a live preview and a conflict count without recomputing either
// one from the other.
type PlanResult struct {
	Changes  []Change
	Problems []Problem
}

// Plan computes what Rules would do to each of paths, in the order
// given — that order is also what the numbering step (see
// applyNumbering) counts against, so callers should pass paths in
// whatever order they're actually showing them (see internal/ui's own
// sorted selection), not an arbitrary one.
//
// A path whose name doesn't change at all under Rules is left out of
// both Changes and Problems — the same "only report what's actually
// happening" convention replace.Preview already follows for content
// changes. Three things turn a path into a Problem instead of a
// Change:
//
//   - Rename itself fails (only possible with an invalid Regex).
//   - The computed name is empty (e.g. Trim removed everything and
//     there's no extension left either) — not a valid filename.
//   - The computed path collides — with another file in this same
//     batch that would land on the identical name, or with anything
//     already sitting on disk under that name.
//
// Deliberately does not attempt to resolve a rename *chain* (a's new
// name is b's old name, while b is itself being renamed elsewhere in
// the same batch) — treated as an on-disk collision like any other.
// Total Commander's own Multi-Rename Tool doesn't resolve these either;
// a real solve (reorder renames, or stage through temporary names)
// is a worthwhile follow-up, not part of this first version.
func Plan(paths []string, rules Rules) PlanResult {
	var result PlanResult
	claimedBy := map[string]string{} // new full path -> the original path that wants it

	for i, p := range paths {
		dir := filepath.Dir(p)
		name := filepath.Base(p)

		newName, err := Rename(rules, name, i)
		switch {
		case err != nil:
			result.Problems = append(result.Problems, Problem{Path: p, Reason: err.Error()})
			continue
		case newName == "":
			result.Problems = append(result.Problems, Problem{Path: p, Reason: "would produce an empty filename"})
			continue
		case newName == name:
			continue // unchanged — nothing to preview or apply
		}

		newPath := filepath.Join(dir, newName)

		if owner, taken := claimedBy[newPath]; taken {
			result.Problems = append(result.Problems, Problem{
				Path:   p,
				Reason: fmt.Sprintf("would collide with the new name planned for %s", filepath.Base(owner)),
			})
			continue
		}
		if _, statErr := os.Lstat(newPath); statErr == nil {
			result.Problems = append(result.Problems, Problem{
				Path:   p,
				Reason: fmt.Sprintf("would overwrite the existing %s", filepath.Base(newPath)),
			})
			continue
		}

		claimedBy[newPath] = p
		result.Changes = append(result.Changes, Change{From: p, To: newPath})
	}

	return result
}
