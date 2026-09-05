package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSettingDocsCoverEveryRecognizedKey is the drift guard between
// SettingDocs' own table and Settings.apply's own switch: every key one
// knows about, the other must too. Without this, adding a setting to
// apply but forgetting SettingDocs would silently leave it out of both
// the generated config template and the Options screen, with nothing
// failing to say so.
//
// Checks apply's side by feeding each documented key its own documented
// default: a key apply doesn't recognize comes back as an "unknown key"
// error, and a default apply rejects means the two disagree about the
// value's own shape.
func TestSettingDocsCoverEveryRecognizedKey(t *testing.T) {
	for _, doc := range SettingDocs() {
		s := DefaultSettings()
		if err := s.apply(doc.Key, doc.Default); err != nil {
			t.Errorf("apply(%q, %q) = %v, want it accepted — SettingDocs and apply disagree", doc.Key, doc.Default, err)
		}
	}
}

// TestSettingDocsDefaultsMatchDefaultSettings pins that every documented
// default really is the built-in one, rather than a literal that drifted
// — applying a doc's own default to a zero Settings must reproduce
// exactly what DefaultSettings would have given.
func TestSettingDocsDefaultsMatchDefaultSettings(t *testing.T) {
	s := Settings{}
	for _, doc := range SettingDocs() {
		if err := s.apply(doc.Key, doc.Default); err != nil {
			t.Fatalf("apply(%q, %q): %v", doc.Key, doc.Default, err)
		}
	}
	if want := DefaultSettings(); s != want {
		t.Errorf("applying every documented default produced %+v, want %+v", s, want)
	}
}

// TestSettingDocsMarkTheUnimplementedKey pins that "language" — parsed
// but acted on nowhere (see the package doc) — is flagged as such, so
// the Options screen can hide it instead of offering a control that
// visibly does nothing.
func TestSettingDocsMarkTheUnimplementedKey(t *testing.T) {
	doc, ok := FindSettingDoc("language")
	if !ok {
		t.Fatal("no SettingDoc for \"language\"")
	}
	if doc.Implemented {
		t.Error("language is marked Implemented, but nothing reads Settings.Language")
	}

	for _, d := range SettingDocs() {
		if d.Key != "language" && !d.Implemented {
			t.Errorf("%q is marked unimplemented — if that's real, the Options screen needs to hide it too", d.Key)
		}
	}
}

// TestDefaultFileTemplateListsEverySettingCommentedOut pins the two
// things the generated file has to get right: every recognized key is
// present, and none of them is active (an active line would pin that
// value into the user tier, overriding any system default set later —
// see DefaultFileTemplate's own doc comment).
func TestDefaultFileTemplateListsEverySettingCommentedOut(t *testing.T) {
	template := DefaultFileTemplate()

	for _, doc := range SettingDocs() {
		want := "# " + doc.Key + " = " + doc.Default
		if !strings.Contains(template, want) {
			t.Errorf("template is missing %q\n\ngot:\n%s", want, template)
		}
	}

	for _, line := range strings.Split(template, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		t.Errorf("template has an active (uncommented) line: %q", line)
	}
}

// TestDefaultFileTemplateParsesAsAnEmptyConfig is the end-to-end
// consequence of the test above: because everything is commented out,
// feeding the generated file straight back through the real parser must
// set nothing at all and produce no warnings.
func TestDefaultFileTemplateParsesAsAnEmptyConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(DefaultFileTemplate()), 0o644); err != nil {
		t.Fatalf("writing template: %v", err)
	}

	values, warnings, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("parsing the generated template produced warnings: %v", warnings)
	}
	if len(values) != 0 {
		t.Errorf("the generated template set %d values, want none (all commented out): %v", len(values), values)
	}
}

// TestEnsureUserFileCreatesTheTemplate pins the first-run case: no
// config file yet means one gets written, containing the full listing.
func TestEnsureUserFileCreatesTheTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config")

	if err := EnsureUserFile(path); err != nil {
		t.Fatalf("EnsureUserFile: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading created file: %v", err)
	}
	if !strings.Contains(string(data), "# color_scheme = ") {
		t.Errorf("created file doesn't look like the template:\n%s", data)
	}
}

// TestEnsureUserFileLeavesAnExistingFileAlone pins the other half:
// whatever is already in the user's own config is theirs, including
// deliberate deletions — this must never overwrite or "restore" it.
func TestEnsureUserFileLeavesAnExistingFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	original := "show_hidden = false\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	if err := EnsureUserFile(path); err != nil {
		t.Fatalf("EnsureUserFile: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if string(data) != original {
		t.Errorf("EnsureUserFile modified an existing file:\ngot:  %q\nwant: %q", data, original)
	}
}
