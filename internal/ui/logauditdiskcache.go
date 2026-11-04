// logauditdiskcache.go bridges internal/logdiskcache into the Log
// Audit screen and the Options screen's own clear actions — opening
// the cache lazily, once, and exposing it as nil whenever it can't be
// used, so every caller already has to handle "disk caching isn't
// available right now" as the ordinary, non-fatal case it is for a
// feature whose whole point is an optional speedup, not a dependency
// anything else relies on.
package ui

import (
	"path/filepath"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/logdiskcache"
)

// logAuditDiskCacheSubdir is where the disk cache lives under
// config.UserCacheDir() — its own subdirectory, not the cache root
// itself, so a future second disk-backed cache in this app never has
// to share one flat directory (and one manifest file format) with
// this one.
const logAuditDiskCacheSubdir = "logaudit"

// logDiskCache returns the lazily opened, process-lifetime disk cache,
// or nil if it isn't available — no XDG cache directory could be
// resolved, or Open itself failed (e.g. a permissions problem). Tried
// at most once per process: a failure here is assumed to still be a
// failure on the very next call within the same run (the underlying
// cause, if any, won't have changed), so this doesn't retry Open on
// every single progressive load.
func (r *Root) logDiskCache() *logdiskcache.Cache {
	if r.log.DiskCache != nil {
		return r.log.DiskCache
	}
	if r.log.DiskCacheUnavailable {
		return nil
	}
	dir := config.UserCacheDir()
	if dir == "" {
		r.log.DiskCacheUnavailable = true
		return nil
	}
	c, err := logdiskcache.Open(filepath.Join(dir, logAuditDiskCacheSubdir))
	if err != nil {
		r.log.DiskCacheUnavailable = true
		return nil
	}
	r.log.DiskCache = c
	return c
}

// clearLogAuditDiskCache empties the whole disk cache, every cached
// connection at once — the Options screen's own blanket "Clear disk
// cache" action.
func (r *Root) clearLogAuditDiskCache() {
	c := r.logDiskCache()
	if c == nil {
		return
	}
	_ = c.ClearAll()
}

// clearLogAuditDiskCacheForCurrentConnection empties only whatever
// connection the active panel currently has (or "local", a harmless
// no-op, since the disk cache is remote-only by design) — the Options
// screen's own per-connection "Clear disk cache for this connection"
// action. Reads r.panel directly rather than needing a captured
// identity the way startLogAuditProgressiveLoad does, since this is a
// single, synchronous, user-triggered action with no async boundary
// for r.panel to change across.
func (r *Root) clearLogAuditDiskCacheForCurrentConnection() {
	c := r.logDiskCache()
	if c == nil {
		return
	}
	_ = c.ClearIdentity(r.currentLogAuditSourceIdentity())
}
