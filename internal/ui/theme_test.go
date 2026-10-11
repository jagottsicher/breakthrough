package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
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

// TestApplyTviewDefaultThemeMatchesActiveTheme pins that tview's own
// package-level Styles fallback — what any primitive renders in in place
// of whatever it never had set explicitly (a plain tview.NewBox() nobody
// styled, or a forgotten SetBackgroundColor/SetBorderColor call) — always
// tracks the active breakthrough theme, rather than staying at tview's
// own built-in black/blue/white default regardless of which color scheme
// is selected.
func TestApplyTviewDefaultThemeMatchesActiveTheme(t *testing.T) {
	saved := tview.Styles
	t.Cleanup(func() { tview.Styles = saved })

	theme := config.DefaultTheme().Resolve()
	applyTviewDefaultTheme(theme)

	want := map[string]struct {
		got, want tcell.Color
	}{
		"PrimitiveBackgroundColor":    {tview.Styles.PrimitiveBackgroundColor, theme.SurfaceBackground},
		"ContrastBackgroundColor":     {tview.Styles.ContrastBackgroundColor, theme.AccentBackground},
		"MoreContrastBackgroundColor": {tview.Styles.MoreContrastBackgroundColor, theme.InputFocusedBackground},
		"BorderColor":                 {tview.Styles.BorderColor, theme.BorderColor},
		"TitleColor":                  {tview.Styles.TitleColor, theme.TextColor},
		"GraphicsColor":               {tview.Styles.GraphicsColor, theme.BorderColor},
		"PrimaryTextColor":            {tview.Styles.PrimaryTextColor, theme.TextColor},
		"SecondaryTextColor":          {tview.Styles.SecondaryTextColor, theme.MutedTextColor},
		"TertiaryTextColor":           {tview.Styles.TertiaryTextColor, theme.MutedTextColor},
		"InverseTextColor":            {tview.Styles.InverseTextColor, theme.Text},
		"ContrastSecondaryTextColor":  {tview.Styles.ContrastSecondaryTextColor, theme.Text},
	}
	for name, pair := range want {
		if pair.got != pair.want {
			t.Errorf("tview.Styles.%s = %v, want %v (from theme)", name, pair.got, pair.want)
		}
	}
}
