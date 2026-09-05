package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ParseFile reads path as a flat "key = value" file with "#"-prefixed
// comment lines and blank lines ignored (see the package doc for the
// format's own rationale). A missing file is not an error — both the
// system and user config files are optional layers, and Load relies on
// this to treat "not present yet" the same as "present but empty".
//
// Malformed lines (no "=", or an empty key) are skipped rather than
// aborting the whole parse — one typo shouldn't cost every other line —
// but reported back as warnings so a caller can still surface them (see
// cmd/breakthrough).
func ParseFile(path string) (values map[string]string, warnings []string, err error) {
	values = map[string]string{}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return values, nil, nil
		}
		return nil, nil, err
	}
	defer func() {
		// Only overrides err if reading itself succeeded — a failure
		// there (or a malformed line, surfaced as a warning instead) is
		// the more relevant one to report, the same convention
		// appendBashHistory's own deferred Close uses.
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			warnings = append(warnings, fmt.Sprintf("%s:%d: expected \"key = value\", got %q", path, lineNo, line))
			continue
		}
		values[key] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, warnings, err
	}
	return values, warnings, nil
}

// Settings is breakthrough's own recognized set of "key = value" keys
// (see ParseFile):
//
//   - color_scheme: the slug (JSON filename stem) of the active color
//     scheme, resolved via LoadColorSchemes/FindColorScheme.
//   - language: a reserved placeholder (see the package doc) — no
//     translated strings exist behind it yet, kept here now so adding
//     real i18n later doesn't need a config schema break.
//   - show_hidden, size_bytes, mtime_unix: the panel's own "Globals"
//     toggles (see internal/ui's Panel.showHidden/sizeBytes/mtimeUnix),
//     persisted so breakthrough starts up remembering the last
//     session's choice instead of always resetting to the built-in
//     default — per the user's own request. Boolean values accept
//     anything strconv.ParseBool does ("true"/"false", "1"/"0", ...) —
//     SetKey itself always writes the canonical "true"/"false" form.
//   - pager: which of Look's two rendering paths internal/ui uses (see
//     its own openLook/showBuiltinLook/runExternalPager) — "builtin"
//     (the default: breakthrough's own dependency-free text viewer) or
//     "external" (bat/less/$PAGER/more, via internal/ui's
//     externalPagerCommand). Any value other than exactly "external" is
//     treated as "builtin" — the same forgiving, unvalidated-at-parse-
//     time handling color_scheme already gets (an unrecognized scheme
//     slug just falls back to Default via FindColorScheme, rather than
//     Load itself rejecting it).
//   - trash_persistent: whether "Move to Trash" (see internal/fsops'
//     MoveToTrash and internal/session's TrashDir) uses the persistent,
//     user-area trash (true, the default) or the session-scoped one
//     under $XDG_RUNTIME_DIR that disappears once breakthrough's
//     session ends (false) — persistent by default specifically so a
//     file trashed today is still there tomorrow even across a login
//     session boundary, kept from growing forever by trash_max_age_days/
//     trash_quota_percent below rather than by disappearing with the
//     session itself.
//   - trash_max_age_days: how many days a trashed item is kept before
//     PruneTrash (see internal/fsops, run once at startup — internal/
//     ui's pruneTrashAtStartup) removes it unconditionally, regardless
//     of free space. 30 by default. 0 disables age-based pruning
//     entirely.
//   - trash_quota_percent: the trash's own on-disk size is kept at or
//     under this percentage of the filesystem it lives on — a backstop
//     PruneTrash applies, oldest item first, only if trash_max_age_days
//     alone didn't already bring it back under quota. 10 by default. 0
//     disables quota-based pruning entirely.
//   - restore_tabs: whether breakthrough reopens the panel tabs that
//     were open when it last exited (see internal/session's SaveTabs/
//     LoadTabs and internal/ui's RestoreSavedTabs). true by default —
//     per the user's own explicit request that each tab remember its own
//     directory across a restart. Only ever consulted when breakthrough
//     is started with no explicit directory argument: "breakthrough
//     /some/path" is an unambiguous instruction about where to open, and
//     silently reopening yesterday's tabs on top of it would be the
//     wrong answer to it.
type Settings struct {
	ColorScheme       string
	Language          string
	ShowHidden        bool
	SizeBytes         bool
	MtimeUnix         bool
	Pager             string
	TrashPersistent   bool
	TrashMaxAgeDays   int
	TrashQuotaPercent int
	RestoreTabs       bool
}

// DefaultSettings is what a brand-new install has with neither config
// file present — ShowHidden true (dotfiles shown), SizeBytes/MtimeUnix
// false (human-readable size, formatted date), matching Panel's own
// prior hardcoded defaults exactly, so a fresh install behaves
// identically to before this existed.
func DefaultSettings() Settings {
	return Settings{
		ColorScheme:       "default",
		Language:          "en",
		ShowHidden:        true,
		SizeBytes:         false,
		MtimeUnix:         false,
		Pager:             "builtin",
		TrashPersistent:   true,
		TrashMaxAgeDays:   30,
		TrashQuotaPercent: 10,
		RestoreTabs:       true,
	}
}

