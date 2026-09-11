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

// Flag labels for the Date/time strategy's own two checkboxes —
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

	// height fits duplicateTitleBar's own row (1) plus
	// duplicateContentLayout's three stacked widgets (duplicateForm's
	// own worst case — the Date/time strategy's seven items, see
	// renderDuplicateForm's own doc comment for the exact height this
	// derives from — duplicatePreviewView's single row, and
	// duplicateButtons' single row) — checked against a real render, not
	// guessed; a shorter value silently clipped the bottom rows, the
	// same lesson openSedReplace's own doc comment records.
	width, height := 78, 20
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

// newDuplicateForm builds the (initially empty) "Multiply" form — called
// once from NewRoot; renderDuplicateForm populates it fresh on every
// open and on every strategy change, the same reasoning newSedForm's
// own doc comment gives for Sed Replace: tview.Form doesn't lend itself
// to being reset in place.
//
// No border, matching every other floating widget in this app (see
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

// resetDuplicateForm seeds every duplicateXxxValue mirror (see their
// own doc comment on root.go) from the current Duplicate defaults —
// per the user's own design intent that the dialog always show "what
// would happen right now", not a blank form to fill in from scratch —
// then builds the Form for the first time this open (see
// renderDuplicateForm).
func (r *Root) resetDuplicateForm() {
	r.duplicateSeparatorValue = r.settings.DuplicateSeparator
	r.duplicateStrategy = r.settings.DuplicateStrategy
	r.duplicateSuffixTextValue = r.settings.DuplicateSuffixText
	r.duplicateNumberPaddingValue = strconv.Itoa(r.settings.DuplicateNumberPadding)
	r.duplicateDateTimeFormatValue = r.settings.DuplicateDateTimeFormat
	r.duplicateCountValue = strconv.Itoa(r.settings.DuplicateCount)
	r.duplicateFlags = map[string]bool{
		duplicateLabelStrftime: r.settings.DuplicateDateTimeStrftime,
		duplicateLabelUseUnix:  r.settings.DuplicateDateTimeUseUnix,
	}
	r.renderDuplicateForm()
	r.renderDuplicatePreview()
}

