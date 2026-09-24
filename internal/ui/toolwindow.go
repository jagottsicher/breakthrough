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
// every entry in the Toolbox screen (see toolbox.go/openToolCommand),
// networking and hardware tools alike, whether they finish instantly
// (lsblk) or run until stopped (ping, tail -f). Unlike every other
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

	cancel context.CancelFunc // stops the running process (see Root.openToolCommand) — called by close/reload
	closed bool               // guards a late QueueUpdateDraw callback against writing into a torn-down window

	// name/args are exactly what Root.openToolCommand originally started
	// this window with — remembered so reload can run the identical
	// command again, rather than needing openToolCommand's own caller
	// (toolbox.go) to somehow be reached a second time.
	name string
	args []string

	// generation is bumped by Root.startToolCommand every time this
	// window's own command (re)starts — reload's own real point. Each
	// run's own output-scanning/process-wait goroutines close over the
	// generation they were started with, and every QueueUpdateDraw
	// callback they queue checks it against tw.generation before
	// touching content at all: reload cancels whatever's still running
	// first, but that old process's own remaining buffered output (and
	// its own final status line once cmd.Wait finally unblocks) can
	// keep arriving asynchronously, on its own schedule, for a little
	// while after — this is what keeps a stale run's own trailing
	// output/status from a moment ago corrupting the fresh run's
	// content, which starts from a freshly cleared window (see reload).
	generation int

	dragging                 bool // the title bar was pressed and the button is still down
	dragOffsetX, dragOffsetY int  // click position minus the window's own x/y at drag-start — kept constant for the rest of the drag

	// resizing/resizeStart* are the drag-handle's own equivalent of
	// dragging/dragOffsetX/Y above — see MouseHandler's own doc comment.
	// The start position and size (not an incremental delta) are what's
	// kept constant for the rest of the drag, the same reason dragOffsetX/Y
	// are: recomputing the target size fresh from a fixed baseline on
	// every move event, rather than accumulating from whatever the
	// previous (possibly already-clamped) size was, avoids drift once
	// resizeTo's own minimum/screen-edge clamping starts kicking in.
	resizing                             bool
	resizeStartMouseX, resizeStartMouseY int
	resizeStartWidth, resizeStartHeight  int

	// manuallyResized is set the moment the resize handle is ever
	// dragged, and never cleared again — per the user's own explicit
	// request that the resize handle actually let them pick a size,
	// this permanently opts the window out of recalculateWidth's own
	// automatic width-fit (see its own doc comment): the two would
	// otherwise fight each other, since the very next line of output
	// would immediately snap a manually-widened window back down (or a
	// manually-narrowed one back up) to whatever the auto-fit logic
	// alone would have chosen.
	manuallyResized bool

	// lineWidths is appendLine/appendStatus's own display-width tally,
	// one entry per line ever written to content, in the same order —
	// what recalculateWidth looks at to decide how wide this window
	// should be right now (see its own doc comment).
	lineWidths []int

	// hasContent tracks whether writeContentLine has ever written
	// anything yet, so it knows whether the next line needs a leading
	// newline before it — see writeContentLine's own doc comment.
	hasContent bool
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
	tw.titleBar.SetBackgroundColor(root.theme.InputBackground)
	// The colored bar itself is what shows which window currently has
	// real keyboard focus, per the user's own explicit request:
	// FocusedBackground (a dark cyan/"petrol" tone) while focused,
	// EditableBackground (the lighter slate gray) while it isn't — the
	// same two-state scheme Details' own title bar uses (see
	// detailssidebar.go's newDetailsTitleBar).
	tw.SetFocusFunc(func() { tw.titleBar.SetBackgroundColor(root.theme.InputFocusedBackground) })
	tw.SetBlurFunc(func() { tw.titleBar.SetBackgroundColor(root.theme.InputBackground) })

	tw.content = tview.NewTextView()
	tw.content.SetDynamicColors(true) // needed for appendStatus's own style tags
	tw.content.SetWrap(false)         // command output is line-oriented; wrapping would misalign it
	tw.content.SetScrollable(true)
	// One blank column of padding on each side, the same
	// SetBorderPadding(0, 0, 1, 1) virtually every other overlay's own
	// content area in this app already has (Properties, Chmod, the
	// context menu, Search, ...), per the user's own explicit request
	// that this window match that same convention. recalculateWidth's
	// own width-fit accounts for these two extra columns so a line
	// still isn't clipped just because of them.
	tw.content.SetBorderPadding(0, 0, toolWindowContentPadding, toolWindowContentPadding)
	// A TextView's own "track the end" flag starts false even though
	// it's scrollable — verified directly against tview's own
	// textview.go, not guessed: NewTextView never sets it, so newly
	// written lines just pile up past the bottom of whatever's currently
	// visible (starting at the very top) instead of following along.
	// ScrollToEnd flips it on from the start, so this window opens
	// already tracking its own process's newest output — appendLine/
	// appendStatus's later writes then keep it pinned there as they
	// arrive, scrolling older lines off the top, per the user's own
	// explicit request. A manual scroll (arrow keys, the mouse wheel,
	// dragging the scrollbar) turns tracking back off on its own — tview
	// treats that as "the user wants to read history now" — so this
	// doesn't fight anyone who deliberately scrolls up to look back.
	tw.content.ScrollToEnd()
	// A plain default background (i.e. none set) would blend straight
	// into whatever's behind it — the same dark background every other
	// overlay in this app already sits on — and no longer read as a
	// distinct floating window at all past its own one-row title bar.
	// AccentBackground is this app's own shared "normal panel background"
	// tone, per the user's own explicit request — every panel that
	// floats over the main one (Properties, the context menu, the Bash
	// Prompt Editor, Details, every tool window) shares this one color
	// for its own content area, independent of the title bar's own
	// separate focus-dependent EditableBackground/FocusedBackground pair
	// (see above).
	tw.content.SetBackgroundColor(root.theme.SurfaceBackground)

	return tw
}

