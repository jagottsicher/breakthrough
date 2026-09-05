package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// The Options screen: a full-screen editor for every setting
// breakthrough actually acts on, with the categories down the left and
// the settings in the selected one on the right.
//
// Three deliberate choices, all per the user's own explicit design:
//
//   - No save button. Every change takes effect and is written to the
//     user's config file the moment it's made — a toggle on Enter, a
//     picked value on Enter, a typed number on Enter. There is no
//     "unsaved state" to lose, and nothing to confirm.
//   - Reset removes rather than overwrites. "Reset" (per category, or
//     everything at once) deletes the key from the user's config file
//     instead of writing the built-in default into it, so the value
//     falls back through the tiers — see config.UnsetKey and Origin.
//   - Origin is shown per setting. "default" / "system-wide" / "changed
//     by you", so the reset above has a visible meaning rather than
//     looking arbitrary.
//
// The settings themselves live in optioncatalog.go, not here: this file
// knows how to render and edit *a* setting, never which ones exist.

const (
	optionsInfoPage  = "options-info"
	optionsInputPage = "options-input"

	// optionsCategoryWidth is the fixed width of the left-hand category
	// list. Wide enough for the longest category name plus the list's
	// own padding, and deliberately fixed rather than proportional so
	// the settings pane's own column layout doesn't shift about as the
	// terminal is resized.
	optionsCategoryWidth = 20
)

// Column indices within optionsTable. Named rather than inline literals
// because captureOptionsTableMouse maps a click's column back to an
// action through exactly these.
// The info button deliberately comes before the default hint, even
// though the hint reads more naturally right after the value: on a
// terminal too narrow for the whole row, the rightmost column is the
// one that gets clipped, and losing a dim "default: ..." note costs far
// less than losing the button that explains the setting.
const (
	optionsColLabel = iota
	optionsColValue
	optionsColInfo
	optionsColDefault
)

// optionsInfoGlyph marks the per-setting info button at the end of each
// row. Clicking it, or pressing "?" or F1 with the row selected, opens
// that setting's own explanation (see showOptionInfo).
const optionsInfoGlyph = "[?]"

// Column widths for the settings table. Fixed rather than
// self-sizing so the columns line up across categories — a table that
// re-flowed every time the category changed would make the whole right
// pane appear to jump sideways.
const (
	optionsLabelWidth   = 30
	optionsValueWidth   = 34
	optionsDefaultWidth = 16
)

