package termtab

import (
	"bufio"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"

	"github.com/jagottsicher/breakthrough/internal/remotefs"
	"github.com/jagottsicher/breakthrough/internal/sshshell"
)

// DefaultCols/DefaultRows are Start's own fallback terminal size when
// StartOptions.Cols/Rows are left at zero — the standard 80×24 every
// terminal emulator defaults to absent better information, overwritten
// almost immediately in practice by a later UI phase's own first real
// Resize call once it knows the actual widget size.
const (
	DefaultCols = 80
	DefaultRows = 24
)

// StartOptions configures Start.
type StartOptions struct {
	// Cols/Rows size the virtual terminal and the real pty at startup.
	// Zero means DefaultCols/DefaultRows.
	Cols, Rows int

	// Command, if set, overrides the real "ssh <sshshell.Argv(conn)...>"
	// invocation Start would otherwise build — the one seam a test uses
	// to simulate a remote shell's own output (a fixed script that
	// writes known bytes to its own stdout, say) without a real
	// network connection or ssh binary, the same override-var shape
	// mailImapSourceUnreadCount (internal/ui/mailimap.go) already has
	// for the identical "don't require the real external thing in a
	// unit test" reason.
	Command func() *exec.Cmd
}

// Session is one live embedded terminal — a real pty-backed process (see
// StartOptions.Command's own doc comment for the default: an actual ssh
// invocation, reusing sshshell.Argv) with a vt10x virtual terminal
// emulator continuously parsing its output into a cell buffer.
type Session struct {
	cmd  *exec.Cmd
	ptmx *os.File
	term vt10x.Terminal

	mu      sync.Mutex
	exited  bool
	exitErr error
	exitCh  chan struct{}
}

// Start launches conn's own remote shell under a real pty (StartOptions.Command,
// default a real "ssh" invocation — see its own doc comment) and begins
// parsing its output into a new vt10x virtual terminal immediately, on a
// background goroutine. The returned Session is live the moment Start
// returns; closing it (see Close) is the caller's own responsibility once
// it's no longer needed, the same contract an *exec.Cmd itself would have.
func Start(conn remotefs.Connection, opts StartOptions) (*Session, error) {
	cmdFunc := opts.Command
	if cmdFunc == nil {
		cmdFunc = func() *exec.Cmd { return exec.Command("ssh", sshshell.Argv(conn)...) }
	}
	cmd := cmdFunc()

	cols, rows := opts.Cols, opts.Rows
	if cols <= 0 {
		cols = DefaultCols
	}
	if rows <= 0 {
		rows = DefaultRows
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}

	s := &Session{
		cmd:    cmd,
		ptmx:   ptmx,
		term:   vt10x.New(vt10x.WithSize(cols, rows)),
		exitCh: make(chan struct{}),
	}

	go s.parseLoop()
	go s.waitLoop()

	return s, nil
}

// parseLoop continuously feeds the pty's own output into the virtual
// terminal — vt10x.Terminal.Parse itself already blocks on the reader
// until bytes arrive, locking the shared State for only as long as an
// actually-available batch takes to apply (see vt10x's own package doc),
// so this loop just keeps calling it until the pty closes (the process
// exited — see waitLoop for the real outcome) or a read fails outright.
func (s *Session) parseLoop() {
	r := bufio.NewReader(s.ptmx)
	for {
		if err := s.term.Parse(r); err != nil {
			return
		}
	}
}

// waitLoop blocks on the real process's own exit and records it —
// Done/ExitErr are how a later UI phase shows "process ended" instead of
// a frozen, stale buffer once this closes exitCh.
func (s *Session) waitLoop() {
	err := s.cmd.Wait()
	s.mu.Lock()
	s.exited = true
	s.exitErr = err
	s.mu.Unlock()
	close(s.exitCh)
}

// Done returns a channel that's closed once the underlying process has
// exited, on its own or via Close.
func (s *Session) Done() <-chan struct{} {
	return s.exitCh
}

// Exited reports whether the underlying process has already exited, and
// if so, with what error (nil for an ordinary zero exit, the same
// contract exec.Cmd.Wait's own return value already has).
func (s *Session) Exited() (exited bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited, s.exitErr
}

// Cell returns the glyph at (x, y) — locked around vt10x's own shared
// State first: parseLoop mutates that same state concurrently, and
// vt10x's View interface exists specifically so every read takes
// Lock()/Unlock() around it (see its own doc comment on Lock/Unlock) —
// skipping this here would be exactly the data race a later UI phase's
// own Draw(screen) call would otherwise hit the moment it runs
// concurrently with real PTY output, not a theoretical concern.
func (s *Session) Cell(x, y int) vt10x.Glyph {
	s.term.Lock()
	defer s.term.Unlock()
	return s.term.Cell(x, y)
}

// Cursor returns the virtual terminal's own current cursor position and
// attributes — same locking contract as Cell.
func (s *Session) Cursor() vt10x.Cursor {
	s.term.Lock()
	defer s.term.Unlock()
	return s.term.Cursor()
}

// CursorVisible reports whether the cursor should currently be drawn at
// all (a program can hide it, e.g. full-screen TUIs like htop/vim while
// they own the whole display) — same locking contract as Cell.
func (s *Session) CursorVisible() bool {
	s.term.Lock()
	defer s.term.Unlock()
	return s.term.CursorVisible()
}

// Size returns the virtual terminal's own current column/row count —
// same locking contract as Cell.
func (s *Session) Size() (cols, rows int) {
	s.term.Lock()
	defer s.term.Unlock()
	return s.term.Size()
}

// Resize changes both the virtual terminal's own buffer size and the
// real pty's own kernel-level window size (vt10x.ResizePty — a TIOCSWINSZ
// ioctl under the hood) — doing only one of the two is the classic
// embedded-terminal bug: the emulator's own idea of the screen and the
// remote shell's own $LINES/$COLUMNS (and anything that reacts to
// SIGWINCH, e.g. vim/htop) disagree, and whichever one didn't get told
// silently renders garbage or wraps wrong.
//
// Unlike Cell/Cursor/CursorVisible/Size, this one is deliberately NOT
// wrapped in an explicit term.Lock()/Unlock() of its own —
// vt10x.Terminal.Resize already takes that same lock internally (verified
// directly against vt10x's own vt_posix.go, not assumed from the View
// interface's general "callers lock reads" doc comment, which does not
// actually apply to this particular method); wrapping it here too would
// self-deadlock on the second, redundant Lock call from the very same
// goroutine, a real bug this package's own test caught immediately
// (TestResizeUpdatesTheVirtualTerminalsOwnSize hung the whole test binary
// until Go's own test timeout killed it).
func (s *Session) Resize(cols, rows int) error {
	s.term.Resize(cols, rows)
	return vt10x.ResizePty(s.ptmx, cols, rows)
}

// WriteInput sends p straight to the pty master, exactly as if it had
// been typed at a real terminal — translating a key event into the
// right byte sequence (arrow keys, Ctrl combinations, ...) is a later
// phase's own job (feature_ideas.txt #11), not this package's: Session
// itself only ever moves bytes, never interprets tcell's own key events.
func (s *Session) WriteInput(p []byte) (int, error) {
	return s.ptmx.Write(p)
}

// Close terminates the session: closing the pty master first so a
// parseLoop blocked in Read returns on its own, then killing the
// process if Wait hasn't already reported it exited — safe to call more
// than once, or after the process has already exited by itself.
func (s *Session) Close() error {
	_ = s.ptmx.Close()
	if exited, _ := s.Exited(); !exited && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return nil
}
