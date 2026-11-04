// Package logdiskcache is Log Holepunch's own persistent, on-disk
// counterpart to logview.RotatedCache — a rotated file never changes
// once logrotate has renamed it away, so once its own entries have
// been read and parsed over a remote connection once, there is no
// correctness reason to ever pay that network round trip again for
// the rest of this machine's life, not just for the rest of one
// session. Scope is deliberately remote-only: a 2026-11-04 live
// measurement against a real remote host (see feature_ideas.txt,
// section 19d) found re-reading a local file is roughly as cheap as
// decoding this cache's own on-disk format, so there is no measured
// win for local sources — only "no network round trip" actually pays
// for the complexity here.
//
// Kept as its own package, not folded into internal/logview itself,
// because it owns real side effects (files, permissions, a manifest)
// that logview's own pure parse/discover/merge code deliberately
// never has — the same "a genuinely larger workflow gets its own
// package" rule this project's own architecture notes ask for.
package logdiskcache

import (
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/jagottsicher/breakthrough/internal/logview"
)

// dirMode/fileMode: a rotated file cached here can be another
// machine's auth.log or anything else security-sensitive — its own
// content survives on this disk for as long as the cache keeps it,
// not just for this process's lifetime, so it gets the same
// no-other-user-can-read posture a private SSH key would.
const (
	dirMode  = 0o700
	fileMode = 0o600
)

// manifestFileName lives at the cache root, next to the per-identity
// directories — one file, not one per identity, so the byte-budget
// LRU (which must compare "least recently used" across every cached
// connection, not within just one) and the Options screen's own
// per-connection size listing both have one consistent, atomically
// rewritten source of truth to read instead of needing to open every
// identity directory just to answer "how much is cached, for whom".
const manifestFileName = "index.json"

// cacheFormatVersion guards the on-disk entry encoding — bumped
// whenever logview.Entry's own shape changes in a way gob can't
// transparently absorb (a renamed or retyped field), so a stale cache
// from a previous breakthrough version is detected and treated as a
// miss instead of either failing to decode or, worse, decoding into
// garbage.
const cacheFormatVersion = 1

// Cache is one open on-disk cache rooted at a single directory —
// typically $XDG_CACHE_HOME/breakthrough/logaudit. Not safe for
// concurrent use from more than one *Cache instance pointed at the
// same root (e.g. two breakthrough processes sharing one cache
// directory): the manifest is read-modify-written as a whole file,
// so two processes racing a Put each can lose the other's update.
// Documented, not solved, the same way logview.RotatedCache's own doc
// comment documents its single-goroutine-only contract rather than
// adding a mutex that would only paper over a problem this type
// cannot see across process boundaries anyway. Within one process,
// mu serializes every method below.
type Cache struct {
	root string
	mu   sync.Mutex
	man  manifest
	// dirty marks a manifest change Get's own LastUse bump made that
	// hasn't been written to disk yet — see Get's own doc comment for
	// why a hit deliberately doesn't call saveManifest itself (a
	// 40-rotation family would otherwise mean 40 full manifest
	// rewrites on the very path whose entire purpose is being fast).
	// Flush persists it; Put always saves immediately regardless of
	// this flag, since Put already does comparable disk I/O for the
	// cache file itself and a newly written entry's own bookkeeping
	// matters more to get durably recorded than a bump to an existing
	// entry's recency does.
	dirty bool
}

// manifest is the whole index.json — identities keyed by their own
// hashed directory name (see identityDir), so looking up "does this
// identity already have a directory" never needs a directory listing.
type manifest struct {
	// Seq is a monotonic "last used" clock (like RotatedCache's own),
	// not wall-clock time, shared across every identity so eviction
	// can compare "least recently used" globally — one connection's
	// own file being older in wall-clock terms than another's doesn't
	// matter, only which was actually Get/Put more recently does.
	Seq        int64
	Identities map[string]*identityManifest
	// TotalBytes is the sum of every identity's own TotalBytes —
	// kept denormalized so Put's own budget check never has to walk
	// every identity to answer "are we over the limit".
	TotalBytes int64
}

type identityManifest struct {
	// Label is the identity string as currentLogAuditSourceIdentity
	// (internal/ui) produces it, e.g. "remote:user@host:22" — kept in
	// plaintext specifically so the Options screen can list which
	// connections have cached data without reversing a hash.
	Label      string
	Files      map[string]*fileManifest
	TotalBytes int64
}

