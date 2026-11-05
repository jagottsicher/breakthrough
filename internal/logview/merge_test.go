package logview

import (
	"fmt"
	"math/rand"
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

func TestMergeSortedOrdersChronologicallyAndKeepsTiesFromA(t *testing.T) {
	t0 := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
	a := []Entry{
		{Time: t0, Message: "a-tie"},
		{Time: t0.Add(2 * time.Minute), Message: "a2"},
		{Time: t0.Add(4 * time.Minute), Message: "a4"},
	}
	b := []Entry{
		{Time: t0, Message: "b-tie"},
		{Time: t0.Add(1 * time.Minute), Message: "b1"},
		{Time: t0.Add(3 * time.Minute), Message: "b3"},
	}

	got := MergeSorted(a, b)
	want := []string{"a-tie", "b-tie", "b1", "a2", "b3", "a4"}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Message != w {
			t.Errorf("got[%d].Message = %q, want %q", i, got[i].Message, w)
		}
	}

	// a and b themselves must be untouched — MergeSorted always hands
	// back a fresh slice, never mutates either input in place (see its
	// own doc comment on why that matters for log.AllEntries).
	if a[0].Message != "a-tie" || b[0].Message != "b-tie" {
		t.Errorf("MergeSorted mutated an input slice: a=%+v b=%+v", a, b)
	}
}

func TestMergeSortedIntoOrdersChronologicallyAndKeepsTiesFromPrior(t *testing.T) {
	t0 := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
	prior := []Entry{
		{Time: t0, Message: "prior-tie"},
		{Time: t0.Add(2 * time.Minute), Message: "prior2"},
		{Time: t0.Add(4 * time.Minute), Message: "prior4"},
	}
	next := []Entry{
		{Time: t0, Message: "next-tie"},
		{Time: t0.Add(1 * time.Minute), Message: "next1"},
		{Time: t0.Add(3 * time.Minute), Message: "next3"},
	}

	got := MergeSortedInto(prior, next)
	want := []string{"prior-tie", "next-tie", "next1", "prior2", "next3", "prior4"}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Message != w {
			t.Errorf("got[%d].Message = %q, want %q", i, got[i].Message, w)
		}
	}
}

func TestMergeSortedIntoEmptyNextReturnsPriorUnchanged(t *testing.T) {
	prior := []Entry{{Message: "only"}}
	got := MergeSortedInto(prior, nil)
	if len(got) != 1 || got[0].Message != "only" {
		t.Errorf("got = %+v, want prior unchanged", got)
	}
}

// TestIncrementalMergeMatchesFullMergeAcrossRandomBatches pins the
// actual invariant logaudit.go's own incremental recombine (19e, Stufe
// A) depends on: folding one sorted batch at a time into a running
// RotatedEntries via SortByTime+MergeSortedInto, then combining that
// with CurrentEntries via MergeSorted, must produce byte-for-byte the
// same order — including every tie — as calling the original, full
// Merge once over the current entries and every batch concatenated
// together. Randomized, with deliberately clustered/duplicate
// timestamps (a real log's own same-millisecond bursts), across many
// seeds so a tie-break regression can't hide behind one lucky batch
// shape.
func TestIncrementalMergeMatchesFullMergeAcrossRandomBatches(t *testing.T) {
	t0 := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)

	for seed := range int64(50) {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))

			randomEntries := func(n int, prefix string) []Entry {
				entries := make([]Entry, n)
				for i := range entries {
					// A small time range relative to n so duplicate/
					// out-of-order timestamps within and across
					// batches are common, not a rare edge case.
					entries[i] = Entry{
						Time:    t0.Add(time.Duration(rng.Intn(n/4+1)) * time.Second),
						Message: fmt.Sprintf("%s-%d", prefix, i),
					}
				}
				return entries
			}

			current := randomEntries(rng.Intn(20), "current")

			batchCount := rng.Intn(10)
			batches := make([][]Entry, batchCount)
			var allConcatenated []Entry
			for i := range batches {
				batches[i] = randomEntries(rng.Intn(30), fmt.Sprintf("batch%d", i))
				allConcatenated = append(allConcatenated, batches[i]...)
			}

			want := Merge(current, allConcatenated)

			var rotated []Entry
			for _, batch := range batches {
				sorted := append([]Entry(nil), batch...)
				SortByTime(sorted)
				rotated = MergeSortedInto(rotated, sorted)
			}
			currentSorted := append([]Entry(nil), current...)
			SortByTime(currentSorted)
			got := MergeSorted(currentSorted, rotated)

			if len(got) != len(want) {
				t.Fatalf("len(got) = %d, want %d", len(got), len(want))
			}
			for i := range want {
				if got[i].Message != want[i].Message || !got[i].Time.Equal(want[i].Time) {
					t.Fatalf("entry %d: got %+v, want %+v", i, got[i], want[i])
				}
			}
		})
	}
}

// benchmarkBatches builds batchCount already-sorted-within-themselves
// batches of perBatch entries each — a stand-in for the repeated
// rotated-file batches a real progressive load folds into
// RotatedEntries once per throttle tick (19e, Stufe A).
func benchmarkBatches(batchCount, perBatch int) [][]Entry {
	start := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	batches := make([][]Entry, batchCount)
	for i := range batches {
		entries := make([]Entry, perBatch)
		for j := range entries {
			entries[j] = Entry{
				Time:    start.Add(time.Duration(i*perBatch+j) * time.Second),
				Message: "connection established",
			}
		}
		batches[i] = entries
	}
	return batches
}

// BenchmarkRecombineFullResortEachTick reproduces the pre-19e
// behavior this package's own Merge left callers like
// recomputeLogAuditCombined with: every tick re-sorts the *entire*
// accumulated stream from scratch via one Merge call over everything
// seen so far, the cost that grows with every additional tick instead
// of staying bounded to just the newest batch. Compare against
// BenchmarkRecombineIncrementalMerge below with the same batchCount/
// perBatch and -benchmem.
func BenchmarkRecombineFullResortEachTick(b *testing.B) {
	const batchCount = 30
	const perBatch = 40000 // 30*40000 = 1.2M, same order of magnitude as BenchmarkApplyLogAuditRotatedFile
	batches := benchmarkBatches(batchCount, perBatch)

	for b.Loop() {
		var accumulated []Entry
		for _, batch := range batches {
			accumulated = Merge(accumulated, batch)
		}
	}
}

// BenchmarkRecombineIncrementalMerge is
// BenchmarkRecombineFullResortEachTick's own counterpart for the
// post-19e path: each tick sorts only the newest batch, then
// MergeSortedInto folds it into the already-sorted accumulator in one
// linear pass instead of re-sorting everything.
func BenchmarkRecombineIncrementalMerge(b *testing.B) {
	const batchCount = 30
	const perBatch = 40000
	batches := benchmarkBatches(batchCount, perBatch)

	for b.Loop() {
		var accumulated []Entry
		for _, batch := range batches {
			sorted := append([]Entry(nil), batch...)
			SortByTime(sorted)
			accumulated = MergeSortedInto(accumulated, sorted)
		}
	}
}
