package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
// otherwise land on. Only resolveOverwrite/resolveMerge/resolveSkip
// make sense as a one-off, single-conflict answer; the other two are
// conditional rules, meant to be picked once and then applied to every
// conflict a paste runs into, not asked about individually (see
// chooseConflictResolution's own "forAll" parameter).
//
// resolveOverwrite and resolveMerge differ only for a directory
// conflict — see fsops.OverwriteMode's own doc comment for the two
// choices in full; for a plain file conflict, fsops.Copy/Move ignore
// the distinction entirely, and either resolves the same way. Both
// still get their own dialog options (see newPasteConflictDialog)
// rather than folding one into the other, or hiding whichever doesn't
// apply: knowing in advance whether a given conflict is a file or a
// directory would need this dialog to rebuild its own item list per
// conflict, and the label alone ("... into existing folder") already
// says plainly enough when Merge's own choice actually matters.
type conflictResolution int

const (
	resolveOverwrite conflictResolution = iota
	resolveMerge
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

	// startedAt is when this job was created (see startPaste) — never
	// touched again. animatePasteProgress's own ticker uses it purely
	// to average an ETA over the whole run so far (see pasteETA), not
	// for anything correctness-sensitive.
	startedAt time.Time

	total int // clipboard items this job started with — never decremented, just for "N of M" reporting

	// remaining is decremented once for every clipboard item whose final
	// outcome (copied/moved, skipped, or errored) has been recorded —
	// see pasteItemDone. The job is finished once this reaches 0 *and*
	// no conflict is still pending or on screen (current/pending below).
	remaining int

	// ioMu serializes every pasteOne call's own real fsCopy/fsMove work
	// across however many goroutines this job spawns (still one per
	// item — see pasteWalk/resolveConflictAsync — not a bounded worker
	// pool): only one file actually copies/moves at a time, held for the
	// I/O itself only, released again before that same call's own
	// QueueUpdateDraw hand-off (which always blocks its caller until an
	// Application.Run() loop actually services it — verified directly
	// against tview's own application.go, not assumed — so holding this
	// through it would let one item's stuck hand-off (there's no such
	// loop in most of this package's own tests) starve every other
	// item's turn at the lock forever). This is what makes "which file
	// is this Paste working on right now" (see currentFile) a single,
	// well-defined answer instead of however many concurrent copies used
	// to race over it, and bounds a very large paste to one file's worth
	// of actual disk I/O in flight at a time — accepting, as a
	// deliberate trade-off, that a huge paste still starts one goroutine
	// per item up front, all but one of them just waiting on this lock;
	// a real bounded worker pool would be the next step if that count
	// ever became the actual bottleneck rather than the disk itself.
	ioMu sync.Mutex

	// currentFile is pasteOne's own "on it right now" — the absolute
	// path fsCopy/fsMove most recently reported via onFile, read by
	// animatePasteProgress's own ticker to render the status bar's
	// progress segment (see pasteProgressText). A plain atomic, not
	// funneled through QueueUpdateDraw: written directly from whichever
	// goroutine currently holds ioMu, the same "cheap, synchronous,
	// sampled on the reader's own schedule" contract Properties' own
	// hashBytesRead already follows, for the same reason (see
	// fsops.Hash's onProgress doc comment).
	currentFile atomic.Pointer[string]

	// bytesTotal is the sum of every real file's size across the whole
	// job's own clipboard selection (see fsops.TotalBytes — a symlink
	// contributes 0, a path this can't read contributes 0 rather than
	// failing the scan outright) — computed once, in its own
	// background goroutine started alongside pasteWalk itself (see
	// scanPasteBytes), rather than blocking Paste on it: a very large
	// selection's own scan could otherwise noticeably delay the very
	// first file actually starting to copy, exactly the kind of
	// upfront cost byte-accurate progress has always cost (see
	// pasteProgressText's own doc comment on this same trade-off before
	// this existed at all). 0 both before the scan finishes and for a
	// genuinely empty-of-bytes selection (all zero-byte files, say) —
	// deliberately not distinguished from each other with a separate
	// "scan done" flag, since every reader already treats "nothing to
	// show yet" and "nothing to show, full stop" exactly the same way:
	// skip the byte-based parts of the display, item-count progress and
	// the current file name keep working regardless either way.
	bytesTotal atomic.Int64

	// bytesBase, currentFileSize, and currentFileBytes together let
	// animatePasteProgress's own ticker compute a live "bytes done so
	// far, job-wide" total (see pasteProgressText) without pasteOne
	// ever having to report one directly itself. onFile (see pasteOne)
	// updates all three every time it reports a new path: the file that
	// was current a moment ago, if any, is guaranteed to be fully done
	// by then — only one file's content is ever being streamed at a
	// time across the whole job (see ioMu's own doc comment) — so its
	// own size folds into bytesBase right there, and
	// currentFileSize/currentFileBytes reset for the one now starting.
	// onBytes then only ever has to update currentFileBytes as that
	// one's own copy actually progresses. All three are plain atomics
	// for the same "single writer (whichever goroutine currently holds
	// ioMu), sampled reader (this ticker, on a different goroutine)"
	// reason currentFile above already is one.
	bytesBase        atomic.Int64
	currentFileSize  atomic.Int64
	currentFileBytes atomic.Int64

	// animFrame advances once per animatePasteProgress tick — purely
	// decorative (see hashAnimationFrames, reused here for the same
	// visual language), a "still alive" cue for a single very large file
	// where currentFile/remaining might otherwise sit unchanged a while.
	animFrame int

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
	job := &pasteJob{ctx: ctx, cancel: cancel, cut: cut, destDir: destDir, total: len(items), remaining: len(items), startedAt: time.Now()}
	r.pasteJob = job

	onPanic := func() { r.pasteJob = nil }
	r.safeGo("paste", onPanic, func() { r.pasteWalk(job, items) })
	r.safeGo("paste progress animation", onPanic, func() { r.animatePasteProgress(job) })
	r.safeGo("paste byte scan", onPanic, func() { r.scanPasteBytes(job, items) })
	r.refreshStatusBar() // show the progress segment immediately, not just once the first tick lands
}

