// compressjob.go is Compress/Extract's own background execution engine
// — the same "run the real external tool as a detached child, come back
// to the UI only once it's done" shape rsyncjob.go's own "Run in
// background" already established, but simpler: none of the archive
// tools this app shells out to (zip/tar/gzip/bzip2/xz/zstd) offer a
// standard, parseable live-progress line the way rsync's own
// --info=progress2 does, so there is no pty, no percentage, and no
// per-line parsing here — just a spinner and an elapsed time in the
// status bar (see compressProgressText in bottombar.go) while the
// command runs. The whole app stays fully usable throughout: no
// suspended terminal, no "press Esc to return" screen, per the user's
// own explicit request that Compress/Extract behave exactly like Copy/
// Paste already do, rather than borrowing Rsync's own foreground "Run"
// path (see runShellCommandFullScreen).
//
// No pty and no Setsid, unlike reallyStartRsyncBackground: both exist
// there solely to give rsync's own stdout a terminal to keep it
// line-buffered, and Setsid solely to keep that pty from fighting
// breakthrough's own controlling terminal over job control (see its own
// doc comment for the SIGTTOU story in full). Neither applies here —
// stdout/stderr both go to a plain in-memory buffer, so the child never
// has a file descriptor referring to any real terminal at all, and
// there is nothing for it to contend with.
//
// Deliberately still just one Compress/Extract job at a time, queued
// the same way a second background rsync already queues behind a
// running one (see rsyncjob.go) — running two archive tools at once has
// no obviously right behavior if they happen to conflict, and a single
// job is simple to reason about end to end.
package ui

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// compressJob is one backgrounded Compress or Extract run's own async
// state — see this file's own package doc comment for the whole shape.
type compressJob struct {
	ctx    context.Context
	cancel context.CancelFunc
	cmd    *exec.Cmd

	startedAt time.Time
	verb      string // "Compressing"/"Extracting" — the status bar's own leading word (see compressProgressText)
	label     string // the archive's own base name, either direction — the status bar's own trailing text

	// errContext prefixes a genuine failure the same way runCompress/
	// extractCurrentArchive's own synchronous error wrapping used to,
	// before either ran in the background — "compress", or "extract
	// <archive base name>" (see extractCurrentArchive), since Extract's
	// own error already named which archive, and there is no other,
	// later call site left to add that once this job's result only ever
	// surfaces from inside finishCompressJob.
	errContext string

	// destDir is reloaded (every open tab whose own path matches it —
	// see finishCompressJob) once the job finishes successfully — the
	// same "match by path, not by whichever tab happened to be active
	// when this started" reasoning finishRsyncJob's own destPath field
	// already follows, needed here for exactly the same reason: the
	// user is free to switch tabs while this runs in the background.
	destDir string

	// deleteOriginal, once set, is the archive path deleteExtractedArchive
	// should remove once extraction has actually finished successfully —
	// "" for Compress, and for a plain Extract with no delete requested.
	deleteOriginal string

	// animFrame advances once per animateCompressProgress tick — see
	// pasteJob.animFrame's own doc comment for why this is a plain int
	// rather than an atomic: only ever touched from inside an
	// app.QueueUpdateDraw closure, which always runs on tview's own UI
	// goroutine, never concurrently with itself.
	animFrame int
}

// compressRequest is everything one Compress/Extract run needs — both
// startCompressJob's own argument and, unchanged, what sits in
// r.compressQueue while a further request waits behind an already-
// running job (see queuedRsync's own identical dual role in rsyncjob.go).
type compressRequest struct {
	command        string
	errContext     string
	verb           string
	label          string
	destDir        string
	deleteOriginal string // "" unless this is an Extract with "delete original" requested
}

// startCompressJob is runCompress/extractCurrentArchive's own shared
// entry point: queues behind an already-running Compress/Extract job the
// same way startRsyncBackground already queues behind a running rsync,
// or starts req immediately otherwise.
func (r *Root) startCompressJob(req compressRequest) {
	if r.compressJob != nil {
		r.compressQueue = append(r.compressQueue, req)
		r.refreshStatusBar()
		return
	}
	r.reallyStartCompressJob(req)
}