// padRight pads s with spaces to at least width display columns.
//
// Measured with tview's own tag-aware width function, so a value
// carrying color tags (none do today, but the value cells render
// whatever a setting's own display function returns) still lines up.
// Never truncates: a value longer than its column pushes the ones after
// it rather than being silently cut off, which is the better failure for
// a settings screen where the value is the point.
func padRight(s string, width int) string {
	if pad := width - tview.TaggedStringWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// newOptionsScreen builds the whole screen once, at startup — the same
// build-once/repopulate-on-open shape every other overlay in this
// package uses (see newPropertiesView, r.picker). Only the contents are
// rebuilt per open (see openOptions/renderOptions); the widgets
// themselves live for the application's lifetime.
func (r *Root) newOptionsScreen() {
	r.optionsCategories = tview.NewList().ShowSecondaryText(false)
	r.optionsCategories.SetHighlightFullLine(true)
	r.optionsCategories.SetBorderPadding(1, 0, 1, 1)
	// Selecting a category is a pure navigation act, so it happens on
	// mere cursor movement rather than needing Enter — moving down the
	// list walks through the categories, which is what someone
	// exploring the settings expects.
	r.optionsCategories.SetChangedFunc(func(index int, _, _ string, _ rune) {
		r.optionsCategory = index
		r.renderOptions()
	})

	r.optionsTable = tview.NewTable()
	r.optionsTable.SetBorders(false)
	// Top padding of 1 to line the first setting up with the first
	// category across the divider, and 2 columns of left padding so the
	// labels don't hug it.
	r.optionsTable.SetBorderPadding(1, 0, 2, 1)
	r.optionsTable.SetSelectable(true, false) // whole rows: one setting per row
	r.optionsTable.SetSelectedFunc(func(row, _ int) { r.activateOptionRow(row) })
	r.optionsTable.SetMouseCapture(r.captureOptionsTableMouse)

	r.optionsTitleBar = tview.NewTextView()
	r.optionsTitleBar.SetWrap(false)
	r.optionsTitleBar.SetText(" Options ")

	r.optionsHint = tview.NewTextView()
	r.optionsHint.SetWrap(false)
	r.optionsHint.SetText(" ←/→: pane · ↑/↓: move · Enter/Space: change · ?: explain · Tab: buttons · Esc: close ")

	r.optionsButtons = r.newOptionsButtons()

	// The right-hand pane: the settings of the selected category, with
	// the action buttons pinned underneath them.
	rightPane := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.optionsTable, 0, 1, true).
		AddItem(r.optionsButtons, 1, 0, false)

	body := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.optionsCategories, optionsCategoryWidth, 0, false).
		AddItem(rightPane, 0, 1, true)

	r.optionsLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.optionsTitleBar, 1, 0, false).
		AddItem(body, 0, 1, true).
		AddItem(r.optionsHint, 1, 0, false)

	// The info popup (see showOptionInfo) — a small centered window on
	// top of the screen, closed by Escape or a click outside it, exactly
	// like every other overlay layered on another.
	r.optionsInfo = tview.NewTextView()
	r.optionsInfo.SetWrap(true)
	r.optionsInfo.SetWordWrap(true)
	r.optionsInfo.SetBorderPadding(1, 1, 2, 2)

	// One shared input field for a typed value, repopulated per use —
	// the same "one widget, reused" pattern r.prompt already follows.
	// Enum settings need no widget of their own: activating the row
	// cycles them in place (see cycleOptionChoice).
	r.optionsInput = tview.NewInputField()

	// The shared Tab/Escape handling (see captureOptionsKey) goes on the
	// two list-like stops here; the buttons get theirs in
	// newOptionsButtons, where their own actions are already in scope.
	// Both panes additionally get the left/right arrow keys that move
	// between them (see captureOptionsPaneArrows).
	r.optionsCategories.SetInputCapture(chainKeyCaptures(r.captureOptionsKey, r.captureOptionsPaneArrows))

	// Which pane has keyboard focus has to be visible — without it the
	// two selected rows look identical and there's no telling which one
	// the arrow keys will move (a real report).
	//
	// Driven by the focus events themselves, not re-derived during Draw:
	// a style set from inside a draw callback only takes effect on the
	// *next* draw, so the highlight lagged a frame behind the focus (a
	// real bug, caught by reading the colors off an actually-drawn
	// screen rather than trusting the widgets). Neither callback queries
	// HasFocus() either — tview's own Box.Blur runs its callback before
	// clearing the focus flag, so the answer there is wrong, the same
	// trap Panel.setSelectionStyle documents. Each pane simply states
	// its own new state, which is never ambiguous.
	r.optionsCategories.SetFocusFunc(func() { r.setOptionsPaneFocused(r.optionsCategories, true) })
	r.optionsCategories.SetBlurFunc(func() { r.setOptionsPaneFocused(r.optionsCategories, false) })
	r.optionsTable.SetFocusFunc(func() { r.setOptionsPaneFocused(r.optionsTable, true) })
	r.optionsTable.SetBlurFunc(func() { r.setOptionsPaneFocused(r.optionsTable, false) })
	// Neither pane has been focused yet, so start both in the unfocused
	// look; the first real focus event corrects whichever one gains it.
	r.setOptionsPaneFocused(r.optionsCategories, false)
	r.setOptionsPaneFocused(r.optionsTable, false)
	r.optionsTable.SetInputCapture(chainKeyCaptures(r.captureOptionsKey,
		chainKeyCaptures(r.captureOptionsPaneArrows, r.captureOptionsTableKey)))
}

// chainKeyCaptures runs first, then second for any event first didn't
// consume — tview allows only one InputCapture per primitive, so a
// widget that needs both the screen's shared keys and its own specific
// ones has to combine them explicitly.
func chainKeyCaptures(first, second func(*tcell.EventKey) *tcell.EventKey) func(*tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		if event = first(event); event == nil {
			return nil
		}
		return second(event)
	}
}

