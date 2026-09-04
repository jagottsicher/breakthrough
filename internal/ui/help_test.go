package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestHelpShortcutOpensHelp pins F1's own basic action.
func TestHelpShortcutOpensHelp(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.HelpShortcut()

	if r.activePage != helpPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, helpPage)
	}
}

// TestHelpShortcutFloatsOverProperties pins the user's own explicit
// design (see openHelp's own doc comment): F1 while another dialog is
// already open pushes Help on top of it rather than replacing it —
// closing Help returns to that dialog, still open, exactly the same
// "floats on top rather than replacing" behavior
// openOwnerGroupPicker already has over Properties.
func TestHelpShortcutFloatsOverProperties(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = dir + "/apple.txt"
	r.openProperties()
	if r.activePage != propertiesPage {
		t.Fatalf("setup: activePage = %q, want %q", r.activePage, propertiesPage)
	}

	r.HelpShortcut()
	if r.activePage != helpPage {
		t.Fatalf("activePage = %q, want %q (Help on top of Properties)", r.activePage, helpPage)
	}

	r.hideOverlay() // Escape's own action
	if r.activePage != propertiesPage {
		t.Errorf("activePage after closing Help = %q, want back to %q, still open", r.activePage, propertiesPage)
	}
}

// TestHelpShortcutIsNoopWhenAlreadyOpen pins that a second F1 while
// Help is already the front overlay doesn't push a duplicate copy of
// it on top of itself.
func TestHelpShortcutIsNoopWhenAlreadyOpen(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.HelpShortcut()
	if r.activePage != helpPage {
		t.Fatalf("setup: activePage = %q, want %q", r.activePage, helpPage)
	}

	r.HelpShortcut() // second press

	if r.activePage != helpPage {
		t.Errorf("activePage = %q, want still %q", r.activePage, helpPage)
	}
	// Exactly one layer of Help: closing once should leave nothing open,
	// not another copy of Help underneath.
	r.hideOverlay()
	if r.activePage != "" {
		t.Errorf("activePage after one hideOverlay = %q, want closed (\"\") — a second F1 must not have pushed a duplicate layer", r.activePage)
	}
}

// TestHelpTitleBarClosesOnCloseButtonClick pins the user's own explicit
// request for a close button on Help's own title bar, the same shape
// toolWindow's own (see toolwindow.go): clicking the glyph closes Help,
// the same as Escape.
func TestHelpTitleBarClosesOnCloseButtonClick(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.HelpShortcut()
	if r.activePage != helpPage {
		t.Fatalf("setup: activePage = %q, want %q", r.activePage, helpPage)
	}

	x, y, width, _ := r.helpTitleBar.GetRect()
	closeX := x + toolWindowCloseButtonCol(0, width)
	captured, _ := r.captureHelpTitleBarMouse(tview.MouseLeftClick, tcell.NewEventMouse(closeX, y, tcell.ButtonNone, 0))

	if captured != tview.MouseConsumed {
		t.Error("clicking the close button should consume the click")
	}
	if r.activePage != "" {
		t.Errorf("activePage after clicking the close button = %q, want closed (\"\")", r.activePage)
	}
}

// TestHelpTitleBarClickElsewhereDoesNothing pins that only the exact
// close-button cell does anything — Help's own title bar isn't
// draggable the way a non-modal toolWindow's is, so every other click
// on it is simply inert.
func TestHelpTitleBarClickElsewhereDoesNothing(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.HelpShortcut()

	x, y, _, _ := r.helpTitleBar.GetRect()
	captured, _ := r.captureHelpTitleBarMouse(tview.MouseLeftClick, tcell.NewEventMouse(x+1, y, tcell.ButtonNone, 0))

	if captured == tview.MouseConsumed {
		t.Error("a click away from the close button should not be consumed")
	}
	if r.activePage != helpPage {
		t.Errorf("activePage = %q, want still %q", r.activePage, helpPage)
	}
}

// TestHelpTitleBarActiveColorTracksTopmostOverlay pins the user's own
// explicit request that Help's own title bar share the same
// active/inactive color distinction every other overlay's own title
// bar already has (see updateOverlayTitleBarColors' own doc comment):
// FocusedBackground while Help is the topmost overlay, EditableBackground
// once something else (Help pushed on top of Properties, say) is.
func TestHelpTitleBarActiveColorTracksTopmostOverlay(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = dir + "/apple.txt"
	r.openProperties()
	r.HelpShortcut()

	if got, want := r.helpTitleBar.GetBackgroundColor(), r.theme.FocusedBackground; got != want {
		t.Errorf("helpTitleBar background while Help is topmost = %v, want FocusedBackground %v", got, want)
	}

	r.hideOverlay() // back to Properties
	if got, want := r.helpTitleBar.GetBackgroundColor(), r.theme.EditableBackground; got != want {
		t.Errorf("helpTitleBar background once Help is closed = %v, want EditableBackground %v", got, want)
	}
}

// TestHelpTextMentionsEveryRealShortcut pins that the help content
// itself actually names the keybindings this app has — a stale or
// incomplete reference would be worse than none at all.
func TestHelpTextMentionsEveryRealShortcut(t *testing.T) {
	want := []string{
		"F1", "Ctrl+Q", "Ctrl+C",
		"Ctrl+E", "Ctrl+L", "F2", "Ctrl+G", "Ctrl+F", "Ctrl+O",
		"Ctrl+P", "Ctrl+D", "Ctrl+K", "Ctrl+N", "Ctrl+S", "Ctrl+B", "Ctrl+T", "Ctrl+R", "Delete",
		"Enter", "Space", "Right-click",
		"Tab", "Escape",
		"PageUp", "PageDown", // Look's own PDF page-turn
		"Alt+arrow", // tool windows' own move gesture
	}
	for _, s := range want {
		if !strings.Contains(helpText, s) {
			t.Errorf("helpText is missing %q", s)
		}
	}
}
