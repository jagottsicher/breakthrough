package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/compare"
)

const (
	gitBreachDiffUnifiedPage = "unified"
	gitBreachDiffSplitPage   = "split"
)

// newGitBreachDiffSplit builds Main's own second view: two side-by-side
// panes separated by one column of a plain vertical rule, wrapped in a
// tview.Pages alongside the existing unified pager (r.gitBreachDiffView)
// so "v" can switch between the two without rebuilding either one —
// the exact same two-pane shape comparediff.go's own Compare diff view
// already establishes, reused here rather than reinvented, except
// scrolling is unconditionally linked (no link/unlink toggle of its
// own): this is one file's own before/after, not two unrelated things
// the way Compare's own two targets are, so there's no case where
// independent scrolling would ever make sense.
//
// Only gitBreachDiffSplitLeft ever receives real keyboard focus — it's
// the one widget with an InputCapture at all; gitBreachDiffSplitRight
// is a pure mirror, redirecting a click straight back to its own
// sibling (see gitBreachFocusDiffSplitLeftOnClick) rather than ever
// holding focus itself, which is what keeps "linked" meaning "always",
// not "until something takes focus off the one widget driving it".
func (r *Root) newGitBreachDiffSplit() {
	r.gitBreachDiffSplitLeft = tview.NewTextView()
	r.gitBreachDiffSplitLeft.SetDynamicColors(true)
	r.gitBreachDiffSplitLeft.SetWrap(false)
	r.gitBreachDiffSplitLeft.SetInputCapture(r.captureGitBreachDiffSplitKey)
	r.gitBreachDiffSplitLeft.SetMouseCapture(r.captureGitBreachDiffSplitLeftMouse)
	// renderGitBreachDiffHeader also needs to re-run while THIS pane is
	// what's actually being drawn: gitBreachDiffView's own identical
	// SetDrawFunc (gitbreachscreen.go) only ever fires while the
	// unified page is the one tview.Pages is actually showing — Draw()
	// is never called on a page's own content while a sibling page is
	// the visible one (verified directly against tview's own pages.go,
	// not guessed). Without this, "Page X/Y" froze at whatever it last
	// showed before switching into split mode (or before switching to
	// a shorter/longer file or commit while already in it) and never
	// updated again — a real, reported bug, not a hypothetical one.
	//
	// width passed in here is this one pane's own (half of Main's own
	// content area, minus the separator) — too narrow for the header's
	// own right-alignment padding, which has to match the full title
	// bar it's actually drawn across. gitBreachDiffHeader's own current
	// rect is read directly instead; height is shared unchanged, since
	// the panes only split horizontally, never vertically.
	r.gitBreachDiffSplitLeft.SetDrawFunc(func(_ tcell.Screen, x, y, width, height int) (int, int, int, int) {
		_, _, headerWidth, _ := r.gitBreachDiffHeader.GetRect()
		r.renderGitBreachDiffHeader(headerWidth, height)
		return x, y, width, height
	})
	r.gitBreachDiffSplitLeft.SetFocusFunc(func() {
		r.styleGitBreachFocus(r.gitBreachDiffHeader, r.gitBreachDiffSplitLeft, true)
		r.styleGitBreachFocus(r.gitBreachDiffHeader, r.gitBreachDiffSplitRight, true)
		r.styleGitBreachFocus(r.gitBreachDiffHeader, r.gitBreachDiffSplitSpacer, true)
		r.refreshGitBreachHint()
	})
	r.gitBreachDiffSplitLeft.SetBlurFunc(func() {
		r.styleGitBreachFocus(r.gitBreachDiffHeader, r.gitBreachDiffSplitLeft, false)
		r.styleGitBreachFocus(r.gitBreachDiffHeader, r.gitBreachDiffSplitRight, false)
		r.styleGitBreachFocus(r.gitBreachDiffHeader, r.gitBreachDiffSplitSpacer, false)
	})

	r.gitBreachDiffSplitRight = tview.NewTextView()
	r.gitBreachDiffSplitRight.SetDynamicColors(true)
	r.gitBreachDiffSplitRight.SetWrap(false)
	r.gitBreachDiffSplitRight.SetMouseCapture(r.captureGitBreachDiffSplitRightMouse)

	r.gitBreachDiffSplitSeparator = tview.NewBox()
	r.gitBreachDiffSplitSeparator.SetDrawFunc(r.drawGitBreachDiffSplitSeparator)
	// Same real bug comparediff.go's own separator doc comment
	// explains: a bare Box with no mouse capture of its own silently
	// steals keyboard focus onto itself on a stray click, after which
	// every key does nothing while the mouse wheel keeps working.
	r.gitBreachDiffSplitSeparator.SetMouseCapture(gitBreachBlockFocusSteal)

	splitBody := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.gitBreachDiffSplitLeft, 0, 1, true).
		AddItem(r.gitBreachDiffSplitSeparator, 1, 0, false).
		AddItem(r.gitBreachDiffSplitRight, 0, 1, false)

	// Main gets two scrollbars, one per page of gitBreachDiffPages
	// below — only one is ever visible at a time, matching whichever
	// page toggleGitBreachDiffSplit last switched to. The unified
	// pager is a plain TextView (textViewContent, same as every other
	// TextView-backed scrollbar); the split view's own scrollbar moves
	// both panes together via gitBreachDiffSplitContent, never just the
	// one it happens to be drawn next to — the same "always linked, no
	// unlinked mode" invariant scrollGitBreachDiffSplit already
	// enforces for keyboard/wheel scrolling.
	r.gitBreachDiffScrollbar = newVerticalScrollbarForTextView(r, r.gitBreachDiffView)
	r.gitBreachDiffScrollbar.EnableLineWidthMinimap() // gitBreachDiffView is SetWrap(false)
	r.gitBreachDiffScrollbar.SetTrackColorFunc(r.gitBreachScrollbarTrackColor(r.gitBreachDiffView))
	unifiedTopRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.gitBreachDiffView, 0, 1, true).
		AddItem(r.gitBreachDiffScrollbar, 1, 0, false)
	unifiedTopRow.SetMouseCapture(r.gitBreachDiffScrollbar.captureMouse)

	// The horizontal counterpart, same shape as Compare's own corner
	// handling (comparediff.go): a reserved spacer column under the
	// vertical bar, painted its own track color, so the two bars' rects
	// never share a cell.
	r.gitBreachDiffHScrollbar = newHorizontalScrollbarForTextView(r, r.gitBreachDiffView)
	r.gitBreachDiffHScrollbar.SetTrackColorFunc(r.gitBreachScrollbarTrackColor(r.gitBreachDiffView))
	r.gitBreachDiffSpacer = tview.NewBox().SetBackgroundColor(r.theme.PanelBackground)
	unifiedHBarRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.gitBreachDiffHScrollbar, 0, 1, false).
		AddItem(r.gitBreachDiffSpacer, 1, 0, false)
	unifiedHBarRow.SetMouseCapture(r.gitBreachDiffHScrollbar.captureMouse)
	unifiedRow := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(unifiedTopRow, 0, 1, true).
		AddItem(unifiedHBarRow, 1, 0, false)

	splitContent := &gitBreachDiffSplitContent{
		textViewContent: &textViewContent{view: r.gitBreachDiffSplitLeft},
		right:           r.gitBreachDiffSplitRight,
	}
	r.gitBreachDiffSplitScrollbar = newVerticalScrollbar(r, splitContent)
	r.gitBreachDiffSplitScrollbar.EnableLineWidthMinimap() // both panes are SetWrap(false)
	r.gitBreachDiffSplitScrollbar.SetTrackColorFunc(r.gitBreachScrollbarTrackColor(r.gitBreachDiffSplitLeft))
	splitTopRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(splitBody, 0, 1, true).
		AddItem(r.gitBreachDiffSplitScrollbar, 1, 0, false)
	splitTopRow.SetMouseCapture(r.gitBreachDiffSplitScrollbar.captureMouse)

	// One shared horizontal bar for the whole splitBody, the same
	// instance of gitBreachDiffSplitContent as the vertical bar above —
	// its own scrollTo override already moves both panes together (see
	// its doc comment), so sharing it here is what keeps a horizontal
	// scroll linked too, with no further adapter needed.
	r.gitBreachDiffSplitHScrollbar = newHorizontalScrollbar(r, splitContent)
	r.gitBreachDiffSplitHScrollbar.SetTrackColorFunc(r.gitBreachScrollbarTrackColor(r.gitBreachDiffSplitLeft))
	r.gitBreachDiffSplitSpacer = tview.NewBox().SetBackgroundColor(r.theme.PanelBackground)
	splitHBarRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.gitBreachDiffSplitHScrollbar, 0, 1, false).
		AddItem(r.gitBreachDiffSplitSpacer, 1, 0, false)
	splitHBarRow.SetMouseCapture(r.gitBreachDiffSplitHScrollbar.captureMouse)
	splitRow := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(splitTopRow, 0, 1, true).
		AddItem(splitHBarRow, 1, 0, false)

	r.gitBreachDiffPages = tview.NewPages().
		AddPage(gitBreachDiffUnifiedPage, unifiedRow, true, true).
		AddPage(gitBreachDiffSplitPage, splitRow, true, false)
}

