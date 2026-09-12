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
	AccentBackground        string `json:"accent_background"`
	SurfaceBackground       string `json:"surface_background"`
	PopupBackground         string `json:"popup_background"`
	InputBackground         string `json:"input_background"`
	InputFocusedBackground  string `json:"input_focused_background"`
	InputDisabledBackground string `json:"input_disabled_background"`
	SelectionBackground     string `json:"selection_background"`
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
	ButtonBackground        string `json:"button_background"`
	ButtonFocusedBackground string `json:"button_focused_background"`
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

	// ClipboardCopyBackground/ClipboardCutBackground tint every cell of
	// a row whose absolute path is currently held on the clipboard (see
	// Root.clipboard/clipboardCut in internal/ui) — a full-row
	// background, not just the checkbox glyph a checked selection
	// already changes, so "this is staged for Copy/Cut" stays visible
	// without having to scan the checkbox column specifically. Two
	// visibly different colors, not two shades of the same one, per the
	// user's own explicit request: Cut is the more consequential of the
	// two operations (the original disappears once Paste actually
	// succeeds, rather than staying put the way Copy leaves it), and a
	// plain gray/dimgray pairing read as "the same thing, just lighter"
	// rather than as two genuinely different states worth telling apart
	// at a glance. Both now share the same neutral gray base (0x8a on
	// every channel) with one channel each boosted by the same amount in
	// an opposite direction — Cut's own red (a slightly pinkish-tinted
	// gray, "warmer") versus Copy's own blue (a slightly bluish-tinted
	// gray, "cooler"), a deliberately matched, symmetric pair rather than
	// two independently-picked colors — while both stay close enough to
	// a plain neutral gray to still read as the same *family* of
	// highlight ("administrative state", not another file-type or
	// severity signal — DirectoryBackground's gold, EntryError's red,
	// and so on stay meaningfully distinct from both), immediately
	// distinguishable from each other as their own colors rather than
	// requiring a side-by-side brightness comparison. Being a full-cell
	// background rather than DirectoryBackground's own narrow inline-tag
	// highlight, a clipboard-held directory shows this tint across its
	// whole row instead of the two competing for the same pixels — and,
	// since Panel.setRowCells/paintFixedRowCells also give every tinted
	// cell a matching SetSelectedStyle, this tint stays visible even on
	// whichever row happens to be the table's own current cursor row,
	// focused or not, rather than being invisibly replaced by
	// FocusedBackground/EditableBackground the way it used to be — a
	// real, user-reported gap (a Cut/Copy selection that included the
	// cursor's own row looked "deselected" the moment focus moved
	// elsewhere, even though it never actually stopped being staged).
	ClipboardCopyBackground string `json:"clipboard_copy_background"`
	ClipboardCutBackground  string `json:"clipboard_cut_background"`
	// ClipboardCopyBackgroundInactive/ClipboardCutBackgroundInactive
	// override the color a clipboard-held row shows while it's also the
	// panel's own current cursor row but this panel does NOT have real
	// keyboard focus — left empty (the default for every scheme
	// shipped with this app, including DefaultTheme itself), Resolve
	// derives a sensible one automatically from this scheme's own
	// ClipboardCopyBackground/ClipboardCutBackground (see
	// darkenForInactiveFocus's own doc comment for exactly how), so a
	// scheme file only needs to set these two at all if the computed
	// default doesn't come out right for its own particular tint
	// colors — a scheme with a very dark or already-low-saturation
	// clipboard tint to begin with, say, where darkenForInactiveFocus's
	// own fixed factors might not land as well as they do for this
	// app's own defaults.
	ClipboardCopyBackgroundInactive string `json:"clipboard_copy_background_inactive"`
	ClipboardCutBackgroundInactive  string `json:"clipboard_cut_background_inactive"`

	// Text is this app's one primary foreground color, used almost
	// everywhere text is drawn.
	Text      string `json:"text"`
	TextColor string `json:"text_color"`
	// EditableBackground is Properties' own "editable but not currently
	// focused" field background — every field's plain look before
	// FocusedBackground sets the one under keyboard focus apart from it.
	EditableBackground string `json:"editable_background"`
	// PlaceholderText colors the filter field's placeholder text.
	PlaceholderText string `json:"placeholder_text"`
	MutedTextColor  string `json:"muted_text_color"`
	BorderColor     string `json:"border_color"`

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

	// WarningText and CriticalText are this app's own "stopper colors" —
	// deliberately jarring against the rest of a scheme, for the rare
	// moment something needs to grab attention rather than blend in. Per
	// the user's own explicit request, formalized into the theme rather
	// than staying a hardcoded tcell.ColorOrange/tcell.ColorRed the way
	// they used to be (see diskUsageWarnColor in internal/ui/
	// bottombar.go, their first and — so far — only use: the status
	// bar's own disk/inode usage percentage, orange at 80% or more,
	// red at 90% or more) — a scheme that already leans orange or red
	// elsewhere would otherwise have no way to still make this one
	// specific thing stand out. Foreground-only, the same as
	// Text/PlaceholderText, since both are drawn as plain colored text
	// rather than a colored background field.
	WarningText  string `json:"warning_text"`
	CriticalText string `json:"critical_text"`
}

