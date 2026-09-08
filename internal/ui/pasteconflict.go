package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// fsCopy/fsMove are fsops.Copy/fsops.Move, indirected through package
// vars the same way dirSize/hashFile already are in detailssidebar.go/
// properties.go — so a test can substitute a controllable fake instead
// of touching a real filesystem, and can synchronize with pasteWalk's
// own background goroutine (see isolatePaste in pasteconflict_test.go).
var (
	fsCopy = fsops.Copy
	fsMove = fsops.Move
)

// conflictResolution is one way to handle a single paste conflict — the
// destination already has something at the path a clipboard item would
// otherwise land on. Only resolveOverwrite/resolveSkip make sense as a
// one-off, single-conflict answer; the other two are conditional rules,
// meant to be picked once and then applied to every conflict a paste
// runs into, not asked about individually (see conflictResolutionLabel
// and chooseConflictResolution's own "forAll" parameter).
type conflictResolution int

const (
	resolveOverwrite conflictResolution = iota
	resolveSkip
	resolveOverwriteIfNewer
	resolveOverwriteIfSourceNotEmpty
)

// pasteConflict is one paste target whose own destination already had
// something at it by the time pasteWalk got to it — discovered there,
// and now either shown in r.pasteConflictDialog, or queued behind
// whichever one already is (see pasteJob.pending).
type pasteConflict struct {
	src, dst string
	srcInfo  os.FileInfo
	dstInfo  os.FileInfo
}

// pasteJob is one Paste's own asynchronous, resumable state — see
// startPaste's own doc comment for the full shape end to end. Exactly
// one lives on Root at a time (r.pasteJob); starting a second Paste
// while one is still running cancels the first outright (see
// cancelPasteJob) rather than letting two independent walks race on the
// same destination directory.
type pasteJob struct {
	ctx    context.Context
	cancel context.CancelFunc

	cut     bool
	destDir string

	total int // clipboard items this job started with — never decremented, just for "N of M" reporting

	// remaining is decremented once for every clipboard item whose final
	// outcome (copied/moved, skipped, or errored) has been recorded —
	// see pasteItemDone. The job is finished once this reaches 0 *and*
	// no conflict is still pending or on screen (current/pending below).
	remaining int

	pending []pasteConflict // discovered, not yet shown or resolved
	current *pasteConflict  // the one currently in r.pasteConflictDialog, if any

	// forAll is set the moment a "for all remaining" choice is made —
	// every conflict discovered from then on (already queued in pending,
	// or found later by pasteWalk, which keeps running regardless)
	// resolves against it immediately, with no further dialog at all.
	forAll *conflictResolution

	errors []error // genuine failures (permission, I/O, a full disk, ...) — never conflicts, which are expected and always resolved one way or another
}

// startPaste replaces the old, fully synchronous "copy every clipboard
// item in a plain for loop" with an async, resumable one — per the
// user's own explicit request: a conflict (something already at dst)
// must not block the rest of the list from being copied/moved in the
// background while its own dialog is open, and a second conflict
// discovered before the first is resolved queues behind it instead of
// stacking a second dialog on top (see pasteConflictFound).
func (r *Root) startPaste(items []string, cut bool, destDir string) {
	if len(items) == 0 {
		return
	}
	r.cancelPasteJob()

	ctx, cancel := context.WithCancel(context.Background())
	job := &pasteJob{ctx: ctx, cancel: cancel, cut: cut, destDir: destDir, total: len(items), remaining: len(items)}
	r.pasteJob = job

	r.safeGo("paste", func() { r.pasteJob = nil }, func() {
		r.pasteWalk(job, items)
	})
}

// cancelPasteJob stops whatever paste is currently running, if any — a
// second Paste (or a real Cancel — see Root.RequestCancel) while one is
// still in flight. pasteWalk itself checks job.ctx.Err() between every
// item and stops there; anything already handed off to its own
// pasteOne goroutine finishes that one last file rather than being
// interrupted mid-write, but its result is discarded (see pasteOne's
// own job-identity check).
func (r *Root) cancelPasteJob() {
	if r.pasteJob == nil {
		return
	}
	r.pasteJob.cancel()
	if r.pasteJob.current != nil && r.activePage == pasteConflictPage {
		r.hideOverlay()
	}
	r.pasteJob = nil
}

