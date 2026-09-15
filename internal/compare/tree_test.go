package compare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeHash hashes real file content with SHA-256 — a real digest, just
// not routed through fsops (this package has no dependency on it; see
// HashFunc's own doc comment), so ModeHash tests exercise the actual
// comparison logic end to end.
func fakeHash(_ context.Context, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func mkfile(t *testing.T, path string, content string, when time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func entryFor(entries []Entry, rel string) (Entry, bool) {
	for _, e := range entries {
		if e.RelPath == rel {
			return e, true
		}
	}
	return Entry{}, false
}

func TestWalkClassifiesEveryCaseUnderModeQuick(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	t0 := time.Now().Truncate(time.Second)

	mkfile(t, filepath.Join(dirA, "same.txt"), "hello", t0)
	mkfile(t, filepath.Join(dirB, "same.txt"), "hello", t0)

	mkfile(t, filepath.Join(dirA, "sized.txt"), "short", t0)
	mkfile(t, filepath.Join(dirB, "sized.txt"), "much longer content", t0)

	mkfile(t, filepath.Join(dirA, "touched.txt"), "same bytes", t0)
	mkfile(t, filepath.Join(dirB, "touched.txt"), "same bytes", t0.Add(time.Hour))

	mkfile(t, filepath.Join(dirA, "only-a.txt"), "x", t0)
	mkfile(t, filepath.Join(dirB, "only-b.txt"), "y", t0)

	entries, stats, err := Walk(context.Background(), dirA, dirB, ModeQuick, nil, nil)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	cases := map[string]Verdict{
		"same.txt":    Identical,
		"sized.txt":   Differs,
		"touched.txt": Uncertain,
		"only-a.txt":  OnlyInA,
		"only-b.txt":  OnlyInB,
	}
	for rel, want := range cases {
		e, ok := entryFor(entries, rel)
		if !ok {
			t.Errorf("%s: missing from entries", rel)
			continue
		}
		if e.Verdict != want {
			t.Errorf("%s: verdict = %v, want %v", rel, e.Verdict, want)
		}
	}
	if stats.Identical != 1 || stats.Differs != 1 || stats.Uncertain != 1 || stats.OnlyInA != 1 || stats.OnlyInB != 1 {
		t.Errorf("stats = %+v, want one of each", stats)
	}
}

func TestWalkModeHashResolvesSameSizeDifferentTimeDefinitively(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	t0 := time.Now().Truncate(time.Second)

	mkfile(t, filepath.Join(dirA, "really-same.txt"), "identical bytes", t0)
	mkfile(t, filepath.Join(dirB, "really-same.txt"), "identical bytes", t0.Add(time.Hour))

	mkfile(t, filepath.Join(dirA, "coincidence.txt"), "AAAAA", t0)
	mkfile(t, filepath.Join(dirB, "coincidence.txt"), "BBBBB", t0.Add(time.Hour))

	entries, stats, err := Walk(context.Background(), dirA, dirB, ModeHash, fakeHash, nil)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if e, _ := entryFor(entries, "really-same.txt"); e.Verdict != Identical {
		t.Errorf("really-same.txt verdict = %v, want Identical", e.Verdict)
	}
	if e, _ := entryFor(entries, "coincidence.txt"); e.Verdict != Differs {
		t.Errorf("coincidence.txt verdict = %v, want Differs (same size, different content)", e.Verdict)
	}
	if stats.Uncertain != 0 {
		t.Errorf("ModeHash should never produce Uncertain, got %d", stats.Uncertain)
	}
}

func TestWalkNeverDescendsIntoAOneSidedDirectory(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	t0 := time.Now()
	mkfile(t, filepath.Join(dirA, "old-backup", "deep", "file1.txt"), "a", t0)
	mkfile(t, filepath.Join(dirA, "old-backup", "deep", "file2.txt"), "b", t0)
	mkfile(t, filepath.Join(dirA, "old-backup", "file3.txt"), "c", t0)

	entries, stats, err := Walk(context.Background(), dirA, dirB, ModeQuick, nil, nil)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want exactly one row for the whole one-sided subtree", entries)
	}
	e := entries[0]
	if e.RelPath != "old-backup" || e.Verdict != OnlyInA || !e.IsDir {
		t.Errorf("entry = %+v, want {old-backup, OnlyInA, dir}", e)
	}
	if stats.OnlyInA != 1 {
		t.Errorf("stats.OnlyInA = %d, want 1 (not one per descendant)", stats.OnlyInA)
	}
}

func TestWalkRecursesIntoDirectoriesPresentOnBothSides(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	t0 := time.Now().Truncate(time.Second)
	mkfile(t, filepath.Join(dirA, "sub", "nested.txt"), "x", t0)
	mkfile(t, filepath.Join(dirB, "sub", "nested.txt"), "yy", t0)

	entries, _, err := Walk(context.Background(), dirA, dirB, ModeQuick, nil, nil)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	e, ok := entryFor(entries, "sub/nested.txt")
	if !ok {
		t.Fatal("expected an entry for sub/nested.txt, not one for the sub directory itself")
	}
	if e.Verdict != Differs {
		t.Errorf("sub/nested.txt verdict = %v, want Differs", e.Verdict)
	}
	if _, ok := entryFor(entries, "sub"); ok {
		t.Error("a matched directory should never get its own Entry")
	}
}

func TestWalkFlagsAFileVersusDirectoryTypeMismatch(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(dirA, "thing"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dirB, "thing"), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, stats, err := Walk(context.Background(), dirA, dirB, ModeQuick, nil, nil)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	e, ok := entryFor(entries, "thing")
	if !ok || e.Verdict != Differs || e.Note == "" {
		t.Errorf("entry = %+v, ok=%v; want Differs with a Note explaining the type mismatch", e, ok)
	}
	if stats.Differs != 1 {
		t.Errorf("stats.Differs = %d, want 1", stats.Differs)
	}
}

func TestWalkReportsAnUnreadableDirectoryAsErroredNotFatal(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dirA, dirB := t.TempDir(), t.TempDir()
	locked := filepath.Join(dirA, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) }) // TempDir cleanup needs to get back in
	if err := os.Mkdir(filepath.Join(dirB, "locked"), 0o755); err != nil {
		t.Fatal(err)
	}
	mkfile(t, filepath.Join(dirA, "readable.txt"), "x", time.Now())
	mkfile(t, filepath.Join(dirB, "readable.txt"), "x", time.Now())

	entries, stats, err := Walk(context.Background(), dirA, dirB, ModeQuick, nil, nil)
	if err != nil {
		t.Fatalf("Walk: %v (a per-directory permission error should not abort the whole walk)", err)
	}
	e, ok := entryFor(entries, "locked")
	if !ok || e.Verdict != Errored || e.Err == nil {
		t.Errorf("entry = %+v, ok=%v; want an Errored entry with Err set", e, ok)
	}
	if stats.Errored != 1 {
		t.Errorf("stats.Errored = %d, want 1", stats.Errored)
	}
	if _, ok := entryFor(entries, "readable.txt"); !ok {
		t.Error("the rest of the tree should still be walked after one unreadable directory")
	}
}

