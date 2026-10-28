// scrollacknowledge.go is openScrollAcknowledgeNotice: a longer
// warning the user must actually scroll through before the confirming
// list item does anything — openConfirm's usual single-line question
// (see trash.go) isn't enough for a warning that needs to actually be
// read, not just reflexively accepted, per the user's own explicit
// request. Currently only reached from optionsscreen.go's
// activateOptionRow, for consigliere_hardcore_mode ("Machiavelli
// Mode") — but takes its title/message/confirmLabel as parameters
// rather than hardcoding that one case, the same "mechanism stays
// generic, only Options' own call site is specific" shape
// optionSpec.confirmEnable already has.
package ui

import (
	"errors"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// newScrollAcknowledgeNoticeViews builds every widget
// openScrollAcknowledgeNotice shows — called once from NewRoot;
// content (title/message/confirm label) is set fresh on every call to
// openScrollAcknowledgeNotice itself, the same "rebuilt on open"
// contract confirmDialog already follows.
func (r *Root) newScrollAcknowledgeNoticeViews() {
	r.scrollAcknowledgeTitleBar = newPlainTitleBar("")
	r.scrollAcknowledgeTitleBar.SetMouseCapture(r.captureScrollAcknowledgeTitleBarMouse)

	r.scrollAcknowledgeMessageView = tview.NewTextView()
	r.scrollAcknowledgeMessageView.SetWrap(true)
	r.scrollAcknowledgeMessageView.SetBorderPadding(0, 0, 1, 1)
	r.scrollAcknowledgeMessageView.SetInputCapture(r.captureScrollAcknowledgeMessageKey)
	r.scrollAcknowledgeScrollbar = newVerticalScrollbarForTextView(r, r.scrollAcknowledgeMessageView)

	r.scrollAcknowledgeList = tview.NewList().ShowSecondaryText(false)
	r.scrollAcknowledgeList.SetHighlightFullLine(true)
	r.scrollAcknowledgeList.SetBorderPadding(0, 0, 1, 1)
	r.scrollAcknowledgeList.AddItem("Cancel", "", 0, r.cancelScrollAcknowledgeNotice)
	r.scrollAcknowledgeList.AddItem("", "", 0, r.acceptScrollAcknowledgeNotice) // text set fresh by openScrollAcknowledgeNotice
	r.scrollAcknowledgeList.SetCurrentItem(0)
	r.scrollAcknowledgeList.SetDoneFunc(r.cancelScrollAcknowledgeNotice) // Escape
	r.scrollAcknowledgeList.SetInputCapture(r.captureScrollAcknowledgeListKey)

	messageRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.scrollAcknowledgeMessageView, 0, 1, true).
		AddItem(r.scrollAcknowledgeScrollbar, 1, 0, false)
	messageRow.SetMouseCapture(r.scrollAcknowledgeScrollbar.captureMouse)

	r.scrollAcknowledgeLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.scrollAcknowledgeTitleBar, 1, 0, false).
		AddItem(messageRow, 0, 1, true).
		AddItem(r.scrollAcknowledgeList, 2, 0, false)
}

// scrollAcknowledgeWidth/Height size the dialog as a fixed, bounded
// box — never grown to fit message, unlike confirmDialog's own
// headerWidth-driven sizing. That's exactly what let a too-long
// openConfirm message corrupt rendering before (see openConfirm's own
// doc comment on the colored-stripes bug): this dialog's whole point
// is a message too long for that shape, so it scrolls within a fixed
// area instead of ever growing past the screen in the first place.
const (
	scrollAcknowledgeWidth  = 76
	scrollAcknowledgeHeight = 10
)

// openScrollAcknowledgeNotice shows message inside a scrollable,
// fixed-size box titled title, with "Cancel" and confirmLabel as the
// two choices below it — confirmLabel only actually runs onConfirm
// once the user has scrolled message all the way to its own end (see
// canAcknowledgeScrollNotice); before that, selecting it just explains
// why it did nothing, rather than silently ignoring the attempt.
func (r *Root) openScrollAcknowledgeNotice(title, message, confirmLabel string, onConfirm func()) {
	r.scrollAcknowledgeOnConfirm = onConfirm
	r.scrollAcknowledgeMessageView.SetText(message)
	r.scrollAcknowledgeMessageView.ScrollToBeginning()
	r.scrollAcknowledgeList.SetItemText(1, confirmLabel, "")
	r.scrollAcknowledgeList.SetCurrentItem(0)

	_, _, screenWidth, screenHeight := r.GetRect()
	x, y, width, height := r.clampToScreen(
		(screenWidth-scrollAcknowledgeWidth)/2, (screenHeight-scrollAcknowledgeHeight)/2,
		scrollAcknowledgeWidth, scrollAcknowledgeHeight)

	// A real, reported bug: this title bar used to be plain text with
	// no close glyph at all, unlike every sibling dialog's own
	// (activityLogNotice, Log Holepunch, Help, ...) clickable "✕" —
	// toolWindowCloseButtonCol's own one-column-in-from-the-edge
	// convention, reused here rather than duplicated with a different
	// number (see renderConsigliereTitleBar's own identical shape).
	label := " " + title + " "
	closeCol := toolWindowCloseButtonCol(0, width)
	padding := closeCol - tview.TaggedStringWidth(label)
	if padding < 0 {
		padding = 0
	}
	r.scrollAcknowledgeTitleBar.SetText(label + strings.Repeat(" ", padding) + string(toolWindowCloseGlyph) + " ")

	r.scrollAcknowledgeLayout.SetRect(x, y, width, height)
	// Flex only resolves a child's own rect during Draw, never
	// synchronously when SetRect is called on the parent above (the
	// same gotcha drawConsigliereLayout's own doc comment documents
	// elsewhere) — but canAcknowledgeScrollNotice needs an accurate
	// messageView rect immediately, not after the next Draw, both to
	// size the scrollbar correctly on the very first frame and to work
	// at all under a test that never calls Draw. Set it explicitly
	// here instead of waiting on Flex, using this layout's own fixed
	// proportions (1 title row, 2 list rows, everything else to the
	// message/scrollbar row) — unlike openConfirm's confirmDialog,
	// which has no scrollbar of its own and could get away with
	// reusing the full dialog rect purely for captureOutsideClick's
	// bounds check.
	const titleRows, listRows = 1, 2
	r.scrollAcknowledgeMessageView.SetRect(x, y+titleRows, width-1, height-titleRows-listRows)

	// activeWidget is the layout, not messageView, so
	// captureOutsideClick's bounds check still covers the title bar and
	// list rows too — restore explicitly focuses messageView instead
	// of pushOverlay's own default (SetFocus(widget), which would
	// focus the unfocusable Flex layout itself).
	r.pushOverlay(scrollAcknowledgePage, r.scrollAcknowledgeLayout, func() {
		r.app.SetFocus(r.scrollAcknowledgeMessageView)
	})
}

