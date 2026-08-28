package ui

import (
	"slices"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestToggleDetailsSidebarShortcutShowsAndHides pins Ctrl+D's own basic
// show/hide action, and that it's tracked outside activePage/
// overlayStack — see newDetailsSidebarView's own doc comment on why.
func TestToggleDetailsSidebarShortcutShowsAndHides(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	if r.detailsSidebarVisible {
		t.Fatal("setup: details sidebar should start hidden")
	}
	if slices.Contains(r.GetPageNames(true), detailsSidebarPage) {
		t.Fatal("setup: details sidebar page should not be visible yet")
	}

	r.ToggleDetailsSidebarShortcut()
	if !r.detailsSidebarVisible {
		t.Error("first toggle should show the sidebar")
	}
	if !slices.Contains(r.GetPageNames(true), detailsSidebarPage) {
		t.Error("details sidebar page should be visible in Pages after showing")
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q, want unchanged (\"\") — showing the sidebar isn't modal", r.activePage)
	}

	r.ToggleDetailsSidebarShortcut()
	if r.detailsSidebarVisible {
		t.Error("second toggle should hide the sidebar again")
	}
	if slices.Contains(r.GetPageNames(true), detailsSidebarPage) {
		t.Error("details sidebar page should not be visible in Pages after hiding")
	}
}

// TestToggleDetailsSidebarShortcutNoOpsWhileAnOverlayIsOpen mirrors
// TestTrashbinShortcutNoOpsWhileAnOverlayIsOpen (see trash_test.go) for
// Ctrl+D: like every other guarded shortcut, it must not act while some
// other overlay is already open.
func TestToggleDetailsSidebarShortcutNoOpsWhileAnOverlayIsOpen(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	r.openOptions() // any overlay; makes acceptsGlobalShortcut false
	r.ToggleDetailsSidebarShortcut()

	if r.detailsSidebarVisible {
		t.Error("ToggleDetailsSidebarShortcut acted while an overlay was open")
	}
}

// TestInfoSidebarSizeIsAtLeastOneThirdWidthAndFullHeight pins the
// sizing contract from the user's own request: at least a third of the
// screen's width, flush against its right edge, and — for now — its
// full height top to bottom (see detailsSidebarSize's own doc comment
// on why the top/bottom margin is deliberately not done here yet).
func TestInfoSidebarSizeIsAtLeastOneThirdWidthAndFullHeight(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 90, 40)

	r.showDetailsSidebar()

	x, y, width, height := r.detailsSidebar.GetRect()
	if want := 90 / 3; width < want {
		t.Errorf("width = %d, want at least a third of the screen (%d)", width, want)
	}
	if x+width != 90 {
		t.Errorf("sidebar isn't flush against the right edge: x=%d width=%d, screen width=90", x, width)
	}
	if y != 0 || height != 40 {
		t.Errorf("y,height = %d,%d, want 0,40 (the screen's full height)", y, height)
	}
}

// TestDetailsSidebarSizeRespectsMinWidthFloor pins detailsSidebarMinWidth
// as a floor for a terminal narrow enough that a literal third of it
// would otherwise be unusable.
func TestDetailsSidebarSizeRespectsMinWidthFloor(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 30, 20) // a third of this (10) is below the floor

	width, _ := r.detailsSidebarSize()
	if width != detailsSidebarMinWidth {
		t.Errorf("width = %d, want the floor %d", width, detailsSidebarMinWidth)
	}
}

// TestCaptureButtonBarMouseDetailsClickTogglesSidebar pins the "^D
// Details" button (see buildButtonBar/runButtonBarAction) to the same
// toggleDetailsSidebar Ctrl+D already runs — one action, two ways to
// reach it, and unlike Ctrl+D, unguarded (see toggleDetailsSidebar's own
// doc comment on why a click doesn't need acceptsGlobalShortcut).
func TestCaptureButtonBarMouseDetailsClickTogglesSidebar(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	span, ok := buttonBarSpanFor(r, buttonActionDetails)
	if !ok {
		t.Fatal("no Details span found")
	}

	clickButtonBar(t, r, span.startCol)
	if !r.detailsSidebarVisible {
		t.Error("clicking Details should show the sidebar")
	}

	clickButtonBar(t, r, span.startCol)
	if r.detailsSidebarVisible {
		t.Error("clicking Details again should hide the sidebar")
	}
}

// TestCaptureDetailsSidebarMouseSwallowsEveryActionInsideItsRect pins
// the fix for a real gap: tview.Box's own default MouseHandler only
// ever consumes MouseLeftDown, so without this capture, a right-click or
// scroll landing on the sidebar would fall straight through to the
// panel underneath, sharing that same screen space.
func TestCaptureDetailsSidebarMouseSwallowsEveryActionInsideItsRect(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 90, 40)
	r.showDetailsSidebar()

	x, y, width, _ := r.detailsSidebar.GetRect()
	insideX, insideY := x+width/2, y

	action, event := r.captureDetailsSidebarMouse(tview.MouseScrollUp, tcell.NewEventMouse(insideX, insideY, tcell.ButtonNone, 0))
	if action != tview.MouseConsumed || event != nil {
		t.Errorf("inside click: action=%v event=%v, want (MouseConsumed, nil)", action, event)
	}

	outsideX := x - 1
	action, event = r.captureDetailsSidebarMouse(tview.MouseScrollUp, tcell.NewEventMouse(outsideX, insideY, tcell.ButtonNone, 0))
	if action != tview.MouseScrollUp || event == nil {
		t.Errorf("outside click: action=%v event=%v, want passed through unchanged", action, event)
	}
}
