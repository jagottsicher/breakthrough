package ui

import (
	"strings"
	"testing"

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

// TestHelpTextMentionsEveryRealShortcut pins that the help content
// itself actually names the keybindings this app has — a stale or
// incomplete reference would be worse than none at all.
func TestHelpTextMentionsEveryRealShortcut(t *testing.T) {
	want := []string{
		"F1", "Ctrl+Q", "Ctrl+C",
		"Ctrl+E", "Ctrl+L", "Ctrl+R", "Ctrl+G", "Ctrl+F", "Ctrl+O",
		"Enter", "Space", "Right-click",
		"Tab", "Escape",
	}
	for _, s := range want {
		if !strings.Contains(helpText, s) {
			t.Errorf("helpText is missing %q", s)
		}
	}
}
