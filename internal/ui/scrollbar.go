package ui

import (
	"context"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// scrollableContent is what verticalScrollbar needs from whatever it's
// attached to: its own current rect, its own current scroll offset, a
// way to change it, and how many rows exist in total. textViewContent/
// tableContent below are the two adapters this app currently needs — a
// TextView scrolls by (possibly wrapped) line, a Table scrolls by row —
// so the rest of this file never needs to know or care which one it's
// talking to.
type scrollableContent interface {
	rect() (x, y, width, height int)
	scrollOffset() (row, col int)
	scrollTo(row, col int)
	totalRows() int
}

// textViewContent adapts a *tview.TextView. totalRows accounts for
// wrapping (if the view has SetWrap(true) — see newViewerView) and
// caches GetWrappedLineCount's own result: tview's own doc comment
// calls it "an even more expensive call" than counting unwrapped lines,
// since it has to parse the whole text and allocate a line index.
// Recomputing it on every Draw (called on every redraw, i.e. on every
// keypress or mouse move) would be wasteful; cachedWidth/cachedTextLen
// pin it to the last actual change in whatever it depends on.
type textViewContent struct {
	view *tview.TextView

	cachedWidth   int
	cachedTextLen int
	cachedTotal   int
}

func (c *textViewContent) rect() (int, int, int, int) { return c.view.GetRect() }
func (c *textViewContent) scrollOffset() (int, int)   { return c.view.GetScrollOffset() }
func (c *textViewContent) scrollTo(row, col int)      { c.view.ScrollTo(row, col) }

func (c *textViewContent) totalRows() int {
	_, _, width, _ := c.view.GetRect()
	text := c.view.GetText(false)
	if width == c.cachedWidth && len(text) == c.cachedTextLen {
		return c.cachedTotal
	}
	c.cachedWidth = width
	c.cachedTextLen = len(text)
	c.cachedTotal = c.view.GetWrappedLineCount()
	return c.cachedTotal
}

// tableContent adapts a *tview.Table — row-based, no wrapping and
// nothing expensive to cache (GetRowCount is a plain length check), so
// unlike textViewContent this one needs no state of its own at all.
type tableContent struct{ table *tview.Table }

func (c tableContent) rect() (int, int, int, int) { return c.table.GetRect() }
func (c tableContent) scrollOffset() (int, int)   { return c.table.GetOffset() }
func (c tableContent) scrollTo(row, col int)      { c.table.SetOffset(row, col) }
func (c tableContent) totalRows() int             { return c.table.GetRowCount() }

// verticalScrollbar is a custom, 1-column-wide vertical scrollbar —
// tview itself has no scrollbar support at all (checked directly
// against its own textview.go/table.go, not guessed), so this is fully
// custom. First built as a pilot for Look's own built-in pager only
// (per the user's own explicit request to see one working before
// deciding whether to generalize it), then confirmed working and
// extracted into this shared, content-agnostic form once the user
// asked for it on every scrollable widget in the app.
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
	content scrollableContent

	// trackColor is normally nil (Draw then falls back to
	// theme.PanelBackground, the background every plain catalog
	// screen's own body already rests on — see mountsLayout's own
	// comment). Git Breach is the one exception so far: its own boxes
	// brighten when focused (see styleGitBreachFocus), and tview's own
	// Box exposes no getter for "whatever background color I was last
	// told to use" — so rather than reading it back, SetTrackColorFunc
	// lets a screen like that hand the scrollbar the exact same
	// decision its own content already makes, instead of this column
	// staying a flat, mismatched tone while the table next to it lights
	// up.
	trackColor func() tcell.Color

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

// newVerticalScrollbar builds a scrollbar over content — newVerticalScrollbarForTextView/
// newVerticalScrollbarForTable below are the usual way to get one.
func newVerticalScrollbar(root *Root, content scrollableContent) *verticalScrollbar {
	return &verticalScrollbar{Box: tview.NewBox(), root: root, content: content}
}

func newVerticalScrollbarForTextView(root *Root, view *tview.TextView) *verticalScrollbar {
	return newVerticalScrollbar(root, &textViewContent{view: view})
}

func newVerticalScrollbarForTable(root *Root, table *tview.Table) *verticalScrollbar {
	return newVerticalScrollbar(root, tableContent{table: table})
}

// SetTrackColorFunc overrides the track's own background color — see
// the trackColor field's own doc comment for why a screen would need
// this at all. f is called fresh on every Draw, so it can reflect
// live, changing state (e.g. whether the content next to it currently
// has keyboard focus).
func (sb *verticalScrollbar) SetTrackColorFunc(f func() tcell.Color) {
	sb.trackColor = f
}

// viewportRows is how many rows of content are visible at once —
// content's own rect height directly (a TextView's own
// SetBorderPadding, where used, only ever pads left/right here, never
// top/bottom — see newViewerView).
func (sb *verticalScrollbar) viewportRows() int {
	_, _, _, height := sb.content.rect()
	return height
}

// maxOffset is the highest row content can be scrolled to — 0 once
// everything already fits inside the viewport.
func (sb *verticalScrollbar) maxOffset() int {
	m := sb.content.totalRows() - sb.viewportRows()
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
	total := sb.content.totalRows()
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
		offsetRow, _ := sb.content.scrollOffset()
		thumbStart = offsetRow * span / maxOffset
	}
	return thumbRows, thumbStart
}

