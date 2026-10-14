package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/compare"
	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/viewer"
)

const compareDiffPage = "compare-diff"

// compareDiffLinkedGlyph/compareDiffUnlinkedGlyph are the title bar's
// own toggle button, two states of one glyph the same way the header's
// file-icons button is (see panel.go's fileIconsOnGlyph) rather than a
// static icon plus a separate on/off indicator. '=' reads as "these two
// move together"; '≠' (NOT EQUAL TO, U+2260) as "these two are free to
// differ" — both plain, single-width, and common in any monospace
// terminal font, unlike this app's own reload glyph (U+2B6F, see
// toolWindowReloadGlyph's doc comment on its rendering gaps).
const (
	compareDiffLinkedGlyph   = '='
	compareDiffUnlinkedGlyph = '≠'
)

// newCompareDiffScreen builds the side-by-side diff view: a title bar
// (the link/unlink toggle and a close button, see
// renderCompareDiffTitleBar) and two independently scrollable panes
// separated by one column of a plain vertical rule — the same
// list-plus-chrome shape compareTree's own full screen already
// establishes, except the "list" here is two TextViews instead of one
// Table, since a side-by-side diff has two columns of content, not one
// row per item.
//
// Built once here, contents reset on each open (see openCompareDiff) —
// the same build-once/repopulate-on-open shape every other screen in
// this app already follows.
func (r *Root) newCompareDiffScreen() {
	r.compareDiffLeft = tview.NewTextView()
	r.compareDiffLeft.SetDynamicColors(true)
	r.compareDiffLeft.SetWrap(false)
	r.compareDiffLeft.SetBorderPadding(0, 0, 1, 1)
	r.compareDiffLeft.SetInputCapture(r.captureCompareDiffPaneKey(true))
	r.compareDiffLeft.SetMouseCapture(r.captureCompareDiffPaneMouse(true))
	r.compareDiffLeft.SetFocusFunc(func() { r.compareDiffLeftFocused = true })

	r.compareDiffRight = tview.NewTextView()
	r.compareDiffRight.SetDynamicColors(true)
	r.compareDiffRight.SetWrap(false)
	r.compareDiffRight.SetBorderPadding(0, 0, 1, 1)
	r.compareDiffRight.SetInputCapture(r.captureCompareDiffPaneKey(false))
	r.compareDiffRight.SetMouseCapture(r.captureCompareDiffPaneMouse(false))
	r.compareDiffRight.SetFocusFunc(func() { r.compareDiffLeftFocused = false })

	r.compareDiffSeparator = tview.NewBox()
	r.compareDiffSeparator.SetDrawFunc(r.drawCompareDiffSeparator)
	// Box's own default MouseHandler calls setFocus(b) on any
	// MouseLeftDown within its rect — real behavior, not a
	// hypothetical: a plain Box with no mouse capture of its own
	// otherwise silently steals keyboard focus onto itself (a bare Box
	// with no SetInputCapture at all) the moment a click lands on this
	// one-column-wide bar, or even just slightly misses one of the two
	// panes next to it. After that every key does nothing at all,
	// while the mouse wheel keeps working (it hit-tests by screen
	// position, not by focus) — exactly the "scrolling only works with
	// the mouse" the user reported. gitBreachBlockFocusSteal (shared,
	// not gitbreach-specific despite the name) is this app's own
	// established fix for the identical problem on every other purely
	// decorative widget.
	r.compareDiffSeparator.SetMouseCapture(gitBreachBlockFocusSteal)

	r.compareDiffBody = tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.compareDiffLeft, 0, 1, true).
		AddItem(r.compareDiffSeparator, 1, 0, false).
		AddItem(r.compareDiffRight, 0, 1, false)

	r.compareDiffTitleBar = newPlainTitleBar("Diff")
	r.compareDiffTitleBar.SetMouseCapture(r.captureCompareDiffTitleBarMouse())

	// The bottom hint bar — per the user's own explicit request after
	// reporting the link/unlink toggle's own glyph was unclear on its
	// own: every key this screen answers to, spelled out in words, the
	// same shared bottom-hint-bar shape every other full-screen list in
	// this app already uses (see listhint.go's own package doc
	// comment), not a screen-specific one-off.
	r.compareDiffHint = tview.NewTextView()
	r.compareDiffHint.SetWrap(false)
	r.compareDiffHint.SetDynamicColors(true)
	r.compareDiffHint.SetMouseCapture(r.captureListHintMouse(r.compareDiffHint, &r.compareDiffHintSpans))

	r.compareDiffLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.compareDiffTitleBar, 1, 0, false).
		AddItem(r.compareDiffBody, 0, 1, true).
		AddItem(r.compareDiffHint, 1, 0, false)
}