// gitBreachDiffSplitContent adapts the diff split view's own linked
// pair of panes as one scrollableContent (see scrollbar.go) —
// rect/scrollOffset/totalRows all read from the left pane only
// (embedding *textViewContent for it directly, cache and all), since
// gitBreachRenderDiffSplit's own Pair invariant guarantees both panes
// have the same line count, but scrollTo moves both together — the
// same "always linked, no unlinked mode" invariant scrollGitBreachDiffSplit
// already enforces for keyboard/wheel scrolling (see
// newGitBreachDiffSplit's own doc comment for why there's no unlinked
// mode here at all).
type gitBreachDiffSplitContent struct {
	*textViewContent
	right *tview.TextView

	// rightCols caches the right pane's own totalCols the same way
	// textViewContent itself caches the left pane's — see totalCols
	// below for why this adapter needs both at all.
	rightCols *textViewContent
}

func (c *gitBreachDiffSplitContent) scrollTo(row, col int) {
	c.textViewContent.scrollTo(row, col)
	c.right.ScrollTo(row, col)
}

// totalCols overrides the embedded textViewContent's own method: the
// Pair invariant gitBreachRenderDiffSplit enforces only guarantees both
// panes have the same number of lines, never that corresponding lines
// are the same WIDTH (an edit can turn a short line into a much longer
// one, or vice versa) — reading the left pane alone here would under-
// report maxOffset whenever the right pane's own longest line is
// actually the wider of the two, leaving the shared horizontal bar
// unable to scroll far enough to ever reveal the rest of it.
func (c *gitBreachDiffSplitContent) totalCols() int {
	left := c.textViewContent.totalCols()
	if c.rightCols == nil {
		c.rightCols = &textViewContent{view: c.right}
	}
	if right := c.rightCols.totalCols(); right > left {
		return right
	}
	return left
}

