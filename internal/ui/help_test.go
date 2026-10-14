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

// TestHelpTextEscapesLiteralBracketExamples pins a real bug: the Log
// Audit format description uses "[level]" and "[core:error]" as
// literal illustrative text, not as tview style tags. Left unescaped,
// tview's own tag parser consumed them as (invalid) style tags instead
// of displaying them — visibly corrupting the colors of everything
// rendered afterward, and silently breaking keyboard scrolling in the
// process (tview's line index is built from the same tag-parsing pass).
// Rendered through a real TextView, not just scanned as a Go string,
// so this actually exercises tview's own parser rather than an
// approximation of it. See help.go's "[level[]"/"[core:error[]" escape
// syntax (an extra "[" right before the closing "]", not "[[level]")
// for why these specific two render correctly.
func TestHelpTextEscapesLiteralBracketExamples(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openHelp()

	got := r.helpView.GetText(true)
	for _, want := range []string{"[level] message", `"[core:error]"`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered help text is missing literal %q — tview's tag parser likely swallowed it instead of displaying it:\n%s", want, got)
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
// incomplete reference would be worse than none at all. Only three
// Ctrl-letter bindings remain real (see cmd/breakthrough's own package
// doc comment): everything else that used to be here moved to the
// primary keyboard layer (see TestHelpTextMentionsThePrimaryKeyboardLayer
// just below) once each had a plain-letter or chord home covering the
// exact same ground, "also works while Properties is open" reach
// included — Options and mouse reporting (the "o" chord's own "oo"/"om")
// most recently.
func TestHelpTextMentionsEveryRealShortcut(t *testing.T) {
	want := []string{
		"Ctrl+Q", "Ctrl+C",
		"Ctrl+T", "Ctrl+Delete", "Delete",
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
// bindings retired once each gained a plain-letter or chord home
// covering the same ground (see cmd/breakthrough's own package doc
// comment) — a regression test for exactly the state this file was in
// right after the dispatch cases were deleted but before the prose
// describing them was rewritten to match. Checked as a trailing-
// space/word-boundary pattern, not the bare "Ctrl+X", wherever the bare
// form is still a legitimate substring here for an unrelated reason:
// "Ctrl+D" hides inside "Ctrl+Delete" (Remove), "Ctrl+S" inside
// "Ctrl+Shift+Tab", and Ctrl+N is skipped entirely — the bash line's own
// history recall really is bound to Ctrl+P/Ctrl+N (see the "Bash line"
// section) in a shape ("Ctrl+P/Ctrl+N always recall") no fixed pattern
// here can tell apart from a retired "Ctrl+N " metadata binding. Ctrl+O
// and Ctrl+_ (Options, mouse reporting — retired to the "o" chord's own
// "oo"/"om") are safe as bare substrings: neither collides with
// anything else remaining here.
func TestHelpTextNeverMentionsRetiredCtrlBindings(t *testing.T) {
	for _, bad := range []string{
		"Ctrl+E", "Ctrl+L", "Ctrl+G", "Ctrl+F",
		"Ctrl+D ", "Ctrl+K", "Ctrl+U", "Ctrl+S ", "Ctrl+B", "Ctrl+R",
		"Ctrl+O", "Ctrl+_",
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

// TestHelpTextPlainKeyTableColumnsStayAligned pins the three-column
// "c Copy / d Move to Trash / i Properties" grid's own column
// positions — a real, user-reported bug: this block is a hand-typed
// string literal with manually counted spaces (not computed padding),
// and a single miscounted row silently drifted every key/label after
// it one or two characters to the right for the rest of the block,
// which only showed up as "shifted" columns on a real screen, never as
// a build or vet failure. Checked here against every full row of the
// grid (the two narrower continuation rows for "+/-"/"by pattern"
// don't have all three columns, so they're deliberately left out
// rather than special-cased) — each one's key/label columns must start
// at the exact same byte offset as the first row, not just "look
// roughly aligned".
func TestHelpTextPlainKeyTableColumnsStayAligned(t *testing.T) {
	lines := strings.Split(helpText, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "  c   Copy") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("could not locate the plain-key table's own first row (\"  c   Copy\") in helpText — did its content change shape?")
	}
	const tableLen = 13 // c/x/v/r/f/n/s/a rows, the +/- and "by pattern" continuation, then B/C/q
	if start+tableLen > len(lines) {
		t.Fatalf("plain-key table at line %d would run past the end of helpText (%d lines)", start, len(lines))
	}
	table := lines[start : start+tableLen]
	if !strings.Contains(table[tableLen-1], "Bash command line") {
		t.Fatalf("plain-key table's own last row = %q, want it to end with \"Bash command line\" — did its content change shape?", table[tableLen-1])
	}

	var fullRows []string
	for _, line := range table {
		if strings.Contains(line, "Select/deselect") || strings.Contains(line, "by pattern") {
			continue
		}
		fullRows = append(fullRows, line)
	}
	if len(fullRows) != 11 {
		t.Fatalf("found %d full rows in the plain-key table, want 11 — did its content change shape? rows: %q", len(fullRows), fullRows)
	}

	// Only a run of 2+ spaces counts as a real column boundary — a
	// single space is just an ordinary word gap inside a label like
	// "Move to Trash" or "Go to last row", not a new column. Counted in
	// runes, not bytes ("range"'s own index): "…" (used in "Connect…"/
	// "Open with…") is one display column but three UTF-8 bytes, which
	// would otherwise misreport every column after it on its own line
	// as "shifted" by two columns that were never actually there.
	columnStarts := func(line string) []int {
		var starts []int
		spaceRun := 0
		for pos, r := range []rune(line) {
			if r == ' ' {
				spaceRun++
				continue
			}
			if pos == 0 || spaceRun >= 2 {
				starts = append(starts, pos)
			}
			spaceRun = 0
		}
		return starts
	}

	want := columnStarts(fullRows[0])
	if len(want) < 6 {
		t.Fatalf("reference row %q: got %d columns, want at least 6", fullRows[0], len(want))
	}
	want = want[:6]

	for _, line := range fullRows[1:] {
		got := columnStarts(line)
		if len(got) < 6 {
			t.Errorf("line %q: got %d columns, want at least 6", line, len(got))
			continue
		}
		got = got[:6]
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("line %q: columns at %v, want %v (reference row: %q)", line, got, want, fullRows[0])
		}
	}
}

// TestHelpScrollbarMouseClickThroughRealDispatch is the Help overlay's
// own counterpart to the earlier Look/Mounts/Git Breach regression
// tests: a click on the scrollbar's own down arrow must actually
// scroll, driven through the real dispatch chain (helpLayout's own
// MouseHandler), not the scrollbar's captureMouse directly — a direct
// call wouldn't have caught any of the dispatch-ordering bugs found so
// far in this rollout. Help's own full command reference is long
// enough to overflow any realistic terminal height on its own, so no
// synthetic fixture content is needed here.
func TestHelpScrollbarMouseClickThroughRealDispatch(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 20)
	r.openHelp()

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(100, 20)
	r.helpLayout.Draw(screen)

	sb := r.helpScrollbar
	if sb.maxOffset() <= 0 {
		t.Fatalf("maxOffset = %d, want Help's own full command reference to overflow a 20-row screen", sb.maxOffset())
	}
	sbx, sby, _, sbh := sb.GetRect()
	before, _ := r.helpView.GetScrollOffset()

	setFocus := func(p tview.Primitive) { r.app.SetFocus(p) }
	handler := r.helpLayout.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.Button1, 0), setFocus)
	sb.stopRepeat()
	handler(tview.MouseLeftUp, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.ButtonNone, 0), setFocus)

	after, _ := r.helpView.GetScrollOffset()
	if after != before+1 {
		t.Errorf("helpView offset after clicking the down arrow = %d, want %d (before=%d)", after, before+1, before)
	}
}
