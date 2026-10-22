package filemeta

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestIsOLE2Path(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"report.doc", true},
		{"REPORT.DOC", true},
		{"budget.xls", true},
		{"slides.ppt", true},
		{"report.docx", false}, // Office Open XML, not OLE2 — see ForPath's own split
		{"photo.jpg", false},
		{"no-extension", false},
	} {
		if got := IsOLE2Path(tc.path); got != tc.want {
			t.Errorf("IsOLE2Path(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestIsDocumentPathIncludesOLE2(t *testing.T) {
	for _, path := range []string{"report.doc", "budget.xls", "slides.ppt"} {
		if !IsDocumentPath(path) {
			t.Errorf("IsDocumentPath(%q) = false, want true", path)
		}
	}
}

func TestForOLE2NonexistentFile(t *testing.T) {
	_, err := ForOLE2(filepath.Join(t.TempDir(), "does-not-exist.doc"))
	if err == nil {
		t.Error("ForOLE2 on a nonexistent file: want an error, got nil")
	}
}

func TestForOLE2NotACompoundFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake.doc")
	if err := os.WriteFile(path, []byte("this is not a real OLE2 compound file at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ForOLE2(path); err == nil {
		t.Error("ForOLE2 on a non-CFB file: want an error, got nil")
	}
}

// TestForOLE2RealFixture pins the real end-to-end path — mscfb/msoleps
// decoding a genuine Compound File's own SummaryInformation stream —
// against testdata/ole2-fixture.xls (see testdata/README.md for why
// this is a real file rather than a hand-built one, the same reasoning
// ForPDF's own real-file test already has, just not satisfiable the
// same way here). The fixture's own real properties (verified once by
// hand against it, not assumed): Author "Lehane, Richard", CreateTime
// 2013-05-28, LastSaveTime 2013-06-13 — no Title/Subject/AppName set
// at all, a realistic sparse real-world file, not a cherry-picked
// complete one.
func TestForOLE2RealFixture(t *testing.T) {
	got, err := ForOLE2(filepath.Join("testdata", "ole2-fixture.xls"))
	if err != nil {
		t.Fatalf("ForOLE2() error = %v, want nil", err)
	}

	byLabel := make(map[string]string, len(got))
	for _, f := range got {
		byLabel[f.Label] = f.Value
	}

	if byLabel["Author"] != "Lehane, Richard" {
		t.Errorf("Author = %q, want %q", byLabel["Author"], "Lehane, Richard")
	}
	if byLabel["Created"] != "2013-05-28 08:38:24" {
		t.Errorf("Created = %q, want %q", byLabel["Created"], "2013-05-28 08:38:24")
	}
	if byLabel["Modified"] != "2013-06-13 13:36:39" {
		t.Errorf("Modified = %q, want %q", byLabel["Modified"], "2013-06-13 13:36:39")
	}
	// Properties the fixture genuinely doesn't set must simply be
	// absent, not shown as empty-string noise — the same convention
	// ForOfficeOpenXML/ForEPUB's own "add" helpers already follow.
	for _, label := range []string{"Title", "Subject", "Application"} {
		if v, ok := byLabel[label]; ok {
			t.Errorf("%s = %q, want absent (the fixture never sets it)", label, v)
		}
	}
}

func TestForOLE2RealFixtureViaForPath(t *testing.T) {
	got, err := ForPath(filepath.Join("testdata", "ole2-fixture.xls"))
	if err != nil {
		t.Fatalf("ForPath() error = %v, want nil", err)
	}
	found := false
	for _, f := range got {
		if f.Label == "Author" && f.Value == "Lehane, Richard" {
			found = true
		}
	}
	if !found {
		t.Errorf("ForPath() = %+v, want it to have dispatched to ForOLE2 and found Author", got)
	}
}

func TestForOLE2RealFixtureRegardlessOfExtension(t *testing.T) {
	// ForOLE2 itself never looks at the extension — only ForPath's own
	// dispatch does (see its own doc comment: the container format
	// alone can't tell a DOC from an XLS from a PPT). Calling it
	// directly against the real .xls fixture under a renamed .doc
	// copy pins that this function's own eligibility is purely
	// content-based.
	src := filepath.Join("testdata", "ole2-fixture.xls")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join(t.TempDir(), "renamed-as.doc")
	if err := os.WriteFile(renamed, data, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ForOLE2(renamed)
	if err != nil {
		t.Fatalf("ForOLE2() error = %v, want nil", err)
	}
	if len(got) == 0 {
		t.Error("ForOLE2() on the renamed fixture found no fields, want the same ones as the original")
	}
}

func TestForOLE2ErrorIsNotWrappedWeirdly(t *testing.T) {
	_, err := ForOLE2(filepath.Join(t.TempDir(), "does-not-exist.doc"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ForOLE2 on a missing file: error = %v, want it to satisfy os.ErrNotExist", err)
	}
}
