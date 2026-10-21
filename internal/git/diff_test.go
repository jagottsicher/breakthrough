package git

import "testing"

// TestDiffPageReachesTheLastPageOnUnevenContent pins a real, reported
// bug: at a view height of 10 with 29 lines of content (not a multiple
// of the height), the maximum row a view ever actually scrolls to is
// 19 (29-10 -- there's nothing further below to show), which the old
// row/height+1 formula reported as page 2 of 3, a last page that could
// never actually be reached no matter how far down the view scrolled.
// DiffPage maps row proportionally across the real reachable range
// instead, so the true maximum row always reads as the real last page.
func TestDiffPageReachesTheLastPageOnUnevenContent(t *testing.T) {
	const height = 10
	const totalLines = 29
	maxRow := totalLines - height // 19 -- the real ceiling scrolling itself clamps to

	page, totalPages := DiffPage(maxRow, totalLines, height)

	if totalPages != 3 {
		t.Fatalf("totalPages = %d, want 3 (ceil(29/10))", totalPages)
	}
	if page != totalPages {
		t.Errorf("page at the true maximum scroll row = %d, want %d (the last page must be reachable)", page, totalPages)
	}
}

// TestDiffPageStartsAtOneAndNeverExceedsTotal pins the two edges
// DiffPage must never miss regardless of how row/height divide: row 0
// is always page 1, and a row past the real maximum (a caller should
// never hand one, but a defensive clamp costs nothing) never reports a
// page past totalPages.
func TestDiffPageStartsAtOneAndNeverExceedsTotal(t *testing.T) {
	if page, _ := DiffPage(0, 29, 10); page != 1 {
		t.Errorf("page at row 0 = %d, want 1", page)
	}

	page, totalPages := DiffPage(10_000, 29, 10)
	if page != totalPages {
		t.Errorf("page past the real maximum row = %d, want it clamped to %d", page, totalPages)
	}
}

// TestAuthorColorIndexIsDeterministic pins the hash AuthorColorIndex
// is built on: the same email must always land on the same index.
func TestAuthorColorIndexIsDeterministic(t *testing.T) {
	first := AuthorColorIndex("jens@example.com", 12)
	second := AuthorColorIndex("jens@example.com", 12)
	if first != second {
		t.Errorf("AuthorColorIndex(same email) = %d, %d, want identical", first, second)
	}
}

func TestAuthorColorIndexStaysWithinBounds(t *testing.T) {
	emails := []string{"a@example.com", "b@example.com", "c@example.com", "d@example.com"}
	for _, email := range emails {
		if got := AuthorColorIndex(email, 12); got < 0 || got >= 12 {
			t.Errorf("AuthorColorIndex(%q, 12) = %d, want in [0, 12)", email, got)
		}
	}
}

func TestDiffHeaderPathExtractsThePostChangePath(t *testing.T) {
	got, ok := DiffHeaderPath("diff --git a/old/name.go b/new/name.go")
	if !ok {
		t.Fatal("ok = false, want true for a well-formed header line")
	}
	if got != "new/name.go" {
		t.Errorf("path = %q, want %q", got, "new/name.go")
	}
}

func TestDiffHeaderPathUsesTheLastBSlashOccurrenceOnARename(t *testing.T) {
	// The "a" path itself contains the literal substring " b/" ahead of
	// the real rename's own "b/" marker — the last occurrence, not the
	// first, is the correct split point for the actual new path.
	got, ok := DiffHeaderPath("diff --git a/weird b/thing.go b/weird-thing-renamed.go")
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got != "weird-thing-renamed.go" {
		t.Errorf("path = %q, want %q", got, "weird-thing-renamed.go")
	}
}

func TestDiffHeaderPathRejectsALineWithoutTheMarker(t *testing.T) {
	if _, ok := DiffHeaderPath("not a diff header at all"); ok {
		t.Error("ok = true, want false for a line with no \" b/\" marker")
	}
}