// ResolvedTheme is Theme with every field parsed into a real tcell.Color
// — what internal/ui actually applies to widgets (see Theme.Resolve).
type ResolvedTheme struct {
	PanelBackground         tcell.Color
	AccentBackground        tcell.Color
	SurfaceBackground       tcell.Color
	PopupBackground         tcell.Color
	InputBackground         tcell.Color
	InputFocusedBackground  tcell.Color
	InputDisabledBackground tcell.Color
	SelectionBackground     tcell.Color
	ButtonBackground        tcell.Color
	ButtonFocusedBackground tcell.Color
	FocusedBackground       tcell.Color
	ErrorBackground         tcell.Color
	DirectoryBackground     tcell.Color

	ClipboardCopyBackground tcell.Color
	ClipboardCutBackground  tcell.Color
	// ClipboardCopyBackgroundInactive/ClipboardCutBackgroundInactive
	// default to a derived shade of ClipboardCopyBackground/
	// ClipboardCutBackground themselves (see darkenForInactiveFocus), but
	// a scheme file can override either explicitly (see Theme's own
	// same-named fields) — used only for a clipboard-held
	// row that's also the panel's own current cursor row while the panel
	// does NOT have real keyboard focus (see internal/ui's own
	// Panel.rowSelectedStyle). Without a color of its own for
	// that specific combination, that row either lost its clipboard tint
	// entirely (the original bug FocusedBackground/EditableBackground's
	// own doc comment covers) or, once that was fixed, became visually
	// identical to every other tinted row in the same selection — a
	// real, user-reported follow-up: with several files selected, the
	// cursor's own position (the one that would matter for a next
	// action) became just as invisible as the color it replaced, only
	// now for the opposite reason. A plain blend with EditableBackground
	// was tried and rejected: EditableBackground ("slategray") already
	// leans toward Copy's own cool/bluish hue, so blending it into Cut's
	// own warm/reddish tint cancels out almost the whole difference that
	// makes Cut recognizable as Cut in the first place, an asymmetry a
	// scale-down avoids entirely by never mixing in a third, unrelated
	// hue at all — see darkenForInactiveFocus's own doc comment.
	ClipboardCopyBackgroundInactive tcell.Color
	ClipboardCutBackgroundInactive  tcell.Color

	Text               tcell.Color
	TextColor          tcell.Color
	EditableBackground tcell.Color
	PlaceholderText    tcell.Color
	MutedTextColor     tcell.Color
	BorderColor        tcell.Color

	EntryNormal     tcell.Color
	EntryExecutable tcell.Color
	EntryError      tcell.Color
	EntrySymlink    tcell.Color
	EntrySpecial    tcell.Color
	EntryUnreadable tcell.Color
	EntryArchive    tcell.Color
	EntryHidden     tcell.Color

	WarningText  tcell.Color
	CriticalText tcell.Color
}

