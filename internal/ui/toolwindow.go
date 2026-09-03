package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// toolWindow is a small, freely positioned, draggable overlay hosting
// one running external command's live output — the mechanism behind
// this session's own first entry, Ping (see openPingTestWindow), and
// eventually every Toolbox command that doesn't finish instantly
// (nmap, tail -f, ...; see feature_ideas.txt). Unlike every other
// overlay in this codebase — Properties/Chmod/Search are centered, the
// Details sidebar is docked to one edge — a toolWindow's own position is
// state the user controls directly, by dragging its title bar or moving
// it with Alt+arrow keys, per the user's own explicit request: a
// permanently docked panel (the obvious reuse candidate) would cover
// content exactly the way they were trying to avoid.
//
// Deliberately non-modal: each one is its own dynamically added/removed
// Pages entry (see Root.openToolCommand), never routed through
// showOverlay/hideOverlay — the panel underneath, and every other open
// toolWindow, stay fully usable while this one floats over them, the
// same "non-modal" precedent the Details sidebar already established
// (see detailssidebar.go's own doc comment).
type toolWindow struct {
	*tview.Box
	root     *Root
	id       string // this window's own Pages name (see Root.openToolCommand)
	titleBar *tview.TextView
	content  *tview.TextView

	cancel context.CancelFunc // stops the running process (see Root.openToolCommand) — called by close
	closed bool               // guards a late QueueUpdateDraw callback against writing into a torn-down window

	dragging                 bool // the title bar was pressed and the button is still down
	dragOffsetX, dragOffsetY int  // click position minus the window's own x/y at drag-start — kept constant for the rest of the drag
}

// newToolWindow builds one closed-over toolWindow, titled title — a
// plain, borderless Box (deliberately: every other overlay in this
// codebase — Properties, Chmod, Search, Help, Details — is borderless
// too, just padded text; a real box-drawn frame here would have been
// the only one in the whole app) topped with a one-row, solid-colored
// title bar (both for display and as the one draggable region — see
// MouseHandler) above a plain scrollable TextView for the command's own
// output.
func newToolWindow(root *Root, id, title string) *toolWindow {
	tw := &toolWindow{Box: tview.NewBox(), root: root, id: id}

	tw.titleBar = tview.NewTextView()
	tw.titleBar.SetWrap(false)
	tw.titleBar.SetText(" " + title + " ")
	tw.titleBar.SetBackgroundColor(root.theme.AccentBackground)
	// The colored bar itself is what shows which window currently has
	// real keyboard focus, per the user's own explicit request:
	// EditableBackground while focused — a lighter "pop" against the
	// content area, which is AccentBackground itself now (see below) —
	// the same AccentBackground the content already has while it isn't,
	// so an unfocused window reads as one seamless block. The same
	// two-state scheme Details' own title bar uses (see
	// detailssidebar.go's newDetailsTitleBar).
	tw.SetFocusFunc(func() { tw.titleBar.SetBackgroundColor(root.theme.EditableBackground) })
	tw.SetBlurFunc(func() { tw.titleBar.SetBackgroundColor(root.theme.AccentBackground) })

	tw.content = tview.NewTextView()
	tw.content.SetDynamicColors(true) // needed for appendStatus's own style tags
	tw.content.SetWrap(false)         // command output is line-oriented; wrapping would misalign it
	tw.content.SetScrollable(true)
	// A plain default background (i.e. none set) would blend straight
	// into whatever's behind it — the same dark background every other
	// overlay in this app already sits on — and no longer read as a
	// distinct floating window at all past its own one-row title bar.
	// AccentBackground is now this app's own shared "normal panel
	// background" tone, per the user's own explicit request — every
	// panel that floats over the main one (Properties, the context
	// menu, the Bash Prompt Editor, Details, every tool window) shares
	// this one color for its own content area; EditableBackground moved
	// to the focused-title-bar role instead (see above).
	tw.content.SetBackgroundColor(root.theme.AccentBackground)

	return tw
}

// Draw draws the title bar as row 0 of this window's own rect (see
// newToolWindow), then the content TextView across whatever rows are
// left below it — no border, no inner-rect arithmetic (see
// newToolWindow's own doc comment on why there's no border to account
// for here at all).
func (tw *toolWindow) Draw(screen tcell.Screen) {
	tw.DrawForSubclass(screen, tw)
	x, y, width, height := tw.GetRect()
	if height <= 0 {
		return
	}

	tw.titleBar.SetRect(x, y, width, 1)
	tw.titleBar.Draw(screen)

	contentHeight := height - 1
	if contentHeight < 0 {
		contentHeight = 0
	}
	tw.content.SetRect(x, y+1, width, contentHeight)
	tw.content.Draw(screen)
}

