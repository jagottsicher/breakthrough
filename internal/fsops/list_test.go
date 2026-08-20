package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListDir(t *testing.T) {
	dir := t.TempDir()

	mustCreate := func(name string, isDir bool) {
		t.Helper()
		path := filepath.Join(dir, name)
		if isDir {
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
			return
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Created in an order that would NOT already match the expected
	// sorted output, so the test actually exercises the sort.
	mustCreate("zeta.txt", false)
	mustCreate("Alpha", true)
	mustCreate("beta.txt", false)
	mustCreate("Omega", true)

	entries, err := ListDir(dir)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}

	want := []Entry{
		{Name: "Alpha", IsDir: true},
		{Name: "Omega", IsDir: true},
		{Name: "beta.txt", IsDir: false},
		{Name: "zeta.txt", IsDir: false},
	}

	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(entries), len(want), entries)
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, entries[i], want[i])
		}
	}
}

func TestListDirNonExistent(t *testing.T) {
	if _, err := ListDir(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected an error for a non-existent directory, got nil")
	}
}
