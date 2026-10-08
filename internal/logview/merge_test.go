package logview

import (
	"testing"
	"time"
)

func TestMergeOrdersChronologicallyAcrossFiles(t *testing.T) {
	t0 := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
	fileA := []Entry{
		{Time: t0.Add(1 * time.Minute), File: "a.log", Line: 1, Message: "a1"},
		{Time: t0.Add(4 * time.Minute), File: "a.log", Line: 2, Message: "a2"},
	}
	fileB := []Entry{
		{Time: t0.Add(2 * time.Minute), File: "b.log", Line: 1, Message: "b1"},
		{Time: t0.Add(3 * time.Minute), File: "b.log", Line: 2, Message: "b2"},
	}

	got := Merge(fileA, fileB)
	want := []string{"a1", "b1", "b2", "a2"}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Message != w {
			t.Errorf("got[%d].Message = %q, want %q", i, got[i].Message, w)
		}
	}
}

func TestMergeKeepsOriginalOrderForEqualTimestamps(t *testing.T) {
	same := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
	fileA := []Entry{{Time: same, File: "a.log", Line: 1, Message: "first"}}
	fileB := []Entry{{Time: same, File: "b.log", Line: 1, Message: "second"}}

	got := Merge(fileA, fileB)
	if got[0].Message != "first" || got[1].Message != "second" {
		t.Errorf("got = %+v, want stable order first,second", got)
	}
}
