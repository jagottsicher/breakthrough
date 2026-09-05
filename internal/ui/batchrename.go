package ui

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/batchrename"
)

// The Batch Rename screen: a full-screen editor, replacing the context
// menu's old "Mass rename" placeholder — same overall shape as the
// Options screen (see optionsscreen.go), reused deliberately rather
// than invented fresh: a list of "steps" down the left plays the same
// role optionsCategories does, and the selected step's own settings
// table on the right is rendered and driven exactly the same way
// optionsTable is (see batchRenameField/batchRenameStep in
// batchrenamecatalog.go, the direct counterpart of optionSpec/
// optionCategory). Someone who has already used the Options screen
// needs to learn nothing new here.
//
// What's different from Options: there's no config file underneath, so
// nothing is written until "Rename" is actually pressed (and confirmed
// — see confirmApplyBatchRename), and the settings drive a live,
// always-visible preview table instead (see renderBatchRenamePreview) —
// this screen's whole reason to exist, per the user's own explicit
// request for "ordentliche previews".
//
// The actual name-transform/collision/apply logic lives in
// internal/batchrename, not here — this file only ever renders a
// batchrename.Rules and reads back what the widgets say to change on
// it, the same fsops-vs-ui split this project keeps everywhere else.

const (
	batchRenamePage      = "batch-rename"
	batchRenameInputPage = "batch-rename-input"

	// batchRenameFieldsHeight fits the widest step's own field count
	// (Numbering, at 4 rows) plus SetBorderPadding's top row — checked
	// against a real render, the same way sedLayout's own fixed height
	// was (see openSedReplace).
	batchRenameFieldsHeight = 6
)

// newBatchRenameScreen builds the whole screen once, at startup — the
// same build-once/repopulate-on-open shape newOptionsScreen already
// uses.
func (r *Root) newBatchRenameScreen() {
	r.batchRenameStepsList = tview.NewList().ShowSecondaryText(false)
	r.batchRenameStepsList.SetHighlightFullLine(true)
	r.batchRenameStepsList.SetBorderPadding(1, 0, 1, 1)
	r.batchRenameStepsList.SetChangedFunc(func(index int, _, _ string, _ rune) {
		r.batchRenameStep = index
		r.renderBatchRenameFields()
	})

	r.batchRenameFieldsTable = tview.NewTable()
	r.batchRenameFieldsTable.SetBorders(false)
	r.batchRenameFieldsTable.SetBorderPadding(1, 0, 2, 1)
	r.batchRenameFieldsTable.SetSelectable(true, false) // whole rows: one field per row
	r.batchRenameFieldsTable.SetSelectedFunc(func(row, _ int) { r.activateBatchRenameFieldRow(row) })
	r.batchRenameFieldsTable.SetMouseCapture(r.captureBatchRenameFieldsMouse)

	r.batchRenamePreviewTable = tview.NewTable()
	r.batchRenamePreviewTable.SetBorders(false)
	r.batchRenamePreviewTable.SetBorderPadding(1, 0, 2, 1)
	r.batchRenamePreviewTable.SetSelectable(true, false)

	r.batchRenameStatus = tview.NewTextView()
	r.batchRenameStatus.SetWrap(false)

	r.batchRenameButtons = r.newBatchRenameButtons()

	rightPane := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.batchRenameFieldsTable, batchRenameFieldsHeight, 0, true).
		AddItem(r.batchRenamePreviewTable, 0, 1, false).
		AddItem(r.batchRenameStatus, 1, 0, false).
		AddItem(r.batchRenameButtons, 1, 0, false)

	body := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.batchRenameStepsList, optionsCategoryWidth, 0, false).
		AddItem(rightPane, 0, 1, true)

	r.batchRenameTitleBar = tview.NewTextView()
	r.batchRenameTitleBar.SetWrap(false)
	r.batchRenameTitleBar.SetText(" Batch Rename ")

	r.batchRenameHint = tview.NewTextView()
	r.batchRenameHint.SetWrap(false)
	r.batchRenameHint.SetText(" ←/→: pane · ↑/↓: move · Enter/Space: change · Tab: buttons · Esc: close ")

	r.batchRenameLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.batchRenameTitleBar, 1, 0, false).
		AddItem(body, 0, 1, true).
		AddItem(r.batchRenameHint, 1, 0, false)

	// One shared input field for a typed value, the same
	// one-widget-reused-per-edit pattern r.optionsInput already follows
	// (see editOptionValue's own doc comment). Enum fields need no
	// widget of their own — activating the row cycles them in place
	// (see cycleBatchRenameChoice).
	r.batchRenameInput = tview.NewInputField()

	// Shared Tab/Escape handling (see captureBatchRenameKey), plus the
	// left/right pane arrows (see captureBatchRenamePaneArrows) — the
	// same two-layer chain newOptionsScreen wires onto its own two
	// panes.
	r.batchRenameStepsList.SetInputCapture(chainKeyCaptures(r.captureBatchRenameKey, r.captureBatchRenamePaneArrows))
	r.batchRenameFieldsTable.SetInputCapture(chainKeyCaptures(r.captureBatchRenameKey,
		chainKeyCaptures(r.captureBatchRenamePaneArrows, r.captureBatchRenameFieldsTableKey)))
	r.batchRenamePreviewTable.SetInputCapture(r.captureBatchRenameKey)

	// Visible focus, driven by the focus events themselves rather than
	// re-derived at Draw time — see setOptionsPaneFocused's own doc
	// comment (in optionsscreen.go, shared verbatim here) for why.
	r.batchRenameStepsList.SetFocusFunc(func() { r.setOptionsPaneFocused(r.batchRenameStepsList, true) })
	r.batchRenameStepsList.SetBlurFunc(func() { r.setOptionsPaneFocused(r.batchRenameStepsList, false) })
	r.batchRenameFieldsTable.SetFocusFunc(func() { r.setOptionsPaneFocused(r.batchRenameFieldsTable, true) })
	r.batchRenameFieldsTable.SetBlurFunc(func() { r.setOptionsPaneFocused(r.batchRenameFieldsTable, false) })
	r.setOptionsPaneFocused(r.batchRenameStepsList, false)
	r.setOptionsPaneFocused(r.batchRenameFieldsTable, false)
}

