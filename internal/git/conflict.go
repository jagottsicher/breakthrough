package git

import (
	"os"
	"path/filepath"
	"strings"
)

// Operation identifies which multi-step git process (if any) currently
// has the working tree mid-flight — the family BisectInProgress's own
// doc comment already flags as out of scope for that one file ("rebase
// holds with a detached HEAD and needs Continue/Abort/Skip, a bisect
// never does"). Every detection below is a plain file/directory stat
// under .git, the same deliberately cheap approach BisectInProgress
// already uses — none of these states persist anywhere but on disk, so
// there is nothing else to ask.
type Operation int

const (
	OperationNone Operation = iota
	OperationMerge
	OperationRebase
	OperationCherryPick
	OperationRevert
)

// String names Operation the way the dashboard's own status/hint text
// wants it: lower case, matching the git subcommand that drives it.
func (op Operation) String() string {
	switch op {
	case OperationMerge:
		return "merge"
	case OperationRebase:
		return "rebase"
	case OperationCherryPick:
		return "cherry-pick"
	case OperationRevert:
		return "revert"
	default:
		return "none"
	}
}

// InProgressOperation reports which operation (if any) left root's own
// working tree mid-flight. Checked in this fixed order because a rebase
// started as "git rebase" can internally replay commits via the same
// sequencer cherry-pick itself uses — CHERRY_PICK_HEAD can in principle
// coexist with rebase-merge/rebase-apply, and when it does, "rebase" is
// the operation actually driving the show (Continue/Abort/Skip need to
// go through `git rebase`, not `git cherry-pick`, or the sequencer's own
// state goes out of sync with what the user asked for). REVERT_HEAD and
// MERGE_HEAD are mutually exclusive with everything else in practice —
// git itself refuses to start either while another is active — so their
// relative order below doesn't matter.
func InProgressOperation(root string) Operation {
	gitDir := filepath.Join(root, ".git")
	switch {
	case dirExists(filepath.Join(gitDir, "rebase-merge")), dirExists(filepath.Join(gitDir, "rebase-apply")):
		return OperationRebase
	case fileExists(filepath.Join(gitDir, "CHERRY_PICK_HEAD")):
		return OperationCherryPick
	case fileExists(filepath.Join(gitDir, "REVERT_HEAD")):
		return OperationRevert
	case fileExists(filepath.Join(gitDir, "MERGE_HEAD")):
		return OperationMerge
	default:
		return OperationNone
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ConflictSide is which half of a ConflictSection a resolution picks.
type ConflictSide int

const (
	SideOurs ConflictSide = iota
	SideTheirs
)

// ConflictSection is one `<<<<<<<`/`=======`/`>>>>>>>` span of a
// conflicted file, found in document order by ParseConflicts — git
// itself never nests these within one file, so a flat slice (as opposed
// to a tree) is the complete, correct representation. OursLabel/
// TheirsLabel carry whatever git wrote after the marker itself (usually
// a ref name or "HEAD"/"MERGE_HEAD" vs. the merged-in side) verbatim,
// so the dashboard can show the user which side is which without
// guessing — a plain "ours"/"theirs" label would be actively wrong
// during a rebase, where git's own "ours"/"theirs" meaning is reversed
// relative to a merge (see git-merge(1)'s own "NOTES ON HOW CONFLICTS
// ARE PRESENTED").
type ConflictSection struct {
	// Start/End are the zero-based line indices (within the file's own
	// line slice) of the `<<<<<<<` and `>>>>>>>` marker lines
	// themselves, inclusive — needed so Resolve can splice the original
	// file back together without having to re-find the markers.
	Start, End int

	OursLabel, TheirsLabel string
	Ours, Theirs           []string
}

// ParseConflicts finds every conflict section in content (a conflicted
// file's full, unmodified text, straight off disk — see
// UntrackedContent for the equivalent read for an untracked path). A
// line is only ever recognized as a marker at the exact start of a
// line and only in the fixed `<<<<<<<` → `=======` → `>>>>>>>` order —
// real conflict markers are always exactly seven repeated characters
// followed by either nothing or a single space and a label, per
// git's own `merge.conflictStyle` output; a shorter run (e.g. a
// Markdown "<<<<<<<" quoted in prose) is deliberately not matched,
// since git itself would never have produced it.
func ParseConflicts(content string) []ConflictSection {
	lines := strings.Split(content, "\n")
	var sections []ConflictSection

	i := 0
	for i < len(lines) {
		if !isConflictMarker(lines[i], "<<<<<<<") {
			i++
			continue
		}
		sec := ConflictSection{Start: i, OursLabel: conflictMarkerLabel(lines[i], "<<<<<<<")}
		i++
		for i < len(lines) && !isConflictMarker(lines[i], "=======") {
			sec.Ours = append(sec.Ours, lines[i])
			i++
		}
		if i >= len(lines) {
			// Truncated/malformed conflict (no closing marker at all) —
			// nothing sane to resolve, drop the partial section rather
			// than reporting a section the caller could act on.
			break
		}
		i++ // past "======="
		for i < len(lines) && !isConflictMarker(lines[i], ">>>>>>>") {
			sec.Theirs = append(sec.Theirs, lines[i])
			i++
		}
		if i >= len(lines) {
			break
		}
		sec.TheirsLabel = conflictMarkerLabel(lines[i], ">>>>>>>")
		sec.End = i
		i++
		sections = append(sections, sec)
	}
	return sections
}

func isConflictMarker(line, marker string) bool {
	if !strings.HasPrefix(line, marker) {
		return false
	}
	rest := line[len(marker):]
	return rest == "" || strings.HasPrefix(rest, " ")
}

func conflictMarkerLabel(line, marker string) string {
	return strings.TrimPrefix(strings.TrimPrefix(line, marker), " ")
}

// ResolveConflict rewrites content by replacing exactly the section at
// sections[index] with that section's own Ours or Theirs lines
// (per side), leaving every other line — including every other
// conflict section still unresolved — untouched. Index-based rather
// than content-based matching deliberately: two sections can have
// byte-identical Ours/Theirs text (e.g. the same one-line change
// conflicting in two different hunks of a generated file), so only a
// position in the original ParseConflicts result can unambiguously
// pick one.
func ResolveConflict(content string, sections []ConflictSection, index int, side ConflictSide) (string, error) {
	if index < 0 || index >= len(sections) {
		return "", &ConflictIndexError{Index: index, Count: len(sections)}
	}
	sec := sections[index]
	lines := strings.Split(content, "\n")

	resolved := sec.Ours
	if side == SideTheirs {
		resolved = sec.Theirs
	}

	out := make([]string, 0, len(lines))
	out = append(out, lines[:sec.Start]...)
	out = append(out, resolved...)
	out = append(out, lines[sec.End+1:]...)
	return strings.Join(out, "\n"), nil
}

// ConflictIndexError reports an out-of-range section index passed to
// ResolveConflict — kept as its own type rather than fmt.Errorf so a
// caller (the UI layer, after e.g. a stale section list following a
// reload) can tell this apart from a real git failure if it ever needs
// to.
type ConflictIndexError struct {
	Index, Count int
}

func (e *ConflictIndexError) Error() string {
	return "conflict section index out of range"
}
