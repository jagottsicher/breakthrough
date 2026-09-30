package ui

import (
	"fmt"

	"github.com/rivo/tview"
)

// The "mR"/"mC" chords' own dialog — a mnemonic, keyboard-first way to
// cancel specifically a running Rsync or Compress/Extract (never both
// at once the way Ctrl+C's own fallback does when no Paste is running —
// see RequestCancel), with the same "current job only" vs "current job
// plus everything still queued behind it" choice pasteJob's own pause
// dialog already gives Paste (see pastepause.go), asked up front here
// instead of after a pause: unlike a Paste, which can genuinely be
// paused mid-walk, a backgrounded rsync/Compress process is either
// running or it isn't — there's nothing to pause, so the choice is
// asked before anything is touched rather than after.
//
// One shared dialog for both kinds, the same "one widget, repopulated
// per open" shape confirmDialog already has for every one-off yes/no
// question in this app, rather than two near-identical ones: only the
// noun in the question and the two actions actually wired to its own
// two real answers differ between a Rsync and a Compress/Extract open.

// bgJobCancelCurrentItem/bgJobCancelAllItem/bgJobCancelNeverMindItem are
// r.bgJobCancelDialog's own item indices — named the same reason every
// other fixed-position dialog list in this app already is.
const (
	bgJobCancelCurrentItem = iota
	bgJobCancelAllItem
	bgJobCancelNeverMindItem
)

// newBgJobCancelDialog builds r.bgJobCancelDialog once, called from
// NewRoot the same way newPasteConflictDialog/newConfirmDialog are.
// "Never mind" is this dialog's own safe default (see
// openRsyncCancelDialog/openCompressCancelDialog's own SetCurrentItem
// call and SetDoneFunc below) — both of the other two answers are
// destructive, so, unlike confirmDialog's own confirm-first-but-
// Cancel-preselected shape, the safe one is listed last and
// preselected, mirroring pastePauseDialog's own "Continue" being the
// one non-destructive answer, just at the opposite end of the list
// here since there are two destructive answers to list first instead
// of one.
func (r *Root) newBgJobCancelDialog() *tview.List {
	l := tview.NewList().ShowSecondaryText(false)
	l.SetHighlightFullLine(true)
	l.SetBorderPadding(0, 0, 1, 1)
	l.AddItem("", "", 0, func() { r.runBgJobCancel(r.pendingBgJobCancelCurrent) })
	l.AddItem("", "", 0, func() { r.runBgJobCancel(r.pendingBgJobCancelAll) })
	l.AddItem("Never mind", "", 0, func() { r.hideOverlay() })
	l.SetDoneFunc(func() { r.hideOverlay() }) // Escape - the safe default
	return l
}

// runBgJobCancel is both of r.bgJobCancelDialog's own destructive
// answers' shared tail — action is whichever of
// pendingBgJobCancelCurrent/pendingBgJobCancelAll the chosen item
// captured at open time (see openRsyncCancelDialog/
// openCompressCancelDialog), nil-checked the same defensive way
// acceptConfirm already checks r.pendingConfirm: both are only ever set
// together, right before this dialog opens, but a nil check here costs
// nothing and keeps this from ever being the one place a future caller
// forgets to set one.
func (r *Root) runBgJobCancel(action func()) {
	r.hideOverlay()
	if action != nil {
		action()
	}
	r.refreshStatusBar()
}

// openRsyncCancelDialog is the "mR" chord's own action — a no-op if
// there's genuinely nothing to cancel (no running rsync, and nothing
// queued behind one that already finished on its own), the same
// "nothing to do, do nothing" contract cancelRsyncJob's own nil check
// already has, rather than opening a dialog with no meaningful answer
// to give.
func (r *Root) openRsyncCancelDialog() {
	if r.rsyncJob == nil && len(r.rsyncQueue) == 0 {
		return
	}
	r.pendingBgJobCancelCurrent = r.cancelRsyncKeepQueue
	r.pendingBgJobCancelAll = r.cancelRsyncJob
	r.renderBgJobCancelDialog("Rsync", len(r.rsyncQueue))
	r.resizeBgJobCancelDialog()
	r.bgJobCancelDialog.SetCurrentItem(bgJobCancelNeverMindItem)
	r.showOverlay(bgJobCancelPage, r.bgJobCancelDialog)
}

