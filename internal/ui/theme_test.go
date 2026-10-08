package ui

import (
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// TestStyleInputLabelBackgroundMatchesFieldBackground pins a real,
// user-reported bug: a labeled field's label used to render on a
// plain black background while its editable area correctly showed
// InputBackground/InputFocusedBackground — because SetLabelStyle's
// own background was never set at all, left at tview's own default.
// See styleInput's own doc comment (theme.go) for the full mechanism.
func TestStyleInputLabelBackgroundMatchesFieldBackground(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	field := tview.NewInputField()
	field.SetLabel("Filter: ")

	styleInput(field, theme, false)
	if _, bg, _ := field.GetLabelStyle().Decompose(); bg != theme.InputBackground {
		t.Errorf("unfocused label background = %v, want InputBackground %v", bg, theme.InputBackground)
	}

	styleInput(field, theme, true)
	if _, bg, _ := field.GetLabelStyle().Decompose(); bg != theme.InputFocusedBackground {
		t.Errorf("focused label background = %v, want InputFocusedBackground %v", bg, theme.InputFocusedBackground)
	}
}

// TestStyleInputLabelBackgroundMatchesFieldBackgroundOnBlankLabel pins
// that a field with no label at all (most callers of styleInput) is
// unaffected either way — GetLabelStyle still reports whatever this
// sets, it just never renders anything since there's no label text.
func TestStyleInputLabelBackgroundMatchesFieldBackgroundOnBlankLabel(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	field := tview.NewInputField()

	styleInput(field, theme, false)
	if fg, bg, _ := field.GetLabelStyle().Decompose(); bg != theme.InputBackground || fg != theme.TextColor {
		t.Errorf("label style = bg:%v fg:%v, want InputBackground/TextColor even with no label text", bg, fg)
	}
}
