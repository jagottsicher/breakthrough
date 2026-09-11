package config

import (
	"fmt"
	"strconv"
	"strings"
)

// SettingKind is what shape a recognized key's value has — what a
// settings UI needs to know to render and edit it, and what
// DefaultFileTemplate uses to phrase each entry's own comment.
//
// Deliberately coarse: this is about how a value is *entered*, not what
// it means. KindEnum's own allowed values aren't listed here either —
// color_scheme's depend on which scheme files exist on disk right now
// (see LoadColorSchemes), which this package can't answer without being
// told where to look.
type SettingKind int

const (
	// KindBool is a "true"/"false" key (anything strconv.ParseBool
	// accepts on the way in — see Settings.apply — but always written
	// back in the canonical form).
	KindBool SettingKind = iota
	// KindInt is a plain integer key.
	KindInt
	// KindEnum is a string key with a fixed or discoverable set of
	// accepted values.
	KindEnum
	// KindString is a free-form string key.
	KindString
)

// SettingDoc describes one recognized config key: its name, the
// built-in default rendered exactly as it would be written to a config
// file, what kind of value it takes, and a one-line summary.
//
// This is the single place the key list, their defaults, and their
// short descriptions live. Default is derived from DefaultSettings
// rather than repeated as a literal (see SettingDocs), so the two can
// never drift apart — a silent, easy-to-miss class of bug for exactly
// this kind of parallel table.
//
// Implemented is false for a key the parser accepts but nothing acts on
// yet (currently only "language" — see the package doc). Kept in the
// list rather than omitted, so a hand-edited config file's own template
// still documents the complete set of keys Load won't warn about; a
// settings UI is expected to hide these rather than offer a control
// that visibly does nothing.
type SettingDoc struct {
	Key         string
	Default     string
	Kind        SettingKind
	Summary     string
	Implemented bool
}

