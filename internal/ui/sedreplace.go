package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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

// openSedReplace is the context menu's "sed", and (through
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

// buildSedScript resolves what script/extendedRegex runSedPreview
// should actually run: the advanced field verbatim if it has anything
// in it (see resetSedForm's own doc comment on why Extended regex still
// applies even then), otherwise built from the guided Find/Replace
// fields and flags (see replace.BuildScript).
//
// Split out from runSedPreview so it's testable synchronously, without
// needing to synchronize with runSedPreview's own background goroutine
// (see sedPreviewFunc's own doc comment on why that part isn't tested
// the same direct way).
func (r *Root) buildSedScript() (script string, extendedRegex bool, err error) {
	extendedRegex = r.sedFlags[sedLabelExtendedRegex]

	script = strings.TrimSpace(r.sedAdvancedField.GetText())
	if script != "" {
		return script, extendedRegex, nil
	}

	script, err = replace.BuildScript(
		r.sedFindField.GetText(), r.sedReplaceField.GetText(),
		r.sedFlags[sedLabelRegex], extendedRegex,
		r.sedFlags[sedLabelCaseInsensitive], r.sedFlags[sedLabelGlobal],
	)
	return script, extendedRegex, err
}

// sedPreviewFunc is replace.Preview, a package-level var so tests can
// override it — the same reasoning search.go's own searchRun var has:
// runSedPreview's real path runs this in a goroutine and reports back
// through Application.QueueUpdateDraw, which nothing drains in a test
// that never calls Application.Run — see showSedPreviewResult, split
// out separately so the result-handling behavior is still directly
// testable without needing that real event loop.
var sedPreviewFunc = replace.Preview

// runSedPreview is sedActions' own "Preview": builds the sed script
// (guided fields, or the advanced field verbatim if it has anything in
// it) and runs it read-only, in the background, against every target —
// nothing is written to disk yet, see confirmApplySed for that. Shows
// the preview screen immediately with an animated "processing" status
// (see animateSedPreviewProgress) rather than blocking the UI until a
// (possibly large) file has been read and sedded, the same reasoning
// runSearch's own progress animation exists for.
func (r *Root) runSedPreview() {
	script, extendedRegex, err := r.buildSedScript()
	if err != nil {
		r.showError(err)
		return
	}

	r.cancelSedPreview() // stop an earlier run still in flight, if any
	ctx, cancel := context.WithCancel(context.Background())
	r.sedPreviewCancel = cancel

	targets := r.sedTargets
	r.sedPreviewTotal = len(targets)
	r.sedPreviewProcessed = 0
	r.sedPreviewAnimFrame = 0
	r.sedPreviewCurrentPos = ""
	r.renderSedPreviewStatus()
	r.sedPreviewTable.Clear()
	r.showOverlay(sedPreviewPage, r.sedPreviewLayout)

	// Both wrapped in safeGo (see its own doc comment): a panic in
	// either one used to take the whole process down without even
	// restoring the terminal, since neither runs inside
	// tview.Application.Run's own call stack — onPanic here resets the
	// same "in progress" state cancelSedPreview already resets on the
	// normal completion path just below, so a recovered panic doesn't
	// leave this dialog stuck believing a preview is still running
	// forever.
	onPanic := func() {
		r.cancelSedPreview()
		r.renderSedPreviewStatus()
	}
	r.safeGo("sed preview progress animation", onPanic, func() { r.animateSedPreviewProgress(ctx) })
	// Snapshotting the var here, on the calling goroutine, rather than
	// reading sedPreviewFunc directly inside the goroutine below: a test
	// overriding it (see isolateSedPreviewFunc) restores the original via
	// t.Cleanup as soon as the test function returns, which can easily
	// race with this goroutine still running past that point — reading
	// the var exactly once, before the goroutine starts, avoids the race
	// entirely rather than needing to synchronize the two.
	preview := sedPreviewFunc
	r.safeGo("sed preview", onPanic, func() {
		changes, skipped, err := preview(targets, script, extendedRegex, func(path string) {
			r.app.QueueUpdateDraw(func() {
				if ctx.Err() != nil {
					return
				}
				r.sedPreviewProcessed++
				r.sedPreviewCurrentPos = path
				r.renderSedPreviewStatus()
			})
		})
		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			r.cancelSedPreview()
			r.showSedPreviewResult(changes, skipped, err)
		})
	})
}

