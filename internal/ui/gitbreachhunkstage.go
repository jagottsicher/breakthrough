package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/jagottsicher/breakthrough/internal/git"
)

// gitBreachHunkRanges returns rawDiff's own hunk boundaries — the
// line index of each "@@ ... @@" header line, plus one past the
// final line, so consecutive pairs directly bound each hunk's own
// span [starts[i], starts[i+1]), header line included.
func gitBreachHunkRanges(lines []string) []int {
	var starts []int
	for i, l := range lines {
		if strings.HasPrefix(l, "@@") {
			starts = append(starts, i)
		}
	}
	return append(starts, len(lines))
}

// gitBreachHunkAt picks which hunk "contains" lineIndex — a line
// index before the first hunk (Main scrolled to the top, still
// showing the file header) picks hunk 0 rather than reporting
// nothing: "stage the hunk I'm looking at" reads the same way
// whether the cursor rests exactly on a hunk's own "@@" line or a
// header line just above it. Reports false only when rawDiff has no
// "@@" line at all (an empty, placeholder, or error message Main
// sometimes shows isn't a real hunked diff).
func gitBreachHunkAt(lines []string, lineIndex int) (hunkIndex int, ok bool) {
	starts := gitBreachHunkRanges(lines)
	if len(starts) <= 1 {
		return 0, false
	}
	for i := 0; i < len(starts)-1; i++ {
		if lineIndex < starts[i+1] {
			return i, true
		}
	}
	return len(starts) - 2, true // past the last hunk's own end — still the last hunk
}

// gitBreachHunkPatch builds a complete, directly `git apply`-able
// patch for exactly one hunk of rawDiff: every header line before
// the first hunk ("diff --git"/"index"/"---"/"+++"), verbatim,
// followed by hunkIndex's own hunk, also verbatim — no line
// filtering, no recomputed hunk counts. A hunk taken whole is always
// a byte-exact, already-valid patch on its own; filtering individual
// lines out of it (an explicitly later, not-yet-built step — see
// feature_ideas.txt's own #21 item 6) would need the hunk's own
// "@@ -a,b +c,d @@" counts recomputed to match, and getting that
// wrong corrupts the index rather than just failing visibly.
// A trailing "\n" is guaranteed even when rawDiff's own last line
// happens not to end in one (an edge rawDiff normally never hits,
// since git diff's own output always does, but strings.Join never
// adds one on its own and `git apply` reports "corrupt patch" on a
// patch that doesn't end in one) — a hunk's own trailing "\ No
// newline at end of file" marker, when present, is just one more
// line already inside the hunk's own span, so it travels with it
// automatically, no special handling needed.
func gitBreachHunkPatch(rawDiff string, hunkIndex int) (string, bool) {
	lines := strings.Split(rawDiff, "\n")
	starts := gitBreachHunkRanges(lines)
	if len(starts) <= 1 || hunkIndex < 0 || hunkIndex >= len(starts)-1 {
		return "", false
	}
	header := strings.Join(lines[:starts[0]], "\n")
	hunk := strings.Join(lines[starts[hunkIndex]:starts[hunkIndex+1]], "\n")
	patch := header + "\n" + hunk
	if !strings.HasSuffix(patch, "\n") {
		patch += "\n"
	}
	return patch, true
}

// gitBreachMainShowsAStageableHunk reports whether Main is currently
// showing something "s" can act on at all — Files' own unstaged diff,
// specifically: a commit/stash diff's own hunks can't be staged
// (nothing to stage them *into*), and a Staged/Untracked/Conflict row
// has no unstaged hunks of its own either. Shared by
// openGitBreachStageHunk (the actual guard) and the hint bar (whether
// to advertise "s" at all), so the two can never disagree about when
// the key does something.
func (r *Root) gitBreachMainShowsAStageableHunk() bool {
	if r.gitBreach.MainOwner != gitBreachMainOwnerFiles {
		return false
	}
	row, _ := r.gitBreach.FilesTable.GetSelection()
	gr, ok := r.gitBreachRowAt(row)
	return ok && gr.kind == gitBreachRowUnstaged
}

// openGitBreachStageHunk is "s" while Main has real keyboard focus —
// stages exactly the hunk currently at the top of Main's own visible
// area (see gitBreachHunkAt), via a patch built from
// gitBreach.DiffRawText (the real diff text gitBreachSetDiffText
// already keeps alongside the colorized version shown on screen —
// see its own doc comment). Only meaningful for Files' own unstaged
// diff: a commit/stash diff's own hunks can't be staged (nothing to
// stage them *into* — those are already-committed history, not the
// working tree), and gitBreachColorizeDiff guarantees one rendered
// line per raw line (pinned by its own tests), so Main's scroll
// offset maps directly onto a raw-line index with no separate
// tracking of "which line is the cursor on" needed.
func (r *Root) openGitBreachStageHunk() {
	if !r.gitBreachMainShowsAStageableHunk() {
		return
	}

	topLine, _ := r.gitBreach.DiffView.GetScrollOffset()
	lines := strings.Split(r.gitBreach.DiffRawText, "\n")
	hunkIndex, ok := gitBreachHunkAt(lines, topLine)
	if !ok {
		return
	}
	patch, ok := gitBreachHunkPatch(r.gitBreach.DiffRawText, hunkIndex)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	if err := git.ApplyPatchCached(ctx, r.gitBreach.Root, patch); err != nil {
		// showTransientErrorOverOverlay, not showError — same
		// reasoning as openGitBreachCreateTag's own doc comment: Git
		// Breach is itself an overlay, and showError's own
		// showOverlay would close it first.
		r.showTransientErrorOverOverlay(fmt.Errorf("git breach: %w", err))
		return
	}
	r.reloadGitBreach()
}