// MouseHandler drives the mouse side of moving this window: a press on
// the title bar (row 0 of this window's own rect — see newToolWindow)
// starts a drag; every subsequent move with the left button still held (checked
// via event.Buttons() — verified directly against tview's own
// Application.fireMouseActions/MouseMove handling, not guessed: it fires
// on every position change and always carries the button state current
// as of that exact event) repositions the window by the same offset the
// drag started with, so the window doesn't jump to have its corner
// snap to the cursor. Anything else within the window's own rect — the
// content area, or the title bar outside of an active drag — is
// forwarded to the content TextView, and the click is consumed
// regardless of whether content itself did anything with it: visually
// this window fully occludes whatever's beneath it, so nothing should
// ever click through it to the panel below.
func (tw *toolWindow) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
	return tw.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
		x, y := event.Position()
		wx, wy, width, height := tw.GetRect()

		if tw.dragging {
			switch action {
			case tview.MouseMove:
				if event.Buttons()&tcell.ButtonPrimary == 0 {
					// The button came up somewhere this window's own
					// MouseLeftUp case below never saw (e.g. released
					// outside the terminal) — stop rather than follow
					// the cursor forever.
					tw.dragging = false
					return true, nil
				}
				tw.moveTo(x-tw.dragOffsetX, y-tw.dragOffsetY, width, height)
				return true, nil
			case tview.MouseLeftUp:
				tw.dragging = false
				return true, nil
			}
			return true, nil // swallow everything else for the duration of the drag
		}

		if !tw.InRect(x, y) {
			return false, nil
		}

		if action == tview.MouseLeftDown {
			setFocus(tw)
			if y == wy { // the title bar itself
				tw.dragging = true
				tw.dragOffsetX, tw.dragOffsetY = x-wx, y-wy
				return true, nil
			}
		}

		if handler := tw.content.MouseHandler(); handler != nil {
			handler(action, event, setFocus)
		}
		return true, nil
	})
}

// moveTo repositions the window to (x, y), clamped to the whole screen
// (clampToScreen, not clampToPanel — a floating window is meant to be
// draggable anywhere, not confined to one panel the way a centered
// dialog is; see clampToScreen's own doc comment).
func (tw *toolWindow) moveTo(x, y, width, height int) {
	x, y, width, height = tw.root.clampToScreen(x, y, width, height)
	tw.SetRect(x, y, width, height)
}

// InputHandler drives the keyboard side of moving this window
// (Alt+arrow keys, per the user's own explicit request — checked ahead
// of everything else so it can never be shadowed by whatever the
// content TextView's own InputHandler would otherwise do with a plain
// arrow key), plus Escape to close it. Everything else — plain arrows,
// Page Up/Down, Home/End, mouse wheel already handled via MouseHandler —
// falls through to the content TextView's own InputHandler, so
// scrolling a long-running command's output works exactly like it does
// in Look/Help without this needing its own scroll logic.
func (tw *toolWindow) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return tw.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		if event.Modifiers()&tcell.ModAlt != 0 {
			x, y, width, height := tw.GetRect()
			switch event.Key() {
			case tcell.KeyLeft:
				tw.moveTo(x-1, y, width, height)
				return
			case tcell.KeyRight:
				tw.moveTo(x+1, y, width, height)
				return
			case tcell.KeyUp:
				tw.moveTo(x, y-1, width, height)
				return
			case tcell.KeyDown:
				tw.moveTo(x, y+1, width, height)
				return
			}
		}

		if event.Key() == tcell.KeyEscape {
			tw.close()
			return
		}

		if handler := tw.content.InputHandler(); handler != nil {
			handler(event, setFocus)
		}
	})
}

// appendLine appends one line of the running command's own real output —
// always called from inside a Root.app.QueueUpdateDraw closure (see
// openToolCommand), so this itself needs no locking of its own.
// tview.Escape matters here the same way it does for the Look/Help
// TextViews (see viewer.go's own doc comment on newViewerView): with
// SetDynamicColors on, an unescaped "[" in a command's real output
// (plenty of tools print literal brackets) would otherwise be misread as
// one of tview's own style tags instead of shown as-is.
func (tw *toolWindow) appendLine(line string) {
	if tw.closed {
		return
	}
	_, _ = fmt.Fprintln(tw.content, tview.Escape(line)) // a TextView's own Write never fails
}

// appendStatus appends one of this window's own status lines (process
// started/stopped/failed) — unlike appendLine, deliberately NOT escaped:
// these are our own strings carrying our own style tags (dimTag), never
// a command's real output, so there's nothing here that needs protecting
// against being misread as a tag.
func (tw *toolWindow) appendStatus(s string) {
	if tw.closed {
		return
	}
	_, _ = fmt.Fprintln(tw.content, s) // a TextView's own Write never fails
}