// newOptionsButtons builds the row of screen-wide actions under the
// settings table.
//
// Reset is offered twice, deliberately: per category (what someone who
// changed one thing and regretted it wants) and for everything at once
// (what someone who wants a clean slate wants). Both go through
// resetSetting, so both mean "stop overriding" rather than "write the
// default".
func (r *Root) newOptionsButtons() *tview.Flex {
	type buttonSpec struct {
		button **tview.Button
		label  string
		action func()
	}
	specs := []buttonSpec{
		{&r.optionsResetCategoryBtn, "Reset category", r.resetCurrentOptionCategory},
		{&r.optionsResetAllBtn, "Reset all", r.resetAllOptions},
		{&r.optionsEditFileBtn, "Edit config file", r.editConfigFile},
		{&r.optionsNewSchemeBtn, "New color scheme", r.duplicateColorScheme},
	}

	row := tview.NewFlex().SetDirection(tview.FlexColumn)
	for _, spec := range specs {
		b := tview.NewButton(spec.label)
		b.SetSelectedFunc(spec.action)
		// The screen's shared Tab/Escape handling first, then Space as
		// a second way to activate the focused button — matching every
		// other button in this app (see spaceAlsoActivates).
		b.SetInputCapture(chainKeyCaptures(r.captureOptionsKey, spaceAlsoActivates(spec.action)))
		*spec.button = b
		row.AddItem(b, 0, 1, false)
	}
	return row
}

// optionsButtonList is the button row's own fixed order — shared by the
// row's own construction and by the focus ring, so the two can't
// disagree about which buttons exist.
func (r *Root) optionsButtonList() []*tview.Button {
	return []*tview.Button{
		r.optionsResetCategoryBtn,
		r.optionsResetAllBtn,
		r.optionsEditFileBtn,
		r.optionsNewSchemeBtn,
	}
}

// optionsFocusRing is Tab's own stop order within the Options screen:
// the category list, the settings table, then each action button.
//
// A ring of its own rather than tview's built-in Flex focus handling,
// for the same reason CycleFocusShortcut exists for the main window:
// the order has to be deliberate and stable, and Tab has to wrap rather
// than dead-end on the last button.
func (r *Root) optionsFocusRing() []tview.Primitive {
	ring := []tview.Primitive{r.optionsCategories, r.optionsTable}
	for _, b := range r.optionsButtonList() {
		ring = append(ring, b)
	}
	return ring
}

// cycleOptionsFocus moves focus one step around optionsFocusRing —
// forward for Tab, backward for Shift+Tab.
//
// Returns false when nothing in the ring currently has focus, which
// means the keystroke wasn't ours to consume (the Options screen isn't
// what's focused right now) and the caller should let it through
// untouched.
func (r *Root) cycleOptionsFocus(delta int) bool {
	ring := r.optionsFocusRing()
	for i, stop := range ring {
		if !stop.HasFocus() {
			continue
		}
		next := (i + delta) % len(ring)
		if next < 0 {
			next += len(ring) // Go's own % keeps the dividend's sign
		}
		r.app.SetFocus(ring[next])
		return true
	}
	return false
}

// captureOptionsKey is the Options screen's own shared key handling,
// installed on every widget in the focus ring: Tab/Shift+Tab move
// between them, Escape closes the screen.
//
// Escape here rather than relying on each widget's own DoneFunc because
// only some of them have one — a Button and a Table don't — and a
// screen where Escape works from three places out of six would be
// worse than one where it doesn't work at all.
func (r *Root) captureOptionsKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyTab:
		if r.cycleOptionsFocus(1) {
			return nil
		}
	case tcell.KeyBacktab:
		if r.cycleOptionsFocus(-1) {
			return nil
		}
	case tcell.KeyEscape:
		r.closeOptions()
		return nil
	}
	return event
}

// captureOptionsPaneArrows moves between the two panes with the left and
// right arrow keys: Right from the categories into the settings, Left
// from the settings back to the categories.
//
// Tab already cycles the whole focus ring, but a two-pane screen with
// the categories physically to the left of the settings invites moving
// between them by pointing at them — per the user's own explicit
// request for cursor-key navigation of the categories. Up/Down keep
// their usual meaning inside whichever pane has focus, so this adds a
// way through the screen without taking one away.
//
// Left is deliberately inert on the categories pane (there's nothing
// further left) rather than wrapping around to the settings: an arrow
// key that jumps to the opposite side of the screen reads as a glitch,
// not a feature. Tab is the one that wraps.
func (r *Root) captureOptionsPaneArrows(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyRight:
		if r.optionsCategories.HasFocus() {
			r.app.SetFocus(r.optionsTable)
			return nil
		}
	case tcell.KeyLeft:
		if r.optionsTable.HasFocus() {
			r.app.SetFocus(r.optionsCategories)
			return nil
		}
	}
	return event
}

