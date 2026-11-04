package logdiskcache

import "github.com/jagottsicher/breakthrough/internal/logview"

// realiasEntries restores the string-sharing ParseAll originally
// established for entries, which a gob round-trip silently loses —
// measured 2026-11-04 against real parsed entries with
// unsafe.StringData (see feature_ideas.txt, section 19d): a
// FormatPlain line's Message and Raw share one backing array right
// after ParseAll (logview/parse.go sets both to the exact same string
// value), but decode allocates each field from its own independently
// encoded bytes, so a cache hit would otherwise cost roughly double
// the RAM a fresh parse does for exactly the log format most plain,
// unstructured logs actually use — directly undermining the reason
// this cache exists at all.
//
// Called once, right after Get's own gob.Decode, before entries are
// handed to any caller — a single O(n) pass over data already in
// memory, no new allocations of its own (a string assignment copies
// only the (pointer, length) header, never the bytes), so the stale,
// now-unreferenced duplicate backing array becomes garbage the next
// GC cycle naturally reclaims.
func realiasEntries(entries []logview.Entry) {
	if len(entries) == 0 {
		return
	}
	// File: ParseAll stamps the exact same fileName string onto every
	// Entry from one file (see logview.EstimateEntriesBytes' own doc
	// comment), so entries[0].File is a safe canonical value only
	// because Put is always called once per single rotated file's own
	// already-homogeneous batch (see internal/ui's
	// applyLogAuditRotatedFile) — never a mixed batch from more than
	// one file. Unconditional, not "only if already equal": every
	// entry's own File is equal by *content* after a gob decode
	// regardless (nothing else could have written a different file
	// name into this one batch), so a content check here would never
	// actually be false — the whole point is collapsing entries[i]'s
	// own, separately decoded backing array onto entries[0]'s, which a
	// check that only reassigns when they're "already equal" would
	// defeat entirely.
	file := entries[0].File
	for i := range entries {
		entries[i].File = file
		if entries[i].Message == entries[i].Raw {
			entries[i].Raw = entries[i].Message
		}
	}
}