// pasteWalk is startPaste's own background body — walks items in order,
// copying/moving each one immediately unless its own destination
// already exists, in which case it hands the conflict to the UI thread
// (see pasteConflictFound) and moves straight on to the next item
// without waiting for it to be resolved. Every item gets its own
// freshly-spawned goroutine (see pasteOne/pasteConflictFound) rather
// than running in this one, in order, one after another: tview's own
// Application.QueueUpdateDraw always blocks its caller until the event
// loop actually gets to it (verified directly against tview's own
// application.go, not assumed), so a plain, sequential loop calling it
// directly would have this whole walk stall on the very first item's
// own hand-off, defeating the entire point of any of this running in
// the background at all. This loop itself, then, only ever does a fast
// os.Lstat per item and immediately moves on — never anything that
// could itself block waiting on the UI thread.
//
// A real, accepted trade-off from spawning one goroutine per item
// rather than working through them one at a time: a very large paste
// (many hundreds of files at once) starts that many goroutines
// concurrently instead of bounding how many run at once. Fine for the
// sizes this app's own target use actually pastes at once; a worker
// pool would be the next step if that ever stops being true.
func (r *Root) pasteWalk(job *pasteJob, items []string) {
	for _, src := range items {
		if job.ctx.Err() != nil {
			return // superseded by a newer paste, or cancelled outright — see cancelPasteJob
		}
		src := src
		dst := filepath.Join(job.destDir, filepath.Base(src))

		dstInfo, err := os.Lstat(dst)
		switch {
		case err == nil:
			srcInfo, srcErr := os.Lstat(src)
			if srcErr != nil {
				r.reportPasteOutcome(job, func() { r.recordPasteError(job, srcErr) })
				continue
			}
			conflict := pasteConflict{src: src, dst: dst, srcInfo: srcInfo, dstInfo: dstInfo}
			r.reportPasteOutcome(job, func() { r.pasteConflictFound(job, conflict) })
		case os.IsNotExist(err):
			r.safeGo("paste", func() { r.pasteItemDone(job) }, func() {
				r.pasteOne(job, src, dst, false)
			})
		default:
			r.reportPasteOutcome(job, func() { r.recordPasteError(job, err) })
		}
	}
}

// reportPasteOutcome runs fn on the UI thread via QueueUpdateDraw, from
// its own freshly-spawned goroutine rather than inline — see
// pasteWalk's own doc comment on why calling QueueUpdateDraw directly
// from that loop would stall it. Used for the two paths that need to
// reach the UI thread without also going through pasteOne's own I/O
// first: recording a stat failure, and handing a freshly-found conflict
// to pasteConflictFound.
func (r *Root) reportPasteOutcome(job *pasteJob, fn func()) {
	r.safeGo("paste", func() { r.pasteItemDone(job) }, func() {
		r.app.QueueUpdateDraw(fn)
	})
}

// pasteOne does one item's real Copy/Move — called from its own
// freshly-spawned goroutine either way, whether pasteWalk found no
// conflict at all or a conflict resolved to something other than "skip"
// (see resolveConflictAsync) — always off the UI thread, reporting back
// through QueueUpdateDraw once it's done either way.
//
// Checks job.ctx.Err() before touching the filesystem at all: a
// conflict resolved just as (or after) the job itself was cancelled —
// superseded by a newer Paste — shouldn't still write anything.
func (r *Root) pasteOne(job *pasteJob, src, dst string, force bool) {
	if job.ctx.Err() != nil {
		return
	}
	var err error
	if job.cut {
		err = fsMove(src, dst, force)
	} else {
		err = fsCopy(src, dst, force)
	}
	r.app.QueueUpdateDraw(func() { r.applyPasteOneResult(job, src, dst, err) })
}

