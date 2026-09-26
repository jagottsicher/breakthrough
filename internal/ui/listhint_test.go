package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// clickListHint simulates a real left-click on hint (one of the seven
// list screens' own bottom hint bars) at the given column — the same
// "draw onto a real screen first, then resolve InRect/GetInnerRect
// against real layout" shape clickButtonBar's own doc comment
// describes, generalized here to any of these hints rather than just
// the main button bar.
func clickListHint(t *testing.T, r *Root, hint *tview.TextView, spans *[]listHintSpan, col int) {
	t.Helper()
	width := tview.TaggedStringWidth(hint.GetText(true)) + 10
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(width, 24)
	hint.SetRect(0, 0, width, 1)
	hint.Draw(screen)

	rectX, _, _, _ := hint.GetInnerRect()
	r.captureListHintMouse(hint, spans)(tview.MouseLeftClick, tcell.NewEventMouse(rectX+col, 0, tcell.Button1, 0))
}

// TestBuildListHintPadsASingleRuneKey pins highlightKey's own padded
// style for a single-rune key: one space either side of it inside the
// colored background, the label following with no space of its own.
func TestBuildListHintPadsASingleRuneKey(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	keyBG := colorTag(theme.ButtonBackground)

	got, spans := buildListHint(theme, []listHintEntry{hintKey("r", "refresh", func(*Root) {})})

	want := fmt.Sprintf(" [:%s:] r [-:-:-]refresh ", keyBG)
	if got != want {
		t.Errorf("buildListHint text = %q, want %q", got, want)
	}
	if len(spans) != 1 {
		t.Fatalf("len(spans) = %d, want 1", len(spans))
	}
}

// TestBuildListHintWrapsAMultiCharKeyDirectly pins chordHintBar's own
// "Esc" treatment, generalized to any key text longer than one rune —
// no padding space, no space before the label.
func TestBuildListHintWrapsAMultiCharKeyDirectly(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	keyBG := colorTag(theme.ButtonBackground)

	got, _ := buildListHint(theme, []listHintEntry{hintKey("Esc", "close", nil)})

	want := fmt.Sprintf(" [:%s:]Esc[-:-:-]close ", keyBG)
	if got != want {
		t.Errorf("buildListHint = %q, want %q", got, want)
	}
}

// TestBuildListHintJoinsAGroupsOwnKeysWithASlashEachOwnSpan pins that a
// compound entry (e.g. "↑"/"↓" both under "move") renders its own keys
// adjacent, separated by a plain "/", each still getting its own
// independently clickable span — per the user's own explicit request
// that even a grouped arrow key be clickable on its own, not just the
// pair as one region.
func TestBuildListHintJoinsAGroupsOwnKeysWithASlashEachOwnSpan(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	keyBG := colorTag(theme.ButtonBackground)

	// "↑"/"↓" are each a single rune, so each gets highlightKey's own
	// padded style too — the same rule "r" gets, no special-casing for
	// a symbol instead of a letter.
	entry := listHintEntry{keys: []listHintKey{{"↑", func(*Root) {}}, {"↓", func(*Root) {}}}, label: "move"}
	got, spans := buildListHint(theme, []listHintEntry{entry})

	want := fmt.Sprintf(" [:%s:] ↑ [-:-:-]/[:%s:] ↓ [-:-:-]move ", keyBG, keyBG)
	if got != want {
		t.Errorf("buildListHint = %q, want %q", got, want)
	}
	if len(spans) != 2 {
		t.Fatalf("len(spans) = %d, want 2 (one per key, not one for the whole group)", len(spans))
	}
	if spans[0].startCol >= spans[1].startCol {
		t.Errorf("spans = %+v, want the second key's own span to start after the first's", spans)
	}
}

// TestBuildListHintOmitsASpanForANilRun pins that a key with no run
// (there is none for this file's own tests, but keeps buildListHint
// itself defensive) never adds a dead click target.
func TestBuildListHintOmitsASpanForANilRun(t *testing.T) {
	theme := config.DefaultTheme().Resolve()

	_, spans := buildListHint(theme, []listHintEntry{hintKey("r", "refresh", nil)})

	if len(spans) != 0 {
		t.Errorf("spans = %+v, want none for a nil run", spans)
	}
}

// TestBuildListHintSeparatesEntriesWithAPlainSpaceExceptBeforeEsc pins
// the same block-separator convention buildButtonBar's own blockSep and
// chordHintBar's own "Esccancel" already use: every entry gets a plain
// space before it, except one whose first key is exactly "Esc", which
// gets the heavier " │ " instead.
func TestBuildListHintSeparatesEntriesWithAPlainSpaceExceptBeforeEsc(t *testing.T) {
	theme := config.DefaultTheme().Resolve()

	got, _ := buildListHint(theme, []listHintEntry{
		hintKey("r", "refresh", nil),
		hintKey("t", "simulate", nil),
		hintKey("Esc", "close", nil),
	})

	if !strings.Contains(got, "]refresh [") {
		t.Errorf("buildListHint = %q, want a plain space between refresh and the next key", got)
	}
	if !strings.Contains(got, "]simulate │ [") {
		t.Errorf("buildListHint = %q, want the heavier \" │ \" separator right before Esc", got)
	}
}

// TestBuildListHintAppendsASuffixBeforeTheLabel pins Sessions' own
// "/click" case: plain, uncolored text between the last key and the
// label, for something the hint has to mention that isn't a real,
// simulatable key at all.
func TestBuildListHintAppendsASuffixBeforeTheLabel(t *testing.T) {
	theme := config.DefaultTheme().Resolve()

	entry := listHintEntry{keys: []listHintKey{{"Enter", func(*Root) {}}}, suffix: "/click", label: "activate"}
	got, _ := buildListHint(theme, []listHintEntry{entry})

	if !strings.Contains(got, "/click activate") {
		t.Errorf("buildListHint = %q, want it to contain %q", got, "/click activate")
	}
}

// TestBuildListHintReflectsTheGivenTheme pins that the key background
// actually comes from theme.ButtonBackground, not a fixed color — a
// live scheme switch (see applyTheme) must recolor these the same way
// it already does buildButtonBar/chordHintBar's own legend.
func TestBuildListHintReflectsTheGivenTheme(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	theme.ButtonBackground = 0x123456

	got, _ := buildListHint(theme, []listHintEntry{hintKey("r", "refresh", nil)})

	if !strings.Contains(got, colorTag(theme.ButtonBackground)) {
		t.Errorf("buildListHint = %q, want it to use the given theme's own ButtonBackground (%s)", got, colorTag(theme.ButtonBackground))
	}
}

// TestListHintActionAtRunsTheRightKeysOwnAction pins the click-dispatch
// half: a click landing within one key's own column range runs that
// key's own run, not any other's.
func TestListHintActionAtRunsTheRightKeysOwnAction(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	var ran string
	entries := []listHintEntry{
		hintKey("r", "refresh", func(*Root) { ran = "r" }),
		hintKey("Esc", "close", func(*Root) { ran = "Esc" }),
	}
	text, spans := buildListHint(theme, entries)
	_ = text

	if len(spans) != 2 {
		t.Fatalf("len(spans) = %d, want 2", len(spans))
	}
	spans[0].run(nil)
	if ran != "r" {
		t.Errorf("first span ran %q, want it to run \"r\"'s own action", ran)
	}
	spans[1].run(nil)
	if ran != "Esc" {
		t.Errorf("second span ran %q, want it to run \"Esc\"'s own action", ran)
	}
}
