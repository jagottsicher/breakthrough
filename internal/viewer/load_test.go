package viewer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	content := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Load(path, DefaultPreviewLimit)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindText {
		t.Errorf("Kind = %v, want KindText", result.Kind)
	}
	if result.Content != content {
		t.Errorf("Content = %q, want %q", result.Content, content)
	}
	if result.Truncated {
		t.Error("Truncated = true for a file well within the limit")
	}
}

func TestLoadBinaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image.bin")
	content := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Load(path, DefaultPreviewLimit)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindUnsupported {
		t.Errorf("Kind = %v, want KindUnsupported", result.Kind)
	}
	if result.Content != "" {
		t.Error("Content should be empty for an unsupported Kind")
	}
}

func TestLoadTruncatesLargeTextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.log")
	content := strings.Repeat("line\n", 100)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Load(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindText {
		t.Errorf("Kind = %v, want KindText", result.Kind)
	}
	if !result.Truncated {
		t.Error("Truncated = false for a file larger than limit")
	}
	if len(result.Content) != 10 {
		t.Errorf("len(Content) = %d, want 10", len(result.Content))
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist"), DefaultPreviewLimit)
	if err == nil {
		t.Fatal("want an error for a missing file, got nil")
	}
}

func TestLoadNulBeyondSniffLenStillDetectedAsText(t *testing.T) {
	// A NUL byte past Sniff's own inspection window (sniffLen) doesn't
	// retroactively flip an otherwise-text file to KindUnsupported —
	// Load only ever sniffs the file's own first sniffLen bytes,
	// deliberately (see Sniff's own doc comment on why re-scanning the
	// whole file isn't worth it).
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.txt")
	content := strings.Repeat("a", sniffLen+10) + "\x00" + "trailer"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Load(path, DefaultPreviewLimit)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindText {
		t.Errorf("Kind = %v, want KindText (NUL is beyond sniffLen)", result.Kind)
	}
}