// compareDiffHintEntries is the bottom hint bar's own content —
// Up/Down/PgUp/PgDn/Left/Right are dispatched via simulateKeyOnFocused
// rather than calling scrollCompareDiff directly: a click always lands
// on whichever pane currently has real keyboard focus, and routing it
// through the exact same InputHandler a real keypress reaches (see
// captureCompareDiffPaneKey, which wraps that handler) means a hint-bar
// click and the matching real key can never drift apart from each
// other. A method on Root, not a bare function, the same as
// gitBreachHintEntries — the link/unlink entry below needs to read
// r.compareDiffLinked to show the glyph that's actually active right
// now.
func (r *Root) compareDiffHintEntries() []listHintEntry {
	return []listHintEntry{
		{keys: []listHintKey{
			{"↑", simulateKeyOnFocused(tcell.KeyUp)},
			{"↓", simulateKeyOnFocused(tcell.KeyDown)},
		}, label: "scroll"},
		{keys: []listHintKey{
			{"PgUp", simulateKeyOnFocused(tcell.KeyPgUp)},
			{"PgDn", simulateKeyOnFocused(tcell.KeyPgDn)},
		}, label: "page"},
		{keys: []listHintKey{
			{"←", simulateKeyOnFocused(tcell.KeyLeft)},
			{"→", simulateKeyOnFocused(tcell.KeyRight)},
		}, label: "scroll sideways"},
		hintKey("Tab", "switch pane", func(r *Root) { r.cycleCompareDiffFocus() }),
		r.compareDiffLinkHintEntry(),
		hintKey("Esc", "close", func(r *Root) { r.closeCompareDiff() }),
	}
}

// compareDiffLinkHintEntry is the hint bar's own link/unlink toggle
// entry — its key literally IS the current glyph (see
// compareDiffLinkedGlyph/compareDiffUnlinkedGlyph), the same state the
// title bar's own button already shows, so the two never say something
// different; the label spells out what that glyph means in words
// instead of leaving it to speak for itself, per the user's own
// explicit report that the bare glyph alone wasn't clear.
func (r *Root) compareDiffLinkHintEntry() listHintEntry {
	glyph := string(compareDiffUnlinkedGlyph)
	label := "link scroll (currently independent)"
	if r.compareDiffLinked {
		glyph = string(compareDiffLinkedGlyph)
		label = "unlink scroll (currently linked)"
	}
	return hintKey(glyph, label, func(r *Root) { r.toggleCompareDiffLinked() })
}

// renderCompareDiffHint rebuilds the hint bar's own text/spans from
// compareDiffHintEntries' own current, link-state-dependent list —
// mirrors refreshGitBreachHint exactly, called on open, on every
// link/unlink toggle, and on a theme switch.
func (r *Root) renderCompareDiffHint() {
	hintText, hintSpans := buildListHint(r.theme, r.compareDiffHintEntries())
	r.compareDiffHint.SetText(hintText)
	r.compareDiffHintSpans = hintSpans
}

// drawCompareDiffSeparator paints a plain vertical rule the full height
// of its own one-column-wide Box — the character the user's own
// reference screenshot shows between the two panes, kept as a real
// drawn glyph rather than baked into either TextView's own text, so it
// stays exactly one column wide regardless of how either pane's own
// content scrolls horizontally.
func (r *Root) drawCompareDiffSeparator(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
	style := tcell.StyleDefault.Foreground(r.theme.MutedTextColor).Background(r.compareDiffSeparator.GetBackgroundColor())
	for row := y; row < y+height; row++ {
		screen.SetContent(x, row, '│', nil, style)
	}
	return x, y, width, height
}

