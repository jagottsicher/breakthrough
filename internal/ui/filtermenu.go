package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const filterMenuPage = "filter-menu"

// filterMenuWidth/Height are the dropdown's own fixed size: a one-row
// title bar over three fixed-height rows (glob/regex, size,
// modified-time). Wide enough for the glob row's own embedded
// filterRegexBtn (8 columns) and filterField (headerFilterWidth, 17
// columns) plus a 2-column checkbox lead-in and a little breathing
// room either side — the two stub rows (size/modified-time) fit
// comfortably inside the same width with room to spare.
const (
	filterMenuWidth  = 2 + 8 + headerFilterWidth + 4
	filterMenuHeight = 4
)

// openFilterMenu shows the filter-menu dropdown for the currently
// active panel (r.panel) — see Panel.filterMenuBtn/onOpenFilterMenu's
// own doc comments for why this exists at all: filterField/
// filterRegexBtn (the existing glob/regex filter, fully working,
// unchanged) plus two not-yet-built toggles for size and
// modified-time, all three combinable, per the user's own explicit
// request.
//
// Rebuilds filterMenuLayout's own content fresh on every open (see
// renderFilterMenu) rather than keeping it built once: which panel is
// "active" can change between one open and the next (a different
// tab), and filterField/filterRegexBtn themselves are owned by
// whichever Panel is currently active, not by Root — the same
// "freshly configure a shared overlay against today's real target"
// shape openChmod/openSedReplace/openProperties all already follow,
// just against a Panel instead of a file.
//
// Anchored under the button that opened it, right-aligned to its own
// right edge — the same reasoning openTabSwitcher's own doc comment
// gives for anchoring under the tab strip rather than centering on
// screen: the button is what this drops down from, so appearing
// anywhere else would read as an unrelated dialog.
func (r *Root) openFilterMenu() {
	r.renderFilterMenu()

	right, y := r.filterMenuAnchor()
	x := right - filterMenuWidth
	x, y, w, h := r.clampToPanel(x, y, filterMenuWidth, filterMenuHeight)
	r.filterMenuLayout.SetRect(x, y, w, h)
	r.showOverlay(filterMenuPage, r.filterMenuLayout)
}

// filterMenuAnchor returns the screen position filterMenuBtn's own
// bottom-left corner sits at, within r.panel's own header row — the
// same GetRect-based anchoring tabStripAnchor already uses for the tab
// switcher.
func (r *Root) filterMenuAnchor() (right, y int) {
	x, y, w, _ := r.panel.filterMenuBtn.GetRect()
	return x + w, y + 1
}

// renderFilterMenu (re)builds filterMenuLayout's own three rows against
// r.panel — the active panel at the moment this runs — from scratch
// every time: cheap (three Flex rows, a handful of TextViews), and the
// only way to guarantee the glob row always embeds whichever panel's
// own real filterField/filterRegexBtn is actually current, since Root
// itself owns neither.
func (r *Root) renderFilterMenu() {
	panel := r.panel

	globCheckbox := tview.NewTextView().SetDynamicColors(true)
	globCheckbox.SetBackgroundColor(panel.theme.AccentBackground)
	renderGlobCheckbox := func() { globCheckbox.SetText(checkboxText(panel.filterGlobActive)) }
	renderGlobCheckbox()
	globCheckbox.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		// Checked first — see Panel.filterMenuBtn's own identical guard
		// for the full reasoning (a real, user-reported regression
		// without it): globRow calls globCheckbox before
		// filterRegexBtn/filterField, in that order, regardless of
		// which one a click actually landed on.
		if !globCheckbox.InRect(event.Position()) {
			return action, event
		}
		if action == tview.MouseLeftClick {
			panel.filterGlobActive = !panel.filterGlobActive
			renderGlobCheckbox()
			panel.renderFilterMenuBtn()
			panel.reportError(panel.load(panel.path))
		}
		return tview.MouseConsumed, nil
	})

	// filterField itself gets initial focus (see AddItem's own focus
	// flag below) — Application.SetFocus's own delegate chain (verified
	// directly against tview's own flex.go/application.go — see
	// buildHeaderSpans' sibling doc comments elsewhere in this package
	// for the same verification) cascades down through globRow to it,
	// so typing works the instant this dropdown opens, matching every
	// other dialog in this app whose own first field takes focus
	// immediately (Sed Replace's sedForm, Search's Filename span, ...).
	globRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(globCheckbox, 2, 0, false).
		AddItem(panel.filterRegexBtn, 8, 0, false).
		AddItem(panel.filterField, headerFilterWidth, 0, true)

	sizeRow := r.newFilterMenuToggleRow(&panel.filterSizeActive, "Size filter", panel)
	mtimeRow := r.newFilterMenuToggleRow(&panel.filterMtimeActive, "Modified time filter", panel)

	r.filterMenuLayout.Clear()
	r.filterMenuLayout.
		AddItem(r.filterMenuTitleBar, 1, 0, false).
		AddItem(globRow, 1, 0, true).
		AddItem(sizeRow, 1, 0, false).
		AddItem(mtimeRow, 1, 0, false)
}

// newFilterMenuToggleRow builds one of the filter-menu's two not-yet-
// built stub rows (size/modified-time — see Panel.filterSizeActive's
// own doc comment on why there's no filtering logic behind either
// yet): a single clickable "○/● label" TextView, the same checkboxText
// convention every other toggle in this app already uses (see
// checkboxText in panel.go — Sed Replace's own flag list, the Search
// dialog's checkboxes, chmod's recursive/Files toggles). Clicking it
// flips *active in place and re-renders just this row, the same
// "toggle relabels itself, dialog stays open" shape
// Root.toggleSedFlag already has — the whole point of a menu offering
// three independently combinable filters is that ticking one doesn't
// close it before you can tick another.
func (r *Root) newFilterMenuToggleRow(active *bool, label string, panel *Panel) *tview.TextView {
	row := tview.NewTextView().SetDynamicColors(true)
	row.SetBackgroundColor(panel.theme.AccentBackground)
	render := func() { row.SetText(checkboxText(*active) + " " + label) }
	render()
	row.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		// Checked first — see Panel.filterMenuBtn's own identical guard
		// for the full reasoning (a real, user-reported regression
		// without it): filterMenuLayout calls every top-level row in
		// order regardless of which one a click actually landed on, so
		// this row's own capture must decline whatever isn't actually
		// its own, or it swallows clicks meant for whichever row comes
		// after it (mtimeRow, when this is sizeRow).
		if !row.InRect(event.Position()) {
			return action, event
		}
		if action == tview.MouseLeftClick {
			*active = !*active
			render()
			panel.renderFilterMenuBtn()
		}
		return tview.MouseConsumed, nil
	})
	return row
}
