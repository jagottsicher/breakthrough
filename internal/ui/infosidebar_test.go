package ui

import (
	"slices"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestToggleInfoSidebarShortcutShowsAndHides pins F3's own basic
// show/hide action, and that it's tracked outside activePage/
// overlayStack — see newInfoSidebarView's own doc comment on why.
func TestToggleInfoSidebarShortcutShowsAndHides(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	if r.infoSidebarVisible {
		t.Fatal("setup: info sidebar should start hidden")
	}
	if slices.Contains(r.GetPageNames(true), infoSidebarPage) {
		t.Fatal("setup: info sidebar page should not be visible yet")
	}

	r.ToggleInfoSidebarShortcut()
	if !r.infoSidebarVisible {
		t.Error("first toggle should show the sidebar")
	}
	if !slices.Contains(r.GetPageNames(true), infoSidebarPage) {
		t.Error("info sidebar page should be visible in Pages after showing")
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q, want unchanged (\"\") — showing the sidebar isn't modal", r.activePage)
	}

	r.ToggleInfoSidebarShortcut()
	if r.infoSidebarVisible {
		t.Error("second toggle should hide the sidebar again")
	}
	if slices.Contains(r.GetPageNames(true), infoSidebarPage) {
		t.Error("info sidebar page should not be visible in Pages after hiding")
	}
}

// TestInfoSidebarSizeIsAtLeastOneThirdWidthAndFullHeight pins the
// sizing contract from the user's own request: at least a third of the
// screen's width, flush against its right edge, and — for now — its
// full height top to bottom (see infoSidebarSize's own doc comment on
// why the top/bottom margin is deliberately not done here yet).
func TestInfoSidebarSizeIsAtLeastOneThirdWidthAndFullHeight(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 90, 40)

	r.showInfoSidebar()

	x, y, width, height := r.infoSidebar.GetRect()
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

// TestInfoSidebarSizeRespectsMinWidthFloor pins infoSidebarMinWidth as a
// floor for a terminal narrow enough that a literal third of it would
// otherwise be unusable.
func TestInfoSidebarSizeRespectsMinWidthFloor(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 30, 20) // a third of this (10) is below the floor

	width, _ := r.infoSidebarSize()
	if width != infoSidebarMinWidth {
		t.Errorf("width = %d, want the floor %d", width, infoSidebarMinWidth)
	}
}

// TestCaptureInfoSidebarMouseSwallowsEveryActionInsideItsRect pins the
// fix for a real gap: tview.Box's own default MouseHandler only ever
// consumes MouseLeftDown, so without this capture, a right-click or
// scroll landing on the sidebar would fall straight through to the
// panel underneath, sharing that same screen space.
func TestCaptureInfoSidebarMouseSwallowsEveryActionInsideItsRect(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 90, 40)
	r.showInfoSidebar()

	x, y, width, _ := r.infoSidebar.GetRect()
	insideX, insideY := x+width/2, y

	action, event := r.captureInfoSidebarMouse(tview.MouseScrollUp, tcell.NewEventMouse(insideX, insideY, tcell.ButtonNone, 0))
	if action != tview.MouseConsumed || event != nil {
		t.Errorf("inside click: action=%v event=%v, want (MouseConsumed, nil)", action, event)
	}

	outsideX := x - 1
	action, event = r.captureInfoSidebarMouse(tview.MouseScrollUp, tcell.NewEventMouse(outsideX, insideY, tcell.ButtonNone, 0))
	if action != tview.MouseScrollUp || event == nil {
		t.Errorf("outside click: action=%v event=%v, want passed through unchanged", action, event)
	}
}
