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

// TestGitBreachDiffSplitScrollbarMovesBothPanes pins the split view's
// own scrollbar: clicking its down arrow must advance both panes by
// the same amount, through the real dispatch chain (gitBreachLayout's
// own MouseHandler, not gitBreachDiffSplitScrollbar.captureMouse
// directly) — the same "don't trust a direct call, the bug lives in
// dispatch" lesson TestGitBreachCommitsScrollbarMouseClickThroughRealDispatch
// already pins for the catalog tables.
func TestGitBreachDiffSplitScrollbarMovesBothPanes(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	var original, changed strings.Builder
	for i := 0; i < 60; i++ {
		original.WriteString("line\n")
		changed.WriteString("CHANGED\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(original.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(changed.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	r := openGitBreachFilesDiff(t, dir, "a.txt")
	r.app.SetFocus(r.gitBreachDiffView)
	rawDiff, err := git.Diff(context.Background(), dir, "a.txt", false)
	if err != nil {
		t.Fatalf("git.Diff: %v", err)
	}
	r.gitBreachSetDiffText("test", gitBreachColorizeDiff(rawDiff, "a.txt", r.theme), rawDiff)
	r.toggleGitBreachDiffSplit()

	r.SetRect(0, 0, 100, 20)
	r.gitBreachLayout.SetRect(0, 0, 100, 20)
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(100, 20)
	r.gitBreachLayout.Draw(screen)

	sb := r.gitBreachDiffSplitScrollbar
	if sb.maxOffset() <= 0 {
		t.Fatalf("maxOffset = %d, want real overflow for 60 changed lines on a 20-row screen", sb.maxOffset())
	}
	sbx, sby, _, sbh := sb.GetRect()

	leftBefore, _ := r.gitBreachDiffSplitLeft.GetScrollOffset()
	rightBefore, _ := r.gitBreachDiffSplitRight.GetScrollOffset()

	setFocus := func(p tview.Primitive) { r.app.SetFocus(p) }
	handler := r.gitBreachLayout.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.Button1, 0), setFocus)
	sb.stopRepeat()
	handler(tview.MouseLeftUp, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.ButtonNone, 0), setFocus)

	leftAfter, _ := r.gitBreachDiffSplitLeft.GetScrollOffset()
	rightAfter, _ := r.gitBreachDiffSplitRight.GetScrollOffset()
	if leftAfter != leftBefore+1 {
		t.Errorf("left pane offset = %d, want %d (before=%d)", leftAfter, leftBefore+1, leftBefore)
	}
	if rightAfter != rightBefore+1 {
		t.Errorf("right pane offset = %d, want %d (before=%d)", rightAfter, rightBefore+1, rightBefore)
	}
	if leftAfter != rightAfter {
		t.Errorf("panes drifted apart: left=%d right=%d, want them to always match", leftAfter, rightAfter)
	}
}

// TestGitBreachDiffUnifiedScrollbarMouseClickThroughRealDispatch is the
// unified pager's own counterpart to
// TestGitBreachDiffSplitScrollbarMovesBothPanes — a real, user-reported
// regression: the Main box's own header (gitBreachDiffHeader)
// swallowed every MouseLeftDown anywhere in the whole box, header or
// not, the same bug class gitBreachFocus{Files,Branches,Commits,Stash}OnClick
// already had (see gitBreachDiffHeader's own SetMouseCapture doc
// comment). Affected both the unified and split pages identically,
// since the header sits above gitBreachDiffPages itself, a shared
// ancestor for both.
func TestGitBreachDiffUnifiedScrollbarMouseClickThroughRealDispatch(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	var original, changed strings.Builder
	for i := 0; i < 60; i++ {
		original.WriteString("line\n")
		changed.WriteString("CHANGED\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(original.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(changed.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	r := openGitBreachFilesDiff(t, dir, "a.txt")
	r.app.SetFocus(r.gitBreachDiffView)
	rawDiff, err := git.Diff(context.Background(), dir, "a.txt", false)
	if err != nil {
		t.Fatalf("git.Diff: %v", err)
	}
	r.gitBreachSetDiffText("test", gitBreachColorizeDiff(rawDiff, "a.txt", r.theme), rawDiff)

	r.SetRect(0, 0, 100, 20)
	r.gitBreachLayout.SetRect(0, 0, 100, 20)
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(100, 20)
	r.gitBreachLayout.Draw(screen)

	sb := r.gitBreachDiffScrollbar
	if sb.maxOffset() <= 0 {
		t.Fatalf("maxOffset = %d, want real overflow for 60 changed lines on a 20-row screen", sb.maxOffset())
	}
	sbx, sby, _, sbh := sb.GetRect()
	before, _ := r.gitBreachDiffView.GetScrollOffset()

	setFocus := func(p tview.Primitive) { r.app.SetFocus(p) }
	handler := r.gitBreachLayout.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.Button1, 0), setFocus)
	sb.stopRepeat()
	handler(tview.MouseLeftUp, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.ButtonNone, 0), setFocus)

	after, _ := r.gitBreachDiffView.GetScrollOffset()
	if after != before+1 {
		t.Errorf("gitBreachDiffView offset after clicking the down arrow = %d, want %d (before=%d)", after, before+1, before)
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

// TestGitBreachDiffSplitLeftDrawReRendersTheHeader is the real bug the
// user reported: "Page 1/20" sticking around regardless of the actual
// commit's own size, never advancing while scrolling. gitBreachDiffView's
// own SetDrawFunc (gitbreachscreen.go) only fires while the unified
// page is the one tview.Pages is actually drawing — never while split
// mode's own pane is the visible one — so without gitBreachDiffSplitLeft
// getting the identical SetDrawFunc, the header froze at whatever it
// last showed before switching into split (here: the short diff's own
// "Page 1/1") and never updated to the longer one actually on screen.
func TestGitBreachDiffSplitLeftDrawReRendersTheHeader(t *testing.T) {
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

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(80, 4)
	r.gitBreachDiffHeader.SetRect(0, 0, 80, 1)
	r.gitBreachDiffSplitLeft.SetRect(0, 1, 39, 3)

	// A much longer file than the short "Page 1/1" diff this already
	// showed before toggling into split — set directly, the same
	// "bypass the async fetch, set what it would have produced" shape
	// this suite's own other tests already establish.
	var long strings.Builder
	for i := 0; i < 50; i++ {
		long.WriteString("line\n")
	}
	r.gitBreachDiffSplitLeft.SetText(long.String())
	r.gitBreachDiffSplitRight.SetText(long.String())

	r.gitBreachDiffSplitLeft.Draw(screen)

	// 50 lines over a 2-row body (height 3, minus nothing borrowed here)
	// is 17 pages — the exact count, not just "not stale", so a
	// substring check can't be fooled by "Page 1/1" being a prefix of
	// the correct "Page 1/17" the way an earlier version of this
	// assertion was.
	header := r.gitBreachDiffHeader.GetText(true)
	if !strings.Contains(header, "Page 1/17 ") {
		t.Errorf("header = %q, want \"Page 1/17\" for 50 lines over a 3-row body — the stale pre-toggle count never updated if this fails", header)
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
