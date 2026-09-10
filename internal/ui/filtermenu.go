package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
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
// Also what the "/" plain command opens now (see plainCommands in
// keymap.go) — a real, user-reported fix: it used to call
// Application.SetFocus(r.panel.filterField) directly instead, which
// left keystrokes going nowhere and no dropdown ever appearing at all.
// filterField only ever actually receives real keyboard input once
// it's part of the page tree this function's own showOverlay call
// puts it in — a bare SetFocus on it while that page was never shown
// bypasses that entirely. Going through this function does everything
// a real click on the "Y" button already does instead, including
// rewiring the whole dropdown's own keyboard focus-cycling (see
// renderFilterMenu's own doc comment) fresh for this specific open.
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

// filterMenuRowStyle applies focused's own AccentBackground/
// FocusedBackground pair to row — the same "petrol means this
// currently has real keyboard focus" convention every other focusable
// element in this app already follows. Takes focused explicitly rather
// than querying row.HasFocus() itself: Panel.setSelectionStyle's own
// doc comment covers why — a SetBlurFunc callback runs before tview
// clears its own hasFocus flag, so HasFocus() would lie specifically
// there.
func filterMenuRowStyle(row *tview.TextView, theme config.ResolvedTheme, focused bool) {
	bg := theme.AccentBackground
	if focused {
		bg = theme.FocusedBackground
	}
	row.SetBackgroundColor(bg)
}

// filterMenuCheckboxCapture is the InputCapture shared by all three of
// the filter-menu's own checkbox-style rows (globCheckbox, sizeRow,
// mtimeRow — see renderFilterMenu/newFilterMenuToggleRow): Space or
// Enter toggles the row, Down/Tab moves keyboard focus to the next
// stop in the dropdown's own order, Up/Backtab to the previous one,
// and Escape closes the whole dropdown. Consumes every one of these
// outright — unlike filterRegexBtn/filterField (real tview widgets
// whose own native handling for some of these keys is worth
// preserving alongside this), a plain TextView has no native behavior
// here worth keeping: Enter/Tab/Backtab/Escape only ever reach its own
// (here, deliberately never set) DoneFunc, and Space/Down/Up do
// nothing at all unless the view is scrollable, which none of these
// three rows are.
//
// A real, user-reported gap this closes: before this existed, the
// dropdown had no keyboard path in or out at all beyond typing into
// the glob field itself — not even Escape closed it (see
// renderFilterMenu's own doc comment on filterField's own fix for the
// closely related half of the same report), let alone reaching the
// size/modified-time rows without a mouse.
func filterMenuCheckboxCapture(this tview.Primitive, toggle func(), moveFocus func(tview.Primitive, int), closeMenu func()) func(*tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		isSpace := event.Key() == tcell.KeyRune && event.Rune() == ' '
		switch {
		case event.Key() == tcell.KeyEscape:
			closeMenu()
		case event.Key() == tcell.KeyEnter, isSpace:
			toggle()
		case event.Key() == tcell.KeyDown, event.Key() == tcell.KeyTab:
			moveFocus(this, 1)
		case event.Key() == tcell.KeyUp, event.Key() == tcell.KeyBacktab:
			moveFocus(this, -1)
		default:
			return event
		}
		return nil
	}
}