type fileManifest struct {
	Bytes   int64
	LastUse int64
}

// Open creates root (and its manifest) if it doesn't exist yet, or
// loads the existing manifest if it does. A corrupt or
// version-mismatched manifest is treated as "start fresh", not a
// fatal error — the worst case is every file looking like a miss
// again, exactly like a cold cache, never data loss for anything that
// matters (the real log files on the remote host are untouched
// either way). "Start fresh" also wipes every identity directory
// already on disk at root: an unreadable manifest means those
// directories are unreachable by construction (nothing in a fresh,
// empty manifest will ever name them again), so leaving them behind
// would silently orphan however much of the byte budget they hold —
// TotalBytes would report far less than root actually occupies, and
// eviction could never reclaim it, on a feature whose entire
// settings surface (see feature_ideas.txt, section 19d) assumes those
// two numbers agree.
func Open(root string) (*Cache, error) {
	if err := os.MkdirAll(root, dirMode); err != nil {
		return nil, fmt.Errorf("logdiskcache: creating cache root: %w", err)
	}
	c := &Cache{root: root, man: manifest{Identities: map[string]*identityManifest{}}}
	manifestPath := filepath.Join(root, manifestFileName)
	data, err := os.ReadFile(manifestPath)
	if err == nil {
		var m manifest
		if jsonErr := json.Unmarshal(data, &m); jsonErr == nil && m.Identities != nil {
			c.man = m
			return c, nil
		}
	}
	if err := wipeExcept(root, manifestFileName); err != nil {
		return nil, fmt.Errorf("logdiskcache: clearing orphaned cache data: %w", err)
	}
	return c, nil
}

// wipeExcept removes every entry directly inside dir except keep —
// Open's own "start fresh" fallback, kept as a small named helper so
// the reasoning above stays next to Open itself instead of inline
// loop logic.
func wipeExcept(dir, keep string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == keep {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// identityDir names identity's own on-disk subdirectory — hashed
// rather than the raw identity string, both because a raw
// "remote:user@host:22" is not a safe path component as-is (escaping
// hazard from an unusual username/host) and because a directory
// listing of the cache root must not casually leak the user's own
// connection inventory to anything that can `ls` the filesystem (see
// feature_ideas.txt's own "Sensible Daten" note on this cache).
func identityDir(identity string) string {
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:16])
}

// fileName names key's own on-disk cache file within its identity's
// directory — same hashing reasoning as identityDir, applied to the
// path|size|mtime key callers already use for logview.RotatedCache
// (see internal/ui's logAuditCacheKey), so a cache file name never
// has to encode a possibly-very-long remote path as-is.
func fileName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:16]) + ".cache"
}

// Get returns key's cached entries for identity, or (nil, false) on a
// miss — including a version mismatch, a missing file, a decode
// error, or ctx already being done, all treated identically as "not
// cached", the same fail-open-to-a-cold-read posture Open's own doc
// comment already takes: a miss here is never fatal, the caller
// always has the real remote read as a fallback. A hit promotes the
// entry to most-recently-used exactly like RotatedCache.Get already
// does, so Put's own eviction order reflects real access, not
// insertion order — but unlike RotatedCache.Get, that promotion is
// only applied to the in-memory manifest here, not saved to disk
// immediately (see Cache.dirty's own doc comment); call Flush once a
// batch of Get calls is done.
//
// ctx is checked once before any I/O, matching this app's own
// existing per-file (not sub-file) cancellation granularity for a Log
// Audit load — see startLogAuditProgressiveLoad's own ctx.Err checks
// in internal/ui/logaudit.go, which likewise never abort mid-read of
// one already-started file. Get must take a context at all because,
// unlike the in-memory RotatedCache.Get it parallels, this one does
// real file I/O and a gob decode that can run for the better part of
// a second on a large family (measured 2026-11-04, see
// feature_ideas.txt section 19d) — calling it from the UI goroutine's
// own synchronous pre-pass the way RotatedCache.Get is called today
// would freeze the UI exactly like the 19a freeze bug this project
// already fixed once; callers must run it from the same background
// goroutine that already does the real network read, trying disk
// first.
func (c *Cache) Get(ctx context.Context, identity, key string) ([]logview.Entry, bool) {
	if ctx.Err() != nil {
		return nil, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	im, ok := c.man.Identities[identityDir(identity)]
	if !ok {
		return nil, false
	}
	fm, ok := im.Files[fileName(key)]
	if !ok {
		return nil, false
	}

	path := filepath.Join(c.root, identityDir(identity), fileName(key))
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()

	var rec cacheRecord
	if err := gob.NewDecoder(f).Decode(&rec); err != nil || rec.Version != cacheFormatVersion {
		return nil, false
	}
	if ctx.Err() != nil {
		return nil, false
	}

	realiasEntries(rec.Entries)

	c.man.Seq++
	fm.LastUse = c.man.Seq
	c.dirty = true

	return rec.Entries, true
}

// Flush persists any manifest changes Get accumulated since the last
// Flush or Put — callers do one Get per rotated file during a
// progressive load (see startLogAuditProgressiveLoad), so flushing
// once at the end of that load, the same point RAM's own
// finishLogAuditProgressiveLoad does its own final, unconditional
// recombine, keeps this to one manifest rewrite per load instead of
// one per file. A no-op, not an error, when nothing is dirty.
func (c *Cache) Flush() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return nil
	}
	if err := c.saveManifest(); err != nil {
		return err
	}
	c.dirty = false
	return nil
}

