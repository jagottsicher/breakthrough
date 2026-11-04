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

// logAuditDiskCacheRoot resolves where the disk cache should live
// right now: settings.LogAuditDiskCachePath if the user has set one
// (via "Change disk cache location", or by hand-editing the config
// file — see that setting's own doc comment), otherwise
// config.UserCacheDir()'s own logAuditDiskCacheSubdir. "" (both
// settings.LogAuditDiskCachePath and this function's own result, if
// UserCacheDir itself can't resolve) means "no usable path at all".
func (r *Root) logAuditDiskCacheRoot() string {
	if r.settings.LogAuditDiskCachePath != "" {
		return r.settings.LogAuditDiskCachePath
	}
	return logAuditDiskCacheDefaultRoot()
}

// logAuditDiskCacheDefaultRoot is logAuditDiskCacheRoot's own
// built-in default, ignoring any override — what "Change disk cache
// location" moves back to when the user clears the path field instead
// of typing a new one (see confirmLogAuditDiskCacheMove), since
// logAuditDiskCacheRoot itself can't answer "what's the default"
// while an override is still set.
func logAuditDiskCacheDefaultRoot() string {
	dir := config.UserCacheDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, logAuditDiskCacheSubdir)
}

// logDiskCache returns the lazily opened, process-lifetime disk cache,
// or nil if it isn't available — no usable root could be resolved, or
// Open itself failed (e.g. a permissions problem, or the configured
// path already holds unrelated files — see isSafeRoot). Tried at most
// once per process: a failure here is assumed to still be a failure
// on the very next call within the same run (the underlying cause, if
// any, won't have changed), so this doesn't retry Open on every single
// progressive load. Reset to nil/false by startLogAuditDiskCacheMove
// once a move actually succeeds, so the very next call re-opens at the
// new root instead of keeping whatever was cached from the old one.
func (r *Root) logDiskCache() *logdiskcache.Cache {
	if r.log.DiskCache != nil {
		return r.log.DiskCache
	}
	if r.log.DiskCacheUnavailable {
		return nil
	}
	root := r.logAuditDiskCacheRoot()
	if root == "" {
		r.log.DiskCacheUnavailable = true
		return nil
	}
	c, err := logdiskcache.Open(root)
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