// canAcknowledgeScrollNotice reports whether the message has been
// scrolled all the way to its own end — or was never long enough to
// need scrolling in the first place (maxOffset of 0), which must also
// count as "read": a warning short enough to fit the box outright
// should never permanently lock the confirming choice out.
func (r *Root) canAcknowledgeScrollNotice() bool {
	max := r.scrollAcknowledgeScrollbar.maxOffset()
	if max == 0 {
		return true
	}
	row, _ := r.scrollAcknowledgeMessageView.GetScrollOffset()
	return row >= max
}

// acceptScrollAcknowledgeNotice is the confirmLabel item's own
// SelectedFunc: runs scrollAcknowledgeOnConfirm and closes only once
// canAcknowledgeScrollNotice is true — otherwise explains why nothing
// happened instead of leaving the attempt looking like a dead click.
func (r *Root) acceptScrollAcknowledgeNotice() {
	if !r.canAcknowledgeScrollNotice() {
		r.showTransientErrorOverOverlay(errors.New("scroll down to read the rest first"))
		return
	}
	onConfirm := r.scrollAcknowledgeOnConfirm
	r.scrollAcknowledgeOnConfirm = nil
	r.hideOverlay()
	if onConfirm != nil {
		onConfirm()
	}
}

// cancelScrollAcknowledgeNotice is "Cancel" and Escape alike: closes
// without ever running scrollAcknowledgeOnConfirm.
func (r *Root) cancelScrollAcknowledgeNotice() {
	r.scrollAcknowledgeOnConfirm = nil
	r.hideOverlay()
}

// captureScrollAcknowledgeMessageKey lets every ordinary scrolling key
// (arrows, Page Up/Down, Home/End) fall through to the TextView's own
// default handling — only Escape (cancel) and Tab (move on to
// Cancel/confirm) are this dialog's own concern here.
func (r *Root) captureScrollAcknowledgeMessageKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape:
		r.cancelScrollAcknowledgeNotice()
		return nil
	case tcell.KeyTab:
		r.app.SetFocus(r.scrollAcknowledgeList)
		return nil
	}
	return event
}

// captureScrollAcknowledgeTitleBarMouse makes the header's own "✕"
// act exactly like Cancel on a left click — the same shape
// captureConsigliereTitleBarMouse/activityLogNoticeTitleBarMouseCapture
// already establish elsewhere. A real, reported miss: this dialog's
// own close column sits much further from the left edge than those
// two (a centered, fixed-width box rather than one docked flush to a
// screen edge), and toolWindowCloseGlyph's own doc comment already
// documents this exact glyph rendering double-width in at least one
// real terminal before — accepting the one column after the
// "expected" single-width position too, not just an exact match,
// covers that without any downside (nothing else sits there to
// accidentally trigger).
func (r *Root) captureScrollAcknowledgeTitleBarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftDown {
		return action, event
	}
	x, y, width, _ := r.scrollAcknowledgeTitleBar.GetRect()
	clickX, clickY := event.Position()
	closeCol := x + toolWindowCloseButtonCol(0, width)
	if clickY == y && (clickX == closeCol || clickX == closeCol+1) {
		r.cancelScrollAcknowledgeNotice()
		return tview.MouseConsumed, nil
	}
	return action, event
}

// captureScrollAcknowledgeListKey is the message view's own Tab in
// reverse: Backtab returns focus to it, the simple two-stop ring this
// dialog needs (List's own Up/Down/Enter/Escape already do everything
// else via its default handler and SetDoneFunc).
func (r *Root) captureScrollAcknowledgeListKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyBacktab {
		r.app.SetFocus(r.scrollAcknowledgeMessageView)
		return nil
	}
	return event
}
