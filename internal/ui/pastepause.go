package ui

import (
	"fmt"
	"path/filepath"

	"github.com/rivo/tview"
)

// Ctrl+C's own answer while a Paste (Copy or Cut) is running: rather
// than killing the job outright the instant it's pressed (this app's
// own long-standing behavior, and still exactly what happens for a
// backgrounded Rsync or Compress/Extract — see RequestCancel), it
// pauses the walk — the file currently streaming still finishes (a
// Copy) or is interrupted with its source left untouched (a Cut — see
// pasteOne's own doc comment on why the two differ), but nothing
// further starts — and opens this dialog to ask what to do next: keep
// going, stop just this job and let whatever's queued behind it run,
// or abandon the whole queue. A real, user-requested feature: Ctrl+C's
// own instant, unconditional cancel had no way to say "actually, just
// skip this one" once a Paste was already several files into a large
// queued batch, short of letting it finish or losing every item still
// waiting behind it.
//
// Deliberately scoped to Paste alone — Rsync and Compress/Extract keep
// running untouched even while this dialog is open (see
// renderPastePauseDialog's own notice when either is currently active):
// unlike Paste's own conflict-resolving walk, neither of those has
// anything mid-job a user might want to steer around, so there was
// nothing this dialog would usefully add for them.

// pastePauseContinueItem/pastePauseCancelCurrentItem/
// pastePauseCancelAllItem are r.pastePauseDialog's own item indices —
// named rather than left as bare numbers the same reason
// pasteConflictOverwriteItem and friends already are.
const (
	pastePauseContinueItem = iota
	pastePauseCancelCurrentItem
	pastePauseCancelAllItem
)

// newPastePauseDialog builds r.pastePauseDialog once, called from
// NewRoot the same way newPasteConflictDialog is — a List, the same
// reasoning every other dialog in this app already follows (keyboard
// navigation, a clickable legend, consistent with everything else
// here). "Continue" is listed first and is this dialog's own safe
// default (see requestPastePause's own SetCurrentItem call and
// SetDoneFunc below) — the reverse of confirmDialog's own
// confirming-answer-first-but-Cancel-preselected shape, because here
// it's the other two answers that are destructive, not this one.
func (r *Root) newPastePauseDialog() *tview.List {
	l := tview.NewList().ShowSecondaryText(false)
	l.SetHighlightFullLine(true)
	l.SetBorderPadding(0, 0, 1, 1)
	l.AddItem("Continue", "", 0, r.resumePastePause)
	l.AddItem("Cancel current job, keep queue", "", 0, r.cancelPastePauseKeepQueue)
	l.AddItem("Cancel everything", "", 0, r.cancelPastePauseEverything)
	l.SetDoneFunc(r.resumePastePause) // Escape - the one non-destructive answer
	return l
}

// requestPastePause is Ctrl+C's own entry point while r.pasteJob is
// running (see RequestCancel) — pauses the job (see pasteJob.pause's
// own doc comment for exactly what that does and doesn't stop) and
// shows this dialog. Also reached with r.activePage already
// pasteConflictPage (a conflict was on screen the moment Ctrl+C landed)
// — showOverlay below closes it the same way it closes any other
// overlay already open, and resumePastePause's own doc comment covers
// how that conflict finds its way back on screen once this dialog is
// answered rather than being silently stranded.
func (r *Root) requestPastePause() {
	job := r.pasteJob
	if job == nil {
		return
	}
	job.pause()
	r.renderPastePauseDialog(job)
	r.resizePastePauseDialog()
	r.pastePauseDialog.SetCurrentItem(pastePauseContinueItem)
	r.showOverlay(pastePausePage, r.pastePauseDialog)
}

