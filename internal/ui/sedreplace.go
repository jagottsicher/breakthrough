package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/replace"
)

// Flag labels, shared between resetSedForm (where sedFlagsList's items
// are built) and r.sedFlags (where their state is read back) — constants
// rather than repeated literals so a typo in one place can't silently
// desync them. sedFlagOrder is the fixed display order.
const (
	sedLabelRegex           = "Regex (Find is a pattern, not literal text)"
	sedLabelExtendedRegex   = "Extended regex (-E)"
	sedLabelCaseInsensitive = "Case-insensitive"
	sedLabelGlobal          = "Replace all matches per line"
	sedLabelBackup          = "Keep a .bak backup before overwriting"
)

var sedFlagOrder = []string{sedLabelRegex, sedLabelExtendedRegex, sedLabelCaseInsensitive, sedLabelGlobal, sedLabelBackup}

// openSedReplace is the context menu's "Sed Replace", and (through
// SedReplaceShortcut) Ctrl+S's action: opens a dialog to run a real
// sed(1) substitution against the current selection (or the current
// row) — see internal/replace's own package doc for why this shells out
// to real sed rather than reimplementing its regex/scripting engine,
// and why it never uses sed's own -i.
func (r *Root) openSedReplace() {
	targets := r.selectedOrCurrentPaths()
	if len(targets) == 0 {
		return
	}
	r.sedTargets = targets
	r.resetSedForm()

	// height fits sedLayout's three stacked widgets (sedForm's four
	// fields, sedFlagsList's five toggles, sedActions' two buttons, plus
	// spacing) — checked against a real render, not guessed; a shorter
	// value silently clipped the bottom rows.
	width, height := 78, 17
	_, _, screenWidth, screenHeight := r.GetRect() // Root fills the whole screen
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.sedLayout.SetRect(x, y, width, height)
	r.showOverlay(sedReplacePage, r.sedLayout)
}

