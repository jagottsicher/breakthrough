package logview

// RotatedCache is a session-only, byte-budget LRU cache for rotated
// log files' own already-parsed entries — a rotated file never
// changes once logrotate has renamed it away, so a cache hit is
// always correct, and re-opening a log family a user already viewed
// this session should not mean re-reading and re-parsing every
// rotated file from disk again. Keeping every family's entries
// forever would grow without bound over a long session instead — a
// single multi-million-entry family alone can already be several
// gigabytes — so Put's own maxBytes argument caps the total estimated
// size across every cached file, evicting whichever was least
// recently Get/Put first.
//
// maxBytes is passed to Put on every call rather than fixed once at
// construction, so a live config change (breakthrough's Options
// screen can edit it mid-session) takes effect on the very next Put,
// without needing to discard and recreate this cache — which would
// also throw away everything already cached for no reason connected
// to the setting change itself.
//
// An item whose own EstimateEntriesBytes already exceeds maxBytes is
// never inserted at all: letting it in would mean evicting literally
// everything else just to fit one file, which defeats the cache for
// every other file for no benefit. That one file just never gets a
// "reopen is instant" cache hit, degrading to "always re-read" for it
// specifically — not a crash, and not a cache that silently grows past
// its own configured limit.
//
// Not safe for concurrent use — callers in this app only ever touch
// it from the UI goroutine (see breakthrough's logaudit.go, whose own
// RotatedCache field's doc comment states the background progressive
// loader never reaches into it), so no mutex here.
type RotatedCache struct {
	usedBytes int64
	items     map[string]*rotatedCacheItem
	seq       int64 // a monotonic "last used" clock, not wall-clock time — exact ordering, deterministic in tests
}

type rotatedCacheItem struct {
	entries []Entry
	bytes   int64
	lastUse int64
}

// NewRotatedCache returns an empty cache.
func NewRotatedCache() *RotatedCache {
	return &RotatedCache{items: map[string]*rotatedCacheItem{}}
}

// Get returns key's cached entries and promotes key to
// most-recently-used — the half of this type that actually delivers
// "switching back and forth between two logs stays fast", since Put's
// own eviction order is driven by whatever Get last touched, not
// insertion order.
func (c *RotatedCache) Get(key string) ([]Entry, bool) {
	if c == nil {
		return nil, false
	}
	item, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.seq++
	item.lastUse = c.seq
	return item.entries, true
}

// Put inserts entries under key, evicting whichever cached item was
// least recently Get/Put until back under maxBytes — unless entries'
// own estimated size already exceeds maxBytes, in which case it is
// never inserted (see this type's own doc comment for why). A
// non-positive maxBytes means caching is off: an existing entry under
// key is dropped (if any) and nothing new is inserted.
func (c *RotatedCache) Put(key string, entries []Entry, maxBytes int64) {
	if c == nil {
		return
	}
	if existing, ok := c.items[key]; ok {
		c.usedBytes -= existing.bytes
		delete(c.items, key)
	}
	if maxBytes <= 0 {
		return
	}
	size := EstimateEntriesBytes(entries)
	if size > maxBytes {
		return
	}
	for c.usedBytes+size > maxBytes && len(c.items) > 0 {
		c.evictOldest()
	}
	c.seq++
	c.items[key] = &rotatedCacheItem{entries: entries, bytes: size, lastUse: c.seq}
	c.usedBytes += size
}

// evictOldest drops whichever cached item has the smallest lastUse —
// a plain linear scan, not container/list or a heap: the number of
// cached items is always small (tens, bounded by how many distinct
// rotated files fit under MaxBytes at all), so O(n) here is the
// correct, simplest choice, not a shortcut.
func (c *RotatedCache) evictOldest() {
	var oldestKey string
	var oldestUse int64
	first := true
	for k, v := range c.items {
		if first || v.lastUse < oldestUse {
			oldestKey = k
			oldestUse = v.lastUse
			first = false
		}
	}
	if first {
		return
	}
	c.usedBytes -= c.items[oldestKey].bytes
	delete(c.items, oldestKey)
}

// Len reports how many files are currently cached — tests only.
func (c *RotatedCache) Len() int {
	if c == nil {
		return 0
	}
	return len(c.items)
}

// UsedBytes reports the cache's own current estimated total — tests
// only.
func (c *RotatedCache) UsedBytes() int64 {
	if c == nil {
		return 0
	}
	return c.usedBytes
}

// entryBaseOverhead is a rough, deliberately generous per-Entry
// estimate of the struct's own fixed fields (Time, Level, Line, and
// four string headers) — EstimateEntriesBytes' own doc comment
// explains why overestimating here is the safe direction for a
// budget, undercounting is not.
const entryBaseOverhead = 96

// EstimateEntriesBytes estimates entries' own total memory footprint
// for RotatedCache's budget. Message and Raw are counted separately
// even though ParseAll's own FormatPlain path sets both to the same
// string value (sharing one backing array, so that case's real cost
// is counted twice here) — a deliberate overestimate, not a bug:
// structured formats (JSON, syslog) extract Message as its own
// distinct allocation from a decoded field, so undercounting by
// assuming they always share would mean the budget silently lets more
// bytes in than configured on exactly the log formats most logs
// actually use. File is counted once per batch, not once per entry —
// ParseAll stamps the exact same fileName string onto every Entry
// from one file, so per-entry counting would overcount by roughly
// len(entries)x for no reason.
func EstimateEntriesBytes(entries []Entry) int64 {
	if len(entries) == 0 {
		return 0
	}
	var total int64
	for _, e := range entries {
		total += int64(entryBaseOverhead + len(e.Message) + len(e.Raw) + len(e.Source))
	}
	total += int64(len(entries[0].File))
	return total
}