// applyCompareDiffTheme themes the diff view — the same
// SurfaceBackground/InputFocusedBackground convention every other
// overlay here already follows (see applyCompareFileTheme). Also
// re-renders both panes and the title bar: the diff coloring itself
// (see renderCompareDiff) picks dark/light syntax colors from the
// background, and the title bar's own padding depends on
// SetBackgroundColor having already run, same ordering
// applyCompareFileTheme already follows for its own buttons.
func (r *Root) applyCompareDiffTheme(theme config.ResolvedTheme) {
	if r.compareDiffLayout == nil {
		return
	}
	r.compareDiffLayout.SetBackgroundColor(theme.SurfaceBackground)
	r.compareDiffLeft.SetBackgroundColor(theme.SurfaceBackground)
	r.compareDiffLeft.SetTextColor(theme.TextColor)
	r.compareDiffRight.SetBackgroundColor(theme.SurfaceBackground)
	r.compareDiffRight.SetTextColor(theme.TextColor)
	r.compareDiffSeparator.SetBackgroundColor(theme.SurfaceBackground)
	r.compareDiffTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.compareDiffTitleBar.SetTextColor(theme.TextColor)
	r.compareDiffHint.SetBackgroundColor(theme.InputBackground)
	r.compareDiffHint.SetTextColor(theme.MutedTextColor)
	if r.compareDiffPairs != nil {
		r.renderCompareDiff()
	}
	r.renderCompareDiffTitleBar()
	r.renderCompareDiffHint()
}

// openCompareDiff is "Show diff": a real line-by-line answer for the
// currently-open file pair, side by side (see internal/compare's own
// doc comment for why that's compare.SideBySide, in-process, rather
// than the system's diff(1) this used to shell out to via the Look
// pager). Hidden entirely for a binary pair or a directory before this
// is ever reached (see renderCompareFile's own SetDisabled call); an
// identical pair still gets here — nothing upstream rules it out — and
// is handled below as its own case rather than silently opening an
// empty view.
func (r *Root) openCompareDiff() {
	a, err := os.ReadFile(r.compareA)
	if err != nil {
		r.showError(fmt.Errorf("compare: %w", err))
		return
	}
	b, err := os.ReadFile(r.compareB)
	if err != nil {
		r.showError(fmt.Errorf("compare: %w", err))
		return
	}
	if string(a) == string(b) {
		r.showError(fmt.Errorf("compare: %s and %s have no differences", filepath.Base(r.compareA), filepath.Base(r.compareB)))
		return
	}

	r.compareDiffPairs = compare.SideBySide(string(a), string(b))
	r.compareDiffLinked = true
	r.compareDiffLeftFocused = true
	r.renderCompareDiff()
	r.renderCompareDiffTitleBar()
	r.renderCompareDiffHint()
	r.compareDiffLeft.ScrollToBeginning()
	r.compareDiffRight.ScrollToBeginning()
	r.showOverlayWithRestore(compareDiffPage, r.compareDiffLayout, func() { r.app.SetFocus(r.compareDiffLeft) })
}

// closeCompareDiff hides the diff view — Escape, the title bar's own
// close glyph, or a click outside, the same as every other overlay.
func (r *Root) closeCompareDiff() {
	r.hideOverlay()
}