// newBatchRenameButtons builds the row of screen-wide actions under the
// preview: Rename actually does it (after confirming — see
// confirmApplyBatchRename), Reset all steps clears every field back to
// its no-op zero value without closing the screen (nothing has been
// written to disk yet, so unlike the Options screen's own resets this
// needs no confirmation of its own), Cancel discards everything and
// closes.
func (r *Root) newBatchRenameButtons() *tview.Flex {
	type buttonSpec struct {
		button **tview.Button
		label  string
		action func()
	}
	specs := []buttonSpec{
		{&r.batchRenameApplyBtn, "Rename", r.confirmApplyBatchRename},
		{&r.batchRenameResetBtn, "Reset all steps", r.resetBatchRenameSteps},
		{&r.batchRenameCancelBtn, "Cancel", r.closeBatchRename},
	}
	row := tview.NewFlex().SetDirection(tview.FlexColumn)
	for _, spec := range specs {
		b := tview.NewButton(spec.label)
		b.SetSelectedFunc(spec.action)
		b.SetInputCapture(chainKeyCaptures(r.captureBatchRenameKey, spaceAlsoActivates(spec.action)))
		*spec.button = b
		row.AddItem(b, 0, 1, false)
	}
	return row
}

// batchRenameButtonList is the button row's own fixed order — shared by
// its construction and by the focus ring below, the same reasoning
// optionsButtonList already documents.
func (r *Root) batchRenameButtonList() []*tview.Button {
	return []*tview.Button{r.batchRenameApplyBtn, r.batchRenameResetBtn, r.batchRenameCancelBtn}
}

// batchRenameFocusRing is Tab's own stop order: the steps list, the
// current step's fields, the preview (so a long selection can be
// scrolled without a mouse), then each action button.
func (r *Root) batchRenameFocusRing() []tview.Primitive {
	ring := []tview.Primitive{r.batchRenameStepsList, r.batchRenameFieldsTable, r.batchRenamePreviewTable}
	return append(ring, batchRenameButtonPrimitives(r)...)
}

