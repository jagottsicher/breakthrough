package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const detailsSidebarPage = "details-sidebar"

// detailsSidebarMinWidth is detailsSidebarSize's own floor for a
// terminal too narrow for a genuine one-third share to still be usable
// — same shape as helpMinWidth/helpMinHeight (see help.go).
const detailsSidebarMinWidth = 24

// newDetailsSidebarView builds the Details sidebar: for now just an
// empty, scrollable TextView — the container this feature will grow its
// real, file-type-dependent content into later (recursive folder sizes,
// aggregated multi-selection totals, and so on), deliberately built and
// wired up on its own first, before any of that content exists.
//
// Unlike every other overlay in this app (Properties, Search, Help, the
// Look pager, ...), this one is not modal: it's meant to stay visible
// and live-updating while the user keeps browsing and selecting in the
// still-focused, still-fully-interactive panel underneath — the same
// way Midnight Commander's own "Info" panel mode works, rather than
// taking over the screen the way a dialog does. So it deliberately
// doesn't go through showOverlay/pushOverlay (which both hand keyboard
// focus to the widget they show) or get tracked in
// activePage/overlayStack — showDetailsSidebar/hideDetailsSidebar/
// toggleDetailsSidebar below are its own, separate, focus-preserving
// show/hide path.
func (r *Root) newDetailsSidebarView() *tview.TextView {
	v := tview.NewTextView()
	v.SetDynamicColors(true)
	v.SetWrap(true)
	v.SetBorderPadding(0, 0, 1, 1)
	v.SetText("Details") // placeholder — real, file-type-dependent content is a later step
	v.SetMouseCapture(r.captureDetailsSidebarMouse)
	return v
}

// detailsSidebarSize sizes the sidebar against the whole screen (Root's
// own rect, like helpSize — see clampToScreen's own doc comment on why
// Help uses the same base instead of clampToPanel): at least a third of
// the screen's width, and — for now — its full height, top to bottom.
// Leaving two rows clear at the top and three at the bottom (to stay
// clear of the header and the button bar/status bar/bash console rows)
// is a deliberate follow-up, not done here yet.
func (r *Root) detailsSidebarSize() (width, height int) {
	_, _, screenWidth, screenHeight := r.GetRect()
	width = screenWidth / 3
	if width < detailsSidebarMinWidth {
		width = detailsSidebarMinWidth
	}
	return width, screenHeight
}

// showDetailsSidebar positions the sidebar flush against the right edge
// of the screen (see detailsSidebarSize) and shows it, without touching
// keyboard focus — see newDetailsSidebarView's own doc comment on why
// that matters here specifically. SendToFront matters for the same
// reason it does in pushOverlay: ShowPage alone doesn't reorder Pages'
// own internal draw order, so without it the sidebar could end up drawn
// underneath — and fully hidden by — some other page registered after
// it in NewRoot.
func (r *Root) showDetailsSidebar() {
	width, height := r.detailsSidebarSize()
	_, _, screenWidth, _ := r.GetRect()
	x, y, width, height := r.clampToScreen(screenWidth-width, 0, width, height)
	r.detailsSidebar.SetRect(x, y, width, height)
	r.ShowPage(detailsSidebarPage)
	r.SendToFront(detailsSidebarPage)
	r.detailsSidebarVisible = true
}

// hideDetailsSidebar closes the sidebar — showDetailsSidebar's own
// counterpart.
func (r *Root) hideDetailsSidebar() {
	r.HidePage(detailsSidebarPage)
	r.detailsSidebarVisible = false
}

// toggleDetailsSidebar is the Details button's own action (see
// runButtonBarAction) — called directly and unguarded, the same "a
// click is always deliberate" reasoning every other button click
// already gets. ToggleDetailsSidebarShortcut (Ctrl+D) is this plus the
// same acceptsGlobalShortcut precondition every other keyboard shortcut
// checks — see its own doc comment for why that one specifically can't
// skip it the way this can.
func (r *Root) toggleDetailsSidebar() {
	if r.detailsSidebarVisible {
		r.hideDetailsSidebar()
	} else {
		r.showDetailsSidebar()
	}
}

// ToggleDetailsSidebarShortcut is Ctrl+D's own action — see
// cmd/breakthrough, which falls through to bashLine's own handling
// (returns the event, not nil) whenever acceptsGlobalShortcut reports
// false, the same as Ctrl+T/Ctrl+S/Ctrl+P/Ctrl+B: tview's own TextArea
// binds Ctrl+D to "delete forward" (the same as the physical Delete
// key), and losing that while typing a command would be a real, working
// feature silently broken, not just an unlikely readline convention no
// one would ever actually hit.
func (r *Root) ToggleDetailsSidebarShortcut() {
	if r.acceptsGlobalShortcut() {
		r.toggleDetailsSidebar()
	}
}

// captureDetailsSidebarMouse swallows every mouse action landing within
// the sidebar's own current rect, regardless of action type. Without
// this, only a plain left-click would actually be consumed (tview.Box's
// own default MouseHandler only ever handles MouseLeftDown, setting
// focus to itself — see its doc comment in box.go); anything else
// (right-click, scroll, plain movement) would fall through to whatever
// is still there in the panel underneath, which shares that same screen
// space while the sidebar floats over it — reacting to a click that, on
// screen, looks like it landed on the sidebar instead.
func (r *Root) captureDetailsSidebarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	x, y := event.Position()
	if primitiveContains(r.detailsSidebar, x, y) {
		return tview.MouseConsumed, nil
	}
	return action, event
}