// lineWidths overrides the embedded textViewContent's own method for
// the same reason totalCols does: the minimap's own per-row glyph
// (see verticalScrollbar.EnableLineWidthMinimap) should show whichever
// pane is actually wider on that row, not always the left one. The
// Pair invariant that guarantees equal line counts is what makes
// pairing by index safe here.
func (c *gitBreachDiffSplitContent) lineWidths() []int {
	left := c.textViewContent.lineWidths()
	if c.rightCols == nil {
		c.rightCols = &textViewContent{view: c.right}
	}
	right := c.rightCols.lineWidths()

	widths := make([]int, len(left))
	for i, w := range left {
		widths[i] = w
		if i < len(right) && right[i] > w {
			widths[i] = right[i]
		}
	}
	return widths
}

// drawGitBreachDiffSplitSeparator mirrors
// drawCompareDiffSeparator (comparediff.go) exactly.
func (r *Root) drawGitBreachDiffSplitSeparator(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
	style := tcell.StyleDefault.Foreground(r.theme.MutedTextColor).Background(r.gitBreachDiffSplitSeparator.GetBackgroundColor())
	for row := y; row < y+height; row++ {
		screen.SetContent(x, row, '│', nil, style)
	}
	return x, y, width, height
}

// toggleGitBreachDiffSplit is "v" while Main has focus (see
// captureGitBreachMainKey/captureGitBreachDiffSplitKey) — switches
// gitBreachDiffPages between the unified pager and the side-by-side
// view, re-rendering the split view from gitBreachDiffRawText first
// when switching into it, since the raw text (or which file/commit/
// stash entry it belongs to) can have changed since the last time it
// was shown without the split view itself having been asked to
// re-render meanwhile (gitBreachSetDiffText only does that
// automatically while split mode is already active).
func (r *Root) toggleGitBreachDiffSplit() {
	r.gitBreachDiffSplit = !r.gitBreachDiffSplit
	if r.gitBreachDiffSplit {
		r.gitBreachRenderDiffSplit()
		row, _ := r.gitBreachDiffView.GetScrollOffset()
		r.gitBreachDiffSplitLeft.ScrollTo(row, 0)
		r.gitBreachDiffSplitRight.ScrollTo(row, 0)
		r.gitBreachDiffPages.SwitchToPage(gitBreachDiffSplitPage)
		r.app.SetFocus(r.gitBreachDiffSplitLeft)
	} else {
		row, _ := r.gitBreachDiffSplitLeft.GetScrollOffset()
		r.gitBreachDiffView.ScrollTo(row, 0)
		r.gitBreachDiffPages.SwitchToPage(gitBreachDiffUnifiedPage)
		r.app.SetFocus(r.gitBreachDiffView)
	}
	r.refreshGitBreachHint()
}