// openOptions shows the Options screen, rebuilt from the settings
// currently in force.
func (r *Root) openOptions() {
	if r.optionsCategory < 0 || r.optionsCategory >= len(optionCategories()) {
		r.optionsCategory = 0
	}
	r.renderOptionCategories()
	r.renderOptions()
	// The whole layout is the overlay, not just the settings table:
	// captureOutsideClick closes an overlay on any click outside the
	// widget it was shown with, so registering only the table made a
	// click on the categories — or the buttons, or the title bar — count
	// as "outside" and shut the screen (a real report). Focus still
	// starts on the table, via the restore callback, exactly the way
	// Properties already registers its own full layout and then focuses
	// a field inside it.
	r.showOverlayWithRestore(optionsPage, r.optionsLayout, func() {
		r.app.SetFocus(r.optionsTable)
	})
}

// closeOptions hides the Options screen. Nothing to save or discard —
// every change already took effect when it was made (see this file's
// own doc comment).
func (r *Root) closeOptions() {
	r.hideOverlay()
}

// renderOptionCategories fills the left-hand list. Rebuilt rather than
// kept in sync, the same as every other list in this package.
func (r *Root) renderOptionCategories() {
	// SetChangedFunc fires on Clear/AddItem too, which would re-enter
	// renderOptions mid-rebuild against a half-populated list — detach
	// it for the rebuild and restore it after, then set the selection
	// deliberately.
	r.optionsCategories.SetChangedFunc(nil)
	r.optionsCategories.Clear()
	for _, cat := range optionCategories() {
		r.optionsCategories.AddItem(cat.name, "", 0, nil)
	}
	r.optionsCategories.SetCurrentItem(r.optionsCategory)
	r.optionsCategories.SetChangedFunc(func(index int, _, _ string, _ rune) {
		r.optionsCategory = index
		r.renderOptions()
	})
}

// renderOptions fills the right-hand table with the selected category's
// settings: label, current value, where that value came from, and the
// info button.
func (r *Root) renderOptions() {
	r.optionsTable.Clear()

	categories := optionCategories()
	if r.optionsCategory < 0 || r.optionsCategory >= len(categories) {
		return
	}

	for row, opt := range categories[r.optionsCategory].options {
		// The label column is given a generous fixed width rather than
		// being left to size itself: without it the value column starts
		// at a different place in every category, and switching
		// categories visibly jumps the whole layout sideways.
		r.optionsTable.SetCell(row, optionsColLabel,
			tview.NewTableCell(padRight(opt.label, optionsLabelWidth)).
				SetTextColor(r.theme.Text).
				SetSelectable(true))

		display := r.optionValueDisplay(opt)
		if opt.restartHint {
			display += "  (on next start)"
		}
		r.optionsTable.SetCell(row, optionsColValue,
			tview.NewTableCell(padRight(display, optionsValueWidth)).
				SetTextColor(r.theme.Text).
				SetSelectable(true))

		// Dimmed, and empty whenever the value already is the default:
		// this is context for the value beside it, not a value in its
		// own right, and a permanent marker on every row would be
		// noise. See optionDefaultHint.
		r.optionsTable.SetCell(row, optionsColDefault,
			tview.NewTableCell(padRight(r.optionDefaultHint(opt), optionsDefaultWidth)).
				SetTextColor(r.theme.PlaceholderText).
				SetSelectable(true))

		r.optionsTable.SetCell(row, optionsColInfo,
			tview.NewTableCell(optionsInfoGlyph).
				SetTextColor(r.theme.PlaceholderText).
				SetSelectable(true))
	}

	// Keep the cursor in range after a category switch shortened the
	// list under it.
	if row, _ := r.optionsTable.GetSelection(); row >= r.optionsTable.GetRowCount() {
		r.optionsTable.Select(0, 0)
	}
}

// setOptionsPaneFocused paints one pane's own selected-row highlight for
// the focus state it has just entered: FocusedBackground when it has
// keyboard focus, EditableBackground when it doesn't.
//
// The same "petrol means this is where keystrokes go right now"
// convention the rest of this app already follows — the main panel's own
// current row, every overlay title bar, every focused button. Without
// it, both panes highlight a row identically and nothing says which of
// them the arrow keys are actually driving.
//
// Takes the state explicitly rather than asking the widget, and takes
// the widget explicitly rather than checking both: see the wiring in
// newOptionsScreen for why either shortcut would be wrong.
func (r *Root) setOptionsPaneFocused(pane tview.Primitive, focused bool) {
	background := r.theme.EditableBackground
	if focused {
		background = r.theme.FocusedBackground
	}
	style := tcell.StyleDefault.Background(background).Foreground(r.theme.Text)

	switch p := pane.(type) {
	case *tview.List:
		p.SetSelectedStyle(style)
	case *tview.Table:
		p.SetSelectedStyle(style)
	}
}

