package ui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// Flag labels for duplicateOptionsList's own two boolean toggles —
// constants rather than repeated literals for the same reason
// sedFlagOrder's own labels are (see sedreplace.go): a typo in one
// place can't silently desync display text from the map key that
// tracks its state.
const (
	duplicateLabelStrftime = "Strftime-style format"
	duplicateLabelUseUnix  = "Use Unix timestamp"
)

// openDuplicate is the context menu's "Multiply" entry (mnemonic "m",
// see contextMenuTree) — there is no bare-letter shortcut of its own
// outside the menu, since "m" itself already opens the menu (see
// keymap.go's own doc comment on why m stays an ordinary, unchanged,
// immediately-firing plain key rather than a second chord family).
//
// Rebuilds the dialog's own fields fresh from the current Duplicate
// defaults on every open (see resetDuplicateForm) — the same "freshly
// configure a shared overlay against today's real target" shape
// openSedReplace/openChmod/openProperties all already follow.
func (r *Root) openDuplicate() {
	targets := r.selectedOrCurrentPaths()
	if len(targets) == 0 {
		return
	}
	r.duplicateTargets = targets
	r.resetDuplicateForm()

	// height fits duplicateTitleBar's own row (1) plus duplicateContentLayout's
	// three stacked widgets (duplicateForm's 16, duplicateOptionsList's 3,
	// duplicateActions' 2 — see newDuplicateContentLayout's own doc comment
	// for exactly how duplicateForm's own 16 was derived) — checked against
	// a real render, not guessed; a shorter value silently clipped the
	// bottom rows, the same lesson openSedReplace's own doc comment records.
	width, height := 78, 22
	_, _, screenWidth, screenHeight := r.GetRect() // Root fills the whole screen
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.duplicateLayout.SetRect(x, y, width, height)
	r.showOverlay(duplicatePage, r.duplicateLayout)
}

// newDuplicateForm builds the (initially empty) "Multiply" text-field
// form — called once from NewRoot; resetDuplicateForm populates it
// fresh on every open, the same reasoning newSedForm's own doc comment
// gives for Sed Replace: tview.Form doesn't lend itself to being reset
// in place.
//
// Deliberately holds only Target/the five text fields/the live preview
// — no border, matching every other floating widget in this app (see
// NewRoot's own comment on menu/quitConfirm/confirmDialog).
func (r *Root) newDuplicateForm() *tview.Form {
	f := tview.NewForm()
	f.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			r.hideOverlay()
			return nil
		}
		return event
	})
	return f
}

