package logview

import "sort"

// Merge combines several files' own already-parsed entries into one
// chronological stream — a plain stable sort by Time, not a streaming
// k-way merge: Phase 1's own target (an admin reading a handful of
// rotated log files for one service) comfortably fits in memory, and a
// stable sort keeps entries that share an identical Time (a burst of
// same-millisecond lines, or several timestamp-less FormatPlain lines
// sharing one fallbackTime) in their original File/Line order rather
// than shuffling them — see ParseAll's own fallbackTime handling for
// why that original order is still meaningful even without a real
// timestamp.
func Merge(groups ...[]Entry) []Entry {
	var total int
	for _, g := range groups {
		total += len(g)
	}
	out := make([]Entry, 0, total)
	for _, g := range groups {
		out = append(out, g...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Time.Before(out[j].Time)
	})
	return out
}

// SortByTime stable-sorts entries by Time in place — the same
// comparator Merge's own sort.SliceStable call uses, factored out so
// a caller that already knows entries is the only thing that still
// needs ordering (a single freshly read batch, not a whole combined
// stream) can sort just that instead of paying for a full Merge.
func SortByTime(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Time.Before(entries[j].Time)
	})
}

// MergeSorted combines two already Time-sorted (non-decreasing),
// stable-ordered entry slices into a new slice in
// O(len(a)+len(b)) time — unlike Merge, which always re-sorts
// everything from scratch regardless of how much of it was already
// sorted. Ties (equal Time) take from a before b, the same
// first-argument-wins tie-break sort.SliceStable gives Merge's own
// combined slice (a's own elements always precede b's own in the
// array Merge builds before sorting it).
//
// Callers must guarantee both a and b are themselves already sorted
// by Time (SortByTime, or a slice built by Merge/MergeSorted/
// MergeSortedInto) — ParseAll's own fallback-time handling does not
// guarantee a freshly parsed file comes out that way on its own (a
// line with no real timestamp borrows the file's mtime as a guess,
// which is not necessarily later than every real timestamp already
// seen in that same file), so a batch straight from ParseAll needs
// SortByTime first.
func MergeSorted(a, b []Entry) []Entry {
	out := make([]Entry, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if b[j].Time.Before(a[i].Time) {
			out = append(out, b[j])
			j++
		} else {
			out = append(out, a[i])
			i++
		}
	}
	out = append(out, a[i:]...)
	out = append(out, b[j:]...)
	return out
}

// MergeSortedInto folds next (already Time-sorted, see MergeSorted's
// own doc comment on that precondition) into prior (already
// Time-sorted), reusing prior's own backing array wherever its
// capacity allows — the normal amortized-growth append, not a fresh
// full-size allocation on every call. Ties take from prior before
// next, the same convention MergeSorted gives its own first argument.
//
// Safe only when nothing else can be reading prior concurrently while
// this runs — unlike MergeSorted, which always hands back a brand-new
// slice safe to alias with whatever the caller already had, this one
// mutates prior's backing array in place. logaudit.go only ever calls
// this with log.RotatedEntries, which the UI goroutine is the sole
// reader and writer of; the one background reader of entries this
// package feeds into (the log audit filter scan, a *copy* of the
// log.AllEntries slice header taken before it starts) is handed its
// own, separately allocated MergeSorted result instead — see
// recomputeLogAuditCombined's own doc comment.
//
// Implementation: appends next onto the end of prior (so both runs
// sit contiguously in one backing array), takes its own copy of just
// that newly appended tail (bounded by len(next), not len(prior) —
// the actual memory win over resorting the combined whole), then
// merges the two sorted runs back into the combined array working
// from the last index down to the first, so a prior value already
// read is never clobbered before every later write past it has
// happened — the textbook "merge two sorted runs in O(min) extra
// space" technique (the same shape LeetCode's "Merge Sorted Array"
// problem uses), just with Entry/Time and this package's own
// prior-wins tie-break in place of plain integers.
func MergeSortedInto(prior, next []Entry) []Entry {
	if len(next) == 0 {
		return prior
	}
	m := len(prior)
	combined := append(prior, next...)
	tail := append([]Entry(nil), combined[m:]...)
	i, j, k := m-1, len(tail)-1, len(combined)-1
	for j >= 0 {
		if i >= 0 && tail[j].Time.Before(combined[i].Time) {
			combined[k] = combined[i]
			i--
		} else {
			combined[k] = tail[j]
			j--
		}
		k--
	}
	return combined
}