// cancelSedPreview stops whatever sedPreviewFunc call is currently in
// flight, if any, and its paired animateSedPreviewProgress ticker (both
// share the same ctx/sedPreviewCancel) — the same shape cancelSearch
// already has, so a slow preview started by an earlier, already-closed
// dialog never keeps updating a screen the user has moved on from.
func (r *Root) cancelSedPreview() {
	if r.sedPreviewCancel != nil {
		r.sedPreviewCancel()
		r.sedPreviewCancel = nil
	}
}

// animateSedPreviewProgress advances sedPreviewAnimFrame every
// hashAnimationInterval until ctx is done — the same ticker-driven "in
// progress" animation animateSearchProgress/animateHashProgress already
// use, reusing hashAnimationFrames directly rather than a second,
// separately-defined set of frames.
func (r *Root) animateSedPreviewProgress(ctx context.Context) {
	ticker := time.NewTicker(hashAnimationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			r.app.QueueUpdateDraw(func() {
				if ctx.Err() != nil {
					return
				}
				r.sedPreviewAnimFrame++
				r.renderSedPreviewStatus()
			})
		case <-ctx.Done():
			return
		}
	}
}

// renderSedPreviewStatus paints sedPreviewStatus's one line while a
// preview is still running: the current animation frame, how many of
// the targets have been looked at so far, and whichever one is current
// — the "Zeilenzahl als Indikator" the user asked for, the same
// "N of M, here's where" shape renderSearchStatus already reports for a
// live search.
func (r *Root) renderSedPreviewStatus() {
	frame := hashAnimationFrames[r.sedPreviewAnimFrame%len(hashAnimationFrames)]
	text := fmt.Sprintf("%s Checking %d of %d: %s", frame, r.sedPreviewProcessed, r.sedPreviewTotal, tview.Escape(r.sedPreviewCurrentPos))
	r.sedPreviewStatus.SetText(text)
}

// showSedPreviewResult renders sedPreviewFunc's own outcome. A bad sed
// script fails identically for every file (see replace.Preview's own
// doc comment), so that error closes the dialog and reports it via
// showError rather than showing an empty table; otherwise populates
// sedPreviewTable (see renderSedPreviewTable) and remembers changes for
// confirmApplySed.
//
// Split out from runSedPreview specifically so it's callable directly,
// without needing a real Application event loop to drain
// QueueUpdateDraw first — see sedPreviewFunc's own doc comment.
func (r *Root) showSedPreviewResult(changes []replace.FileChange, skipped map[string]string, err error) {
	if err != nil {
		r.hideOverlay()
		r.showError(err)
		return
	}
	r.sedPendingChanges = changes
	r.renderSedPreviewTable(changes, skipped)
}

// sedPreviewRow is one line sedPreviewTable actually shows — either one
// changed line within a file (name/line/excerpt all real), a summary row
// for a file that changed without any single line lining up positionally
// (see sedPreviewRows' own doc comment), or a skipped input (line "-",
// excerpt explaining why).
type sedPreviewRow struct {
	name, line, excerpt string
	skipped             bool
}

// sedPreviewRows computes sedPreviewTable's own rows from Preview's
// result — kept as a pure function, separate from actually populating
// the tview.Table, so the row content itself is testable without a
// screen (see TestSedPreviewRows).
//
// The per-line comparison is deliberately simple — index-for-index, not
// a real diff algorithm: a sed script that inserts or deletes whole
// lines shifts every following line out of alignment, which would show
// up here as every one of them looking "changed" even where only their
// position moved. A file whose line count changed, or where nothing
// lines up at the same position at all (a multi-line substitution),
// gets one summary row instead of a misleading line-by-line dump.
//
// Skipped entries are sorted by path — map iteration order isn't
// stable, and the table should look the same across two runs of an
// otherwise-identical preview.
func sedPreviewRows(changes []replace.FileChange, skipped map[string]string) []sedPreviewRow {
	var rows []sedPreviewRow
	for _, c := range changes {
		name := filepath.Base(c.Path)
		beforeLines := strings.Split(string(c.Before), "\n")
		afterLines := strings.Split(string(c.After), "\n")

		if len(beforeLines) != len(afterLines) {
			rows = append(rows, sedPreviewRow{
				name: name, line: "-",
				excerpt: fmt.Sprintf("(line count changed: %d -> %d)", len(beforeLines), len(afterLines)),
			})
			continue
		}

		shown := 0
		for i, before := range beforeLines {
			if before == afterLines[i] {
				continue
			}
			rows = append(rows, sedPreviewRow{name: name, line: strconv.Itoa(i + 1), excerpt: afterLines[i]})
			shown++
		}
		if shown == 0 {
			rows = append(rows, sedPreviewRow{name: name, line: "-", excerpt: "(changed, but no single line differs at the same position)"})
		}
	}

	paths := make([]string, 0, len(skipped))
	for path := range skipped {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		rows = append(rows, sedPreviewRow{name: filepath.Base(path), line: "-", excerpt: "skipped: " + skipped[path], skipped: true})
	}
	return rows
}