// resetDuplicateForm rebuilds duplicateForm's fields and
// duplicateOptionsList's rows fresh for the current r.duplicateTargets
// — Clear(true) first, the same reasoning resetSedForm's own doc
// comment gives.
//
// Every field starts out prefilled from the current Duplicate defaults
// (r.settings.Duplicate*) rather than empty — per the user's own design
// intent that the dialog always show "what would happen right now",
// not a blank form to fill in from scratch. r.duplicateStrategy is the
// dialog's own working copy of the enum choice (duplicateOptionsList
// has no Form field of its own to hold it); duplicateFlags is the same
// shape for the two booleans. Neither touches r.settings until the
// user actually confirms (see applyDuplicateSelection) — cycling or
// toggling one while experimenting, then hitting Cancel, must never
// silently overwrite the sticky default with a value the user just
// walked away from.
func (r *Root) resetDuplicateForm() {
	r.duplicateForm.Clear(true)

	r.duplicateForm.AddTextView("Target", duplicateTargetsLabel(r.duplicateTargets), 0, 2, true, false)

	r.duplicateSeparatorField = tview.NewInputField().SetLabel("Separator").SetText(r.settings.DuplicateSeparator)
	r.duplicateSeparatorField.SetChangedFunc(func(string) { r.renderDuplicatePreview() })
	r.duplicateForm.AddFormItem(r.duplicateSeparatorField)

	r.duplicateSuffixTextField = tview.NewInputField().SetLabel("Suffix text").SetText(r.settings.DuplicateSuffixText)
	r.duplicateSuffixTextField.SetChangedFunc(func(string) { r.renderDuplicatePreview() })
	r.duplicateForm.AddFormItem(r.duplicateSuffixTextField)

	r.duplicateNumberPaddingField = tview.NewInputField().
		SetLabel("Number padding (digits)").
		SetText(strconv.Itoa(r.settings.DuplicateNumberPadding)).
		SetAcceptanceFunc(tview.InputFieldInteger)
	r.duplicateNumberPaddingField.SetChangedFunc(func(string) { r.renderDuplicatePreview() })
	r.duplicateForm.AddFormItem(r.duplicateNumberPaddingField)

	r.duplicateDateTimeFormatField = tview.NewInputField().SetLabel("Date/time format").SetText(r.settings.DuplicateDateTimeFormat)
	r.duplicateDateTimeFormatField.SetChangedFunc(func(string) { r.renderDuplicatePreview() })
	r.duplicateForm.AddFormItem(r.duplicateDateTimeFormatField)

	r.duplicateCountField = tview.NewInputField().
		SetLabel("Number of duplicates").
		SetText(strconv.Itoa(r.settings.DuplicateCount)).
		SetAcceptanceFunc(tview.InputFieldInteger)
	r.duplicateCountField.SetChangedFunc(func(string) { r.renderDuplicatePreview() })
	r.duplicateForm.AddFormItem(r.duplicateCountField)

	// SetSize(1, 0) is not cosmetic: a TextView's own GetFieldHeight
	// returns 0 until SetSize gives it a real one, and Form.Draw
	// substitutes tview's own DefaultFormFieldHeight (5) for a 0 —
	// verified directly against tview's own form.go/textview.go, not
	// guessed, after a real, reproducible bug this exact gap caused:
	// a five-row-tall Preview item that Form's own box wasn't tall
	// enough to contain drew its extra rows straight past the box's own
	// bottom edge and into whatever sat below it (duplicateOptionsList),
	// erasing rows that had already drawn correctly, but only once
	// duplicateForm itself actually had focus — tview.Flex.Draw defers a
	// focused child's own Draw call to the very end specifically so a
	// focused item draws on top, which here meant "after, not before,
	// its already-correct sibling" once Preview's own rect started
	// bleeding past the form. Explicit height 1 exactly matches every
	// other field here, so this can never recur.
	r.duplicatePreviewView = tview.NewTextView().SetLabel("Preview").SetDynamicColors(false).SetSize(1, 0)
	r.duplicateForm.AddFormItem(r.duplicatePreviewView)

	r.duplicateStrategy = r.settings.DuplicateStrategy
	r.duplicateFlags = map[string]bool{
		duplicateLabelStrftime: r.settings.DuplicateDateTimeStrftime,
		duplicateLabelUseUnix:  r.settings.DuplicateDateTimeUseUnix,
	}
	r.renderDuplicateOptionsList()
	r.renderDuplicatePreview()
}

// duplicateTargetsLabel is the form's own "Target" line — the same
// shape sedTargetsLabel already has for Sed Replace, just worded for a
// set of files/directories rather than sed's own "files" (Duplicate
// works on a directory just as well as a plain file — it's an ordinary
// Copy underneath, see runDuplicate).
func duplicateTargetsLabel(targets []string) string {
	if len(targets) == 1 {
		return targets[0]
	}
	return fmt.Sprintf("%d selected items", len(targets))
}

// newDuplicateOptionsList builds duplicateOptionsList once, from
// NewRoot — a List rather than Form checkboxes/a dropdown, for the same
// reason sedFlagsList already is one (see newSedForm's own doc
// comment): a tview.Form can't give one item a background distinct
// from a real editable field's. Its "Strategy" row doesn't toggle, it
// cycles through three values in place (see cycleDuplicateStrategy) —
// the same "activating it is the change" shape cycleOptionChoice
// already gives the Options screen itself. Repopulated fresh on every
// open (see resetDuplicateForm) and after every cycle/toggle (see
// renderDuplicateOptionsList), the same as sedFlagsList's own items.
func (r *Root) newDuplicateOptionsList() *tview.List {
	l := tview.NewList().ShowSecondaryText(false)
	l.SetHighlightFullLine(true)
	l.SetDoneFunc(r.hideOverlay) // Escape
	return l
}

// renderDuplicateOptionsList (re)builds duplicateOptionsList's three
// rows from r.duplicateStrategy/r.duplicateFlags — cheap enough (three
// rows) to just clear and rebuild on every change rather than updating
// one row in place, the same choice resetSedForm's own sedFlagsList
// population already makes.
func (r *Root) renderDuplicateOptionsList() {
	r.duplicateOptionsList.Clear()
	r.duplicateOptionsList.AddItem(r.duplicateStrategyItemText(r.duplicateStrategy), "", 0, r.cycleDuplicateStrategy)
	for _, label := range []string{duplicateLabelStrftime, duplicateLabelUseUnix} {
		label := label // capture for the closure below
		r.duplicateOptionsList.AddItem(duplicateFlagItemText(label, r.duplicateFlags[label]), "", 0, func() { r.toggleDuplicateFlag(label) })
	}
}