// renderFilterMenu (re)builds filterMenuLayout's own three rows against
// r.panel — the active panel at the moment this runs — from scratch
// every time: cheap (three Flex rows, a handful of TextViews), and the
// only way to guarantee the glob row always embeds whichever panel's
// own real filterField/filterRegexBtn is actually current, since Root
// itself owns neither.
//
// Also wires up the whole dropdown's own keyboard focus-cycling and
// closing, fresh on every render: order lists its five genuinely
// different focusable pieces in the same order Tab moves through them
// (glob checkbox, glob/regex mode button, glob pattern field, size
// toggle, modified-time toggle, wrapping back to the first) — tview
// has no built-in way to cycle Tab between Primitives of different
// types on its own, so this is hand-rolled the same way chmoddialog.go
// already hand-rolls its own multi-field Tab-cycling, for the same
// reason. moveFocus/closeMenu are built once per render and closed
// over by every row's own InputCapture/SetDoneFunc/SetExitFunc below,
// rather than needing any of this stored on Root or Panel longer than
// one render's own lifetime.
//
// filterField's own SetDoneFunc is (re)installed here rather than once
// in NewPanel, on purpose: NewPanel has no closeMenu/moveFocus of its
// own render pass to close over, and a real, user-reported bug
// existed here before this at all — filterField's own SetDoneFunc used
// to just call Application.SetFocus(p.table) unconditionally,
// regardless of which key triggered it, which moves keyboard focus
// away but never actually calls hideOverlay — leaving the dropdown
// itself stuck open, invisible to the keyboard, until a later mouse
// click elsewhere happened to close it via the ordinary outside-click
// mechanism. Escape and Enter now both actually close the menu here;
// Tab/Backtab cycle focus instead of leaving the dropdown entirely.
func (r *Root) renderFilterMenu() {
	panel := r.panel

	var order []tview.Primitive
	moveFocus := func(current tview.Primitive, delta int) {
		for i, p := range order {
			if p == current {
				next := (i + delta + len(order)) % len(order)
				r.app.SetFocus(order[next])
				return
			}
		}
	}
	closeMenu := func() { r.hideOverlay() }
	// arrowCapture intercepts only Down/Up — the one pair of keys
	// neither Button.SetExitFunc nor InputField's own SetDoneFunc ever
	// sees at all (both only ever fire for Enter/Escape/Tab/Backtab,
	// verified directly against tview's own button.go/inputfield.go) —
	// and lets every other key fall through unchanged to whichever
	// primitive's own native handling comes next (typing, Enter,
	// Tab/Backtab/Escape, ...). Shared by filterRegexBtn and
	// filterField, the two real tview widgets in order that need this
	// added on top of their own native handling rather than instead of
	// it, the way filterMenuCheckboxCapture replaces it entirely for
	// the three plain TextView rows.
	arrowCapture := func(this tview.Primitive) func(*tcell.EventKey) *tcell.EventKey {
		return func(event *tcell.EventKey) *tcell.EventKey {
			switch event.Key() {
			case tcell.KeyDown:
				moveFocus(this, 1)
				return nil
			case tcell.KeyUp:
				moveFocus(this, -1)
				return nil
			}
			return event
		}
	}

	globCheckbox := tview.NewTextView().SetDynamicColors(true)
	filterMenuRowStyle(globCheckbox, panel.theme, false)
	renderGlobCheckbox := func() { globCheckbox.SetText(checkboxText(panel.filterGlobActive)) }
	renderGlobCheckbox()
	toggleGlob := func() {
		panel.filterGlobActive = !panel.filterGlobActive
		renderGlobCheckbox()
		panel.renderFilterMenuBtn()
		panel.reportError(panel.load(panel.path))
	}
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
			toggleGlob()
		}
		return tview.MouseConsumed, nil
	})
	globCheckbox.SetFocusFunc(func() { filterMenuRowStyle(globCheckbox, panel.theme, true) })
	globCheckbox.SetBlurFunc(func() { filterMenuRowStyle(globCheckbox, panel.theme, false) })
	globCheckbox.SetInputCapture(filterMenuCheckboxCapture(globCheckbox, toggleGlob, moveFocus, closeMenu))
	order = append(order, globCheckbox)

	panel.filterRegexBtn.SetInputCapture(arrowCapture(panel.filterRegexBtn))
	panel.filterRegexBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			moveFocus(panel.filterRegexBtn, 1)
		case tcell.KeyBacktab:
			moveFocus(panel.filterRegexBtn, -1)
		case tcell.KeyEscape:
			closeMenu()
		}
	})
	order = append(order, panel.filterRegexBtn)

	panel.filterField.SetInputCapture(arrowCapture(panel.filterField))
	panel.filterField.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			moveFocus(panel.filterField, 1)
		case tcell.KeyBacktab:
			moveFocus(panel.filterField, -1)
		case tcell.KeyEnter, tcell.KeyEscape:
			closeMenu()
		}
	})
	order = append(order, panel.filterField)

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

	sizeRow := r.newFilterMenuToggleRow(&panel.filterSizeActive, "Size filter", panel, moveFocus, closeMenu)
	order = append(order, sizeRow)
	mtimeRow := r.newFilterMenuToggleRow(&panel.filterMtimeActive, "Modified time filter", panel, moveFocus, closeMenu)
	order = append(order, mtimeRow)

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
// dialog's checkboxes, chmod's recursive/Files toggles). Clicking it,
// or reaching it by keyboard and pressing Space/Enter (see
// filterMenuCheckboxCapture), flips *active in place and re-renders
// just this row, the same "toggle relabels itself, dialog stays open"
// shape Root.toggleSedFlag already has — the whole point of a menu
// offering three independently combinable filters is that ticking one
// doesn't close it before you can tick another. moveFocus/closeMenu
// come from renderFilterMenu's own single render pass (see its own doc
// comment) rather than being rebuilt here.
func (r *Root) newFilterMenuToggleRow(active *bool, label string, panel *Panel, moveFocus func(tview.Primitive, int), closeMenu func()) *tview.TextView {
	row := tview.NewTextView().SetDynamicColors(true)
	filterMenuRowStyle(row, panel.theme, false)
	render := func() { row.SetText(checkboxText(*active) + " " + label) }
	render()
	toggle := func() {
		*active = !*active
		render()
		panel.renderFilterMenuBtn()
	}
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
			toggle()
		}
		return tview.MouseConsumed, nil
	})
	row.SetFocusFunc(func() { filterMenuRowStyle(row, panel.theme, true) })
	row.SetBlurFunc(func() { filterMenuRowStyle(row, panel.theme, false) })
	row.SetInputCapture(filterMenuCheckboxCapture(row, toggle, moveFocus, closeMenu))
	return row
}
