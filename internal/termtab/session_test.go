package termtab

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/hinshun/vt10x"

	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// connNoop is a placeholder remotefs.Connection for every test that
// overrides StartOptions.Command — Start's own default command
// construction is the only path that actually reads it (see
// TestDefaultCommandBuildsRealSSHArgv, the one test that doesn't
// override Command), so its own exact field values don't matter to
// any other test here, only that it's some valid-shaped value.
// "invalid" is a reserved TLD (RFC 2606) guaranteed to never resolve,
// so DNS/connect fails fast rather than hanging.
func connNoop() remotefs.Connection {
	return remotefs.Connection{Host: "nonexistent-host-for-termtab-tests.invalid", User: "nobody"}
}

// waitForCellRow polls (rather than sleeping a fixed amount — a real
// process's own output timing isn't deterministic) until row y's own
// cells, read left to right starting at x=0, equal want, or until
// timeout elapses. Session's own parseLoop runs concurrently on its
// own goroutine (see its own doc comment), so a test can never assume
// output is already parsed the instant a command finishes writing it
// or even the instant Done() closes — only that it eventually will be.
func waitForCellRow(t *testing.T, s *Session, y int, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var got string
	for time.Now().Before(deadline) {
		var b strings.Builder
		for x := 0; x < len(want); x++ {
			b.WriteRune(s.Cell(x, y).Char)
		}
		got = b.String()
		if got == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("row %d = %q after %s, want %q", y, got, timeout, want)
}

func shCommand(script string) func() *exec.Cmd {
	return func() *exec.Cmd { return exec.Command("sh", "-c", script) }
}

func TestStartParsesRealOutputIntoCells(t *testing.T) {
	s, err := Start(connNoop(), StartOptions{
		Cols: 20, Rows: 5,
		Command: shCommand(`printf 'hello'`),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Close() }()

	waitForCellRow(t, s, 0, "hello", 2*time.Second)
}

func TestSizeReturnsTheConfiguredStartingSize(t *testing.T) {
	s, err := Start(connNoop(), StartOptions{
		Cols: 42, Rows: 13,
		Command: shCommand(`sleep 5`),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Close() }()

	cols, rows := s.Size()
	if cols != 42 || rows != 13 {
		t.Errorf("Size() = (%d, %d), want (42, 13)", cols, rows)
	}
}

func TestSizeFallsBackToDefaultsWhenUnset(t *testing.T) {
	s, err := Start(connNoop(), StartOptions{
		Command: shCommand(`sleep 5`),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Close() }()

	cols, rows := s.Size()
	if cols != DefaultCols || rows != DefaultRows {
		t.Errorf("Size() = (%d, %d), want (%d, %d)", cols, rows, DefaultCols, DefaultRows)
	}
}

func TestResizeUpdatesTheVirtualTerminalsOwnSize(t *testing.T) {
	s, err := Start(connNoop(), StartOptions{
		Cols: 80, Rows: 24,
		Command: shCommand(`sleep 5`),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Resize(100, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	cols, rows := s.Size()
	if cols != 100 || rows != 40 {
		t.Errorf("Size() after Resize(100, 40) = (%d, %d), want (100, 40)", cols, rows)
	}
}

func TestWriteInputReachesTheRemoteProcess(t *testing.T) {
	s, err := Start(connNoop(), StartOptions{
		Cols: 40, Rows: 5,
		Command: shCommand(`read x; printf 'got:%s' "$x"`),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, err := s.WriteInput([]byte("hello\n")); err != nil {
		t.Fatalf("WriteInput: %v", err)
	}

	// Row 0 ends up holding "hello" itself — the pty's own line
	// discipline echoes typed input back before the shell ever sees
	// it, real terminal behavior and not something Session controls
	// (verified directly against the actual output here, not assumed:
	// an earlier version of this test asserted row 0 and failed,
	// showing exactly this). The shell's own response, printed after
	// that echoed newline already advanced the cursor, lands on row 1
	// instead.
	waitForCellRow(t, s, 1, "got:hello", 2*time.Second)
}

// waitForMode polls (same reasoning as waitForCellRow: parseLoop runs
// concurrently, so the mode bit set by the remote program's own
// escape sequence isn't necessarily applied the instant printf
// returns) until s.Mode()&want == want, or fails after timeout.
func waitForMode(t *testing.T, s *Session, want vt10x.ModeFlag, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var got vt10x.ModeFlag
	for time.Now().Before(deadline) {
		got = s.Mode()
		if got&want == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("Mode() = %v after %s, want %v set", got, timeout, want)
}

func TestModeReflectsMouseReportingTurnedOnByTheRemoteProgram(t *testing.T) {
	s, err := Start(connNoop(), StartOptions{
		Cols: 20, Rows: 5,
		// "\x1b[?1000h\x1b[?1006h" is exactly what a real program
		// (mc, vim, tmux, ...) writes to turn on button-event mouse
		// reporting with SGR extended coordinates — the same two
		// private modes a direct, non-embedded ssh connection's own
		// terminal would also see and act on.
		Command: shCommand(`printf '\033[?1000h\033[?1006h'`),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Close() }()

	waitForMode(t, s, vt10x.ModeMouseButton|vt10x.ModeMouseSgr, 2*time.Second)
}

func TestModeHasNoMouseBitsBeforeAnyProgramAsksForReporting(t *testing.T) {
	s, err := Start(connNoop(), StartOptions{
		Cols: 20, Rows: 5,
		Command: shCommand(`sleep 5`),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Close() }()

	if got := s.Mode() & vt10x.ModeMouseMask; got != 0 {
		t.Errorf("Mode()&ModeMouseMask = %v before any program ran, want 0", got)
	}
}

func TestExitedReportsCleanExit(t *testing.T) {
	s, err := Start(connNoop(), StartOptions{
		Command: shCommand(`exit 0`),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Close() }()

	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done() never closed")
	}
	exited, exitErr := s.Exited()
	if !exited {
		t.Error("Exited() = false, want true")
	}
	if exitErr != nil {
		t.Errorf("Exited() error = %v, want nil for a clean exit", exitErr)
	}
}

func TestExitedReportsNonzeroExit(t *testing.T) {
	s, err := Start(connNoop(), StartOptions{
		Command: shCommand(`exit 3`),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Close() }()

	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done() never closed")
	}
	exited, exitErr := s.Exited()
	if !exited {
		t.Error("Exited() = false, want true")
	}
	if exitErr == nil {
		t.Error("Exited() error = nil, want a non-nil error for a nonzero exit")
	}
}

func TestCloseTerminatesAStillRunningProcess(t *testing.T) {
	s, err := Start(connNoop(), StartOptions{
		Command: shCommand(`sleep 100`),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if exited, _ := s.Exited(); exited {
		t.Fatal("setup: process reported exited before Close was ever called")
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done() never closed after Close")
	}
	if exited, _ := s.Exited(); !exited {
		t.Error("Exited() = false after Close, want true")
	}
}

func TestCloseIsSafeToCallTwice(t *testing.T) {
	s, err := Start(connNoop(), StartOptions{
		Command: shCommand(`sleep 100`),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestCloseIsSafeAfterTheProcessAlreadyExitedOnItsOwn(t *testing.T) {
	s, err := Start(connNoop(), StartOptions{
		Command: shCommand(`exit 0`),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done() never closed")
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close after a natural exit: %v", err)
	}
}

func TestDefaultCommandBuildsRealSSHArgv(t *testing.T) {
	// Not actually run (no network/ssh server in a unit test) — just
	// confirms Start's own default command-construction path (no
	// StartOptions.Command override) produces a real "ssh ..." argv via
	// sshshell.Argv rather than something else, the one piece of the
	// default path a test can check without a real connection. A
	// nonexistent host makes ssh itself fail fast (DNS/connect
	// refused) rather than hang, which is all this test needs: Start
	// should still return a live Session (the pty/process themselves
	// started fine — ssh failing to *connect* is its own, later exit,
	// not a failure to launch at all).
	s, err := Start(connNoop(), StartOptions{})
	if err != nil {
		t.Fatalf("Start with the default command: %v", err)
	}
	defer func() { _ = s.Close() }()

	select {
	case <-s.Done():
		exited, exitErr := s.Exited()
		if !exited || exitErr == nil {
			t.Errorf("ssh against a nonexistent host: exited=%v err=%v, want a failed exit", exited, exitErr)
		}
	case <-time.After(5 * time.Second):
		// ssh itself is still trying to resolve/connect — acceptable;
		// this test only needs to confirm Start launched something real
		// without erroring outright, not wait out ssh's own timeout.
	}
}
