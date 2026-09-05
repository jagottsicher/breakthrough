package batchrename

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyRenamesEveryChange(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}

	changes := []Change{
		{From: a, To: filepath.Join(dir, "alpha.txt")},
		{From: b, To: filepath.Join(dir, "beta.txt")},
	}
	applied, err := Apply(changes)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("got %d applied, want 2", len(applied))
	}

	got, err := os.ReadFile(filepath.Join(dir, "alpha.txt"))
	if err != nil || string(got) != "alpha" {
		t.Errorf("alpha.txt: content = %q, err = %v", got, err)
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Errorf("original a.txt should be gone, stat err = %v", err)
	}
}

func TestApplyContinuesPastOneFailure(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	touch(t, a)
	touch(t, b)

	// a's own rename fails because its source no longer exists (removed
	// out from under Apply, simulating something else having already
	// moved it) — b's independent rename must still go through.
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}

	changes := []Change{
		{From: a, To: filepath.Join(dir, "renamed-a.txt")},
		{From: b, To: filepath.Join(dir, "renamed-b.txt")},
	}
	applied, err := Apply(changes)
	if err == nil {
		t.Fatal("expected an error for the missing source, got nil")
	}
	if len(applied) != 1 || applied[0].From != b {
		t.Fatalf("expected only b's rename to have applied, got %+v", applied)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "renamed-b.txt")); statErr != nil {
		t.Errorf("renamed-b.txt should exist: %v", statErr)
	}
}

func TestUndoReversesApplyExactly(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}

	changes := []Change{
		{From: a, To: filepath.Join(dir, "alpha.txt")},
		{From: b, To: filepath.Join(dir, "beta.txt")},
	}
	applied, err := Apply(changes)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	undone, err := Undo(applied)
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if len(undone) != 2 {
		t.Fatalf("got %d undone, want 2", len(undone))
	}

	if got, err := os.ReadFile(a); err != nil || string(got) != "alpha" {
		t.Errorf("a.txt after undo: content = %q, err = %v", got, err)
	}
	if got, err := os.ReadFile(b); err != nil || string(got) != "beta" {
		t.Errorf("b.txt after undo: content = %q, err = %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "alpha.txt")); !os.IsNotExist(err) {
		t.Errorf("alpha.txt should be gone after undo, stat err = %v", err)
	}
}
