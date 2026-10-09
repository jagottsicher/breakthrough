package ui

import (
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// TestStyleInputLabelBackgroundStaysInputBackgroundRegardlessOfFocus
// pins two real, user-reported fixes layered on the same mechanism:
// first, a labeled field's label used to render on a plain black
// background while its editable area correctly showed InputBackground/
// InputFocusedBackground — because SetLabelStyle's own background was
// never set at all, left at tview's own default. Later, once the label
// started tracking the field's own focused color, a second report
// followed: a label that turns the same color as the field on focus
// made the two harder to tell apart, when the point of a focus color
// is to stand out against something that stays put — so the label now
// always stays InputBackground, only the field/placeholder still react
// to focused. See styleInput's own doc comment (theme.go) for the full
// mechanism.
func TestStyleInputLabelBackgroundStaysInputBackgroundRegardlessOfFocus(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	field := tview.NewInputField()
	field.SetLabel("Filter: ")

	styleInput(field, theme, false)
	if _, bg, _ := field.GetLabelStyle().Decompose(); bg != theme.InputBackground {
		t.Errorf("unfocused label background = %v, want InputBackground %v", bg, theme.InputBackground)
	}

	styleInput(field, theme, true)
	if _, bg, _ := field.GetLabelStyle().Decompose(); bg != theme.InputBackground {
		t.Errorf("focused label background = %v, want it to stay InputBackground %v, not track the field's own focused color", bg, theme.InputBackground)
	}
	if _, bg, _ := field.GetFieldStyle().Decompose(); bg != theme.InputFocusedBackground {
		t.Errorf("focused field background = %v, want InputFocusedBackground %v — only the label stays put, not the field itself", bg, theme.InputFocusedBackground)
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