// DefaultTheme is breakthrough's own built-in scheme: the exact colors
// this app used before color schemes existed, so a fresh install (or a
// scheme file that overrides nothing) looks identical to before it.
func DefaultTheme() Theme {
	return Theme{
		Name: "Default",

		PanelBackground:         "#1c3232",
		AccentBackground:        "darkslategray",
		SurfaceBackground:       "darkslategray",
		PopupBackground:         "#263f3f",
		InputBackground:         "slategray",
		InputFocusedBackground:  "darkcyan",
		InputDisabledBackground: "#334747",
		SelectionBackground:     "darkcyan",
		ButtonBackground:        "lightseagreen",
		ButtonFocusedBackground: "darkcyan",
		FocusedBackground:       "darkcyan",
		ErrorBackground:         "darkred",
		DirectoryBackground:     "darkgoldenrod",

		ClipboardCopyBackground: "#8a8aa0", // a slightly bluish-tinted gray — see this field's own doc comment for the matched pair this and ClipboardCutBackground deliberately form
		ClipboardCutBackground:  "#a08a8a", // a slightly pinkish-tinted gray — same base gray, same offset, just on red instead of blue — see this field's own doc comment

		Text:               "white",
		TextColor:          "white",
		EditableBackground: "slategray",
		PlaceholderText:    "lightgray",
		MutedTextColor:     "lightgray",
		BorderColor:        "#537070",

		EntryNormal:     "white",
		EntryExecutable: "green",
		EntryError:      "red",
		EntrySymlink:    "aqua",
		EntrySpecial:    "orange",
		EntryUnreadable: "#ad0000",
		EntryArchive:    "fuchsia",
		EntryHidden:     "dimgray",

		WarningText:  "orange",
		CriticalText: "red",
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
	resolveRole := func(primary, legacy, fallback string) tcell.Color {
		if c := tcell.GetColor(primary); c != tcell.ColorDefault {
			return c
		}
		if c := tcell.GetColor(legacy); c != tcell.ColorDefault {
			return c
		}
		return tcell.GetColor(fallback)
	}
	copyBg := resolve(t.ClipboardCopyBackground, def.ClipboardCopyBackground)
	cutBg := resolve(t.ClipboardCutBackground, def.ClipboardCutBackground)
	// The Inactive pair's own "fallback" isn't a fixed default-theme
	// string the way every other field's is — it's computed from
	// *this* scheme's own copyBg/cutBg above (already resolved,
	// including any override), so a scheme that also overrides
	// ClipboardCopyBackground/ClipboardCutBackground themselves still
	// gets an Inactive variant actually derived from its own colors,
	// not DefaultTheme's. resolve itself can't express that (it only
	// ever falls back to a fixed string), so this is inlined directly
	// rather than forcing that shared helper to special-case it.
	copyBgInactive := darkenForInactiveFocus(copyBg)
	if c := tcell.GetColor(t.ClipboardCopyBackgroundInactive); c != tcell.ColorDefault {
		copyBgInactive = c
	}
	cutBgInactive := darkenForInactiveFocus(cutBg)
	if c := tcell.GetColor(t.ClipboardCutBackgroundInactive); c != tcell.ColorDefault {
		cutBgInactive = c
	}
	return ResolvedTheme{
		PanelBackground:         resolve(t.PanelBackground, def.PanelBackground),
		AccentBackground:        resolve(t.AccentBackground, def.AccentBackground),
		SurfaceBackground:       resolveRole(t.SurfaceBackground, t.AccentBackground, def.SurfaceBackground),
		PopupBackground:         resolveRole(t.PopupBackground, t.AccentBackground, def.PopupBackground),
		InputBackground:         resolveRole(t.InputBackground, t.EditableBackground, def.InputBackground),
		InputFocusedBackground:  resolveRole(t.InputFocusedBackground, t.FocusedBackground, def.InputFocusedBackground),
		InputDisabledBackground: resolve(t.InputDisabledBackground, def.InputDisabledBackground),
		SelectionBackground:     resolveRole(t.SelectionBackground, t.FocusedBackground, def.SelectionBackground),
		ButtonBackground:        resolve(t.ButtonBackground, def.ButtonBackground),
		ButtonFocusedBackground: resolveRole(t.ButtonFocusedBackground, t.FocusedBackground, def.ButtonFocusedBackground),
		FocusedBackground:       resolve(t.FocusedBackground, def.FocusedBackground),
		ErrorBackground:         resolve(t.ErrorBackground, def.ErrorBackground),
		DirectoryBackground:     resolve(t.DirectoryBackground, def.DirectoryBackground),

		ClipboardCopyBackground:         copyBg,
		ClipboardCutBackground:          cutBg,
		ClipboardCopyBackgroundInactive: copyBgInactive,
		ClipboardCutBackgroundInactive:  cutBgInactive,

		Text:               resolve(t.Text, def.Text),
		TextColor:          resolveRole(t.TextColor, t.Text, def.TextColor),
		EditableBackground: resolve(t.EditableBackground, def.EditableBackground),
		PlaceholderText:    resolve(t.PlaceholderText, def.PlaceholderText),
		MutedTextColor:     resolveRole(t.MutedTextColor, t.PlaceholderText, def.MutedTextColor),
		BorderColor:        resolve(t.BorderColor, def.BorderColor),

		EntryNormal:     resolve(t.EntryNormal, def.EntryNormal),
		EntryExecutable: resolve(t.EntryExecutable, def.EntryExecutable),
		EntryError:      resolve(t.EntryError, def.EntryError),
		EntrySymlink:    resolve(t.EntrySymlink, def.EntrySymlink),
		EntrySpecial:    resolve(t.EntrySpecial, def.EntrySpecial),
		EntryUnreadable: resolve(t.EntryUnreadable, def.EntryUnreadable),
		EntryArchive:    resolve(t.EntryArchive, def.EntryArchive),
		EntryHidden:     resolve(t.EntryHidden, def.EntryHidden),

		WarningText:  resolve(t.WarningText, def.WarningText),
		CriticalText: resolve(t.CriticalText, def.CriticalText),
	}
}

// inactiveFocusBaseDarkenFactor is how much darkenForInactiveFocus
// dims c's own shared, achromatic base — see its own doc comment for
// what that means and why only the base, not the whole color, is
// scaled by this. 1.0 would mean no dimming at all; the lower this is,
// the darker the shared base gets.
const inactiveFocusBaseDarkenFactor = 0.8

// inactiveFocusExcessBoost is how much darkenForInactiveFocus
// multiplies c's own per-channel excess above its shared base (see its
// own doc comment) — deliberately *greater* than 1.0: a human eye
// reads a given RGB difference as less colorful the darker the two
// colors it's between are (this app's own default clipboard tints are
// already a deliberately subtle cast to begin with — a 22-point spread
// on a bright base — see ClipboardCopyBackground/ClipboardCutBackground's
// own doc comment), so the same absolute spread that reads as a clear,
// if subtle, tint at full brightness reads as barely-there on a dimmer
// base — a real, user-reported outcome of the very first fix here,
// which preserved the spread exactly but left it that dim regardless.
// Boosting it compensates.
const inactiveFocusExcessBoost = 2.0

// darkenForInactiveFocus derives ClipboardCopyBackgroundInactive/
// ClipboardCutBackgroundInactive from their own full-brightness
// counterparts — dimmer, but still clearly recognizable as the same
// hue, not a shade of plain gray.
//
// Two rejected attempts before this one, in order, each addressing a
// real, specific gap the previous one left open rather than a
// hypothetical concern:
//
//  1. Scaling every RGB channel down by the same factor: preserves
//     saturation (the max/min ratio is unchanged by a uniform scale),
//     but shrinks the absolute difference between channels by that
//     same factor — and it's that absolute difference a viewer's eye
//     actually picks up against a dim terminal background. The result
//     read as plain dark gray.
//  2. Decomposing c into its shared, achromatic base (min(R, G, B))
//     and each channel's own excess above it, then dimming only the
//     base while leaving the excess exactly as it was: fixed the
//     shrinking spread from attempt 1, but the result was reported as
//     both still too dark overall and still barely colored — the
//     unchanged spread, it turns out, isn't enough on its own once the
//     base it sits on is this much dimmer; the same raw RGB gap simply
//     reads as less colorful at lower brightness (see
//     inactiveFocusExcessBoost's own doc comment).
//
// This version keeps attempt 2's own decomposition, but tunes both
// halves in the two directions actually reported as needed: the base
// is dimmed less (inactiveFocusBaseDarkenFactor raised, so the overall
// result is noticeably brighter — "too dark" was the direct
// complaint), and the excess is deliberately amplified beyond its own
// original magnitude (inactiveFocusExcessBoost), rather than merely
// preserved, to compensate for exactly the darkness-dependent
// perceived-colorfulness loss attempt 2 didn't account for. Clamped to
// the valid 0-255 range per channel either way — boosting the excess
// this much could otherwise push a channel out of range for some other
// scheme's own, more saturated clipboard colors, even though none of
// this app's own default ones come close.
func darkenForInactiveFocus(c tcell.Color) tcell.Color {
	r, g, b := c.RGB()
	base := r
	if g < base {
		base = g
	}
	if b < base {
		base = b
	}
	dimmedBase := float64(base) * inactiveFocusBaseDarkenFactor
	clamp := func(v int32) int32 {
		excess := float64(v-base) * inactiveFocusExcessBoost
		result := int32(dimmedBase + excess)
		switch {
		case result < 0:
			return 0
		case result > 255:
			return 255
		default:
			return result
		}
	}
	return tcell.NewRGBColor(clamp(r), clamp(g), clamp(b))
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
