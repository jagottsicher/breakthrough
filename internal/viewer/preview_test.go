package viewer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPreviewWithinLimitIsNotTruncated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.txt")
	content := "hello, world\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	data, truncated, err := ReadPreview(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Error("truncated = true for a file well within the limit")
	}
	if string(data) != content {
		t.Errorf("data = %q, want %q", data, content)
	}
}

func TestReadPreviewOverLimitIsTruncatedAtExactLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	content := strings.Repeat("x", 200)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	data, truncated, err := ReadPreview(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Error("truncated = false for a file larger than the limit")
	}
	if len(data) != 100 {
		t.Errorf("len(data) = %d, want exactly 100", len(data))
	}
	if string(data) != strings.Repeat("x", 100) {
		t.Error("data isn't the file's own first 100 bytes")
	}
}

func TestReadPreviewExactlyAtLimitIsNotTruncated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exact.txt")
	content := strings.Repeat("y", 100)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	data, truncated, err := ReadPreview(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Error("truncated = true for a file exactly at the limit")
	}
	if string(data) != content {
		t.Errorf("data = %q, want %q", data, content)
	}
}

func TestReadPreviewMissingFile(t *testing.T) {
	_, _, err := ReadPreview(filepath.Join(t.TempDir(), "does-not-exist"), 1024)
	if err == nil {
		t.Fatal("want an error for a missing file, got nil")
	}
}

func TestReadPreviewOnDirectory(t *testing.T) {
	dir := t.TempDir()
	_, _, err := ReadPreview(dir, 1024)
	if err == nil {
		t.Fatal("want an error when path is a directory, got nil")
	}
}
