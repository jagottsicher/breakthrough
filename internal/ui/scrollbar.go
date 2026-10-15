package ui

import (
	"context"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// scrollbarHost is whatever hosts a scrollbar — its own current theme
// (for Draw's own track/thumb colors) and Application (for
// startRepeat's own QueueUpdateDraw). Both *Root and *Panel already
// have exactly these two things as plain fields; implementing this
// small interface instead of taking a concrete *Root directly is what
// lets Panel build its own scrollbar fully at construction time (see
// NewPanel's own doc comment on why — *Root doesn't exist yet the
// moment the very first Panel is built, and many tests build a bare
// *Panel with no *Root at all).
type scrollbarHost interface {
	Theme() config.ResolvedTheme
	App() *tview.Application
}

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
// keypress or mouse move) would be wasteful; cachedWidth/cachedText pin
// it to the last actual change in whatever it depends on.
//
// The cache key is the full text, not len(text) — a real, found bug in
// an earlier version: two different diffs (or any two different texts)
// landing at the exact same byte length, which real-world content does
// hit by coincidence, collided on a len()-only key and silently kept
// serving the previous text's own stale totalRows/totalCols. Reported
// live as "the horizontal scrollbar doesn't show up" while clicking
// through several commits in Git Breach's own Commits table — some
// commit's diff happened to be exactly as many bytes as the previous
// one, so its real overflow was masked by whatever totalCols the
// previous, shorter-lined diff had already cached. Comparing the whole
// string costs one more memcmp per Draw than comparing its length
// already did — negligible next to GetWrappedLineCount's own cost,
// which is the expensive call this cache exists to avoid in the first
// place.
type textViewContent struct {
	view *tview.TextView

	cachedWidth int
	cachedText  string
	cachedTotal int

	// cachedColsText/cachedLineWidths/cachedTotalCols are totalCols' and
	// lineWidths' own shared cache — unlike cachedTotal above, neither
	// depends on the view's own rect (wrapping is irrelevant: both are
	// only ever asked of a SetWrap(false) view, see
	// horizontallyScrollableContent's/lineWidthMeasurer's own doc
	// comments), so the cache key here is just the text itself. One
	// cache, not two, deliberately — computing cachedTotalCols as a
	// byproduct of cachedLineWidths (see lineWidths) is what keeps the
	// two from ever disagreeing with each other the way two separate
	// caches over the same text could.
	cachedColsText   string
	cachedLineWidths []int
	cachedTotalCols  int
}

func (c *textViewContent) rect() (int, int, int, int) { return c.view.GetRect() }
func (c *textViewContent) scrollOffset() (int, int)   { return c.view.GetScrollOffset() }
func (c *textViewContent) scrollTo(row, col int)      { c.view.ScrollTo(row, col) }

func (c *textViewContent) totalRows() int {
	_, _, width, _ := c.view.GetRect()
	text := c.view.GetText(false)
	if width == c.cachedWidth && text == c.cachedText {
		return c.cachedTotal
	}
	c.cachedWidth = width
	c.cachedText = text
	c.cachedTotal = c.view.GetWrappedLineCount()
	return c.cachedTotal
}

// totalCols is the widest line's own on-screen width across the whole
// text, for horizontalScrollbar — tview.TaggedStringWidth, not a plain
// rune/byte count: these views all use SetDynamicColors(true), so a
// line like "[red]foo[-]" is full of style tags that take up zero
// screen columns; counting them would make the thumb spuriously tiny
// and maxOffset scroll out past where any real content ends.
func (c *textViewContent) totalCols() int {
	c.lineWidths() // populates/refreshes cachedTotalCols as a byproduct
	return c.cachedTotalCols
}

// lineWidths is each logical ("\n"-delimited) line's own on-screen
// width, in document order — the per-row data verticalScrollbar's own
// line-width minimap (see lineWidthMeasurer) needs to show each track
// row's own average relative to totalCols' own global maximum.
//
// Only ever meaningful for a SetWrap(false) view, the same restriction
// totalCols already documents: a logical line here is also exactly one
// displayed row (what scrollOffset's own row component addresses), so
// index i here lines up with content row i. For a SetWrap(true) view
// (Look, Help, Details — see verticalScrollbar.EnableLineWidthMinimap's
// own doc comment on why those are excluded rather than approximated),
// a single logical line can span several displayed rows of very
// different widths, tview exposes no public API to recover the real
// per-displayed-row text to measure instead, and a displayed row can
// never exceed the box width in the first place — every glyph would
// read as "nearly full" regardless of the line's own real length, which
// is informative exactly nowhere. Called only by widgets that already
// know they're SetWrap(false) (see each one's own
// EnableLineWidthMinimap call site), never unconditionally.
func (c *textViewContent) lineWidths() []int {
	text := c.view.GetText(false)
	if text == c.cachedColsText {
		return c.cachedLineWidths
	}
	c.cachedColsText = text
	lines := strings.Split(text, "\n")
	widths := make([]int, len(lines))
	max := 0
	for i, line := range lines {
		w := tview.TaggedStringWidth(line)
		widths[i] = w
		if w > max {
			max = w
		}
	}
	c.cachedLineWidths = widths
	c.cachedTotalCols = max
	return c.cachedLineWidths
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
	host    scrollbarHost
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

	// lineWidthMinimap is set via EnableLineWidthMinimap — see its own
	// doc comment for which widgets actually call it and why most
	// don't.
	lineWidthMinimap bool

	dragging        bool
	dragStartY      int
	dragStartOffset int
	dragSpan        int
	dragMaxOffset   int

	repeatCancel context.CancelFunc
}

// scrollbarThumbContrastDelta is the thumb's fixed brightness offset
// from the track's own background, per channel — darker on a light
// track, lighter on a dark one (see scrollbarThumbColor) — not a
// separately themed token: the track already uses whatever background
// the surrounding widget uses, so the thumb derives from that directly
// and stays correct under any color scheme without its own override
// field.
//
// A plain percentage darken (the original approach, per the user's own
// "one shade darker" request) looks fine against a mid-tone track but
// nearly disappears against an already very dark one: Git Breach's own
// PanelBackground (#1c3232) darkened by 0.6 landed only ~11-20 points
// apart per channel — a real, user-reported "can't tell there's a
// scrollbar there at all" once that same screen's own focus-dependent
// brighter track (styleGitBreachFocus) gave the eye a directly
// comparable, much higher-contrast reference right next to it. A fixed
// absolute delta, flipped to lighten instead of darken once darkening
// would clip at 0 (see scrollbarThumbColor), keeps the thumb clearly
// visible either way instead of asymptotically vanishing into black.
const scrollbarThumbContrastDelta = 32

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
	// Darkening is the default (matches the user's own original "one
	// shade darker" request, and is what Git Breach's own focused —
	// already-confirmed-good — track color still gets below). Only
	// flips to lightening once the track's own darkest channel is so
	// low that darkening by the fixed delta would clip at 0 — losing
	// the very distinction a thumb is supposed to provide, rather than
	// just making it subtler. That clip-at-0 case is exactly Git
	// Breach's own unfocused state (PanelBackground, #1c3232): a real,
	// user-reported "can't tell there's a scrollbar there at all" once
	// that screen's own focus-dependent brighter track gave the eye a
	// directly comparable, much higher-contrast reference right next
	// to it. A luminance-based switch was tried first and rejected: on
	// this app's own teal-leaning palette, even the focused track's
	// luminance (~83 out of 255) still read as "dark" under any
	// reasonable fixed threshold, which would have also flipped —
	// visibly changing — the one case already confirmed to look right.
	base := r
	if g < base {
		base = g
	}
	if b < base {
		base = b
	}
	delta := int32(scrollbarThumbContrastDelta)
	if base-delta < 0 {
		delta = -delta
	}
	shift := func(v int32) int32 {
		v -= delta
		switch {
		case v < 0:
			return 0
		case v > 255:
			return 255
		default:
			return v
		}
	}
	return tcell.NewRGBColor(shift(r), shift(g), shift(b))
}