// renderPastePauseDialog fills in pastePauseDialogTitleBar's own text —
// the question IS the header, the same treatment every other dialog in
// this app already gets. Names the file currently in flight, if any is
// known yet (job.currentFile can still be nil/empty this early in a
// very small job — see pasteOne's own doc comment on when it's set),
// how many more Pastes are already queued behind this one, and —
// per the user's own explicit request — an unambiguous note that a
// currently-running Rsync or Compress/Extract keeps going regardless of
// whatever answer this dialog gets, since neither is affected by it at
// all (see this file's own package doc comment).
func (r *Root) renderPastePauseDialog(job *pasteJob) {
	verb := "Copying"
	if job.cut {
		verb = "Moving"
	}
	msg := verb + " paused."
	if current := job.currentFile.Load(); current != nil && *current != "" {
		msg = fmt.Sprintf("%s paused after %q.", verb, filepath.Base(*current))
	}
	if n := len(r.pasteQueue); n > 0 {
		msg += fmt.Sprintf(" (%d more queued)", n)
	}
	if r.rsyncJob != nil || r.compressJob != nil {
		msg += " Rsync/Compress keep running."
	}
	r.pastePauseDialogTitleBar.SetText(" " + msg + " ")
}

// resizePastePauseDialog sizes and re-centers r.pastePauseDialog for
// whatever renderPastePauseDialog just set — the exact same
// listSize/centeredOnScreen/SetRect sequence resizePasteConflictDialog
// already uses, for the identical reason (a header whose width varies
// with live state, not fixed at build time).
func (r *Root) resizePastePauseDialog() {
	width, height := listSize(r.pastePauseDialog)
	if headerWidth := tview.TaggedStringWidth(r.pastePauseDialogTitleBar.GetText(false)); headerWidth > width {
		width = headerWidth
	}
	height++ // reserved title bar row (see pastePauseDialogLayout)
	x, y := r.centeredOnScreen(width, height)
	r.pastePauseDialogLayout.SetRect(x, y, width, height)
	// r.pastePauseDialog's own rect is also set, to the same full area —
	// captureOutsideClick's own bounds check reads r.activeWidget.GetRect()
	// directly, and r.activeWidget stays r.pastePauseDialog (the real
	// focus target — see requestPastePause), not pastePauseDialogLayout.
	// See resizePasteConflictDialog's own doc comment for the same
	// reasoning in full.
	r.pastePauseDialog.SetRect(x, y, width, height)
}

// resumePastePause is "Continue"'s own action (and Escape's, via
// SetDoneFunc) — releases the job to keep walking exactly where it left
// off (see pasteJob.resume). If a conflict was still waiting to be
// answered the moment Ctrl+C interrupted it (job.current != nil — see
// requestPastePause's own doc comment on reaching this dialog from
// pasteConflictPage), it's shown again here rather than left stranded:
// an unconditional hideOverlay would otherwise silently abandon it the
// same way a stray outside click on it once could, before
// captureOutsideClick's own pasteConflictPage exception existed to stop
// that.
func (r *Root) resumePastePause() {
	job := r.pasteJob
	if job == nil {
		r.hideOverlay()
		return
	}
	job.resume()
	if job.current != nil {
		r.renderPasteConflictDialog(job)
		r.resizePasteConflictDialog()
		r.pasteConflictDialog.SetCurrentItem(pasteConflictSkipItem)
		r.showOverlay(pasteConflictPage, r.pasteConflictDialog)
	} else {
		r.hideOverlay()
	}
	r.refreshStatusBar()
}

// cancelPastePauseKeepQueue is "Cancel current job, keep queue"'s own
// action — stops just this one job (see pasteJob.cancel/pasteOne's own
// doc comment on what that does and doesn't interrupt) and, unlike
// cancelPasteJob's own "stop everything" contract, goes straight on to
// whatever else is already queued behind it instead of discarding that
// too. job.current, if a conflict happened to still be pending, is
// simply abandoned along with the rest of this job's own state — safe
// here specifically because the whole job is being torn down anyway
// (r.pasteJob set to nil below), unlike resumePastePause's own case
// where the job survives and a stranded job.current would matter.
func (r *Root) cancelPastePauseKeepQueue() {
	job := r.pasteJob
	r.hideOverlay()
	if job == nil {
		return
	}
	job.cancel()
	r.pasteJob = nil
	r.advancePasteQueue()
	r.refreshStatusBar()
}

// cancelPastePauseEverything is "Cancel everything"'s own action —
// exactly cancelPasteJob's existing "stop this job and drop the whole
// queue behind it" contract (see its own doc comment), reached through
// the pause dialog instead of Ctrl+C's own former direct call.
func (r *Root) cancelPastePauseEverything() {
	r.hideOverlay()
	r.cancelPasteJob()
	r.refreshStatusBar()
}
