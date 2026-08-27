package replace

import (
	"os"
	"path/filepath"
	"testing"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func TestPreviewFindsChangedFilesOnly(t *testing.T) {
	dir := t.TempDir()
	changed := filepath.Join(dir, "a.txt")
	unchanged := filepath.Join(dir, "b.txt")
	mustWriteFile(t, changed, "hello world\n")
	mustWriteFile(t, unchanged, "nothing to see\n")

	script, err := BuildScript("hello", "goodbye", false, false, false, true)
	if err != nil {
		t.Fatalf("BuildScript: %v", err)
	}

	changes, skipped, err := Preview([]string{changed, unchanged}, script, false, nil)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none", skipped)
	}
	if len(changes) != 1 || changes[0].Path != changed {
		t.Fatalf("changes = %+v, want exactly one change for %s", changes, changed)
	}
	if string(changes[0].After) != "goodbye world\n" {
		t.Errorf("changes[0].After = %q, want %q", changes[0].After, "goodbye world\n")
	}
	if string(changes[0].Before) != "hello world\n" {
		t.Errorf("changes[0].Before = %q, want the original content", changes[0].Before)
	}

	// Preview must not have touched the actual file yet.
	data, _ := os.ReadFile(changed)
	if string(data) != "hello world\n" {
		t.Errorf("Preview modified %s on disk: %q", changed, data)
	}
}

func TestPreviewSkipsDirectoriesAndBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	binaryFile := filepath.Join(dir, "bin.dat")
	if err := os.WriteFile(binaryFile, []byte("abc\x00def"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "does-not-exist")

	script, err := BuildScript("abc", "xyz", false, false, false, true)
	if err != nil {
		t.Fatal(err)
	}

	changes, skipped, err := Preview([]string{subdir, binaryFile, missing}, script, false, nil)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("changes = %+v, want none (all inputs should be skipped)", changes)
	}
	for _, path := range []string{subdir, binaryFile, missing} {
		if _, ok := skipped[path]; !ok {
			t.Errorf("expected %s to be reported in skipped, got %v", path, skipped)
		}
	}
}

func TestPreviewReportsProgressForEveryPath(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	mustWriteFile(t, a, "hello\n")
	mustWriteFile(t, b, "hello\n")
	missing := filepath.Join(dir, "gone.txt")

	script, err := BuildScript("hello", "goodbye", false, false, false, true)
	if err != nil {
		t.Fatal(err)
	}

	var seen []string
	_, _, err = Preview([]string{a, missing, b}, script, false, func(path string) {
		seen = append(seen, path)
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	want := []string{a, missing, b}
	if len(seen) != len(want) {
		t.Fatalf("onProgress saw %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("onProgress[%d] = %q, want %q", i, seen[i], want[i])
		}
	}
}

func TestPreviewStopsOnBadScript(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	mustWriteFile(t, f, "hello\n")

	_, _, err := Preview([]string{f}, "s/unterminated", false, nil)
	if err == nil {
		t.Fatal("Preview with a malformed sed script should return an error")
	}
}

func TestApplyWritesChangesAtomicallyAndPreservesMode(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	mustWriteFile(t, f, "hello world\n")
	if err := os.Chmod(f, 0o640); err != nil {
		t.Fatal(err)
	}

	changes := []FileChange{{Path: f, Before: []byte("hello world\n"), After: []byte("goodbye world\n")}}

	applied, err := Apply(changes, false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}

	data, err := os.ReadFile(f)
	if err != nil || string(data) != "goodbye world\n" {
		t.Fatalf("file content = %q, %v, want %q", data, err, "goodbye world\n")
	}
	fi, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %o, want 0640 preserved", fi.Mode().Perm())
	}
	if _, err := os.Stat(f + ".bak"); !os.IsNotExist(err) {
		t.Error("no .bak file should exist when backup=false")
	}
}

func TestApplyWithBackupWritesOriginalToBakFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	mustWriteFile(t, f, "hello world\n")

	changes := []FileChange{{Path: f, Before: []byte("hello world\n"), After: []byte("goodbye world\n")}}

	if _, err := Apply(changes, true); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	backup, err := os.ReadFile(f + ".bak")
	if err != nil {
		t.Fatalf("reading .bak: %v", err)
	}
	if string(backup) != "hello world\n" {
		t.Errorf(".bak content = %q, want the original content", backup)
	}
}

func TestApplyContinuesPastOneFailure(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok.txt")
	mustWriteFile(t, ok, "before\n")
	missing := filepath.Join(dir, "gone.txt") // Stat will fail: never written

	changes := []FileChange{
		{Path: missing, Before: []byte("x"), After: []byte("y")},
		{Path: ok, Before: []byte("before\n"), After: []byte("after\n")},
	}

	applied, err := Apply(changes, false)
	if err == nil {
		t.Fatal("Apply should report the missing file's error")
	}
	if applied != 1 {
		t.Errorf("applied = %d, want 1 (the failing file shouldn't block the rest)", applied)
	}
	data, _ := os.ReadFile(ok)
	if string(data) != "after\n" {
		t.Errorf("ok.txt content = %q, want %q", data, "after\n")
	}
}