// openCompressCancelDialog is openRsyncCancelDialog's own Compress/
// Extract counterpart — see its own doc comment for the shared
// reasoning throughout.
func (r *Root) openCompressCancelDialog() {
	if r.compressJob == nil && len(r.compressQueue) == 0 {
		return
	}
	r.pendingBgJobCancelCurrent = r.cancelCompressKeepQueue
	r.pendingBgJobCancelAll = r.cancelCompressJob
	r.renderBgJobCancelDialog("Compress/Extract", len(r.compressQueue))
	r.resizeBgJobCancelDialog()
	r.bgJobCancelDialog.SetCurrentItem(bgJobCancelNeverMindItem)
	r.showOverlay(bgJobCancelPage, r.bgJobCancelDialog)
}

// renderBgJobCancelDialog fills in bgJobCancelDialogTitleBar's own text
// and both destructive items' own labels for kind ("Rsync" or
// "Compress/Extract") — the question IS the header, the same treatment
// every other dialog in this app already gets.
func (r *Root) renderBgJobCancelDialog(kind string, queued int) {
	r.bgJobCancelDialogTitleBar.SetText(fmt.Sprintf(" Cancel the running %s job? ", kind))
	r.bgJobCancelDialog.SetItemText(bgJobCancelCurrentItem, fmt.Sprintf("Cancel current %s job, keep queue", kind), "")
	all := fmt.Sprintf("Cancel everything (%s)", kind)
	if queued > 0 {
		all = fmt.Sprintf("Cancel everything (%s, %d queued)", kind, queued)
	}
	r.bgJobCancelDialog.SetItemText(bgJobCancelAllItem, all, "")
}

// resizeBgJobCancelDialog sizes and re-centers r.bgJobCancelDialog for
// whatever renderBgJobCancelDialog just set — the exact same
// listSize/centeredOnScreen/SetRect sequence resizePasteConflictDialog/
// resizePastePauseDialog already use, for the identical reason (a
// header whose width varies with live state, not fixed at build time).
func (r *Root) resizeBgJobCancelDialog() {
	width, height := listSize(r.bgJobCancelDialog)
	if headerWidth := tview.TaggedStringWidth(r.bgJobCancelDialogTitleBar.GetText(false)); headerWidth > width {
		width = headerWidth
	}
	height++ // reserved title bar row (see bgJobCancelDialogLayout)
	x, y := r.centeredOnScreen(width, height)
	r.bgJobCancelDialogLayout.SetRect(x, y, width, height)
	r.bgJobCancelDialog.SetRect(x, y, width, height)
}

// confirmCancelCurrentRsync/confirmCancelCurrentCompress/
// confirmCancelCurrentPaste back the status bar's own "✕" button on
// each of the three background-job progress segments (see
// bottombar.go) — per the user's own explicit request, clicking it
// always means "stop just the current job, never the queue behind it"
// (the queue-dropping "Cancel everything" answer stays "mR"/"mC"/
// Ctrl+C's own job, not a single click's), but still asks first,
// reusing r.confirmDialog (see openConfirm) rather than a bespoke
// dialog for a plain yes/no question. A no-op if the job already
// finished on its own before the click was even processed — the same
// "nothing to do" contract every cancel function in this file already
// has.
func (r *Root) confirmCancelCurrentRsync() {
	if r.rsyncJob == nil {
		return
	}
	r.openConfirm("Stop the current Rsync job?", "Yes, stop it", r.cancelRsyncKeepQueue)
}

func (r *Root) confirmCancelCurrentCompress() {
	if r.compressJob == nil {
		return
	}
	r.openConfirm("Stop the current Compress/Extract job?", "Yes, stop it", r.cancelCompressKeepQueue)
}

// confirmCancelCurrentPaste is confirmCancelCurrentRsync's own Paste
// counterpart — "Copy"/"Move" in the question itself, the same
// distinction renderPastePauseDialog's own verb already draws, so the
// question reads as what's actually running rather than a generic
// "Paste" neither term on its own.
func (r *Root) confirmCancelCurrentPaste() {
	job := r.pasteJob
	if job == nil {
		return
	}
	verb := "Copy"
	if job.cut {
		verb = "Move"
	}
	r.openConfirm(fmt.Sprintf("Stop the current %s job?", verb), "Yes, stop it", r.cancelPastePauseKeepQueue)
}
