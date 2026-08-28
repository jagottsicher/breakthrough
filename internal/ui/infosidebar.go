package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const infoSidebarPage = "info-sidebar"

// infoSidebarMinWidth is infoSidebarSize's own floor for a terminal too
// narrow for a genuine one-third share to still be usable — same shape
// as helpMinWidth/helpMinHeight (see help.go).
const infoSidebarMinWidth = 24

// newInfoSidebarView builds the info sidebar: for now just an empty,
// scrollable TextView — the container this feature will grow its real,
// file-type-dependent content into later (recursive folder sizes,
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
// activePage/overlayStack — showInfoSidebar/hideInfoSidebar/
// ToggleInfoSidebarShortcut below are its own, separate,
// focus-preserving show/hide path.
func (r *Root) newInfoSidebarView() *tview.TextView {
	v := tview.NewTextView()
	v.SetDynamicColors(true)
	v.SetWrap(true)
	v.SetBorderPadding(0, 0, 1, 1)
	v.SetText("Info") // placeholder — real, file-type-dependent content is a later step
	v.SetMouseCapture(r.captureInfoSidebarMouse)
	return v
}

// infoSidebarSize sizes the sidebar against the whole screen (Root's own
// rect, like helpSize — see clampToScreen's own doc comment on why Help
// uses the same base instead of clampToPanel): at least a third of the
// screen's width, and — for now — its full height, top to bottom.
// Leaving two rows clear at the top and three at the bottom (to stay
// clear of the header and the button bar/status bar/bash console rows)
// is a deliberate follow-up, not done here yet.
func (r *Root) infoSidebarSize() (width, height int) {
	_, _, screenWidth, screenHeight := r.GetRect()
	width = screenWidth / 3
	if width < infoSidebarMinWidth {
		width = infoSidebarMinWidth
	}
	return width, screenHeight
}

// showInfoSidebar positions the sidebar flush against the right edge of
// the screen (see infoSidebarSize) and shows it, without touching
// keyboard focus — see newInfoSidebarView's own doc comment on why that
// matters here specifically. SendToFront matters for the same reason it
// does in pushOverlay: ShowPage alone doesn't reorder Pages' own
// internal draw order, so without it the sidebar could end up drawn
// underneath — and fully hidden by — some other page registered after
// it in NewRoot.
func (r *Root) showInfoSidebar() {
	width, height := r.infoSidebarSize()
	_, _, screenWidth, _ := r.GetRect()
	x, y, width, height := r.clampToScreen(screenWidth-width, 0, width, height)
	r.infoSidebar.SetRect(x, y, width, height)
	r.ShowPage(infoSidebarPage)
	r.SendToFront(infoSidebarPage)
	r.infoSidebarVisible = true
}

// hideInfoSidebar closes the sidebar — showInfoSidebar's own counterpart.
func (r *Root) hideInfoSidebar() {
	r.HidePage(infoSidebarPage)
	r.infoSidebarVisible = false
}

// ToggleInfoSidebarShortcut is F3's own action — see cmd/breakthrough.
// It always fires, the same as F1/Help, regardless of what else is open
// or focused: unlike the Ctrl-letter shortcuts (see
// acceptsGlobalShortcut), showing or hiding the sidebar never moves
// keyboard focus anywhere, so there's no in-progress edit it could ever
// silently step on.
func (r *Root) ToggleInfoSidebarShortcut() {
	if r.infoSidebarVisible {
		r.hideInfoSidebar()
	} else {
		r.showInfoSidebar()
	}
}

// captureInfoSidebarMouse swallows every mouse action landing within the
// sidebar's own current rect, regardless of action type. Without this,
// only a plain left-click would actually be consumed (tview.Box's own
// default MouseHandler only ever handles MouseLeftDown, setting focus to
// itself — see its doc comment in box.go); anything else (right-click,
// scroll, plain movement) would fall through to whatever is still there
// in the panel underneath, which shares that same screen space while the
// sidebar floats over it — reacting to a click that, on screen, looks
// like it landed on the sidebar instead.
func (r *Root) captureInfoSidebarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	x, y := event.Position()
	if primitiveContains(r.infoSidebar, x, y) {
		return tview.MouseConsumed, nil
	}
	return action, event
}
