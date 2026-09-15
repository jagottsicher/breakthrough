package compare

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeAt(t *testing.T, path string, content []byte, when time.Time) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func TestCompareFilesReportsSizeAndModTimeAgreement(t *testing.T) {
	dir := t.TempDir()
	when := time.Now().Truncate(time.Second)
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	writeAt(t, a, []byte("hello"), when)
	writeAt(t, b, []byte("hello"), when)

	fc, err := CompareFiles(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !fc.SizeEqual || !fc.ModTimeEqual {
		t.Errorf("fc = %+v, want both equal", fc)
	}
	if fc.A.Size != 5 || fc.B.Size != 5 {
		t.Errorf("sizes = %d/%d, want 5/5", fc.A.Size, fc.B.Size)
	}
}

func TestCompareFilesFlagsSizeAndModTimeDisagreement(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	writeAt(t, a, []byte("hello"), time.Now())
	writeAt(t, b, []byte("hello world"), time.Now().Add(time.Hour))

	fc, err := CompareFiles(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if fc.SizeEqual || fc.ModTimeEqual {
		t.Errorf("fc = %+v, want both disagreeing", fc)
	}
}

func TestCompareFilesErrorsOnAMissingPath(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	writeAt(t, a, []byte("x"), time.Now())
	if _, err := CompareFiles(a, filepath.Join(dir, "nope.txt")); err == nil {
		t.Error("expected an error for a missing second path")
	}
}
