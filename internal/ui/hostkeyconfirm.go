package ui

import (
	"fmt"

	"github.com/rivo/tview"
)

// newHostKeyConfirmDialog builds the trust-on-first-use prompt once,
// from NewRoot — see askHostKeyTrust for when and why it's shown, and
// remotefs's own hostkey.go for why a host whose key *changed* never
// reaches this at all (no dialog exists for that — it's always
// rejected outright). "No, cancel" defaults selected, the same
// conservative "a plain Enter never accidentally trusts anything"
// default newConfirmDialog's own doc comment establishes for every
// other yes/no dialog in this app.
func (r *Root) newHostKeyConfirmDialog() *tview.List {
	l := tview.NewList().ShowSecondaryText(false)
	l.SetHighlightFullLine(true)
	l.SetBorderPadding(0, 0, 1, 1)
	l.AddItem("Yes, trust and connect", "", 0, func() { r.resolveHostKeyConfirm(true) })
	l.AddItem("No, cancel", "", 0, func() { r.resolveHostKeyConfirm(false) })
	l.SetCurrentItem(1)
	l.SetDoneFunc(func() { r.resolveHostKeyConfirm(false) }) // Escape declines
	return l
}

func (r *Root) newHostKeyConfirmLayout() *tview.Flex {
	r.hostKeyConfirmTitleBar = newPlainTitleBar("")
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.hostKeyConfirmTitleBar, 1, 0, false).
		AddItem(r.hostKeyConfirmDialog, 0, 1, true)
}

// askHostKeyTrust implements remotefs.HostKeyPrompt (see
// connectdialog.go's own runConnect, which passes this as
// DialOptions.HostKeyPrompt) — called on the background Dial
// goroutine, never the main one. It queues the confirmation dialog
// onto the main loop via QueueUpdateDraw and then blocks *this* call's
// own goroutine on a fresh, single-use channel until
// resolveHostKeyConfirm answers it, one way or the other, from a
// button click running on the main loop.
//
// This is safe precisely because the two goroutines involved — this
// call's own, and tview's main loop — are never the same one: there is
// nothing here for the two to deadlock against each other over, the
// same reasoning that makes QueueUpdateDraw itself safe to call from a
// background goroutine at all (see sedreplace.go's own doc comment on
// why runSedPreview reports back through it rather than touching
// widgets directly).
func (r *Root) askHostKeyTrust(hostname, keyType, fingerprint string) (bool, error) {
	resp := make(chan bool, 1)
	r.app.QueueUpdateDraw(func() {
		r.hostKeyConfirmResponse = resp

		message := fmt.Sprintf("Unknown host key for %s (%s %s) — trust it and connect?", hostname, keyType, fingerprint)
		r.hostKeyConfirmTitleBar.SetText(" " + message + " ")

		width, _ := listSize(r.hostKeyConfirmDialog)
		if headerWidth := tview.TaggedStringWidth(r.hostKeyConfirmTitleBar.GetText(false)); headerWidth > width {
			width = headerWidth
		}
		height := r.hostKeyConfirmDialog.GetItemCount() + 1 // +1: the title bar's own row
		_, _, screenWidth, screenHeight := r.GetRect()      // Root fills the whole screen
		if width > screenWidth-4 {
			width = screenWidth - 4
		}
		if height > screenHeight-4 {
			height = screenHeight - 4
		}
		x := (screenWidth - width) / 2
		y := (screenHeight - height) / 2

		r.hostKeyConfirmLayout.SetRect(x, y, width, height)
		r.hostKeyConfirmDialog.SetCurrentItem(1)
		// Layered on top of the still-open Connect dialog, not replacing
		// it (see pushOverlay) — the connection attempt this is deciding
		// belongs to that dialog, which must still be there underneath
		// once this closes, one way or the other.
		r.pushOverlay(hostKeyConfirmPage, r.hostKeyConfirmLayout, nil)
	})
	return <-resp, nil
}

// resolveHostKeyConfirm answers whichever askHostKeyTrust call is
// currently waiting, if any (there's ever only one at a time — a
// second Dial can't start until the first one's own runConnect call
// has returned), and closes the dialog.
func (r *Root) resolveHostKeyConfirm(accept bool) {
	r.hideOverlay()
	if r.hostKeyConfirmResponse != nil {
		r.hostKeyConfirmResponse <- accept
		r.hostKeyConfirmResponse = nil
	}
}