// cacheRecord is the whole on-disk payload for one cached file —
// Version first so a future format change can be detected by
// decoding just that field's own tag, without gob choking on the
// rest, though in practice gob already refuses to decode a struct
// whose field types changed incompatibly; Version exists for the
// subtler case of a field being reinterpreted with the same Go type
// but different meaning, which gob itself cannot detect at all.
type cacheRecord struct {
	Version int
	Entries []logview.Entry
}

// Put stores entries under identity/key, evicting whichever cached
// file (across every identity, not just this one — see manifest's own
// doc comment on why the budget is global) was least recently used
// until back under maxBytes. A non-positive maxBytes turns the cache
// off: any existing entry under identity/key is removed and nothing
// new is written, the same "0 or negative means off" contract
// RotatedCache.Put already establishes for its own byte budget.
func (c *Cache) Put(identity, key string, entries []logview.Entry, maxBytes int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.removeLocked(identity, key)

	if maxBytes <= 0 {
		return c.saveManifest()
	}

	idDir := identityDir(identity)
	fname := fileName(key)
	dirPath := filepath.Join(c.root, idDir)
	if err := os.MkdirAll(dirPath, dirMode); err != nil {
		return fmt.Errorf("logdiskcache: creating identity directory: %w", err)
	}

	filePath := filepath.Join(dirPath, fname)
	tmpPath := filePath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fileMode)
	if err != nil {
		return fmt.Errorf("logdiskcache: creating cache file: %w", err)
	}
	if err := gob.NewEncoder(f).Encode(cacheRecord{Version: cacheFormatVersion, Entries: entries}); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("logdiskcache: encoding cache file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("logdiskcache: closing cache file: %w", err)
	}
	fi, err := os.Stat(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("logdiskcache: statting cache file: %w", err)
	}
	size := fi.Size()
	if size > maxBytes {
		// A single file bigger than the whole budget is never
		// inserted at all — same reasoning as RotatedCache's own
		// doc comment: letting it in would mean evicting literally
		// everything else just to fit one file.
		_ = os.Remove(tmpPath)
		return c.saveManifest()
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("logdiskcache: finalizing cache file: %w", err)
	}

	im, ok := c.man.Identities[idDir]
	if !ok {
		im = &identityManifest{Label: identity, Files: map[string]*fileManifest{}}
		c.man.Identities[idDir] = im
	}
	c.man.Seq++
	im.Files[fname] = &fileManifest{Bytes: size, LastUse: c.man.Seq}
	im.TotalBytes += size
	c.man.TotalBytes += size

	for c.man.TotalBytes > maxBytes {
		if !c.evictOldestLocked(idDir, fname) {
			break
		}
	}

	return c.saveManifest()
}