// applyPasteOneResult is pasteOne's own completion handling, split out
// on its own so a test can call it directly — bypassing the
// QueueUpdateDraw hop pasteOne itself needs in production (to stay
// thread-safe: it runs after real file I/O on a background goroutine)
// but which never fires in a test lacking a running Application.Run()
// loop, the same reason detailssidebar.go's own computeDetailsDirSize
// leaves its result-handling untestable through the goroutine and pins
// it directly instead (see TestComputeDetailsDirSizeStoresResult).
func (r *Root) applyPasteOneResult(job *pasteJob, src, dst string, err error) {
	if job.ctx.Err() != nil {
		return
	}
	if err != nil {
		r.recordPasteError(job, err)
		return
	}
	if job.cut {
		// Only for a successful Move, not Copy: src is untouched by a
		// copy (still exactly what Details would already be showing, if
		// anything), and dst is a brand new path a copy could never
		// already have been the target of. A move, like a rename,
		// genuinely relocates the same real entry — Details needs to
		// keep following it under its new path.
		r.refreshDetailsIfShowing(src, dst)
	}
	r.pasteItemDone(job)
}

// recordPasteError records a genuine failure (not a conflict — those
// are never errors, only ever resolved one way or another) and marks
// this item's outcome as final.
func (r *Root) recordPasteError(job *pasteJob, err error) {
	job.errors = append(job.errors, err)
	r.pasteItemDone(job)
}

// pasteItemDone marks one clipboard item's outcome as final (copied/
// moved, skipped, or errored) — see pasteJob.remaining's own doc
// comment — and finishes the whole job once every item has one and no
// conflict is still pending or on screen.
func (r *Root) pasteItemDone(job *pasteJob) {
	job.remaining--
	if job.remaining <= 0 && job.current == nil && len(job.pending) == 0 {
		r.finishPasteJob(job)
	}
}

// finishPasteJob runs once every clipboard item has a final outcome and
// no conflict is left pending or on screen — reports every genuine
// failure collected along the way, not just the first the way the old
// synchronous pasteInto did (per the user's own explicit request — see
// feature_ideas.txt's own note on this being step one, before any real
// notification channel exists to send them to instead), reloads every
// open tab showing the destination (per the user's own explicit
// request for an auto-reload there), and clears the clipboard once a
// clean (no errors) Cut has fully landed.
func (r *Root) finishPasteJob(job *pasteJob) {
	if r.pasteJob != job {
		return // already superseded/cancelled — see cancelPasteJob
	}
	r.pasteJob = nil

	if job.cut && len(job.errors) == 0 {
		// Moved away cleanly; nothing left to paste again — goes through
		// setClipboard (not a bare "r.clipboard = nil"), the same as
		// Copy/Cut themselves, so every open tab's own row highlighting
		// and the status bar's own indicator clear along with it instead
		// of drifting stale.
		r.setClipboard(nil, false)
	}

	// Every open tab currently showing destDir, not just r.panel — the
	// same directory can be open in more than one tab (see
	// syncClipboardHighlight's own doc comment for the same "one
	// Root-level event, every matching tab needs to know" reasoning).
	// A tab showing something else — a search result's own directory,
	// elsewhere, or a different path entirely — is left exactly as it
	// was; pasting shouldn't force-navigate or otherwise disturb it
	// (see pasteClipboard's own doc comment).
	r.forEachTab(func(p *Panel) {
		if p.path != job.destDir {
			return
		}
		if err := p.load(p.path); err != nil {
			job.errors = append(job.errors, err)
		}
	})

	if len(job.errors) > 0 {
		r.showError(pasteSummaryError(job))
	}
}

