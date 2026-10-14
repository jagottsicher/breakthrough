package ui

import (
	"context"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// verticalScrollbar is a custom, 1-column-wide vertical scrollbar for a
// scrollable *tview.TextView — tview itself has no scrollbar support at
// all (checked directly against its own textview.go, not guessed), so
// this is fully custom. Pilot for Look's own built-in pager only (see
// root.go/viewerLayout), per the user's own explicit request to see one
// working before deciding whether to generalize it to any other
// scrollable widget in this app — deliberately not a shared, reusable
// component yet.
//
// Layout: a track (with the proportional thumb drawn over part of it)
// filling everything above the bottom two rows, where both arrow
// buttons sit together — classic Mac OS style, per the user's own
// explicit request, not one arrow at each end. The thumb is always one
// shade darker than the track's own background (scrollbarThumbColor),
// not a fixed palette color, so it still fits whatever color scheme is
// active.
type verticalScrollbar struct {
	*tview.Box
	root    *Root
	content *tview.TextView

	// wrappedLineCount cache — tview's own GetWrappedLineCount doc
	// comment calls it "an even more expensive call" than counting
	// unwrapped lines, since it has to parse the whole text and
	// allocate a line index. Recomputing it on every Draw (called on
	// every redraw, i.e. on every keypress or mouse move) would be
	// wasteful; cachedWidth/cachedTextLen pin it to the last actual
	// change in whatever it depends on.
	cachedWidth   int
	cachedTextLen int
	cachedWrapped int

	dragging        bool
	dragStartY      int
	dragStartOffset int
	dragSpan        int
	dragMaxOffset   int

	repeatCancel context.CancelFunc
}

// scrollbarThumbDarkenFactor scales the track's own background down,
// per channel, to get the thumb's color — "one shade darker", per the
// user's own explicit request, not a separately themed token: the track
// already uses whatever background the surrounding widget uses, so the
// thumb derives from that directly and stays correct under any color
// scheme without its own override field.
const scrollbarThumbDarkenFactor = 0.6

// scrollbarRepeatDelay/scrollbarRepeatInterval govern press-and-hold
// auto-repeat on the arrow buttons and the page-jump track: a single
// step on the initial click, then — only while the mouse button stays
// down, per the user's own explicit request — an initial pause before
// repeating, and a steady, faster cadence after that, the same shape
// most GUI scrollbars already use.
const (
	scrollbarRepeatDelay    = 400 * time.Millisecond
	scrollbarRepeatInterval = 80 * time.Millisecond
)

func scrollbarThumbColor(track tcell.Color) tcell.Color {
	r, g, b := track.RGB()
	scale := func(v int32) int32 {
		return int32(float64(v) * scrollbarThumbDarkenFactor)
	}
	return tcell.NewRGBColor(scale(r), scale(g), scale(b))
}

func newVerticalScrollbar(root *Root, content *tview.TextView) *verticalScrollbar {
	return &verticalScrollbar{Box: tview.NewBox(), root: root, content: content}
}

// wrappedLineCount is content's own total row count, wrapping included
// — see the cache fields' own doc comment on why this isn't just
// content.GetWrappedLineCount() directly.
func (sb *verticalScrollbar) wrappedLineCount() int {
	_, _, width, _ := sb.content.GetRect()
	text := sb.content.GetText(false)
	if width == sb.cachedWidth && len(text) == sb.cachedTextLen {
		return sb.cachedWrapped
	}
	sb.cachedWidth = width
	sb.cachedTextLen = len(text)
	sb.cachedWrapped = sb.content.GetWrappedLineCount()
	return sb.cachedWrapped
}

// viewportRows is how many rows of content are visible at once —
// content's own rect height directly, since newViewerView's own
// SetBorderPadding(0, 0, 1, 1) only pads left/right, never top/bottom.
func (sb *verticalScrollbar) viewportRows() int {
	_, _, _, height := sb.content.GetRect()
	return height
}

// maxOffset is the highest row content can be scrolled to — 0 once
// everything already fits inside the viewport.
func (sb *verticalScrollbar) maxOffset() int {
	m := sb.wrappedLineCount() - sb.viewportRows()
	if m < 0 {
		return 0
	}
	return m
}

func (sb *verticalScrollbar) clampOffset(v int) int {
	if v < 0 {
		return 0
	}
	if m := sb.maxOffset(); v > m {
		return m
	}
	return v
}

// thumbGeometry returns the thumb's own height and its row offset
// within trackRows (both in track-relative rows, 0-based) — shared by
// Draw and every mouse-driven move so the visual thumb and its hit
// testing never disagree. thumbRows floors at 1 row (per the user's
// own explicit request: below that, the same single row simply
// represents a varying share of the total, rather than vanishing).
func (sb *verticalScrollbar) thumbGeometry(trackRows int) (thumbRows, thumbStart int) {
	if trackRows <= 0 {
		return 0, 0
	}
	total := sb.wrappedLineCount()
	viewport := sb.viewportRows()
	if total <= viewport || total <= 0 {
		return trackRows, 0
	}
	thumbRows = trackRows * viewport / total
	if thumbRows < 1 {
		thumbRows = 1
	}
	if thumbRows > trackRows {
		thumbRows = trackRows
	}
	span := trackRows - thumbRows
	maxOffset := total - viewport
	if span > 0 && maxOffset > 0 {
		offsetRow, _ := sb.content.GetScrollOffset()
		thumbStart = offsetRow * span / maxOffset
	}
	return thumbRows, thumbStart
}

func (sb *verticalScrollbar) scrollBy(delta int) {
	row, col := sb.content.GetScrollOffset()
	sb.content.ScrollTo(sb.clampOffset(row+delta), col)
}

func (sb *verticalScrollbar) pageJump(dir int) {
	sb.scrollBy(dir * sb.viewportRows())
}

// dragTo moves the thumb so the point originally grabbed stays under
// the cursor, matching mouseY's own movement since the drag started
// (dragStartY/dragStartOffset/dragSpan/dragMaxOffset are all snapshot
// once, in captureViewerScrollbarMouse, at MouseLeftDown) rather than
// snapping the thumb's top edge to the cursor on every move.
func (sb *verticalScrollbar) dragTo(mouseY int) {
	if sb.dragSpan <= 0 {
		return
	}
	deltaRows := (mouseY - sb.dragStartY) * sb.dragMaxOffset / sb.dragSpan
	_, col := sb.content.GetScrollOffset()
	sb.content.ScrollTo(sb.clampOffset(sb.dragStartOffset+deltaRows), col)
}

// startRepeat begins step's own press-and-hold auto-repeat — an
// initial scrollbarRepeatDelay pause, then a step every
// scrollbarRepeatInterval, until stopRepeat cancels it. tcell delivers
// a mouse-down event but no further events while the button stays
// still held down, so "repeat while held" has to come from a timer, not
// from more mouse events — each step is marshalled back onto the UI
// goroutine via QueueUpdateDraw, never called directly from this
// goroutine, per this app's own async/UI-update rules.
func (sb *verticalScrollbar) startRepeat(step func()) {
	sb.stopRepeat()
	ctx, cancel := context.WithCancel(context.Background())
	sb.repeatCancel = cancel
	go func() {
		timer := time.NewTimer(scrollbarRepeatDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		ticker := time.NewTicker(scrollbarRepeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sb.root.app.QueueUpdateDraw(step)
			}
		}
	}()
}

// stopRepeat cancels any in-flight startRepeat goroutine — called on
// MouseLeftUp/a stray button release, and from hideOverlay when Look
// closes outright (see root.go) so a held-down arrow/page-jump never
// keeps scrolling a TextView nothing shows any more. A nil receiver or
// a repeat that was never started are both already a no-op.
func (sb *verticalScrollbar) stopRepeat() {
	if sb == nil || sb.repeatCancel == nil {
		return
	}
	sb.repeatCancel()
	sb.repeatCancel = nil
}

// Draw paints the arrow rows, the track, and the thumb directly onto
// screen — there's nothing here tview itself can draw for us (see this
// type's own doc comment), so every cell in this 1-column strip is
// painted by hand.
func (sb *verticalScrollbar) Draw(screen tcell.Screen) {
	sb.Box.DrawForSubclass(screen, sb)
	x, y, _, height := sb.GetInnerRect()
	if height < 3 {
		return
	}

	theme := sb.root.theme
	track := theme.PanelBackground
	thumb := scrollbarThumbColor(track)

	setCell := func(row int, bg tcell.Color, ch rune) {
		screen.SetContent(x, row, ch, nil, tcell.StyleDefault.Background(bg).Foreground(theme.TextColor))
	}

	// Both arrows sit together at the track's own bottom end, per the
	// user's own explicit request (classic Mac OS style) — not one at
	// each end. The track itself is everything above them.
	trackRows := height - 2
	thumbRows, thumbStart := sb.thumbGeometry(trackRows)
	for i := 0; i < trackRows; i++ {
		row := y + i
		if i >= thumbStart && i < thumbStart+thumbRows {
			setCell(row, thumb, ' ')
		} else {
			setCell(row, track, ' ')
		}
	}
	setCell(y+height-2, thumb, '▲')
	setCell(y+height-1, thumb, '▼')
}

// captureViewerScrollbarMouse is viewerLayout's own SetMouseCapture
// hook (see root.go) — the same "intercept before normal position-based
// dispatch, do the hit testing by hand" shape dragSearchMouseCapture
// already uses for Search's own draggable title bar (see search.go),
// needed here for the same reason: a drag in progress has to keep
// tracking the mouse even once it strays outside this scrollbar's own
// 1-column rect, which plain position-gated dispatch wouldn't allow.
func (r *Root) captureViewerScrollbarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	sb := r.viewerScrollbar
	x, y := event.Position()

	if sb.dragging {
		switch action {
		case tview.MouseMove:
			if event.Buttons()&tcell.ButtonPrimary == 0 {
				sb.dragging = false
				return tview.MouseConsumed, nil
			}
			sb.dragTo(y)
			return tview.MouseConsumed, nil
		case tview.MouseLeftUp:
			sb.dragging = false
			return tview.MouseConsumed, nil
		}
		return tview.MouseConsumed, nil // swallow everything else for the duration of the drag
	}

	if sb.repeatCancel != nil {
		switch action {
		case tview.MouseMove:
			if event.Buttons()&tcell.ButtonPrimary == 0 {
				sb.stopRepeat()
				return tview.MouseConsumed, nil
			}
		case tview.MouseLeftUp:
			sb.stopRepeat()
		}
		return tview.MouseConsumed, nil // swallow everything else for the duration of the repeat
	}

	if !sb.InRect(x, y) {
		return action, event
	}

	_, sby, _, sbh := sb.GetRect()
	rel := y - sby
	trackRows := sbh - 2

	if action == tview.MouseScrollUp {
		sb.scrollBy(-1)
		return tview.MouseConsumed, nil
	}
	if action == tview.MouseScrollDown {
		sb.scrollBy(1)
		return tview.MouseConsumed, nil
	}
	if action != tview.MouseLeftDown {
		// Anything else landing on the scrollbar's own column is still
		// ours, not whatever happens to be drawn underneath it — in
		// particular tview derives a trailing MouseLeftClick from the
		// very same physical click that already fired MouseLeftDown
		// above (see Application.fireMouseActions), and Look is
		// maximized to the full screen (see viewerSize's own doc
		// comment), so without this, that second action falls through
		// Pages' own dispatch to whatever's drawn at the same screen
		// cell underneath — a real, user-reported bug: a click on the
		// scrollbar's own column triggered the main panel's Details
		// slide-out button (see panel.go/detailsExpandBtn), which sits
		// at that exact coordinate on the page stacked right behind it.
		return tview.MouseConsumed, nil
	}

	switch {
	case rel == sbh-2: // up arrow — both arrows sit together at the bottom, see Draw
		sb.scrollBy(-1)
		sb.startRepeat(func() { sb.scrollBy(-1) })
	case rel == sbh-1: // down arrow
		sb.scrollBy(1)
		sb.startRepeat(func() { sb.scrollBy(1) })
	default:
		thumbRows, thumbStart := sb.thumbGeometry(trackRows)
		trackRel := rel // the track now starts at the scrollbar's own top row (rel 0), no top arrow to offset past
		switch {
		case trackRel < thumbStart:
			sb.pageJump(-1)
			sb.startRepeat(func() { sb.pageJump(-1) })
		case trackRel >= thumbStart+thumbRows:
			sb.pageJump(1)
			sb.startRepeat(func() { sb.pageJump(1) })
		default:
			sb.dragging = true
			sb.dragStartY = y
			sb.dragStartOffset, _ = sb.content.GetScrollOffset()
			sb.dragSpan = trackRows - thumbRows
			sb.dragMaxOffset = sb.maxOffset()
		}
	}
	return tview.MouseConsumed, nil
}