// gitBreachRenderDiffSplit parses gitBreachDiffRawText (see
// compare.ParseUnifiedDiff) into gitBreachDiffFiles and renders it into
// both panes — one row per LinePair, always the same row on both
// sides, exactly the invariant comparediff.go's own renderCompareDiff
// relies on (see its own doc comment), which is what lets scrolling
// stay linked by raw row index alone. Multiple files (a commit/stash
// entry touching several of them) are shown stacked, each introduced by
// its own path header line on both sides; a PairGap row (see
// compare.PairKind's own doc comment) becomes a muted "⋮" marker rather
// than disappearing silently, so a hunk boundary still reads as "lines
// were skipped here", the same as an ordinary unified diff's own gap
// between hunks.
//
// Reuses writeCompareDiffLine (comparediff.go) for each content line —
// the same per-language syntax highlighting plus added/removed
// background tint, not a second, parallel rendering scheme.
func (r *Root) gitBreachRenderDiffSplit() {
	r.gitBreachDiffFiles = compare.ParseUnifiedDiff(r.gitBreachDiffRawText)
	if len(r.gitBreachDiffFiles) == 0 {
		msg := fmt.Sprintf("[%s]nothing to show side by side here.[-]", colorTag(r.theme.MutedTextColor))
		r.gitBreachDiffSplitLeft.SetText(msg)
		r.gitBreachDiffSplitRight.SetText(msg)
		return
	}

	palette := paletteFor(r.theme.PopupBackground)
	var left, right strings.Builder
	for i, f := range r.gitBreachDiffFiles {
		if i > 0 {
			left.WriteString("\n")
			right.WriteString("\n")
		}
		header := fmt.Sprintf("[%s::b]%s[-:-:-]\n", colorTag(r.theme.MutedTextColor), tview.Escape(f.Path))
		left.WriteString(header)
		right.WriteString(header)
		for _, p := range f.Pairs {
			if p.Kind == compare.PairGap {
				gap := fmt.Sprintf("%s[%s]⋮[-]\n", strings.Repeat(" ", compareDiffGutterWidth+1), colorTag(r.theme.MutedTextColor))
				left.WriteString(gap)
				right.WriteString(gap)
				continue
			}
			leftBg, haveLeftBg := tcell.ColorDefault, false
			rightBg, haveRightBg := tcell.ColorDefault, false
			switch p.Kind {
			case compare.PairChanged:
				leftBg, haveLeftBg = gitBreachDiffRemovedBackground, true
				rightBg, haveRightBg = gitBreachDiffAddedBackground, true
			case compare.PairOnlyLeft:
				leftBg, haveLeftBg = gitBreachDiffRemovedBackground, true
			case compare.PairOnlyRight:
				rightBg, haveRightBg = gitBreachDiffAddedBackground, true
			}
			writeCompareDiffLine(&left, p.LeftNo, p.Left, f.Path, palette, leftBg, haveLeftBg)
			writeCompareDiffLine(&right, p.RightNo, p.Right, f.Path, palette, rightBg, haveRightBg)
		}
	}
	r.gitBreachDiffSplitLeft.SetText(left.String())
	r.gitBreachDiffSplitRight.SetText(right.String())
}

// gitBreachDiffSplitPageStep mirrors compareDiffPageStep exactly.
func (r *Root) gitBreachDiffSplitPageStep() int {
	_, _, _, height := r.gitBreachDiffSplitLeft.GetRect()
	if height <= 0 {
		return 10
	}
	return height
}

// scrollGitBreachDiffSplit moves both panes by the same row/column
// delta — always, unconditionally: unlike comparediff.go's own
// scrollCompareDiff, there is no unlinked mode here to check (see
// newGitBreachDiffSplit's own doc comment for why). TextView's own Draw
// clamps an out-of-range row/column back into bounds on its own, the
// same as every other scroll call in this app relies on.
func (r *Root) scrollGitBreachDiffSplit(rowDelta, colDelta int) {
	row, col := r.gitBreachDiffSplitLeft.GetScrollOffset()
	row += rowDelta
	col += colDelta
	r.gitBreachDiffSplitLeft.ScrollTo(row, col)
	r.gitBreachDiffSplitRight.ScrollTo(row, col)
}

