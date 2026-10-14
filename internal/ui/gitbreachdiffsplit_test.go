package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/git"
)

// openGitBreachFilesDiff opens Git breach on dir, focuses Files, and
// moves the cursor to path's own row — the common setup every test in
// this file needs before it can reach Main at all.
func openGitBreachFilesDiff(t *testing.T, dir, path string) *Root {
	t.Helper()
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachFilesTable)
	row := gitBreachTableRowForPath(r, path)
	if row < 0 {
		t.Fatalf("path %q not found in Files table", path)
	}
	r.gitBreachFilesTable.Select(row, 0)
	r.startGitBreachFilesDiffIfOwner()
	return r
}

func TestToggleGitBreachDiffSplitSwitchesViewAndFocus(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := openGitBreachFilesDiff(t, dir, "a.txt")
	r.app.SetFocus(r.gitBreachDiffView)

	// startGitBreachFilesDiffIfOwner's own fetch runs on a background
	// goroutine, landing via QueueUpdateDraw — which blocks forever
	// without a real Application.Run() loop draining it (verified
	// directly against tview's own application.go, not guessed), so
	// there's no real fetch to wait for here. gitBreachSetDiffText is
	// called directly instead, with the exact text a real fetch would
	// have produced, the same "bypass the goroutine, set what it would
	// have produced" shape this suite's own computeCompareHash tests
	// already establish for the identical reason.
	rawDiff, err := git.Diff(context.Background(), dir, "a.txt", false)
	if err != nil {
		t.Fatalf("git.Diff: %v", err)
	}
	r.gitBreachSetDiffText("test", gitBreachColorizeDiff(rawDiff, "a.txt", r.theme), rawDiff)

	r.toggleGitBreachDiffSplit()
	if !r.gitBreachDiffSplit {
		t.Error("gitBreachDiffSplit = false after toggling, want true")
	}
	if !r.gitBreachDiffSplitLeft.HasFocus() {
		t.Error("focus did not move to gitBreachDiffSplitLeft after toggling into split mode")
	}
	left := r.gitBreachDiffSplitLeft.GetText(true)
	right := r.gitBreachDiffSplitRight.GetText(true)
	if !strings.Contains(left, "two") {
		t.Errorf("split left pane = %q, missing %q", left, "two")
	}
	if !strings.Contains(right, "TWO") {
		t.Errorf("split right pane = %q, missing %q", right, "TWO")
	}

	r.toggleGitBreachDiffSplit()
	if r.gitBreachDiffSplit {
		t.Error("gitBreachDiffSplit = true after toggling back, want false")
	}
	if !r.gitBreachDiffView.HasFocus() {
		t.Error("focus did not move back to gitBreachDiffView after toggling out of split mode")
	}
}

func TestCaptureGitBreachMainKeyVEntersSplitMode(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("ONE\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := openGitBreachFilesDiff(t, dir, "a.txt")
	r.app.SetFocus(r.gitBreachDiffView)

	if got := r.captureGitBreachMainKey(tcell.NewEventKey(tcell.KeyRune, 'v', tcell.ModNone)); got != nil {
		t.Errorf("captureGitBreachMainKey('v') = %v, want nil (consumed)", got)
	}
	if !r.gitBreachDiffSplit {
		t.Error("'v' from the unified view did not enter split mode")
	}
}

func TestCaptureGitBreachDiffSplitKeyLeftAtColumnZeroLeavesMain(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("ONE\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := openGitBreachFilesDiff(t, dir, "a.txt")
	r.app.SetFocus(r.gitBreachDiffView)
	r.toggleGitBreachDiffSplit()
	r.app.SetFocus(r.gitBreachDiffSplitLeft)

	// At column 0 (ScrollTo(0, 0) is openGitBreachFilesDiff's own
	// implicit starting state): Left leaves back to Files, the same
	// action Escape/Enter/Space already reach.
	if got := r.captureGitBreachDiffSplitKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)); got != nil {
		t.Errorf("captureGitBreachDiffSplitKey(Left) at column 0 = %v, want nil (consumed)", got)
	}
	if !r.gitBreachFilesTable.HasFocus() {
		t.Error("Left at column 0 did not return focus to Files")
	}
}