// optionValueDisplay renders one setting's current value for the table:
// a readable Yes/No for a boolean, an enum choice's own label rather
// than its config literal, and the raw value for everything else.
func (r *Root) optionValueDisplay(opt optionSpec) string {
	return r.renderOptionValue(opt, opt.value(r))
}

// renderOptionValue renders one literal value for opt the way the screen
// shows it — the radio glyph for a boolean, an enum choice's own label
// rather than its config literal, the raw text otherwise.
//
// Takes the value rather than reading the current one, so the default
// hint beside a changed setting is rendered in exactly the same
// vocabulary as the value it's being compared against (see
// optionDefaultHint) — a hint reading "default: false" next to a value
// reading "○" would make the reader do the translation themselves.
func (r *Root) renderOptionValue(opt optionSpec, value string) string {
	doc, ok := opt.doc()
	if ok && doc.Kind == config.KindBool {
		// The same filled/outline circle this app already uses for a
		// boolean everywhere else — the panel's own checkbox column and
		// the sed dialog's flag toggles (see checkboxText) — rather than
		// the words Yes/No, per the user's own explicit request that
		// every such toggle look identical throughout.
		return checkboxText(value == "true")
	}
	if opt.choices != nil {
		for _, c := range opt.choices(r) {
			if c.value == value {
				return c.label
			}
		}
	}
	return value
}

// optionDefaultHint is what the table shows beside a value: nothing at
// all while the value is the default, and "default: <value>" once it
// isn't.
//
// Replaces an earlier column that named where the value came from
// ("default" / "system-wide" / "changed by you"), which a real report
// rightly called out as both noise and misleading: toggling a setting
// twice puts the default value back, but the key stays in the config
// file, so that column went on claiming "changed by you" about a value
// that plainly wasn't changed any more. Comparing against the default
// can't drift that way — it describes the value on screen rather than
// the history behind it.
//
// The provenance itself is still worth knowing occasionally, and still
// shown, in the one place with room to explain it properly: the info
// window (see showOptionInfo).
func (r *Root) optionDefaultHint(opt optionSpec) string {
	doc, ok := opt.doc()
	if !ok || doc.Default == opt.value(r) {
		return ""
	}
	return "default: " + r.renderOptionValue(opt, doc.Default)
}

// settingOriginLabel renders where a setting's current value came from,
// for the info window (see optionDefaultHint on why it left the table).
func (r *Root) settingOriginLabel(key string) string {
	origin, ok := r.settingOrigins[key]
	if !ok {
		return config.OriginDefault.String()
	}
	return origin.String()
}

// currentOptionCategory is the category the left-hand list currently
// has selected.
func (r *Root) currentOptionCategory() (optionCategory, bool) {
	categories := optionCategories()
	if r.optionsCategory < 0 || r.optionsCategory >= len(categories) {
		return optionCategory{}, false
	}
	return categories[r.optionsCategory], true
}

// optionAtRow is the setting shown on one row of the settings table.
func (r *Root) optionAtRow(row int) (optionSpec, bool) {
	cat, ok := r.currentOptionCategory()
	if !ok || row < 0 || row >= len(cat.options) {
		return optionSpec{}, false
	}
	return cat.options[row], true
}

// activateOptionRow is Enter (or a click) on a setting: change its
// value, by whichever means suits its kind.
func (r *Root) activateOptionRow(row int) {
	opt, ok := r.optionAtRow(row)
	if !ok {
		return
	}
	doc, _ := opt.doc()

	switch {
	case doc.Kind == config.KindBool:
		// Toggled in place — no dialog for a two-state value, since
		// picking from a list of exactly "Yes" and "No" is pure
		// ceremony.
		opt.apply(r, strconv.FormatBool(opt.value(r) != "true"))
		r.renderOptions()
	case opt.choices != nil:
		r.cycleOptionChoice(opt)
	default:
		r.editOptionValue(opt)
	}
}

