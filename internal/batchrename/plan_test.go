package batchrename

import (
	"os"
	"path/filepath"
	"testing"
)

// touch creates an empty file at path, failing the test if it can't.
func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("creating %s: %v", path, err)
	}
}

func TestPlanComputesEveryChange(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "vacation1.jpg")
	b := filepath.Join(dir, "vacation2.jpg")
	touch(t, a)
	touch(t, b)

	rules := Rules{Find: "vacation", Replace: "trip"}
	result := Plan([]string{a, b}, rules)

	if len(result.Problems) != 0 {
		t.Fatalf("unexpected problems: %+v", result.Problems)
	}
	want := []Change{
		{From: a, To: filepath.Join(dir, "trip1.jpg")},
		{From: b, To: filepath.Join(dir, "trip2.jpg")},
	}
	if len(result.Changes) != len(want) {
		t.Fatalf("got %d changes, want %d: %+v", len(result.Changes), len(want), result.Changes)
	}
	for i, c := range result.Changes {
		if c != want[i] {
			t.Errorf("change %d = %+v, want %+v", i, c, want[i])
		}
	}
}

func TestPlanOmitsUnchangedNames(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "README.txt")
	touch(t, p)

	result := Plan([]string{p}, Rules{}) // zero Rules never changes anything
	if len(result.Changes) != 0 || len(result.Problems) != 0 {
		t.Errorf("expected no changes and no problems for an unchanged name, got %+v / %+v", result.Changes, result.Problems)
	}
}

func TestPlanFlagsAnEmptyResultingName(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ab.txt")
	touch(t, p)

	rules := Rules{TrimFront: 10, ExtensionMode: ExtensionRemove}
	result := Plan([]string{p}, rules)

	if len(result.Changes) != 0 {
		t.Fatalf("expected no changes, got %+v", result.Changes)
	}
	if len(result.Problems) != 1 || result.Problems[0].Path != p {
		t.Fatalf("expected one problem for %s, got %+v", p, result.Problems)
	}
}

func TestPlanFlagsACollisionBetweenTwoFilesInTheBatch(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "IMG_0001.jpg")
	b := filepath.Join(dir, "IMG_0002.jpg")
	touch(t, a)
	touch(t, b)

	// Both would become "photo.jpg" once the counter's stripped by a
	// trim wide enough to eat it.
	rules := Rules{TrimFront: 4, TrimBack: 4}
	result := Plan([]string{a, b}, rules)

	if len(result.Changes) != 1 {
		t.Fatalf("expected exactly one winner, got %+v", result.Changes)
	}
	if len(result.Problems) != 1 || result.Problems[0].Path != b {
		t.Fatalf("expected the second file to be reported as a collision, got %+v", result.Problems)
	}
}

func TestPlanFlagsACollisionWithAnExistingFileOnDisk(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "final.txt")
	touch(t, target)
	source := filepath.Join(dir, "draft.txt")
	touch(t, source)

	rules := Rules{Find: "draft", Replace: "final"}
	result := Plan([]string{source}, rules)

	if len(result.Changes) != 0 {
		t.Fatalf("expected no changes, got %+v", result.Changes)
	}
	if len(result.Problems) != 1 || result.Problems[0].Path != source {
		t.Fatalf("expected the source to be reported as a collision, got %+v", result.Problems)
	}
}

func TestPlanNumbersInTheGivenOrder(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "b.jpg")
	second := filepath.Join(dir, "a.jpg")
	touch(t, first)
	touch(t, second)

	// Passing [first, second] in that exact order — Plan must count 1, 2
	// in that order too, not re-sort them itself (see Plan's own doc
	// comment: ordering is the caller's job).
	rules := Rules{NumberPosition: NumberSuffix, NumberStart: 1, NumberDigits: 1}
	result := Plan([]string{first, second}, rules)

	want := []string{filepath.Join(dir, "b-1.jpg"), filepath.Join(dir, "a-2.jpg")}
	if len(result.Changes) != 2 {
		t.Fatalf("got %+v", result.Changes)
	}
	for i, c := range result.Changes {
		if c.To != want[i] {
			t.Errorf("change %d To = %q, want %q", i, c.To, want[i])
		}
	}
}
