package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/git"
)

// openGitBreachWrapDiff sets Git Breach up on a working-tree diff whose
// lines are far too wide for Main, with two separate hunks far apart —
// wide enough that soft wrap has something to do, and hunked enough
// that staging the wrong one would be visible.
//
// XDG_CONFIG_HOME is redirected first: toggling wrap persists the new
// value immediately (see setGitBreachDiffWrap), and a test has no
// business writing into the real user's config file.
func openGitBreachWrapDiff(t *testing.T, width, height int) *Root {
	t.Helper()
	requireGitForBreach(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir := initGitBreachRepo(t)
	long := strings.Repeat("alpha ", 30)
	var original strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&original, "line %d %s\n", i, long)
	}
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte(original.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	// Two clusters of changes far enough apart to stay two hunks, each
	// big enough that the resulting diff overflows the pager by a wide
	// margin — otherwise tview clamps any scroll offset back to the top
	// and a test about where the view is sitting proves nothing.
	changed := strings.Split(original.String(), "\n")
	for i := 10; i < 30; i++ {
		changed[i] = fmt.Sprintf("FIRST CHANGE %d %s", i, long)
	}
	for i := 150; i < 170; i++ {
		changed[i] = fmt.Sprintf("SECOND CHANGE %d %s", i, long)
	}
	if err := os.WriteFile(path, []byte(strings.Join(changed, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	r := openGitBreachFilesDiff(t, dir, "a.txt")
	r.app.SetFocus(r.gitBreach.DiffView)

	// The real fetch runs on a background goroutine that lands via
	// QueueUpdateDraw, which never drains without a real
	// Application.Run() loop — so the text a real fetch would have
	// produced is set directly, the same shape every other test in this
	// suite uses for the identical reason.
	rawDiff, err := git.Diff(context.Background(), r.gitBreach.Root, "a.txt", false)
	if err != nil {
		t.Fatalf("git.Diff: %v", err)
	}
	r.gitBreachSetDiffText("test", gitBreachColorizeDiff(rawDiff, "a.txt", r.theme), rawDiff)

	r.SetRect(0, 0, width, height)
	r.gitBreach.Layout.SetRect(0, 0, width, height)
	return r
}

func drawGitBreachForWrap(t *testing.T, r *Root, width, height int) {
	t.Helper()
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(width, height)
	r.gitBreach.Layout.Draw(screen)
}

// TestGitBreachDiffWrapRendersThroughTheDrawHook pins that the unified
// pager picks soft wrap up on the next draw, since that is the only
// moment it knows its own width.
func TestGitBreachDiffWrapRendersThroughTheDrawHook(t *testing.T) {
	r := openGitBreachWrapDiff(t, 100, 30)

	drawGitBreachForWrap(t, r, 100, 30)
	unwrapped := strings.Count(r.gitBreach.DiffView.GetText(true), "\n")
	if r.gitBreach.DiffWrapWidth != 0 {
		t.Fatalf("DiffWrapWidth = %d with wrap off, want 0", r.gitBreach.DiffWrapWidth)
	}

	r.toggleGitBreachDiffWrap()
	drawGitBreachForWrap(t, r, 100, 30)

	if r.gitBreach.DiffWrapWidth <= 0 {
		t.Fatalf("DiffWrapWidth = %d after turning wrap on and drawing, want a real width", r.gitBreach.DiffWrapWidth)
	}
	if got := strings.Count(r.gitBreach.DiffView.GetText(true), "\n"); got <= unwrapped {
		t.Errorf("wrapped pager has %d line breaks, unwrapped had %d — want more", got, unwrapped)
	}
	paneWidth := r.gitBreach.DiffPaneWidth
	for i, row := range strings.Split(r.gitBreach.DiffView.GetText(true), "\n") {
		if got := tview.TaggedStringWidth(row); got > paneWidth {
			t.Errorf("row %d is %d columns wide, pager is %d", i, got, paneWidth)
		}
	}
}

// TestGitBreachStageHunkPicksTheSameHunkWithWrapOn is the one that
// matters most: Main's scroll offset used to be a raw diff line number
// outright, and soft wrap breaks that. Staging has to follow the
// content, not the row number, or "s" quietly stages the wrong hunk.
func TestGitBreachStageHunkPicksTheSameHunkWithWrapOn(t *testing.T) {
	r := openGitBreachWrapDiff(t, 100, 30)
	drawGitBreachForWrap(t, r, 100, 30)

	rawLines := strings.Split(r.gitBreach.DiffRawText, "\n")

	// Park the view on the second hunk while unwrapped, and remember
	// which hunk that is.
	secondHunkRow := -1
	hunks := 0
	for i, line := range rawLines {
		if strings.HasPrefix(line, "@@") {
			hunks++
			if hunks == 2 {
				secondHunkRow = i
				break
			}
		}
	}
	if secondHunkRow < 0 {
		t.Fatalf("diff has %d hunks, want at least 2:\n%s", hunks, r.gitBreach.DiffRawText)
	}
	r.gitBreach.DiffView.ScrollTo(secondHunkRow, 0)
	drawGitBreachForWrap(t, r, 100, 30)

	unwrappedRow, _ := r.gitBreach.DiffView.GetScrollOffset()
	if unwrappedRow != secondHunkRow {
		t.Fatalf("view clamped to row %d instead of %d — the fixture's diff is too short to scroll into", unwrappedRow, secondHunkRow)
	}
	wantHunk, ok := gitBreachHunkAt(rawLines, sourceLineAtRow(r.gitBreach.DiffRowToRaw, unwrappedRow))
	if !ok {
		t.Fatalf("no hunk at row %d with wrap off", unwrappedRow)
	}

	r.toggleGitBreachDiffWrap()
	drawGitBreachForWrap(t, r, 100, 30)

	wrappedRow, _ := r.gitBreach.DiffView.GetScrollOffset()
	if wrappedRow == unwrappedRow {
		t.Fatalf("row %d is unchanged after wrapping — the fixture isn't exercising the remap", wrappedRow)
	}
	gotHunk, ok := gitBreachHunkAt(rawLines, sourceLineAtRow(r.gitBreach.DiffRowToRaw, wrappedRow))
	if !ok {
		t.Fatalf("no hunk at row %d with wrap on", wrappedRow)
	}
	if gotHunk != wantHunk {
		t.Errorf("with wrap on the view sits on hunk %d, want the same hunk %d it sat on before", gotHunk, wantHunk)
	}

	// And the real thing, not just the arithmetic: stage from here and
	// check git itself got the second hunk, not the first.
	if !r.gitBreachMainShowsAStageableHunk() {
		t.Fatal("Main doesn't consider this a stageable hunk — the fixture isn't on an unstaged Files row")
	}
	r.openGitBreachStageHunk()

	staged, err := git.Diff(context.Background(), r.gitBreach.Root, "a.txt", true)
	if err != nil {
		t.Fatalf("git.Diff (staged): %v", err)
	}
	if !strings.Contains(staged, "SECOND CHANGE") {
		t.Errorf("staged diff is missing the second hunk's own content:\n%s", staged)
	}
	if strings.Contains(staged, "FIRST CHANGE") {
		t.Errorf("staged diff contains the first hunk too — the wrong hunk was staged:\n%s", staged)
	}
}

// TestGitBreachDiffSplitWrapKeepsBothPanesRowAligned is the
// side-by-side view's own half of the invariant writeDiffPairRows
// exists for.
func TestGitBreachDiffSplitWrapKeepsBothPanesRowAligned(t *testing.T) {
	r := openGitBreachWrapDiff(t, 100, 30)
	r.toggleGitBreachDiffSplit()
	r.toggleGitBreachDiffWrap()
	drawGitBreachForWrap(t, r, 100, 30)

	if r.gitBreach.DiffSplitWrapWidth <= 0 {
		t.Fatalf("DiffSplitWrapWidth = %d after turning wrap on and drawing, want a real width", r.gitBreach.DiffSplitWrapWidth)
	}

	left := strings.Split(strings.TrimSuffix(r.gitBreach.DiffSplitLeft.GetText(true), "\n"), "\n")
	right := strings.Split(strings.TrimSuffix(r.gitBreach.DiffSplitRight.GetText(true), "\n"), "\n")
	if len(left) != len(right) {
		t.Fatalf("left pane has %d rows, right pane %d — they must match", len(left), len(right))
	}

	paneWidth := r.gitBreach.DiffSplitPaneWidth
	for i, row := range left {
		if got := tview.TaggedStringWidth(row); got > paneWidth {
			t.Errorf("left row %d is %d columns wide, pane is %d", i, got, paneWidth)
		}
		if got := tview.TaggedStringWidth(right[i]); got > paneWidth {
			t.Errorf("right row %d is %d columns wide, pane is %d", i, got, paneWidth)
		}
	}
}

// TestGitBreachDiffWrapIsReachableByKeyInBothViews pins that "w" is
// wired up in the unified pager and the side-by-side view alike, and
// that the hint bar advertises it in both — one state, one key, both
// presentations.
func TestGitBreachDiffWrapIsReachableByKeyInBothViews(t *testing.T) {
	r := openGitBreachWrapDiff(t, 100, 30)

	if hint := r.gitBreach.Hint.GetText(true); !strings.Contains(hint, "wrap long lines") {
		t.Errorf("hint bar = %q, want it to advertise wrapping while Main has focus", hint)
	}
	if ev := r.captureGitBreachMainKey(tcell.NewEventKey(tcell.KeyRune, 'w', tcell.ModNone)); ev != nil {
		t.Error("the unified pager passed 'w' through instead of consuming it")
	}
	if !r.settings.GitBreachDiffWrap {
		t.Fatal("'w' in the unified pager did not turn soft wrap on")
	}
	if hint := r.gitBreach.Hint.GetText(true); !strings.Contains(hint, "stop wrapping") {
		t.Errorf("hint bar = %q, want it to offer turning wrapping back off", hint)
	}

	r.toggleGitBreachDiffSplit()
	if hint := r.gitBreach.Hint.GetText(true); !strings.Contains(hint, "stop wrapping") {
		t.Errorf("split-view hint bar = %q, want the same toggle offered there", hint)
	}
	if ev := r.captureGitBreachDiffSplitKey(tcell.NewEventKey(tcell.KeyRune, 'w', tcell.ModNone)); ev != nil {
		t.Error("the split view passed 'w' through instead of consuming it")
	}
	if r.settings.GitBreachDiffWrap {
		t.Error("'w' in the split view did not turn soft wrap back off — the two views must share one state")
	}
}
