package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestGitBreachCommitsScrollbarMouseClickThroughRealDispatch is a real,
// user-reported regression test: clicking the Commits box's own
// scrollbar did nothing, even though the scrollbar itself rendered
// correctly and keyboard scrolling still worked. The cause was
// upstream of the scrollbar entirely — gitBreachFocusCommitsOnClick
// (the Commits header's own mouse capture) consumed every
// MouseLeftDown anywhere in the whole box, header or not, since
// SetMouseCapture's own callback runs for every event the box's Flex
// hands it, not just ones actually on the header's own row (see that
// function's own doc comment). The scrollbar's own captureMouse was
// never reached as a result.
//
// Exercises the real dispatch chain (gitBreachLayout's own
// MouseHandler, as Application.fireMouseActions would actually call
// it), not captureMouse directly — a direct call already worked and
// would not have caught this.
func TestGitBreachCommitsScrollbarMouseClickThroughRealDispatch(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	for i := 0; i < 40; i++ {
		path := filepath.Join(dir, fmt.Sprintf("f%02d.txt", i))
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGitBreach(t, dir, "add", ".")
		runGitBreach(t, dir, "commit", "-q", "-m", fmt.Sprintf("commit %d", i))
	}

	r := newTestRootForGitBreachDir(t, dir)
	r.SetRect(0, 0, 100, 40)
	r.openGitBreach()

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(100, 40)
	r.gitBreachLayout.SetRect(0, 0, 100, 40)
	r.gitBreachLayout.Draw(screen)

	sb := r.gitBreachCommitsScrollbar
	if sb == nil {
		t.Fatal("gitBreachCommitsScrollbar is nil")
	}
	if sb.maxOffset() <= 0 {
		t.Fatalf("maxOffset = %d, want real overflow to scroll through for 40 commits", sb.maxOffset())
	}
	sbx, sby, _, sbh := sb.GetRect()

	before, _ := r.gitBreachCommitsTable.GetOffset()

	setFocus := func(p tview.Primitive) { r.app.SetFocus(p) }
	handler := r.gitBreachLayout.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.Button1, 0), setFocus)
	sb.stopRepeat()
	handler(tview.MouseLeftUp, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.ButtonNone, 0), setFocus)

	after, _ := r.gitBreachCommitsTable.GetOffset()
	if after != before+1 {
		t.Errorf("commits table offset after clicking the down arrow = %d, want %d (before=%d)", after, before+1, before)
	}
}

// TestGitBreachHeaderClickCapturesOnlyOwnRow pins the fix itself: the
// four gitBreachFocus*OnClick functions must only react to a click on
// their own header row, not anywhere else in the box (the real bug
// above).
func TestGitBreachHeaderClickCapturesOnlyOwnRow(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.SetRect(0, 0, 100, 40)
	r.openGitBreach()

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(100, 40)
	r.gitBreachLayout.SetRect(0, 0, 100, 40)
	r.gitBreachLayout.Draw(screen)

	hx, hy, _, _ := r.gitBreachCommitsHeader.GetRect()
	tx, ty, _, th := r.gitBreachCommitsTable.GetRect()

	// A click below the header, within the table's own body, must not
	// be claimed by the header's capture.
	action, event := r.gitBreachFocusCommitsOnClick(tview.MouseLeftDown, tcell.NewEventMouse(tx, ty+th-1, tcell.Button1, 0))
	if action != tview.MouseLeftDown || event == nil {
		t.Errorf("gitBreachFocusCommitsOnClick(click in the table body) = (%v, %v), want it passed through unchanged", action, event)
	}

	// A click on the header's own row must still work as before.
	action, event = r.gitBreachFocusCommitsOnClick(tview.MouseLeftDown, tcell.NewEventMouse(hx, hy, tcell.Button1, 0))
	if action != tview.MouseConsumed || event != nil {
		t.Errorf("gitBreachFocusCommitsOnClick(click on the header itself) = (%v, %v), want it consumed", action, event)
	}
}