// renderCompareDiff fills both panes from compareDiffPairs — one row
// per LinePair, always the same row on both sides (see LinePair's own
// doc comment: every pair produces exactly one line on each side, blank
// where PairOnlyLeft/PairOnlyRight has nothing on that side), which is
// what lets scrollCompareDiff sync the two panes by raw row index alone
// rather than two independently-numbered line sets that merely happen
// to correspond.
//
// Diff coloring is gitbreach.go's own two-channel scheme
// (gitBreachColorizeDiffLine), reused rather than reinvented per the
// user's own explicit request to match that screen's look: a changed/
// removed/added line's whole background tints (gitBreachDiffAddedBackground/
// gitBreachDiffRemovedBackground), and its content is real, per-language
// syntax highlighting (internal/viewer.Highlight + renderSyntax) rather
// than one solid diff-add/diff-delete foreground color — background
// carries "this line changed", foreground carries "what this code
// actually is". A context line (PairSame) gets the same per-language
// coloring with no background tint, exactly like gitbreach's own
// context lines. Left and right panes are lexed against compareA/
// compareB respectively — almost always the same language, but never
// assumed to be: a .go-vs-.go.bak compare should still highlight both
// sides as Go.
//
// Same known limitation as gitbreach's own doc comment on
// gitBreachColorizeDiff spells out: each line is lexed in isolation, so
// a lexer construct spanning several lines (a block comment, a raw
// string literal) can come out misclassified at its own boundaries.
func (r *Root) renderCompareDiff() {
	palette := paletteFor(r.theme.SurfaceBackground)

	var left, right strings.Builder
	for _, p := range r.compareDiffPairs {
		leftBg, rightBg := tcell.ColorDefault, tcell.ColorDefault
		haveLeftBg, haveRightBg := false, false
		switch p.Kind {
		case compare.PairChanged:
			leftBg, haveLeftBg = gitBreachDiffRemovedBackground, true
			rightBg, haveRightBg = gitBreachDiffAddedBackground, true
		case compare.PairOnlyLeft:
			leftBg, haveLeftBg = gitBreachDiffRemovedBackground, true
		case compare.PairOnlyRight:
			rightBg, haveRightBg = gitBreachDiffAddedBackground, true
		}
		writeCompareDiffLine(&left, p.LeftNo, p.Left, r.compareA, palette, leftBg, haveLeftBg)
		writeCompareDiffLine(&right, p.RightNo, p.Right, r.compareB, palette, rightBg, haveRightBg)
	}
	r.compareDiffLeft.SetText(left.String())
	r.compareDiffRight.SetText(right.String())
}

// compareDiffGutterWidth is how many columns the line-number gutter
// gets — four digits plus one space covers any file up to 9999 lines
// without the gutter itself ever needing to resize mid-view; a longer
// file still lines up (fmt's own %4d just stops padding), it just loses
// the fixed-width alignment past that point, same tradeoff
// compareHeaderHeight's own fixed row budget already accepts elsewhere
// in this package for a similarly rare edge case.
const compareDiffGutterWidth = 4

// writeCompareDiffLine appends one line to b: a right-aligned line
// number (blank, not "0", when lineNo is 0 — see LinePair's own doc
// comment on what that means), a space, then content lexed against path
// via viewer.Highlight/renderSyntax (tview.Escape happens inside
// renderSyntax itself — see its own doc comment — so content is never
// escaped twice). When haveBg, the whole content (gutter excluded, the
// same choice gitBreachColorizeDiffLine makes for its own leading
// marker column) is wrapped in a background-only tag
// ("[:#rrggbb:]...[-:-:-]", panel.go's own nameHighlightTags shape) so
// the tint and the per-language foreground coloring never fight over
// the same tag.
func writeCompareDiffLine(b *strings.Builder, lineNo int, content, path string, palette syntaxPalette, bg tcell.Color, haveBg bool) {
	if lineNo > 0 {
		fmt.Fprintf(b, "%*d ", compareDiffGutterWidth, lineNo)
	} else {
		b.WriteString(strings.Repeat(" ", compareDiffGutterWidth+1))
	}
	highlighted := renderSyntax(viewer.Highlight(path, content), palette)
	if haveBg {
		fmt.Fprintf(b, "[:#%06x:]%s[-:-:-]", uint32(bg.Hex())&0xffffff, highlighted)
	} else {
		b.WriteString(highlighted)
	}
	b.WriteString("\n")
}

// renderCompareDiffTitleBar sets the title bar's own text: the label,
// the link/unlink toggle glyph (see compareDiffLinkedGlyph), then the
// close glyph — the same renderReloadCloseTitleBar shape (root.go),
// specialized for this screen's own two actions instead of
// reload/close, since neither of those apply to a diff that's already
// fully computed and has no state of its own to reload.
func (r *Root) renderCompareDiffTitleBar() {
	if r.compareDiffTitleBar == nil {
		return
	}
	glyph := compareDiffUnlinkedGlyph
	if r.compareDiffLinked {
		glyph = compareDiffLinkedGlyph
	}
	label := " Diff "
	width := r.lastScreenWidth
	col := compareDiffCloseButtonCol(width) - 2
	padding := col - tview.TaggedStringWidth(label)
	if padding < 0 {
		padding = 0
	}
	r.compareDiffTitleBar.SetText(label + strings.Repeat(" ", padding) + string(glyph) + " " + string(toolWindowCloseGlyph) + " ")
}