// cycleOptionChoice advances an enum setting to its next value and
// applies it there and then.
//
// No intermediate picker dialog, per the user's own explicit request
// that every option be set immediately: activating the row *is* the
// change, exactly as it already is for a boolean, rather than opening a
// list to choose from and then confirming. For a color scheme that also
// means each step is visible at once, which is the only honest way to
// pick one — a scheme name says very little about what it looks like.
//
// Wraps around, so the list is reachable in either direction by going
// far enough, and an unrecognized current value (a hand-edited config
// naming a scheme that no longer exists) starts from the first choice
// rather than sticking.
func (r *Root) cycleOptionChoice(opt optionSpec) {
	choices := opt.choices(r)
	if len(choices) == 0 {
		return
	}

	next := 0
	current := opt.value(r)
	for i, c := range choices {
		if c.value == current {
			next = (i + 1) % len(choices)
			break
		}
	}

	opt.apply(r, choices[next].value)
	r.renderOptions()
}

// editOptionValue opens a one-line input for a setting typed rather than
// picked — currently the two integer trash limits.
//
// Enter commits, Escape discards: the one place in this screen where a
// value isn't applied the instant it changes, because applying every
// intermediate keystroke would mean typing "30" briefly sets 3.
func (r *Root) editOptionValue(opt optionSpec) {
	doc, _ := opt.doc()

	r.optionsInput.SetLabel(" " + opt.label + ": ")
	r.optionsInput.SetText(opt.value(r))
	if doc.Kind == config.KindInt {
		// Rejects a non-digit as it's typed, rather than accepting it
		// and reporting a parse failure on Enter.
		r.optionsInput.SetAcceptanceFunc(tview.InputFieldInteger)
	} else {
		r.optionsInput.SetAcceptanceFunc(nil)
	}

	r.optionsInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			if value := strings.TrimSpace(r.optionsInput.GetText()); value != "" {
				opt.apply(r, value)
			}
		}
		r.hideOverlay()
		r.renderOptions()
	})

	width := 48
	height := 1
	x, y := r.centeredOnScreen(width, height)
	x, y, width, height = r.clampToScreen(x, y, width, height)
	r.optionsInput.SetRect(x, y, width, height)

	r.pushOverlay(optionsInputPage, r.optionsInput, nil)
}

// showOptionInfo opens the small centered window explaining one setting
// — the info button's own action.
//
// A real, focusable window rather than a hover tooltip, per the user's
// own explicit design: it's reachable without a mouse, it can hold more
// than a line, and it closes the same way everything else does (Escape,
// or a click outside it).
func (r *Root) showOptionInfo(opt optionSpec) {
	doc, _ := opt.doc()

	var b strings.Builder
	fmt.Fprintf(&b, "[::b]%s[::-]\n\n", opt.label)
	b.WriteString(opt.help)
	fmt.Fprintf(&b, "\n\n[::d]Config key:[::-] %s", opt.key)
	fmt.Fprintf(&b, "\n[::d]Default:[::-] %s", doc.Default)
	fmt.Fprintf(&b, "\n[::d]Currently:[::-] %s (%s)", opt.value(r), r.settingOriginLabel(opt.key))
	b.WriteString("\n\n[::d]Escape or a click outside closes this.[::-]")

	r.optionsInfo.SetText(b.String())
	r.optionsInfo.SetDynamicColors(true)

	width, height := r.optionsInfoSize(b.String())
	x, y := r.centeredOnScreen(width, height)
	x, y, width, height = r.clampToScreen(x, y, width, height)
	r.optionsInfo.SetRect(x, y, width, height)

	r.pushOverlay(optionsInfoPage, r.optionsInfo, nil)
}

// optionsInfoSize picks the info window's own size: a fixed, readable
// column width, and a height derived from how many lines the text
// actually wraps to at that width.
//
// Measured rather than guessed — the help texts differ a lot in length,
// and a fixed height would either clip the long ones or leave the short
// ones mostly empty.
func (r *Root) optionsInfoSize(text string) (width, height int) {
	const contentWidth = 56
	const padding = 4 // SetBorderPadding's own 2 columns either side

	lines := 0
	for _, paragraph := range strings.Split(tview.Escape(text), "\n") {
		// Tag-aware, since the text carries [::b]/[::d] style tags that
		// occupy no columns on screen.
		w := tview.TaggedStringWidth(paragraph)
		lines += max(1, (w+contentWidth-1)/contentWidth)
	}
	return contentWidth + padding, lines + 2 // +2 for the top/bottom padding rows
}

