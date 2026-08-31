package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMode(t *testing.T) {
	tests := []struct {
		in      string
		want    os.FileMode
		wantErr bool
	}{
		{"755", 0o755, false},
		{"0644", 0o644, false},
		{"000", 0, false},
		{"777", 0o777, false},
		{"778", 0, true},  // not a valid octal digit
		{"abc", 0, true},  // not octal at all
		{"4755", 0, true}, // setuid digit: explicitly rejected, not approximated
		{"-1", 0, true},
	}

	for _, tt := range tests {
		got, err := ParseMode(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseMode(%q) = %v, want an error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseMode(%q): unexpected error %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseMode(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestChmod(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Chmod(path, 0o600); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

// chmodRecursiveFixture builds:
//
//	<dir>/f.txt
//	<dir>/sub/
//	<dir>/sub/g.txt
//
// — a small tree with both a top-level and a nested file and directory,
// enough to tell "only the top-level entry" apart from "the whole tree"
// for both ChmodDirsRecursive and ChmodFilesRecursive.
func chmodRecursiveFixture(t *testing.T) (dir, topFile, subDir, subFile string) {
	t.Helper()
	dir = t.TempDir()
	topFile = filepath.Join(dir, "f.txt")
	subDir = filepath.Join(dir, "sub")
	subFile = filepath.Join(subDir, "g.txt")

	if err := os.WriteFile(topFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, topFile, subDir, subFile
}

func perm(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

// TestChmodDirsRecursive checks find(1)'s own "-type d" semantics:
// every directory in the tree (including the root itself, which
// ChmodDirsRecursive's own path argument is) gets mode, files are left
// exactly as they were.
func TestChmodDirsRecursive(t *testing.T) {
	dir, topFile, subDir, subFile := chmodRecursiveFixture(t)

	if err := ChmodDirsRecursive(dir, 0o700); err != nil {
		t.Fatalf("ChmodDirsRecursive: %v", err)
	}

	if got := perm(t, dir); got != 0o700 {
		t.Errorf("dir mode = %v, want 0700", got)
	}
	if got := perm(t, subDir); got != 0o700 {
		t.Errorf("subDir mode = %v, want 0700", got)
	}
	if got := perm(t, topFile); got != 0o644 {
		t.Errorf("topFile mode = %v, want unchanged 0644", got)
	}
	if got := perm(t, subFile); got != 0o644 {
		t.Errorf("subFile mode = %v, want unchanged 0644", got)
	}
}

// TestChmodFilesRecursive is TestChmodDirsRecursive's mirror image for
// find(1)'s "-type f": every regular file in the tree gets mode,
// directories (including the root) are left exactly as they were.
func TestChmodFilesRecursive(t *testing.T) {
	dir, topFile, subDir, subFile := chmodRecursiveFixture(t)

	// Captured rather than assumed: t.TempDir()'s own root in particular
	// isn't guaranteed to be a plain 0755 (observed 0775 in this sandbox,
	// likely inherited from how its own TMPDIR itself is set up) — the
	// point of these two assertions is "ChmodFilesRecursive didn't touch
	// this directory", not "directories default to any specific mode".
	dirModeBefore, subDirModeBefore := perm(t, dir), perm(t, subDir)

	if err := ChmodFilesRecursive(dir, 0o600); err != nil {
		t.Fatalf("ChmodFilesRecursive: %v", err)
	}

	if got := perm(t, topFile); got != 0o600 {
		t.Errorf("topFile mode = %v, want 0600", got)
	}
	if got := perm(t, subFile); got != 0o600 {
		t.Errorf("subFile mode = %v, want 0600", got)
	}
	if got := perm(t, dir); got != dirModeBefore {
		t.Errorf("dir mode = %v, want unchanged %v", got, dirModeBefore)
	}
	if got := perm(t, subDir); got != subDirModeBefore {
		t.Errorf("subDir mode = %v, want unchanged %v", got, subDirModeBefore)
	}
}