func batchRenameButtonPrimitives(r *Root) []tview.Primitive {
	buttons := r.batchRenameButtonList()
	primitives := make([]tview.Primitive, len(buttons))
	for i, b := range buttons {
		primitives[i] = b
	}
	return primitives
}

// cycleBatchRenameFocus moves focus one step around
// batchRenameFocusRing — forward for Tab, backward for Shift+Tab. See
// cycleOptionsFocus (optionsscreen.go) for the identical reasoning this
// mirrors.
func (r *Root) cycleBatchRenameFocus(delta int) bool {
	ring := r.batchRenameFocusRing()
	for i, stop := range ring {
		if !stop.HasFocus() {
			continue
		}
		next := (i + delta) % len(ring)
		if next < 0 {
			next += len(ring)
		}
		r.app.SetFocus(ring[next])
		return true
	}
	return false
}

// captureBatchRenameKey is this screen's own shared key handling,
// installed on every widget in the focus ring — see captureOptionsKey
// for the identical reasoning (Tab/Shift+Tab cycle, Escape closes,
// centralized here since a Button/Table has no DoneFunc of its own).
func (r *Root) captureBatchRenameKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyTab:
		if r.cycleBatchRenameFocus(1) {
			return nil
		}
	case tcell.KeyBacktab:
		if r.cycleBatchRenameFocus(-1) {
			return nil
		}
	case tcell.KeyEscape:
		r.closeBatchRename()
		return nil
	}
	return event
}

// captureBatchRenamePaneArrows moves between the steps list and its
// fields table with the left/right arrow keys — see
// captureOptionsPaneArrows for the identical reasoning (including why
// Left is deliberately inert on the steps list rather than wrapping).
func (r *Root) captureBatchRenamePaneArrows(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyRight:
		if r.batchRenameStepsList.HasFocus() {
			r.app.SetFocus(r.batchRenameFieldsTable)
			return nil
		}
	case tcell.KeyLeft:
		if r.batchRenameFieldsTable.HasFocus() {
			r.app.SetFocus(r.batchRenameStepsList)
			return nil
		}
	}
	return event
}

// captureBatchRenameFieldsTableKey adds Space as a second way to
// activate the selected field — the same convention every other toggle
// in this app follows (see captureOptionsTableKey).
func (r *Root) captureBatchRenameFieldsTableKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyRune && event.Rune() == ' ' {
		row, _ := r.batchRenameFieldsTable.GetSelection()
		r.activateBatchRenameFieldRow(row)
		return nil
	}
	return event
}

// captureBatchRenameFieldsMouse routes a click anywhere on a field's row
// to changing its value — see captureOptionsTableMouse, simpler here
// since there's no separate info column to route around.
func (r *Root) captureBatchRenameFieldsMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftClick {
		return action, event
	}
	x, y := event.Position()
	if !r.batchRenameFieldsTable.InRect(x, y) {
		return action, event
	}
	row, _ := r.batchRenameFieldsTable.CellAt(x, y)
	if _, ok := r.batchRenameFieldAtRow(row); !ok {
		return action, event
	}
	r.batchRenameFieldsTable.Select(row, 0)
	r.activateBatchRenameFieldRow(row)
	return tview.MouseConsumed, nil
}