// evictOldestLocked drops whichever cached file across every identity
// has the smallest LastUse, except the one just written
// (idDir/fname) — Put's own entry must never evict itself immediately
// after being the only thing over budget, which a plain global
// smallest-LastUse scan would otherwise do since it was only just
// inserted but every *other* file could still coincidentally be
// larger. Returns false once nothing else is left to evict (the
// just-written file alone exceeds maxBytes, already handled by Put's
// own "bigger than the whole budget" check before this is ever
// reached in practice).
func (c *Cache) evictOldestLocked(keepIDDir, keepFName string) bool {
	var oldestIDDir, oldestFName string
	var oldestUse int64
	found := false
	for idDir, im := range c.man.Identities {
		for fname, fm := range im.Files {
			if idDir == keepIDDir && fname == keepFName {
				continue
			}
			if !found || fm.LastUse < oldestUse {
				oldestIDDir, oldestFName, oldestUse = idDir, fname, fm.LastUse
				found = true
			}
		}
	}
	if !found {
		return false
	}
	c.removeFileLocked(oldestIDDir, oldestFName)
	return true
}

// removeLocked drops identity/key's own cached file, if present —
// shared by Put's own "overwrite means remove old first" step and by
// the public Clear* methods below.
func (c *Cache) removeLocked(identity, key string) {
	c.removeFileLocked(identityDir(identity), fileName(key))
}

func (c *Cache) removeFileLocked(idDir, fname string) {
	im, ok := c.man.Identities[idDir]
	if !ok {
		return
	}
	fm, ok := im.Files[fname]
	if !ok {
		return
	}
	_ = os.Remove(filepath.Join(c.root, idDir, fname))
	delete(im.Files, fname)
	im.TotalBytes -= fm.Bytes
	c.man.TotalBytes -= fm.Bytes
	if len(im.Files) == 0 {
		delete(c.man.Identities, idDir)
		_ = os.Remove(filepath.Join(c.root, idDir))
	}
}

// ClearAll removes every cached file for every identity — the
// Options screen's own blanket "empty the disk cache" action.
func (c *Cache) ClearAll() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for idDir := range c.man.Identities {
		_ = os.RemoveAll(filepath.Join(c.root, idDir))
	}
	c.man.Identities = map[string]*identityManifest{}
	c.man.TotalBytes = 0
	return c.saveManifest()
}

// ClearIdentity removes only identity's own cached files — the
// Options screen's own per-connection clear, independent of every
// other cached connection's own data.
func (c *Cache) ClearIdentity(identity string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	idDir := identityDir(identity)
	im, ok := c.man.Identities[idDir]
	if !ok {
		return nil
	}
	_ = os.RemoveAll(filepath.Join(c.root, idDir))
	c.man.TotalBytes -= im.TotalBytes
	delete(c.man.Identities, idDir)
	return c.saveManifest()
}

// IdentitySummary describes one cached connection for the Options
// screen's own per-connection listing — Label in plaintext (see
// identityManifest's own doc comment for why it's stored that way),
// Bytes/Files already summed so the UI never has to walk the
// manifest's own nested maps itself.
type IdentitySummary struct {
	Label string
	Bytes int64
	Files int
}

// Identities lists every identity with at least one cached file,
// Label-sorted so the Options screen's own listing has a stable,
// predictable order across repeated opens rather than Go's own
// randomized map iteration order.
func (c *Cache) Identities() []IdentitySummary {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]IdentitySummary, 0, len(c.man.Identities))
	for _, im := range c.man.Identities {
		out = append(out, IdentitySummary{Label: im.Label, Bytes: im.TotalBytes, Files: len(im.Files)})
	}
	sortIdentitySummaries(out)
	return out
}

// TotalBytes reports the cache's own current total across every
// identity — the Options screen's own "x of y GB used" display.
func (c *Cache) TotalBytes() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.man.TotalBytes
}

// saveManifest rewrites index.json atomically (write to a temp file,
// then rename) so a crash or power loss mid-write never leaves a
// half-written, unparseable manifest behind — Open's own "corrupt
// manifest means start fresh" fallback exists for defense in depth,
// not as the primary way this is kept consistent.
func (c *Cache) saveManifest() error {
	data, err := json.Marshal(c.man)
	if err != nil {
		return fmt.Errorf("logdiskcache: encoding manifest: %w", err)
	}
	tmpPath := filepath.Join(c.root, manifestFileName+".tmp")
	if err := os.WriteFile(tmpPath, data, fileMode); err != nil {
		return fmt.Errorf("logdiskcache: writing manifest: %w", err)
	}
	if err := os.Rename(tmpPath, filepath.Join(c.root, manifestFileName)); err != nil {
		return fmt.Errorf("logdiskcache: finalizing manifest: %w", err)
	}
	return nil
}
