package ui

import (
	"context"
	"fmt"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/notify"
)

// startOrphanLabelScan is the Options screen's own "Remove orphaned
// labels" button (see optioncatalog.go): scans every currently
// labeled path in the background (see filelabels.Store.FindOrphans's
// own doc comment for exactly what counts as orphaned) without
// blocking the UI, then — once it finishes — reports "N orphaned
// label(s) found, remove them?" and only actually deletes anything
// once that's confirmed.
//
// A no-op if a scan is already running (r.labelScanCancel != nil):
// nothing here queues a second one the way Paste/Rsync/Compress queue
// further jobs, since re-running the exact same scan while one is
// already in flight has nothing new to offer over just waiting for it.
func (r *Root) startOrphanLabelScan() {
	if r.labels == nil {
		r.showError(errLabelsUnavailable)
		return
	}
	if r.labelScanCancel != nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.labelScanCancel = cancel
	r.refreshStatusBar()

	go func() {
		orphans := r.labels.FindOrphans(ctx)
		r.app.QueueUpdateDraw(func() {
			r.finishOrphanLabelScan(ctx, orphans)
		})
	}()
}

// cancelOrphanLabelScan is the status bar's own "✕" for a running scan
// (see buildStatusBar's labelScanCancelSpan) — no confirmation asked,
// the same as Paste's own Ctrl+C pause: a label scan reads the
// filesystem, nothing more, so there's nothing destructive to weigh
// against just stopping it.
func (r *Root) cancelOrphanLabelScan() {
	if r.labelScanCancel != nil {
		r.labelScanCancel()
	}
}

// finishOrphanLabelScan runs once FindOrphans returns, on the UI
// thread (see startOrphanLabelScan's own QueueUpdateDraw) — clears the
// running state first, unconditionally, so the button/status-bar
// segment never gets stuck showing a scan as still in progress
// regardless of how it ended.
func (r *Root) finishOrphanLabelScan(ctx context.Context, orphans []string) {
	r.labelScanCancel = nil
	r.refreshStatusBar()

	if ctx.Err() != nil {
		return // cancelled — nothing to report
	}
	if len(orphans) == 0 {
		r.notify.Push(notify.LevelInfo, notify.CategoryLabel, "No orphaned labels found")
		return
	}

	what := fmt.Sprintf("%d orphaned label(s)", len(orphans))
	r.openConfirm(fmt.Sprintf("Remove %s? Their own files or directories no longer exist at the paths they were set on.", what), "Yes, remove", func() {
		r.removeOrphanLabels(orphans)
	})
}

// removeOrphanLabels is finishOrphanLabelScan's own confirmed action:
// clears every one of orphans in a single save (see
// filelabels.Store.SetMany) and repaints every open tab, the same as
// applyLabel already does for an ordinary label change.
func (r *Root) removeOrphanLabels(orphans []string) {
	if err := r.labels.SetMany(orphans, 0); err != nil {
		r.activityLog.Error(activitylog.CategoryFileOps, fmt.Sprintf("remove orphaned labels: %v", err))
		r.showError(err)
		return
	}
	r.activityLog.Action(activitylog.CategoryFileOps, fmt.Sprintf("removed %d orphaned label(s)", len(orphans)))
	r.notify.Push(notify.LevelSuccess, notify.CategoryLabel, fmt.Sprintf("Removed %d orphaned label(s)", len(orphans)))
	r.forEachTab(func(p *Panel) { p.repaintLabels() })
}