// toolWindowCloseGlyph/toolWindowResizeGlyph are drawn directly onto
// the screen (see Draw), not appended to titleBar's own text: a plain
// Unicode glyph, deliberately not the letter "X", per the user's own
// explicit request. '✕' (MULTIPLICATION X, U+2715) rather than '⛌'
// (CROSSING LANES, U+26CC, tried first): the user's own terminal
// rendered CROSSING LANES as a double-width glyph, eating into
// toolWindowCloseButtonCol's own one-column gap even though
// TaggedStringWidth (and every width calculation here) still counts it
// as a single column — MULTIPLICATION X is confirmed single-width by
// hand, via the exact same live check, so it's the safe choice here,
// not just the first one tried. '◢' is a filled lower-right triangle,
// the same shape most GUI apps already use for a resize grip.
const (
	toolWindowCloseGlyph  = '✕'
	toolWindowResizeGlyph = '◢'

	// toolWindowReloadGlyph reuses the exact same glyph the header's own
	// path-bar Reload button already draws (see panel.go's own
	// headerButtons) — already confirmed single-width in this app's own
	// terminal environment (unlike an earlier close-glyph candidate, see
	// toolWindowCloseGlyph's own doc comment on that), and reusing it
	// keeps "reload" reading as the same action everywhere it appears in
	// this app rather than introducing a second icon for the same idea.
	toolWindowReloadGlyph = '⭯'
)

// toolWindowCloseButtonCol returns the close glyph's own column within
// a window width columns wide: one column in from the right edge, not
// the edge itself, per the user's own explicit request for a full
// character of breathing room between the glyph and the window's own
// border — see minWidth's own doc comment for the matching floor this
// leaves on how narrow the window can ever get.
func toolWindowCloseButtonCol(x, width int) int {
	return x + width - 2
}

// toolWindowReloadButtonCol returns the reload glyph's own column,
// immediately to the close glyph's own left with one blank column of
// breathing room between them — the same one-column gap the close
// glyph itself already keeps from the window's own right edge (see
// toolWindowCloseButtonCol), per the user's own explicit request for a
// reload button "links vom x".
func toolWindowReloadButtonCol(x, width int) int {
	return toolWindowCloseButtonCol(x, width) - 2
}