// SettingDocs returns every recognized config key, in the order a
// generated config file lists them (see DefaultFileTemplate) — grouped
// by topic rather than alphabetically, since that's how someone reading
// the file will actually look for them.
//
// Every Default here is read from DefaultSettings rather than written
// out again as a literal, so adding or changing a default only ever has
// to happen in one place. TestSettingDocsCoverEveryRecognizedKey pins
// that this list and Settings.apply's own switch stay in agreement.
func SettingDocs() []SettingDoc {
	d := DefaultSettings()
	return []SettingDoc{
		{
			Key:         "color_scheme",
			Default:     d.ColorScheme,
			Kind:        KindEnum,
			Summary:     "Active color scheme (the filename stem of a colorschemes/*.json file).",
			Implemented: true,
		},
		{
			Key:         "show_hidden",
			Default:     strconv.FormatBool(d.ShowHidden),
			Kind:        KindBool,
			Summary:     "Show dotfiles and dot-directories in the listing.",
			Implemented: true,
		},
		{
			Key:         "size_bytes",
			Default:     strconv.FormatBool(d.SizeBytes),
			Kind:        KindBool,
			Summary:     "Show the Size column as exact bytes instead of a human-readable shorthand.",
			Implemented: true,
		},
		{
			Key:         "mtime_unix",
			Default:     strconv.FormatBool(d.MtimeUnix),
			Kind:        KindBool,
			Summary:     "Show the time column as a Unix timestamp instead of a formatted date.",
			Implemented: true,
		},
		{
			Key:         "restore_tabs",
			Default:     strconv.FormatBool(d.RestoreTabs),
			Kind:        KindBool,
			Summary:     "Reopen the tabs that were open when breakthrough last exited.",
			Implemented: true,
		},
		{
			Key:         "split_stacked",
			Default:     strconv.FormatBool(d.SplitStacked),
			Kind:        KindBool,
			Summary:     "Split view stacks its two panes above each other instead of side by side.",
			Implemented: true,
		},
		{
			Key:         "mouse_enabled",
			Default:     strconv.FormatBool(d.MouseEnabled),
			Kind:        KindBool,
			Summary:     "Whether mouse reporting is on at startup (clicks/drags work, but the terminal's own native text selection doesn't).",
			Implemented: true,
		},
		{
			Key:         "filter_persistent",
			Default:     strconv.FormatBool(d.FilterPersistent),
			Kind:        KindBool,
			Summary:     "Keep the filter menu's own text/size/modified-time filters active when you navigate into a different directory.",
			Implemented: true,
		},
		{
			Key:         "chord_timeout_ms",
			Default:     strconv.Itoa(d.ChordTimeoutMS),
			Kind:        KindInt,
			Summary:     "How long, in milliseconds, a chord's second key (gg, oo, ...) stays live for.",
			Implemented: true,
		},
		{
			Key:         "duplicate_separator",
			Default:     d.DuplicateSeparator,
			Kind:        KindString,
			Summary:     "Sits between the original name and Duplicate's own suffix.",
			Implemented: true,
		},
		{
			Key:         "duplicate_strategy",
			Default:     d.DuplicateStrategy,
			Kind:        KindEnum,
			Summary:     `How Duplicate names a copy: "suffix_text", "numbered", or "datetime".`,
			Implemented: true,
		},
		{
			Key:         "duplicate_suffix_text",
			Default:     d.DuplicateSuffixText,
			Kind:        KindString,
			Summary:     `Duplicate's own fixed suffix text, used only by the "suffix_text" strategy.`,
			Implemented: true,
		},
		{
			Key:         "duplicate_number_padding",
			Default:     strconv.Itoa(d.DuplicateNumberPadding),
			Kind:        KindInt,
			Summary:     `Zero-padding width for the "numbered" strategy's own number. 0 disables padding.`,
			Implemented: true,
		},
		{
			Key:         "duplicate_datetime_format",
			Default:     d.DuplicateDateTimeFormat,
			Kind:        KindString,
			Summary:     `Format string for the "datetime" strategy, interpreted per duplicate_datetime_strftime.`,
			Implemented: true,
		},
		{
			Key:         "duplicate_datetime_strftime",
			Default:     strconv.FormatBool(d.DuplicateDateTimeStrftime),
			Kind:        KindBool,
			Summary:     "Interpret duplicate_datetime_format as a strftime-style format instead of Go's reference layout.",
			Implemented: true,
		},
		{
			Key:         "duplicate_datetime_use_unix",
			Default:     strconv.FormatBool(d.DuplicateDateTimeUseUnix),
			Kind:        KindBool,
			Summary:     "Use a raw Unix timestamp instead of a formatted date for the \"datetime\" strategy.",
			Implemented: true,
		},
		{
			Key:         "duplicate_count",
			Default:     strconv.Itoa(d.DuplicateCount),
			Kind:        KindInt,
			Summary:     "How many duplicates one Duplicate invocation creates at once.",
			Implemented: true,
		},
		{
			Key:         "duplicate_count_max",
			Default:     strconv.Itoa(d.DuplicateCountMax),
			Kind:        KindInt,
			Summary:     "Upper bound duplicate_count itself can be set to.",
			Implemented: true,
		},
		{
			Key:         "pager",
			Default:     d.Pager,
			Kind:        KindEnum,
			Summary:     `How "Look" renders a file: "builtin" or "external" (bat/less/$PAGER/more).`,
			Implemented: true,
		},
		{
			Key:         "trash_confirm",
			Default:     strconv.FormatBool(d.TrashConfirm),
			Kind:        KindBool,
			Summary:     "Ask for confirmation before Move to Trash, the same way Remove permanently always has.",
			Implemented: true,
		},
		{
			Key:         "trash_persistent",
			Default:     strconv.FormatBool(d.TrashPersistent),
			Kind:        KindBool,
			Summary:     "Keep trashed files across login sessions instead of discarding them with the session.",
			Implemented: true,
		},
		{
			Key:         "trash_max_age_days",
			Default:     strconv.Itoa(d.TrashMaxAgeDays),
			Kind:        KindInt,
			Summary:     "Remove trashed items older than this many days at startup. 0 disables age pruning.",
			Implemented: true,
		},
		{
			Key:         "trash_quota_percent",
			Default:     strconv.Itoa(d.TrashQuotaPercent),
			Kind:        KindInt,
			Summary:     "Keep the trash at or under this percentage of its filesystem. 0 disables quota pruning.",
			Implemented: true,
		},
		{
			Key:         "language",
			Default:     d.Language,
			Kind:        KindString,
			Summary:     "Reserved for future translations. Parsed but currently has no effect.",
			Implemented: false,
		},
		{
			Key:         "copy_preserve_attributes",
			Default:     strconv.FormatBool(d.CopyPreserveAttributes),
			Kind:        KindBool,
			Summary:     "Copy jobs preserve the source's permissions/ownership/mtime on the destination.",
			Implemented: true,
		},
		{
			Key:         "move_preserve_attributes",
			Default:     strconv.FormatBool(d.MovePreserveAttributes),
			Kind:        KindBool,
			Summary:     "Move jobs preserve the source's permissions/ownership/mtime on the destination.",
			Implemented: true,
		},
		{
			Key:         "copy_follow_symlinks",
			Default:     strconv.FormatBool(d.CopyFollowSymlinks),
			Kind:        KindBool,
			Summary:     "Copy jobs dereference a symlink by default, writing a real copy of its target instead of a new link.",
			Implemented: true,
		},
		{
			Key:         "move_follow_symlinks",
			Default:     strconv.FormatBool(d.MoveFollowSymlinks),
			Kind:        KindBool,
			Summary:     "Move jobs (cut/paste) dereference a symlink by default, writing a real copy of its target instead of a new link.",
			Implemented: true,
		},
		{
			Key:         "copy_auto_merge_directories",
			Default:     strconv.FormatBool(d.CopyAutoMergeDirectories),
			Kind:        KindBool,
			Summary:     "Copy jobs resolve a directory-vs-directory paste conflict as a merge automatically, without asking.",
			Implemented: true,
		},
		{
			Key:         "move_auto_merge_directories",
			Default:     strconv.FormatBool(d.MoveAutoMergeDirectories),
			Kind:        KindBool,
			Summary:     "Move jobs resolve a directory-vs-directory paste conflict as a merge automatically, without asking.",
			Implemented: true,
		},
		{
			Key:         "copy_stable_symlinks",
			Default:     strconv.FormatBool(d.CopyStableSymlinks),
			Kind:        KindBool,
			Summary:     "Copy jobs rewrite a symlink's target to the new location if it points inside the tree being copied.",
			Implemented: true,
		},
		{
			Key:         "move_stable_symlinks",
			Default:     strconv.FormatBool(d.MoveStableSymlinks),
			Kind:        KindBool,
			Summary:     "Move jobs rewrite a symlink's target to the new location if it points inside the tree being moved.",
			Implemented: true,
		},
	}
}

