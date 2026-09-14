package batchrename

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A preset is one saved Rules, kept as its own small JSON file so a
// pipeline someone builds up once ("strip the camera prefix, number
// from 1, lowercase the extension") can be brought back by name next
// week rather than re-entered — the same reason Total Commander's
// Multi-Rename Tool has its own preset list. One file per preset,
// named after the preset, under the directory the caller passes in
// (internal/ui uses the rename-presets/ subdirectory of the user's own
// config directory) — the same one-file-per-thing layout color schemes
// already use, so the presets can be copied between machines, kept
// under version control, or rolled out system-wide by hand, without
// touching any other file.

// presetExt is the file extension every preset file carries.
const presetExt = ".json"

// Preset is one saved pipeline: its name (the filename stem) and the
// Rules it holds.
type Preset struct {
	Name  string
	Rules Rules
}

// ValidatePresetName rejects a name that can't be a plain filename stem
// in the preset directory: empty, or containing a path separator or a
// leading dot (which would hide the file from the listing, and from
// the user). Anything else — spaces, umlauts, punctuation — is fine;
// this is a label, not an identifier.
func ValidatePresetName(name string) error {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return errors.New("preset name is empty")
	case strings.ContainsAny(name, `/\`):
		return errors.New("preset name can't contain a path separator")
	case strings.HasPrefix(name, "."):
		return errors.New("preset name can't start with a dot")
	}
	return nil
}

// presetPath is the file a preset of that name lives in.
func presetPath(dir, name string) string {
	return filepath.Join(dir, strings.TrimSpace(name)+presetExt)
}

// SavePreset writes rules as the preset called name into dir, creating
// dir if needed and overwriting a preset of the same name — saving
// under an existing name is how one is updated. The caller is expected
// to have asked about that overwrite first if it cares (see
// internal/ui's own save flow); this just writes.
func SavePreset(dir, name string, rules Rules) error {
	if err := ValidatePresetName(name); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating preset directory: %w", err)
	}
	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding preset: %w", err)
	}
	if err := os.WriteFile(presetPath(dir, name), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing preset: %w", err)
	}
	return nil
}

// PresetExists reports whether a preset called name is already in dir.
func PresetExists(dir, name string) bool {
	_, err := os.Stat(presetPath(dir, name))
	return err == nil
}

// LoadPresets reads every preset in dir, sorted by name
// (case-insensitively). A missing dir is simply no presets, not an
// error. A file that isn't valid JSON, or names an enum spelling this
// version doesn't know (see enums.go), is skipped and reported through
// the returned error — the good ones still load, the same way one
// unreadable color scheme doesn't take the others down with it.
func LoadPresets(dir string) ([]Preset, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading preset directory: %w", err)
	}

	var (
		presets  []Preset
		firstErr error
	)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), presetExt) || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), presetExt)
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("preset %q: %w", name, err)
			}
			continue
		}
		var rules Rules
		if err := json.Unmarshal(data, &rules); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("preset %q: %w", name, err)
			}
			continue
		}
		presets = append(presets, Preset{Name: name, Rules: rules})
	}
	sort.SliceStable(presets, func(i, j int) bool {
		return strings.ToLower(presets[i].Name) < strings.ToLower(presets[j].Name)
	})
	return presets, firstErr
}

// DeletePreset removes the preset called name from dir. Deleting one
// that isn't there is not an error — the outcome is the same.
func DeletePreset(dir, name string) error {
	if err := ValidatePresetName(name); err != nil {
		return err
	}
	if err := os.Remove(presetPath(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("deleting preset: %w", err)
	}
	return nil
}