// duplicateStrategyItemText renders the Strategy row's own label from
// value ("numbered"/"suffix_text"/"datetime") using the exact same
// choice labels the Options screen shows for "duplicate_strategy" (see
// optionSpecByKey) — one shared source for what each value is called,
// rather than a second copy of the same three labels living here.
func (r *Root) duplicateStrategyItemText(value string) string {
	if opt, ok := optionSpecByKey("duplicate_strategy"); ok {
		for _, c := range opt.choices(r) {
			if c.value == value {
				return "Strategy: " + c.label
			}
		}
	}
	return "Strategy: " + value
}

// duplicateFlagItemText renders one duplicateOptionsList toggle row —
// the same outline/filled circle this app already uses for a boolean
// everywhere else (see checkboxText), matching sedFlagItemText's own
// shape for Sed Replace's own toggle list.
func duplicateFlagItemText(label string, checked bool) string {
	return fmt.Sprintf("%s  %s", checkboxText(checked), label)
}

// cycleDuplicateStrategy advances r.duplicateStrategy — the dialog's
// own in-progress choice, not yet r.settings.DuplicateStrategy — to its
// next value, wrapping around. Reuses "duplicate_strategy"'s own
// optionSpec.choices (see optionSpecByKey) rather than a second,
// independently-maintained list of the same three values, the same
// "can't drift apart" reasoning cycleOptionChoice's own doc comment
// gives for the Options screen itself.
func (r *Root) cycleDuplicateStrategy() {
	opt, ok := optionSpecByKey("duplicate_strategy")
	if !ok {
		return
	}
	choices := opt.choices(r)
	if len(choices) == 0 {
		return
	}
	next := 0
	for i, c := range choices {
		if c.value == r.duplicateStrategy {
			next = (i + 1) % len(choices)
			break
		}
	}
	r.duplicateStrategy = choices[next].value
	r.renderDuplicateOptionsList()
	r.renderDuplicatePreview()
}

// toggleDuplicateFlag flips one of duplicateFlags' two entries and
// re-renders the row plus the live preview — the same "selectedFunc
// flips state, then relabels" shape toggleSedFlag already uses.
func (r *Root) toggleDuplicateFlag(label string) {
	r.duplicateFlags[label] = !r.duplicateFlags[label]
	r.renderDuplicateOptionsList()
	r.renderDuplicatePreview()
}

// newDuplicateActions builds duplicateForm's own action row once, from
// NewRoot — the same "a List rather than Form.AddButton, purely for
// consistency" choice newSedActions already makes.
func (r *Root) newDuplicateActions() *tview.List {
	l := tview.NewList().ShowSecondaryText(false)
	l.SetHighlightFullLine(true)
	l.AddItem("Duplicate", "", 0, r.runDuplicate)
	l.AddItem("Cancel", "", 0, r.hideOverlay)
	l.SetDoneFunc(r.hideOverlay) // Escape
	return l
}

// newDuplicateLayout wraps duplicateTitleBar (" Multiply ") above
// duplicateContentLayout — the same widget/layout split
// menu/menuTitleBar/menuLayout already established, mirrored exactly
// by sedLayout/sedTitleBar/sedContentLayout for Sed Replace.
func (r *Root) newDuplicateLayout() *tview.Flex {
	r.duplicateTitleBar = newPlainTitleBar("Multiply")
	r.duplicateContentLayout = r.newDuplicateContentLayout()
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.duplicateTitleBar, 1, 0, false).
		AddItem(r.duplicateContentLayout, 0, 1, true)
}

// newDuplicateContentLayout stacks duplicateForm (Target/Separator/
// Suffix text/Number padding/Date-time format/Number of duplicates/
// Preview), duplicateOptionsList (Strategy/the two toggles), and
// duplicateActions (Duplicate/Cancel) — the same three-widget stack
// newSedContentLayout already establishes for Sed Replace. Initial
// focus goes to duplicateForm, for the same "typing works immediately"
// reasoning newSedContentLayout's own doc comment gives.
func (r *Root) newDuplicateContentLayout() *tview.Flex {
	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	// 16 rows for duplicateForm's own 7 items (Target's own fieldHeight
	// 2, six single-row fields/Preview after it), matching tview.Form's
	// own vertical layout exactly (verified directly against its own
	// form.go, not guessed — see Form.Draw's per-item y += itemHeight +
	// itemPadding advance, plus 1 row of border padding top and bottom):
	// a shorter value silently drops Preview, the very last item, off
	// the bottom the same way openSedReplace's own doc comment already
	// warns about for its dialog.
	layout.AddItem(r.duplicateForm, 16, 0, true)
	layout.AddItem(r.duplicateOptionsList, 3, 0, false)
	layout.AddItem(r.duplicateActions, 2, 0, false)
	return layout
}

