// messagedetail.go is the Messages screen's own detail view (Stufe 5
// of feature_ideas.txt's "3a. Benachrichtigungen"): Enter or a click on
// the Message cell (see activateMessagesCell) shows that row's full,
// untruncated text. A small, standalone modal — feature_ideas.txt's
// own third, simplest option (over generalizing the existing Details
// sidebar, or building an optically identical copy of it), since
// neither of the other two touches anything this feature otherwise
// needs.
package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// messagesDetailMaxWidth caps how wide the modal grows before its own
// text starts wrapping instead — errorViewMaxWidth's own reasoning
// (errors.go), reused here for the same kind of centered, screen-sized
// overlay.
const messagesDetailMaxWidth = 70

// newMessageDetailScreen builds the modal once, at startup — the same
// build-once/repopulate-on-open shape every other overlay here uses.
// Escape closes it (see captureMessageDetailKey); nothing in it is
// otherwise interactive.
func (r *Root) newMessageDetailScreen() {
	r.messagesDetailTitleBar = newPlainTitleBar("Message")

	r.messagesDetailView = tview.NewTextView()
	r.messagesDetailView.SetWrap(true)
	r.messagesDetailView.SetBorderPadding(0, 0, 1, 1)
	r.messagesDetailView.SetInputCapture(r.captureMessageDetailKey)

	r.messagesDetailLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.messagesDetailTitleBar, 1, 0, false).
		AddItem(r.messagesDetailView, 0, 1, true)
}

// openMessageDetail shows row's own full Message text, centered over
// the whole screen — clampToScreen, the same reasoning showError's own
// doc comment gives: this is meant to read as centered on the whole
// terminal, not anchored to wherever the Messages screen itself
// happens to be laid out.
func (r *Root) openMessageDetail(row int) {
	m, ok := r.messageAt(row)
	if !ok {
		return
	}

	// The same computed width errorWidth() uses for showError: capped at
	// messagesDetailMaxWidth, or less if the screen itself is narrower —
	// already leaving room for the overlay's own 1-column padding on
	// each side (see errorWidth's own doc comment), so wrapText below
	// needs no further reduction of its own.
	width := messagesDetailMaxWidth
	if _, _, screenWidth, _ := r.GetRect(); screenWidth > 2 && width > screenWidth-2 {
		width = screenWidth - 2
	}
	text := strings.Join(wrapText(m.Text, width), "\n")
	r.messagesDetailView.SetText(text)
	r.messagesDetailTitleBar.SetText(" " + activityLogTimeText(m.Time) + " ")

	textWidth, textHeight := textSize(text)
	height := textHeight + 1 // +1 for the title bar row
	_, _, screenWidth, screenHeight := r.GetRect()
	x, y, boxWidth, boxHeight := r.clampToScreen((screenWidth-textWidth)/2, (screenHeight-height)/2, textWidth, height)

	r.messagesDetailLayout.SetRect(x, y, boxWidth, boxHeight)
	r.pushOverlay(messagesDetailPage, r.messagesDetailLayout, nil)
}

func (r *Root) closeMessageDetail() {
	r.hideOverlay()
}

// captureMessageDetailKey: Escape is the only key this modal itself
// handles — everything else falls through to TextView's own default
// (e.g. arrow-key scrolling, for a message long enough to need it).
func (r *Root) captureMessageDetailKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeMessageDetail()
		return nil
	}
	return event
}

// applyMessageDetailTheme themes the detail modal — called from
// applyMessagesTheme (messages.go), the same split every other
// overlay-with-its-own-sub-dialog here uses (e.g. Firewall's own
// Add-rule form).
func (r *Root) applyMessageDetailTheme(theme config.ResolvedTheme) {
	if r.messagesDetailView == nil {
		return
	}
	r.messagesDetailLayout.SetBackgroundColor(theme.PopupBackground)
	r.messagesDetailView.SetBackgroundColor(theme.PopupBackground)
	r.messagesDetailView.SetTextColor(theme.TextColor)
	r.messagesDetailTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.messagesDetailTitleBar.SetTextColor(theme.TextColor)
}
