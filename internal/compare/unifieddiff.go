package compare

import (
	"regexp"
	"strconv"
	"strings"
)

// FileDiff is one file's own side-by-side rows within a (possibly
// multi-file) unified diff — e.g. one file out of several a commit or
// stash entry touched.
type FileDiff struct {
	Path  string
	Pairs []LinePair
}

// unifiedDiffFileHeader matches a "diff --git a/X b/Y" line — Y (the
// post-change path) is what gets shown, the same choice gitbreach.go's
// own gitBreachDiffHeaderPath already makes for the identical line, for
// the identical reason (a rename's own "a/X" is rarely what the user
// wants to read as "the file", and a plain modification has X == Y
// anyway).
var unifiedDiffFileHeader = regexp.MustCompile(`^diff --git a/.* b/(.*)$`)

// unifiedDiffHunkHeader matches a "@@ -a,b +c,d @@" hunk marker —
// everything after the closing "@@" (a function name git sometimes
// appends for context) is deliberately not part of the match, so a
// hunk header containing its own literal "@@" substring in that
// trailing context doesn't confuse this. The counts (b, d) aren't
// captured at all: a hunk whose context line count was trimmed by a
// narrower/wider -U value than this diff's own hunk markers expect
// still parses correctly from the starting line numbers (a, c) alone,
// since every following content line is classified by its own leading
// "+"/"-"/" " character, not counted against a fixed total.
var unifiedDiffHunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// ParseUnifiedDiff splits diff — a unified diff exactly as git diff/git
// show already produce it, possibly spanning several files the way a
// commit or stash entry's own combined diff does — into one FileDiff
// per file, each with its own side-by-side LinePairs built from that
// file's own hunks.
//
// Deliberately built by parsing this text directly rather than
// recomputing the diff from two full file contents (see
// internal/compare's own doc comment, and SideBySide for the version
// that does read two full contents): the caller already has this exact
// text on hand (it's what the existing unified-diff pager already
// shows), re-deriving two full "before"/"after" file bodies for every
// file in a commit would mean extra git calls per file, and every
// shape of diff this needs to handle — unstaged, staged, a commit, a
// stash entry, a rename, a brand-new or now-deleted file — already
// comes out as this same unified-diff text, with no further git
// plumbing to get right case by case.
//
// A hunk boundary within the same file becomes a PairGap row (the
// unchanged lines a narrower diff context already left out, same as
// any ordinary unified diff's own gap between hunks) — never
// reconstructed from a full file read, so it stays a visible gap
// rather than a seamless, gap-free view the way SideBySide's own
// result always is.
func ParseUnifiedDiff(diff string) []FileDiff {
	var files []FileDiff
	var cur *FileDiff
	var leftNo, rightNo int
	var hunkLines []opLine
	inHunk := false

	flushHunk := func() {
		if cur == nil || len(hunkLines) == 0 {
			return
		}
		if len(cur.Pairs) > 0 {
			cur.Pairs = append(cur.Pairs, LinePair{Kind: PairGap})
		}
		cur.Pairs = append(cur.Pairs, pairUpLines(hunkLines, &leftNo, &rightNo)...)
		hunkLines = nil
	}

	for _, line := range strings.Split(diff, "\n") {
		if m := unifiedDiffFileHeader.FindStringSubmatch(line); m != nil {
			flushHunk()
			inHunk = false
			files = append(files, FileDiff{Path: m[1]})
			cur = &files[len(files)-1]
			continue
		}
		if m := unifiedDiffHunkHeader.FindStringSubmatch(line); m != nil {
			flushHunk()
			if cur == nil {
				// No "diff --git" header seen at all — not real git
				// output (see this function's own doc comment), but
				// handled anyway rather than dropping the hunk.
				files = append(files, FileDiff{})
				cur = &files[len(files)-1]
			}
			leftNo, _ = strconv.Atoi(m[1])
			rightNo, _ = strconv.Atoi(m[2])
			leftNo--
			rightNo--
			inHunk = true
			continue
		}
		if !inHunk {
			continue // "index ...", "--- a/X", "+++ b/X", "similarity index ...", ...
		}
		switch {
		case strings.HasPrefix(line, "-"):
			hunkLines = append(hunkLines, opLine{kind: opDelete, text: line[1:]})
		case strings.HasPrefix(line, "+"):
			hunkLines = append(hunkLines, opLine{kind: opInsert, text: line[1:]})
		case strings.HasPrefix(line, " "):
			hunkLines = append(hunkLines, opLine{kind: opEqual, text: line[1:]})
			// Anything else — "\ No newline at end of file" chief among
			// it — isn't a content line and is silently skipped.
		}
	}
	flushHunk()
	return files
}