// openBatchRename is the context menu's "Batch rename": opens the
// screen fresh, for the current checkbox selection (or the current
// row) — the same target-gathering fallback Sed Replace/Move to
// Trash/Remove all already share (see selectedOrCurrentPaths).
//
// Always starts from a blank Rules{} rather than remembering the last
// session's own settings: a stale "Find: vacation" silently applied to
// a completely different selection is a much worse surprise than
// having to re-enter a pipeline that was actually meant for the files
// it was built against.
func (r *Root) openBatchRename() {
	targets := r.selectedOrCurrentPaths()
	if len(targets) == 0 {
		return
	}
	r.batchRenameTargets = targets
	r.batchRenameRules = batchrename.Rules{}
	r.batchRenameStep = 0

	r.renderBatchRenameStepsList()
	r.renderBatchRenameFields()
	r.renderBatchRenamePreview()

	// The whole layout is the overlay, not just the fields table — the
	// same fix captureOutsideClick's own doc comment on openOptions
	// documents: registering a narrower widget makes a click on the
	// steps list, the preview, or the buttons count as "outside" and
	// close the screen.
	//
	// Restores to the fields table, not the steps list: this same
	// callback also fires when a *nested* overlay closes (the typed-
	// value editor — see editBatchRenameField), and every field lives
	// on the fields table, never on the steps list — a real bug, caught
	// live: committing a value with Enter silently landed keyboard focus
	// back on the steps list, so the very next Down arrow moved between
	// steps instead of between that step's own fields.
	r.showOverlayWithRestore(batchRenamePage, r.batchRenameLayout, func() {
		r.app.SetFocus(r.batchRenameFieldsTable)
	})
}

// closeBatchRename hides the screen. Nothing to save or discard —
// nothing was ever written; see confirmApplyBatchRename for the one
// path that actually touches disk.
func (r *Root) closeBatchRename() {
	r.hideOverlay()
}

// renderBatchRenameStepsList fills the left-hand list — rebuilt rather
// than kept in sync, the same as renderOptionCategories.
func (r *Root) renderBatchRenameStepsList() {
	r.batchRenameStepsList.SetChangedFunc(nil)
	r.batchRenameStepsList.Clear()
	for _, step := range batchRenameSteps() {
		r.batchRenameStepsList.AddItem(step.name, "", 0, nil)
	}
	r.batchRenameStepsList.SetCurrentItem(r.batchRenameStep)
	r.batchRenameStepsList.SetChangedFunc(func(index int, _, _ string, _ rune) {
		r.batchRenameStep = index
		r.renderBatchRenameFields()
	})
}

// currentBatchRenameStep is the step the left-hand list currently has
// selected.
func (r *Root) currentBatchRenameStep() (batchRenameStep, bool) {
	steps := batchRenameSteps()
	if r.batchRenameStep < 0 || r.batchRenameStep >= len(steps) {
		return batchRenameStep{}, false
	}
	return steps[r.batchRenameStep], true
}

// batchRenameFieldAtRow is the field shown on one row of the fields
// table.
func (r *Root) batchRenameFieldAtRow(row int) (batchRenameField, bool) {
	step, ok := r.currentBatchRenameStep()
	if !ok || row < 0 || row >= len(step.fields) {
		return batchRenameField{}, false
	}
	return step.fields[row], true
}

// renderBatchRenameFields fills the right-hand table with the selected
// step's own fields: label and current value — no info column, unlike
// Options' own table, since this first version has no per-field help
// text to show (see the package doc's own scope note).
func (r *Root) renderBatchRenameFields() {
	r.batchRenameFieldsTable.Clear()

	step, ok := r.currentBatchRenameStep()
	if !ok {
		return
	}
	for row, f := range step.fields {
		r.batchRenameFieldsTable.SetCell(row, 0,
			tview.NewTableCell(padRight(f.label, optionsLabelWidth)).
				SetTextColor(r.theme.Text).
				SetSelectable(true))
		r.batchRenameFieldsTable.SetCell(row, 1,
			tview.NewTableCell(r.batchRenameFieldDisplay(f)).
				SetTextColor(r.theme.Text).
				SetSelectable(true))
	}

	if row, _ := r.batchRenameFieldsTable.GetSelection(); row >= r.batchRenameFieldsTable.GetRowCount() {
		r.batchRenameFieldsTable.Select(0, 0)
	}
}

// batchRenameFieldDisplay renders one field's current value the way the
// table shows it: the shared radio glyph for a boolean (see
// checkboxText), an enum choice's own label rather than its stored
// value, the raw text otherwise — the same rendering rules
// renderOptionValue already applies for the Options screen.
func (r *Root) batchRenameFieldDisplay(f batchRenameField) string {
	value := f.value(r)
	switch f.kind {
	case brFieldBool:
		return checkboxText(value == "true")
	case brFieldEnum:
		for _, c := range f.choices(r) {
			if c.value == value {
				return c.label
			}
		}
	}
	return value
}