// renderSedPreviewTable fills sedPreviewTable (Name/Line/Excerpt,
// header fixed via SetFixed so it never scrolls away) from
// sedPreviewRows — the "looks like the Find results" layout the user
// asked for, replacing the earlier free-form text summary.
func (r *Root) renderSedPreviewTable(changes []replace.FileChange, skipped map[string]string) {
	r.sedPreviewTable.Clear()

	header := func(col int, text string) {
		r.sedPreviewTable.SetCell(0, col, tview.NewTableCell(text).
			SetTextColor(r.theme.Text).SetAttributes(tcell.AttrBold).SetSelectable(false))
	}
	header(0, "Name")
	header(1, "Line")
	header(2, "Excerpt")
	r.sedPreviewTable.SetFixed(1, 0)

	rows := sedPreviewRows(changes, skipped)
	for i, row := range rows {
		color := r.theme.Text
		if row.skipped {
			color = r.theme.PlaceholderText // dimmer, same role it already has elsewhere
		}
		r.sedPreviewTable.SetCell(i+1, 0, tview.NewTableCell(row.name).SetTextColor(color))
		r.sedPreviewTable.SetCell(i+1, 1, tview.NewTableCell(row.line).SetTextColor(color))
		r.sedPreviewTable.SetCell(i+1, 2, tview.NewTableCell(row.excerpt).SetTextColor(color))
	}

	if len(rows) == 0 {
		r.sedPreviewTable.SetCell(1, 0, tview.NewTableCell("No files would change.").SetTextColor(r.theme.PlaceholderText).SetSelectable(false))
	}
	r.sedPreviewTable.ScrollToBeginning()

	r.sedPreviewStatus.SetText(fmt.Sprintf("%d file(s) changed, %d skipped", len(changes), len(skipped)))
}

// newSedPreviewLayout builds Preview's own result screen once, from
// NewRoot: a one-line status (progress while running, a summary once
// done — see renderSedPreviewStatus/renderSedPreviewTable), the
// Name/Line/Excerpt table itself, and Apply/Back/Cancel below it — the
// same three-choice shape purgeConfirm already has.
func (r *Root) newSedPreviewLayout() *tview.Flex {
	r.sedPreviewStatus = tview.NewTextView()

	r.sedPreviewTable = tview.NewTable()
	r.sedPreviewTable.SetSelectable(true, false)

	r.sedPreviewActions = tview.NewList().ShowSecondaryText(false)
	r.sedPreviewActions.SetHighlightFullLine(true)
	r.sedPreviewActions.AddItem("Apply", "", 0, r.confirmApplySed)
	r.sedPreviewActions.AddItem("Back", "", 0, r.backToSedForm)
	r.sedPreviewActions.AddItem("Cancel", "", 0, r.closeSedPreview)
	r.sedPreviewActions.SetDoneFunc(r.backToSedForm) // Esc back to Sed, like the search results' own Esc

	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	layout.AddItem(r.sedPreviewStatus, 1, 0, false)
	layout.AddItem(r.sedPreviewTable, 0, 1, false)
	layout.AddItem(r.sedPreviewActions, 3, 0, true)
	return layout
}

// backToSedForm is Preview's own "Back" (and Escape — see
// newSedPreviewLayout): stops any preview computation still running
// (see cancelSedPreview) and returns to sedLayout with whatever the user
// already typed/toggled still in place (Clear/reset only happens on a
// fresh openSedReplace, not here) so a Find/Replace typo spotted in the
// preview can be fixed without starting over.
func (r *Root) backToSedForm() {
	r.cancelSedPreview()
	r.showOverlay(sedReplacePage, r.sedLayout)
}

// closeSedPreview is Preview's own "Cancel": stops any preview
// computation still running (see cancelSedPreview — without this, a
// slow preview finishing after the user already navigated away would
// still fire its completion callback and repopulate a table nobody is
// looking at any more) and closes the dialog entirely, unlike Back.
func (r *Root) closeSedPreview() {
	r.cancelSedPreview()
	r.hideOverlay()
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
// context menu's "sed".
func (r *Root) SedReplaceShortcut() {
	if r.acceptsGlobalShortcut() {
		r.openSedReplace()
	}
}
