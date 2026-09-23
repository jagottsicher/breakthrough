// rsyncjob.go is the "Run in background" button's own engine — an
// alternative to runRsync's suspend-the-terminal path (see its own doc
// comment in rsync.go) for the common case where watching rsync's own
// live terminal output isn't actually needed: this runs the exact same
// command as a real child process, with its stdout attached to a real
// pty rather than connected to breakthrough's own terminal, parses its
// own --info=progress2 output (already always on — see
// currentRsyncJob's own Progress: true) into a percentage shown in the
// status bar the same way a Paste's own progress already is, and lets
// it keep running fully independently of whatever Copy/Cut/Paste is
// doing at the same time, per the user's own explicit request. The two
// share no state, no lock, and no queue — they touch entirely different
// process trees, so there is no reason for one to ever wait on the
// other.
//
// A pty for stdout, not a plain pipe — a real, live-tested finding, not
// a guess: a piped (non-tty) stdout is fully block-buffered by glibc's
// own default (commonly 4KB), so rsync's own short progress lines can
// sit invisible in that buffer for as long as a whole transfer takes to
// fill it, defeating the entire point of a *live* percentage. A pty's
// own slave end still answers isatty(1) truthfully, keeping rsync
// flushing every update the same way it would printing straight to a
// real terminal — see reallyStartRsyncBackground's own doc comment for
// the exact mechanics.
//
// Deliberately still just one background rsync at a time, queued the
// same way Paste already queues a second request behind a running one
// (see r.rsyncQueue/advanceRsyncQueue): showing more than one rsync's
// own live progress at once would need its own multi-row status-bar
// design this first version doesn't attempt.
//
// A backgrounded rsync's own stdin reads from the OS's null device —
// not this application's own, and not the same pty as stdout either —
// an unknown ssh host key or a password prompt neither of those can
// answer then fails fast with a real, reported error instead of
// hanging forever with no visible prompt at all, the same fail-fast
// behavior a real `ssh` client already falls back to whenever its own
// stdin isn't a terminal.
package ui

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/rsync"
)

// rsyncJob is one backgrounded rsync run's own async state.
type rsyncJob struct {
	ctx    context.Context
	cancel context.CancelFunc
	cmd    *exec.Cmd

	startedAt time.Time
	label     string // "sourceBase -> destBase", the status bar's own trailing text
	destPath  string // job.Destination.Path — reload whichever open tab shows this once it finishes

	// percent is -1 until rsync's own first progress line arrives (still
	// starting — connecting, or building its file list) and 0-100 after.
	// detail carries the rest of that same line verbatim (rate, elapsed
	// time, and the "(xfr#i, to-chk=j/k)" file count rsync itself
	// already renders human-readably) — never reformatted or
	// reinterpreted, the same "show exactly what the real tool says"
	// principle the live preview line above this dialog already
	// follows. Both atomics: written from the goroutine reading rsync's
	// own stdout, read from refreshStatusBar's own ticker, the same
	// single-writer/sampled-reader contract pasteJob.currentFile already
	// uses for the identical reason.
	percent atomic.Int32
	detail  atomic.Pointer[string]
}

// queuedRsync is one backgrounded rsync request startRsyncBackground had
// to defer because another was already running — see
// advanceRsyncQueue's own doc comment.
type queuedRsync struct {
	job   rsync.Job
	label string
}

// rsyncProgressLineRegexp parses one line of rsync's own --info=progress2
// output:
//
//	1,234,567  45%    2.00MB/s    0:00:03 (xfr#5, to-chk=10/15)
//
// Group 1 is the percentage; group 2 is everything rsync prints after
// it (rate, elapsed time, and the parenthesized transfer count) —
// carried through to rsyncJob.detail completely verbatim rather than
// re-parsed field by field, so a rsync version that phrases the
// parenthesized part slightly differently (older releases say
// "ir-chk" instead of "to-chk") still displays correctly; only the
// percentage itself needs to be a real number this app can put in a
// bar.
//
// The leading byte count is matched as a bare \S+, not a digit/comma
// character class — a real, live-tested finding, not a guess: rsync
// formats that number through the C library's own locale-aware
// grouping, whatever LC_NUMERIC this process (and so its rsync child)
// inherits from the environment. A comma-grouped "1,234,567" is only
// the English-locale spelling; de_DE (among plenty of others) instead
// prints "1.234.567" — a period-grouped number a digit/comma-only
// pattern would flatly fail to match, silently leaving every progress
// line unparsed and the status bar stuck on "starting…" for the whole
// transfer. This app never actually needs that number's own internal
// digit grouping at all, only that some non-blank text precedes the
// percentage, so matching it loosely is strictly more correct, not
// just more permissive.
var rsyncProgressLineRegexp = regexp.MustCompile(`^\s*\S+\s+(\d+)%\s+(.*\S)\s*$`)

