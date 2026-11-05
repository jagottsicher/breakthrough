// logauditdiskcachemove.go is the Options screen's own "Change disk
// cache location" action — the one piece of Log Holepunch's disk
// cache with real blast radius (moving a potentially multi-gigabyte
// directory), kept in its own file for that reason.
package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/logdiskcache"
)

// activateLogAuditDiskCacheLocationRow is the "Disk cache location"
// row's own action — re-activating it while a move is already running
// cancels that move instead of starting a second, overlapping one;
// otherwise it prompts for a new path and, once that's validated,
// confirmed, and actually different from the current one, starts the
// move.
func (r *Root) activateLogAuditDiskCacheLocationRow() {
	if r.log.DiskCacheMoveCancel != nil {
		r.log.DiskCacheMoveCancel()
		return // finishLogAuditDiskCacheMove (via the move's own goroutine) clears the in-progress state once it actually stops
	}
	if r.log.LoadingMore {
		r.showError(errors.New("can't change the disk cache location while a Log Holepunch family is still loading — wait for it to finish, or close the viewer, then try again"))
		return
	}
	// openPromptOverOptions, not openPrompt: this is reached from
	// inside the Options screen (itself an overlay), where openPrompt's
	// own ordinary showOverlay semantics would close Options first —
	// see openPromptOverOptions' own doc comment.
	r.openPromptOverOptions("New disk cache location", r.logAuditDiskCacheRoot(), func(text string) {
		r.confirmLogAuditDiskCacheMove(strings.TrimSpace(text))
	})
}

// confirmLogAuditDiskCacheMove validates newPath and, only if there is
// real data to move, asks before actually moving it — adopting an
// empty/unavailable cache's new path is just a setting, nothing to
// confirm, the same way picking a Compress format isn't.
func (r *Root) confirmLogAuditDiskCacheMove(newPath string) {
	if newPath == "" {
		return // Escape/empty submit from the prompt — leave everything as it was
	}
	current := r.logAuditDiskCacheRoot()
	if currentAbs, err := filepath.Abs(current); err == nil {
		if newAbs, err := filepath.Abs(newPath); err == nil && newAbs == currentAbs {
			return // already there
		}
	}

	if err := logdiskcache.ValidateNewRoot(newPath); err != nil {
		r.showError(err)
		return
	}

	c := r.logDiskCache()
	if c == nil {
		// Nothing has actually been cached yet (or the disk cache is
		// unavailable for some other reason) — there's no existing
		// data to move, so just point future caching at the new path
		// directly, the same no-ceremony "pick a new value" contract
		// every other plain setting on this screen already has.
		r.applyLogAuditDiskCachePath(newPath)
		return
	}

	r.openConfirm(
		fmt.Sprintf("Move the disk cache (%s) to %q?", config.FormatByteSize(c.TotalBytes()), newPath),
		"Yes, move",
		func() { r.startLogAuditDiskCacheMove(c, newPath) },
	)
}

// applyLogAuditDiskCachePath adopts newPath as settings.LogAuditDiskCachePath
// with nothing to physically move — shared by confirmLogAuditDiskCacheMove's
// own "no existing cache" branch and finishLogAuditDiskCacheMove's own
// success branch, so both end up at the exact same post-condition
// (setting persisted, DiskCache/DiskCacheUnavailable reset so the very
// next logDiskCache call opens fresh at the new path).
func (r *Root) applyLogAuditDiskCachePath(newPath string) {
	r.settings.LogAuditDiskCachePath = newPath
	r.persistSetting("log_audit_disk_cache_path", newPath)
	r.log.DiskCache = nil
	r.log.DiskCacheUnavailable = false
	r.renderOptions()
}

// startLogAuditDiskCacheMove actually runs the move, in the background
// — c.Move itself does the real work (rename, or copy-then-delete on
// EXDEV, see internal/logdiskcache's own doc comment); this is just
// the safeGo/context/QueueUpdateDraw wiring every other background job
// in this app already follows (see e.g. computeHashes in
// properties.go). Re-checks LoadingMore right before actually
// starting, not just when the row was first activated — a progressive
// load could have started in the time it took to type a path and
// confirm the dialog.
func (r *Root) startLogAuditDiskCacheMove(c *logdiskcache.Cache, newPath string) {
	if r.log.LoadingMore {
		r.showError(errors.New("can't change the disk cache location while a Log Holepunch family is still loading — wait for it to finish, or close the viewer, then try again"))
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.log.DiskCacheMoveCancel = cancel
	r.log.DiskCacheMoveTarget = newPath
	r.renderOptions()

	onPanic := func() { r.finishLogAuditDiskCacheMove(newPath, errors.New("panic during disk cache move")) }
	r.safeGo("log audit disk cache move", onPanic, func() {
		err := c.Move(ctx, newPath)
		r.app.QueueUpdateDraw(func() {
			r.finishLogAuditDiskCacheMove(newPath, err)
		})
	})
}

// finishLogAuditDiskCacheMove ends a Move call once it actually
// returns — split out from startLogAuditDiskCacheMove specifically so
// a test can call it directly with an already-known result, without
// needing a real goroutine or Application.Run to drain
// QueueUpdateDraw first, the same shape finishHashComputation's own
// doc comment gives for the identical reason in properties.go.
// Cancellation (activateLogAuditDiskCacheLocationRow re-activated mid-
// move) is reported as a silent revert, not an error dialog — nothing
// changed, exactly as the user just asked for.
func (r *Root) finishLogAuditDiskCacheMove(newPath string, err error) {
	r.log.DiskCacheMoveCancel = nil
	r.log.DiskCacheMoveTarget = ""
	switch {
	case err == nil:
		r.applyLogAuditDiskCachePath(newPath)
	case errors.Is(err, context.Canceled):
		r.renderOptions()
	default:
		r.showError(err)
		r.renderOptions()
	}
}