// renderDuplicateForm (re)builds duplicateForm's own items from
// scratch: Target, the Strategy dropdown, Separator (used by every
// strategy, so always shown), then — and only then — whichever fields
// the CURRENTLY selected strategy actually uses, and finally Number of
// duplicates. Never all three strategies' own fields at once: showing
// "Suffix text" while "Numbered" is selected, say, would read as if
// every method combines, when only one ever actually applies — the
// exact confusion a guided, strategy-driven form exists to rule out.
//
// Called on every open (see resetDuplicateForm) and every time the
// Strategy dropdown itself changes — Form has no in-place way to
// show/hide one of its own items, so a full Clear(true)-and-rebuild is
// the only option, the same reasoning newSedForm's own doc comment
// gives for why Sed Replace's own form is rebuilt on every open too,
// just triggered more often here.
//
// Every field this builds is seeded from, and writes straight back
// into, its own plain-string mirror (duplicateSeparatorValue and
// friends — see their own doc comment on root.go) rather than reading
// back a widget that may not exist a moment from now: switching
// strategy destroys and recreates every one of these widgets, but the
// values they held must survive that unchanged.
//
// Field-count worst case (Date/time, the strategy with the most of its
// own fields): Target (its own AddTextView height 2) + Strategy
// dropdown (1) + Separator (1) + Date/time format (1) + two checkboxes
// (1 each) + Number of duplicates (1) — seven items, heights summing
// to 8, needing an inner height of 8 + 6*itemPadding(1) = 14, plus 1
// row of border padding top and bottom — 16 in total (verified
// directly against tview's own Form.Draw, the same derivation
// newDuplicateContentLayout's own doc comment spells out in full).
func (r *Root) renderDuplicateForm() {
	r.duplicateForm.Clear(true)
	r.duplicateSuffixTextField = nil
	r.duplicateNumberPaddingField = nil
	r.duplicateDateTimeFormatField = nil

	r.duplicateForm.AddTextView("Target", duplicateTargetsLabel(r.duplicateTargets), 0, 2, true, false)

	opt, ok := optionSpecByKey("duplicate_strategy")
	var choices []optionChoice
	if ok {
		choices = opt.choices(r)
	}
	labels := make([]string, len(choices))
	currentIndex := 0
	for i, c := range choices {
		labels[i] = c.label
		if c.value == r.duplicateStrategy {
			currentIndex = i
		}
	}
	r.duplicateStrategyField = tview.NewDropDown().SetLabel("Strategy").SetOptions(labels, func(_ string, index int) {
		if index < 0 || index >= len(choices) {
			return
		}
		// AddDropDown/SetCurrentOption fires this same callback
		// synchronously as part of *setting the initial option* below —
		// verified directly against tview's own dropdown.go, not
		// assumed — with index equal to currentIndex, i.e. no real
		// change at all. Without this guard, that first, synthetic call
		// would rebuild the form again, which recreates this same
		// dropdown again, which fires the callback again — an infinite
		// recursion, not just a wasted rebuild.
		if choices[index].value == r.duplicateStrategy {
			return
		}
		r.duplicateStrategy = choices[index].value
		r.renderDuplicateForm()
		r.renderDuplicatePreview()
	})
	r.duplicateStrategyField.SetCurrentOption(currentIndex)
	// A real, reproducible bug this pins down: Form's own generic
	// per-item theming (SetFieldBackgroundColor/SetFieldTextColor in
	// applyTheme, applied every Draw via SetFormAttributes) only ever
	// reaches DropDown.SetFieldStyle — DropDown keeps a *separate*
	// focusedStyle for "has real focus and is closed" that only
	// DropDown.SetFocusedStyle itself sets (verified directly against
	// tview's own dropdown.go, not assumed, after Strategy rendered in
	// tview's own stock blue-on-white instead of this app's palette
	// while focused — SetFormAttributes calling SetFieldStyle alone
	// never touches it at all). Since this is the very first item this
	// form ever opens with real focus on, focusedStyle is the one that
	// actually paints on screen far more than fieldStyle does — both
	// need setting explicitly, here, right after construction, for this
	// to match every other field regardless of Form's own generic pass.
	fieldStyle := tcell.StyleDefault.Background(r.theme.FocusedBackground).Foreground(r.theme.Text)
	r.duplicateStrategyField.SetFieldStyle(fieldStyle)
	r.duplicateStrategyField.SetFocusedStyle(fieldStyle)
	r.duplicateForm.AddFormItem(r.duplicateStrategyField)

	r.duplicateSeparatorField = tview.NewInputField().SetLabel("Separator").SetText(r.duplicateSeparatorValue)
	r.duplicateSeparatorField.SetChangedFunc(func(v string) {
		r.duplicateSeparatorValue = v
		r.renderDuplicatePreview()
	})
	r.duplicateForm.AddFormItem(r.duplicateSeparatorField)

	switch r.duplicateStrategy {
	case "suffix_text":
		r.duplicateSuffixTextField = tview.NewInputField().SetLabel("Suffix text").SetText(r.duplicateSuffixTextValue)
		r.duplicateSuffixTextField.SetChangedFunc(func(v string) {
			r.duplicateSuffixTextValue = v
			r.renderDuplicatePreview()
		})
		r.duplicateForm.AddFormItem(r.duplicateSuffixTextField)
	case "datetime":
		r.duplicateDateTimeFormatField = tview.NewInputField().SetLabel("Date/time format").SetText(r.duplicateDateTimeFormatValue)
		r.duplicateDateTimeFormatField.SetChangedFunc(func(v string) {
			r.duplicateDateTimeFormatValue = v
			r.renderDuplicatePreview()
		})
		r.duplicateForm.AddFormItem(r.duplicateDateTimeFormatField)
		r.duplicateForm.AddCheckbox(duplicateLabelStrftime, r.duplicateFlags[duplicateLabelStrftime], func(checked bool) {
			r.duplicateFlags[duplicateLabelStrftime] = checked
			r.renderDuplicatePreview()
		})
		r.duplicateForm.AddCheckbox(duplicateLabelUseUnix, r.duplicateFlags[duplicateLabelUseUnix], func(checked bool) {
			r.duplicateFlags[duplicateLabelUseUnix] = checked
			r.renderDuplicatePreview()
		})
	default: // "numbered", and any unrecognized value (see cycleOptionChoice's own equivalent fallback)
		r.duplicateNumberPaddingField = tview.NewInputField().
			SetLabel("Number padding (digits)").
			SetText(r.duplicateNumberPaddingValue).
			SetAcceptanceFunc(tview.InputFieldInteger)
		r.duplicateNumberPaddingField.SetChangedFunc(func(v string) {
			r.duplicateNumberPaddingValue = v
			r.renderDuplicatePreview()
		})
		r.duplicateForm.AddFormItem(r.duplicateNumberPaddingField)
	}

	r.duplicateCountField = tview.NewInputField().
		SetLabel("Number of duplicates").
		SetText(r.duplicateCountValue).
		SetAcceptanceFunc(tview.InputFieldInteger)
	r.duplicateCountField.SetChangedFunc(func(v string) {
		r.duplicateCountValue = v
		r.renderDuplicatePreview()
	})
	r.duplicateForm.AddFormItem(r.duplicateCountField)
}

