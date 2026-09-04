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

	// PanelBackground is the main file-list panel's own background — the
	// "bottom layer" every other panel floats over. Unlike every other
	// field here, this one used to not be a themed value at all: the
	// panel simply drew nothing of its own there, showing whatever the
	// terminal's own default background happened to be. Pinned to an
	// explicit color instead, per the user's own explicit request, in
	// the same "slate" hue family as AccentBackground/EditableBackground
	// (their shared blue-gray cast — the blue channel highest in all
	// three) but darker than either, so the panel/overlay layering reads
	// consistently across terminals regardless of the user's own
	// terminal profile.
	PanelBackground string `json:"panel_background"`
	// AccentBackground colors header bars, dialogs, and other floating
	// chrome — this app's one "everything not otherwise specified"
	// background.
	AccentBackground string `json:"accent_background"`
	// ButtonBackground is every real button's own base look (Cancel/
	// Save/Apply/Select/Find, the filter's regex-mode toggle, ...) —
	// per the user's own explicit request, a lighter turquoise than
	// FocusedBackground, replacing what turned out to be tview's own
	// entirely uncustomized default (a plain blue): a raw tview.Button
	// recomputes its own displayed background from its internal
	// style/activatedStyle fields on every single Draw, discarding
	// whatever a plain SetBackgroundColor call set moments earlier —
	// verified directly against tview's own button.go, not guessed. See
	// internal/ui's own button styling helper for the fix (SetStyle/
	// SetActivatedStyle, not SetBackgroundColor). A focused button
	// switches to FocusedBackground instead, the same "petrol means
	// this currently has real keyboard focus" convention every other
	// focusable element in this app already follows.
	ButtonBackground string `json:"button_background"`
	// FocusedBackground highlights whichever field currently has
	// keyboard focus in the Properties overlay, a list's currently
	// selected item, and the panel's own currently selected row while it
	// (rather than some other panel — Details, a tool window, ...) has
	// real keyboard focus — one single "this is where keyboard input
	// goes right now" color, used consistently everywhere in the app
	// that needs one. Was two separate, always-identical fields
	// (FocusedBackground and SelectionBackground) before the user's own
	// explicit request to merge them: every shipped color scheme,
	// including this app's own default, had already set them to the
	// exact same value, making the distinction real in the type system
	// but never in practice.
	FocusedBackground string `json:"focused_background"`
	// ErrorBackground is the error overlay's background.
	ErrorBackground string `json:"error_background"`
	// DirectoryBackground highlights an entry's own name — not the
	// trailing "/" beside it, nor a symlink's " -> target" arrow, nor the
	// rest of the row — whenever Enter navigates into it: directories,
	// symlinks to directories, mount points, and ".." itself (see
	// rowRef.isDir in internal/ui). So "Downloads/" shows the highlight
	// only on "Downloads", and a directory symlink like "pictures ->
	// /home/jens/Pictures" shows it only on "pictures".
	DirectoryBackground string `json:"directory_background"`

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
	// EntrySymlink colors a working symlink that resolves to a plain
	// file — not a directory symlink, which DirectoryBackground already
	// marks, and not a broken one, which stays EntryError.
	EntrySymlink string `json:"entry_symlink"`
	// EntrySpecial colors the four rarer filesystem entry types a
	// listing can contain: sockets, FIFOs, and character/block devices
	// (see fsops.EntryType) — one color for all four, the same way
	// DirectoryBackground covers several related EntryTypes with one
	// color, since none of them is a file whose content you'd normally
	// open or edit the way EntryNormal/EntryExecutable/EntrySymlink
	// entries are.
	EntrySpecial string `json:"entry_special"`
	// EntryUnreadable colors an entry the invoking user can't actually
	// read — a permission-denied file, or a directory they can't list —
	// per fsops.Entry.Unreadable (see its own doc comment on how that's
	// determined). Deliberately not EntryError's own bright red: that
	// color already means "broken symlink", a different, more specific
	// problem, and reads too close to DirectoryBackground's own gold to
	// stay legible on an unreadable directory's name specifically. A
	// darker red than EntryError, per the user's own explicit request
	// and hex value — but not so dark it disappears against the plain
	// (non-directory) background an unreadable file's own name sits on.
	EntryUnreadable string `json:"entry_unreadable"`
	// EntryArchive colors a file whose name matches a recognized
	// archive/compression extension (see isArchiveName in internal/ui) —
	// a purely visual "you can probably search inside this" cue,
	// deliberately not tied to which formats internal/search's own
	// Include Archives option can actually search (see
	// archiveHighlightExtensions' own doc comment on why that list is
	// broader). Never applied to a directory, even one literally named
	// like an archive (e.g. "backup.tar.gz/"): DirectoryBackground
	// already marks it as a folder, and it isn't actually an archive.
	EntryArchive string `json:"entry_archive"`
	// EntryHidden colors a dotfile/dotdir's name — anything starting
	// with "." except ".." itself, which DirectoryBackground already
	// marks and isn't "hidden" in spirit — a dimmer shade so it recedes
	// a little against the ordinary listing. Checked last, after every
	// other Entry* case (see entryColor in internal/ui): a hidden entry
	// that's also broken/unreadable/special/an archive/a symlink/
	// executable keeps that color instead, since those all say something
	// more specific and more worth noticing than "this is a dotfile".
	EntryHidden string `json:"entry_hidden"`
}