// captureOptionsTableKey adds the settings table's own two extra keys:
// "?" and F1 open the selected setting's explanation.
//
// Both, because "?" is the conventional "explain this" key in a list
// like this while F1 is what this app already means by help everywhere
// else — and neither costs anything the table itself was using.
func (r *Root) captureOptionsTableKey(event *tcell.EventKey) *tcell.EventKey {
	row, _ := r.optionsTable.GetSelection()

	// Space changes the selected setting, the same as Enter — per the
	// user's own explicit request, and matching every other toggle in
	// this app, where Space has always been the second way to flip the
	// thing under the cursor (the panel's own checkbox column, every
	// button here — see spaceAlsoActivates).
	if event.Key() == tcell.KeyRune && event.Rune() == ' ' {
		r.activateOptionRow(row)
		return nil
	}

	if event.Key() != tcell.KeyF1 && event.Rune() != '?' {
		return event
	}
	if opt, ok := r.optionAtRow(row); ok {
		r.showOptionInfo(opt)
	}
	return nil
}

// captureOptionsTableMouse routes a click on the info column to that
// setting's explanation, and a click anywhere else on a row to changing
// its value.
func (r *Root) captureOptionsTableMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftClick {
		return action, event
	}
	x, y := event.Position()
	if !r.optionsTable.InRect(x, y) {
		return action, event
	}

	row, column := r.optionsTable.CellAt(x, y)
	opt, ok := r.optionAtRow(row)
	if !ok {
		return action, event
	}
	// Select the clicked row first either way, so the cursor follows the
	// mouse the same as it does in the file panel.
	r.optionsTable.Select(row, 0)

	if column == optionsColInfo {
		r.showOptionInfo(opt)
	} else {
		r.activateOptionRow(row)
	}
	return tview.MouseConsumed, nil
}

// Both resets ask first, through the same shared confirmation every
// other hard-to-undo action in this app goes through (see openConfirm in
// trash.go) rather than a dialog of their own: a reset discards work —
// possibly a long-tuned set of preferences — and, unlike every single
// setting change on this screen, there is no equally quick way back.
// Cancel is preselected, so a stray Enter can never trigger one.

// resetCurrentOptionCategory resets every setting in the category
// currently shown — "Reset category".
func (r *Root) resetCurrentOptionCategory() {
	cat, ok := r.currentOptionCategory()
	if !ok {
		return
	}
	// Named rather than "this category": the button sits under a pane
	// that may not be the one the reader is looking at.
	r.openConfirm(
		fmt.Sprintf("Reset every setting under %q to its default?", cat.name),
		"Yes, reset "+cat.name,
		func() { r.resetOptions(cat.options) },
	)
}

// resetAllOptions resets every setting in every category — "Reset all".
func (r *Root) resetAllOptions() {
	var all []optionSpec
	for _, cat := range optionCategories() {
		all = append(all, cat.options...)
	}
	r.openConfirm(
		fmt.Sprintf("Reset all %d settings, in every category, to their defaults?", len(all)),
		"Yes, reset everything",
		func() { r.resetOptions(all) },
	)
}

// resetOptions is the shared body of both reset buttons: stop
// overriding each of these settings, then apply whatever value that
// leaves in force.
//
// Applying afterwards matters — removing the key from the config file
// only changes what the *next* start would read. Feeding the resulting
// value back through the setting's own apply is what makes the reset
// visible immediately, on the same no-save-button terms as every other
// change here.
func (r *Root) resetOptions(options []optionSpec) {
	for _, opt := range options {
		value := r.resetSetting(opt.key)
		// Without persistence: the value being applied is precisely the
		// one that resetting produced, and writing it back would
		// re-create the key the reset just removed (see
		// applyWithoutPersisting).
		r.applyWithoutPersisting(func() { opt.apply(r, value) })
	}
	r.refreshSettingOrigins()
	r.renderOptions()
}

// refreshSettingOrigins re-reads which tier each setting's value
// currently comes from. Called after a reset, and after the config file
// has been edited externally (see editConfigFile).
func (r *Root) refreshSettingOrigins() {
	_, origins, _, _ := loadInitialSettings()
	r.settingOrigins = origins
}

