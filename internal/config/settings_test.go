package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseFileMissingFileIsEmpty(t *testing.T) {
	values, warnings, err := ParseFile(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(values) != 0 {
		t.Errorf("values = %v, want empty", values)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
}

func TestParseFileParsesKeyValueSkipsCommentsAndBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, `# a comment
color_scheme = solarized

language=de
   # indented comment
`)

	values, warnings, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	want := map[string]string{"color_scheme": "solarized", "language": "de"}
	if len(values) != len(want) {
		t.Fatalf("values = %v, want %v", values, want)
	}
	for k, v := range want {
		if values[k] != v {
			t.Errorf("values[%q] = %q, want %q", k, values[k], v)
		}
	}
}

func TestParseFileWarnsOnMalformedLineButKeepsGoing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, "not a valid line\ncolor_scheme = solarized\n")

	values, warnings, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if values["color_scheme"] != "solarized" {
		t.Errorf("valid line after a malformed one should still parse, got values = %v", values)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly 1", warnings)
	}
	if !strings.Contains(warnings[0], "1:") {
		t.Errorf("warning %q should reference line 1", warnings[0])
	}
}

func TestLoadDefaultsWhenNeitherFileExists(t *testing.T) {
	dir := t.TempDir()
	s, warnings := Load(filepath.Join(dir, "system"), filepath.Join(dir, "user"))

	if s != DefaultSettings() {
		t.Errorf("Load() = %+v, want defaults %+v", s, DefaultSettings())
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
}

func TestLoadMergesSystemThenUserOverridesKeyWise(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system")
	userPath := filepath.Join(dir, "user")
	writeFile(t, systemPath, "color_scheme = system-scheme\nlanguage = de\n")
	writeFile(t, userPath, "color_scheme = user-scheme\n") // language left at the system tier's value

	s, warnings := Load(systemPath, userPath)
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if s.ColorScheme != "user-scheme" {
		t.Errorf("ColorScheme = %q, want the user tier's override %q", s.ColorScheme, "user-scheme")
	}
	if s.Language != "de" {
		t.Errorf("Language = %q, want the system tier's value %q (user didn't override it)", s.Language, "de")
	}
}

func TestLoadWarnsOnUnknownKey(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user")
	writeFile(t, userPath, "made_up_key = x\n")

	_, warnings := Load(filepath.Join(dir, "system"), userPath)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "made_up_key") {
		t.Errorf("warnings = %v, want one mentioning made_up_key", warnings)
	}
}

func TestSetKeyAppendsNewKeyToEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user", "config") // parent dir doesn't exist yet
	if err := SetKey(path, "color_scheme", "solarized"); err != nil {
		t.Fatalf("SetKey: %v", err)
	}

	values, _, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if values["color_scheme"] != "solarized" {
		t.Errorf("values = %v, want color_scheme = solarized", values)
	}
}

func TestSetKeyReplacesExistingAssignmentPreservingOtherLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, "# a header comment\ncolor_scheme = old\nlanguage = de\n")

	if err := SetKey(path, "color_scheme", "new"); err != nil {
		t.Fatalf("SetKey: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "# a header comment") {
		t.Errorf("comment should be preserved, got:\n%s", text)
	}
	if !strings.Contains(text, "language = de") {
		t.Errorf("unrelated key should be preserved, got:\n%s", text)
	}
	if strings.Contains(text, "color_scheme = old") {
		t.Errorf("old assignment should have been replaced, got:\n%s", text)
	}
	if !strings.Contains(text, "color_scheme = new") {
		t.Errorf("new assignment should be present, got:\n%s", text)
	}
	// Exactly one color_scheme line — not appended alongside the old one.
	if n := strings.Count(text, "color_scheme"); n != 1 {
		t.Errorf("color_scheme appears %d times, want exactly 1, got:\n%s", n, text)
	}
}

func TestSetKeyIgnoresCommentedOutAssignment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeFile(t, path, "# color_scheme = commented-out\n")

	if err := SetKey(path, "color_scheme", "new"); err != nil {
		t.Fatalf("SetKey: %v", err)
	}

	values, _, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if values["color_scheme"] != "new" {
		t.Errorf("values = %v, want color_scheme = new (the comment shouldn't have been treated as the existing assignment)", values)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# color_scheme = commented-out") {
		t.Errorf("the comment line should be preserved untouched, got:\n%s", data)
	}
}

func TestSetKeyThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "config")

	if err := SetKey(userPath, "color_scheme", "solarized"); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	s, warnings := Load(filepath.Join(dir, "system-does-not-exist"), userPath)
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if s.ColorScheme != "solarized" {
		t.Errorf("ColorScheme = %q, want %q", s.ColorScheme, "solarized")
	}
}