// scanPasteBytes runs once per job, in its own goroutine started
// alongside pasteWalk itself (see startPaste) rather than before it:
// walking the whole selection to size it exactly is the one real cost
// of showing byte-accurate progress at all (see
// pasteJob.bytesTotal's own doc comment), and paying it up front,
// blocking, would delay the very first file actually starting to copy
// for however long a very large selection takes to size — this way the
// two run fully concurrently, and the byte-based parts of the display
// simply switch on once this finishes, whenever that happens to be,
// with the item-count progress and current file name working from the
// very first tick regardless.
func (r *Root) scanPasteBytes(job *pasteJob, items []string) {
	total := fsops.TotalBytes(job.ctx, items)
	if job.ctx.Err() != nil {
		return // cancelled or superseded before the scan finished — nothing left to report this to
	}
	job.bytesTotal.Store(total)
	r.app.QueueUpdateDraw(func() {
		if job.ctx.Err() != nil {
			return
		}
		r.refreshStatusBar()
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
// could itself block waiting on the UI thread. The real disk I/O each
// of those goroutines eventually does is still serialized to one at a
// time (see pasteJob.ioMu's own doc comment) — this loop just starts
// them all without waiting for that turn.
//
// A real, accepted trade-off from spawning one goroutine per item
// rather than working through them one at a time: a very large paste
// (many hundreds of files at once) starts that many goroutines
// concurrently — mostly just waiting on ioMu, not actually touching the
// disk at once, but still that many live goroutines — instead of
// bounding how many exist at all. Fine for the sizes this app's own
// target use actually pastes at once; a real bounded worker pool would
// be the next step if that ever stopped being true.
func (r *Root) pasteWalk(job *pasteJob, items []string) {
	for _, src := range items {
		if job.ctx.Err() != nil {
			return // superseded by a newer paste, or cancelled outright — see cancelPasteJob
		}
		src := src
		dst := filepath.Join(job.destDir, filepath.Base(src))

		// Checked before anything else, including the conflict scan just
		// below: pasting a file back into the very directory it's already
		// in, or a directory into one of its own subdirectories, must
		// never even reach fsCopy/fsMove, let alone a conflict dialog that
		// would offer "Overwrite" as if this were an ordinary collision —
		// choosing it here would destroy the only copy there ever was (a
		// real bug found and fixed at fsops.Copy/Move's own level too, see
		// fsops.Overlaps' own doc comment for the full reasoning); this is
		// what makes the item never start at all instead, reported as a
		// genuine error like any other real failure.
		if fsops.Overlaps(src, dst) {
			r.reportPasteOutcome(job, func() {
				r.recordPasteError(job, fmt.Errorf("%s: source and destination are the same, or one is inside the other", filepath.Base(src)))
			})
			continue
		}

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
				// force is false — no conflict here at all — so mode is
				// never actually consulted; ReplaceEntirely is just the
				// harmless, arbitrary placeholder for that.
				r.pasteOne(job, src, dst, false, fsops.ReplaceEntirely)
			})
		default:
			r.reportPasteOutcome(job, func() { r.recordPasteError(job, err) })
		}
	}
}