// ResolvedTheme is Theme with every field parsed into a real tcell.Color
// — what internal/ui actually applies to widgets (see Theme.Resolve).
type ResolvedTheme struct {
	PanelBackground     tcell.Color
	AccentBackground    tcell.Color
	ButtonBackground    tcell.Color
	FocusedBackground   tcell.Color
	ErrorBackground     tcell.Color
	DirectoryBackground tcell.Color

	Text               tcell.Color
	EditableBackground tcell.Color
	PlaceholderText    tcell.Color

	EntryNormal     tcell.Color
	EntryExecutable tcell.Color
	EntryError      tcell.Color
	EntrySymlink    tcell.Color
	EntrySpecial    tcell.Color
	EntryUnreadable tcell.Color
	EntryArchive    tcell.Color
	EntryHidden     tcell.Color
}

// DefaultTheme is breakthrough's own built-in scheme: the exact colors
// this app used before color schemes existed, so a fresh install (or a
// scheme file that overrides nothing) looks identical to before it.
func DefaultTheme() Theme {
	return Theme{
		Name: "Default",

		PanelBackground:     "#1c3232",
		AccentBackground:    "darkslategray",
		ButtonBackground:    "lightseagreen",
		FocusedBackground:   "darkcyan",
		ErrorBackground:     "darkred",
		DirectoryBackground: "darkgoldenrod",

		Text:               "white",
		EditableBackground: "slategray",
		PlaceholderText:    "lightgray",

		EntryNormal:     "white",
		EntryExecutable: "green",
		EntryError:      "red",
		EntrySymlink:    "aqua",
		EntrySpecial:    "orange",
		EntryUnreadable: "#ad0000",
		EntryArchive:    "fuchsia",
		EntryHidden:     "dimgray",
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
		PanelBackground:     resolve(t.PanelBackground, def.PanelBackground),
		AccentBackground:    resolve(t.AccentBackground, def.AccentBackground),
		ButtonBackground:    resolve(t.ButtonBackground, def.ButtonBackground),
		FocusedBackground:   resolve(t.FocusedBackground, def.FocusedBackground),
		ErrorBackground:     resolve(t.ErrorBackground, def.ErrorBackground),
		DirectoryBackground: resolve(t.DirectoryBackground, def.DirectoryBackground),

		Text:               resolve(t.Text, def.Text),
		EditableBackground: resolve(t.EditableBackground, def.EditableBackground),
		PlaceholderText:    resolve(t.PlaceholderText, def.PlaceholderText),

		EntryNormal:     resolve(t.EntryNormal, def.EntryNormal),
		EntryExecutable: resolve(t.EntryExecutable, def.EntryExecutable),
		EntryError:      resolve(t.EntryError, def.EntryError),
		EntrySymlink:    resolve(t.EntrySymlink, def.EntrySymlink),
		EntrySpecial:    resolve(t.EntrySpecial, def.EntrySpecial),
		EntryUnreadable: resolve(t.EntryUnreadable, def.EntryUnreadable),
		EntryArchive:    resolve(t.EntryArchive, def.EntryArchive),
		EntryHidden:     resolve(t.EntryHidden, def.EntryHidden),
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
