package ui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/rsync"
)

func TestScanLinesOrCR(t *testing.T) {
	tests := []struct {
		name string
		data string
		want []string
	}{
		{"newline only", "a\nb\n", []string{"a", "b"}},
		{"carriage return only", "a\rb\r", []string{"a", "b"}},
		{"mixed", "a\rb\nc", []string{"a", "b", "c"}},
		{"no trailing terminator", "abc", []string{"abc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := newTestScanner(tt.data)
			var got []string
			for scanner.Scan() {
				got = append(got, scanner.Text())
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func newTestScanner(data string) *testScanner { return &testScanner{data: data} }

// testScanner is a tiny bufio.Scanner-alike built directly on
// scanLinesOrCR — avoids pulling in bufio just to exercise the split
// function in isolation from any real io.Reader.
type testScanner struct {
	data string
	cur  string
}

func (s *testScanner) Scan() bool {
	if s.data == "" {
		return false
	}
	advance, token, _ := scanLinesOrCR([]byte(s.data), true)
	if advance == 0 {
		return false
	}
	s.cur = string(token)
	s.data = s.data[advance:]
	return true
}

func (s *testScanner) Text() string { return s.cur }

func TestWatchRsyncProgressParsesThePercentageAndDetail(t *testing.T) {
	r := &Root{}
	job := &rsyncJob{}
	job.percent.Store(-1)

	input := "receiving file list ... done\r" +
		"      1,234  45%    2.00MB/s    0:00:03 (xfr#1, to-chk=3/5)\r" +
		"      9,999 100%    5.00MB/s    0:00:05 (xfr#5, to-chk=0/5)\n"

	r.watchRsyncProgress(job, strings.NewReader(input))

	if got := job.percent.Load(); got != 100 {
		t.Errorf("percent = %d, want 100 (the last progress line wins)", got)
	}
	detail := job.detail.Load()
	if detail == nil || *detail != "5.00MB/s    0:00:05 (xfr#5, to-chk=0/5)" {
		t.Errorf("detail = %v, want the last line's own rate/time/xfr-count text", detail)
	}
}

// TestWatchRsyncProgressParsesAPeriodGroupedByteCount pins a real,
// live-tested regression: rsync formats the leading byte count through
// the C library's own locale-aware grouping, whatever LC_NUMERIC the
// process inherits — de_DE (among plenty of others) groups with "."
// rather than english's ",", which a digit/comma-only pattern for that
// field would silently fail to match at all, leaving the status bar
// stuck reporting "starting…" for a transfer's entire length.
func TestWatchRsyncProgressParsesAPeriodGroupedByteCount(t *testing.T) {
	r := &Root{}
	job := &rsyncJob{}
	job.percent.Store(-1)

	input := "        204.800   2%  314,02kB/s    0:00:00 (xfr#1, to-chk=49/51)\r"

	r.watchRsyncProgress(job, strings.NewReader(input))

	if got := job.percent.Load(); got != 2 {
		t.Errorf("percent = %d, want 2", got)
	}
	detail := job.detail.Load()
	if detail == nil || *detail != "314,02kB/s    0:00:00 (xfr#1, to-chk=49/51)" {
		t.Errorf("detail = %v, want the locale-formatted rate/time/xfr-count text", detail)
	}
}

func TestWatchRsyncProgressIgnoresUnparseableLines(t *testing.T) {
	r := &Root{}
	job := &rsyncJob{}
	job.percent.Store(-1)

	// Neither an rsync progress line nor anything close to one — an
	// interactive shell's own .bashrc noise, say.
	r.watchRsyncProgress(job, strings.NewReader("Welcome to bash\nsome other output\n"))

	if got := job.percent.Load(); got != -1 {
		t.Errorf("percent = %d, want -1 (unchanged — nothing here looked like a progress line)", got)
	}
	if job.detail.Load() != nil {
		t.Error("detail should still be nil")
	}
}

// newFakeRsyncCmd builds a real *exec.Cmd running script through a real
// shell — not through userShell()/job.Command() at all, since this
// tests watchAndWaitRsync's own stdout/stderr/Wait plumbing directly,
// independent of reallyStartRsyncBackground's own argv construction
// (already covered by internal/rsync's own Command tests).
func newFakeRsyncCmd(t *testing.T, script string) (cmd *exec.Cmd, stdout, stderr *os.File) {
	t.Helper()
	cmd = exec.Command("/bin/sh", "-c", script)
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = stdoutW.Close()
	_ = stderrW.Close()
	return cmd, stdoutR, stderrR
}

func TestWatchAndWaitRsyncReportsACleanExit(t *testing.T) {
	r := &Root{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	job := &rsyncJob{ctx: ctx, cancel: cancel}
	job.percent.Store(-1)

	cmd, stdout, stderr := newFakeRsyncCmd(t, `printf '      1,000  50%%   1.00MB/s    0:00:01 (xfr#1, to-chk=1/2)\r'; printf '      2,000 100%%   1.00MB/s    0:00:02 (xfr#2, to-chk=0/2)\n'; exit 0`)

	err := r.watchAndWaitRsync(job, cmd, stdout, stderr)

	if err != nil {
		t.Fatalf("watchAndWaitRsync: %v", err)
	}
	if got := job.percent.Load(); got != 100 {
		t.Errorf("percent = %d, want 100", got)
	}
}

func TestWatchAndWaitRsyncReportsAFailureWithStderr(t *testing.T) {
	r := &Root{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	job := &rsyncJob{ctx: ctx, cancel: cancel}
	job.percent.Store(-1)

	cmd, stdout, stderr := newFakeRsyncCmd(t, `echo 'rsync: connection unexpectedly closed' >&2; exit 23`)

	err := r.watchAndWaitRsync(job, cmd, stdout, stderr)

	if err == nil {
		t.Fatal("expected an error for a non-zero exit, got nil")
	}
	if !strings.Contains(err.Error(), "connection unexpectedly closed") {
		t.Errorf("error = %q, want it to include rsync's own stderr", err.Error())
	}
}

func newTestRootForRsyncJob(t *testing.T) *Root {
	t.Helper()
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	return r
}

func TestFinishRsyncJobReloadsTheDestinationPanel(t *testing.T) {
	r := newTestRootForRsyncJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	job := &rsyncJob{ctx: ctx, cancel: cancel, destPath: r.panel.path}
	r.rsyncJob = job

	if err := os.WriteFile(filepath.Join(r.panel.path, "synced.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	r.finishRsyncJob(job, nil)

	var found bool
	for row := 1; row < r.panel.table.GetRowCount(); row++ {
		if ref, ok := r.panel.rowRef(row); ok && ref.name == "synced.txt" {
			found = true
		}
	}
	if !found {
		t.Error("panel was not reloaded after the rsync job finished — synced.txt is missing")
	}
	if r.rsyncJob != nil {
		t.Error("rsyncJob should be cleared once finished")
	}
}

func TestFinishRsyncJobReportsARealError(t *testing.T) {
	r := newTestRootForRsyncJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	job := &rsyncJob{ctx: ctx, cancel: cancel, destPath: r.panel.path}
	r.rsyncJob = job

	r.finishRsyncJob(job, errors.New("rsync: boom"))

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want %q for a genuine failure", r.activePage, errorPage)
	}
}

// TestFinishRsyncJobSuppressesErrorWhenCancelled pins
// cancelRsyncJob/finishRsyncJob's own shared contract: cmd.Wait
// returning an error because the process was just killed by a
// deliberate Ctrl+C is expected, not a failure worth an error overlay
// — job.ctx.Err() is what tells the two apart.
func TestFinishRsyncJobSuppressesErrorWhenCancelled(t *testing.T) {
	r := newTestRootForRsyncJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	job := &rsyncJob{ctx: ctx, cancel: cancel, destPath: r.panel.path}
	r.rsyncJob = job

	cancel() // simulate cancelRsyncJob already having run
	r.finishRsyncJob(job, errors.New("signal: killed"))

	if r.activePage == errorPage {
		t.Error("a cancelled job's own wait-error should not show an error overlay")
	}
}

func TestFinishRsyncJobStartsTheNextQueuedRun(t *testing.T) {
	r := newTestRootForRsyncJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	job := &rsyncJob{ctx: ctx, cancel: cancel, destPath: r.panel.path}
	r.rsyncJob = job
	r.rsyncQueue = []queuedRsync{{
		job:   rsync.Job{Source: rsync.Endpoint{Path: filepath.Join(r.panel.path, "a.txt")}, Destination: rsync.Endpoint{Path: t.TempDir()}},
		label: "queued",
	}}

	r.finishRsyncJob(job, nil)

	if r.rsyncJob == nil {
		t.Fatal("expected the queued run to have started")
	}
	if r.rsyncJob.label != "queued" {
		t.Errorf("started job label = %q, want %q", r.rsyncJob.label, "queued")
	}
	r.cancelRsyncJob() // don't leave a real rsync process running past this test
}

func TestCancelRsyncJobStopsTheProcessAndClearsTheQueue(t *testing.T) {
	r := newTestRootForRsyncJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	job := &rsyncJob{ctx: ctx, cancel: cancel}
	r.rsyncJob = job
	r.rsyncQueue = []queuedRsync{{label: "queued"}}

	r.cancelRsyncJob()

	if job.ctx.Err() == nil {
		t.Error("job's own context should be cancelled")
	}
	if len(r.rsyncQueue) != 0 {
		t.Error("rsyncQueue should be cleared by a deliberate cancel")
	}
}

func TestRunRsyncBackgroundRefusesAnEmptyDestination(t *testing.T) {
	r := newTestRootForRsyncJob(t)
	r.openRsync()
	r.rsyncSourceField.SetText(r.panel.path)
	r.rsyncDestinationField.SetText("")

	r.runRsyncBackground()

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want %q for an empty Destination", r.activePage, errorPage)
	}
	if r.rsyncJob != nil {
		t.Error("no background job should have started")
	}
}

func TestRsyncEndpointBase(t *testing.T) {
	tests := []struct {
		name string
		e    rsync.Endpoint
		want string
	}{
		{"local", rsync.Endpoint{Path: "/home/jens/reports"}, "reports"},
		{"remote", rsync.Endpoint{Host: "example.com", User: "tester", Path: "/srv/reports"}, "example.com:reports"},
	}
	for _, tt := range tests {
		if got := rsyncEndpointBase(tt.e); got != tt.want {
			t.Errorf("%s: rsyncEndpointBase() = %q, want %q", tt.name, got, tt.want)
		}
	}
}