// apply sets the one field key names to value, returning an error
// (without applying anything) if key isn't recognized at all, or if a
// boolean/integer key's value isn't one strconv.ParseBool/Atoi
// understands — Load turns either into a warning rather than silently
// ignoring a typo'd key or a malformed value.
func (s *Settings) apply(key, value string) error {
	parseBool := func(dst *bool) error {
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean for %q: %q", key, value)
		}
		*dst = b
		return nil
	}
	parseInt := func(dst *int) error {
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid integer for %q: %q", key, value)
		}
		*dst = n
		return nil
	}
	switch key {
	case "color_scheme":
		s.ColorScheme = value
	case "language":
		s.Language = value
	case "show_hidden":
		return parseBool(&s.ShowHidden)
	case "size_bytes":
		return parseBool(&s.SizeBytes)
	case "mtime_unix":
		return parseBool(&s.MtimeUnix)
	case "pager":
		s.Pager = value
	case "trash_persistent":
		return parseBool(&s.TrashPersistent)
	case "trash_max_age_days":
		return parseInt(&s.TrashMaxAgeDays)
	case "trash_quota_percent":
		return parseInt(&s.TrashQuotaPercent)
	case "restore_tabs":
		return parseBool(&s.RestoreTabs)
	default:
		return fmt.Errorf("unknown key %q", key)
	}
	return nil
}

// Load reads systemPath then userPath (see ParseFile — either may not
// exist) and merges them onto DefaultSettings key-by-key: user values
// override system values, which override the built-in defaults — the
// two-tier merge the package doc describes. Every parse warning from
// either file, plus one for each key/value apply rejects, is returned
// for the caller to surface (see cmd/breakthrough); none of them stop
// the merge — an unreadable or malformed config should degrade to
// defaults, not prevent the program from starting.
// Delegates to LoadWithOrigins (see origin.go) and discards the origin
// map — the merge itself is identical either way, and keeping one
// implementation means the two can't drift into disagreeing about which
// tier won.
func Load(systemPath, userPath string) (Settings, []string) {
	s, _, warnings := LoadWithOrigins(systemPath, userPath)
	return s, warnings
}

// sortedKeys returns values' own keys in sorted order — used to give
// the merge (and therefore the warnings it produces) a deterministic
// order, since ranging a map directly wouldn't have one.
func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SetKey updates key's value in path's own "key = value" file,
// preserving every other line — comments, ordering, blank lines —
// exactly as it was: an existing assignment line for key is replaced in
// place; if none exists, "key = value" is appended. The file and its
// parent directory are created if they don't exist yet (e.g. the user's
// config directory on a first run — see UserDir). Written via a temp
// file plus rename in the same directory, so a crash mid-write can't
// leave a half-written config behind.
func SetKey(path, key, value string) error {
	lines, err := readConfigLines(path)
	if err != nil {
		return err
	}

	assignment := key + " = " + value
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		k, _, ok := strings.Cut(trimmed, "=")
		if ok && strings.TrimSpace(k) == key {
			lines[i] = assignment
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, assignment)
	}

	return writeConfigLines(path, lines)
}

// UnsetKey removes key's own assignment line from path, preserving
// every other line exactly as it was — SetKey's counterpart, and what
// the Options screen's own "reset to default" runs.
//
// Removing the line rather than writing the built-in default back is
// the whole point: this file is only the *user* tier, so deleting the
// key lets the value fall back through the tiers to a system-wide
// setting where an administrator provided one, and only to the built-in
// default where nobody did (see Origin's own doc comment). Writing the
// built-in default as an active line instead would silently pin it,
// overriding a system default that "reset" should have restored.
//
// Removes every assignment for key, not just the first: a hand-edited
// file can legitimately contain the same key twice (the last one wins
// during the merge — see ParseFile, which builds a map), and leaving a
// stale earlier one behind would make the reset look like it silently
// failed. Commented-out lines are left untouched — they set nothing, and
// they're exactly the documentation DefaultFileTemplate put there.
//
// A missing file, or a key that isn't in it, is success with nothing to
// do — "make sure this key isn't set here" is already true.
func UnsetKey(path, key string) error {
	lines, err := readConfigLines(path)
	if err != nil {
		return err
	}

	kept := make([]string, 0, len(lines))
	removed := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			if k, _, ok := strings.Cut(trimmed, "="); ok && strings.TrimSpace(k) == key {
				removed = true
				continue
			}
		}
		kept = append(kept, line)
	}
	if !removed {
		return nil // nothing set here to begin with — don't rewrite the file for no reason
	}
	return writeConfigLines(path, kept)
}

// EnsureUserFile creates path with DefaultFileTemplate's own
// commented-out listing of every setting if it doesn't exist yet, and
// does nothing at all if it does.
//
// Called before handing the file to an external editor (see
// internal/ui's Options screen), so someone opening their config for
// the first time gets the full set of available settings to read and
// uncomment rather than an empty buffer with no clue what's valid.
// Never touches an existing file: whatever is in there is the user's,
// including deliberate deletions.
func EnsureUserFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeConfigLines(path, strings.Split(strings.TrimRight(DefaultFileTemplate(), "\n"), "\n"))
}

// readConfigLines reads path into individual lines, treating a missing
// file as an empty one — the shared front half of SetKey/UnsetKey.
//
// Drops the trailing empty element Split produces from the file's own
// final newline, so callers can append without landing after a blank
// line and writeConfigLines can re-add exactly one terminator.
func readConfigLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
	case os.IsNotExist(err):
		return nil, nil
	default:
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines, nil
}

// writeConfigLines writes lines to path as a newline-terminated file,
// creating the parent directory if needed — the shared back half of
// SetKey/UnsetKey/EnsureUserFile.
//
// Via a temp file plus rename in the same directory, so a crash
// mid-write can't leave a half-written config behind.
func writeConfigLines(path string, lines []string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".breakthrough-config-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	content := strings.Join(lines, "\n") + "\n"
	if _, err := tmp.WriteString(content); err != nil {
		// Best-effort cleanup: the write itself already failed, so
		// that's the error worth reporting — a failure removing the
		// half-written temp file too isn't worth masking it with (it
		// just leaves a stray file behind, not a correctness problem).
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
