package config

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestExampleColorSchemesParseAndResolve loads the two example scheme
// files shipped in examples/colorschemes (see the README's own "Color
// schemes" section) through the real LoadColorSchemes path, and checks
// every field they set actually resolves to a real color rather than
// silently falling back to DefaultTheme's own value — the way a typo'd
// color name or a JSON syntax error would, neither of which this test
// should let slip past unnoticed into a shipped example.
func TestExampleColorSchemesParseAndResolve(t *testing.T) {
	schemes := LoadColorSchemes("", "../../examples/colorschemes")

	want := map[string]string{
		"solarized": "Solarized Dark",
		"light":     "Light",
	}
	for slug, name := range want {
		th, ok := findScheme(schemes, slug)
		if !ok {
			t.Errorf("example scheme %q not found via LoadColorSchemes — is examples/colorschemes/%s.json missing or misnamed?", slug, slug)
			continue
		}
		if th.Name != name {
			t.Errorf("scheme %q: Name = %q, want %q", slug, th.Name, name)
		}

		resolved := th.Resolve()
		fields := map[string]tcell.Color{
			"AccentBackground":    resolved.AccentBackground,
			"FocusedBackground":   resolved.FocusedBackground,
			"ErrorBackground":     resolved.ErrorBackground,
			"SelectionBackground": resolved.SelectionBackground,
			"Text":                resolved.Text,
			"EditableBackground":  resolved.EditableBackground,
			"PlaceholderText":     resolved.PlaceholderText,
			"EntryNormal":         resolved.EntryNormal,
			"EntryExecutable":     resolved.EntryExecutable,
			"EntryError":          resolved.EntryError,
		}
		defaults := DefaultTheme().Resolve()
		defaultFields := map[string]tcell.Color{
			"AccentBackground":    defaults.AccentBackground,
			"FocusedBackground":   defaults.FocusedBackground,
			"ErrorBackground":     defaults.ErrorBackground,
			"SelectionBackground": defaults.SelectionBackground,
			"Text":                defaults.Text,
			"EditableBackground":  defaults.EditableBackground,
			"PlaceholderText":     defaults.PlaceholderText,
			"EntryNormal":         defaults.EntryNormal,
			"EntryExecutable":     defaults.EntryExecutable,
			"EntryError":          defaults.EntryError,
		}
		for field, c := range fields {
			// Every field in both example files sets its own value,
			// deliberately different from the default scheme's — so
			// resolving to the same color as DefaultTheme's own would
			// mean this field silently fell back instead of using what
			// the JSON actually specified (a typo'd color name, most
			// likely — tcell.GetColor returns ColorDefault for those,
			// which Resolve then papers over with the default value).
			if c == defaultFields[field] {
				t.Errorf("scheme %q: %s resolved to the same color as DefaultTheme (%v) — likely fell back instead of using the JSON's own value", slug, field, c)
			}
		}
	}
}

func findScheme(schemes []NamedTheme, slug string) (Theme, bool) {
	for _, s := range schemes {
		if s.Slug == slug {
			return s.Theme, true
		}
	}
	return Theme{}, false
}