// duplicateTargetsLabel is the form's own "Target" line — the same
// shape sedTargetsLabel already has for Sed Replace, just worded for a
// set of files/directories rather than sed's own "files" (Duplicate
// works on a directory just as well as a file — it's an ordinary Copy
// underneath, see runDuplicate).
func duplicateTargetsLabel(targets []string) string {
	if len(targets) == 1 {
		return targets[0]
	}
	return fmt.Sprintf("%d selected items", len(targets))
}

// newDuplicatePreviewView builds duplicatePreviewView once, from
// NewRoot — a plain, read-only TextView sibling of duplicateForm, not
// one of its items (see its own doc comment on root.go for why: a
// TextView added as a Form item needs an explicit height or tview
// substitutes a 5-row default, a real bug this shape rules out
// entirely rather than just remembering to avoid). Its own text is set
// fresh by renderDuplicatePreview, not here — there is nothing
// meaningful to show before a real target exists.
func (r *Root) newDuplicatePreviewView() *tview.TextView {
	return tview.NewTextView()
}

// newDuplicateButtons builds duplicateForm's own action row once, from
// NewRoot — a real Cancel/Duplicate button pair, bottom-left/
// bottom-right, the same shape newChmodButtons/newSearchButtons/
// newPropertiesButtons already establish for their own dialogs,
// per the user's own explicit request that this dialog's buttons match
// that established look rather than Sed Replace's own vertical
// two-item List.
func (r *Root) newDuplicateButtons() *tview.Flex {
	r.duplicateCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.hideOverlay)
	r.duplicateApplyBtn = tview.NewButton("Duplicate").SetSelectedFunc(r.runDuplicate)
	r.duplicateCancelBtn.SetInputCapture(spaceAlsoActivates(r.hideOverlay))
	r.duplicateApplyBtn.SetInputCapture(spaceAlsoActivates(r.runDuplicate))

	exitFunc := func(key tcell.Key) {
		if key == tcell.KeyEscape {
			r.hideOverlay()
		}
	}
	r.duplicateCancelBtn.SetExitFunc(exitFunc)
	r.duplicateApplyBtn.SetExitFunc(exitFunc)

	// Equal proportion (0, 1) each, nothing else, so the two together
	// fill the whole row edge to edge, split exactly in half — the same
	// shape newChmodButtons/newSearchButtons/newPropertiesButtons all
	// already use.
	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.duplicateCancelBtn, 0, 1, false).
		AddItem(r.duplicateApplyBtn, 0, 1, false)
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

// newDuplicateContentLayout stacks duplicateForm, duplicatePreviewView
// (its own always-visible row, never a Form item — see its own doc
// comment on root.go for the real bug that shape rules out), and
// duplicateButtons — the same three-widget stack newSedContentLayout
// already establishes for Sed Replace, just with a plain preview row
// and a button pair instead of a flags list and an actions list.
//
// 16 rows for duplicateForm: its own worst case (the Date/time
// strategy's seven items — Target height 2, five height-1 items) sums
// to 8 rows of content, plus itemPadding (1 row between every pair of
// items, i.e. 6 for seven items) plus 2 rows of the Form's own
// top/bottom border padding — 8 + 6 + 2 = 16 (verified directly against
// tview's own Form.Draw, not guessed — a shorter value silently drops
// the last item off the bottom the same way openSedReplace's own doc
// comment already warns about for its own dialog).
func (r *Root) newDuplicateContentLayout() *tview.Flex {
	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	layout.AddItem(r.duplicateForm, 16, 0, true)
	layout.AddItem(r.duplicatePreviewView, 1, 0, false)
	layout.AddItem(r.duplicateButtons, 1, 0, false)
	return layout
}

