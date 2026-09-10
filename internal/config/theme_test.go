package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestDefaultThemeResolvesAllFieldsToValidColors(t *testing.T) {
	resolved := DefaultTheme().Resolve()
	fields := map[string]tcell.Color{
		"PanelBackground":         resolved.PanelBackground,
		"ButtonBackground":        resolved.ButtonBackground,
		"AccentBackground":        resolved.AccentBackground,
		"FocusedBackground":       resolved.FocusedBackground,
		"ErrorBackground":         resolved.ErrorBackground,
		"ClipboardCopyBackground": resolved.ClipboardCopyBackground,
		"ClipboardCutBackground":  resolved.ClipboardCutBackground,
		"Text":                    resolved.Text,
		"EditableBackground":      resolved.EditableBackground,
		"PlaceholderText":         resolved.PlaceholderText,
		"EntryNormal":             resolved.EntryNormal,
		"EntryExecutable":         resolved.EntryExecutable,
		"EntryError":              resolved.EntryError,
	}
	for name, c := range fields {
		if c == tcell.ColorDefault {
			t.Errorf("DefaultTheme's %s resolved to ColorDefault, want a real color", name)
		}
	}
}

func TestThemeResolveFallsBackPerFieldOnEmptyOrInvalidValue(t *testing.T) {
	th := Theme{
		AccentBackground: "",                      // empty: falls back
		Text:             "not-a-real-color-name", // unrecognized: falls back
		ErrorBackground:  "maroon",                // valid override: kept
	}
	resolved := th.Resolve()
	def := DefaultTheme().Resolve()

	if resolved.AccentBackground != def.AccentBackground {
		t.Errorf("empty AccentBackground should fall back to the default, got %v want %v", resolved.AccentBackground, def.AccentBackground)
	}
	if resolved.Text != def.Text {
		t.Errorf("unrecognized Text should fall back to the default, got %v want %v", resolved.Text, def.Text)
	}
	if resolved.ErrorBackground != tcell.GetColor("maroon") {
		t.Errorf("a valid override should be kept, got %v want %v", resolved.ErrorBackground, tcell.GetColor("maroon"))
	}
}

// spread reports the difference between c's own brightest and dimmest
// RGB channel — the absolute magnitude a viewer's eye actually reads
// as "this has a color cast", independent of how dark or bright c is
// overall.
func spread(c tcell.Color) int32 {
	r, g, b := c.RGB()
	max, min := r, r
	for _, v := range []int32{g, b} {
		if v > max {
			max = v
		}
		if v < min {
			min = v
		}
	}
	return max - min
}

// TestDarkenForInactiveFocusBoostsSpreadWhileStillDarker pins the
// combined fix for two real, user-reported outcomes of the two earlier,
// rejected attempts in turn (see darkenForInactiveFocus's own doc
// comment for the full history): scaling every RGB channel down by the
// same factor shrank the absolute spread between channels right along
// with the darkening, reading as plain dark gray; preserving that
// spread exactly, instead of shrinking it, still read as both too dark
// and barely colored, because the same raw spread reads as less
// colorful the darker the two colors around it are. The final version
// must therefore end up BOTH noticeably darker overall than the
// full-brightness color AND with a wider spread than it started
// with — not merely an unchanged one — to compensate for exactly that
// darkness-dependent perceived-colorfulness loss. Checked for both of
// this app's own clipboard colors, rather than relying on eyeballing
// one specific computed hex value.
func TestDarkenForInactiveFocusBoostsSpreadWhileStillDarker(t *testing.T) {
	def := DefaultTheme().Resolve()
	for name, pair := range map[string][2]tcell.Color{
		"Copy": {def.ClipboardCopyBackground, def.ClipboardCopyBackgroundInactive},
		"Cut":  {def.ClipboardCutBackground, def.ClipboardCutBackgroundInactive},
	} {
		bright, dim := pair[0], pair[1]
		if got, want := spread(dim), spread(bright); got <= want {
			t.Errorf("%s: Inactive spread = %d, want more than the full-brightness color's own %d — darkening must boost it, not just preserve or shrink it", name, got, want)
		}
		brightR, brightG, brightB := bright.RGB()
		dimR, dimG, dimB := dim.RGB()
		if dimR+dimG+dimB >= brightR+brightG+brightB {
			t.Errorf("%s: Inactive (%d,%d,%d) is not darker overall than the full-brightness color (%d,%d,%d)", name, dimR, dimG, dimB, brightR, brightG, brightB)
		}
	}
}

func TestThemeResolveAcceptsHexColors(t *testing.T) {
	th := Theme{AccentBackground: "#112233"}
	resolved := th.Resolve()
	if want := tcell.GetColor("#112233"); resolved.AccentBackground != want {
		t.Errorf("AccentBackground = %v, want %v", resolved.AccentBackground, want)
	}
}

func writeScheme(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadColorSchemesAlwaysIncludesDefault(t *testing.T) {
	schemes := LoadColorSchemes("", "")
	found := false
	for _, s := range schemes {
		if s.Slug == "default" {
			found = true
			if s.Theme != DefaultTheme() {
				t.Errorf("default scheme = %+v, want DefaultTheme()", s.Theme)
			}
		}
	}
	if !found {
		t.Error("LoadColorSchemes should always include a \"default\" slug")
	}
}

func TestLoadColorSchemesUserOverridesSystemBySlug(t *testing.T) {
	base := t.TempDir()
	systemDir := filepath.Join(base, "system", "colorschemes")
	userDir := filepath.Join(base, "user", "colorschemes")
	writeScheme(t, systemDir, "solarized.json", `{"name": "Solarized (system)"}`)
	writeScheme(t, userDir, "solarized.json", `{"name": "Solarized (user)"}`)

	schemes := LoadColorSchemes(systemDir, userDir)
	th := FindColorScheme(schemes, "solarized")
	if th.Name != "Solarized (user)" {
		t.Errorf("Name = %q, want the user tier's version to win", th.Name)
	}
}

func TestLoadColorSchemesSkipsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	writeScheme(t, dir, "broken.json", `{not valid json`)

	schemes := LoadColorSchemes(dir, "")
	for _, s := range schemes {
		if s.Slug == "broken" {
			t.Errorf("invalid JSON should have been skipped, got %+v", s)
		}
	}
}

func TestLoadColorSchemesIgnoresNonJSONFiles(t *testing.T) {
	dir := t.TempDir()
	writeScheme(t, dir, "README.md", "not a scheme")

	schemes := LoadColorSchemes(dir, "")
	for _, s := range schemes {
		if s.Slug == "README" || s.Slug == "README.md" {
			t.Errorf("non-.json file should have been ignored, got %+v", s)
		}
	}
}

func TestLoadColorSchemesFillsNameFromSlugWhenMissing(t *testing.T) {
	dir := t.TempDir()
	writeScheme(t, dir, "nameless.json", `{"accent_background": "red"}`)

	schemes := LoadColorSchemes(dir, "")
	th := FindColorScheme(schemes, "nameless")
	if th.Name != "nameless" {
		t.Errorf("Name = %q, want the filename stem \"nameless\" used as a fallback display name", th.Name)
	}
}

func TestFindColorSchemeFallsBackToDefaultWhenSlugMissing(t *testing.T) {
	schemes := LoadColorSchemes("", "")
	th := FindColorScheme(schemes, "does-not-exist")
	if th != DefaultTheme() {
		t.Errorf("FindColorScheme for a missing slug = %+v, want DefaultTheme()", th)
	}
}
