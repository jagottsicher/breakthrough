package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/termtab"
)

// termTabView is the embedded remote terminal's own tview.Primitive —
// feature_ideas.txt #11's Phase 2: renders a termtab.Session's own
// cell buffer directly (Draw) and translates keyboard input into the
// byte sequences a remote shell expects (termKeyBytes), the same
// custom-Primitive shape scrollbar.go/easteregg.go already establish.
//
// Deliberately NOT wired into a tab yet (Phase 3, feature_ideas.txt
// #11's own notes) — standalone and independently testable: a test
// builds one directly over a real termtab.Session (started against a
// plain "sh" command, exactly like internal/termtab's own tests, no
// network needed), draws it to a tcell.SimulationScreen, and feeds it
// key events through InputHandler — the same pattern every other
// custom primitive's own Draw/key tests in this package already use.
//
// Escape is deliberately never intercepted here the way toolWindow's
// own InputHandler intercepts it to close — a remote shell (vim's
// normal mode, any menu-driven TUI) needs Escape to reach it as real
// input; closing this view entirely is a tab-lifecycle concern for
// Phase 3, not this widget's own job.
type termTabView struct {
	*tview.Box
	session *termtab.Session
	// exited and onLeave back a real user request: once this
	// Terminal's own process has ended on its own (set alongside
	// shellTab.exited — see shells.go's own watchShellExit), there's
	// nothing left to send a keystroke to, so Ctrl+C, Escape and Tab
	// stop being forwarded to the (now-closed) session and instead
	// call onLeave — Root's own way back to the panel (wired in
	// shells.go). Checked only while exited, never for a live
	// session: those three keys are real, meaningful input to an
	// actual remote shell (interrupt, cancel, completion) and must
	// keep reaching it right up until there's truly nothing left on
	// the other end.
	exited  bool
	onLeave func()
}

// newTermTabView wraps session in a drawable, focusable Primitive.
func newTermTabView(session *termtab.Session) *termTabView {
	return &termTabView{Box: tview.NewBox(), session: session}
}

// Draw renders the session's own current cell buffer into this
// widget's inner rect, resizing the session first if the widget's own
// size has changed since the last draw (the box being resized is the
// only signal this layer has that the terminal's own dimensions ought
// to change too — see termtab.Session.Resize's own doc comment for
// why that updates both the virtual buffer and the real pty). The
// cursor cell is drawn in reverse video when visible, a plain
// highlight rather than a distinct cursor glyph — good enough for
// Phase 2, revisited only if it turns out not to be.
func (v *termTabView) Draw(screen tcell.Screen) {
	v.DrawForSubclass(screen, v)
	x, y, width, height := v.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}

	if cols, rows := v.session.Size(); cols != width || rows != height {
		_ = v.session.Resize(width, height)
	}

	cursor := v.session.Cursor()
	cursorVisible := v.session.CursorVisible()
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			g := v.session.Cell(col, row)
			style := termCellStyle(g)
			if cursorVisible && col == cursor.X && row == cursor.Y {
				style = style.Reverse(true)
			}
			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			screen.SetContent(x+col, y+row, ch, nil, style)
		}
	}
}

// InputHandler translates every key event into the byte sequence a
// remote shell expects (termKeyBytes) and writes it straight to the
// session — nothing here ever moves local focus or intercepts a key
// for this widget's own purposes (see its own doc comment on why
// Escape in particular must reach the remote side unconditionally).
// A key termKeyBytes doesn't translate at all is simply dropped, per
// its own doc comment — no error, nothing sent.
func (v *termTabView) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return v.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		if v.exited && v.onLeave != nil && isTermLeaveKey(event) {
			v.onLeave()
			return
		}
		b := termKeyBytes(event)
		if b == nil {
			return
		}
		_, _ = v.session.WriteInput(b)
	})
}

// isTermLeaveKey reports whether event is one of the three keys
// exited's own doc comment on the struct names — Ctrl+C, Escape, Tab
// — the user's own explicit choice: all three already mean "get out
// of here" in one context or another (interrupt, cancel, move focus
// on), so once a Terminal is dead there's nothing lost in repurposing
// them to actually leave it, rather than writing bytes into a closed
// pty that go nowhere.
func isTermLeaveKey(event *tcell.EventKey) bool {
	switch event.Key() {
	case tcell.KeyCtrlC, tcell.KeyEscape, tcell.KeyTab:
		return true
	default:
		return false
	}
}