// editConfigFile hands the user's own config file to their editor —
// the escape hatch for everything this screen doesn't offer.
//
// Creates the file first if it doesn't exist yet (see
// config.EnsureUserFile), so a first-time reader gets the full,
// commented-out listing of every available setting rather than an empty
// buffer. Re-reads everything afterwards, since an external edit can
// have changed anything.
func (r *Root) editConfigFile() {
	path := userConfigFilePath()
	if path == "" {
		r.showError(fmt.Errorf("no user config directory available to write a config file to"))
		return
	}
	if err := config.EnsureUserFile(path); err != nil {
		r.showError(fmt.Errorf("creating %s: %w", path, err))
		return
	}

	r.runEditor(path, 0)
	r.reloadSettingsFromDisk()
}

// reloadSettingsFromDisk re-reads both config tiers and puts whatever
// they now say into effect — after an external edit of the config file.
//
// Applies through the same per-setting apply functions a normal change
// goes through (see optioncatalog.go), rather than reaching into the
// application's state directly: an externally edited setting has to end
// up exactly as applied as one changed here.
func (r *Root) reloadSettingsFromDisk() {
	settings, origins, schemes, warnings := loadInitialSettings()
	r.settings = settings
	r.settingOrigins = origins
	r.colorSchemes = schemes

	r.applyWithoutPersisting(func() {
		for _, cat := range optionCategories() {
			for _, opt := range cat.options {
				if value, ok := settingValueByKey(settings, opt.key); ok {
					opt.apply(r, value)
				}
			}
		}
	})

	if len(warnings) > 0 {
		r.showError(fmt.Errorf("config: %s", strings.Join(warnings, "; ")))
	}
	r.renderOptions()
}

// duplicateColorScheme copies the active color scheme into the user's
// own colorschemes directory under a fresh name and opens it in their
// editor — "New color scheme".
//
// A copy rather than a blank file: a scheme has a lot of fields, and
// starting from the one currently on screen means every edit is a
// visible change to something already working, instead of a guess at
// what the field names are.
//
// This is also where hand-edited JSON genuinely belongs, as opposed to
// the settings themselves: schemes already are JSON files, and editing
// them in a real editor beats any control this screen could offer.
func (r *Root) duplicateColorScheme() {
	dir := config.UserColorSchemeDir()
	if dir == "" {
		r.showError(fmt.Errorf("no user config directory available to write a color scheme to"))
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.showError(fmt.Errorf("creating %s: %w", dir, err))
		return
	}

	source := config.FindColorScheme(r.colorSchemes, r.settings.ColorScheme)
	slug := r.freeColorSchemeSlug(dir, r.settings.ColorScheme)
	source.Name = source.Name + " (copy)"

	data, err := json.MarshalIndent(source, "", "  ")
	if err != nil {
		r.showError(fmt.Errorf("rendering the color scheme: %w", err))
		return
	}
	path := filepath.Join(dir, slug+".json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		r.showError(fmt.Errorf("writing %s: %w", path, err))
		return
	}

	r.runEditor(path, 0)
	r.reloadColorSchemes()
}

// freeColorSchemeSlug picks a filename stem in dir that isn't taken yet,
// starting from "<base>-copy" and counting up — so duplicating the same
// scheme twice produces two files rather than silently overwriting the
// first.
func (r *Root) freeColorSchemeSlug(dir, base string) string {
	if base == "" {
		base = "scheme"
	}
	candidate := base + "-copy"
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, candidate+".json")); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-copy-%d", base, i)
	}
}

// reloadColorSchemes re-scans both colorschemes directories, so a scheme
// created or edited during this run shows up without a restart.
//
// Needed because schemes are otherwise only read once at startup (see
// loadInitialSettings) — which was fine when there was no way to create
// one from inside the application, and stopped being fine the moment
// duplicateColorScheme existed.
func (r *Root) reloadColorSchemes() {
	_, _, schemes, _ := loadInitialSettings()
	r.colorSchemes = schemes
	// Re-apply the active one: if it was the scheme just edited, its
	// colors have changed on disk and the running application is still
	// showing the old ones.
	r.applyThemeOnly(r.settings.ColorScheme)
	r.renderOptions()
}

// applyThemeOnly switches the running application to a color scheme
// without persisting the choice — the live-preview half of
// applyColorScheme (see its own doc comment for the persisting
// version).
//
// Split out for two callers that both need exactly this: the scheme
// picker's own preview-as-you-browse, and reloadColorSchemes repainting
// after an edit. Neither is a decision the user made about which scheme
// to use, so neither should touch the config file.
func (r *Root) applyThemeOnly(slug string) {
	r.applyTheme(config.FindColorScheme(r.colorSchemes, slug).Resolve())
}