// captureGitBreachDiffSplitKey is gitBreachDiffSplitLeft's own
// InputCapture — the split view's one real focus target, so this is
// where every key the split view answers to lands, mirroring
// captureGitBreachMainKey's own Tab/Backtab/Escape/Space/c/r handling
// exactly, plus the split view's own Up/Down/PgUp/PgDn/Left/Right
// scrolling (see scrollGitBreachDiffSplit) and "v" to switch back.
//
// Left is the one key that means something conditional: scroll left
// while there's horizontal room left to scroll (column > 0), or leave
// back to whichever box owns Main's content (the same action Escape/
// Enter/Space already reach) once already at the left edge — per the
// user's own explicit request, so a user scrolled right to read a long
// line can always get back out with Left alone, the same muscle memory
// Up/Down/PgUp/PgDn already rely on, without that same key accidentally
// leaving the screen on the very first press while there's still
// content to scroll back through.
func (r *Root) captureGitBreachDiffSplitKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape, tcell.KeyEnter:
		r.deactivateGitBreachMain()
		return nil
	case tcell.KeyTab:
		r.toggleGitBreachFocus()
		return nil
	case tcell.KeyBacktab:
		r.reverseToggleGitBreachFocus()
		return nil
	case tcell.KeyUp:
		r.scrollGitBreachDiffSplit(-1, 0)
		return nil
	case tcell.KeyDown:
		r.scrollGitBreachDiffSplit(1, 0)
		return nil
	case tcell.KeyPgUp:
		r.scrollGitBreachDiffSplit(-r.gitBreachDiffSplitPageStep(), 0)
		return nil
	case tcell.KeyPgDn:
		r.scrollGitBreachDiffSplit(r.gitBreachDiffSplitPageStep(), 0)
		return nil
	case tcell.KeyLeft:
		if _, col := r.gitBreachDiffSplitLeft.GetScrollOffset(); col <= 0 {
			r.deactivateGitBreachMain()
			return nil
		}
		r.scrollGitBreachDiffSplit(0, -1)
		return nil
	case tcell.KeyRight:
		r.scrollGitBreachDiffSplit(0, 1)
		return nil
	case tcell.KeyRune:
		switch event.Rune() {
		case ' ':
			r.deactivateGitBreachMain()
			return nil
		case 'c':
			r.openGitBreachCommitPrompt()
			return nil
		case 'r':
			r.reloadGitBreach()
			return nil
		case 'v':
			r.toggleGitBreachDiffSplit()
			return nil
		}
	}
	return event
}

// captureGitBreachDiffSplitLeftMouse only intercepts the wheel — a
// click still needs to fall through to TextView's own default
// MouseHandler so the left pane keeps real keyboard focus the normal
// way a click on it already gives any other focusable widget.
func (r *Root) captureGitBreachDiffSplitLeftMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	switch action {
	case tview.MouseScrollUp:
		r.scrollGitBreachDiffSplit(-1, 0)
		return tview.MouseConsumed, nil
	case tview.MouseScrollDown:
		r.scrollGitBreachDiffSplit(1, 0)
		return tview.MouseConsumed, nil
	}
	return action, event
}

// captureGitBreachDiffSplitRightMouse mirrors the wheel the same way
// the left pane's own capture does, but redirects a click to the left
// pane instead of letting it fall through to this TextView's own
// default MouseHandler — which would otherwise give the right pane
// itself real keyboard focus, the one widget in split mode with no
// InputCapture of its own at all (see newGitBreachDiffSplit's own doc
// comment on why only the left pane ever holds focus), silently
// breaking every key the moment someone clicked the right half instead
// of the left.
func (r *Root) captureGitBreachDiffSplitRightMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	switch action {
	case tview.MouseScrollUp:
		r.scrollGitBreachDiffSplit(-1, 0)
		return tview.MouseConsumed, nil
	case tview.MouseScrollDown:
		r.scrollGitBreachDiffSplit(1, 0)
		return tview.MouseConsumed, nil
	case tview.MouseLeftDown:
		r.app.SetFocus(r.gitBreachDiffSplitLeft)
		return tview.MouseConsumed, nil
	}
	return action, event
}
