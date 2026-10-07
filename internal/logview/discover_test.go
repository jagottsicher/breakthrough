package logview

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", name, err)
	}
}

func writeGzipFile(t *testing.T, dir, name, content string) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("Create %s: %v", name, err)
	}
	defer func() { _ = f.Close() }()
	gz := gzip.NewWriter(f)
	if _, err := gz.Write([]byte(content)); err != nil {
		t.Fatalf("gzip Write %s: %v", name, err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip Close %s: %v", name, err)
	}
}

func TestDiscoverGroupsLogrotateFamily(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "access.log", "current\n")
	writeFile(t, dir, "access.log.1", "yesterday\n")
	writeGzipFile(t, dir, "access.log.2.gz", "two days ago\n")
	writeFile(t, dir, "error.log", "errors\n")
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	groups, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("len(groups) = %d, want 2 (access.log, error.log)", len(groups))
	}

	access := groups[0]
	if access.Base != "access.log" {
		t.Fatalf("groups[0].Base = %q, want access.log", access.Base)
	}
	if len(access.Files) != 3 {
		t.Fatalf("len(access.Files) = %d, want 3", len(access.Files))
	}
	if filepath.Base(access.Files[0].Path) != "access.log" {
		t.Errorf("Files[0] = %q, want access.log first (newest)", access.Files[0].Path)
	}
	if filepath.Base(access.Files[2].Path) != "access.log.2.gz" {
		t.Errorf("Files[2] = %q, want access.log.2.gz last (oldest)", access.Files[2].Path)
	}
	if !access.Files[2].Compressed || !access.Files[2].Supported {
		t.Errorf("Files[2] (gz) Compressed/Supported = %v/%v, want true/true", access.Files[2].Compressed, access.Files[2].Supported)
	}

	if groups[1].Base != "error.log" {
		t.Fatalf("groups[1].Base = %q, want error.log", groups[1].Base)
	}
}

func TestDiscoverMarksUnsupportedCompression(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app.log.1.xz", "placeholder\n")

	groups, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(groups) != 1 || len(groups[0].Files) != 1 {
		t.Fatalf("groups = %+v", groups)
	}
	f := groups[0].Files[0]
	if !f.Compressed || f.Supported {
		t.Errorf("xz file Compressed/Supported = %v/%v, want true/false", f.Compressed, f.Supported)
	}
}

func TestOpenDecompressesGzip(t *testing.T) {
	dir := t.TempDir()
	writeGzipFile(t, dir, "access.log.1.gz", "hello from gzip\n")

	groups, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	f := groups[0].Files[0]

	r, err := Open(f)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "hello from gzip\n" {
		t.Errorf("data = %q", data)
	}
}
