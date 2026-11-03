package logview

import (
	"strings"
	"testing"
)

// fixedSizeEntries returns a one-entry slice whose own
// EstimateEntriesBytes is exactly entryBaseOverhead+n — Message is n
// bytes, Raw/Source/File are all empty, so the math in a test stays
// exact and readable instead of depending on EstimateEntriesBytes'
// own internals.
func fixedSizeEntries(n int) []Entry {
	return []Entry{{Message: strings.Repeat("x", n)}}
}

func TestRotatedCachePutGetRoundTrip(t *testing.T) {
	c := NewRotatedCache()
	entries := fixedSizeEntries(4)
	c.Put("a", entries, 1000)

	got, ok := c.Get("a")
	if !ok {
		t.Fatal("Get(\"a\") miss right after Put")
	}
	if len(got) != 1 || got[0].Message != entries[0].Message {
		t.Errorf("Get(\"a\") = %+v, want %+v", got, entries)
	}
}

func TestRotatedCacheGetMissReturnsFalse(t *testing.T) {
	c := NewRotatedCache()
	if _, ok := c.Get("missing"); ok {
		t.Error("Get on an empty cache reported a hit")
	}
}

func TestRotatedCachePutOversizedItemNeverInserted(t *testing.T) {
	c := NewRotatedCache()
	entries := fixedSizeEntries(4) // size = entryBaseOverhead+4 = 100
	c.Put("a", entries, 50)        // budget smaller than this one item

	if c.Len() != 0 {
		t.Errorf("Len() = %d, want 0 — an oversized item must never be inserted", c.Len())
	}
	if c.UsedBytes() != 0 {
		t.Errorf("UsedBytes() = %d, want 0", c.UsedBytes())
	}
	if _, ok := c.Get("a"); ok {
		t.Error("Get(\"a\") hit after a Put that should have been rejected for being oversized")
	}
}

func TestRotatedCacheZeroMaxBytesDisablesCaching(t *testing.T) {
	c := NewRotatedCache()
	c.Put("a", fixedSizeEntries(4), 0)
	if c.Len() != 0 {
		t.Errorf("Len() = %d, want 0 — maxBytes=0 must mean caching is off", c.Len())
	}

	// Also: lowering maxBytes to 0 while a key is already cached drops
	// that key on its own next Put, rather than leaving a stale entry
	// a future Get could still hit.
	c.Put("b", fixedSizeEntries(4), 1000)
	if _, ok := c.Get("b"); !ok {
		t.Fatal("setup: \"b\" not cached after a Put with a real budget")
	}
	c.Put("b", fixedSizeEntries(4), 0)
	if _, ok := c.Get("b"); ok {
		t.Error("Get(\"b\") still hit after re-Put with maxBytes=0")
	}
}

// TestRotatedCacheEvictsLeastRecentlyUsedNotLeastRecentlyInserted pins
// the actual point of this cache: Get must promote, so a key Put
// first but read most recently survives a later eviction over a key
// inserted after it but never read again.
func TestRotatedCacheEvictsLeastRecentlyUsedNotLeastRecentlyInserted(t *testing.T) {
	c := NewRotatedCache()
	const itemSize = 100 // entryBaseOverhead(96) + 4
	const budget = 250   // room for two items, not three

	c.Put("a", fixedSizeEntries(4), budget)
	c.Put("b", fixedSizeEntries(4), budget)
	if _, ok := c.Get("a"); !ok {
		t.Fatal("setup: \"a\" not cached after Put")
	} // promotes "a" past "b"

	c.Put("c", fixedSizeEntries(4), budget) // 3*itemSize > budget, one eviction needed

	if _, ok := c.Get("b"); ok {
		t.Error("\"b\" survived eviction — it was least recently used, should have been dropped")
	}
	if _, ok := c.Get("a"); !ok {
		t.Error("\"a\" was evicted — it was Get'd more recently than \"b\" and should have survived")
	}
	if _, ok := c.Get("c"); !ok {
		t.Error("\"c\" missing — the item that was just Put should always be present")
	}
	if c.UsedBytes() > budget {
		t.Errorf("UsedBytes() = %d, want <= %d after eviction", c.UsedBytes(), budget)
	}
}

func TestEstimateEntriesBytesCountsFileOncePerBatchNotPerEntry(t *testing.T) {
	entries := []Entry{
		{File: "abc"},
		{File: "abc"},
		{File: "abc"},
	}
	got := EstimateEntriesBytes(entries)
	want := int64(3*entryBaseOverhead + len("abc")) // File counted once, not 3x
	if got != want {
		t.Errorf("EstimateEntriesBytes() = %d, want %d (File counted once per batch)", got, want)
	}
}

func TestEstimateEntriesBytesEmptyIsZero(t *testing.T) {
	if got := EstimateEntriesBytes(nil); got != 0 {
		t.Errorf("EstimateEntriesBytes(nil) = %d, want 0", got)
	}
}
