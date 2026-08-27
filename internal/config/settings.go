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
//     user-area trash (true) or the session-scoped one under
//     $XDG_RUNTIME_DIR that disappears once breakthrough's session ends
//     (false, the default).
type Settings struct {
	ColorScheme     string
	Language        string
	ShowHidden      bool
	SizeBytes       bool
	MtimeUnix       bool
	Pager           string
	TrashPersistent bool
}

// DefaultSettings is what a brand-new install has with neither config
// file present — ShowHidden true (dotfiles shown), SizeBytes/MtimeUnix
// false (human-readable size, formatted date), matching Panel's own
// prior hardcoded defaults exactly, so a fresh install behaves
// identically to before this existed.
func DefaultSettings() Settings {
	return Settings{
		ColorScheme:     "default",
		Language:        "en",
		ShowHidden:      true,
		SizeBytes:       false,
		MtimeUnix:       false,
		Pager:           "builtin",
		TrashPersistent: false,
	}
}

// apply sets the one field key names to value, returning an error
// (without applying anything) if key isn't recognized at all, or if a
// boolean key's value isn't one strconv.ParseBool understands — Load
// turns either into a warning rather than silently ignoring a typo'd
// key or a malformed value.
func (s *Settings) apply(key, value string) error {
	parseBool := func(dst *bool) error {
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean for %q: %q", key, value)
		}
		*dst = b
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
func Load(systemPath, userPath string) (Settings, []string) {
	s := DefaultSettings()
	var warnings []string

	merge := func(path string) {
		values, parseWarnings, err := ParseFile(path)
		warnings = append(warnings, parseWarnings...)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", path, err))
			return
		}
		keys := make([]string, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys) // deterministic warning order
		for _, k := range keys {
			if err := s.apply(k, values[k]); err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", path, err))
			}
		}
	}
	merge(systemPath)
	merge(userPath)
	return s, warnings
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
	var lines []string
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		lines = strings.Split(string(data), "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1] // drop the blank entry from the file's final "\n"
		}
	case os.IsNotExist(err):
		// No existing file: lines starts empty, assignment gets appended below.
	default:
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
