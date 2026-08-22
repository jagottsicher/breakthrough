package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	defer f.Close()

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
// (see ParseFile) — deliberately small for now:
//
//   - color_scheme: the slug (JSON filename stem) of the active color
//     scheme, resolved via LoadColorSchemes/FindColorScheme.
//   - language: a reserved placeholder (see the package doc) — no
//     translated strings exist behind it yet, kept here now so adding
//     real i18n later doesn't need a config schema break.
type Settings struct {
	ColorScheme string
	Language    string
}

// DefaultSettings is what a brand-new install has with neither config
// file present.
func DefaultSettings() Settings {
	return Settings{ColorScheme: "default", Language: "en"}
}

// apply sets the one field key names to value, reporting false if key
// isn't recognized at all (see Load, which turns that into a warning
// rather than silently ignoring a typo'd key).
func (s *Settings) apply(key, value string) bool {
	switch key {
	case "color_scheme":
		s.ColorScheme = value
	case "language":
		s.Language = value
	default:
		return false
	}
	return true
}

// Load reads systemPath then userPath (see ParseFile — either may not
// exist) and merges them onto DefaultSettings key-by-key: user values
// override system values, which override the built-in defaults — the
// two-tier merge the package doc describes. Every parse warning from
// either file, plus one for each key neither Settings field recognizes,
// is returned for the caller to surface (see cmd/breakthrough); none of
// them stop the merge — an unreadable or malformed config should degrade
// to defaults, not prevent the program from starting.
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
			if !s.apply(k, values[k]) {
				warnings = append(warnings, fmt.Sprintf("%s: unknown key %q", path, k))
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
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