// Draw draws the title bar as row 0 of this window's own rect (see
// newToolWindow), the close button glyph one column in from its own
// top-right corner, the content TextView across the rows in between,
// and finally the resize-handle footer as the very last row — no
// border, no inner-rect arithmetic otherwise (see newToolWindow's own
// doc comment on why there's no border to account for here at all).
func (tw *toolWindow) Draw(screen tcell.Screen) {
	tw.DrawForSubclass(screen, tw)
	x, y, width, height := tw.GetRect()
	if height <= 0 {
		return
	}

	tw.titleBar.SetRect(x, y, width, 1)
	tw.titleBar.Draw(screen)
	buttonStyle := tcell.StyleDefault.Background(tw.titleBar.GetBackgroundColor()).Foreground(tw.root.theme.Text)
	if closeCol := toolWindowCloseButtonCol(x, width); closeCol >= x {
		screen.SetContent(closeCol, y, toolWindowCloseGlyph, nil, buttonStyle)
	}
	if reloadCol := toolWindowReloadButtonCol(x, width); reloadCol >= x {
		screen.SetContent(reloadCol, y, toolWindowReloadGlyph, nil, buttonStyle)
	}

	tw.content.SetRect(x, y+1, width, tw.contentHeight())
	tw.content.Draw(screen)

	// The footer row — see contentHeight's own doc comment on why it's
	// excluded from content's own rect above — always stays empty
	// except for the resize handle in its own bottom-right corner, per
	// the user's own explicit request. Only drawn once there's actually
	// room for it distinct from the title row (height >= 2); painted
	// across its own full width first so it reads as part of the same
	// solid box as the title bar and content above it, not a gap
	// showing whatever's behind this window.
	if height >= 2 {
		footerY := y + height - 1
		footerStyle := tcell.StyleDefault.Background(tw.root.theme.AccentBackground).Foreground(tw.root.theme.Text)
		for col := x; col < x+width; col++ {
			screen.SetContent(col, footerY, ' ', nil, footerStyle)
		}
		if width > 0 {
			screen.SetContent(x+width-1, footerY, toolWindowResizeGlyph, nil, footerStyle)
		}
	}
}

// contentHeight returns how many rows are actually available for
// content: this window's own current height, minus the title bar (row
// 0) and the resize-handle footer row (always the very last row) —
// Draw's own layout above and recalculateWidth both need exactly this
// same number and must always agree, since recalculateWidth's whole
// point is to react to what's actually visible on screen.
func (tw *toolWindow) contentHeight() int {
	_, _, _, height := tw.GetRect()
	h := height - 2
	if h < 0 {
		h = 0
	}
	return h
}