// startRsyncBackground is the "Run in background" button's action —
// queues behind an already-running background rsync the same way
// startPaste already queues a second Paste (see advanceRsyncQueue), or
// starts job immediately otherwise.
func (r *Root) startRsyncBackground(job rsync.Job, label string) {
	if r.rsyncJob != nil {
		r.rsyncQueue = append(r.rsyncQueue, queuedRsync{job: job, label: label})
		r.refreshStatusBar()
		return
	}
	r.reallyStartRsyncBackground(job, label)
}

// reallyStartRsyncBackground is startRsyncBackground's own "actually
// begin" body — split out the same way reallyStartPaste is, so
// advanceRsyncQueue can start the next queued run through exactly the
// same path.
func (r *Root) reallyStartRsyncBackground(job rsync.Job, label string) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, userShell(), fullScreenShellArgs(job.Command())...)
	// Only ever a real local directory — see runShellCommandFullScreen's
	// own doc comment for why r.panel.path is skipped outright while the
	// active panel is a remote connection: it's a path on that other
	// machine, and passing it here would make the child's own chdir fail
	// before rsync ever starts, misreported in a way that reads exactly
	// like rsync itself couldn't find its own remote source file.
	if r.panel.remote == nil {
		cmd.Dir = r.panel.path
	}
	// Setsid: a real, live-tested finding, not a guess — without this,
	// the child (an *interactive* shell, per fullScreenShellArgs' own
	// "-i") inherits breakthrough's own process group and controlling
	// terminal, then immediately tries its own job-control setup
	// (tcsetpgrp and friends) against its stdout — the pty allocated
	// just below, which it has no legitimate claim to as a background
	// member of breakthrough's own foreground group. The kernel's
	// answer to that is SIGTTOU, sent to the whole process group —
	// including breakthrough itself, which promptly stops (the
	// terminal-visible symptom: the whole app freezes as if suspended,
	// dropping back to a shell prompt behind it, exactly like a real
	// Ctrl+Z). Setsid gives the child its own new session with no
	// controlling terminal to fight over in the first place.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	// Stdout goes to a real pty, not a plain pipe — a real, live-tested
	// finding, not a guess: glibc only line-buffers a program's stdout
	// when isatty(1) is true, and treats anything else (a pipe, same as
	// a plain file) as fully block-buffered instead, by default around
	// 4KB. rsync's own --info=progress2 line is short — tens of files'
	// worth of updates can sit held in that buffer, invisible, for as
	// long as a whole transfer takes to fill it, which defeats the
	// entire point of a *live* percentage. A pty's own slave end still
	// answers isatty(1) truthfully, so rsync keeps flushing every
	// update the same way it would printing straight to a real
	// terminal — see runRsync's own doc comment for the direct,
	// foreground equivalent of exactly this.
	//
	// Stdin stays the OS's null device (Go's own documented behavior
	// for a nil Stdin, untouched by any of this — a pty's own master/
	// slave pair is a separate file descriptor from Stdin entirely): an
	// unknown ssh host key or a password prompt neither of those can
	// answer then fails fast with a real, reported error instead of
	// hanging forever with no visible prompt at all, the same fail-fast
	// behavior a real `ssh` client already falls back to whenever its
	// own stdin isn't a terminal.
	ptyMaster, ptySlave, err := pty.Open()
	if err != nil {
		cancel()
		r.showError(fmt.Errorf("rsync: %w", err))
		return
	}
	cmd.Stdout = ptySlave
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		_ = ptyMaster.Close()
		_ = ptySlave.Close()
		r.showError(fmt.Errorf("rsync: %w", err))
		return
	}

	rj := &rsyncJob{
		ctx: ctx, cancel: cancel, cmd: cmd,
		startedAt: time.Now(), label: label, destPath: job.Destination.Path,
	}
	rj.percent.Store(-1)
	r.rsyncJob = rj

	if err := cmd.Start(); err != nil {
		cancel()
		_ = ptyMaster.Close()
		_ = ptySlave.Close()
		r.showError(fmt.Errorf("rsync: %w", err))
		r.rsyncJob = nil
		return
	}
	// The child has its own copy of the slave now (inherited across
	// Start) — closing this process' own copy is what lets ptyMaster's
	// own Read eventually unblock once the child's copy closes too
	// (on exit), the same reason a pipe's own write end has to close
	// in every process that inherited it before the read end ever
	// sees EOF.
	_ = ptySlave.Close()

	r.safeGo("rsync background", func() { r.finishRsyncJob(rj, nil) }, func() {
		finishErr := r.watchAndWaitRsync(rj, cmd, ptyMaster, stderr)
		_ = ptyMaster.Close()
		r.app.QueueUpdateDraw(func() { r.finishRsyncJob(rj, finishErr) })
	})
	r.safeGo("rsync background animation", func() { r.finishRsyncJob(rj, nil) }, func() {
		r.animateRsyncProgress(rj)
	})

	r.refreshStatusBar()
}