// newSedForm builds the (initially empty) "Sed Replace" text-field form
// — called once from NewRoot; resetSedForm populates it fresh on every
// open (see openSedReplace), since tview.Form doesn't lend itself to
// being reset in place the way a List's own AddItem/SetItemText does.
//
// Deliberately holds only Target/Find/Replace/the advanced script —
// none of the flag toggles: tview.Form re-applies one uniform field
// background to every item it owns on every single Draw call (verified
// by reading form.go directly, not guessed), so a checkbox added via
// Form.AddCheckbox can never keep a background different from a real
// text field's — the checkbox always ends up with the same
// "you can type here" highlight (FocusedBackground, see applyTheme)
// real fields get, even though it isn't one. The five flags live in
// sedFlagsList instead (see newSedFlagsList), the same plain List-item-
// with-a-relabeling-glyph pattern this app's own context menu already
// uses for "Hide/Show hidden files" — a List has no such per-item
// styling fight, since it never forces one color onto every row.
//
// No border, matching every other floating widget in this app (see
// NewRoot's own comment on menu/quitConfirm/purgeConfirm) — a plain
// background color set apart from the panel already does the same job.
func (r *Root) newSedForm() *tview.Form {
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

// resetSedForm rebuilds sedForm's fields and sedFlagsList's toggles
// fresh for the current r.sedTargets. Clear(true) first — Form has no
// other way to remove everything a previous open's field values left
// behind.
//
// The advanced field overrides Find/Replace/Regex/Extended/Case-
// insensitive/Global entirely once it has anything in it (see
// runSedPreview) — deliberately not a separate "advanced mode" toggle
// requiring the form to dynamically show/hide fields (tview.Form has no
// clean way to do that without rebuilding it anyway, so there is nothing
// simpler on the other side of that complexity). Extended regex (-E)
// still applies to an advanced script too — it is a real sed invocation
// flag (see RunSed), not just a guided-mode escaping choice.
func (r *Root) resetSedForm() {
	r.sedForm.Clear(true)

	r.sedForm.AddTextView("Target", sedTargetsLabel(r.sedTargets), 0, 2, true, false)

	r.sedFindField = tview.NewInputField().SetLabel("Find")
	r.sedForm.AddFormItem(r.sedFindField)
	r.sedReplaceField = tview.NewInputField().SetLabel("Replace with")
	r.sedForm.AddFormItem(r.sedReplaceField)

	r.sedAdvancedField = tview.NewInputField().SetLabel("Advanced sed script (overrides Find/Replace above)")
	r.sedForm.AddFormItem(r.sedAdvancedField)

	r.sedFlags = map[string]bool{
		sedLabelRegex:           false,
		sedLabelExtendedRegex:   false,
		sedLabelCaseInsensitive: false,
		sedLabelGlobal:          true,
		sedLabelBackup:          false,
	}
	r.sedFlagsList.Clear()
	for _, label := range sedFlagOrder {
		label := label // capture for the closure below
		r.sedFlagsList.AddItem(sedFlagItemText(label, r.sedFlags[label]), "", 0, func() { r.toggleSedFlag(label) })
	}
}

// sedFlagItemText renders one sedFlagsList row: the same outline/filled
// circle (○/●) this app already uses for the panel's own checkbox
// column (see checkboxText in panel.go), so a boolean toggle looks the
// same everywhere in this app rather than switching styles depending on
// which widget happens to implement it.
func sedFlagItemText(label string, checked bool) string {
	return fmt.Sprintf("%s  %s", checkboxText(checked), label)
}

// toggleSedFlag flips one flag's state and re-renders just that row —
// the same "selectedFunc flips state, then relabels" shape
// Root.toggleHidden already uses for the context menu's own toggle.
func (r *Root) toggleSedFlag(label string) {
	r.sedFlags[label] = !r.sedFlags[label]
	for i, l := range sedFlagOrder {
		if l == label {
			r.sedFlagsList.SetItemText(i, sedFlagItemText(label, r.sedFlags[label]), "")
			return
		}
	}
}

// newSedFlagsList builds sedFlagsList once, from NewRoot — see
// newSedForm's own doc comment for why the five flag toggles live here
// instead of as Form checkboxes. Repopulated fresh on every open (see
// resetSedForm), the same as sedForm's own fields.
func (r *Root) newSedFlagsList() *tview.List {
	l := tview.NewList().ShowSecondaryText(false)
	l.SetHighlightFullLine(true)
	l.SetDoneFunc(r.hideOverlay) // Escape
	return l
}

// newSedActions builds sedForm's own action row once, from NewRoot — a
// List rather than Form.AddButton, purely for consistency with
// sedFlagsList/sedPreviewActions right above and below it (a Form's own
// buttons would have worked fine here since buttons aren't checkboxes
// and don't hit the per-item-background problem newSedForm's own doc
// comment describes — this is a style choice, not a workaround).
func (r *Root) newSedActions() *tview.List {
	l := tview.NewList().ShowSecondaryText(false)
	l.SetHighlightFullLine(true)
	l.AddItem("Preview", "", 0, r.runSedPreview)
	l.AddItem("Cancel", "", 0, r.hideOverlay)
	l.SetDoneFunc(r.hideOverlay) // Escape
	return l
}

// newSedLayout stacks sedForm (Target/Find/Replace/advanced script),
// sedFlagsList (the five toggles), and sedActions (Preview/Cancel) into
// the single widget sedReplacePage actually shows — see newSedForm's own
// doc comment for why the flags live in a separate List rather than as
// Form checkboxes. Initial focus goes to sedForm: typing Find/Replace
// immediately, without an extra click first, is the common case:
// reaching the flags or the buttons instead is one click away, the same
// as moving between any two of this app's other independent widgets
// (e.g. panel and bashLine) already is.
func (r *Root) newSedLayout() *tview.Flex {
	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	layout.AddItem(r.sedForm, 8, 0, true)
	layout.AddItem(r.sedFlagsList, 5, 0, false)
	layout.AddItem(r.sedActions, 2, 0, false)
	return layout
}

// sedTargetsLabel is the form's own "Target" line: the single file's
// path if there's only one, otherwise a plain count — the same shape
// removeConfirmMessage already uses for Remove's own confirmation (see
// trash.go), just without that one's recursive item count, which has no
// equivalent meaning for a set of plain files.
func sedTargetsLabel(targets []string) string {
	if len(targets) == 1 {
		return targets[0]
	}
	return fmt.Sprintf("%d selected files", len(targets))
}

// runSedPreview is sedActions' own "Preview": builds the sed script
// (guided fields, or the advanced field verbatim if it has anything in
// it), runs it read-only against every target via replace.Preview, and
// shows the result — nothing is written to disk yet, see confirmApplySed
// for that.
func (r *Root) runSedPreview() {
	extendedRegex := r.sedFlags[sedLabelExtendedRegex]

	script := strings.TrimSpace(r.sedAdvancedField.GetText())
	if script == "" {
		built, err := replace.BuildScript(
			r.sedFindField.GetText(), r.sedReplaceField.GetText(),
			r.sedFlags[sedLabelRegex], extendedRegex,
			r.sedFlags[sedLabelCaseInsensitive], r.sedFlags[sedLabelGlobal],
		)
		if err != nil {
			r.showError(err)
			return
		}
		script = built
	}

	changes, skipped, err := replace.Preview(r.sedTargets, script, extendedRegex)
	if err != nil {
		r.showError(err)
		return
	}
	r.sedPendingChanges = changes

	r.sedPreviewView.SetText(formatSedPreview(changes, skipped))
	r.sedPreviewView.ScrollToBeginning()
	r.showOverlay(sedPreviewPage, r.sedPreviewLayout)
}

// formatSedPreview renders Preview's own result as a scrollable summary:
// each changed file with a simple positional comparison of its before/
// after lines, then every skipped input with its reason.
//
// The line comparison is deliberately simple — index-for-index, not a
// real diff algorithm: a sed script that inserts or deletes whole lines
// shifts every following line out of alignment, which shows up here as
// every one of them looking "changed" even where only their position
// moved. Good enough to spot-check an ordinary substitution; not a
// substitute for understanding what an advanced script that adds/
// removes lines outright actually does.
func formatSedPreview(changes []replace.FileChange, skipped map[string]string) string {
	var b strings.Builder

	if len(changes) == 0 {
		b.WriteString("No files would change.\n\n")
	}
	for _, c := range changes {
		beforeLines := strings.Split(string(c.Before), "\n")
		afterLines := strings.Split(string(c.After), "\n")

		fmt.Fprintf(&b, "[::b]%s[::-]\n", tview.Escape(c.Path))

		n := len(beforeLines)
		if len(afterLines) < n {
			n = len(afterLines)
		}
		shown := 0
		for i := 0; i < n; i++ {
			if beforeLines[i] == afterLines[i] {
				continue
			}
			fmt.Fprintf(&b, "  %d: [red]- %s[-]\n      [green]+ %s[-]\n", i+1, tview.Escape(beforeLines[i]), tview.Escape(afterLines[i]))
			shown++
		}
		if len(beforeLines) != len(afterLines) {
			fmt.Fprintf(&b, "  (line count changed: %d -> %d)\n", len(beforeLines), len(afterLines))
		} else if shown == 0 {
			b.WriteString("  (changed, but no single line differs at the same position)\n")
		}
		b.WriteString("\n")
	}

	if len(skipped) > 0 {
		b.WriteString("Skipped:\n")
		for path, reason := range skipped {
			fmt.Fprintf(&b, "  %s (%s)\n", tview.Escape(path), tview.Escape(reason))
		}
	}
	return b.String()
}

// newSedPreviewLayout builds Preview's own result screen once, from
// NewRoot: a scrollable read-only summary (see formatSedPreview/
// runSedPreview) with Apply/Back/Cancel below it — the same three-choice
// shape purgeConfirm already has, just paired with a TextView instead of
// being one on its own, since the summary itself can run to many lines.
func (r *Root) newSedPreviewLayout() *tview.Flex {
	r.sedPreviewView = tview.NewTextView()
	r.sedPreviewView.SetDynamicColors(true)

	r.sedPreviewActions = tview.NewList().ShowSecondaryText(false)
	r.sedPreviewActions.SetHighlightFullLine(true)
	r.sedPreviewActions.AddItem("Apply", "", 0, r.confirmApplySed)
	r.sedPreviewActions.AddItem("Back", "", 0, r.backToSedForm)
	r.sedPreviewActions.AddItem("Cancel", "", 0, r.hideOverlay)
	r.sedPreviewActions.SetDoneFunc(r.hideOverlay) // Escape

	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	layout.AddItem(r.sedPreviewView, 0, 1, false)
	layout.AddItem(r.sedPreviewActions, 3, 0, true)
	return layout
}

// backToSedForm is Preview's own "Back": returns to sedLayout with
// whatever the user already typed/toggled still in place (Clear/reset
// only happens on a fresh openSedReplace, not here) so a Find/Replace
// typo spotted in the preview can be fixed without starting over.
func (r *Root) backToSedForm() {
	r.showOverlay(sedReplacePage, r.sedLayout)
}

// confirmApplySed is Preview's own "Apply": asks first (Cancel
// preselected — reuses openPurgeConfirm exactly as Remove/Empty Trash
// already do, see trash.go) before writing anything back, since Sed
// Replace overwrites originals in place — optionally keeping a .bak,
// see sedFlagsList's own toggle — just as permanently as those.
func (r *Root) confirmApplySed() {
	changes := r.sedPendingChanges
	if len(changes) == 0 {
		return
	}
	backup := r.sedFlags[sedLabelBackup]

	note := " Originals will be kept as .bak files."
	if !backup {
		note = " No backup will be kept."
	}
	message := fmt.Sprintf("Apply sed changes to %d file(s)?%s", len(changes), note)

	r.openPurgeConfirm(message, func() {
		applied, err := replace.Apply(changes, backup)
		r.sedPendingChanges = nil
		if err != nil {
			r.showError(fmt.Errorf("sed replace: %d of %d files updated, then: %w", applied, len(changes), err))
			return
		}
		r.reloadPanel(nil)
	})
}

// SedReplaceShortcut is Ctrl+S's global action (see cmd/breakthrough and
// acceptsGlobalShortcut) — the keyboard/button-bar equivalent of the
// context menu's "Sed Replace".
func (r *Root) SedReplaceShortcut() {
	if r.acceptsGlobalShortcut() {
		r.openSedReplace()
	}
}
