package ui

import (
	"strconv"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// The Options screen's own catalogue: which settings it offers, how
// they're grouped into categories, and — per setting — how to read the
// value currently in force and how to put a new one into effect.
//
// Deliberately a data table rather than hand-built UI per setting: the
// screen itself (see optionsscreen.go) then knows nothing about any
// individual setting, so adding one is a single entry here rather than
// a new widget, a new click zone and a new persistence call scattered
// across three places.
//
// Every apply below goes through whatever path already existed for that
// setting — setShowHidden, applyColorScheme, and so on — rather than
// writing to r.settings and persisting directly. That's the rule that
// keeps the Options screen and the context menu's own "Globals" toggles
// from drifting into doing subtly different things to the same value.

// optionChoice is one selectable value for a KindEnum setting: the
// literal written to the config file, plus how it reads on screen.
type optionChoice struct {
	value string
	label string
}

// optionSpec is one editable setting as the Options screen sees it.
//
// key ties it back to config.SettingDocs (the canonical list of
// recognized keys) — kind, the default, and the one-line summary all
// come from there rather than being repeated here, so this table only
// carries what's genuinely UI-side: the display label, the longer help
// text, how to read and write the live value, and the enum choices
// where the set isn't fixed at compile time.
type optionSpec struct {
	key   string
	label string

	// help is the full explanation the info popup shows (see
	// Root.showOptionInfo). Longer and more concrete than
	// config.SettingDoc.Summary, which has to stay a single line
	// because it's also a comment in the generated config file.
	help string

	// restartHint marks a setting whose new value can't take effect
	// until the next start. Shown next to the value, because a setting
	// that silently appears to do nothing is worse than one that says
	// when it will.
	restartHint bool

	// value reads what's currently in force, as the literal that would
	// be written to a config file. Read from the live application state
	// (the panel's own flags, r.settings) rather than from disk — those
	// are what the user is actually looking at.
	value func(*Root) string

	// apply puts a new value into effect and persists it. Takes the same
	// literal form value returns.
	apply func(*Root, string)

	// choices lists the selectable values for a KindEnum setting, or is
	// nil for every other kind. A function rather than a fixed slice
	// because color_scheme's own set depends on which scheme files are
	// present right now (see Root.colorSchemes).
	choices func(*Root) []optionChoice
}

// doc returns the config-package metadata for this setting — its kind,
// built-in default and one-line summary. The second result is false for
// a key config.SettingDocs doesn't know, which
// TestOptionCatalogMatchesSettingDocs pins as impossible.
func (o optionSpec) doc() (config.SettingDoc, bool) {
	return config.FindSettingDoc(o.key)
}

// optionCategory is one entry in the Options screen's own left-hand
// list, and the group of settings its right-hand pane then shows.
type optionCategory struct {
	name    string
	options []optionSpec
}

// boolOption builds the common case: a true/false setting read from and
// written through a pair of accessors.
func boolOption(key, label, help string, restartHint bool, get func(*Root) bool, set func(*Root, bool)) optionSpec {
	return optionSpec{
		key:         key,
		label:       label,
		help:        help,
		restartHint: restartHint,
		value:       func(r *Root) string { return strconv.FormatBool(get(r)) },
		apply: func(r *Root, v string) {
			// A value that doesn't parse can only come from a
			// hand-edited config file, never from this screen's own
			// toggle — ignore it rather than guessing, and leave the
			// current value alone.
			b, err := strconv.ParseBool(v)
			if err != nil {
				return
			}
			set(r, b)
		},
	}
}

// intOption builds a whole-number setting. min/max bound what the input
// dialog accepts (see Root.editOptionValue) — every integer setting here
// has a range outside which it would be meaningless rather than merely
// unusual.
func intOption(key, label, help string, get func(*Root) int, set func(*Root, int)) optionSpec {
	return optionSpec{
		key:   key,
		label: label,
		help:  help,
		value: func(r *Root) string { return strconv.Itoa(get(r)) },
		apply: func(r *Root, v string) {
			n, err := strconv.Atoi(v)
			if err != nil {
				return // see boolOption's own equivalent guard
			}
			set(r, n)
		},
	}
}

// optionCategories is the whole catalogue, in the order the Options
// screen lists them.
//
// A function rather than a package-level var because several entries
// close over per-Root state, and because color_scheme's own choices
// have to be re-derived whenever the available schemes change (see
// Root.reloadColorSchemes).
//
// "language" is deliberately absent: the parser accepts it, but nothing
// reads it (see config.SettingDocs, which marks it unimplemented) — a
// control that visibly does nothing is worse than no control at all. It
// still appears, clearly marked, in the generated config file, which
// documents the file format rather than what's wired up.
func optionCategories() []optionCategory {
	return []optionCategory{
		{
			name: "Appearance",
			options: []optionSpec{
				{
					key:   "color_scheme",
					label: "Color scheme",
					help: "Which color scheme the whole application uses.\n\n" +
						"Schemes are JSON files in colorschemes/ under either config directory — " +
						"your own (~/.config/breakthrough) or the system-wide one (/etc/breakthrough). " +
						"A scheme only needs to set the colors it wants to change; anything it leaves " +
						"out falls back to the built-in default.\n\n" +
						"Use \"New color scheme\" below to copy the current one and open it in your editor.",
					value: func(r *Root) string { return r.settings.ColorScheme },
					apply: func(r *Root, v string) { r.applyColorScheme(v) },
					choices: func(r *Root) []optionChoice {
						out := make([]optionChoice, 0, len(r.colorSchemes))
						for _, s := range r.colorSchemes {
							out = append(out, optionChoice{value: s.Slug, label: s.Theme.Name})
						}
						return out
					},
				},
				boolOption("show_hidden", "Show hidden files",
					"Whether dotfiles and dot-directories appear in the listing.\n\n"+
						"The same thing Ctrl+G and the button bar's own Hide/Unhide toggle do.",
					false,
					func(r *Root) bool { return r.panel.showHidden },
					func(r *Root, b bool) { r.setShowHidden(b) },
				),
				boolOption("size_bytes", "Size as exact bytes",
					"How the Size column is written.\n\n"+
						"Off shows a rounded, human-readable shorthand (\"4.0K\", \"1.2M\"). "+
						"On shows the exact byte count, which is what you want when comparing "+
						"two files of nearly the same size.",
					false,
					func(r *Root) bool { return r.panel.sizeBytes },
					func(r *Root, b bool) { r.setSizeBytes(b) },
				),
				boolOption("mtime_unix", "Time as Unix timestamp",
					"How the time column is written.\n\n"+
						"Off shows a formatted date and time. On shows the raw Unix timestamp, "+
						"which sorts and compares unambiguously across time zones.\n\n"+
						"Applies to the modification time in a normal listing and to the deletion "+
						"time while browsing the trash.",
					false,
					func(r *Root) bool { return r.panel.mtimeUnix },
					func(r *Root, b bool) { r.setMtimeUnix(b) },
				),
			},
		},
		{
			name: "Behavior",
			options: []optionSpec{
				boolOption("restore_tabs", "Restore tabs on start",
					"Whether the tabs that were open when you last quit are reopened on the next start.\n\n"+
						"The layout is saved on a clean exit only. Starting breakthrough with an "+
						"explicit directory (\"breakthrough /some/path\") opens just that instead, "+
						"regardless of this setting.",
					true,
					func(r *Root) bool { return r.settings.RestoreTabs },
					func(r *Root, b bool) {
						r.settings.RestoreTabs = b
						r.persistSetting("restore_tabs", strconv.FormatBool(b))
					},
				),
			},
		},
		{
			name: "Programs",
			options: []optionSpec{
				{
					key:   "pager",
					label: "Pager for Look",
					help: "Which viewer \"Look\" (Ctrl+L) opens a file in.\n\n" +
						"\"Built-in\" uses breakthrough's own viewer, which needs nothing installed " +
						"and can also show images and PDF pages.\n\n" +
						"\"External\" hands the file to bat, less, $PAGER or more — whichever is " +
						"available first — which is worth it mainly for syntax highlighting.",
					value: func(r *Root) string { return r.settings.Pager },
					apply: func(r *Root, v string) {
						r.settings.Pager = v
						r.persistSetting("pager", v)
					},
					choices: func(*Root) []optionChoice {
						return []optionChoice{
							{value: "builtin", label: "Built-in viewer"},
							{value: "external", label: "External (bat/less/$PAGER/more)"},
						}
					},
				},
			},
		},
		{
			name: "Trash",
			options: []optionSpec{
				boolOption("trash_persistent", "Keep trash across sessions",
					"Where deleted files go.\n\n"+
						"On (the default) uses a lasting trash under your data directory, so a file "+
						"deleted today is still there tomorrow. Off uses a session-scoped location "+
						"that the system clears when your login session ends — which can be sooner "+
						"than you'd expect, and makes a poor safety net.\n\n"+
						"Changing this only affects what you delete from now on. Anything already "+
						"in the old location stays there.",
					false,
					func(r *Root) bool { return r.settings.TrashPersistent },
					func(r *Root, b bool) {
						r.settings.TrashPersistent = b
						r.persistSetting("trash_persistent", strconv.FormatBool(b))
					},
				),
				intOption("trash_max_age_days", "Maximum age (days)",
					"How long a trashed item is kept before it's removed automatically.\n\n"+
						"Pruning runs once at startup, never while you're working. Set to 0 to "+
						"disable age-based pruning entirely and keep everything until you empty "+
						"the trash yourself.",
					func(r *Root) int { return r.settings.TrashMaxAgeDays },
					func(r *Root, n int) {
						r.settings.TrashMaxAgeDays = n
						r.persistSetting("trash_max_age_days", strconv.Itoa(n))
					},
				),
				intOption("trash_quota_percent", "Size limit (% of filesystem)",
					"An upper bound on how much space the trash may occupy, as a percentage of the "+
						"filesystem it lives on.\n\n"+
						"A backstop, applied at startup only after the age limit above has already "+
						"run: if the trash is still over quota, the oldest items go first until it "+
						"fits. Set to 0 to disable this entirely.",
					func(r *Root) int { return r.settings.TrashQuotaPercent },
					func(r *Root, n int) {
						r.settings.TrashQuotaPercent = n
						r.persistSetting("trash_quota_percent", strconv.Itoa(n))
					},
				),
			},
		},
	}
}

// settingValueByKey reads one setting out of a config.Settings by its
// config key, in the same literal form a config file would hold.
//
// Used by resetSetting to find out what a key's value became once the
// user tier stopped overriding it — that answer has to come from a
// freshly merged config.Settings, which the optionSpec.value accessors
// (deliberately reading live application state instead) can't provide.
func settingValueByKey(s config.Settings, key string) (string, bool) {
	switch key {
	case "color_scheme":
		return s.ColorScheme, true
	case "language":
		return s.Language, true
	case "show_hidden":
		return strconv.FormatBool(s.ShowHidden), true
	case "size_bytes":
		return strconv.FormatBool(s.SizeBytes), true
	case "mtime_unix":
		return strconv.FormatBool(s.MtimeUnix), true
	case "pager":
		return s.Pager, true
	case "trash_persistent":
		return strconv.FormatBool(s.TrashPersistent), true
	case "trash_max_age_days":
		return strconv.Itoa(s.TrashMaxAgeDays), true
	case "trash_quota_percent":
		return strconv.Itoa(s.TrashQuotaPercent), true
	case "restore_tabs":
		return strconv.FormatBool(s.RestoreTabs), true
	}
	return "", false
}
