package git

import (
	"hash/fnv"
	"strings"
)

// DiffPage maps a scroll row to a 1-based "page" number out of
// totalPages, given the view's own current height — not simply
// row/height+1, clamped: when totalLines isn't a multiple of height,
// a real scroll offset never goes past totalLines-height (there's
// nothing further to scroll to), which that naive formula would then
// report as something short of the real last page, forever out of
// reach no matter how far down the view scrolls — a real, reported
// case: a 29-line diff at height 10 (3 pages, ceil(29/10)) never
// showing "Page 3/3" because the maximum reachable row (19) is still
// "page 2" under row/height+1. Instead, row is mapped proportionally
// across [0, maxRow] onto [1, totalPages], so the true top always
// reads as page 1 and the true bottom (wherever scrolling is clamped
// to) always reads as the real last page, reachable by construction —
// the tradeoff is that a page "boundary" while scrolling line by line
// no longer falls at an exact multiple of height once totalLines
// doesn't divide evenly, the same rounding every proportional (not
// fixed-size) pagination scheme has.
func DiffPage(row, totalLines, height int) (currentPage, totalPages int) {
	totalPages = (totalLines + height - 1) / height
	if totalPages < 1 {
		totalPages = 1
	}
	maxRow := totalLines - height
	if maxRow <= 0 {
		return 1, totalPages
	}
	if row > maxRow {
		row = maxRow
	}
	if row < 0 {
		row = 0
	}
	currentPage = 1 + (row*(totalPages-1)+maxRow/2)/maxRow
	return currentPage, totalPages
}

// AuthorColorIndex deterministically maps email to an index in
// [0, n) via FNV-1a — the same author always lands on the same slot
// across a render and across reloads, without this package having to
// track "which author got which slot already" state of its own
// anywhere, or know what a color even is: that's entirely the
// caller's own fixed-size palette to index into. Hashes the email, not
// a display name, since the same person's commits should read as the
// same author even if they changed their git config's user.name at
// some point, and email is what actually stays stable across that.
// FNV-1a, not a cryptographic hash: this only ever needs to be a
// consistent, well-distributed bucket index, never resistant to
// anyone deliberately choosing an email to collide with another
// author's slot — a real but harmless failure mode (two authors
// sharing a slot), not a security concern.
func AuthorColorIndex(email string, n int) int {
	h := fnv.New32a()
	h.Write([]byte(email))
	return int(h.Sum32() % uint32(n))
}

// DiffHeaderPath extracts the post-change ("b/...") path out of a
// "diff --git a/X b/Y" header line — the last " b/" occurrence, not
// the first, since X itself could coincidentally contain the
// substring " b/" (rare, but a known, accepted edge case: a path
// containing that exact substring right before a rename's own "b/"
// marker could still pick the wrong split point).
func DiffHeaderPath(line string) (string, bool) {
	rest := strings.TrimPrefix(line, "diff --git ")
	idx := strings.LastIndex(rest, " b/")
	if idx < 0 {
		return "", false
	}
	return rest[idx+len(" b/"):], true
}