// FindSettingDoc returns the SettingDoc for key, if it's a recognized
// one.
func FindSettingDoc(key string) (SettingDoc, bool) {
	for _, doc := range SettingDocs() {
		if doc.Key == key {
			return doc, true
		}
	}
	return SettingDoc{}, false
}

// defaultFileHeader introduces a generated config file. Deliberately
// explains the two-tier merge and the commented-out convention up
// front: someone opening this file for the first time is exactly the
// person who doesn't know either yet.
//
// Deliberately tier-neutral ("this file", never "your personal config"):
// the exact same template is EnsureUserFile's own text for a user's
// first config AND, via cmd/gen-etc-config, what the .deb/.rpm packages
// ship as /etc/breakthrough/config — the system tier, not owned by
// whichever one person happens to be reading it. Wording that assumed
// the user tier would read as flatly wrong on a machine an
// administrator set this up for other people to log into.
const defaultFileHeader = `# breakthrough configuration
#
# This file lists every setting breakthrough recognizes, commented out,
# showing its built-in default. There are two tiers of this file:
# /etc/breakthrough/config (system-wide) and ~/.config/breakthrough/config
# (or $XDG_CONFIG_HOME/breakthrough/config) for one user's own overrides.
# A value set in the user tier wins; the system tier wins over
# breakthrough's own built-in default below that.
#
# Uncomment a line and change its value to set it in *this* file.
# Deleting a line (or commenting it out again) restores whatever the
# tier below this one would have used — which is exactly what the
# Options screen's own "reset" does.
#
# Format: one "key = value" per line. Lines starting with "#" are
# comments. Booleans accept true/false (also 1/0, yes/no).
`

// DefaultFileTemplate renders a complete config file in which every
// recognized setting appears, commented out, at its built-in default —
// the starting point EnsureUserFile writes for a user who has never had
// one, so opening it in an editor shows the full set of available
// settings rather than an empty file.
//
// Commented out rather than active on purpose: an active line would
// pin that value into the user tier permanently, silently overriding
// any system-wide default an administrator set later. A commented line
// documents without deciding.
func DefaultFileTemplate() string {
	var b strings.Builder
	b.WriteString(defaultFileHeader)

	for _, doc := range SettingDocs() {
		b.WriteString("\n# ")
		b.WriteString(doc.Summary)
		if !doc.Implemented {
			b.WriteString("\n# NOTE: not implemented yet — setting this currently does nothing.")
		}
		fmt.Fprintf(&b, "\n# %s = %s\n", doc.Key, doc.Default)
	}
	return b.String()
}
