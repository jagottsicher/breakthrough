package fsops

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
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

// TestListDirLarge is a real-world stress test for the "does this even
// work with tens of thousands of entries?" question: it creates 20,000
// files, calls ListDir, and checks both that all of them come back and
// that the result is actually sorted. It logs how long ListDir itself
// took (file creation dominates the test's total time, not ListDir).
func TestListDirLarge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large-directory test in -short mode")
	}

	const n = 20000
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("file-%05d.txt", i)
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
	}

	start := time.Now()
	entries, err := ListDir(dir)
	elapsed := time.Since(start)
	t.Logf("ListDir on %d entries took %v", n, elapsed)

	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(entries) != n {
		t.Fatalf("got %d entries, want %d", len(entries), n)
	}
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Name >= entries[i].Name {
			t.Fatalf("not sorted at index %d: %q >= %q", i, entries[i-1].Name, entries[i].Name)
		}
	}
}
