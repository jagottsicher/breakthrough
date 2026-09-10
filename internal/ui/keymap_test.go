package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestNoDuplicatePlainKeyBindings is the collision test the whole
// registry approach exists to make possible: five hand-maintained
// copies of a keymap (the old dispatch switch, the Ctrl+_ prefix tree,
// panel captures, button-bar strings, the help text) is exactly how
// this project ended up with real collisions while designing this very
// layer (m for both "menu" and an earlier draft's "metadata", S
// claimed by both "swap" and "sed" in an earlier pass) — caught by
// re-reading a hand-written list each time, which does not scale and
// did not catch all of them. This test does, mechanically, from here
// on.
func TestNoDuplicatePlainKeyBindings(t *testing.T) {
	seen := map[rune]string{}
	for _, c := range plainCommands() {
		if other, dup := seen[c.key]; dup {
			t.Errorf("key %q is bound to both %q and %q", string(c.key), other, c.label)
		}
		seen[c.key] = c.label
	}

	for _, f := range chordFamilies() {
		if other, dup := seen[f.prefix]; dup {
			t.Errorf("chord prefix %q collides with plain command %q", string(f.prefix), other)
		}
		seen[f.prefix] = "chord prefix: " + f.name

		members := map[rune]string{}
		for _, m := range f.members {
			if other, dup := members[m.key]; dup {
				t.Errorf("chord %q: key %q is bound to both %q and %q", string(f.prefix), string(m.key), other, m.label)
			}
			members[m.key] = m.label
		}
	}
}

// TestPlainCommandsHaveNoEmptyFields guards the registry's own basic
// shape — a zero key or a nil action would silently do nothing when
// dispatched, which is exactly the class of bug this file's own
// dispatch tests below are meant to catch, but only for the handful of
// keys they exercise directly.
func TestPlainCommandsHaveNoEmptyFields(t *testing.T) {
	for _, c := range plainCommands() {
		if c.key == 0 {
			t.Errorf("command %q has no key", c.label)
		}
		if c.label == "" {
			t.Errorf("key %q has no label", string(c.key))
		}
		if c.action == nil {
			t.Errorf("key %q (%q) has no action", string(c.key), c.label)
		}
	}
	for _, f := range chordFamilies() {
		if f.prefix == 0 || f.name == "" || len(f.members) == 0 {
			t.Errorf("chord family %+v is incomplete", f)
		}
		for _, m := range f.members {
			if m.key == 0 || m.label == "" || m.action == nil {
				t.Errorf("chord %q: member %+v is incomplete", string(f.prefix), m)
			}
		}
	}
}

// newPlainKeyRoot is a Root on a real, throwaway directory with the
// panel's own table holding real keyboard focus — the precondition
// acceptsPlainKeyCommand checks, and the state every test below needs
// to actually reach HandlePlainKey's dispatch rather than have it
// report false and do nothing.
func newPlainKeyRoot(t *testing.T) *Root {
	t.Helper()
	isolateUserConfigFile(t)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.app.SetFocus(r.panel.table)
	return r
}

// runeEvent is a plain, unmodified character keystroke — what every
// test below sends, since that's the only shape HandlePlainKey ever
// acts on.
func runeEvent(r rune) *tcell.EventKey {
	return tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone)
}

func TestPlainKeyTogglesHiddenFiles(t *testing.T) {
	root := newPlainKeyRoot(t)
	before := root.panel.showHidden

	if !root.HandlePlainKey(runeEvent('.')) {
		t.Fatal("'.' should have been consumed")
	}
	if root.panel.showHidden == before {
		t.Error("showHidden did not change")
	}
}

func TestPlainKeySelectAllChecksEveryRow(t *testing.T) {
	root := newPlainKeyRoot(t)

	root.HandlePlainKey(runeEvent('a'))

	if len(root.panel.SelectedPaths()) == 0 {
		t.Error("'a' should have selected at least one row")
	}
}