// compareDiffCloseButtonCol mirrors closeTitleBarButtonCol (root.go):
// one column in from the right edge.
func compareDiffCloseButtonCol(width int) int {
	return width - 2
}

// compareDiffLinkButtonCol is the toggle glyph's own column, immediately
// left of the close glyph with one blank column of breathing room
// between them — the same spacing reloadCloseTitleBarButtonCol already
// uses for its own pair of buttons.
func compareDiffLinkButtonCol(width int) int {
	return compareDiffCloseButtonCol(width) - 2
}

// captureCompareDiffTitleBarMouse dispatches a click on either of the
// title bar's own two glyphs — the same
// captureReloadCloseTitleBarMouse shape (root.go), specialized for
// toggleCompareDiffLinked/closeCompareDiff instead of reload/close.
func (r *Root) captureCompareDiffTitleBarMouse() func(tview.MouseAction, *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	return func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseLeftClick {
			x, y := event.Position()
			rectX, rectY, width, _ := r.compareDiffTitleBar.GetRect()
			if y == rectY {
				switch x - rectX {
				case compareDiffCloseButtonCol(width):
					r.closeCompareDiff()
					return tview.MouseConsumed, nil
				case compareDiffLinkButtonCol(width):
					r.toggleCompareDiffLinked()
					return tview.MouseConsumed, nil
				}
			}
		}
		// Any other click on the bar (not one of the two glyphs above)
		// is blocked here too, same reasoning as
		// compareDiffSeparator's own mouse capture: a plain TextView's
		// default MouseHandler also calls setFocus(t) unconditionally
		// on MouseLeftDown, which would otherwise steal keyboard focus
		// onto this bar — a TextView with no SetInputCapture of its
		// own — the instant a click lands anywhere on it but those two
		// glyphs.
		return gitBreachBlockFocusSteal(action, event)
	}
}

// toggleCompareDiffLinked flips whether the two panes scroll together
// (see scrollCompareDiff) — syncing the inactive pane to the active
// one's own current position right away, rather than leaving them
// visibly out of step until the next scroll, the same "catch up
// immediately" expectation a user clicking a sync toggle already has
// elsewhere (e.g. a diff tool's own side-by-side view, which the user's
// own reference screenshot is one example of).
func (r *Root) toggleCompareDiffLinked() {
	r.compareDiffLinked = !r.compareDiffLinked
	if r.compareDiffLinked {
		row, col := r.activeCompareDiffPane().GetScrollOffset()
		r.inactiveCompareDiffPane().ScrollTo(row, col)
	}
	r.renderCompareDiffTitleBar()
	r.renderCompareDiffHint()
}

// activeCompareDiffPane/inactiveCompareDiffPane return the focused pane
// and its own other half — compareDiffLeftFocused is kept up to date by
// each TextView's own SetFocusFunc (see newCompareDiffScreen), not by
// re-deriving it from tview's own focus tree on every call.
func (r *Root) activeCompareDiffPane() *tview.TextView {
	if r.compareDiffLeftFocused {
		return r.compareDiffLeft
	}
	return r.compareDiffRight
}

func (r *Root) inactiveCompareDiffPane() *tview.TextView {
	if r.compareDiffLeftFocused {
		return r.compareDiffRight
	}
	return r.compareDiffLeft
}

