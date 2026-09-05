package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfig is the shared fixture helper for the tests here: a config
// file at a fresh path with the given content.
func writeConfig(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

// TestLoadWithOriginsTracksWhichTierWon pins the whole point of the
// origin map: for each key, which of the two files (or neither) the
// effective value actually came from.
func TestLoadWithOriginsTracksWhichTierWon(t *testing.T) {
	dir := t.TempDir()
	system := writeConfig(t, dir, "system", "show_hidden = false\npager = external\n")
	user := writeConfig(t, dir, "user", "pager = builtin\n")

	settings, origins, warnings := LoadWithOrigins(system, user)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	// Set only system-wide: the system tier is the origin.
	if got := origins["show_hidden"]; got != OriginSystem {
		t.Errorf("show_hidden origin = %v, want OriginSystem", got)
	}
	if settings.ShowHidden {
		t.Error("show_hidden should be false, from the system tier")
	}

	// Set in both: the user tier wins, and is the origin.
	if got := origins["pager"]; got != OriginUser {
		t.Errorf("pager origin = %v, want OriginUser", got)
	}
	if settings.Pager != "builtin" {
		t.Errorf("pager = %q, want the user tier's %q", settings.Pager, "builtin")
	}

	// Set in neither: the built-in default is in force.
	if got := origins["restore_tabs"]; got != OriginDefault {
		t.Errorf("restore_tabs origin = %v, want OriginDefault", got)
	}
}

// TestLoadWithOriginsCoversEveryKnownKey pins that the map always has an
// entry for every recognized key, so a caller can index it without
// having to special-case a missing one.
func TestLoadWithOriginsCoversEveryKnownKey(t *testing.T) {
	dir := t.TempDir()
	_, origins, _ := LoadWithOrigins(
		filepath.Join(dir, "no-system"),
		filepath.Join(dir, "no-user"),
	)

	for _, doc := range SettingDocs() {
		if _, ok := origins[doc.Key]; !ok {
			t.Errorf("origins map has no entry for %q", doc.Key)
		}
	}
}

// TestLoadWithOriginsIgnoresARejectedValue pins the subtle case spelled
// out in LoadWithOrigins' own doc comment: a key present in a file but
// rejected by apply (a malformed boolean here) sets nothing, so the tier
// below must stay in force *and* stay credited as the origin — crediting
// the tier that actually failed would misreport where the value came
// from.
func TestLoadWithOriginsIgnoresARejectedValue(t *testing.T) {
	dir := t.TempDir()
	system := writeConfig(t, dir, "system", "show_hidden = false\n")
	user := writeConfig(t, dir, "user", "show_hidden = not-a-boolean\n")

	settings, origins, warnings := LoadWithOrigins(system, user)

	if len(warnings) == 0 {
		t.Error("a malformed boolean should have produced a warning")
	}
	if settings.ShowHidden {
		t.Error("show_hidden should still be the system tier's false, not the rejected user value")
	}
	if got := origins["show_hidden"]; got != OriginSystem {
		t.Errorf("show_hidden origin = %v, want OriginSystem (the rejected user value must not be credited)", got)
	}
}

// TestLoadAgreesWithLoadWithOrigins pins that the two entry points can't
// drift — Load delegates, so the merged result must be identical.
func TestLoadAgreesWithLoadWithOrigins(t *testing.T) {
	dir := t.TempDir()
	system := writeConfig(t, dir, "system", "show_hidden = false\ntrash_max_age_days = 7\n")
	user := writeConfig(t, dir, "user", "pager = external\n")

	viaLoad, loadWarnings := Load(system, user)
	viaOrigins, _, originWarnings := LoadWithOrigins(system, user)

	if viaLoad != viaOrigins {
		t.Errorf("Load = %+v, LoadWithOrigins = %+v — they must agree", viaLoad, viaOrigins)
	}
	if len(loadWarnings) != len(originWarnings) {
		t.Errorf("warning counts differ: Load %d, LoadWithOrigins %d", len(loadWarnings), len(originWarnings))
	}
}

// TestUnsetKeyRemovesTheAssignment pins reset's own core behaviour:
// the key's line goes away entirely (rather than being rewritten to the
// built-in default), so the value falls back through the tiers.
func TestUnsetKeyRemovesTheAssignment(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "config", "show_hidden = false\npager = external\n")

	if err := UnsetKey(path, "show_hidden"); err != nil {
		t.Fatalf("UnsetKey: %v", err)
	}

	values, _, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if _, still := values["show_hidden"]; still {
		t.Error("show_hidden is still set after UnsetKey")
	}
	if got := values["pager"]; got != "external" {
		t.Errorf("pager = %q, want the untouched %q — UnsetKey must only remove its own key", got, "external")
	}
}

// TestUnsetKeyKeepsCommentsAndTemplateLines pins that a reset doesn't
// strip the commented-out documentation DefaultFileTemplate wrote — those
// lines set nothing, and they're the whole reason the file is readable.
func TestUnsetKeyKeepsCommentsAndTemplateLines(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "config", "# a comment\n# show_hidden = true\nshow_hidden = false\n")

	if err := UnsetKey(path, "show_hidden"); err != nil {
		t.Fatalf("UnsetKey: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	got := string(data)
	if want := "# a comment\n# show_hidden = true\n"; got != want {
		t.Errorf("after UnsetKey the file is:\n%q\nwant:\n%q", got, want)
	}
}

// TestUnsetKeyRemovesEveryDuplicate pins the hand-edited-file case: the
// same key set twice must be fully removed, not just its first
// occurrence — leaving a stale earlier line behind would make the reset
// look like it silently failed.
func TestUnsetKeyRemovesEveryDuplicate(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "config", "show_hidden = true\npager = external\nshow_hidden = false\n")

	if err := UnsetKey(path, "show_hidden"); err != nil {
		t.Fatalf("UnsetKey: %v", err)
	}

	values, _, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if _, still := values["show_hidden"]; still {
		t.Error("a duplicated show_hidden survived UnsetKey")
	}
}

// TestUnsetKeyOnAMissingKeyOrFileIsSuccess pins that "make sure this
// isn't set here" is already true in both of those cases — reset must
// not fail just because there was nothing to undo.
func TestUnsetKeyOnAMissingKeyOrFileIsSuccess(t *testing.T) {
	dir := t.TempDir()

	if err := UnsetKey(filepath.Join(dir, "does-not-exist"), "show_hidden"); err != nil {
		t.Errorf("UnsetKey on a missing file = %v, want nil", err)
	}

	path := writeConfig(t, dir, "config", "pager = external\n")
	if err := UnsetKey(path, "show_hidden"); err != nil {
		t.Errorf("UnsetKey on an absent key = %v, want nil", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if got, want := string(data), "pager = external\n"; got != want {
		t.Errorf("file was rewritten for a no-op reset: got %q, want %q", got, want)
	}
}

// TestSetKeyThenUnsetKeyRoundTrip pins the two together as the Options
// screen actually uses them: change a value, then reset it, and the file
// is back to setting nothing.
func TestSetKeyThenUnsetKeyRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")

	if err := SetKey(path, "show_hidden", "false"); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	values, _, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if got := values["show_hidden"]; got != "false" {
		t.Fatalf("after SetKey, show_hidden = %q, want %q", got, "false")
	}

	if err := UnsetKey(path, "show_hidden"); err != nil {
		t.Fatalf("UnsetKey: %v", err)
	}
	values, _, err = ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if _, still := values["show_hidden"]; still {
		t.Error("show_hidden survived the round trip")
	}
}
