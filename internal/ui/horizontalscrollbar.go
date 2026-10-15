package ui

import (
	"context"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// horizontallyScrollableContent is what horizontalScrollbar needs from
// whatever it's attached to — the column-axis counterpart of
// scrollableContent. Deliberately a separate, narrower interface rather
// than adding totalCols to scrollableContent itself: tableContent has
// no honest answer for "how many columns wide is this" (see
// horizontalScrollbar's own doc comment on why tables don't get a
// horizontal scrollbar at all yet), so folding it into the one
// interface every scrollableContent adapter implements would force a
// meaningless stub onto tableContent for no widget that will ever use
// it. *textViewContent implements this directly (see its own totalCols
// method, scrollbar.go); the bespoke linked-pane adapters
// (gitBreachDiffSplitContent, compareDiffPaneContent) get it for free
// by already embedding *textViewContent.
type horizontallyScrollableContent interface {
	rect() (x, y, width, height int)
	scrollOffset() (row, col int)
	scrollTo(row, col int)
	totalCols() int
}

// horizontalScrollbar is a custom, 1-row-tall horizontal scrollbar —
// the column-axis counterpart of verticalScrollbar, built once that one
// was rolled out everywhere and every remaining SetWrap(false) content
// pane (toolWindow's own command output, Git Breach's and Compare's
// diff panes, the Log Audit timeline/stats views) turned out to still
// need real horizontal scrolling of its own.
//
// Deliberately its own type, not verticalScrollbar reparameterized onto
// the other axis: the vertical bar's exact behavior (arrow placement,
// click-vs-click-leak, hide-when-fits, thumb contrast) was each
// corrected by hand, live, against real user reports — reworking that
// type into an axis-generic form risks quietly regressing one of those
// already-signed-off behaviors for no benefit anyone would see. The
// repeat/clamp/thumb-geometry arithmetic below is the same shape as
// verticalScrollbar's own, just swapped onto columns; scrollbarThumbColor
// and the two repeat-timing constants are reused as-is (already
// axis-neutral).
//
// Layout: both arrow buttons sit together at the track's own LEFT end
// (there's no established platform precedent for a horizontal
// scrollbar's own arrow placement the way Mac Classic set one for
// vertical bars — left was chosen so the arrows read first, in the same
// direction text itself reads, rather than being tucked at the far
// right where they'd be the last thing noticed). The track (with its
// thumb) fills everything to their right.
type horizontalScrollbar struct {
	*tview.Box
	host    scrollbarHost
	content horizontallyScrollableContent

	// trackColor mirrors verticalScrollbar's own field exactly — see its
	// doc comment there for why a screen needs this at all.
	trackColor func() tcell.Color

	scrollbarDrag

	repeatCancel context.CancelFunc
}

// newHorizontalScrollbar builds a scrollbar over content —
// newHorizontalScrollbarForTextView below is the usual way to get one.
func newHorizontalScrollbar(host scrollbarHost, content horizontallyScrollableContent) *horizontalScrollbar {
	return &horizontalScrollbar{Box: tview.NewBox(), host: host, content: content}
}

func newHorizontalScrollbarForTextView(host scrollbarHost, view *tview.TextView) *horizontalScrollbar {
	return newHorizontalScrollbar(host, &textViewContent{view: view})
}

// SetTrackColorFunc mirrors verticalScrollbar's own method of the same
// name — see its doc comment there.
func (sb *horizontalScrollbar) SetTrackColorFunc(f func() tcell.Color) {
	sb.trackColor = f
}

// viewportCols is how many columns of content are visible at once.
func (sb *horizontalScrollbar) viewportCols() int {
	_, _, width, _ := sb.content.rect()
	return width
}

// maxOffset is the furthest column content can be scrolled to — 0 once
// everything already fits inside the viewport, the same "hide when it
// fits" threshold verticalScrollbar's own Draw/captureMouse already
// check for rows.
func (sb *horizontalScrollbar) maxOffset() int {
	m := sb.content.totalCols() - sb.viewportCols()
	if m < 0 {
		return 0
	}
	return m
}

func (sb *horizontalScrollbar) clampOffset(v int) int {
	return clampScrollOffset(v, sb.maxOffset())
}

// thumbGeometry mirrors verticalScrollbar's own method exactly, swapped
// onto columns — see its doc comment there.
func (sb *horizontalScrollbar) thumbGeometry(trackCols int) (thumbCols, thumbStart int) {
	_, col := sb.content.scrollOffset()
	return scrollbarThumbGeometry(trackCols, sb.content.totalCols(), sb.viewportCols(), col)
}

func (sb *horizontalScrollbar) scrollBy(delta int) {
	row, col := sb.content.scrollOffset()
	sb.content.scrollTo(row, sb.clampOffset(col+delta))
}

func (sb *horizontalScrollbar) pageJump(dir int) {
	sb.scrollBy(dir * sb.viewportCols())
}

// dragTo mirrors verticalScrollbar's own dragTo exactly, swapped onto
// columns — see its doc comment there.
func (sb *horizontalScrollbar) dragTo(mouseX int) {
	if sb.dragSpan <= 0 {
		return
	}
	row, _ := sb.content.scrollOffset()
	sb.content.scrollTo(row, sb.clampOffset(sb.delta(mouseX)))
}

// startRepeat/stopRepeat both delegate to the shared
// startScrollbarRepeat/stopScrollbarRepeat functions (scrollbar.go) —
// see their own doc comments.
func (sb *horizontalScrollbar) startRepeat(step func()) {
	startScrollbarRepeat(sb.host.App(), &sb.repeatCancel, step)
}

func (sb *horizontalScrollbar) stopRepeat() {
	if sb == nil {
		return
	}
	stopScrollbarRepeat(&sb.repeatCancel)
}

// Draw paints the arrow columns, the track, and the thumb directly onto
// screen, the same hand-painted single-strip approach verticalScrollbar's
// own Draw uses — just one row instead of one column.
func (sb *horizontalScrollbar) Draw(screen tcell.Screen) {
	sb.DrawForSubclass(screen, sb)
	x, y, width, _ := sb.GetInnerRect()
	if width < 3 {
		return
	}

	theme := sb.host.Theme()
	track := theme.PanelBackground
	if sb.trackColor != nil {
		track = sb.trackColor()
	}
	thumb := scrollbarThumbColor(track)

	setCell := func(col int, bg tcell.Color, ch rune) {
		screen.SetContent(col, y, ch, nil, tcell.StyleDefault.Background(bg).Foreground(theme.TextColor))
	}

	// Nothing to scroll — same hide-when-it-fits rule verticalScrollbar's
	// own Draw follows: blank track, no arrows, no thumb, the column
	// itself (here: the row) stays reserved rather than reflowing.
	if sb.maxOffset() <= 0 {
		for i := 0; i < width; i++ {
			setCell(x+i, track, ' ')
		}
		return
	}

	// Both arrows sit together at the track's own left end — see this
	// type's own doc comment on why left rather than mirroring the
	// vertical bar's bottom placement. The track itself is everything to
	// their right.
	trackCols := width - 2
	thumbCols, thumbStart := sb.thumbGeometry(trackCols)
	setCell(x, thumb, '◀')
	setCell(x+1, thumb, '▶')
	for i := 0; i < trackCols; i++ {
		col := x + 2 + i
		if i >= thumbStart && i < thumbStart+thumbCols {
			setCell(col, thumb, ' ')
		} else {
			setCell(col, track, ' ')
		}
	}
}

// captureMouse mirrors verticalScrollbar's own captureMouse exactly,
// swapped onto the x axis — see its doc comment there, including why
// every action landing in this bar's own rect is consumed rather than
// just the ones this bar actually acts on.
func (sb *horizontalScrollbar) captureMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	x, y := event.Position()

	if sb.dragging {
		switch action {
		case tview.MouseMove:
			if event.Buttons()&tcell.ButtonPrimary == 0 {
				sb.dragging = false
				return tview.MouseConsumed, nil
			}
			sb.dragTo(x)
			return tview.MouseConsumed, nil
		case tview.MouseLeftUp:
			sb.dragging = false
			return tview.MouseConsumed, nil
		}
		return tview.MouseConsumed, nil
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
		return tview.MouseConsumed, nil
	}

	if !sb.InRect(x, y) {
		return action, event
	}

	if sb.maxOffset() <= 0 {
		return tview.MouseConsumed, nil
	}

	sbx, _, sbw, _ := sb.GetRect()
	rel := x - sbx
	trackCols := sbw - 2

	if action == tview.MouseScrollLeft {
		sb.scrollBy(-1)
		return tview.MouseConsumed, nil
	}
	if action == tview.MouseScrollRight {
		sb.scrollBy(1)
		return tview.MouseConsumed, nil
	}
	if action != tview.MouseLeftDown {
		// Same trailing-MouseLeftClick reasoning as verticalScrollbar's
		// own captureMouse — see its doc comment there.
		return tview.MouseConsumed, nil
	}

	switch rel {
	case 0: // left arrow — both arrows sit together at the left end, see Draw
		sb.scrollBy(-1)
		sb.startRepeat(func() { sb.scrollBy(-1) })
	case 1: // right arrow
		sb.scrollBy(1)
		sb.startRepeat(func() { sb.scrollBy(1) })
	default:
		thumbCols, thumbStart := sb.thumbGeometry(trackCols)
		trackRel := rel - 2 // the track starts two columns in, past the two arrows
		switch {
		case trackRel < thumbStart:
			sb.pageJump(-1)
			sb.startRepeat(func() { sb.pageJump(-1) })
		case trackRel >= thumbStart+thumbCols:
			sb.pageJump(1)
			sb.startRepeat(func() { sb.pageJump(1) })
		default:
			sb.dragging = true
			sb.dragStartPos = x
			_, sb.dragStartOffset = sb.content.scrollOffset()
			sb.dragSpan = trackCols - thumbCols
			sb.dragMaxOffset = sb.maxOffset()
		}
	}
	return tview.MouseConsumed, nil
}