func (sb *verticalScrollbar) scrollBy(delta int) {
	row, col := sb.content.scrollOffset()
	sb.content.scrollTo(sb.clampOffset(row+delta), col)
}

func (sb *verticalScrollbar) pageJump(dir int) {
	sb.scrollBy(dir * sb.viewportRows())
}

// dragTo moves the thumb so the point originally grabbed stays under
// the cursor, matching mouseY's own movement since the drag started
// (dragStartY/dragStartOffset/dragSpan/dragMaxOffset are all snapshot
// once, in captureMouse, at MouseLeftDown) rather than snapping the
// thumb's top edge to the cursor on every move.
func (sb *verticalScrollbar) dragTo(mouseY int) {
	if sb.dragSpan <= 0 {
		return
	}
	deltaRows := (mouseY - sb.dragStartY) * sb.dragMaxOffset / sb.dragSpan
	_, col := sb.content.scrollOffset()
	sb.content.scrollTo(sb.clampOffset(sb.dragStartOffset+deltaRows), col)
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
// MouseLeftUp/a stray button release, and from hideOverlay when an
// overlay closes outright (see root.go) so a held-down arrow/page-jump
// never keeps scrolling content nothing shows any more. A nil receiver
// or a repeat that was never started are both already a no-op.
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
	sb.DrawForSubclass(screen, sb)
	x, y, _, height := sb.GetInnerRect()
	if height < 3 {
		return
	}

	theme := sb.root.theme
	track := theme.PanelBackground
	if sb.trackColor != nil {
		track = sb.trackColor()
	}
	thumb := scrollbarThumbColor(track)

	setCell := func(row int, bg tcell.Color, ch rune) {
		screen.SetContent(x, row, ch, nil, tcell.StyleDefault.Background(bg).Foreground(theme.TextColor))
	}

	// Nothing to scroll: per the user's own explicit request, the
	// scrollbar only shows up once content is actually taller than the
	// box — blank track, no arrows, no thumb, rather than a permanently
	// visible but functionally dead control. The column itself stays
	// reserved (no reflow), it just reads as plain background.
	if sb.maxOffset() <= 0 {
		for i := 0; i < height; i++ {
			setCell(y+i, track, ' ')
		}
		return
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

// captureMouse is this scrollbar's own SetMouseCapture hook — install
// it on whatever Flex/Pages container holds both this scrollbar and its
// content (see root.go/viewerLayout for the shape), the same
// "intercept before normal position-based dispatch, do the hit testing
// by hand" pattern dragSearchMouseCapture already uses for Search's own
// draggable title bar (see search.go), needed here for the same
// reason: a drag in progress has to keep tracking the mouse even once
// it strays outside this scrollbar's own 1-column rect, which plain
// position-gated dispatch wouldn't allow.
func (sb *verticalScrollbar) captureMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
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

	if sb.maxOffset() <= 0 {
		// Nothing to scroll — Draw left this column blank (see its own
		// doc comment), so there's no arrow/thumb/track here to react
		// to; still consume the click rather than let it leak through
		// to whatever's drawn on another page at the same screen cell
		// (see the MouseLeftClick case below for why that matters).
		return tview.MouseConsumed, nil
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
		// above (see Application.fireMouseActions), and a maximized
		// overlay (e.g. Look, see viewerSize's own doc comment) can
		// share a screen cell with something drawn on another page
		// entirely, so without this, that second action falls through
		// Pages' own dispatch to whatever's underneath — a real,
		// user-reported bug: a click on Look's own scrollbar column
		// triggered the main panel's Details slide-out button (see
		// panel.go/detailsExpandBtn), which sat at that exact
		// coordinate on the page stacked right behind it.
		return tview.MouseConsumed, nil
	}

	switch rel {
	case sbh - 2: // up arrow — both arrows sit together at the bottom, see Draw
		sb.scrollBy(-1)
		sb.startRepeat(func() { sb.scrollBy(-1) })
	case sbh - 1: // down arrow
		sb.scrollBy(1)
		sb.startRepeat(func() { sb.scrollBy(1) })
	default:
		thumbRows, thumbStart := sb.thumbGeometry(trackRows)
		trackRel := rel // the track starts at the scrollbar's own top row (rel 0), no top arrow to offset past
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
			sb.dragStartOffset, _ = sb.content.scrollOffset()
			sb.dragSpan = trackRows - thumbRows
			sb.dragMaxOffset = sb.maxOffset()
		}
	}
	return tview.MouseConsumed, nil
}