// newVerticalScrollbar builds a scrollbar over content — newVerticalScrollbarForTextView/
// newVerticalScrollbarForTable below are the usual way to get one.
func newVerticalScrollbar(host scrollbarHost, content scrollableContent) *verticalScrollbar {
	return &verticalScrollbar{Box: tview.NewBox(), host: host, content: content}
}

func newVerticalScrollbarForTextView(host scrollbarHost, view *tview.TextView) *verticalScrollbar {
	return newVerticalScrollbar(host, &textViewContent{view: view})
}

func newVerticalScrollbarForTable(host scrollbarHost, table *tview.Table) *verticalScrollbar {
	return newVerticalScrollbar(host, tableContent{table: table})
}

// SetTrackColorFunc overrides the track's own background color — see
// the trackColor field's own doc comment for why a screen would need
// this at all. f is called fresh on every Draw, so it can reflect
// live, changing state (e.g. whether the content next to it currently
// has keyboard focus).
func (sb *verticalScrollbar) SetTrackColorFunc(f func() tcell.Color) {
	sb.trackColor = f
}

// lineWidthMeasurer is the extra capability a scrollbar's own content
// needs to draw the line-width minimap (see EnableLineWidthMinimap) —
// bundled into one interface (rather than a loose type-assertion on
// totalCols alone) since both numbers have to come from the same
// content to mean anything together. *textViewContent implements this
// directly; tableContent deliberately does not (a per-cell concept
// doesn't carry over cleanly to a table's many columns — explicitly
// out of scope for now, not forgotten).
type lineWidthMeasurer interface {
	lineWidths() []int
	totalCols() int
}

