package git

import "testing"

// glyphString renders a LaneRow's own Cells as a plain ASCII string
// (one rune per column) for easy comparison in tests — '●' for the
// node, '|' for pass, '-' for a horizontal run, and the four corners
// as '7'/'r'/'j'/'L' (fork-right, fork-left, join-right, join-left;
// plain ASCII keeps test failure diffs readable without a terminal
// that renders box-drawing glyphs).
func glyphString(cells []LaneCell) string {
	out := make([]byte, len(cells))
	for i, c := range cells {
		switch c.Glyph {
		case LaneGlyphNone:
			out[i] = ' '
		case LaneGlyphPass:
			out[i] = '|'
		case LaneGlyphNode:
			out[i] = '*'
		case LaneGlyphHorizontal:
			out[i] = '-'
		case LaneGlyphForkRight:
			out[i] = '7'
		case LaneGlyphForkLeft:
			out[i] = 'r'
		case LaneGlyphJoinRight:
			out[i] = 'j'
		case LaneGlyphJoinLeft:
			out[i] = 'L'
		default:
			out[i] = '?'
		}
	}
	return string(out)
}

func rowStrings(t *testing.T, rows []LaneRow) []string {
	t.Helper()
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = glyphString(r.Cells)
	}
	return out
}

func TestCommitLanesOnALinearHistoryStaysInOneColumn(t *testing.T) {
	commits := []LogEntry{
		{Hash: "c3", Parents: []string{"c2"}},
		{Hash: "c2", Parents: []string{"c1"}},
		{Hash: "c1", Parents: nil},
	}
	rows := CommitLanes(commits)
	want := []string{"*", "*", "*"}
	got := rowStrings(t, rows)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d: got %q, want %q", i, got[i], want[i])
		}
		if rows[i].Column != 0 {
			t.Errorf("row %d: got column %d, want 0", i, rows[i].Column)
		}
	}
}

// TestCommitLanesOnASimpleMergeForksAndJoins pins a single-commit
// feature branch merged back into mainline — the merge commit forks a
// second lane for the feature tip, and the two lanes join again at the
// shared base commit, the exact shape most breakthrough release merges
// actually have.
func TestCommitLanesOnASimpleMergeForksAndJoins(t *testing.T) {
	commits := []LogEntry{
		{Hash: "merge", Parents: []string{"mainline", "feature"}},
		{Hash: "mainline", Parents: []string{"base"}},
		{Hash: "feature", Parents: []string{"base"}},
		{Hash: "base", Parents: []string{"root"}},
		{Hash: "root", Parents: nil},
	}
	rows := CommitLanes(commits)
	want := []string{"*7", "*|", "|*", "*j", "*"}
	got := rowStrings(t, rows)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d (%s): got %q, want %q", i, commits[i].Hash, got[i], want[i])
		}
	}
	if rows[0].Column != 0 || rows[2].Column != 1 {
		t.Errorf("merge should sit in column 0 and the feature commit in column 1, got %d and %d", rows[0].Column, rows[2].Column)
	}
}

// TestCommitLanesOnAnOctopusMergeForksEveryExtraParent pins a merge
// commit with three parents — not just two — forking two new lanes in
// the same row, since breakthrough's own real history can contain one.
func TestCommitLanesOnAnOctopusMergeForksEveryExtraParent(t *testing.T) {
	commits := []LogEntry{
		{Hash: "octopus", Parents: []string{"a", "b", "c"}},
		{Hash: "a", Parents: nil},
		{Hash: "b", Parents: nil},
		{Hash: "c", Parents: nil},
	}
	rows := CommitLanes(commits)
	if rows[0].Column != 0 {
		t.Fatalf("octopus merge should sit in column 0, got %d", rows[0].Column)
	}
	if len(rows[0].Cells) != 3 {
		t.Fatalf("octopus merge row should have 3 columns (node + 2 forks), got %d (%q)", len(rows[0].Cells), glyphString(rows[0].Cells))
	}
	if rows[0].Cells[0].Glyph != LaneGlyphNode {
		t.Errorf("column 0 should be the node, got %v", rows[0].Cells[0].Glyph)
	}
	if rows[0].Cells[1].Glyph != LaneGlyphForkRight || rows[0].Cells[2].Glyph != LaneGlyphForkRight {
		t.Errorf("columns 1 and 2 should both be fresh fork-right lanes, got %q", glyphString(rows[0].Cells))
	}
}

// TestCommitLanesClosesALaneWithNoParents pins a root commit — lanes
// pointing at it must close rather than carry a dangling empty hash
// forward, confirmed indirectly here by checking that a later commit
// reusing the same column doesn't see a stale match.
func TestCommitLanesClosesALaneWithNoParents(t *testing.T) {
	commits := []LogEntry{
		{Hash: "merge", Parents: []string{"a", "b"}},
		{Hash: "a", Parents: nil},
		{Hash: "b", Parents: nil},
		{Hash: "unrelated", Parents: nil},
	}
	rows := CommitLanes(commits)
	// "a" (column 0) and "b" (column 1) both close with no parents.
	// "unrelated" has no parent relationship to either and must reuse
	// a freed column (0, the lowest) rather than opening a third.
	if rows[3].Column != 0 {
		t.Errorf("a closed lane's column should be reused by the next unrelated root commit, got column %d", rows[3].Column)
	}
}

// TestCommitLanesWithMultipleRootsKeepsThemInSeparateLanes pins two
// histories with no common ancestor at all within the loaded window
// (e.g. an orphan branch), both still two commits deep at the row
// where they overlap — each keeps its own lane rather than being
// forced to collide while both are genuinely still active.
func TestCommitLanesWithMultipleRootsKeepsThemInSeparateLanes(t *testing.T) {
	commits := []LogEntry{
		{Hash: "a2", Parents: []string{"a1"}},
		{Hash: "b2", Parents: []string{"b1"}},
		{Hash: "a1", Parents: nil},
		{Hash: "b1", Parents: nil},
	}
	rows := CommitLanes(commits)
	if rows[0].Column == rows[1].Column {
		t.Errorf("a2 and b2 share no ancestor and are both still active here, should never land in the same column, both got column %d", rows[0].Column)
	}
}

func TestCommitLanesOnEmptyHistoryReturnsNoRows(t *testing.T) {
	rows := CommitLanes(nil)
	if len(rows) != 0 {
		t.Errorf("got %d rows for no commits, want 0", len(rows))
	}
}

// TestCommitLanesPreservesRowOrderAndCommit pins the "exactly one row
// per LogEntry, same order" invariant internal/ui's own row->index
// mapping and pagination both depend on.
func TestCommitLanesPreservesRowOrderAndCommit(t *testing.T) {
	commits := []LogEntry{
		{Hash: "c2", Subject: "second", Parents: []string{"c1"}},
		{Hash: "c1", Subject: "first", Parents: nil},
	}
	rows := CommitLanes(commits)
	if len(rows) != len(commits) {
		t.Fatalf("got %d rows for %d commits, want one row per commit", len(rows), len(commits))
	}
	for i, c := range commits {
		if rows[i].Commit.Hash != c.Hash {
			t.Errorf("row %d: got commit %q, want %q", i, rows[i].Commit.Hash, c.Hash)
		}
	}
}
