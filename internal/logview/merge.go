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