// close stops the running process, if it's still running (via cancel —
// see openToolCommand's own exec.CommandContext), and removes this
// window from the screen. Safe to call more than once (Escape and a
// process exiting on its own could both race to call it) — the closed
// guard makes every call after the first a no-op.
func (tw *toolWindow) close() {
	if tw.closed {
		return
	}
	tw.closed = true
	tw.cancel()
	tw.root.RemovePage(tw.id)
	for i, w := range tw.root.toolWindows {
		if w == tw {
			tw.root.toolWindows = append(tw.root.toolWindows[:i], tw.root.toolWindows[i+1:]...)
			break
		}
	}
}

// toolWindowDefaultWidth/Height is a fixed starting size for every new
// tool window — resizing isn't part of this first slice (see
// openToolCommand), only moving.
const toolWindowDefaultWidth, toolWindowDefaultHeight = 60, 16

// openToolCommand starts name(args...) and shows its combined
// stdout+stderr, live, in a new draggable toolWindow titled title — the
// general-purpose launcher every Toolbox entry that doesn't finish
// instantly is meant to go through eventually (this session's first is
// Ping — see openPingTestWindow). The process runs for as long as the
// window stays open: closing it (Escape, or the process ending on its
// own) cancels ctx, which — via exec.CommandContext — kills the process
// if it's still running. This is the same context.WithCancel +
// QueueUpdateDraw pairing computeHashes already uses in properties.go,
// just for a genuinely long-running, line-streaming process instead of
// one bounded computation reporting progress.
//
// Output is captured through a manual io.Pipe rather than
// cmd.StdoutPipe() so stdout and stderr both land in the same
// line-ordered stream (many of these tools — ping included, when a host
// doesn't resolve — report the one thing worth seeing on stderr, not
// stdout); cmd.StdoutPipe alone would silently drop that.
func (r *Root) openToolCommand(title, name string, args []string) *toolWindow {
	r.toolWindowSeq++
	id := fmt.Sprintf("toolwindow-%d", r.toolWindowSeq)

	ctx, cancel := context.WithCancel(context.Background())
	tw := newToolWindow(r, id, title)
	tw.cancel = cancel

	pr, pw := io.Pipe()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		_ = pw.Close() // io.PipeWriter.Close never fails
		cancel()
		r.showError(fmt.Errorf("%s: %w", name, err))
		return nil
	}
	r.toolWindows = append(r.toolWindows, tw)

	go func() {
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			line := scanner.Text()
			r.app.QueueUpdateDraw(func() { tw.appendLine(line) })
		}
	}()

	go func() {
		waitErr := cmd.Wait()
		_ = pw.Close() // unblocks the scanning goroutine's Read with EOF; io.PipeWriter.Close never fails
		r.app.QueueUpdateDraw(func() {
			if tw.closed {
				return
			}
			switch {
			case ctx.Err() != nil:
				tw.appendStatus(dimTag + "— stopped —" + "[-:-:-]")
			case waitErr != nil:
				tw.appendStatus(fmt.Sprintf("%s— exited: %s —%s", dimTag, waitErr, "[-:-:-]"))
			default:
				tw.appendStatus(dimTag + "— finished —" + "[-:-:-]")
			}
		})
	}()

	x, y := r.nextToolWindowPosition()
	x, y, width, height := r.clampToScreen(x, y, toolWindowDefaultWidth, toolWindowDefaultHeight)
	tw.SetRect(x, y, width, height)

	r.AddPage(id, tw, false, true)
	r.SendToFront(id)
	r.app.SetFocus(tw)

	return tw
}

// nextToolWindowPosition returns a cascading spawn point for a new tool
// window — each successive one offset a little further down-right of
// the last (wrapping back after a few, rather than eventually spawning
// off-screen), so opening several at once (a ping and a tail -f side by
// side) don't land exactly on top of each other before the user has had
// a chance to drag any apart.
func (r *Root) nextToolWindowPosition() (x, y int) {
	const step, wrap = 3, 6
	n := len(r.toolWindows) % wrap
	return 4 + n*step, 2 + n*step
}

// openPingTestWindow is this first toolWindow slice's own proof of
// concept: asks for a host via the existing generic prompt overlay (see
// openPrompt), then runs a plain, unbounded "ping <host>" — deliberately
// not "ping -c N": running until explicitly stopped is exactly the case
// a movable, non-modal window is for, and it doubles as this feature's
// own test of closing a still-running process cleanly. A placeholder
// entry point, not the planned feature itself — see openToolCommand's
// own doc comment and feature_ideas.txt for the real Toolbox this is a
// first step towards.
func (r *Root) openPingTestWindow() {
	r.openPrompt("Ping host:", "", func(host string) {
		if host == "" {
			return
		}
		r.openToolCommand("ping "+host, "ping", []string{host})
	})
}