// TestPlainKeyMoveToTrash is a real, end-to-end run of "d" against a
// real file on a real filesystem — not just that the right method got
// called, but that the file actually moved.
func TestPlainKeyMoveToTrash(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	isolateUserConfigFile(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1) // off ".." onto the one real entry
	r.app.SetFocus(r.panel.table)

	if !r.HandlePlainKey(runeEvent('d')) {
		t.Fatal("'d' should have been consumed")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("a.txt should be gone from its original location, stat err = %v", err)
	}
}

// TestPlainKeyCopyWorksWithoutAPriorRightClick is the regression test
// for the gap copyCurrentSelection/cutCurrentSelection exist to close:
// before, Copy and Cut only ever ran after something had already set
// r.target (a right-click, or one of the *CurrentEntry functions) —
// neither had a bare-key path of its own. Calling copyToClipboard
// directly here, bypassing the sync, reproduces the original bug.
func TestPlainKeyCopyWorksWithoutAPriorRightClick(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.panel.focusRow(1) // a real entry, never right-clicked this run
	if root.target != "" {
		t.Fatal("setup: target should still be empty")
	}

	if !root.HandlePlainKey(runeEvent('c')) {
		t.Fatal("'c' should have been consumed")
	}

	if len(root.clipboard) == 0 {
		t.Error("Copy produced an empty clipboard — the original bug this wrapper fixes")
	}
}

// TestPlainKeyIgnoredOutsidePanelFocus pins acceptsPlainKeyCommand's own
// central promise: typing in the filter box must never fire a command,
// however innocuous the letter looks.
func TestPlainKeyIgnoredOutsidePanelFocus(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.app.SetFocus(root.panel.filterField)

	if root.HandlePlainKey(runeEvent('d')) {
		t.Error("a key typed into the filter field must not be read as a command")
	}
}

func TestPlainKeyIgnoredWithAnOverlayOpen(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.openHelp()

	if root.HandlePlainKey(runeEvent('d')) {
		t.Error("a key must not fire a panel command while an overlay is open")
	}
}

func TestPlainKeyIgnoredWithCtrlOrAltHeld(t *testing.T) {
	root := newPlainKeyRoot(t)

	ctrlD := tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModCtrl)
	if root.HandlePlainKey(ctrlD) {
		t.Error("Ctrl+d must fall through untouched, not be read as the plain 'd' command")
	}
}

// --- Chords -----------------------------------------------------------

func TestChordStartsAndResolves(t *testing.T) {
	root := newPlainKeyRoot(t)

	if !root.HandlePlainKey(runeEvent('g')) {
		t.Fatal("'g' should start the chord")
	}
	if root.pendingChord != 'g' {
		t.Errorf("pendingChord = %q, want 'g'", string(root.pendingChord))
	}

	root.panel.focusRow(3)
	if !root.HandlePlainKey(runeEvent('g')) {
		t.Fatal("the resolving key should also be consumed")
	}
	if root.pendingChord != 0 {
		t.Error("chord should be cleared after resolving")
	}
	if row, _ := root.panel.table.GetSelection(); row != 0 {
		t.Errorf("cursor row = %d, want 0 — \"gg\" should jump to the top", row)
	}
}

// TestChordGoUpNavigatesToParent pins "gu" — the user's own explicit
// request, added alongside "gp"/"gn" below: mirrors actionUp (the
// header row's own "↑" button), one level up from wherever the panel
// currently is.
func TestChordGoUpNavigatesToParent(t *testing.T) {
	root := newPlainKeyRoot(t)
	dir := root.panel.path
	sub := filepath.Join(dir, "app-data")
	if err := root.panel.navigate(sub); err != nil {
		t.Fatalf("setup: navigate(sub): %v", err)
	}

	root.HandlePlainKey(runeEvent('g'))
	root.HandlePlainKey(runeEvent('u'))

	if root.panel.path != dir {
		t.Errorf("path after \"gu\" = %q, want the parent %q", root.panel.path, dir)
	}
}

