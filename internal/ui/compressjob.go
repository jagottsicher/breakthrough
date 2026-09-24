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
// No pty, unlike reallyStartRsyncBackground: stdout/stderr both go to a
// plain in-memory buffer here, not a terminal rsync's own live progress
// needs to stay line-buffered.
//
// Setsid is still needed regardless, and for exactly the same reason
// reallyStartRsyncBackground's own doc comment already gives in full: a
// real, reported bug (the user's own "das Programm scheint einfach
// ausgestiegen zu sein... bei Mausbewegung nur noch Zeichencodes
// angezeigt", and, independently, "ich glaube breakthrough wird
// irgendwie gestoppt und als job in den Hintergrund verschoben" —
// exactly what happened). fullScreenShellArgs' own "-i" makes the child
// an *interactive* shell regardless of what its stdout/stderr are
// redirected to — job-control setup (tcsetpgrp and friends) happens
// against /dev/tty, opened directly, never against fd 1/2 — so a plain
// buffer in place of rsyncjob.go's own pty was never the part that
// mattered. Without Setsid, that child still inherits breakthrough's
// own session and controlling terminal, tries to become its own
// foreground process group there, and the kernel's answer is SIGTTOU
// sent to the whole process group — including breakthrough itself,
// which promptly stops (bash's own job control then reports it as
// "[1]+ Stopped", exactly the terminal-visible symptom reported: the
// whole app appears to freeze/exit, dropping back to a shell prompt
// behind it, with mouse reporting left enabled and never disabled since
// breakthrough's own shutdown path never ran). Setsid gives the child
// its own new session with no controlling terminal to fight over in
// the first place — see reallyStartRsyncBackground's own doc comment
// for the rest of this exact mechanism.
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
	"strings"
	"syscall"
	"time"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
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

	// onSuccess, once set, runs on the main goroutine right after a
	// successful finish (after destDir's own reload and deleteOriginal,
	// if any) — how a local compress/extract stage chains into a
	// following upload stage once a remote destination is involved (see
	// runCompressToRemote/extractCurrentArchiveToRemote in compress.go),
	// each still its own separate compressJob/status-bar entry rather
	// than one job silently doing two unrelated things.
	onSuccess func(r *Root)

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
	// command is the real shell command line to run for a subprocess-
	// backed stage (the actual zip/tar/... invocation) — exactly one of
	// command/goFunc is ever set. goFunc instead runs a plain Go
	// function on this job's own background goroutine, no subprocess at
	// all: how a download-from-remote or upload-to-remote stage (see
	// runCompressToRemote/extractCurrentArchiveToRemote) shares this
	// same job/status-bar/queue machinery despite having nothing to
	// exec — copyTransferItem is already a plain Go call, not a real
	// external tool, unlike every other stage this file drives.
	command string
	goFunc  func(ctx context.Context) error

	errContext     string
	verb           string
	label          string
	destDir        string
	deleteOriginal string // "" unless this is an Extract with "delete original" requested
	onSuccess      func(r *Root)
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
// the same path. Dispatches to reallyStartCompressGoFunc for a
// goFunc-backed request (see compressRequest's own doc comment) — the
// two share every field below goFunc/command themselves, only how the
// actual work runs differs.
func (r *Root) reallyStartCompressJob(req compressRequest) {
	if req.goFunc != nil {
		r.reallyStartCompressGoFunc(req)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, userShell(), fullScreenShellArgs(req.command)...)
	// See this file's own package doc comment for why this is needed at
	// all — a real, reported bug, not a defensive guess.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// Only ever a real local directory: a subprocess-backed stage is
	// always the actual zip/tar/... invocation, which only ever runs
	// against locally-staged files even once either endpoint is remote
	// (see runCompressToRemote/extractCurrentArchiveToRemote's own doc
	// comments in compress.go for the download-stage/upload-stage split
	// that makes that true) — this mirrors runShellCommandFullScreen's
	// own identical, unconditional cmd.Dir.
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
		onSuccess: req.onSuccess,
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

// reallyStartCompressGoFunc is reallyStartCompressJob's own sibling
// for a goFunc-backed request (see compressRequest's own doc comment)
// — the same job/status-bar/queue shape, minus the subprocess: no cmd,
// no output buffer, no stderrTail, just req.goFunc run on this job's
// own background goroutine. Used for a download-from-remote or
// upload-to-remote stage (see runCompressToRemote/
// extractCurrentArchiveToRemote in compress.go), each a plain
// copyTransferItem call, not a real external tool to exec.
//
// Not independently cancellable mid-transfer: copyTransferItem takes
// no context of its own to check, the same accepted limitation
// remotepaste.go's own transfer engine already has for the identical
// reason (see its own package doc comment) — cancelling job.ctx here
// only stops this stage from being *waited on* further, not a transfer
// already under way inside req.goFunc itself.
func (r *Root) reallyStartCompressGoFunc(req compressRequest) {
	ctx, cancel := context.WithCancel(context.Background())
	job := &compressJob{
		ctx: ctx, cancel: cancel,
		startedAt: time.Now(), verb: req.verb, label: req.label,
		errContext: req.errContext, destDir: req.destDir, deleteOriginal: req.deleteOriginal,
		onSuccess: req.onSuccess,
	}
	r.compressJob = job

	r.safeGo(req.errContext+" job", func() { r.finishCompressJob(job, nil) }, func() {
		err := req.goFunc(ctx)
		var finishErr error
		if err != nil {
			finishErr = fmt.Errorf("%s: %w", job.errContext, err)
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
		r.activityLog.Error(activitylog.CategoryArchive, fmt.Sprintf("%s %s: %v", compressPastTenseVerb(job.verb), job.label, err))
		r.showError(err)
	case !wasCancelled:
		r.activityLog.Action(activitylog.CategoryArchive, fmt.Sprintf("%s %s", compressPastTenseVerb(job.verb), job.label))
		r.forEachTab(func(p *Panel) {
			if p.path == job.destDir {
				r.showError(p.load(p.path))
			}
		})
		if job.deleteOriginal != "" {
			r.deleteExtractedArchive(job.deleteOriginal)
		}
		if job.onSuccess != nil {
			job.onSuccess(r)
		}
	}
	r.refreshStatusBar()
	r.advanceCompressQueue()
}

// compressPastTenseVerb turns a compressJob's own present-continuous
// verb (the status bar's own leading word — "Compressing"/"Extracting"/
// "Uploading") into the past tense the activity log's completed-action
// lines use — every verb this file ever sets ends in "-ing" and drops
// it cleanly ("Compressing" -> "compressed"), so no per-verb table is
// needed.
func compressPastTenseVerb(verb string) string {
	if base, ok := strings.CutSuffix(verb, "ing"); ok {
		return strings.ToLower(base) + "ed"
	}
	return strings.ToLower(verb)
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