func TestWalkStopsOnCancellationButKeepsWhatItFound(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		mkfile(t, filepath.Join(dirA, name+".txt"), name, time.Now())
	}
	// dirB stays empty: everything is OnlyInA, one report() call per name.

	ctx, cancel := context.WithCancel(context.Background())
	seen := 0
	_, _, err := Walk(ctx, dirA, dirB, ModeQuick, nil, func(count int) {
		seen = count
		if count == 2 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if seen < 2 {
		t.Errorf("onProgress saw %d, want at least 2 before cancellation took effect", seen)
	}
}

func TestWalkErrorsImmediatelyOnAMissingRoot(t *testing.T) {
	dirA := t.TempDir()
	if _, _, err := Walk(context.Background(), dirA, filepath.Join(dirA, "nope"), ModeQuick, nil, nil); err == nil {
		t.Error("expected an error for a nonexistent second root")
	}
}

func TestWalkProgressCountsEveryReportedEntry(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	t0 := time.Now()
	mkfile(t, filepath.Join(dirA, "one.txt"), "1", t0)
	mkfile(t, filepath.Join(dirB, "one.txt"), "1", t0)
	mkfile(t, filepath.Join(dirA, "two.txt"), "2", t0)

	var last int
	calls := 0
	entries, _, err := Walk(context.Background(), dirA, dirB, ModeQuick, nil, func(count int) {
		calls++
		last = count
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != len(entries) || last != len(entries) {
		t.Errorf("onProgress called %d times ending at %d, want %d calls ending at %d", calls, last, len(entries), len(entries))
	}
}