// animatePasteProgress advances job.animFrame and redraws the status
// bar every hashAnimationInterval until job.ctx is done — mirrors
// Properties' own animateHashProgress (same interval, same idea: keep a
// "something is happening" cue moving smoothly regardless of how bursty
// the actual I/O is, decoupled from it on its own goroutine), reusing
// hashAnimationFrames' own glyph set for visual consistency with every
// other "in progress" indicator this app already shows.
func (r *Root) animatePasteProgress(job *pasteJob) {
	ticker := time.NewTicker(hashAnimationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if job.ctx.Err() != nil {
				return
			}
			r.app.QueueUpdateDraw(func() {
				if job.ctx.Err() != nil {
					return
				}
				job.animFrame++
				r.refreshStatusBar()
			})
		case <-job.ctx.Done():
			return
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
//
// job.ioMu (see its own doc comment) is held for the real fsCopy/fsMove
// call only — acquired right before, released again right after,
// deliberately before the QueueUpdateDraw hand-off below, which always
// blocks its own caller until serviced and must never hold up whichever
// other goroutine is waiting its own turn at the lock. onFile stores
// each real file/symlink fsCopy/fsMove is about to touch into
// job.currentFile as it goes (see its own doc comment) — a plain atomic
// write, not routed through QueueUpdateDraw itself: animatePasteProgress
// samples it on its own schedule, the same "no rate-limiting done at
// the source" contract fsops.Hash's own onProgress already follows.
// onFile also folds the *previous* file's own size into job.bytesBase
// and resets currentFileSize/currentFileBytes for the one now starting
// (see their own doc comment for why that inference is safe here); the
// os.Lstat it does for the new file's size is a small, synchronous cost
// paid once per file, on the same goroutine already about to do that
// file's own real I/O, not on the UI thread. onBytes then only updates
// currentFileBytes as that copy actually streams.
//
// mode is fsops.OverwriteMode — meaningless (see its own doc comment)
// unless force is true and the conflict turns out to be a directory,
// which is exactly why pasteWalk's own non-conflict call site can pass
// either value without it mattering at all: force is false there, so
// mode is never even consulted.
func (r *Root) pasteOne(job *pasteJob, src, dst string, force bool, mode fsops.OverwriteMode) {
	if job.ctx.Err() != nil {
		return
	}
	onFile := func(path string) {
		job.currentFile.Store(&path)
		job.bytesBase.Add(job.currentFileSize.Load())
		var size int64
		if fi, err := os.Lstat(path); err == nil {
			size = fi.Size()
		}
		job.currentFileSize.Store(size)
		job.currentFileBytes.Store(0)
	}
	onBytes := func(copiedBytes int64) { job.currentFileBytes.Store(copiedBytes) }
	job.ioMu.Lock()
	var err error
	if job.cut {
		err = fsMove(src, dst, fsops.MoveOptions{Force: force, Mode: mode, OnFile: onFile, OnBytes: onBytes})
	} else {
		err = fsCopy(src, dst, fsops.CopyOptions{Force: force, Mode: mode, OnFile: onFile, OnBytes: onBytes})
	}
	job.ioMu.Unlock()
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
	// Stops pasteWorker/animatePasteProgress (both select on job.ctx.Done()
	// — see their own doc comments): there's nothing left in job.work by
	// this point (every item already has a final outcome, or this
	// wouldn't have run at all — see pasteItemDone), so both goroutines
	// would otherwise sit blocked forever, doing nothing, until the
	// process exits.
	job.cancel()

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
// never block on file I/O itself. pasteOne itself still serializes the
// actual fsCopy/fsMove call against every other item's own goroutine
// (see job.ioMu's own doc comment) — this just starts it without
// waiting for that turn, the same as pasteWalk's own non-conflicting
// items already do.
func (r *Root) resolveConflictAsync(job *pasteJob, c pasteConflict, resolution conflictResolution) {
	mode := fsops.ReplaceEntirely
	switch resolution {
	case resolveSkip:
		r.pasteItemDone(job)
		return
	case resolveMerge:
		mode = fsops.MergeInto
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
		r.pasteOne(job, c.src, c.dst, true, mode)
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
	pasteConflictMergeItem
	pasteConflictMergeAllItem
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
//
// "Overwrite"/"Overwrite all" mean fsops.ReplaceEntirely (see its own
// doc comment) — for a directory conflict, the destination ends up
// *exactly* the source's own tree afterward, nothing left over from
// whatever was already there; "Merge into existing folder"/"Merge all
// into existing folders" are the explicit alternative, keeping
// whatever the source doesn't also have. Both pairs are always shown,
// even though the distinction is only real for a directory conflict —
// see conflictResolution's own doc comment on why this doesn't rebuild
// the item list per conflict to hide whichever doesn't apply.
func (r *Root) newPasteConflictDialog() *tview.List {
	l := tview.NewList().ShowSecondaryText(false)
	l.SetHighlightFullLine(true)
	l.SetBorderPadding(0, 0, 1, 1)
	l.AddItem("", "", 0, nil) // pasteConflictMessageItem — set fresh by renderPasteConflictDialog before every show
	l.AddItem("Overwrite", "", 0, func() { r.chooseConflictResolution(resolveOverwrite, false) })
	l.AddItem("Overwrite all", "", 0, func() { r.chooseConflictResolution(resolveOverwrite, true) })
	l.AddItem("Merge into existing folder", "", 0, func() { r.chooseConflictResolution(resolveMerge, false) })
	l.AddItem("Merge all into existing folders", "", 0, func() { r.chooseConflictResolution(resolveMerge, true) })
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