// captureCompareDiffPaneKey is the diff view's own shared key handling,
// installed on both panes: Tab/Shift+Tab swap focus between them,
// Escape closes, Up/Down/PgUp/PgDn scroll (see scrollCompareDiff) —
// intercepted here rather than left to TextView's own default
// handling, since a linked scroll has to move both panes together,
// which TextView's own InputHandler has no way to do on its own.
// isLeft tells scrollCompareDiff which pane this capture belongs to,
// since the same function is shared between both panes' own
// SetInputCapture.
func (r *Root) captureCompareDiffPaneKey(isLeft bool) func(event *tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab, tcell.KeyBacktab:
			r.cycleCompareDiffFocus()
			return nil
		case tcell.KeyEscape:
			r.closeCompareDiff()
			return nil
		case tcell.KeyUp:
			r.scrollCompareDiff(isLeft, -1)
			return nil
		case tcell.KeyDown:
			r.scrollCompareDiff(isLeft, 1)
			return nil
		case tcell.KeyPgUp:
			r.scrollCompareDiff(isLeft, -r.compareDiffPageStep())
			return nil
		case tcell.KeyPgDn:
			r.scrollCompareDiff(isLeft, r.compareDiffPageStep())
			return nil
		case tcell.KeyRune:
			// Both glyphs toggle, regardless of which one is currently
			// showing — a user reaching for the key the hint bar/title
			// bar just displayed shouldn't have to notice it flipped to
			// the other one first.
			if event.Rune() == compareDiffLinkedGlyph || event.Rune() == compareDiffUnlinkedGlyph {
				r.toggleCompareDiffLinked()
				return nil
			}
		}
		return event
	}
}

// compareDiffPageStep is how many rows PgUp/PgDn moves — the pane's own
// current height, the same "one screenful" convention Page Up/Down
// anywhere else in this app already follows, or a small fixed fallback
// before the first Draw has ever sized the pane (GetRect returns 0,0,0,0
// until then — the same defensive floor renderReloadTitleBar's own doc
// comment already explains for an identical reason).
func (r *Root) compareDiffPageStep() int {
	_, _, _, height := r.compareDiffLeft.GetRect()
	if height <= 0 {
		return 10
	}
	return height
}

// cycleCompareDiffFocus swaps tview's own focus between the two panes —
// there are only ever two, so "the other one" needs no index math the
// way cycleCompareFocus's own button list does.
func (r *Root) cycleCompareDiffFocus() {
	if r.compareDiffLeftFocused {
		r.app.SetFocus(r.compareDiffRight)
		return
	}
	r.app.SetFocus(r.compareDiffLeft)
}

// scrollCompareDiff moves the pane named by isLeft by delta rows
// (negative scrolls up) and, if the two panes are currently linked,
// moves the other pane to the exact same row — never by delta itself a
// second time, which would double the movement on whichever pane isn't
// the one the key landed on. TextView's own Draw clamps an
// out-of-range row back into bounds on its own (see tview's textview.go
// — row is never validated here), so delta can run past either edge
// safely.
func (r *Root) scrollCompareDiff(isLeft bool, delta int) {
	pane := r.compareDiffRight
	if isLeft {
		pane = r.compareDiffLeft
	}
	row, col := pane.GetScrollOffset()
	row += delta
	pane.ScrollTo(row, col)
	if !r.compareDiffLinked {
		return
	}
	other := r.compareDiffLeft
	if isLeft {
		other = r.compareDiffRight
	}
	other.ScrollTo(row, col)
}

// captureCompareDiffPaneMouse intercepts the mouse wheel on one pane —
// TextView's own default MouseHandler scrolls only the pane the wheel
// event landed on, with no notion of the other pane at all, which is
// exactly how a click can still correctly change focus (see below) but
// also exactly why linked mode was silently not holding for the wheel:
// a real, reported bug, not a hypothetical one. Routed through
// scrollCompareDiff — the same function Up/Down/PgUp/PgDn already use —
// so the wheel and the keyboard can never drift apart on what "linked"
// means. Every other action (MouseLeftDown in particular, which must
// still focus whichever pane was actually clicked) passes through to
// TextView's own default handling unchanged.
func (r *Root) captureCompareDiffPaneMouse(isLeft bool) func(tview.MouseAction, *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	return func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		switch action {
		case tview.MouseScrollUp:
			r.scrollCompareDiff(isLeft, -1)
			return tview.MouseConsumed, nil
		case tview.MouseScrollDown:
			r.scrollCompareDiff(isLeft, 1)
			return tview.MouseConsumed, nil
		}
		return action, event
	}
}