// reallyStartCompressJob is startCompressJob's own "actually begin"
// body — split out the same way reallyStartRsyncBackground is, so
// advanceCompressQueue can start the next queued run through exactly
// the same path.
func (r *Root) reallyStartCompressJob(req compressRequest) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, userShell(), fullScreenShellArgs(req.command)...)
	// Only ever a real local directory — Compress/Extract have no
	// remote path yet either way (see openCompress/extractCurrentArchive's
	// own remote guard), but this mirrors runShellCommandFullScreen's
	// own local-only cmd.Dir regardless.
	if r.panel.remote == nil {
		cmd.Dir = r.panel.path
	}

	// A plain buffer, not a pty: nothing here needs to watch this
	// output live (see this file's own package doc comment) — only
	// enough of it to explain a real failure, the same "rsync's own
	// stderr" reasoning stderrTail already exists for.
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	job := &compressJob{
		ctx: ctx, cancel: cancel, cmd: cmd,
		startedAt: time.Now(), verb: req.verb, label: req.label,
		errContext: req.errContext, destDir: req.destDir, deleteOriginal: req.deleteOriginal,
	}
	r.compressJob = job

	if err := cmd.Start(); err != nil {
		cancel()
		r.showError(fmt.Errorf("%s: %w", req.errContext, err))
		r.compressJob = nil
		return
	}

	r.safeGo(req.errContext+" job", func() { r.finishCompressJob(job, nil) }, func() {
		waitErr := cmd.Wait()
		var finishErr error
		if waitErr != nil {
			finishErr = fmt.Errorf("%s: %w%s", job.errContext, waitErr, stderrTail(output.String()))
		}
		r.app.QueueUpdateDraw(func() { r.finishCompressJob(job, finishErr) })
	})
	r.safeGo(req.errContext+" animation", func() { r.finishCompressJob(job, nil) }, func() {
		r.animateCompressProgress(job)
	})

	r.refreshStatusBar()
}

// finishCompressJob applies a backgrounded Compress/Extract's own
// outcome — the same shape finishRsyncJob already has: reports a
// genuine failure (nil for a clean exit or a deliberate cancel — see
// cancelCompressJob), reloads whichever open tab shows destDir, runs
// deleteOriginal's own Trash step once the job actually succeeded (see
// deleteExtractedArchive — never for a failed or cancelled job, which
// left nothing safe to delete), and starts the next queued run, if any.
func (r *Root) finishCompressJob(job *compressJob, err error) {
	if r.compressJob != job {
		return // already superseded/cancelled
	}
	// Read before job.cancel() below, which would otherwise make this
	// check always true — see finishRsyncJob's own identical ordering.
	wasCancelled := job.ctx.Err() != nil
	r.compressJob = nil
	job.cancel()

	switch {
	case err != nil && !wasCancelled:
		r.showError(err)
	case !wasCancelled:
		r.forEachTab(func(p *Panel) {
			if p.path == job.destDir {
				r.showError(p.load(p.path))
			}
		})
		if job.deleteOriginal != "" {
			r.deleteExtractedArchive(job.deleteOriginal)
		}
	}
	r.refreshStatusBar()
	r.advanceCompressQueue()
}

// cancelCompressJob stops a running backgrounded Compress/Extract, if
// any — Ctrl+C's own third target alongside cancelPasteJob/
// cancelRsyncJob (see RequestCancel).
func (r *Root) cancelCompressJob() {
	if r.compressJob == nil {
		return
	}
	r.compressJob.cancel()
	r.compressQueue = nil
}

// advanceCompressQueue starts the next queued Compress/Extract, if any
// — the same shape advanceRsyncQueue already has.
func (r *Root) advanceCompressQueue() {
	if len(r.compressQueue) == 0 {
		return
	}
	next := r.compressQueue[0]
	r.compressQueue = r.compressQueue[1:]
	r.reallyStartCompressJob(next)
}

// animateCompressProgress redraws the status bar on a fixed tick for as
// long as job is still running — the same shape animateRsyncProgress
// already has, except this also advances job.animFrame itself: rsync's
// own progress line supplies a discrete update signal of its own (see
// rsyncProgressText's own doc comment), so it needs no spinner; nothing
// here has an equivalent, so the spinner is this segment's only "still
// alive" cue.
func (r *Root) animateCompressProgress(job *compressJob) {
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