// EnableLineWidthMinimap turns on the track's own line-width minimap —
// Phase 2 of the user's original scrollbar request, built once Phase 1
// (the plain thumb/arrows/drag scrollbar, already rolled out everywhere)
// was confirmed working. Each track row fills with one of
// scrollbarMinimapBlocks' own horizontal eighth-block glyphs,
// representing the average on-screen width of the content rows that
// track row represents, relative to the single widest line in the
// WHOLE document (content.totalCols(), not just this row's own local
// share — an explicit correction from the user mid-spec, rejecting an
// earlier local-max design that would have made every section look
// equally "full" regardless of its real relative length).
//
// Deliberately opt-in per widget, not automatic for every
// scrollableContent that happens to implement lineWidthMeasurer:
// SetWrap(true) views (Look, Help, Details) also build on
// textViewContent and so also satisfy the interface, but a displayed
// row there can never exceed the box's own width in the first place —
// the minimap would read every line as "nearly full" regardless of its
// real length, informative nowhere. Call this only from a widget whose
// own TextView is SetWrap(false) (checked by the author at each call
// site, not detectable here — tview exposes no public getter for its
// own unexported wrap field). Currently called by toolWindow's command
// output, Git Breach's Main diff (unified and split), Compare's
// side-by-side diff view, and Log Audit's Statistics modal — see each
// one's own construction site.
func (sb *verticalScrollbar) EnableLineWidthMinimap() {
	sb.lineWidthMinimap = true
}

// scrollbarMinimapBlocks are the line-width minimap's own glyphs, one
// per 1/8th of fill, horizontal and left-aligned (unlike the vertical,
// bottom-up blocks Log Audit's own timeline bar uses elsewhere in this
// app) — chosen so the fill direction visually matches the direction
// text itself flows, per the user's own explicit request.
var scrollbarMinimapBlocks = []rune{' ', '▏', '▎', '▍', '▌', '▋', '▊', '▉', '█'}

// scrollbarMinimapGlyphs maps lineWidths (one entry per content row, in
// document order) onto exactly trackRows glyphs — a pure function,
// deliberately split out of Draw and unit-tested on its own, since a
// row-range off-by-one is exactly the kind of bug that hides inside a
// larger rendering function. Each track row i represents content rows
// [i*total/trackRows, (i+1)*total/trackRows) (at least one row, even
// where trackRows exceeds total — the tail end of a short document
// then has a few track rows repeating its own last row's width, rather
// than an empty range with nothing to average); its own glyph is that
// range's average width as a fraction of globalMax, rounded to the
// nearest eighth.
func scrollbarMinimapGlyphs(lineWidths []int, trackRows, globalMax int) []rune {
	glyphs := make([]rune, trackRows)
	for i := range glyphs {
		glyphs[i] = scrollbarMinimapBlocks[0]
	}
	total := len(lineWidths)
	if trackRows <= 0 || total <= 0 || globalMax <= 0 {
		return glyphs
	}

	for i := 0; i < trackRows; i++ {
		start := i * total / trackRows
		end := (i + 1) * total / trackRows
		if end <= start {
			end = start + 1
		}
		if end > total {
			end = total
		}
		if start >= end {
			start = end - 1
		}

		sum := 0
		for _, w := range lineWidths[start:end] {
			sum += w
		}
		avg := float64(sum) / float64(end-start)

		level := int(avg/float64(globalMax)*8 + 0.5)
		if level < 0 {
			level = 0
		}
		if level > 8 {
			level = 8
		}
		glyphs[i] = scrollbarMinimapBlocks[level]
	}
	return glyphs
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
				sb.host.App().QueueUpdateDraw(step)
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

	theme := sb.host.Theme()
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

	// The line-width minimap (see EnableLineWidthMinimap) — nil unless
	// both this scrollbar opted in AND its own content actually supports
	// it (a SetWrap(true) textViewContent would technically satisfy
	// lineWidthMeasurer too, but EnableLineWidthMinimap is never called
	// for one — see its own doc comment). Drawn under the thumb too, not
	// just the plain track: the minimap's whole point is to show the
	// document's shape at a glance, which a blank gap where the thumb
	// currently sits would undercut.
	var minimap []rune
	if sb.lineWidthMinimap {
		if lw, ok := sb.content.(lineWidthMeasurer); ok {
			minimap = scrollbarMinimapGlyphs(lw.lineWidths(), trackRows, lw.totalCols())
		}
	}

	for i := 0; i < trackRows; i++ {
		row := y + i
		bg := track
		if i >= thumbStart && i < thumbStart+thumbRows {
			bg = thumb
		}
		ch := ' '
		fg := theme.TextColor
		if minimap != nil {
			ch = minimap[i]
			// The same fixed-delta contrast scrollbarThumbColor already
			// uses for the thumb's own background against the track —
			// reused here, not a second color algorithm, since it's
			// already proven safe against every background this app's
			// scrollbars sit on (Git Breach's own dark unfocused track
			// included — see scrollbarThumbColor's own doc comment).
			fg = scrollbarThumbColor(bg)
		}
		screen.SetContent(x, row, ch, nil, tcell.StyleDefault.Background(bg).Foreground(fg))
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
