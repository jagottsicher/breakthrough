package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestHelpShortcutOpensHelp pins openHelp's own basic action.
func TestHelpShortcutOpensHelp(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openHelp()

	if r.activePage != helpPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, helpPage)
	}
}

// TestHelpShortcutFloatsOverProperties pins the user's own explicit
// design (see openHelp's own doc comment): opening Help while another
// dialog is already open pushes Help on top of it rather than replacing
// it —
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

	r.openHelp()
	if r.activePage != helpPage {
		t.Fatalf("activePage = %q, want %q (Help on top of Properties)", r.activePage, helpPage)
	}

	r.hideOverlay() // Escape's own action
	if r.activePage != propertiesPage {
		t.Errorf("activePage after closing Help = %q, want back to %q, still open", r.activePage, propertiesPage)
	}
}

// TestHelpShortcutIsNoopWhenAlreadyOpen pins that opening Help a second
// time while it's already the front overlay doesn't push a duplicate
// copy of it on top of itself.
func TestHelpShortcutIsNoopWhenAlreadyOpen(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openHelp()
	if r.activePage != helpPage {
		t.Fatalf("setup: activePage = %q, want %q", r.activePage, helpPage)
	}

	r.openHelp() // second press

	if r.activePage != helpPage {
		t.Errorf("activePage = %q, want still %q", r.activePage, helpPage)
	}
	// Exactly one layer of Help: closing once should leave nothing open,
	// not another copy of Help underneath.
	r.hideOverlay()
	if r.activePage != "" {
		t.Errorf("activePage after one hideOverlay = %q, want closed (\"\") — opening Help a second time must not have pushed a duplicate layer", r.activePage)
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
	r.openHelp()
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
	r.openHelp()

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
	r.openHelp()

	if got, want := r.helpTitleBar.GetBackgroundColor(), r.theme.FocusedBackground; got != want {
		t.Errorf("helpTitleBar background while Help is topmost = %v, want FocusedBackground %v", got, want)
	}

	r.hideOverlay() // back to Properties
	if got, want := r.helpTitleBar.GetBackgroundColor(), r.theme.EditableBackground; got != want {
		t.Errorf("helpTitleBar background once Help is closed = %v, want EditableBackground %v", got, want)
	}
}

// TestHelpSizeWidthMatchesContentNotScreen pins a real, user-reported
// bug: helpSize used to size the window against a fixed percentage of
// the screen's own width (90%), which on a wide terminal left most of
// it as dead space — helpText's own lines are hand-wrapped at a fixed
// width for readability in the source, and SetWrap(true) only wraps a
// line *longer* than the window, never un-wraps a shorter one to use
// more of it. helpSize's own width must now track helpContentWidth
// (the text's own longest real line, plus its 1-column left/right
// border padding) regardless of how wide the screen actually is, as
// long as the screen is wide enough to show it at all.
func TestHelpSizeWidthMatchesContentNotScreen(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 300, 60) // deliberately much wider than the content needs

	width, _ := r.helpSize()
	if want := r.helpContentWidth(); width != want {
		t.Errorf("width = %d, want %d (helpContentWidth) — a wide screen must not stretch it further", width, want)
	}
	if width == 300*9/10 {
		t.Error("width still matches the old, screen-percentage-based formula")
	}
}

// TestHelpSizeWidthNeverExceedsScreen pins the other side of the same
// change: helpContentWidth can be wider than a genuinely narrow
// terminal, and helpSize must still never ask for more than the screen
// actually has (clampToScreen, called right after in openHelp, would
// otherwise silently absorb this instead — better to never produce an
// oversized value in the first place). Screen width is comfortably
// above helpMinWidth here so that floor — a separate, pre-existing
// concern for a genuinely tiny terminal, left to openHelp's own
// clampToScreen as the real final safety net — can't mask this check.
func TestHelpSizeWidthNeverExceedsScreen(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	screenWidth := helpMinWidth + 10 // above the floor, still narrower than helpContentWidth()
	r.SetRect(0, 0, screenWidth, 60)

	width, _ := r.helpSize()
	if width > screenWidth {
		t.Errorf("width = %d, want at most the screen's own width %d", width, screenWidth)
	}
}

// TestHelpShowsAboutSectionWithVersionInfo pins the user's own explicit
// request for an About section at the bottom of Help, with version
// info in it: SetVersionInfo's own values show up in what actually
// gets displayed (fullHelpText, via openHelp) once Help is open.
func TestHelpShowsAboutSectionWithVersionInfo(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetVersionInfo("v1.2.3", "abc1234", "2026-09-05", "goreleaser")

	r.openHelp()

	got := r.helpView.GetText(true)
	for _, want := range []string{"About", "v1.2.3", "abc1234", "2026-09-05", "goreleaser"} {
		if !strings.Contains(got, want) {
			t.Errorf("displayed help text is missing %q", want)
		}
	}
}

// TestAboutTextDefaultsMatchPlainBuild pins that a Root nothing ever
// calls SetVersionInfo on (every test in this package, and a real
// binary if main's own call to it were ever removed) shows exactly the
// same "dev build" text cmd/breakthrough's own version/commit/date/
// builtBy vars default to for a plain "go build" with no ldflags.
func TestAboutTextDefaultsMatchPlainBuild(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	got := r.aboutText()
	for _, want := range []string{"dev", "none", "unknown", "source"} {
		if !strings.Contains(got, want) {
			t.Errorf("aboutText() is missing the default %q: %s", want, got)
		}
	}
}