// watchAndWaitRsync is reallyStartRsyncBackground's own synchronous
// core — split out on its own so a test can drive it directly against
// a fake process with no real Application event loop behind it, the
// same reasoning runRemotePaste's own doc comment gives for its
// identical split from startRemotePaste. Parses job's own progress from
// stdout (see watchRsyncProgress) and drains stderr, concurrently, both
// fully before cmd.Wait() is ever called — exec.Cmd's own documented
// contract for StderrPipe specifically: Wait closes it as soon as it
// sees the process exit, so calling Wait before every read has
// finished can hand a reader a closed pipe out from under it, silently
// truncating whatever of rsync's own stderr hadn't been read yet.
// stdout itself is a pty's master end here (see reallyStartRsyncBackground's
// own doc comment on why), not something Wait manages at all — closing
// it once this function returns is the caller's own job.
func (r *Root) watchAndWaitRsync(job *rsyncJob, cmd *exec.Cmd, stdout, stderr io.Reader) error {
	var wg sync.WaitGroup
	var stderrBuf bytes.Buffer
	wg.Add(2)
	go func() {
		defer wg.Done()
		r.watchRsyncProgress(job, stdout)
	}()
	go func() {
		defer wg.Done()
		_, _ = stderrBuf.ReadFrom(stderr)
	}()
	wg.Wait()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("rsync: %w%s", err, stderrTail(stderrBuf.String()))
	}
	return nil
}

// stderrTail appends rsync's own stderr, if it said anything, to an
// error already carrying *exec.ExitError's own uninformative "exit
// status 23" — rsync's real reason (a bad host, a permission error, a
// vanished source file) always goes to stderr, never captured by the
// exit code alone.
func stderrTail(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	return ": " + stderr
}

// watchRsyncProgress reads stdout line by line — splitting on a bare
// "\r" as well as "\n", since --info=progress2 rewrites its one running
// total line in place with carriage returns exactly the way a real
// terminal would render it, the same reason runShellCommandFullScreen's
// own foreground path needs a real terminal for this to be visible at
// all; reading it as a stream instead recovers every intermediate
// update a plain bufio.Scanner (newline-only) would otherwise buffer up
// and only ever reveal the very last one, at the moment the whole
// transfer already finished.
//
// Only ever writes to job's own atomics — never touches the UI
// directly (no QueueUpdateDraw here at all): animateRsyncProgress's own
// ticker is what actually samples them onto the screen, the same
// single-writer/sampled-reader split pasteJob.currentFile already uses,
// and the reason this whole function stays directly testable against a
// plain io.Reader with no real Application event loop behind it.
func (r *Root) watchRsyncProgress(job *rsyncJob, stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Split(scanLinesOrCR)
	for scanner.Scan() {
		line := scanner.Text()
		m := rsyncProgressLineRegexp.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		percent, err := strconv.Atoi(m[1])
		if err != nil {
			continue // the regex only matches one or more digits here — never actually reachable, but no reason to trust that blindly
		}
		job.percent.Store(int32(percent))
		detail := m[2]
		job.detail.Store(&detail)
	}
}