// pasteSummaryError collects every genuine failure a job ran into into
// one message — a single error reports as itself, unchanged; more than
// one reports as "N of M items failed:" followed by each on its own
// line, the same "don't stack one overlay per failure" shape
// batchrename's/sedreplace's own multi-file summaries already use.
func pasteSummaryError(job *pasteJob) error {
	if len(job.errors) == 1 {
		return job.errors[0]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d items failed:", len(job.errors), job.total)
	for _, err := range job.errors {
		fmt.Fprintf(&b, "\n  %s", err)
	}
	return fmt.Errorf("%s", b.String())
}

// pasteConflictFound is pasteWalk's own handoff to the UI thread for one
// discovered conflict — the one place a job's shared state
// (forAll/current/pending) is ever read or written, so it's the only
// thing that needs to be safe against pasteWalk's own concurrent
// progress through the rest of the list: this always runs via
// QueueUpdateDraw, serialized onto the same goroutine as every dialog
// button's own callback, so nothing here needs a mutex of its own.
func (r *Root) pasteConflictFound(job *pasteJob, c pasteConflict) {
	if job.ctx.Err() != nil {
		return
	}
	if job.forAll != nil {
		r.resolveConflictAsync(job, c, *job.forAll)
		return
	}
	if job.current != nil {
		job.pending = append(job.pending, c)
		r.refreshPasteConflictDialog(job)
		return
	}
	r.showPasteConflictDialog(job, c)
}

// resolveConflictAsync applies resolution to c. The two conditional
// rules (resolveOverwriteIfNewer/resolveOverwriteIfSourceNotEmpty) are
// decided right here, synchronously, from the stat info already
// gathered when the conflict was first found — no further disk access
// needed to know the answer. Whatever the outcome, the real work (if
// any) always happens in its own freshly-spawned goroutine, never on
// this one: this runs on the UI thread (a dialog button's own callback,
// or pasteConflictFound above finding forAll already set), which must
// never block on file I/O itself.
func (r *Root) resolveConflictAsync(job *pasteJob, c pasteConflict, resolution conflictResolution) {
	switch resolution {
	case resolveSkip:
		r.pasteItemDone(job)
		return
	case resolveOverwriteIfNewer:
		if !c.srcInfo.ModTime().After(c.dstInfo.ModTime()) {
			r.pasteItemDone(job)
			return
		}
	case resolveOverwriteIfSourceNotEmpty:
		if c.srcInfo.Size() == 0 {
			r.pasteItemDone(job)
			return
		}
	}
	r.safeGo("paste (conflict)", func() { r.pasteItemDone(job) }, func() {
		r.pasteOne(job, c.src, c.dst, true)
	})
}

// showPasteConflictDialog shows c (already resolved to be the one to
// ask about) in r.pasteConflictDialog — the one dialog every paste
// conflict shares, built once in NewRoot the same way newConfirmDialog
// is.
func (r *Root) showPasteConflictDialog(job *pasteJob, c pasteConflict) {
	job.current = &c
	r.renderPasteConflictDialog(job)
	r.resizePasteConflictDialog()
	r.pasteConflictDialog.SetCurrentItem(pasteConflictSkipItem) // "Skip" preselected — the safe default, the same "Cancel preselected" reasoning every other irreversible-leaning choice in this app already follows
	r.showOverlay(pasteConflictPage, r.pasteConflictDialog)
}

// refreshPasteConflictDialog re-renders the already-open dialog's own
// message — called when a further conflict queues up behind the one
// currently shown, so its own "(N more waiting)" count stays live
// rather than only being accurate the moment the dialog first opened.
// Also re-sizes it (see resizePasteConflictDialog): unlike this app's
// other dialogs, this one's own message can grow while it's already on
// screen, and its box was only ever sized for whatever the message
// said at the moment it first opened — without this, a longer "(N more
// waiting)" suffix gets silently clipped by the unchanged-width List
// box instead of ever becoming visible, no matter how long it waits.
func (r *Root) refreshPasteConflictDialog(job *pasteJob) {
	if job.current != nil {
		r.renderPasteConflictDialog(job)
		r.resizePasteConflictDialog()
	}
}

// resizePasteConflictDialog sizes and re-centers r.pasteConflictDialog
// for whatever its message currently says — shared by
// showPasteConflictDialog (first open) and refreshPasteConflictDialog
// (message changed while already open) rather than duplicated, since
// both need the exact same listSize/centeredOnScreen/SetRect sequence.
func (r *Root) resizePasteConflictDialog() {
	width, height := listSize(r.pasteConflictDialog)
	x, y := r.centeredOnScreen(width, height)
	r.pasteConflictDialog.SetRect(x, y, width, height)
}

// pasteConflictSkipItem/pasteConflictOverwriteItem are r.pasteConflictDialog's
// own item indices, named rather than left as bare numbers since
// SetCurrentItem/SetItemText both need them and a reordering of
// newPasteConflictDialog's own AddItem calls would otherwise have to be
// mirrored by hand everywhere they're used.
const (
	pasteConflictMessageItem = iota
	pasteConflictOverwriteItem
	pasteConflictOverwriteAllItem
	pasteConflictSkipItem
	pasteConflictSkipAllItem
	pasteConflictIfNewerItem
	pasteConflictIfNotEmptyItem
)

// renderPasteConflictDialog fills in job.current's own message — the
// only part of the dialog that ever changes between one conflict and
// the next; every button's own label is fixed, set once in
// newPasteConflictDialog.
func (r *Root) renderPasteConflictDialog(job *pasteJob) {
	c := *job.current
	msg := fmt.Sprintf("%q already exists in this folder.", filepath.Base(c.dst))
	if n := len(job.pending); n > 0 {
		msg += fmt.Sprintf(" (%d more waiting)", n)
	}
	r.pasteConflictDialog.SetItemText(pasteConflictMessageItem, msg, "")
}

// newPasteConflictDialog builds r.pasteConflictDialog once, called from
// NewRoot the same way newConfirmDialog is — a List rather than a
// Modal, the same reasoning every other dialog in this app already
// follows (keyboard navigation, a clickable legend, consistent with
// everything else here).
func (r *Root) newPasteConflictDialog() *tview.List {
	l := tview.NewList().ShowSecondaryText(false)
	l.SetHighlightFullLine(true)
	l.SetBorderPadding(0, 0, 1, 1)
	l.AddItem("", "", 0, nil) // pasteConflictMessageItem — set fresh by renderPasteConflictDialog before every show
	l.AddItem("Overwrite", "", 0, func() { r.chooseConflictResolution(resolveOverwrite, false) })
	l.AddItem("Overwrite all", "", 0, func() { r.chooseConflictResolution(resolveOverwrite, true) })
	l.AddItem("Skip", "", 0, func() { r.chooseConflictResolution(resolveSkip, false) })
	l.AddItem("Skip all", "", 0, func() { r.chooseConflictResolution(resolveSkip, true) })
	l.AddItem("Overwrite all if source is newer", "", 0, func() { r.chooseConflictResolution(resolveOverwriteIfNewer, true) })
	l.AddItem("Overwrite all if source is not empty", "", 0, func() { r.chooseConflictResolution(resolveOverwriteIfSourceNotEmpty, true) })
	l.SetDoneFunc(func() { r.chooseConflictResolution(resolveSkip, false) }) // Escape - same safe default as the preselected item
	return l
}

// chooseConflictResolution is every r.pasteConflictDialog button's own
// action — resolves whichever conflict is currently shown, remembers
// the choice for everything still pending/to come if forAll is true,
// closes the dialog, and moves on to whatever's next (see
// advancePasteConflicts).
func (r *Root) chooseConflictResolution(resolution conflictResolution, forAll bool) {
	job := r.pasteJob
	if job == nil || job.current == nil {
		return
	}
	c := *job.current
	job.current = nil
	if forAll {
		res := resolution
		job.forAll = &res
	}
	r.hideOverlay()
	r.resolveConflictAsync(job, c, resolution)
	r.advancePasteConflicts(job)
}

// advancePasteConflicts runs right after one conflict is answered —
// shows the next queued one, if any, unless a "for all" policy was just
// set, in which case every one of them (and anything pasteWalk still
// finds afterward — see pasteConflictFound) resolves against it
// immediately instead, with no further dialog at all.
func (r *Root) advancePasteConflicts(job *pasteJob) {
	pending := job.pending
	job.pending = nil
	for _, c := range pending {
		switch {
		case job.forAll != nil:
			r.resolveConflictAsync(job, c, *job.forAll)
		case job.current == nil:
			r.showPasteConflictDialog(job, c)
		default:
			job.pending = append(job.pending, c)
		}
	}
	// The loop above can both show a new dialog (with job.pending still 0
	// at that moment) and then, for anything after it, re-queue straight
	// back into job.pending — leaving the dialog just shown with a stale
	// "(N more waiting)" count (or none at all) unless refreshed again
	// now that job.pending has its final count for this round. Same
	// no-op-when-nothing-is-open guard as everywhere else this is called.
	r.refreshPasteConflictDialog(job)
	if job.current == nil && len(job.pending) == 0 && job.remaining <= 0 {
		r.finishPasteJob(job)
	}
}
