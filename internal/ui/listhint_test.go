package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// TestBuildListHintPadsASingleRuneKey pins highlightKey's own padded
// style for a single-rune key: one space either side of it inside the
// colored background, the label following with no space of its own.
func TestBuildListHintPadsASingleRuneKey(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	keyBG := colorTag(theme.ButtonBackground)

	got := buildListHint(theme, []listHintEntry{{"r", "refresh"}})

	want := fmt.Sprintf(" [:%s:] r [-:-:-]refresh ", keyBG)
	if got != want {
		t.Errorf("buildListHint = %q, want %q", got, want)
	}
}

// TestBuildListHintWrapsAMultiCharKeyDirectly pins chordHintBar's own
// "Esc" treatment, generalized to any key text longer than one rune —
// no padding space, no space before the label.
func TestBuildListHintWrapsAMultiCharKeyDirectly(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	keyBG := colorTag(theme.ButtonBackground)

	got := buildListHint(theme, []listHintEntry{{"↑/↓", "move"}})

	want := fmt.Sprintf(" [:%s:]↑/↓[-:-:-]move ", keyBG)
	if got != want {
		t.Errorf("buildListHint = %q, want %q", got, want)
	}
}

// TestBuildListHintSeparatesEntriesWithAPlainSpaceExceptBeforeEsc pins
// the same block-separator convention buildButtonBar's own blockSep and
// chordHintBar's own "Esccancel" already use: every entry gets a plain
// space before it, except one whose key is exactly "Esc", which gets
// the heavier " │ " instead.
func TestBuildListHintSeparatesEntriesWithAPlainSpaceExceptBeforeEsc(t *testing.T) {
	theme := config.DefaultTheme().Resolve()

	got := buildListHint(theme, []listHintEntry{
		{"r", "refresh"},
		{"t", "simulate"},
		{"Esc", "close"},
	})

	if !strings.Contains(got, "]refresh [") {
		t.Errorf("buildListHint = %q, want a plain space between refresh and the next key", got)
	}
	if !strings.Contains(got, "]simulate │ [") {
		t.Errorf("buildListHint = %q, want the heavier \" │ \" separator right before Esc", got)
	}
}

// TestBuildListHintReflectsTheGivenTheme pins that the key background
// actually comes from theme.ButtonBackground, not a fixed color — a
// live scheme switch (see applyTheme) must recolor these the same way
// it already does buildButtonBar/chordHintBar's own legend.
func TestBuildListHintReflectsTheGivenTheme(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	theme.ButtonBackground = 0x123456

	got := buildListHint(theme, []listHintEntry{{"r", "refresh"}})

	if !strings.Contains(got, colorTag(theme.ButtonBackground)) {
		t.Errorf("buildListHint = %q, want it to use the given theme's own ButtonBackground (%s)", got, colorTag(theme.ButtonBackground))
	}
}