// currentDuplicateOptions builds fsops.DuplicateOptions from the
// dialog's own current mirror values (see their own doc comment on
// root.go) — never from the widgets directly, since a strategy switch
// destroys and recreates most of them; the mirrors are what survives
// that. The value actually used for this one computation (a live
// preview, or a real run) is whatever's currently typed or selected,
// before applyDuplicateSelection (if it ever even runs — Cancel never
// touches r.settings at all) has a chance to make it the new default.
func (r *Root) currentDuplicateOptions(padding int) fsops.DuplicateOptions {
	strategy := fsops.DuplicateNumbered
	switch r.duplicateStrategy {
	case "suffix_text":
		strategy = fsops.DuplicateSuffixText
	case "datetime":
		strategy = fsops.DuplicateDateTime
	}
	return fsops.DuplicateOptions{
		Separator:        r.duplicateSeparatorValue,
		Strategy:         strategy,
		SuffixText:       r.duplicateSuffixTextValue,
		NumberPadding:    padding,
		DateTimeFormat:   r.duplicateDateTimeFormatValue,
		DateTimeStrftime: r.duplicateFlags[duplicateLabelStrftime],
		DateTimeUseUnix:  r.duplicateFlags[duplicateLabelUseUnix],
	}
}

// duplicateNumberPadding parses duplicateNumberPaddingValue — a real,
// negative value can't occur (the field's own
// SetAcceptanceFunc(tview.InputFieldInteger) never lets a "-" through
// at all while it exists, and it's simply absent while some other
// strategy is selected), so the only failure this ever actually sees
// is an empty value, treated the same as "0" (no padding) rather than
// an error the user would have to clear before the live preview
// updates at all.
func (r *Root) duplicateNumberPadding() int {
	padding, err := strconv.Atoi(strings.TrimSpace(r.duplicateNumberPaddingValue))
	if err != nil || padding < 0 {
		return 0
	}
	return padding
}

// renderDuplicatePreview updates duplicatePreviewView with the name
// the *first* target's own duplicate would actually get right now,
// given every current mirror value — recomputed on every keystroke,
// every checkbox toggle, and every Strategy change, so a typo or an
// unexpected format string shows up immediately rather than only once
// "Duplicate" is actually pressed.
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
		r.duplicatePreviewView.SetText("Preview: (" + err.Error() + ")")
		return
	}

	text := "Preview: " + filepath.Base(name)
	if len(r.duplicateTargets) > 1 {
		text += fmt.Sprintf(" (+%d more targets)", len(r.duplicateTargets)-1)
	}
	if count, err := strconv.Atoi(strings.TrimSpace(r.duplicateCountValue)); err == nil && count > 1 {
		text += fmt.Sprintf(", %d in total per target", count)
	}
	r.duplicatePreviewView.SetText(text)
}

// applyDuplicateSelection writes the dialog's own current mirror
// values back through the exact same optionSpec.apply functions the
// Options screen itself calls for every "duplicate_*" key (found by
// key lookup, see optionSpecByKey) — the one deliberate exception to
// how every other setting in this app works: whatever gets chosen here
// becomes the new sticky default for next time, per the user's own
// explicit request. Reusing these exact functions, rather than writing
// to r.settings and persisting directly a second time, is what keeps
// this path and the Options screen's own from ever drifting apart (see
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
	apply("duplicate_separator", r.duplicateSeparatorValue)
	apply("duplicate_strategy", r.duplicateStrategy)
	apply("duplicate_suffix_text", r.duplicateSuffixTextValue)
	apply("duplicate_number_padding", strconv.Itoa(padding))
	apply("duplicate_datetime_format", r.duplicateDateTimeFormatValue)
	apply("duplicate_datetime_strftime", strconv.FormatBool(r.duplicateFlags[duplicateLabelStrftime]))
	apply("duplicate_datetime_use_unix", strconv.FormatBool(r.duplicateFlags[duplicateLabelUseUnix]))
	apply("duplicate_count", strconv.Itoa(count))
}

// runDuplicate is duplicateButtons' own "Duplicate": validates the
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
	count, err := strconv.Atoi(strings.TrimSpace(r.duplicateCountValue))
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