// currentDuplicateOptions builds fsops.DuplicateOptions from the
// dialog's own current field values — read directly from the widgets
// and from r.duplicateStrategy/r.duplicateFlags rather than
// r.settings, since the value actually used for this one computation
// (a live preview, or a real run) is whatever's currently typed or
// selected, before applyDuplicateSelection (if it ever even runs —
// Cancel never touches r.settings at all) has a chance to make it the
// new default.
func (r *Root) currentDuplicateOptions(padding int) fsops.DuplicateOptions {
	strategy := fsops.DuplicateNumbered
	switch r.duplicateStrategy {
	case "suffix_text":
		strategy = fsops.DuplicateSuffixText
	case "datetime":
		strategy = fsops.DuplicateDateTime
	}
	return fsops.DuplicateOptions{
		Separator:        r.duplicateSeparatorField.GetText(),
		Strategy:         strategy,
		SuffixText:       r.duplicateSuffixTextField.GetText(),
		NumberPadding:    padding,
		DateTimeFormat:   r.duplicateDateTimeFormatField.GetText(),
		DateTimeStrftime: r.duplicateFlags[duplicateLabelStrftime],
		DateTimeUseUnix:  r.duplicateFlags[duplicateLabelUseUnix],
	}
}

// duplicateNumberPadding parses duplicateNumberPaddingField's own text
// — a real, on-disk-checked negative value can't occur (the field's own
// SetAcceptanceFunc(tview.InputFieldInteger) never lets a "-" through
// at all), so the only failure this ever actually sees is an empty
// field, treated the same as "0" (no padding) rather than an error the
// user would have to clear before the live preview updates at all.
func (r *Root) duplicateNumberPadding() int {
	padding, err := strconv.Atoi(strings.TrimSpace(r.duplicateNumberPaddingField.GetText()))
	if err != nil || padding < 0 {
		return 0
	}
	return padding
}

// renderDuplicatePreview updates duplicatePreviewView with the name
// the *first* target's own duplicate would actually get right now,
// given every field's current value — recomputed on every keystroke
// and every Strategy-row/flag activation, so a typo or an unexpected
// format string shows up immediately rather than only once "Duplicate"
// is actually pressed.
//
// Only the first target this was opened for, and only the first of
// possibly several requested duplicates: fsops.ComputeDuplicateName
// really checks the filesystem, so this one live preview is never a
// guess, but simulating every remaining target/count pair would need
// to pretend earlier ones already exist on disk when they don't yet —
// the Numbered strategy's own "next" candidate genuinely depends on
// the previous one having actually been created (see its own doc
// comment), not just previewed. Everything past the first is
// summarized by count instead (see the "+N more"/"M in total" text
// below), which is honest about what it's actually showing.
func (r *Root) renderDuplicatePreview() {
	if r.duplicatePreviewView == nil || len(r.duplicateTargets) == 0 {
		return
	}
	opts := r.currentDuplicateOptions(r.duplicateNumberPadding())

	name, err := fsops.ComputeDuplicateName(r.duplicateTargets[0], opts)
	if err != nil {
		r.duplicatePreviewView.SetText("(" + err.Error() + ")")
		return
	}

	text := filepath.Base(name)
	if len(r.duplicateTargets) > 1 {
		text += fmt.Sprintf(" (+%d more targets)", len(r.duplicateTargets)-1)
	}
	if count, err := strconv.Atoi(strings.TrimSpace(r.duplicateCountField.GetText())); err == nil && count > 1 {
		text += fmt.Sprintf(", %d in total per target", count)
	}
	r.duplicatePreviewView.SetText(text)
}

// applyDuplicateSelection writes the dialog's own current field values
// back through the exact same optionSpec.apply functions the Options
// screen itself calls for every "duplicate_*" key (found by key lookup,
// see optionSpecByKey) — the one deliberate exception to how every
// other setting in this app works: whatever gets chosen here becomes
// the new sticky default for next time, per the user's own explicit
// request. Reusing these exact functions, rather than writing to
// r.settings and persisting directly a second time, is what keeps this
// path and the Options screen's own from ever drifting apart (see
// optionSpecByKey's own doc comment).
//
// "duplicate_count_max" is deliberately absent — that one is a safety
// bound the dialog itself never offers a field for (see runDuplicate's
// own check against it), not something this run's own count becomes.
func (r *Root) applyDuplicateSelection(padding, count int) {
	apply := func(key, value string) {
		if opt, ok := optionSpecByKey(key); ok {
			opt.apply(r, value)
		}
	}
	apply("duplicate_separator", r.duplicateSeparatorField.GetText())
	apply("duplicate_strategy", r.duplicateStrategy)
	apply("duplicate_suffix_text", r.duplicateSuffixTextField.GetText())
	apply("duplicate_number_padding", strconv.Itoa(padding))
	apply("duplicate_datetime_format", r.duplicateDateTimeFormatField.GetText())
	apply("duplicate_datetime_strftime", strconv.FormatBool(r.duplicateFlags[duplicateLabelStrftime]))
	apply("duplicate_datetime_use_unix", strconv.FormatBool(r.duplicateFlags[duplicateLabelUseUnix]))
	apply("duplicate_count", strconv.Itoa(count))
}

