package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/replace"
)

// Checkbox labels, shared between resetSedForm (where they're added) and
// sedCheckbox (where their state is read back) — constants rather than
// repeated literals so a typo in one place can't silently desync them.
const (
	sedLabelRegex           = "Regex (Find is a pattern, not literal text)"
	sedLabelExtendedRegex   = "Extended regex (-E)"
	sedLabelCaseInsensitive = "Case-insensitive"
	sedLabelGlobal          = "Replace all matches per line"
	sedLabelBackup          = "Keep a .bak backup before overwriting"
)

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

	// height fits every field/checkbox/button this form actually has
	// (10 items plus their labels/spacing plus the button row) — checked
	// against a real render, not guessed; a shorter value silently
	// clipped the bottom rows, Preview/Cancel included.
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
	r.sedForm.SetRect(x, y, width, height)
	r.showOverlay(sedReplacePage, r.sedForm)
}

// newSedForm builds the (initially empty) "Sed Replace" dialog — called
// once from NewRoot; resetSedForm populates it fresh on every open (see
// openSedReplace), since tview.Form doesn't lend itself to being reset
// in place the way a List's own AddItem/SetItemText does.
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

// resetSedForm rebuilds sedForm's fields fresh for the current
// r.sedTargets. Clear(true) first — Form has no other way to remove
// everything a previous open's Preview/Cancel buttons and field values
// left behind.
//
// The advanced field overrides Find/Replace/Regex/Extended/Case-
// insensitive/Global entirely once it has anything in it (see
// runSedPreview) — deliberately not a separate "advanced mode" checkbox
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

	r.sedForm.AddCheckbox(sedLabelRegex, false, nil)
	r.sedForm.AddCheckbox(sedLabelExtendedRegex, false, nil)
	r.sedForm.AddCheckbox(sedLabelCaseInsensitive, false, nil)
	r.sedForm.AddCheckbox(sedLabelGlobal, true, nil)

	r.sedAdvancedField = tview.NewInputField().SetLabel("Advanced sed script (overrides Find/Replace above)")
	r.sedForm.AddFormItem(r.sedAdvancedField)

	r.sedForm.AddCheckbox(sedLabelBackup, false, nil)

	r.sedForm.AddButton("Preview", r.runSedPreview)
	r.sedForm.AddButton("Cancel", r.hideOverlay)

	r.styleSedCheckboxes()
}

// styleSedCheckboxes swaps every checkbox's default "X" glyph for the
// outline/filled circle (○/●) this app already uses for the panel's own
// checkbox column (see checkboxText in panel.go) — one visual language
// for "this is a boolean toggle" instead of two different ones.
func (r *Root) styleSedCheckboxes() {
	for _, label := range []string{sedLabelRegex, sedLabelExtendedRegex, sedLabelCaseInsensitive, sedLabelGlobal, sedLabelBackup} {
		if cb, ok := r.sedForm.GetFormItemByLabel(label).(*tview.Checkbox); ok {
			cb.SetCheckedString(checkboxText(true)).SetUncheckedString(checkboxText(false))
		}
	}
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

// sedCheckbox reads one of resetSedForm's own checkboxes back by label —
// simpler than keeping a separate *tview.Checkbox field per checkbox
// alongside the input fields that do need one (their typed values, not
// just a bool, are read directly).
func (r *Root) sedCheckbox(label string) bool {
	if item := r.sedForm.GetFormItemByLabel(label); item != nil {
		if cb, ok := item.(*tview.Checkbox); ok {
			return cb.IsChecked()
		}
	}
	return false
}

// runSedPreview is the form's own "Preview" button: builds the sed
// script (guided fields, or the advanced field verbatim if it has
// anything in it), runs it read-only against every target via
// replace.Preview, and shows the result — nothing is written to disk
// yet, see confirmApplySed for that.
func (r *Root) runSedPreview() {
	extendedRegex := r.sedCheckbox(sedLabelExtendedRegex)

	script := strings.TrimSpace(r.sedAdvancedField.GetText())
	if script == "" {
		built, err := replace.BuildScript(
			r.sedFindField.GetText(), r.sedReplaceField.GetText(),
			r.sedCheckbox(sedLabelRegex), extendedRegex,
			r.sedCheckbox(sedLabelCaseInsensitive), r.sedCheckbox(sedLabelGlobal),
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

// backToSedForm is Preview's own "Back": returns to sedForm with
// whatever the user already typed still in place (Clear/reset only
// happens on a fresh openSedReplace, not here) so a Find/Replace typo
// spotted in the preview can be fixed without starting over.
func (r *Root) backToSedForm() {
	r.showOverlay(sedReplacePage, r.sedForm)
}

// confirmApplySed is Preview's own "Apply": asks first (Cancel
// preselected — reuses openPurgeConfirm exactly as Remove/Empty Trash
// already do, see trash.go) before writing anything back, since Sed
// Replace overwrites originals in place — optionally keeping a .bak,
// see the form's own checkbox — just as permanently as those.
func (r *Root) confirmApplySed() {
	changes := r.sedPendingChanges
	if len(changes) == 0 {
		return
	}
	backup := r.sedCheckbox(sedLabelBackup)

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