// MouseHandler drives the mouse side of this window's own chrome: a
// press on the title bar (row 0 — see newToolWindow) starts a move-drag,
// except right on the close glyph itself (one column in from its own
// top-right corner — see toolWindowCloseButtonCol/Draw), which closes
// the window outright instead; a press on the resize
// handle (the bottom-right corner of the footer row — see contentHeight's
// own doc comment) starts a resize-drag. Every subsequent move with the
// left button still held (checked via event.Buttons() — verified
// directly against tview's own Application.fireMouseActions/MouseMove
// handling, not guessed: it fires on every position change and always
// carries the button state current as of that exact event) repositions
// or resizes the window relative to wherever the drag started, so
// neither the window's corner nor its size snaps straight to the
// cursor. Anything else within the window's own rect — the content
// area, the title bar outside of an active drag, or the otherwise-empty
// rest of the footer row — is forwarded to the content TextView (the
// footer row's own clicks just land outside its rect and do nothing),
// and the click is consumed regardless of whether content itself did
// anything with it: visually this window fully occludes whatever's
// beneath it, so nothing should ever click through it to the panel
// below.
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

		if tw.resizing {
			switch action {
			case tview.MouseMove:
				if event.Buttons()&tcell.ButtonPrimary == 0 {
					tw.resizing = false // same stray-release case dragging's own MouseMove case guards against
					return true, nil
				}
				newWidth := tw.resizeStartWidth + (x - tw.resizeStartMouseX)
				newHeight := tw.resizeStartHeight + (y - tw.resizeStartMouseY)
				tw.resizeTo(newWidth, newHeight)
				return true, nil
			case tview.MouseLeftUp:
				tw.resizing = false
				return true, nil
			}
			return true, nil // swallow everything else for the duration of the resize
		}

		if !tw.InRect(x, y) {
			return false, nil
		}

		if action == tview.MouseLeftDown {
			setFocus(tw)
			switch {
			case y == wy && x == toolWindowCloseButtonCol(wx, width): // the close glyph, one column in from the title bar's own top-right corner
				tw.close()
				return true, nil
			case y == wy && x == toolWindowReloadButtonCol(wx, width): // the reload glyph, immediately left of the close glyph
				tw.reload()
				return true, nil
			case y == wy: // the rest of the title bar
				tw.dragging = true
				tw.dragOffsetX, tw.dragOffsetY = x-wx, y-wy
				return true, nil
			case y == wy+height-1 && x == wx+width-1: // the resize handle, bottom-right of the footer row
				tw.resizing = true
				tw.resizeStartMouseX, tw.resizeStartMouseY = x, y
				tw.resizeStartWidth, tw.resizeStartHeight = width, height
				return true, nil
			case y == wy+height-1: // the rest of the footer row — always empty, per the user's own explicit request
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

// toolWindowMinHeight is the shortest this window can ever be resized
// to (see resizeTo) — the title bar, one row of actual content, and the
// resize-handle footer row itself, per the user's own explicit request.
const toolWindowMinHeight = 3

// minWidth is the narrowest this window can ever be resized to (see
// resizeTo) — its own title, one space, the reload button, one blank
// column, the close button, and one more space to its right (see
// toolWindowCloseButtonCol/toolWindowReloadButtonCol's own doc
// comments), per the user's own explicit request: any narrower and the
// two buttons would either overlap the title text, overlap each other,
// or sit flush against the window's own right edge.
func (tw *toolWindow) minWidth() int {
	return tview.TaggedStringWidth(tw.titleBar.GetText(false)) + 5 // +1 space, +1 reload glyph, +1 gap, +1 close glyph, +1 space
}

// resizeTo is the resize handle's own counterpart to moveTo: unlike a
// move, x and y themselves must never change here — only width and
// height, floored at minWidth/toolWindowMinHeight and capped so the
// window's own far edge never grows past the screen's, the same way
// clampToScreen already caps width/height for a window whose position
// would otherwise put it there (see moveTo), just without moveTo's own
// additional freedom to shift x/y to compensate — the corner being
// dragged is the only one allowed to move.
//
// Setting manuallyResized permanently opts this window out of
// recalculateWidth's own automatic width-fit from here on — see
// manuallyResized's own doc comment on the struct for why the two can't
// coexist.
func (tw *toolWindow) resizeTo(width, height int) {
	if minW := tw.minWidth(); width < minW {
		width = minW
	}
	if height < toolWindowMinHeight {
		height = toolWindowMinHeight
	}

	x, y, _, _ := tw.GetRect()
	if _, _, sw, sh := tw.root.GetRect(); sw > 0 && sh > 0 {
		if maxWidth := sw - x; width > maxWidth {
			width = maxWidth
		}
		if maxHeight := sh - y; height > maxHeight {
			height = maxHeight
		}
	}

	tw.manuallyResized = true
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
	escaped := tview.Escape(line)
	tw.writeContentLine(escaped)
	tw.lineWidths = append(tw.lineWidths, tview.TaggedStringWidth(escaped))
	tw.recalculateWidth()
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
	tw.writeContentLine(s)
	tw.lineWidths = append(tw.lineWidths, tview.TaggedStringWidth(s))
	tw.recalculateWidth()
}

// writeContentLine appends one line to content — appendLine/
// appendStatus's own shared tail — without ever leaving a trailing
// newline in the underlying text. A trailing newline after *every*
// line (fmt.Fprintln's own behavior, used here originally) is a real
// bug, found by hand and confirmed by rendering this window to a
// tcell.SimulationScreen at several heights, not guessed: a TextView's
// own trackEnd scrolling (see ScrollToEnd in newToolWindow) treats a
// trailing newline as introducing one more, empty trailing line of
// text, which then permanently occupies this window's own very last
// visible content row — every window, at every height, was showing
// exactly one real line fewer than its content height should allow,
// with a mystery blank row in its place; at the resize handle's own
// minimum height (a single content row), that one row was always
// empty, no matter how much real output there actually was to show.
// Writing a *leading* newline before every line except the first,
// instead of a trailing one after every line, keeps the underlying
// text exactly as many real lines as have actually been appended, with
// nothing dangling off the end.
func (tw *toolWindow) writeContentLine(s string) {
	if tw.hasContent {
		_, _ = fmt.Fprint(tw.content, "\n", s) // a TextView's own Write never fails
		return
	}
	_, _ = fmt.Fprint(tw.content, s)
	tw.hasContent = true
}

// recalculateWidth resizes this window to exactly fit what's actually
// on screen right now: its own title, and the widest of the visible
// content lines — the last contentHeight entries of lineWidths, since
// ScrollToEnd (see newToolWindow's own doc comment) always keeps the
// newest ones in view — never a line that has already scrolled out of
// view. Called after every appendLine/appendStatus, so the window
// grows the moment a longer line arrives and shrinks back again just
// as promptly once the visible lines get shorter, per the user's own
// explicit request: a long line from early in a long-running command's
// output shouldn't keep the window wide forever after it's scrolled
// away. toolWindowMinWidth is a floor for when both the title and the
// visible content are narrower than that. Each visible line's own
// width gets toolWindowContentPadding added back in on both sides
// (content's own left/right border padding — see newToolWindow), so
// that padding is never what ends up clipping a line that would
// otherwise fit exactly.
//
// Position and height are left untouched — this only ever changes
// width. Growing past the right edge of the screen is handled the same
// way dragging a window there already is, via moveTo's own
// clampToScreen call.
//
// A no-op once manuallyResized is set — see its own doc comment on the
// struct for why the resize handle permanently takes over from this
// once it's ever been used.
func (tw *toolWindow) recalculateWidth() {
	if tw.manuallyResized {
		return
	}

	x, y, _, height := tw.GetRect()
	visible := tw.lineWidths
	if contentHeight := tw.contentHeight(); len(visible) > contentHeight {
		visible = visible[len(visible)-contentHeight:]
	}

	width := tview.TaggedStringWidth(tw.titleBar.GetText(false))
	for _, w := range visible {
		if padded := w + 2*toolWindowContentPadding; padded > width {
			width = padded
		}
	}
	if width < toolWindowMinWidth {
		width = toolWindowMinWidth
	}

	tw.moveTo(x, y, width, height)
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

// reload is the title bar's own reload button (see
// toolWindowReloadButtonCol/MouseHandler) — per the user's own explicit
// request, refreshes/repeats whatever this window's own command was:
// stops it first if it's still running (harmless if it already
// finished — cancelling an already-done context is a no-op), clears
// this window's own content back to empty exactly the way it started
// out before this run's first line ever arrived, and starts tw.name/
// tw.args again from scratch. Position, size, and manuallyResized are
// all left completely alone — this only ever replaces the running
// command and what it's shown so far, never where or how big the
// window itself is.
func (tw *toolWindow) reload() {
	if tw.closed {
		return
	}
	tw.cancel()
	tw.content.Clear()
	tw.lineWidths = nil
	tw.hasContent = false
	tw.root.startToolCommand(tw)
	tw.recalculateWidth() // back to fitting just the title, the same as when this window was first opened
}

// toolWindowDefaultHeight is a fixed height for every tool window, for
// its whole lifetime — unlike width (see recalculateWidth), height
// isn't part of the user's own auto-fit request, so it's still exactly
// what it was before that: a plain fixed starting size, never resized
// afterward.
const toolWindowDefaultHeight = 16

// toolWindowMinWidth is the floor recalculateWidth never shrinks below,
// even once both the title and the visible content are narrower than
// this — enough room for a short title or a short output line to still
// read comfortably, rather than collapsing to an awkwardly thin sliver.
const toolWindowMinWidth = 24

// toolWindowContentPadding is content's own left/right border padding
// (see newToolWindow's SetBorderPadding call) — one blank column on
// each side, so recalculateWidth's own line-width comparisons know to
// add both back in: a line that's exactly window-width-wide once this
// padding is accounted for would otherwise get clipped by it.
const toolWindowContentPadding = 1

// openToolCommand starts name(args...) and shows its combined
// stdout+stderr, live, in a new draggable toolWindow titled title —
// the general-purpose launcher every Toolbox entry (see toolbox.go)
// runs through, whether or not it finishes instantly. The process runs
// for as long as the
// window stays open: closing it (Escape, or the process ending on its
// own) cancels ctx, which — via exec.CommandContext — kills the process
// if it's still running. This is the same context.WithCancel +
// QueueUpdateDraw pairing computeHashes already uses in properties.go,
// just for a genuinely long-running, line-streaming process instead of
// one bounded computation reporting progress. The window's own title
// bar also grew a reload button (see toolWindowReloadButtonCol/
// toolWindow.reload) for running the identical command again — whether
// to get a fresh snapshot from a one-shot tool or to restart one that's
// still running — per the user's own explicit request; the real
// process/goroutine setup below lives in startToolCommand so both this
// function and reload share exactly one copy of it.
//
// Output is captured through a manual io.Pipe rather than
// cmd.StdoutPipe() so stdout and stderr both land in the same
// line-ordered stream (many of these tools — ping included, when a host
// doesn't resolve — report the one thing worth seeing on stderr, not
// stdout); cmd.StdoutPipe alone would silently drop that.
func (r *Root) openToolCommand(title, name string, args []string) *toolWindow {
	r.toolWindowSeq++
	id := fmt.Sprintf("toolwindow-%d", r.toolWindowSeq)

	tw := newToolWindow(r, id, title)
	tw.name = name
	tw.args = args

	if !r.startToolCommand(tw) {
		return nil
	}

	// nextToolWindowPosition reads len(r.toolWindows) to decide where
	// this window spawns — computed (and this window's own rect set)
	// before appending tw to that slice, not after: appending first
	// would make even the very first window ever opened see its own
	// not-yet-positioned self already counted, landing it one cascade
	// step further down-right than nextToolWindowPosition's own doc
	// comment promises.
	x, y := r.nextToolWindowPosition()
	tw.SetRect(x, y, toolWindowMinWidth, toolWindowDefaultHeight)
	tw.recalculateWidth() // sizes it for real, off just the title until output starts arriving (see its own doc comment)

	r.toolWindows = append(r.toolWindows, tw)

	r.AddPage(id, tw, false, true)
	r.SendToFront(id)
	r.app.SetFocus(tw)

	return tw
}

// startToolCommand actually starts (or, via reload, restarts) tw's own
// tw.name(tw.args...) — split out from openToolCommand so reload can
// run through the exact same process/goroutine setup rather than a
// second, drifting copy of it. Returns false (having already shown the
// error) if the command failed to even start; openToolCommand's own
// caller relies on this to bail out before this window is ever shown at
// all.
//
// tw.generation is bumped here and captured by both goroutines below —
// see its own doc comment on the struct for why a reload needs this at
// all: cancelling the previous run's context (see reload) only asks the
// kernel to stop that process, it doesn't retroactively un-queue
// whatever that run's own goroutines already had in flight, so without
// this a stale line or status from a run that's being replaced could
// still land in the freshly cleared window a moment later.
func (r *Root) startToolCommand(tw *toolWindow) bool {
	tw.generation++
	gen := tw.generation

	ctx, cancel := context.WithCancel(context.Background())
	tw.cancel = cancel

	pr, pw := io.Pipe()
	cmd := exec.CommandContext(ctx, tw.name, tw.args...)
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		_ = pw.Close() // io.PipeWriter.Close never fails
		cancel()
		r.showError(fmt.Errorf("%s: %w", tw.name, err))
		return false
	}

	// Both wrapped in safeGo (see its own doc comment): a panic in
	// either one used to take the whole process down without even
	// restoring the terminal, since neither runs inside
	// tview.Application.Run's own call stack. No onPanic cleanup beyond
	// what safeGo already does on its own — a lost output line or a
	// missing final status line isn't itself a stuck "in progress"
	// state the way a hash/search/sed computation's is, so there's
	// nothing further here worth resetting.
	r.safeGo("tool window output", nil, func() {
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			line := scanner.Text()
			r.app.QueueUpdateDraw(func() {
				if tw.closed || tw.generation != gen {
					return
				}
				tw.appendLine(line)
			})
		}
	})

	r.safeGo("tool window process wait", nil, func() {
		waitErr := cmd.Wait()
		_ = pw.Close() // unblocks the scanning goroutine's Read with EOF; io.PipeWriter.Close never fails
		r.app.QueueUpdateDraw(func() {
			if tw.closed || tw.generation != gen {
				return
			}
			switch {
			case ctx.Err() != nil:
				tw.appendStatus(dimTag + "— stopped —" + "[-:-:-]")
			case waitErr != nil:
				tw.appendStatus(fmt.Sprintf("%s— exited: %s —%s", dimTag, waitErr, "[-:-:-]"))
			}
			// A clean exit (neither of the above) gets no status line at
			// all, per the user's own explicit report that "— finished —"
			// was superfluous there: the output itself already makes that
			// obvious for a one-shot tool (lsblk, ip addr, ...), and there
			// is nothing further coming either way.
		})
	})

	return true
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