// runDuplicate is duplicateActions' own "Duplicate": validates the
// count against "duplicate_count_max" (the dialog's own one guard rail
// — see its own optionSpec help text), writes every field back as the
// new sticky default (see applyDuplicateSelection), then actually
// creates the duplicate(s).
//
// Runs sequentially — one real Copy call per (target × requested
// count), computing each destination immediately before that specific
// copy runs — rather than through the existing async pasteJob machinery
// (see pasteconflict.go): the Numbered strategy's own scan (see
// fsops.ComputeDuplicateName) has to see each previously-created
// duplicate already on disk to keep counting up correctly, which
// pasteJob's own up-front, all-at-once destination computation can't
// give it — precomputing "_1" for every target before any of them
// exist would collide on that very first candidate for every one of
// them. Duplicate's own "conflicts" are also already resolved by
// construction (a fresh, not-yet-existing name computed right before
// its own copy runs), so the six-option paste conflict dialog has
// nothing to add here anyway. Runs in the background (see safeGo) so a
// large file or a big count never blocks the UI thread, the same
// reasoning every other background job in this app already follows.
func (r *Root) runDuplicate() {
	count, err := strconv.Atoi(strings.TrimSpace(r.duplicateCountField.GetText()))
	if err != nil || count < 1 {
		r.showError(fmt.Errorf("duplicate: \"Number of duplicates\" must be a positive whole number"))
		return
	}
	if r.settings.DuplicateCountMax > 0 && count > r.settings.DuplicateCountMax {
		r.showError(fmt.Errorf("duplicate: %d duplicates requested, but \"Maximum number of duplicates\" in Options currently caps this at %d", count, r.settings.DuplicateCountMax))
		return
	}
	padding := r.duplicateNumberPadding()

	// Snapshotted here, on the calling (UI) goroutine, before the
	// background one starts — the same reasoning runSedPreview's own doc
	// comment gives for reading sedPreviewFunc once up front rather than
	// letting the goroutine read live application state as it goes,
	// which could otherwise race against whatever the user does next
	// once this dialog has already closed.
	opts := r.currentDuplicateOptions(padding)
	targets := append([]string(nil), r.duplicateTargets...)
	copyOpts := fsops.CopyOptions{
		FollowSymlinks: r.settings.CopyFollowSymlinks,
		SkipAttributes: !r.settings.CopyPreserveAttributes,
		StableSymlinks: r.settings.CopyStableSymlinks,
	}

	r.applyDuplicateSelection(padding, count)
	r.hideOverlay()

	r.safeGo("duplicate", nil, func() {
		created, err := performDuplicate(targets, opts, copyOpts, count)
		r.app.QueueUpdateDraw(func() {
			r.reloadPanel(nil)
			if err != nil {
				r.showError(fmt.Errorf("duplicate: created %d, then: %w", created, err))
			}
		})
	})
}

// performDuplicate runs Duplicate's own sequential copy loop for real,
// computing each destination immediately before its own copy runs (see
// runDuplicate's own doc comment for why this can't be precomputed in
// bulk the way pasteJob's own destinations are) and stopping at the
// first failure, whichever target/repetition it happens on.
//
// Split out from runDuplicate so the core logic is testable directly
// and synchronously, without needing to synchronize with runDuplicate's
// own background goroutine or its closing QueueUpdateDraw hand-off —
// the same reasoning buildSedScript's own doc comment gives for Sed
// Replace's guided-vs-advanced script logic.
func performDuplicate(targets []string, opts fsops.DuplicateOptions, copyOpts fsops.CopyOptions, count int) (created int, err error) {
	for _, src := range targets {
		for i := 0; i < count; i++ {
			dst, err := fsops.ComputeDuplicateName(src, opts)
			if err != nil {
				return created, err
			}
			if err := fsCopy(src, dst, copyOpts); err != nil {
				return created, err
			}
			created++
		}
	}
	return created, nil
}
