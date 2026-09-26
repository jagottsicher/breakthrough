package ui

import (
	"strconv"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
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

	// section, when non-empty, groups this option under a labeled
	// subsection within its own category, rather than a separate
	// top-level category of its own — per the user's own explicit
	// request that a related cluster of settings (Copy & Move, and
	// later Duplicate) subdivide the category it conceptually belongs
	// under instead of costing its own vertical tab. A subsection header
	// is inserted in the settings table wherever an option's own section
	// differs from the option right before it (see
	// optionCategoryDisplayRows) — so every option belonging to the same
	// subsection must sit contiguously in the category's own options
	// slice, and the *first* option in any category must always have an
	// empty section (see renderOptions' own doc comment on why row 0
	// specifically can never be a header).
	section string
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

// withSection stamps opt with section (see optionSpec.section's own doc
// comment) — a small wrapper rather than a field on boolOption/
// intOption themselves, since only a handful of settings actually
// belong to a subsection and every constructor gaining an extra,
// almost-always-empty parameter would read worse than wrapping the few
// call sites that need it.
func withSection(section string, opt optionSpec) optionSpec {
	opt.section = section
	return opt
}

// statusBarSegmentOption builds one Status bar toggle: like
// boolOption, but always persists under key and refreshes the status
// bar immediately on top of whatever get/set actually flips — a
// toggle that visibly changes something the moment it's pressed,
// same as every other Options toggle, rather than needing a restart
// or a manual reload to show up. Every one of these seven is a plain
// "show this segment or don't" bool with no side effect beyond the
// status bar's own next render, so this one small wrapper replaces
// what would otherwise be seven near-identical persist+refresh set
// closures.
func statusBarSegmentOption(key, label, help string, get func(*Root) bool, set func(*Root, bool)) optionSpec {
	return boolOption(key, label, help, false, get, func(r *Root, b bool) {
		set(r, b)
		r.persistSetting(key, strconv.FormatBool(b))
		r.refreshStatusBar()
	})
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

// stringOption builds a free-text setting — editOptionValue's own
// default case already accepts arbitrary text for anything that isn't
// KindInt/KindBool/an enum, so this needs no acceptance-func wiring of
// its own the way intOption does; it exists purely so a string setting
// reads the same as every other entry in this file instead of a bare
// struct literal.
func stringOption(key, label, help string, get func(*Root) string, set func(*Root, string)) optionSpec {
	return optionSpec{
		key:   key,
		label: label,
		help:  help,
		value: get,
		apply: set,
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
						"The same thing \".\" and the button bar's own Hide/Unhide toggle do.",
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
			name: "Status bar",
			options: []optionSpec{
				statusBarSegmentOption("status_bar_show_username", "Username",
					"Whether the current username shows in the status bar — colored "+
						"green normally, red while running as root.",
					func(r *Root) bool { return r.settings.StatusBarShowUsername },
					func(r *Root, b bool) { r.settings.StatusBarShowUsername = b },
				),
				statusBarSegmentOption("status_bar_show_mouse", "Mouse status",
					"Whether \"Mouse on\"/\"Mouse off\" shows in the status bar. This is "+
						"purely display — it doesn't affect mouse reporting itself, see the "+
						"\"om\" chord or mouse_enabled above for that.",
					func(r *Root) bool { return r.settings.StatusBarShowMouse },
					func(r *Root, b bool) { r.settings.StatusBarShowMouse = b },
				),
				statusBarSegmentOption("status_bar_show_disk", "Disk space",
					"Whether free/total disk space for the current directory's own "+
						"filesystem shows in the status bar, with the percentage used "+
						"colored green/orange/red below 80% / 80-89% / 90%+.",
					func(r *Root) bool { return r.settings.StatusBarShowDisk },
					func(r *Root, b bool) { r.settings.StatusBarShowDisk = b },
				),
				statusBarSegmentOption("status_bar_show_inodes", "Inode usage",
					"Whether used/total inode count for the current directory's own "+
						"filesystem shows in the status bar, with the same green/orange/red "+
						"scale as disk space.",
					func(r *Root) bool { return r.settings.StatusBarShowInodes },
					func(r *Root, b bool) { r.settings.StatusBarShowInodes = b },
				),
				statusBarSegmentOption("status_bar_show_kernel", "Kernel version",
					"Whether the running kernel version (\"uname -r\") shows in the status bar.",
					func(r *Root) bool { return r.settings.StatusBarShowKernel },
					func(r *Root, b bool) { r.settings.StatusBarShowKernel = b },
				),
				statusBarSegmentOption("status_bar_show_uptime", "Uptime",
					"Whether system uptime shows in the status bar, where the platform "+
						"exposes it (Linux's own /proc/uptime; quietly omitted elsewhere).",
					func(r *Root) bool { return r.settings.StatusBarShowUptime },
					func(r *Root, b bool) { r.settings.StatusBarShowUptime = b },
				),
				statusBarSegmentOption("status_bar_show_load", "Load average",
					"Whether the 1/5/15-minute load average shows in the status bar, "+
						"where the platform exposes it (Linux's own /proc/loadavg). Each "+
						"number is colored against this machine's own core count: green "+
						"below 70% of a core-worth of load per core, orange from 70%, red "+
						"at or above one core's worth of load per core.",
					func(r *Root) bool { return r.settings.StatusBarShowLoad },
					func(r *Root, b bool) { r.settings.StatusBarShowLoad = b },
				),
				statusBarSegmentOption("show_git_status", "Git status",
					"Whether the current directory's own git branch and working-tree "+
						"state show up — both in the status bar, and as its own section "+
						"in the Details sidebar (\"I\") when a directory that's part of a "+
						"git repository is selected there. Colored green when clean, "+
						"orange with anything uncommitted (staged, unstaged, or "+
						"untracked), red the moment there's a real merge conflict. "+
						"Quietly shows nothing outside a git repository.",
					func(r *Root) bool { return r.settings.ShowGitStatus },
					func(r *Root, b bool) { r.settings.ShowGitStatus = b },
				),
			},
		},
		{
			name: "Behavior",
			options: []optionSpec{
				// "General" — per the user's own explicit request, this
				// first group gets a section header of its own too, the
				// same as every group after it, rather than being the one
				// unlabeled exception at the top.
				withSection("General", boolOption("restore_tabs", "Restore tabs on start",
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
				)),
				withSection("General", boolOption("split_stacked", "Split view stacked",
					"How split view (\"s\") divides the window between its two panes.\n\n"+
						"Off puts them side by side, which suits a wide terminal and keeps every "+
						"row of both listings visible. On stacks them above each other, which "+
						"keeps the full column width — worth it for long filenames, or on a "+
						"terminal too narrow to halve.\n\n"+
						"\"z\" then \"o\" flips this too, without coming here.",
					false,
					func(r *Root) bool { return r.settings.SplitStacked },
					func(r *Root, b bool) { r.setSplitStacked(b) },
				)),
				withSection("General", boolOption("mouse_enabled", "Mouse reporting",
					"Whether clicks and drags work in breakthrough at all.\n\n"+
						"On (the default) lets you click, drag, and scroll — but it also hands "+
						"every mouse event to breakthrough instead of your terminal emulator, "+
						"which breaks that terminal's own native text selection/copy (e.g. to "+
						"grab a filename) unless you already know its own override gesture "+
						"(Shift-drag, on most xterm-derived emulators).\n\n"+
						"\"o\" then \"m\" flips this too, without coming here.",
					false,
					func(r *Root) bool { return r.mouseEnabled },
					func(r *Root, b bool) { r.setMouseEnabled(b) },
				)),
				withSection("General", boolOption("filter_persistent", "Filter carries over between directories",
					"Whether the filter menu's own text/glob/regex pattern and its size/"+
						"modified-time toggles stay active when you move to a different directory.\n\n"+
						"On (the default) keeps browsing with the same filter switched on until "+
						"you change or clear it yourself — the filter-menu button's own \"Nx\" "+
						"count stays visible the whole time, so a filter that's still narrowing "+
						"what you see is never silently forgotten. Off goes back to the original "+
						"behavior: every new directory starts unfiltered, and you filter it again "+
						"from scratch if you want to.",
					false,
					func(r *Root) bool { return r.settings.FilterPersistent },
					func(r *Root, b bool) { r.setFilterPersistent(b) },
				)),
				// "Copy & Move" subsection — per the user's own explicit
				// request, these eight settings belong grouped under
				// Behavior rather than costing their own top-level
				// category (see optionSpec.section's own doc comment);
				// the same applies to the planned Duplicate settings
				// later.
				withSection("Copy & Move", boolOption("copy_preserve_attributes", "Copy preserves attributes",
					"Whether a Copy job carries the source's own permissions, ownership and "+
						"modification time over to the destination.\n\n"+
						"On (the default) matches what you almost always want. Off leaves the "+
						"destination at whatever creating it just produced — its own default "+
						"permissions (narrowed by your umask, same as any new file) and its own "+
						"real creation time.",
					false,
					func(r *Root) bool { return r.settings.CopyPreserveAttributes },
					func(r *Root, b bool) {
						r.settings.CopyPreserveAttributes = b
						r.persistSetting("copy_preserve_attributes", strconv.FormatBool(b))
					},
				)),
				// Grouped Copy-then-Move (all four Copy settings, then
				// all four Move ones) rather than alternating pairs —
				// per the user's own explicit request.
				withSection("Copy & Move", boolOption("copy_follow_symlinks", "Copy follows symlinks",
					"The default a Copy paste (\"v\") uses when the selection includes a "+
						"symlink: dereference it, writing a real copy of whatever it points to, "+
						"instead of a new symlink pointing at the same place.\n\n"+
						"Off (the default) copies the link itself. Pressing \"V\" instead of \"v\" "+
						"flips this once, for that one paste only, without changing this setting.\n\n"+
						"Worth leaving off: dereferencing can silently pull in far more data than "+
						"the size shown in the panel (a link to a large file or an entire "+
						"directory tree), and it can't be undone back into \"just a link\" "+
						"afterward.",
					false,
					func(r *Root) bool { return r.settings.CopyFollowSymlinks },
					func(r *Root, b bool) {
						r.settings.CopyFollowSymlinks = b
						r.persistSetting("copy_follow_symlinks", strconv.FormatBool(b))
					},
				)),
				withSection("Copy & Move", boolOption("copy_auto_merge_directories", "Copy auto-merges directories",
					"Whether a Copy paste that runs into a directory already existing at the "+
						"destination combines the two automatically, without asking.\n\n"+
						"Only ever applies when both sides are directories — a conflict between two "+
						"files, or a file and a directory, always still asks, since \"merge\" has no "+
						"meaning there. Off (the default) shows the usual conflict dialog every "+
						"time, so a paste job never silently starts combining directory contents.",
					false,
					func(r *Root) bool { return r.settings.CopyAutoMergeDirectories },
					func(r *Root, b bool) {
						r.settings.CopyAutoMergeDirectories = b
						r.persistSetting("copy_auto_merge_directories", strconv.FormatBool(b))
					},
				)),
				withSection("Copy & Move", boolOption("copy_stable_symlinks", "Copy keeps symlinks stable",
					"Whether a symlink whose target lives inside the tree being copied gets "+
						"that target rewritten to point at the corresponding new location, instead "+
						"of keeping the original target verbatim.\n\n"+
						"Off (the default) copies the link's target exactly as it was. Left as is, "+
						"a link pointing elsewhere in the very tree being copied can end up "+
						"pointing back at the original source (an absolute target) or resolving "+
						"nowhere at all (a relative target that climbed out of the copied root and "+
						"back in by its old name) once that source is later moved, renamed or "+
						"removed.",
					false,
					func(r *Root) bool { return r.settings.CopyStableSymlinks },
					func(r *Root, b bool) {
						r.settings.CopyStableSymlinks = b
						r.persistSetting("copy_stable_symlinks", strconv.FormatBool(b))
					},
				)),
				withSection("Copy & Move", boolOption("move_preserve_attributes", "Move preserves attributes",
					"Move's own counterpart to \"Copy preserves attributes\".\n\n"+
						"Has no effect on a same-filesystem move: that's a single atomic rename, "+
						"the same inode throughout, so its attributes never change regardless of "+
						"this setting. It only matters once a move falls back to a real copy — "+
						"across filesystems, or merging into an existing directory.",
					false,
					func(r *Root) bool { return r.settings.MovePreserveAttributes },
					func(r *Root, b bool) {
						r.settings.MovePreserveAttributes = b
						r.persistSetting("move_preserve_attributes", strconv.FormatBool(b))
					},
				)),
				withSection("Copy & Move", boolOption("move_follow_symlinks", "Move follows symlinks",
					"Move's own counterpart to \"Copy follows symlinks\" — the default a Cut "+
						"paste (\"v\" after \"x\") uses, with \"V\" flipping it once for that one "+
						"paste the same way.",
					false,
					func(r *Root) bool { return r.settings.MoveFollowSymlinks },
					func(r *Root, b bool) {
						r.settings.MoveFollowSymlinks = b
						r.persistSetting("move_follow_symlinks", strconv.FormatBool(b))
					},
				)),
				withSection("Copy & Move", boolOption("move_auto_merge_directories", "Move auto-merges directories",
					"Move's own counterpart to \"Copy auto-merges directories\", for a Cut paste.",
					false,
					func(r *Root) bool { return r.settings.MoveAutoMergeDirectories },
					func(r *Root, b bool) {
						r.settings.MoveAutoMergeDirectories = b
						r.persistSetting("move_auto_merge_directories", strconv.FormatBool(b))
					},
				)),
				withSection("Copy & Move", boolOption("move_stable_symlinks", "Move keeps symlinks stable",
					"Move's own counterpart to \"Copy keeps symlinks stable\". Has no effect on a "+
						"same-filesystem move (the fast path never touches an individual symlink at "+
						"all) or on a move with \"Follow symlinks\" on (nothing survives as a link "+
						"to rewrite) — it only matters once a move falls back to a real, "+
						"link-preserving copy.",
					false,
					func(r *Root) bool { return r.settings.MoveStableSymlinks },
					func(r *Root, b bool) {
						r.settings.MoveStableSymlinks = b
						r.persistSetting("move_stable_symlinks", strconv.FormatBool(b))
					},
				)),
				// "Duplicate" subsection — per the user's own explicit
				// request. Unlike every other setting in this app, eight
				// of these nine are self-adapting: applyDuplicateSelection
				// (duplicate.go) writes back through these exact same
				// apply functions whenever the Duplicate dialog itself is
				// actually confirmed, not just here — so whatever was
				// chosen there becomes the new default shown here next
				// time, "so passt sich das den Vorlieben an" being the
				// user's own explicit words for it. The ninth,
				// "duplicate_count_max", is deliberately not — it's a
				// safety bound the dialog itself never offers a field
				// for, only ever set here.
				withSection("Duplicate", stringOption("duplicate_separator", "Separator",
					"Sits between the original name — its extension included, untouched — and "+
						"Duplicate's own suffix — free-form: \"_\", \"-\", \".\" are the obvious "+
						"choices, but nothing here requires any particular one.\n\n"+
						"\"_\" by default: \"report.txt\" duplicates to \"report.txt_1\" (with the "+
						"default \"Numbered\" strategy below) — always after the *entire* original "+
						"name, never before an extension: \"archive.tar.gz\" duplicates to "+
						"\"archive.tar.gz_1\", not \"archive.tar_1.gz\" or \"archive_1.tar.gz\". "+
						"Nothing about a basename is treated as somehow more \"real\" than the "+
						"rest of it just because it follows a dot.",
					func(r *Root) string { return r.settings.DuplicateSeparator },
					func(r *Root, v string) {
						r.settings.DuplicateSeparator = v
						r.persistSetting("duplicate_separator", v)
					},
				)),
				withSection("Duplicate", optionSpec{
					key:   "duplicate_strategy",
					label: "Naming strategy",
					help: "How Duplicate names a copy — always appended after the original " +
						"name's own entire text, extension included (see \"Separator\" above).\n\n" +
						"\"Numbered\" (the default) appends an incrementing number, scanning " +
						"upward from 1 until a free name is found — \"report.txt_1\", then " +
						"\"report.txt_2\", and so on, however many duplicates already exist.\n\n" +
						"\"Fixed suffix text\" appends the same literal text every time (see " +
						"\"Suffix text\" below) — \"report.txt_copy\". If that already exists, this " +
						"does not count up: duplicating \"report.txt_copy\" itself produces " +
						"\"report.txt_copy_copy\", the same rule applied again to the new name — " +
						"not an automatic retry within one Duplicate.\n\n" +
						"\"Date/time\" appends a timestamp (see the three settings below) — " +
						"computed once, not retried if it happens to collide.",
					value: func(r *Root) string { return r.settings.DuplicateStrategy },
					apply: func(r *Root, v string) {
						r.settings.DuplicateStrategy = v
						r.persistSetting("duplicate_strategy", v)
					},
					choices: func(*Root) []optionChoice {
						return []optionChoice{
							{value: "numbered", label: "Numbered"},
							{value: "suffix_text", label: "Fixed suffix text"},
							{value: "datetime", label: "Date/time"},
						}
					},
				}),
				withSection("Duplicate", stringOption("duplicate_suffix_text", "Suffix text",
					"The literal text \"Fixed suffix text\" (see the strategy above) appends — "+
						"only relevant while that strategy is selected.\n\n"+
						"\"copy\" by default: \"report.txt\" duplicates to \"report.txt_copy\".",
					func(r *Root) string { return r.settings.DuplicateSuffixText },
					func(r *Root, v string) {
						r.settings.DuplicateSuffixText = v
						r.persistSetting("duplicate_suffix_text", v)
					},
				)),
				withSection("Duplicate", intOption("duplicate_number_padding", "Number padding (digits)",
					"How many digits the \"Numbered\" strategy (see above) pads its own number "+
						"to with leading zeros — only relevant while that strategy is selected.\n\n"+
						"0 (the default) disables padding entirely: \"report.txt_1\", "+
						"\"report.txt_2\", ... \"report.txt_10\". Set to, say, 3 for "+
						"\"report.txt_001\", \"report.txt_002\", ... \"report.txt_010\" instead — "+
						"keeps a directory's own listing sorted in numeric order by name even "+
						"once there are 10 or more duplicates.",
					func(r *Root) int { return r.settings.DuplicateNumberPadding },
					func(r *Root, n int) {
						r.settings.DuplicateNumberPadding = n
						r.persistSetting("duplicate_number_padding", strconv.Itoa(n))
					},
				)),
				withSection("Duplicate", stringOption("duplicate_datetime_format", "Date/time format",
					"The format string the \"Date/time\" strategy (see above) renders its own "+
						"timestamp with — only relevant while that strategy is selected, and "+
						"ignored entirely once \"Use Unix timestamp\" below is on.\n\n"+
						"Interpreted as Go's own reference-time layout by default, or as a "+
						"strftime-style format instead once \"Strftime-style format\" below is "+
						"on — see that setting's own explanation for the exact format string "+
						"each mode expects to render the same example date and time.",
					func(r *Root) string { return r.settings.DuplicateDateTimeFormat },
					func(r *Root, v string) {
						r.settings.DuplicateDateTimeFormat = v
						r.persistSetting("duplicate_datetime_format", v)
					},
				)),
				withSection("Duplicate", boolOption("duplicate_datetime_strftime", "Strftime-style format",
					"How \"Date/time format\" above is interpreted — in the Multiply dialog "+
						"itself, its own \"Date/time format type\" dropdown picks this (and "+
						"\"Use Unix timestamp\" below) directly instead of a pair of separate "+
						"toggles.\n\n"+
						"Off (the default): Go's own reference-time layout, the format Go's code "+
						"itself uses internally — e.g. \"2006-1-2 15:04:05\" renders as "+
						"\"2026-11-9 23:59:59\".\n\n"+
						"On: a strftime-style format instead, more familiar from date(1)/cron — "+
						"e.g. \"%Y-%-m-%-d %H:%M:%S\" renders that exact same \"2026-11-9 "+
						"23:59:59\". Supports %Y %y %m %d %H %I %M %S %p %B %b %A %a %j %Z %z, "+
						"plus GNU's \"%-\" no-padding variant (%-m, %-d, %-I) for the fields "+
						"where a leading zero is usually unwanted in a filename — %-H falls back "+
						"to the padded form regardless, since Go's own layout has no unpadded "+
						"24-hour token to translate it to.",
					false,
					func(r *Root) bool { return r.settings.DuplicateDateTimeStrftime },
					func(r *Root, b bool) {
						r.settings.DuplicateDateTimeStrftime = b
						r.persistSetting("duplicate_datetime_strftime", strconv.FormatBool(b))
					},
				)),
				withSection("Duplicate", boolOption("duplicate_datetime_use_unix", "Use Unix timestamp",
					"Bypasses \"Date/time format\" above entirely in favor of a raw Unix "+
						"timestamp (seconds since 1970-01-01 UTC) — for anyone who'd rather not "+
						"deal with a format string at all. Off by default.",
					false,
					func(r *Root) bool { return r.settings.DuplicateDateTimeUseUnix },
					func(r *Root, b bool) {
						r.settings.DuplicateDateTimeUseUnix = b
						r.persistSetting("duplicate_datetime_use_unix", strconv.FormatBool(b))
					},
				)),
				withSection("Duplicate", intOption("duplicate_count", "Number of duplicates",
					"How many duplicates one Duplicate invocation creates at once — each named "+
						"in sequence by the strategy above (three \"Numbered\" duplicates of "+
						"\"report.txt\" become \"report.txt_1\", \"report.txt_2\", "+
						"\"report.txt_3\" in one go).\n\n"+
						"1 by default — today's exact single-duplicate behavior, unchanged. "+
						"Capped by \"Maximum number of duplicates\" below.",
					func(r *Root) int { return r.settings.DuplicateCount },
					func(r *Root, n int) {
						r.settings.DuplicateCount = n
						r.persistSetting("duplicate_count", strconv.Itoa(n))
					},
				)),
				withSection("Duplicate", intOption("duplicate_count_max", "Maximum number of duplicates",
					"The upper bound \"Number of duplicates\" above can be set to — purely a "+
						"safety net against an accidental \"ten thousand copies\", not a hard "+
						"limit: raising this is itself just another config value.\n\n"+
						"100 by default.",
					func(r *Root) int { return r.settings.DuplicateCountMax },
					func(r *Root, n int) {
						r.settings.DuplicateCountMax = n
						r.persistSetting("duplicate_count_max", strconv.Itoa(n))
					},
				)),
				// "Miscellaneous" subsection — per the user's own explicit
				// request, for settings that don't belong to a larger
				// cluster of their own.
				withSection("Miscellaneous", intOption("chord_timeout_ms", "Chord timeout (ms)",
					"How long, in milliseconds, a chord's second key (gg, oo, po, zr, ...) "+
						"stays live for once the first key is pressed.\n\n"+
						"4000 (four seconds) by default — long enough to actually read the "+
						"button bar's own legend for the chord's members first, short enough "+
						"that an abandoned chord doesn't sit waiting indefinitely for a "+
						"keystroke that might arrive minutes later and mean something "+
						"completely different by then.",
					func(r *Root) int { return r.settings.ChordTimeoutMS },
					func(r *Root, n int) {
						r.settings.ChordTimeoutMS = n
						r.persistSetting("chord_timeout_ms", strconv.Itoa(n))
					},
				)),
				// "Compress & Extract" subsection — self-adapting, per
				// the user's own explicit request that this follow the
				// same "whatever gets chosen becomes the new sticky
				// default" shape Duplicate's own settings above already
				// have: confirming Compress with a given Format writes
				// it back here (see openCompress/runCompress in
				// compress.go), so the dialog opens on whichever one was
				// actually used last instead of always "zip".
				withSection("Compress & Extract", optionSpec{
					key:   "compress_format",
					label: "Default archive format",
					help: "Which format Compress's own \"Format\" dropdown starts on.\n\n" +
						"Self-adapting: whichever format you actually pick and confirm Compress " +
						"with becomes the new value here — this only ever sets the starting " +
						"point the dialog opens with, never overrides a format you then pick " +
						"differently for one particular archive.\n\n" +
						"\"zip\" by default.",
					value: func(r *Root) string { return r.settings.CompressFormat },
					apply: func(r *Root, v string) {
						r.settings.CompressFormat = v
						r.persistSetting("compress_format", v)
					},
					choices: func(*Root) []optionChoice {
						out := make([]optionChoice, 0, len(archiveFormats()))
						for _, f := range archiveFormats() {
							out = append(out, optionChoice{value: archiveFormatID(f), label: f.label})
						}
						return out
					},
				}),
				// "Rsync" subsection — self-adapting, the same shape as
				// "Compress & Extract" above: running Rsync (Run or Run
				// in background) writes its own five flags back here
				// (see runRsync/runRsyncBackground in rsync.go), so the
				// dialog opens on whichever combination was actually
				// used last.
				withSection("Rsync", boolOption("rsync_copy_contents", "Copy the folder's contents in",
					"Rsync's own last-used \"Copy the folder's contents in (not the folder "+
						"itself)\" flag — see internal/rsync's own CopyContents doc comment for "+
						"what it actually changes. Off by default.",
					false,
					func(r *Root) bool { return r.settings.RsyncCopyContents },
					func(r *Root, b bool) {
						r.settings.RsyncCopyContents = b
						r.persistSetting("rsync_copy_contents", strconv.FormatBool(b))
					},
				)),
				withSection("Rsync", boolOption("rsync_archive", "Archive mode (-a)",
					"Rsync's own last-used \"Archive mode\" flag — preserves permissions, "+
						"times, and symlinks. On by default, rsync's own conventional "+
						"\"just copy it properly\" starting point.",
					false,
					func(r *Root) bool { return r.settings.RsyncArchive },
					func(r *Root, b bool) {
						r.settings.RsyncArchive = b
						r.persistSetting("rsync_archive", strconv.FormatBool(b))
					},
				)),
				withSection("Rsync", boolOption("rsync_compress", "Compress data in transit (-z)",
					"Rsync's own last-used compression flag — mainly useful over a slow "+
						"link. Off by default.",
					false,
					func(r *Root) bool { return r.settings.RsyncCompress },
					func(r *Root, b bool) {
						r.settings.RsyncCompress = b
						r.persistSetting("rsync_compress", strconv.FormatBool(b))
					},
				)),
				withSection("Rsync", boolOption("rsync_delete", "Delete extraneous files (--delete)",
					"Rsync's own last-used \"also remove from Destination whatever no "+
						"longer exists in Source\" flag. Off by default — this can permanently "+
						"remove files at the destination, so it's never silently pre-enabled "+
						"just because a previous run happened to use it.",
					false,
					func(r *Root) bool { return r.settings.RsyncDelete },
					func(r *Root, b bool) {
						r.settings.RsyncDelete = b
						r.persistSetting("rsync_delete", strconv.FormatBool(b))
					},
				)),
				withSection("Rsync", boolOption("rsync_dry_run", "Dry run (-n)",
					"Rsync's own last-used \"report what would happen, change nothing\" "+
						"flag. Off by default.",
					false,
					func(r *Root) bool { return r.settings.RsyncDryRun },
					func(r *Root, b bool) {
						r.settings.RsyncDryRun = b
						r.persistSetting("rsync_dry_run", strconv.FormatBool(b))
					},
				)),
				// "Sed Replace" subsection — self-adapting, the same
				// shape as above: running Preview writes its own five
				// flags back here (see runSedPreview in sedreplace.go).
				withSection("Sed Replace", boolOption("sed_regex", "Regex",
					"Sed Replace's own last-used \"Find is a pattern, not literal text\" "+
						"flag. Off by default.",
					false,
					func(r *Root) bool { return r.settings.SedRegex },
					func(r *Root, b bool) {
						r.settings.SedRegex = b
						r.persistSetting("sed_regex", strconv.FormatBool(b))
					},
				)),
				withSection("Sed Replace", boolOption("sed_extended_regex", "Extended regex (-E)",
					"Sed Replace's own last-used extended-regex flag. Off by default.",
					false,
					func(r *Root) bool { return r.settings.SedExtendedRegex },
					func(r *Root, b bool) {
						r.settings.SedExtendedRegex = b
						r.persistSetting("sed_extended_regex", strconv.FormatBool(b))
					},
				)),
				withSection("Sed Replace", boolOption("sed_case_insensitive", "Case-insensitive",
					"Sed Replace's own last-used case-insensitive flag. Off by default.",
					false,
					func(r *Root) bool { return r.settings.SedCaseInsensitive },
					func(r *Root, b bool) {
						r.settings.SedCaseInsensitive = b
						r.persistSetting("sed_case_insensitive", strconv.FormatBool(b))
					},
				)),
				withSection("Sed Replace", boolOption("sed_global", "Replace all matches per line",
					"Sed Replace's own last-used \"replace every match, not just the "+
						"first\" flag. On by default, today's existing behavior.",
					false,
					func(r *Root) bool { return r.settings.SedGlobal },
					func(r *Root, b bool) {
						r.settings.SedGlobal = b
						r.persistSetting("sed_global", strconv.FormatBool(b))
					},
				)),
				withSection("Sed Replace", boolOption("sed_backup", "Keep a .bak backup",
					"Sed Replace's own last-used \"keep a .bak backup before overwriting\" "+
						"flag. Off by default.",
					false,
					func(r *Root) bool { return r.settings.SedBackup },
					func(r *Root, b bool) {
						r.settings.SedBackup = b
						r.persistSetting("sed_backup", strconv.FormatBool(b))
					},
				)),
				// "Search" subsection — self-adapting, the same shape as
				// above: running a real search writes these four back
				// here (see runSearch in search.go) — the same four that
				// already stayed put for the rest of a session, never
				// resetting between searches (see openSearch's own doc
				// comment), now surviving a restart too.
				withSection("Search", optionSpec{
					key:   "search_engine",
					label: "Default engine",
					help: "Which Engine the Search dialog starts on: \"find\" or \"locate\" " +
						"(only ever offered where locate's own database is actually usable — " +
						"see search.LocateAvailable).\n\n" +
						"Self-adapting: running a search with a given Engine selected becomes " +
						"the new value here. \"find\" by default.",
					value: func(r *Root) string { return r.settings.SearchEngine },
					apply: func(r *Root, v string) {
						r.settings.SearchEngine = v
						r.persistSetting("search_engine", v)
					},
					choices: func(*Root) []optionChoice {
						return []optionChoice{
							{value: "find", label: "find"},
							{value: "locate", label: "locate"},
						}
					},
				}),
				withSection("Search", boolOption("search_shell_patterns", "Using shell patterns",
					"Search's own last-used Filename match mode: on for a shell-glob "+
						"pattern, off for a regex. On by default, matching MC's own real "+
						"default for this same checkbox.",
					false,
					func(r *Root) bool { return r.settings.SearchShellPatterns },
					func(r *Root, b bool) {
						r.settings.SearchShellPatterns = b
						r.persistSetting("search_shell_patterns", strconv.FormatBool(b))
					},
				)),
				withSection("Search", boolOption("search_case_sensitive", "Case sensitive",
					"Search's own last-used \"Case sensitive\" flag. Off by default.",
					false,
					func(r *Root) bool { return r.settings.SearchCaseSensitive },
					func(r *Root, b bool) {
						r.settings.SearchCaseSensitive = b
						r.persistSetting("search_case_sensitive", strconv.FormatBool(b))
					},
				)),
				withSection("Search", boolOption("search_skip_hidden", "Skip hidden",
					"Search's own last-used \"Skip hidden\" flag. Off by default.",
					false,
					func(r *Root) bool { return r.settings.SearchSkipHidden },
					func(r *Root, b bool) {
						r.settings.SearchSkipHidden = b
						r.persistSetting("search_skip_hidden", strconv.FormatBool(b))
					},
				)),
			},
		},
		{
			name: "Programs",
			options: []optionSpec{
				{
					key:   "pager",
					label: "Pager for Look",
					help: "Which viewer \"Look\" (\"l\") opens a file in.\n\n" +
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
				stringOption("open_with_command", "Last \"Open with…\" command",
					"The command \"Open with…\" last ran, prefilled the next time it's opened.\n\n"+
						"Self-adapting: whatever you actually type into \"Open with…\" and run "+
						"becomes the new value here, the same way it already prefilled the next "+
						"open within a session — this just makes that survive a restart too.\n\n"+
						"Empty by default (nothing has been typed yet).",
					func(r *Root) string { return r.settings.OpenWithCommand },
					func(r *Root, v string) {
						r.settings.OpenWithCommand = v
						r.persistSetting("open_with_command", v)
					},
				),
			},
		},
		{
			name: "Trash",
			options: []optionSpec{
				boolOption("trash_confirm", "Confirm before moving to Trash",
					"Whether \"d\" (Move to Trash) asks for confirmation first, the same way "+
						"\"D\" (Remove permanently) always has.\n\n"+
						"Off (the default) moves straight to the trash without asking — the "+
						"reversible action here has never needed to ask, unlike Remove. Turning "+
						"this on adds an extra safety net on top of that for anyone who wants "+
						"one; \"D\"'s own confirmation is unconditional either way, regardless of "+
						"this setting.",
					false,
					func(r *Root) bool { return r.settings.TrashConfirm },
					func(r *Root, b bool) {
						r.settings.TrashConfirm = b
						r.persistSetting("trash_confirm", strconv.FormatBool(b))
					},
				),
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
		{
			name: "Remote connections",
			options: []optionSpec{
				stringOption("remote_archive_confirm_size", "Confirm before downloading archives over",
					"Opening a zip/tar/... that lives on a remote SFTP connection has to download "+
						"it whole first — there's no way to browse one without the whole thing "+
						"local, the same as a zip's own central directory always sitting at the "+
						"end of the file regardless of where it lives.\n\n"+
						"Below this size, that download just happens on its own, the same "+
						"proactively-transparent way every other remote operation already works. "+
						"At or above it, a confirmation names the real size first, so a large "+
						"archive over a slow link can't turn one keypress into an unexpected, "+
						"unwarned multi-minute wait.\n\n"+
						`Type a size with a unit — "10MB", "500KB", "1GB" — or a bare number of `+
						"bytes. 10MB by default.",
					func(r *Root) string { return config.FormatByteSize(r.settings.RemoteArchiveConfirmSize) },
					func(r *Root, v string) {
						n, err := config.ParseByteSize(v)
						if err != nil {
							return // see boolOption's own equivalent guard: ignore, don't guess
						}
						r.settings.RemoteArchiveConfirmSize = n
						r.persistSetting("remote_archive_confirm_size", config.FormatByteSize(n))
					},
				),
			},
		},
		{
			// Off by default (see internal/activitylog's own doc
			// comment on why this stays opt-in) — a plain-text, one-
			// line-per-entry activity log of what breakthrough itself
			// did, kept for two reasons the user's own explicit request
			// named: making what happened with breakthrough traceable
			// after the fact, and giving a future Undo feature a real,
			// structured record to work from. Written to
			// /var/log/breakthrough if that's writable (root or a
			// pre-prepared shared group directory), or the same
			// per-user directory the crash log already uses otherwise
			// — see activitylog.ResolvePath's own doc comment.
			name: "Activity log",
			options: []optionSpec{
				{
					key:   "log_level",
					label: "Detail level",
					help: "How much the activity log records — each step a superset of the one " +
						"before it. Off by default.\n\n" +
						"\"Errors\" records only a failed action. \"Actions\" additionally records " +
						"every successful, state-changing action (Copy, Move, Rename, Trash/Remove, " +
						"Compress/Extract, chmod/chown, a Rsync run, ...) — the level a future Undo " +
						"feature, and simply reviewing what you did with breakthrough, actually " +
						"needs. \"Detailed\" additionally records an action's own sub-steps (each " +
						"file within a batch Copy/Paste, each conflict resolution, each file Rsync " +
						"itself reports transferring). \"Debug\" additionally records internal " +
						"diagnostic detail (the exact command line an external tool was handed, " +
						"connection handshake steps, timing) — for troubleshooting breakthrough " +
						"itself, not for reviewing what you did with it.\n\n" +
						"The categories below are independent of this level: turning one off means " +
						"nothing in that category is ever recorded, at any level.",
					value: func(r *Root) string { return r.settings.LogLevel },
					apply: func(r *Root, v string) {
						r.settings.LogLevel = v
						r.persistSetting("log_level", v)
						r.reopenActivityLog()
					},
					choices: func(*Root) []optionChoice {
						var choices []optionChoice
						for _, level := range activitylog.Levels() {
							choices = append(choices, optionChoice{value: level.String(), label: level.Label()})
						}
						return choices
					},
				},
				activityLogCategoryOption("log_category_fileops", "File operations",
					"Copy, Cut/Paste, Rename, Trash/Remove/Restore, Multiply, and New file/New dir.",
					func(r *Root) bool { return r.settings.LogCategoryFileOps },
					func(r *Root, b bool) { r.settings.LogCategoryFileOps = b }),
				activityLogCategoryOption("log_category_permissions", "Permission changes",
					"chmod and chown.",
					func(r *Root) bool { return r.settings.LogCategoryPermissions },
					func(r *Root, b bool) { r.settings.LogCategoryPermissions = b }),
				activityLogCategoryOption("log_category_archive", "Archives",
					"Compress and Extract.",
					func(r *Root) bool { return r.settings.LogCategoryArchive },
					func(r *Root, b bool) { r.settings.LogCategoryArchive = b }),
				activityLogCategoryOption("log_category_textops", "Sed Replace / Batch Rename",
					"Sed Replace and Batch Rename.",
					func(r *Root) bool { return r.settings.LogCategoryTextOps },
					func(r *Root, b bool) { r.settings.LogCategoryTextOps = b }),
				activityLogCategoryOption("log_category_rsync", "Rsync",
					"A Rsync run, foreground or backgrounded.",
					func(r *Root) bool { return r.settings.LogCategoryRsync },
					func(r *Root, b bool) { r.settings.LogCategoryRsync = b }),
				activityLogCategoryOption("log_category_remote", "Remote connections",
					"Connecting/disconnecting an SFTP connection, and any remote file transfer.",
					func(r *Root) bool { return r.settings.LogCategoryRemote },
					func(r *Root, b bool) { r.settings.LogCategoryRemote = b }),
				activityLogCategoryOption("log_category_shell", "Shell",
					"The bash line, \"Open with…\", and Edit.",
					func(r *Root) bool { return r.settings.LogCategoryShell },
					func(r *Root, b bool) { r.settings.LogCategoryShell = b }),
				activityLogCategoryOption("log_category_firewall", "Firewall rules",
					"Adding a firewall rule via the Firewall screen's own Add-rule form, and its own self-lockout rollback.",
					func(r *Root) bool { return r.settings.LogCategoryFirewall },
					func(r *Root, b bool) { r.settings.LogCategoryFirewall = b }),
			},
		},
		{
			// Deliberately NOT self-adapting, unlike every other
			// category above — see FirewallDefaultDirection's own doc
			// comment in config/settings.go for why: the "Add rule"
			// form itself never writes back through these three, only
			// ever reads them, so an accidentally reused Deny-any-port
			// from one rule can never silently become the starting
			// point for the next, unrelated one.
			name: "Firewall",
			options: []optionSpec{
				{
					key:   "firewall_default_direction",
					label: "Default direction",
					help: "Which Direction the Firewall screen's own \"Add rule\" form starts " +
						"on.\n\n" +
						"Not self-adapting: submitting a rule with a different Direction " +
						"selected never changes this — the form always resets to this value on " +
						"every open, exactly the same as before this setting existed. \"Incoming\" " +
						"by default.",
					value: func(r *Root) string { return r.settings.FirewallDefaultDirection },
					apply: func(r *Root, v string) {
						r.settings.FirewallDefaultDirection = v
						r.persistSetting("firewall_default_direction", v)
					},
					choices: func(*Root) []optionChoice {
						out := make([]optionChoice, 0, len(firewallDirectionChoices))
						for _, c := range firewallDirectionChoices {
							out = append(out, optionChoice{value: firewallDirectionConfigValue(c.value), label: c.label})
						}
						return out
					},
				},
				{
					key:   "firewall_default_action",
					label: "Default action",
					help: "Which Action the Firewall screen's own \"Add rule\" form starts on.\n\n" +
						"Not self-adapting, the same as \"Default direction\" above. \"Allow\" by " +
						"default — deliberately never \"Deny\" or \"Reject\", regardless of what " +
						"this is set to changing that: raising the ceiling here is a config edit " +
						"the same as any other, not a silent behavior change.",
					value: func(r *Root) string { return r.settings.FirewallDefaultAction },
					apply: func(r *Root, v string) {
						r.settings.FirewallDefaultAction = v
						r.persistSetting("firewall_default_action", v)
					},
					choices: func(*Root) []optionChoice {
						out := make([]optionChoice, 0, len(firewallActionChoices))
						for _, c := range firewallActionChoices {
							out = append(out, optionChoice{value: firewallActionConfigValue(c.value), label: c.label})
						}
						return out
					},
				},
				{
					key:   "firewall_default_protocol",
					label: "Default protocol",
					help: "Which Protocol the Firewall screen's own \"Add rule\" form starts " +
						"on.\n\n" +
						"Not self-adapting, the same as the two settings above. \"any\" by " +
						"default.",
					value: func(r *Root) string { return r.settings.FirewallDefaultProtocol },
					apply: func(r *Root, v string) {
						r.settings.FirewallDefaultProtocol = v
						r.persistSetting("firewall_default_protocol", v)
					},
					choices: func(*Root) []optionChoice {
						out := make([]optionChoice, 0, len(firewallProtocolChoices))
						for _, c := range firewallProtocolChoices {
							out = append(out, optionChoice{value: c.value, label: c.label})
						}
						return out
					},
				},
			},
		},
	}
}

// activityLogCategoryOption builds one Activity log category toggle —
// like boolOption, but also reopens the activity log afterward (see
// reopenActivityLog's own doc comment for why a category change needs
// that and a level change already gets it inline above): the Logger
// itself holds a snapshot of which categories are enabled, taken once
// when it was last (re)built, so a toggle here has no visible effect
// at all until that snapshot is refreshed.
func activityLogCategoryOption(key, label, help string, get func(*Root) bool, set func(*Root, bool)) optionSpec {
	return boolOption(key, label, help, false, get, func(r *Root, b bool) {
		set(r, b)
		r.persistSetting(key, strconv.FormatBool(b))
		r.reopenActivityLog()
	})
}

// optionSpecByKey finds one setting's own optionSpec by its config key,
// searching every category — used by callers outside optionsscreen.go
// itself that still want to read/apply/enumerate-choices-for a setting
// through the exact same path the Options screen uses, rather than
// duplicating that logic against r.settings directly. The Duplicate
// dialog (see duplicate.go) is the first such caller: its own
// "self-adapting defaults" behavior — whatever gets chosen there
// becomes the new sticky default — reuses these optionSpecs' own apply
// functions verbatim, so the two persistence paths can never drift
// apart the way two independent copies of the same switch statement
// eventually would.
func optionSpecByKey(key string) (optionSpec, bool) {
	for _, cat := range optionCategories() {
		for _, opt := range cat.options {
			if opt.key == key {
				return opt, true
			}
		}
	}
	return optionSpec{}, false
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
	case "trash_confirm":
		return strconv.FormatBool(s.TrashConfirm), true
	case "trash_persistent":
		return strconv.FormatBool(s.TrashPersistent), true
	case "trash_max_age_days":
		return strconv.Itoa(s.TrashMaxAgeDays), true
	case "trash_quota_percent":
		return strconv.Itoa(s.TrashQuotaPercent), true
	case "restore_tabs":
		return strconv.FormatBool(s.RestoreTabs), true
	case "split_stacked":
		return strconv.FormatBool(s.SplitStacked), true
	case "mouse_enabled":
		return strconv.FormatBool(s.MouseEnabled), true
	case "filter_persistent":
		return strconv.FormatBool(s.FilterPersistent), true
	case "chord_timeout_ms":
		return strconv.Itoa(s.ChordTimeoutMS), true
	case "status_bar_show_username":
		return strconv.FormatBool(s.StatusBarShowUsername), true
	case "status_bar_show_mouse":
		return strconv.FormatBool(s.StatusBarShowMouse), true
	case "status_bar_show_disk":
		return strconv.FormatBool(s.StatusBarShowDisk), true
	case "status_bar_show_inodes":
		return strconv.FormatBool(s.StatusBarShowInodes), true
	case "status_bar_show_kernel":
		return strconv.FormatBool(s.StatusBarShowKernel), true
	case "status_bar_show_uptime":
		return strconv.FormatBool(s.StatusBarShowUptime), true
	case "status_bar_show_load":
		return strconv.FormatBool(s.StatusBarShowLoad), true
	case "show_git_status":
		return strconv.FormatBool(s.ShowGitStatus), true
	case "duplicate_separator":
		return s.DuplicateSeparator, true
	case "duplicate_strategy":
		return s.DuplicateStrategy, true
	case "duplicate_suffix_text":
		return s.DuplicateSuffixText, true
	case "duplicate_number_padding":
		return strconv.Itoa(s.DuplicateNumberPadding), true
	case "duplicate_datetime_format":
		return s.DuplicateDateTimeFormat, true
	case "duplicate_datetime_strftime":
		return strconv.FormatBool(s.DuplicateDateTimeStrftime), true
	case "duplicate_datetime_use_unix":
		return strconv.FormatBool(s.DuplicateDateTimeUseUnix), true
	case "duplicate_count":
		return strconv.Itoa(s.DuplicateCount), true
	case "duplicate_count_max":
		return strconv.Itoa(s.DuplicateCountMax), true
	case "copy_preserve_attributes":
		return strconv.FormatBool(s.CopyPreserveAttributes), true
	case "move_preserve_attributes":
		return strconv.FormatBool(s.MovePreserveAttributes), true
	case "copy_follow_symlinks":
		return strconv.FormatBool(s.CopyFollowSymlinks), true
	case "move_follow_symlinks":
		return strconv.FormatBool(s.MoveFollowSymlinks), true
	case "copy_auto_merge_directories":
		return strconv.FormatBool(s.CopyAutoMergeDirectories), true
	case "move_auto_merge_directories":
		return strconv.FormatBool(s.MoveAutoMergeDirectories), true
	case "copy_stable_symlinks":
		return strconv.FormatBool(s.CopyStableSymlinks), true
	case "move_stable_symlinks":
		return strconv.FormatBool(s.MoveStableSymlinks), true
	case "remote_archive_confirm_size":
		return config.FormatByteSize(s.RemoteArchiveConfirmSize), true
	case "compress_format":
		return s.CompressFormat, true
	case "open_with_command":
		return s.OpenWithCommand, true
	case "rsync_copy_contents":
		return strconv.FormatBool(s.RsyncCopyContents), true
	case "rsync_archive":
		return strconv.FormatBool(s.RsyncArchive), true
	case "rsync_compress":
		return strconv.FormatBool(s.RsyncCompress), true
	case "rsync_delete":
		return strconv.FormatBool(s.RsyncDelete), true
	case "rsync_dry_run":
		return strconv.FormatBool(s.RsyncDryRun), true
	case "sed_regex":
		return strconv.FormatBool(s.SedRegex), true
	case "sed_extended_regex":
		return strconv.FormatBool(s.SedExtendedRegex), true
	case "sed_case_insensitive":
		return strconv.FormatBool(s.SedCaseInsensitive), true
	case "sed_global":
		return strconv.FormatBool(s.SedGlobal), true
	case "sed_backup":
		return strconv.FormatBool(s.SedBackup), true
	case "search_engine":
		return s.SearchEngine, true
	case "search_shell_patterns":
		return strconv.FormatBool(s.SearchShellPatterns), true
	case "search_case_sensitive":
		return strconv.FormatBool(s.SearchCaseSensitive), true
	case "search_skip_hidden":
		return strconv.FormatBool(s.SearchSkipHidden), true
	case "log_level":
		return s.LogLevel, true
	case "log_category_fileops":
		return strconv.FormatBool(s.LogCategoryFileOps), true
	case "log_category_permissions":
		return strconv.FormatBool(s.LogCategoryPermissions), true
	case "log_category_archive":
		return strconv.FormatBool(s.LogCategoryArchive), true
	case "log_category_textops":
		return strconv.FormatBool(s.LogCategoryTextOps), true
	case "log_category_rsync":
		return strconv.FormatBool(s.LogCategoryRsync), true
	case "log_category_remote":
		return strconv.FormatBool(s.LogCategoryRemote), true
	case "log_category_shell":
		return strconv.FormatBool(s.LogCategoryShell), true
	case "log_category_firewall":
		return strconv.FormatBool(s.LogCategoryFirewall), true
	case "firewall_default_direction":
		return s.FirewallDefaultDirection, true
	case "firewall_default_action":
		return s.FirewallDefaultAction, true
	case "firewall_default_protocol":
		return s.FirewallDefaultProtocol, true
	}
	return "", false
}
