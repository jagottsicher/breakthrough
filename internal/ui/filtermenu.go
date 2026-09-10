package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

const filterMenuPage = "filter-menu"

// filterMenuLabelWidth/filterMenuExprWidth/filterMenuWidth/Height are
// the dropdown's own fixed size: a one-row title bar over three
// fixed-height rows (glob/regex, size, modified-time). filterMenuWidth
// is sized off the size/modified-time rows now, not the glob row —
// "Modified time filter" (the longer of their two labels) plus its own
// checkbox glyph and a space is filterMenuLabelWidth wide, with a
// little breathing room to spare before filterMenuExprWidth's own
// expression field starts; the glob row's own filterField simply
// stretches to fill whatever that leaves (see renderFilterMenu's own
// proportional AddItem for it) rather than needing a width constant of
// its own to stay in sync with these two.
const (
	filterMenuLabelWidth = 25
	filterMenuExprWidth  = 22
	filterMenuWidth      = filterMenuLabelWidth + filterMenuExprWidth + 2
	filterMenuHeight     = 4
)

// openFilterMenu shows the filter-menu dropdown for the currently
// active panel (r.panel) — see Panel.filterMenuBtn/onOpenFilterMenu's
// own doc comments for why this exists at all: filterField/
// filterRegexBtn (the glob/regex filter) plus a size row and a
// modified-time row, each pairing a checkbox with its own real
// comparison-expression field (see internal/filterexpr), all three
// combinable, per the user's own explicit request.
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
// closing, fresh on every render: order lists its seven genuinely
// different focusable pieces in the same order Tab moves through them
// (glob checkbox, glob/regex mode button, glob pattern field, size
// checkbox, size expression field, modified-time checkbox,
// modified-time expression field, wrapping back to the first) — tview
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
	//
	// filterField itself stretches to fill whatever filterMenuWidth
	// leaves once the checkbox/regex-button take their own fixed share
	// (0 fixed width, proportion 1) rather than a fixed width of its
	// own — filterMenuWidth is now sized off the size/modified-time
	// rows below (see its own doc comment), so this row simply takes
	// whatever's left instead of needing a second width constant kept
	// in sync with the first.
	globRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(globCheckbox, 2, 0, false).
		AddItem(panel.filterRegexBtn, 8, 0, false).
		AddItem(panel.filterField, 0, 1, true)

	sizeRow, sizeCheckbox, sizeField := r.newFilterMenuFieldRow(&panel.filterSizeActive, &panel.filterSizeText, "Size filter", "e.g. > 1m and < 1g", panel, moveFocus, closeMenu)
	order = append(order, sizeCheckbox)
	sizeField.SetInputCapture(arrowCapture(sizeField))
	sizeField.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			moveFocus(sizeField, 1)
		case tcell.KeyBacktab:
			moveFocus(sizeField, -1)
		case tcell.KeyEnter, tcell.KeyEscape:
			closeMenu()
		}
	})
	order = append(order, sizeField)

	mtimeRow, mtimeCheckbox, mtimeField := r.newFilterMenuFieldRow(&panel.filterMtimeActive, &panel.filterMtimeText, "Modified time filter", "e.g. last 7 days", panel, moveFocus, closeMenu)
	order = append(order, mtimeCheckbox)
	mtimeField.SetInputCapture(arrowCapture(mtimeField))
	mtimeField.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			moveFocus(mtimeField, 1)
		case tcell.KeyBacktab:
			moveFocus(mtimeField, -1)
		case tcell.KeyEnter, tcell.KeyEscape:
			closeMenu()
		}
	})
	order = append(order, mtimeField)

	r.filterMenuLayout.Clear()
	r.filterMenuLayout.
		AddItem(r.filterMenuTitleBar, 1, 0, false).
		AddItem(globRow, 1, 0, true).
		AddItem(sizeRow, 1, 0, false).
		AddItem(mtimeRow, 1, 0, false)
}

// newFilterMenuFieldRow builds one of the filter-menu's size/modified-
// time rows: a checkbox+label (the same checkboxText convention every
// other toggle in this app already uses — Sed Replace's own flag list,
// the Search dialog's checkboxes, chmod's recursive/Files toggles)
// paired with a real InputField for the row's own comparison
// expression (see internal/filterexpr for the syntax each one
// accepts) — the same real-time "type it, the listing narrows
// immediately" behavior the glob row's own filterField already has.
// Typing anything into the field auto-activates the checkbox, exactly
// mirroring filterField's own identical "going from empty to
// non-empty is as deliberate a signal as pressing the checkbox would
// be" behavior (see panel.go's own doc comment on filterGlobActive).
//
// Neither the checkbox nor the field is a persistent Panel field the
// way filterField/filterRegexBtn are: *active/*text (pointers into
// Panel.filterSizeActive/filterSizeText or their modified-time
// counterparts) already hold everything worth keeping between opens,
// so a fresh checkbox+field seeded from them on every render (the same
// "cheap enough to just rebuild" reasoning the old checkbox-only rows
// already followed) is simpler than also giving each its own
// long-lived widget identity to keep in sync.
//
// Returns the row itself plus its own checkbox/field so
// renderFilterMenu can wire both into the dropdown's own keyboard
// focus-cycling separately — two real stops per row now, not one.
// moveFocus/closeMenu come from renderFilterMenu's own single render
// pass rather than being rebuilt here.
func (r *Root) newFilterMenuFieldRow(active *bool, text *string, label, placeholder string, panel *Panel, moveFocus func(tview.Primitive, int), closeMenu func()) (row *tview.Flex, checkbox *tview.TextView, field *tview.InputField) {
	checkbox = tview.NewTextView().SetDynamicColors(true)
	filterMenuRowStyle(checkbox, panel.theme, false)
	renderCheckbox := func() { checkbox.SetText(checkboxText(*active) + " " + label) }
	renderCheckbox()

	reload := func() { panel.reportError(panel.load(panel.path)) }

	toggle := func() {
		*active = !*active
		renderCheckbox()
		panel.renderFilterMenuBtn()
		reload()
	}
	checkbox.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		// Checked first — see Panel.filterMenuBtn's own identical guard
		// for the full reasoning (a real, user-reported regression
		// without it): filterMenuLayout calls every top-level row in
		// order regardless of which one a click actually landed on, so
		// this checkbox's own capture must decline whatever isn't
		// actually its own, or it swallows clicks meant for its own
		// field right next to it, or the row after it.
		if !checkbox.InRect(event.Position()) {
			return action, event
		}
		if action == tview.MouseLeftClick {
			toggle()
		}
		return tview.MouseConsumed, nil
	})
	checkbox.SetFocusFunc(func() { filterMenuRowStyle(checkbox, panel.theme, true) })
	checkbox.SetBlurFunc(func() { filterMenuRowStyle(checkbox, panel.theme, false) })
	checkbox.SetInputCapture(filterMenuCheckboxCapture(checkbox, toggle, moveFocus, closeMenu))

	field = tview.NewInputField()
	field.SetPlaceholder(placeholder)
	field.SetText(*text)
	field.SetChangedFunc(func(t string) {
		if t == *text {
			return // triggered by this same SetText call above, not real typing
		}
		*text = t
		if t != "" {
			*active = true
			renderCheckbox()
			panel.renderFilterMenuBtn()
		}
		reload()
	})

	row = tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(checkbox, filterMenuLabelWidth, 0, false).
		AddItem(field, 0, 1, false)
	return row, checkbox, field
}