// TestChordGoBackAndGoForwardStepThroughHistory pins "gp"/"gn" — the
// user's own explicit request for a keyboard equivalent to the header
// row's own "<"/">" buttons (actionBack/actionForward), which until now
// only had a mouse path at all.
func TestChordGoBackAndGoForwardStepThroughHistory(t *testing.T) {
	root := newPlainKeyRoot(t)
	original := root.panel.path
	sub := filepath.Join(original, "app-data")
	if err := root.panel.navigate(sub); err != nil {
		t.Fatalf("setup: navigate(sub): %v", err)
	}

	root.HandlePlainKey(runeEvent('g'))
	root.HandlePlainKey(runeEvent('p')) // Back
	if root.panel.path != original {
		t.Fatalf("path after \"gp\" = %q, want %q", root.panel.path, original)
	}

	root.HandlePlainKey(runeEvent('g'))
	root.HandlePlainKey(runeEvent('n')) // Forward
	if root.panel.path != sub {
		t.Errorf("path after \"gn\" = %q, want %q", root.panel.path, sub)
	}
}

func TestChordEscapeCancelsWithoutRunningAnything(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.panel.focusRow(3)
	beforeRow, _ := root.panel.table.GetSelection()

	root.HandlePlainKey(runeEvent('g'))
	root.HandlePlainKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	if root.pendingChord != 0 {
		t.Error("Escape should have cleared the pending chord")
	}
	if row, _ := root.panel.table.GetSelection(); row != beforeRow {
		t.Errorf("cursor moved to %d, want it left at %d — Escape must not run a member", row, beforeRow)
	}
}

func TestChordUnknownSecondKeyReportsItself(t *testing.T) {
	root := newPlainKeyRoot(t)

	root.HandlePlainKey(runeEvent('g'))
	root.HandlePlainKey(runeEvent('q')) // not a member of the "g" family

	if root.activePage != errorPage {
		t.Fatalf("activePage = %q, want a notice on %q", root.activePage, errorPage)
	}
	got := strings.Join(strings.Fields(root.errorView.GetText(true)), " ")
	if !strings.Contains(got, "gq") {
		t.Errorf("notice = %q, want it to name the unrecognized chord", got)
	}
}

func TestChordSwapsTheButtonBarAndRestoresIt(t *testing.T) {
	root := newPlainKeyRoot(t)
	ordinary := root.buttonBar.GetText(true)

	root.HandlePlainKey(runeEvent('p'))
	hint := root.buttonBar.GetText(true)
	if hint == ordinary {
		t.Fatal("the button bar should show the chord's own legend")
	}
	if !strings.Contains(hint, "chmod") || !strings.Contains(hint, "chown") {
		t.Errorf("legend = %q, want it to list chmod and chown", hint)
	}

	root.HandlePlainKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if got := root.buttonBar.GetText(true); got != ordinary {
		t.Errorf("button bar = %q, want the ordinary buttons back", got)
	}
}

func TestChordTimeoutCancelsSilently(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.HandlePlainKey(runeEvent('z'))

	// Force the deadline into the past rather than sleeping through the
	// real chordTimeout — this test only needs to know that an expired
	// deadline is treated as "cancel", not that the ticker's own timing
	// is exact.
	root.chordDeadline = time.Now().Add(-time.Second)
	root.cancelChord()

	if root.pendingChord != 0 {
		t.Error("an expired chord should have been cleared")
	}
	if root.activePage == errorPage {
		t.Error("a timeout must not show an error notice — only an explicit wrong key does")
	}
}

func TestChordIndicatorTextNamesThePendingPrefix(t *testing.T) {
	root := newPlainKeyRoot(t)

	if got := root.chordIndicatorText(); got != "" {
		t.Errorf("indicator = %q, want empty with no chord pending", got)
	}

	root.HandlePlainKey(runeEvent('g'))
	got := root.chordIndicatorText()
	if !strings.HasPrefix(got, "g") {
		t.Errorf("indicator = %q, want it to start with the pending prefix %q", got, "g")
	}
	if len([]rune(got)) != 2 {
		t.Errorf("indicator = %q, want exactly the prefix plus one countdown glyph", got)
	}
}

