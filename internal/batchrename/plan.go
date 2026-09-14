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
//     batch that would land on the identical name, or with something
//     already sitting on disk under that name that is *not* itself
//     about to be renamed away by this very batch.
//
// That last distinction is what makes a rename *chain* work: "a" is to
// become "b" while "b" is itself to become "c". "b" exists on disk
// right now, but only until its own rename runs, so a's rename isn't
// really an overwrite — Apply orders the two so b moves first (see
// Order), and this Plan reports both as ordinary Changes. Total
// Commander's own Multi-Rename Tool refuses exactly this case as a
// collision; here it just works. A chain whose occupant turns out not
// to be moving after all (its own rename is a Problem, or its name is
// unchanged) is the real overwrite it looks like, and reported as one —
// see the settling loop below.
//
// A name that changes only in letter case ("readme.txt" to
// "README.txt") on a case-insensitive filesystem (macOS's default APFS,
// Windows) stats as "already exists" — because it *is* the very same
// file. That's recognized (os.SameFile) and allowed through as an
// ordinary Change rather than a collision: os.Rename handles the case
// change fine on every such filesystem this app targets.
func Plan(paths []string, rules Rules) PlanResult {
	var result PlanResult
	var candidates []Change
	claimedBy := map[string]string{} // new full path -> the original path that wants it

	for i, p := range paths {
		dir := filepath.Dir(p)
		name := filepath.Base(p)

		info, statErr := os.Lstat(p)
		isDir := statErr == nil && info.IsDir()

		newName, err := Rename(rules, Input{Name: name, IsDir: isDir, Index: i})
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
		claimedBy[newPath] = p
		candidates = append(candidates, Change{From: p, To: newPath})
	}

	changes, problems := settleCollisions(candidates)
	result.Changes = changes
	result.Problems = append(result.Problems, problems...)
	return result
}

// settleCollisions is Plan's own on-disk collision check, on
// candidates whose destinations are already distinct from each
// other's (see Plan's claimedBy). A candidate whose destination is
// occupied by another candidate's *source* is a chain — fine, as long
// as that other candidate itself survives; one whose destination is
// occupied by anything else is a real overwrite, refused. Dropping one
// candidate can turn a chain that depended on it into an overwrite, so
// this loops until nothing more changes — bounded by the number of
// candidates, since every pass either drops at least one or stops.
//
// The case-only rename on a case-insensitive filesystem (see Plan's
// own doc comment) is recognized here too: a destination that stats
// as the very same file as its own source isn't occupied by anything
// else at all.
func settleCollisions(candidates []Change) (kept []Change, problems []Problem) {
	moving := make(map[string]bool, len(candidates)) // source paths still being renamed away
	for _, c := range candidates {
		moving[c.From] = true
	}
	for {
		dropped := false
		kept = candidates[:0]
		for _, c := range candidates {
			toInfo, statErr := os.Lstat(c.To)
			switch {
			case statErr != nil:
				// nothing there — free to take
			case sameFile(c.From, toInfo):
				// case-only rename on a case-insensitive filesystem
			case moving[c.To]:
				// occupied by a batch member that's moving away first
			default:
				problems = append(problems, Problem{
					Path:   c.From,
					Reason: fmt.Sprintf("would overwrite the existing %s", filepath.Base(c.To)),
				})
				delete(moving, c.From)
				dropped = true
				continue
			}
			kept = append(kept, c)
		}
		candidates = kept
		if !dropped {
			return kept, problems
		}
	}
}

// sameFile reports whether path stats as the very same file toInfo
// describes — os.SameFile on two Lstat results, with an unreadable
// path counting as "no".
func sameFile(path string, toInfo os.FileInfo) bool {
	fromInfo, err := os.Lstat(path)
	return err == nil && os.SameFile(fromInfo, toInfo)
}
