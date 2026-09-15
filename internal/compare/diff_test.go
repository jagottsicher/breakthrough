package compare

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnifiedDiffOnIdenticalFiles(t *testing.T) {
	if !Available() {
		t.Skip("diff(1) not on PATH")
	}
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("same\ncontent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("same\ncontent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output, identical, err := UnifiedDiff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !identical || output != "" {
		t.Errorf("identical=%v output=%q, want true and empty", identical, output)
	}
}

func TestUnifiedDiffOnDifferingFiles(t *testing.T) {
	if !Available() {
		t.Skip("diff(1) not on PATH")
	}
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("line one\nline TWO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output, identical, err := UnifiedDiff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if identical {
		t.Fatal("identical=true for two files that actually differ")
	}
	if !strings.Contains(output, "-line two") || !strings.Contains(output, "+line TWO") {
		t.Errorf("output missing expected unified-diff lines:\n%s", output)
	}
}

func TestUnifiedDiffReportsARealErrorNotAsADifference(t *testing.T) {
	if !Available() {
		t.Skip("diff(1) not on PATH")
	}
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(a, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := UnifiedDiff(a, filepath.Join(dir, "does-not-exist.txt"))
	if err == nil {
		t.Error("expected an error comparing against a nonexistent file")
	}
}