// activateBatchRenameFieldRow is Enter, Space, or a click on a field:
// change its value, by whichever means suits its kind — the same
// three-way split activateOptionRow already makes.
func (r *Root) activateBatchRenameFieldRow(row int) {
	f, ok := r.batchRenameFieldAtRow(row)
	if !ok {
		return
	}
	switch f.kind {
	case brFieldBool:
		f.apply(r, strconv.FormatBool(f.value(r) != "true"))
		r.renderBatchRenameFields()
	case brFieldEnum:
		r.cycleBatchRenameChoice(f)
	default:
		r.editBatchRenameField(f)
	}
}

// cycleBatchRenameChoice advances an enum field to its next value and
// applies it there and then — no intermediate picker, the same
// immediate-effect convention cycleOptionChoice already establishes.
func (r *Root) cycleBatchRenameChoice(f batchRenameField) {
	choices := f.choices(r)
	if len(choices) == 0 {
		return
	}

	next := 0
	current := f.value(r)
	for i, c := range choices {
		if c.value == current {
			next = (i + 1) % len(choices)
			break
		}
	}

	f.apply(r, choices[next].value)
	r.renderBatchRenameFields()
}

// editBatchRenameField opens a one-line input for a field typed rather
// than picked (Find/Replace with, the trim/numbering counts, the
// extension's "Set to" value) — see editOptionValue for the identical
// reasoning (Enter commits, Escape discards).
func (r *Root) editBatchRenameField(f batchRenameField) {
	r.batchRenameInput.SetLabel(" " + f.label + ": ")
	r.batchRenameInput.SetText(f.value(r))
	if f.kind == brFieldInt {
		r.batchRenameInput.SetAcceptanceFunc(tview.InputFieldInteger)
	} else {
		r.batchRenameInput.SetAcceptanceFunc(nil)
	}

	r.batchRenameInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			f.apply(r, r.batchRenameInput.GetText())
		}
		r.hideOverlay()
		r.renderBatchRenameFields()
	})

	width, height := 56, 1
	x, y := r.centeredOnScreen(width, height)
	x, y, width, height = r.clampToScreen(x, y, width, height)
	r.batchRenameInput.SetRect(x, y, width, height)

	r.pushOverlay(batchRenameInputPage, r.batchRenameInput, nil)
}

// renderBatchRenamePreview is this screen's whole reason to exist: a
// live, always-visible Old name/New name table over every target, run
// fresh from batchrename.Plan on every single field change (see
// batchRenameField's own apply functions) — no explicit "Preview"
// button to press first, unlike Sed Replace's own dialog, since a
// rename has no file content to read and no meaningful delay to hide
// behind an extra click.
//
// Deliberately shows every target, not just the ones that would
// change (unlike Plan's own PlanResult, and unlike sedPreviewRows'
// identical convention for content changes) — the whole selection's
// outcome, changed or not, is what someone reviewing a batch of
// possibly a few hundred files actually wants to see in one place
// before pressing Rename, per the user's own explicit request for "a
// proper preview".
func (r *Root) renderBatchRenamePreview() {
	r.batchRenamePreviewTable.Clear()

	header := func(col int, text string) {
		r.batchRenamePreviewTable.SetCell(0, col, tview.NewTableCell(text).
			SetTextColor(r.theme.Text).SetAttributes(tcell.AttrBold).SetSelectable(false))
	}
	header(0, "Name")
	header(1, "New name")
	header(2, "Note")
	r.batchRenamePreviewTable.SetFixed(1, 0)

	targets := r.batchRenameTargets
	result := batchrename.Plan(targets, r.batchRenameRules)
	r.batchRenamePendingChanges = result.Changes

	changedTo := make(map[string]string, len(result.Changes))
	for _, c := range result.Changes {
		changedTo[c.From] = c.To
	}
	problemReason := make(map[string]string, len(result.Problems))
	for _, p := range result.Problems {
		problemReason[p.Path] = p.Reason
	}

	changing, conflicts := 0, 0
	for i, path := range targets {
		name := filepath.Base(path)
		row := i + 1

		color := r.theme.PlaceholderText // dimmer: this row isn't changing, same role skipped/unchanged rows already have elsewhere
		newName := name
		note := "(unchanged)"

		switch {
		case problemReason[path] != "":
			color = r.theme.EntryError // the same color a broken symlink gets — something is wrong here
			newName = "—"
			note = problemReason[path]
			conflicts++
		case changedTo[path] != "":
			color = r.theme.Text
			newName = filepath.Base(changedTo[path])
			note = ""
			changing++
		}

		r.batchRenamePreviewTable.SetCell(row, 0, tview.NewTableCell(name).SetTextColor(color))
		r.batchRenamePreviewTable.SetCell(row, 1, tview.NewTableCell(newName).SetTextColor(color))
		r.batchRenamePreviewTable.SetCell(row, 2, tview.NewTableCell(note).SetTextColor(color))
	}

	if len(targets) == 0 {
		r.batchRenamePreviewTable.SetCell(1, 0, tview.NewTableCell("Nothing selected.").SetTextColor(r.theme.PlaceholderText).SetSelectable(false))
	}
	r.batchRenamePreviewTable.ScrollToBeginning()

	unchanged := len(targets) - changing - conflicts
	r.batchRenameStatus.SetText(fmt.Sprintf(" %d changing · %d unchanged · %d conflict(s)", changing, unchanged, conflicts))
}