// TestHelpTextNeverMentionsAI is a regression guard for this project's
// own strict rule against any AI/assistant attribution anywhere in its
// own output — the About section is exactly the kind of place that
// convention could otherwise slip into unnoticed.
func TestHelpTextNeverMentionsAI(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	got := strings.ToLower(r.fullHelpText())
	for _, bad := range []string{"claude", "anthropic", "ai-generated", "ai generated"} {
		if strings.Contains(got, bad) {
			t.Errorf("help text contains %q, which must never appear here", bad)
		}
	}
}

// TestHelpTextNeverMentionsFunctionKeys pins the removal itself: no
// function key does anything in this app any more (see
// cmd/breakthrough's own dispatch), so none should be documented as if
// it still did — a regression test for exactly the state this file was
// in right after the F1-F12 dispatch cases were deleted but before the
// prose describing them was rewritten to match.
func TestHelpTextNeverMentionsFunctionKeys(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	got := r.fullHelpText()
	for n := 1; n <= 12; n++ {
		bad := fmt.Sprintf("F%d", n)
		if strings.Contains(got, bad) {
			t.Errorf("help text mentions %q — no function key does anything in this app any more", bad)
		}
	}
}

// TestHelpTextMentionsEveryRealShortcut pins that the help content
// itself actually names the keybindings this app has — a stale or
// incomplete reference would be worse than none at all. Only five
// Ctrl-letter bindings remain real (see cmd/breakthrough's own package
// doc comment): everything else that used to be here moved to the
// primary keyboard layer (see TestHelpTextMentionsThePrimaryKeyboardLayer
// just below) once each had a plain-letter home covering the exact same
// ground, "also works while Properties is open" reach included.
func TestHelpTextMentionsEveryRealShortcut(t *testing.T) {
	want := []string{
		"Ctrl+_", "Ctrl+Q", "Ctrl+C",
		"Ctrl+O", "Ctrl+T", "Ctrl+Delete", "Delete",
		"Enter", "Space", "Right-click",
		"Tab", "Escape",
		"PageUp", "PageDown", // Look's own PDF page-turn
		"Alt+arrow",                                                        // tool windows' own move gesture
		"Ctrl+1", "Ctrl+0", "Alt+1", "Alt+0", "Ctrl+Tab", "Ctrl+Shift+Tab", // panel tabs
	}
	for _, s := range want {
		if !strings.Contains(helpText, s) {
			t.Errorf("helpText is missing %q", s)
		}
	}
}

// TestHelpTextNeverMentionsRetiredCtrlBindings mirrors
// TestHelpTextNeverMentionsFunctionKeys for the round of Ctrl-letter
// bindings retired once each gained a plain-letter home covering the
// same ground (see cmd/breakthrough's own package doc comment) — a
// regression test for exactly the state this file was in right after
// the dispatch cases were deleted but before the prose describing them
// was rewritten to match. Checked as a trailing-space/word-boundary
// pattern, not the bare "Ctrl+X", wherever the bare form is still a
// legitimate substring here for an unrelated reason: "Ctrl+D" hides
// inside "Ctrl+Delete" (Remove), "Ctrl+S" inside "Ctrl+Shift+Tab", and
// Ctrl+N is skipped entirely — the bash line's own history recall really
// is bound to Ctrl+P/Ctrl+N (see the "Bash line" section) in a shape
// ("Ctrl+P/Ctrl+N always recall") no fixed pattern here can tell apart
// from a retired "Ctrl+N " metadata binding.
func TestHelpTextNeverMentionsRetiredCtrlBindings(t *testing.T) {
	for _, bad := range []string{
		"Ctrl+E", "Ctrl+L", "Ctrl+G", "Ctrl+F",
		"Ctrl+D ", "Ctrl+K", "Ctrl+U", "Ctrl+S ", "Ctrl+B", "Ctrl+R",
	} {
		if strings.Contains(helpText, bad) {
			t.Errorf("helpText mentions %q — that binding was retired in favor of a plain letter", bad)
		}
	}
}

// TestHelpTextMentionsThePrimaryKeyboardLayer guards the newer,
// plain-letter layer (see keymap.go) the same way the legacy Ctrl/F-key
// one above is guarded — every key the registry actually binds should
// be findable in the help text, and every chord family/member too.
// Hand-transcribed rather than generated from the registry (a real,
// acknowledged gap — see keymap.go's own package doc on deriving the
// help text mechanically in a follow-up), so this is exactly the kind
// of drift a generated version would make structurally impossible.
func TestHelpTextMentionsThePrimaryKeyboardLayer(t *testing.T) {
	for _, c := range plainCommands() {
		if !strings.ContainsRune(helpText, c.key) {
			t.Errorf("helpText never mentions the plain key %q (%s)", string(c.key), c.label)
		}
	}
	for _, f := range chordFamilies() {
		if !strings.ContainsRune(helpText, f.prefix) {
			t.Errorf("helpText never mentions the chord prefix %q (%s)", string(f.prefix), f.name)
		}
		for _, m := range f.members {
			chord := string(f.prefix) + string(m.key)
			if !strings.Contains(helpText, chord) {
				t.Errorf("helpText never mentions the chord %q (%s)", chord, m.label)
			}
		}
	}
}
