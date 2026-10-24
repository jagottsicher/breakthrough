package ui

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/remotefs"
	"github.com/jagottsicher/breakthrough/internal/termtab"
)

// termNoopConn mirrors internal/termtab's own connNoop — every test
// here overrides StartOptions.Command, so the connection's own field
// values are never actually read by Start.
func termNoopConn() remotefs.Connection {
	return remotefs.Connection{Host: "unused-termview-test-conn", User: "nobody"}
}

func newTestSimulationScreen(t *testing.T, cols, rows int) tcell.SimulationScreen {
	t.Helper()
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(cols, rows)
	return screen
}

// waitForScreenRow polls screen (rather than sleeping a fixed amount
// — see termtab's own waitForCellRow for the identical reasoning: a
// real process's own output timing isn't deterministic, and
// termTabView.Draw must be called repeatedly here anyway to pick up
// whatever the session's own background parse goroutine has produced
// since the last call) until row y's own first len(want) columns read
// back as want, or until timeout elapses.
func waitForScreenRow(t *testing.T, v *termTabView, screen tcell.Screen, y int, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var got string
	for time.Now().Before(deadline) {
		v.Draw(screen)
		var b strings.Builder
		for x := 0; x < len(want); x++ {
			ch, _, _ := screen.Get(x, y)
			b.WriteString(ch)
		}
		got = b.String()
		if got == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("row %d = %q after %s, want %q", y, got, timeout, want)
}

func TestTermTabViewDrawRendersRealSessionOutput(t *testing.T) {
	session, err := termtab.Start(termNoopConn(), termtab.StartOptions{
		Cols: 20, Rows: 5,
		Command: func() *exec.Cmd { return exec.Command("sh", "-c", `printf 'hello'`) },
	})
	if err != nil {
		t.Fatalf("termtab.Start: %v", err)
	}
	defer func() { _ = session.Close() }()

	v := newTermTabView(session)
	v.SetRect(0, 0, 20, 5)
	screen := newTestSimulationScreen(t, 20, 5)

	waitForScreenRow(t, v, screen, 0, "hello", 2*time.Second)
}

func TestTermTabViewDrawResizesTheSessionToMatchItsOwnRect(t *testing.T) {
	session, err := termtab.Start(termNoopConn(), termtab.StartOptions{
		Cols: 20, Rows: 5,
		Command: func() *exec.Cmd { return exec.Command("sh", "-c", `sleep 5`) },
	})
	if err != nil {
		t.Fatalf("termtab.Start: %v", err)
	}
	defer func() { _ = session.Close() }()

	v := newTermTabView(session)
	v.SetRect(0, 0, 40, 10)
	screen := newTestSimulationScreen(t, 40, 10)
	v.Draw(screen)

	cols, rows := session.Size()
	if cols != 40 || rows != 10 {
		t.Errorf("session.Size() after Draw with a 40x10 rect = (%d, %d), want (40, 10)", cols, rows)
	}
}

func TestTermTabViewInputHandlerWritesToTheRealSession(t *testing.T) {
	session, err := termtab.Start(termNoopConn(), termtab.StartOptions{
		Cols: 30, Rows: 5,
		Command: func() *exec.Cmd { return exec.Command("sh", "-c", `read x; printf 'got:%s' "$x"`) },
	})
	if err != nil {
		t.Fatalf("termtab.Start: %v", err)
	}
	defer func() { _ = session.Close() }()

	v := newTermTabView(session)
	v.SetRect(0, 0, 30, 5)
	screen := newTestSimulationScreen(t, 30, 5)

	handler := v.InputHandler()
	noopFocus := func(tview.Primitive) {}
	for _, r := range "hello" {
		handler(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone), noopFocus)
	}
	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), noopFocus)

	// Row 0 ends up holding the echoed "hello" itself (the pty's own
	// line discipline, same as internal/termtab's own
	// TestWriteInputReachesTheRemoteProcess already found) — the
	// shell's own response lands on row 1.
	waitForScreenRow(t, v, screen, 1, "got:hello", 2*time.Second)
}

func TestTermTabViewForwardsCtrlCEscapeTabToALiveSession(t *testing.T) {
	session, err := termtab.Start(termNoopConn(), termtab.StartOptions{
		Cols: 20, Rows: 5,
		Command: func() *exec.Cmd { return exec.Command("sh", "-c", `sleep 5`) },
	})
	if err != nil {
		t.Fatalf("termtab.Start: %v", err)
	}
	defer func() { _ = session.Close() }()

	v := newTermTabView(session)
	leaveCalls := 0
	v.onLeave = func() { leaveCalls++ }

	handler := v.InputHandler()
	noopFocus := func(tview.Primitive) {}
	handler(tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModNone), noopFocus)
	handler(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), noopFocus)
	handler(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), noopFocus)

	if leaveCalls != 0 {
		t.Errorf("onLeave called %d times for a live session, want 0 — these keys must still reach it", leaveCalls)
	}
}

func TestTermTabViewCtrlCEscapeTabCallOnLeaveOnceExited(t *testing.T) {
	session, err := termtab.Start(termNoopConn(), termtab.StartOptions{
		Cols: 20, Rows: 5,
		Command: func() *exec.Cmd { return exec.Command("sh", "-c", `sleep 5`) },
	})
	if err != nil {
		t.Fatalf("termtab.Start: %v", err)
	}
	defer func() { _ = session.Close() }()

	v := newTermTabView(session)
	v.exited = true
	leaveCalls := 0
	v.onLeave = func() { leaveCalls++ }

	handler := v.InputHandler()
	noopFocus := func(tview.Primitive) {}
	handler(tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModNone), noopFocus)
	handler(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), noopFocus)
	handler(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), noopFocus)

	if leaveCalls != 3 {
		t.Errorf("onLeave called %d times, want 3 (Ctrl+C, Escape, Tab each once)", leaveCalls)
	}
}

func TestTermTabViewOtherKeysStillReachTheSessionOnceExited(t *testing.T) {
	session, err := termtab.Start(termNoopConn(), termtab.StartOptions{
		Cols: 20, Rows: 5,
		Command: func() *exec.Cmd { return exec.Command("sh", "-c", `sleep 5`) },
	})
	if err != nil {
		t.Fatalf("termtab.Start: %v", err)
	}
	defer func() { _ = session.Close() }()

	v := newTermTabView(session)
	v.exited = true
	v.onLeave = func() { t.Error("onLeave should not fire for a plain rune key") }

	handler := v.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone), func(tview.Primitive) {})
}