// resetBatchRenameSteps is "Reset all steps": every field back to its
// no-op zero value. No confirmation, unlike the Options screen's own
// resets — nothing here has been written to disk yet, so there is
// nothing at stake beyond retyping the pipeline.
func (r *Root) resetBatchRenameSteps() {
	r.batchRenameRules = batchrename.Rules{}
	r.renderBatchRenameFields()
	r.renderBatchRenamePreview()
}

// confirmApplyBatchRename is "Rename": asks first (see openConfirm),
// then actually renames every file the live preview currently shows as
// changing.
//
// Recomputes Plan once more right here rather than trusting
// batchRenamePendingChanges (already current as of the last render):
// closing that gap matters more here than it did for Sed Replace's own
// confirmApplySed, since Plan's own collision detection is a check
// against the live filesystem (see Plan's own doc comment) — something
// else could in principle have created a colliding file in the moment
// between the last keystroke and this click.
func (r *Root) confirmApplyBatchRename() {
	targets := r.batchRenameTargets
	rules := r.batchRenameRules
	result := batchrename.Plan(targets, rules)
	if len(result.Changes) == 0 {
		return
	}

	// Closes this screen first, the same as openPurgeConfirm already
	// does for Remove/Empty Trash: there is nothing left on it worth
	// keeping visible once the question below is being asked. Not
	// openPurgeConfirm itself — its wording ("Yes, delete permanently")
	// is wrong for a rename, which Undo (see undoLastBatchRename) can
	// reverse.
	r.closeAllOverlays()
	r.openConfirm(
		fmt.Sprintf("Rename %d file(s)?", len(result.Changes)),
		"Yes, rename",
		func() {
			applied, err := batchrename.Apply(result.Changes)
			r.batchRenameUndo = applied
			if err != nil {
				r.showError(fmt.Errorf("batch rename: %d of %d files renamed, then: %w", len(applied), len(result.Changes), err))
				return
			}
			r.reloadPanel(nil)
		},
	)
}

// undoLastBatchRename is the context menu's "Undo last rename": reverses
// whatever confirmApplyBatchRename most recently actually applied — one
// level deep, cleared the moment it's used (a second Undo right after
// has nothing left to reverse; running Batch Rename again is exactly
// how you'd redo). No confirmation of its own: reversing a rename is
// exactly as safe as the rename itself was, which the Rename button's
// own "Yes, rename" already covered.
func (r *Root) undoLastBatchRename() {
	if len(r.batchRenameUndo) == 0 {
		r.showError(fmt.Errorf("undo last rename: nothing to undo"))
		return
	}
	changes := r.batchRenameUndo
	r.batchRenameUndo = nil
	_, err := batchrename.Undo(changes)
	r.reloadPanel(err)
}