// animateRsyncProgress redraws the status bar on a fixed tick for as
// long as job is still running — the same shape and interval
// animatePasteProgress already uses for the identical reason: rsync's
// own progress line updates roughly once a second on its own, but a
// ticker decoupled from that (rather than a redraw pushed from inside
// watchRsyncProgress's own read loop) is what keeps that function a
// plain, directly-testable reader with no UI dependency of its own.
func (r *Root) animateRsyncProgress(job *rsyncJob) {
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
				r.refreshStatusBar()
			})
		case <-job.ctx.Done():
			return
		}
	}
}

// scanLinesOrCR is bufio.ScanLines' own sibling: splits on the first
// "\r" or "\n" found, whichever comes first, rather than "\n" only —
// see watchRsyncProgress's own doc comment for why that distinction is
// the whole point here.
func scanLinesOrCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// finishRsyncJob applies a background rsync's own outcome: reports a
// genuine failure (nil for a clean exit or a deliberate cancel — see
// cancelRsyncJob), reloads whichever open tab shows the destination
// (local or remote — matched by path string alone, the same
// simplification reloadPasteAffectedTabs already accepts for a remote
// destination), and starts the next queued run, if any.
func (r *Root) finishRsyncJob(job *rsyncJob, err error) {
	if r.rsyncJob != job {
		return // already superseded/cancelled
	}
	// Read before job.cancel() below, which would otherwise make this
	// check always true — job.cancel is this same job's own
	// context.CancelFunc, called unconditionally right here as this
	// job's own cleanup, not only from a deliberate cancelRsyncJob.
	wasCancelled := job.ctx.Err() != nil
	r.rsyncJob = nil
	job.cancel()

	switch {
	case err != nil && !wasCancelled:
		// !wasCancelled: a real failure, not this job being cancelled
		// out from under itself (cancelRsyncJob's own context.Cancel is
		// what makes cmd.Wait return an error too — that one is
		// expected, already handled by cancelRsyncJob itself, and must
		// not also show as a spurious error overlay).
		r.activityLog.Error(activitylog.CategoryRsync, fmt.Sprintf("rsync %s: %v", job.label, err))
		r.showError(err)
	case !wasCancelled:
		r.activityLog.Action(activitylog.CategoryRsync, fmt.Sprintf("rsync %s", job.label))
	}
	r.forEachTab(func(p *Panel) {
		if p.path == job.destPath {
			r.showError(p.load(p.path))
		}
	})
	r.refreshStatusBar()
	r.advanceRsyncQueue()
}

// cancelRsyncJob stops a running background rsync, if any — Ctrl+C's
// own second target alongside cancelPasteJob (see RequestCancel).
// Cancelling the context makes exec.CommandContext kill the process
// (SIGKILL, Go's own documented default) and unblocks cmd.Wait, whose
// own goroutine then calls finishRsyncJob — the ctx.Err() check there
// is what tells that expected wait-error apart from a genuine failure.
func (r *Root) cancelRsyncJob() {
	if r.rsyncJob == nil {
		return
	}
	r.rsyncJob.cancel()
	r.rsyncQueue = nil
}

// advanceRsyncQueue starts the next queued background rsync, if any —
// the same shape advancePasteQueue already has for Paste.
func (r *Root) advanceRsyncQueue() {
	if len(r.rsyncQueue) == 0 {
		return
	}
	next := r.rsyncQueue[0]
	r.rsyncQueue = r.rsyncQueue[1:]
	r.reallyStartRsyncBackground(next.job, next.label)
}
