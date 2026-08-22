package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// Theme is one color scheme's on-disk JSON representation — every field
// a color string tcell.GetColor accepts: a W3C color name (e.g.
// "darkslategray") or a "#rrggbb" hex value. A scheme file only needs to
// set the fields it actually wants to change; anything left empty, or
// set to something tcell.GetColor doesn't recognize, falls back to
// DefaultTheme's own value for that field (see Resolve).
type Theme struct {
	// Name is the scheme's display label, shown in the Settings picker
	// (see internal/ui). It's independent of the JSON file's own name —
	// the filename stem is what Settings.ColorScheme actually references
	// (see LoadColorSchemes) — so Name can be duplicated or contain
	// spaces/punctuation freely.
	Name string `json:"name"`

	// AccentBackground colors header bars, dialogs, buttons, and other
	// floating chrome — this app's one "everything not otherwise
	// specified" background.
	AccentBackground string `json:"accent_background"`
	// FocusedBackground highlights whichever field currently has
	// keyboard focus in the Properties overlay.
	FocusedBackground string `json:"focused_background"`
	// ErrorBackground is the error overlay's background.
	ErrorBackground string `json:"error_background"`
	// SelectionBackground highlights the panel's currently selected row.
	SelectionBackground string `json:"selection_background"`

	// Text is this app's one primary foreground color, used almost
	// everywhere text is drawn.
	Text string `json:"text"`
	// EditableBackground is Properties' own "editable but not currently
	// focused" field background — every field's plain look before
	// FocusedBackground sets the one under keyboard focus apart from it.
	EditableBackground string `json:"editable_background"`
	// PlaceholderText colors the filter field's placeholder text.
	PlaceholderText string `json:"placeholder_text"`

	// EntryNormal, EntryExecutable, and EntryError color a panel row's
	// name by what kind of entry it is (see entryColor in internal/ui).
	EntryNormal     string `json:"entry_normal"`
	EntryExecutable string `json:"entry_executable"`
	EntryError      string `json:"entry_error"`
}

// ResolvedTheme is Theme with every field parsed into a real tcell.Color
// — what internal/ui actually applies to widgets (see Theme.Resolve).
type ResolvedTheme struct {
	AccentBackground    tcell.Color
	FocusedBackground   tcell.Color
	ErrorBackground     tcell.Color
	SelectionBackground tcell.Color

	Text               tcell.Color
	EditableBackground tcell.Color
	PlaceholderText    tcell.Color

	EntryNormal     tcell.Color
	EntryExecutable tcell.Color
	EntryError      tcell.Color
}

// DefaultTheme is breakthrough's own built-in scheme: the exact colors
// this app used before color schemes existed, so a fresh install (or a
// scheme file that overrides nothing) looks identical to before it.
func DefaultTheme() Theme {
	return Theme{
		Name: "Default",

		AccentBackground:    "darkslategray",
		FocusedBackground:   "darkcyan",
		ErrorBackground:     "darkred",
		SelectionBackground: "darkcyan",

		Text:               "white",
		EditableBackground: "slategray",
		PlaceholderText:    "lightgray",

		EntryNormal:     "white",
		EntryExecutable: "green",
		EntryError:      "red",
	}
}

// Resolve parses every field via tcell.GetColor, falling back to
// DefaultTheme's own value field-by-field wherever t's own value is
// empty or unrecognized. tcell.GetColor itself returns
// tcell.ColorDefault — the terminal's own default color — for anything
// it doesn't recognize; treating that the same as "not set" avoids a
// typo'd color silently rendering as whatever the terminal happens to
// default to (likely invisible against this app's own explicit
// backgrounds) instead of falling back cleanly.
func (t Theme) Resolve() ResolvedTheme {
	def := DefaultTheme()
	resolve := func(value, fallback string) tcell.Color {
		if c := tcell.GetColor(value); c != tcell.ColorDefault {
			return c
		}
		return tcell.GetColor(fallback)
	}
	return ResolvedTheme{
		AccentBackground:    resolve(t.AccentBackground, def.AccentBackground),
		FocusedBackground:   resolve(t.FocusedBackground, def.FocusedBackground),
		ErrorBackground:     resolve(t.ErrorBackground, def.ErrorBackground),
		SelectionBackground: resolve(t.SelectionBackground, def.SelectionBackground),

		Text:               resolve(t.Text, def.Text),
		EditableBackground: resolve(t.EditableBackground, def.EditableBackground),
		PlaceholderText:    resolve(t.PlaceholderText, def.PlaceholderText),

		EntryNormal:     resolve(t.EntryNormal, def.EntryNormal),
		EntryExecutable: resolve(t.EntryExecutable, def.EntryExecutable),
		EntryError:      resolve(t.EntryError, def.EntryError),
	}
}

// NamedTheme pairs a Theme with the stable slug used to select it from
// Settings.ColorScheme (see LoadColorSchemes) — the JSON filename stem,
// not Theme.Name (see its own doc comment on why those are independent).
type NamedTheme struct {
	Slug  string
	Theme Theme
}

// LoadColorSchemes scans systemDir and userDir (see
// SystemColorSchemeDir/UserColorSchemeDir — either may be "" or not
// exist, contributing nothing then) for "*.json" scheme files, keyed by
// filename stem: a user file replaces a system one of the same stem, the
// same precedence Load gives user config values over system ones.
// "default" (DefaultTheme, see its own doc comment) is always present in
// the result unless a file named default.json overrides it. The result
// is sorted by slug, so callers (e.g. the Settings picker) get a stable
// order across runs.
func LoadColorSchemes(systemDir, userDir string) []NamedTheme {
	bySlug := map[string]Theme{"default": DefaultTheme()}

	load := func(dir string) {
		if dir == "" {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			var th Theme
			if err := json.Unmarshal(data, &th); err != nil {
				continue
			}
			slug := strings.TrimSuffix(e.Name(), ".json")
			if th.Name == "" {
				th.Name = slug
			}
			bySlug[slug] = th
		}
	}
	load(systemDir)
	load(userDir)

	slugs := make([]string, 0, len(bySlug))
	for slug := range bySlug {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	out := make([]NamedTheme, len(slugs))
	for i, slug := range slugs {
		out[i] = NamedTheme{Slug: slug, Theme: bySlug[slug]}
	}
	return out
}

// FindColorScheme returns the theme in schemes whose Slug is slug, or
// DefaultTheme (see its own doc comment) if none matches — the same
// forgiving fallback as an unresolved color field, e.g. after a scheme
// file the config still references has been deleted.
func FindColorScheme(schemes []NamedTheme, slug string) Theme {
	for _, s := range schemes {
		if s.Slug == slug {
			return s.Theme
		}
	}
	return DefaultTheme()
}
