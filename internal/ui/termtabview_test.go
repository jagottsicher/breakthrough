package ui

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/hinshun/vt10x"
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

func TestTermTabViewMouseHandlerClaimsFocusOnButtonDownInRect(t *testing.T) {
	session, err := termtab.Start(termNoopConn(), termtab.StartOptions{
		Cols: 20, Rows: 5,
		Command: func() *exec.Cmd { return exec.Command("sh", "-c", `sleep 5`) },
	})
	if err != nil {
		t.Fatalf("termtab.Start: %v", err)
	}
	defer func() { _ = session.Close() }()

	v := newTermTabView(session)
	v.SetRect(0, 0, 20, 5)

	var focused tview.Primitive
	setFocus := func(p tview.Primitive) { focused = p }

	handler := v.MouseHandler()
	consumed, _ := handler(tview.MouseLeftDown, tcell.NewEventMouse(2, 1, tcell.ButtonPrimary, tcell.ModNone), setFocus)
	if !consumed {
		t.Error("MouseLeftDown inside rect: consumed = false, want true")
	}
	if focused != v {
		t.Error("MouseLeftDown inside rect never called setFocus(v)")
	}
}

func TestTermTabViewMouseHandlerIgnoresEventsOutsideItsRect(t *testing.T) {
	session, err := termtab.Start(termNoopConn(), termtab.StartOptions{
		Cols: 20, Rows: 5,
		Command: func() *exec.Cmd { return exec.Command("sh", "-c", `sleep 5`) },
	})
	if err != nil {
		t.Fatalf("termtab.Start: %v", err)
	}
	defer func() { _ = session.Close() }()

	v := newTermTabView(session)
	v.SetRect(0, 0, 20, 5)

	noopFocus := func(tview.Primitive) {}
	handler := v.MouseHandler()
	consumed, _ := handler(tview.MouseLeftDown, tcell.NewEventMouse(50, 50, tcell.ButtonPrimary, tcell.ModNone), noopFocus)
	if consumed {
		t.Error("MouseLeftDown outside rect: consumed = true, want false (let another primitive have it)")
	}
}

// TestTermTabViewMouseHandlerForwardsClickToARealSessionOnceEnabled is
// the end-to-end proof that Mode(), termMouseBytes and WriteInput are
// wired together correctly: the remote script first turns on SGR
// mouse reporting exactly the way a real program (mc, vim, tmux) does,
// switches the pty to raw mode so a fixed byte count can be read back
// without waiting on a newline the mouse sequence never sends, then
// echoes those bytes back as hex so the test can assert on them
// through the ordinary cell buffer.
func TestTermTabViewMouseHandlerForwardsClickToARealSessionOnceEnabled(t *testing.T) {
	session, err := termtab.Start(termNoopConn(), termtab.StartOptions{
		Cols: 40, Rows: 5,
		Command: func() *exec.Cmd {
			return exec.Command("sh", "-c", `stty raw -echo; printf '\033[?1000h\033[?1006h'; dd bs=1 count=9 2>/dev/null | od -An -tx1 | tr -d ' \n'`)
		},
	})
	if err != nil {
		t.Fatalf("termtab.Start: %v", err)
	}
	defer func() { _ = session.Close() }()

	v := newTermTabView(session)
	v.SetRect(0, 0, 40, 5)
	screen := newTestSimulationScreen(t, 40, 5)

	deadline := time.Now().Add(2 * time.Second)
	for session.Mode()&vt10x.ModeMouseSgr == 0 {
		if time.Now().After(deadline) {
			t.Fatal("session never reported SGR mouse mode enabled")
		}
		time.Sleep(5 * time.Millisecond)
	}

	handler := v.MouseHandler()
	// Left-down at virtual column 0, row 0 — SGR-encodes to the fixed
	// 9-byte sequence "\x1b[<0;1;1M" the remote dd/od pipeline above
	// reads exactly.
	handler(tview.MouseLeftDown, tcell.NewEventMouse(0, 0, tcell.ButtonPrimary, tcell.ModNone), func(tview.Primitive) {})

	waitForScreenRow(t, v, screen, 0, "1b5b3c303b313b314d", 2*time.Second)
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