func TestCaptureGitBreachDiffSplitKeyLeftScrollsWhenNotAtColumnZero(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("ONE\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := openGitBreachFilesDiff(t, dir, "a.txt")
	r.app.SetFocus(r.gitBreachDiffView)
	r.toggleGitBreachDiffSplit()
	r.app.SetFocus(r.gitBreachDiffSplitLeft)
	r.gitBreachDiffSplitLeft.ScrollTo(0, 5)

	if got := r.captureGitBreachDiffSplitKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)); got != nil {
		t.Errorf("captureGitBreachDiffSplitKey(Left) at column 5 = %v, want nil (consumed)", got)
	}
	if !r.gitBreachDiffSplitLeft.HasFocus() {
		t.Error("Left while not at column 0 should scroll, not leave Main")
	}
	_, col := r.gitBreachDiffSplitLeft.GetScrollOffset()
	if col != 4 {
		t.Errorf("left pane column = %d, want 4 (scrolled left by one)", col)
	}
}

func TestScrollGitBreachDiffSplitAlwaysMirrorsBothPanes(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := openGitBreachFilesDiff(t, dir, "a.txt")
	r.app.SetFocus(r.gitBreachDiffView)
	r.toggleGitBreachDiffSplit()

	r.scrollGitBreachDiffSplit(2, 3)
	leftRow, leftCol := r.gitBreachDiffSplitLeft.GetScrollOffset()
	rightRow, rightCol := r.gitBreachDiffSplitRight.GetScrollOffset()
	if leftRow != rightRow || leftCol != rightCol {
		t.Errorf("left=%d/%d right=%d/%d, want identical scroll offsets (no unlinked mode here)", leftRow, leftCol, rightRow, rightCol)
	}

	// The mouse wheel on either pane must reach the exact same
	// function — simulated directly here (MouseScrollDown on the
	// right pane), covering the real bug this screen's own wheel
	// handling exists to avoid (see comparediff.go's own identical
	// fix): scrolling only the pane under the cursor even though
	// there is no unlinked mode to opt into here at all.
	_, _ = r.captureGitBreachDiffSplitRightMouse(tview.MouseScrollDown, nil)
	leftRow2, _ := r.gitBreachDiffSplitLeft.GetScrollOffset()
	rightRow2, _ := r.gitBreachDiffSplitRight.GetScrollOffset()
	if leftRow2 != rightRow2 {
		t.Errorf("after wheel on the right pane: left row=%d right row=%d, want identical", leftRow2, rightRow2)
	}
	if leftRow2 != leftRow+1 {
		t.Errorf("left row after wheel-down = %d, want %d", leftRow2, leftRow+1)
	}
}

func TestGitBreachDiffSplitRightMouseLeftDownRedirectsFocusToLeft(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("ONE\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := openGitBreachFilesDiff(t, dir, "a.txt")
	r.app.SetFocus(r.gitBreachDiffView)
	r.toggleGitBreachDiffSplit()

	_, event := r.captureGitBreachDiffSplitRightMouse(tview.MouseLeftDown, nil)
	if event != nil {
		t.Errorf("captureGitBreachDiffSplitRightMouse(MouseLeftDown) event = %v, want nil (consumed)", event)
	}
	if !r.gitBreachDiffSplitLeft.HasFocus() {
		t.Error("a click on the right pane must redirect focus to the left pane, never hold it itself")
	}
}

// TestRenderGitBreachDiffHeaderUsesTheLongerSplitPane pins down a
// defensive guarantee, not a currently-reachable bug: the two panes
// are supposed to always have exactly the same row count (see
// gitBreachRenderDiffSplit's own doc comment), but "Page X/Y" picks
// whichever is actually longer regardless, in case that invariant
// ever quietly breaks (a future rendering change, an edge case missed
// there) — simulated directly here by setting mismatched content on
// the two panes, something gitBreachRenderDiffSplit itself would never
// actually do.
func TestRenderGitBreachDiffHeaderUsesTheLongerSplitPane(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("ONE\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := openGitBreachFilesDiff(t, dir, "a.txt")
	r.app.SetFocus(r.gitBreachDiffView)
	r.toggleGitBreachDiffSplit()

	r.gitBreachDiffSplitLeft.SetText("one\ntwo")
	r.gitBreachDiffSplitRight.SetText("one\ntwo\nthree\nfour\nfive\nsix")

	r.renderGitBreachDiffHeader(60, 2)
	header := r.gitBreachDiffHeader.GetText(true)
	if !strings.Contains(header, "Page 1/3") {
		t.Errorf("header = %q, want it to report 3 pages (6 lines / 2 rows), the longer (right) pane's own count, not the shorter left one", header)
	}
}