// TestChordIndicatorStartsFullAndEndsNearlyEmpty is a regression test
// for a real bug caught live: the indicator's own fraction was computed
// from time *remaining* rather than time *elapsed*, so it read almost
// empty right after the chord started and almost full right before it
// expired — backwards from a countdown.
func TestChordIndicatorStartsFullAndEndsNearlyEmpty(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.HandlePlainKey(runeEvent('g'))

	justStarted := root.chordIndicatorText()
	if !strings.Contains(justStarted, string(chordCountdownBlocks[0])) {
		t.Errorf("indicator right after starting = %q, want the full block %q", justStarted, string(chordCountdownBlocks[0]))
	}

	root.chordDeadline = time.Now().Add(50 * time.Millisecond)
	aboutToExpire := root.chordIndicatorText()
	last := chordCountdownBlocks[len(chordCountdownBlocks)-1]
	if !strings.Contains(aboutToExpire, string(last)) {
		t.Errorf("indicator just before expiry = %q, want the emptiest block %q", aboutToExpire, string(last))
	}
}

func TestChordIndicatorEmptiesOutAfterItsOwnDeadline(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.HandlePlainKey(runeEvent('g'))

	root.chordDeadline = time.Now().Add(-time.Millisecond)

	if got := root.chordIndicatorText(); got != "" {
		t.Errorf("indicator = %q, want empty once the deadline has passed", got)
	}
}

// TestChordLegendMemberIsClickable pins the user's own explicit request:
// once a chord is showing its own legend in the button bar, each member
// is clickable with the mouse, the same as an ordinary button — not just
// reachable by typing the second letter.
func TestChordLegendMemberIsClickable(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.HandlePlainKey(runeEvent('z')) // the "display" family

	span, ok := buttonBarSpanFor(root, 's') // "zs" — size format
	if !ok {
		t.Fatal("no span found for the 's' member of the 'z' chord")
	}
	before := root.settings.SizeBytes

	clickButtonBar(t, root, span.startCol)

	if root.settings.SizeBytes == before {
		t.Error("clicking the 's' member should have toggled the size format, the same as typing 'zs'")
	}
	if root.pendingChord != 0 {
		t.Error("the chord should be cleared after a member is clicked")
	}
}

// TestChordLegendEscCancelIsClickable pins the legend's own "Esccancel"
// text (see chordHintBar's own doc comment on why there's no space
// between the two) as a click target too: clicking it cancels the chord
// without running any member, the same as pressing Escape.
func TestChordLegendEscCancelIsClickable(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.HandlePlainKey(runeEvent('z'))
	before := root.settings.SizeBytes

	text := root.buttonBar.GetText(true)
	col := strings.Index(text, "Esccancel")
	if col < 0 {
		t.Fatal("legend text has no \"Esccancel\"")
	}
	clickButtonBar(t, root, len([]rune(text[:col])))

	if root.pendingChord != 0 {
		t.Error("clicking \"Esccancel\" should have cleared the pending chord")
	}
	if root.settings.SizeBytes != before {
		t.Error("clicking \"Esccancel\" must not run any member's action")
	}
}

// TestChordLegendHighlightsTheMemberKey pins the visual cue itself: each
// member's own resolving key is set off with the same ButtonBackground
// color every real button in this app already uses, one space on either
// side — per the user's own explicit request to make the letter-to-
// action mapping easier to scan at a glance.
func TestChordLegendHighlightsTheMemberKey(t *testing.T) {
	root := newPlainKeyRoot(t)

	text, _ := root.chordHintBar(chordFamily{
		prefix: 'z',
		name:   "display",
		members: []chordMember{
			{key: 's', label: "Size format"},
		},
	})

	want := fmt.Sprintf("[:%s:] s [-:-:-]Size format", colorTag(root.theme.ButtonBackground))
	if !strings.Contains(text, want) {
		t.Errorf("legend text = %q, want it to contain %q", text, want)
	}
}
