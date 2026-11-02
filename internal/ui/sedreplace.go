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

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/replace"
)

// Flag labels, shared between resetSedForm (where sed.FlagsList's items
// are built) and r.sed.Flags (where their state is read back) — constants
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

// sedState holds the "Sed Replace" dialog's widgets/form values and the
// Preview dry-run's own widgets, pending changes, and progress
// animation.
type sedState struct {
	Form              *tview.Form
	FindField         *tview.InputField
	ReplaceField      *tview.InputField
	AdvancedField     *tview.InputField
	Flags             map[string]bool
	FlagsList         *tview.List
	Actions           *tview.List
	TitleBar          *tview.TextView
	ContentLayout     *tview.Flex
	Layout            *tview.Flex
	Targets           []string
	PreviewStatus     *tview.TextView
	PreviewTable      *tview.Table
	PreviewActions    *tview.List
	PreviewTitleBar   *tview.TextView
	PreviewLayout     *tview.Flex
	PendingChanges    []replace.FileChange
	PreviewCancel     context.CancelFunc
	PreviewAnimFrame  int
	PreviewProcessed  int
	PreviewTotal      int
	PreviewCurrentPos string
}

// openSedReplace is the context menu's "sed", and the plain-letter
// layer's "S" (see keymap.go) action: opens a dialog to run a real
// sed(1) substitution against the current selection (or the current
// row) — see internal/replace's own package doc for why this shells out
// to real sed rather than reimplementing its regex/scripting engine,
// and why it never uses sed's own -i.
func (r *Root) openSedReplace() {
	if r.panel.inArchiveView() {
		r.showError(errNotSupportedInArchive)
		return
	}
	if r.panel.isRemote() {
		r.showError(errNotSupportedRemote)
		return
	}
	targets := r.selectedOrCurrentPaths()
	if len(targets) == 0 {
		return
	}
	r.sed.Targets = targets
	r.resetSedForm()

	// height fits sed.TitleBar's own row plus sed.ContentLayout's three
	// stacked widgets (sed.Form's four fields, sed.FlagsList's five
	// toggles, sed.Actions' two buttons, plus spacing) — checked against a
	// real render, not guessed; a shorter value silently clipped the
	// bottom rows.
	width, height := 78, 18
	_, _, screenWidth, screenHeight := r.GetRect() // Root fills the whole screen
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.sed.Layout.SetRect(x, y, width, height)
	r.showOverlay(sedReplacePage, r.sed.Layout)
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
// sed.FlagsList instead (see newSedFlagsList), the same plain List-item-
// with-a-relabeling-glyph pattern this app's own context menu already
// uses for "Hide/Show hidden files" — a List has no such per-item
// styling fight, since it never forces one color onto every row.
//
// No border, matching every other floating widget in this app (see
// NewRoot's own comment on menu/quitConfirm/confirmDialog) — a plain
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

// resetSedForm rebuilds sed.Form's fields and sed.FlagsList's toggles
// fresh for the current r.sed.Targets. Clear(true) first — Form has no
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
	r.sed.Form.Clear(true)

	r.sed.Form.AddTextView("Target", sedTargetsLabel(r.sed.Targets), 0, 2, true, false)

	r.sed.FindField = tview.NewInputField().SetLabel("Find")
	r.sed.Form.AddFormItem(r.sed.FindField)
	r.sed.ReplaceField = tview.NewInputField().SetLabel("Replace with")
	r.sed.Form.AddFormItem(r.sed.ReplaceField)

	r.sed.AdvancedField = tview.NewInputField().SetLabel("Advanced sed script (overrides Find/Replace above)")
	// Tab/Enter on the form's own last field would otherwise just wrap
	// back to Find instead of ever reaching sed.FlagsList — the same
	// "verified directly against tview's own Form.Focus, not guessed"
	// gap newConnectForm's own doc comment already found and fixed for
	// the Connect dialog (and rsync.ExtraArgsField for Rsync): Form.Focus
	// unconditionally overwrites any SetDoneFunc with its own
	// SetFinishedFunc whenever the form gains focus, so SetInputCapture
	// — which runs first — is the only place left to intercept it. Set
	// fresh here, in resetSedForm, rather than once in newSedForm: this
	// field is itself rebuilt fresh on every open (see resetSedForm's
	// own doc comment), unlike connect.TagsField/rsync.ExtraArgsField,
	// which are each built once and kept.
	r.sed.AdvancedField.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab, tcell.KeyEnter:
			r.app.SetFocus(r.sed.FlagsList)
			return nil
		}
		return event
	})
	r.sed.Form.AddFormItem(r.sed.AdvancedField)

	// Seeded from settings rather than these fixed literals —
	// self-adapting, the same shape Duplicate's own settings and
	// runRsync's own persistRsyncFlags already have (see
	// runSedPreview): whatever combination Sed Replace last actually
	// ran with becomes the new starting point here.
	r.sed.Flags = map[string]bool{
		sedLabelRegex:           r.settings.SedRegex,
		sedLabelExtendedRegex:   r.settings.SedExtendedRegex,
		sedLabelCaseInsensitive: r.settings.SedCaseInsensitive,
		sedLabelGlobal:          r.settings.SedGlobal,
		sedLabelBackup:          r.settings.SedBackup,
	}
	r.sed.FlagsList.Clear()
	for _, label := range sedFlagOrder {
		label := label // capture for the closure below
		r.sed.FlagsList.AddItem(sedFlagItemText(label, r.sed.Flags[label]), "", 0, func() { r.toggleSedFlag(label) })
	}
}

// sedFlagItemText renders one sed.FlagsList row: the same outline/filled
// circle (○/●) this app already uses for the panel's own checkbox
// column (see checkboxText in panel.go), so a boolean toggle looks the
// same everywhere in this app rather than switching styles depending on
// which widget happens to implement it.
func sedFlagItemText(label string, checked bool) string {
	return fmt.Sprintf("%s  %s", checkboxText(checked), label)
}

// persistSedFlags writes sed.Flags back to settings, self-adapting the
// same way persistRsyncFlags already does for Rsync: runSedPreview
// calls this once buildSedScript has actually validated, so a rejected
// script (a bad advanced script, an empty Find) never overwrites a
// previous, real default.
func (r *Root) persistSedFlags() {
	apply := func(key, label string) {
		if opt, ok := optionSpecByKey(key); ok {
			opt.apply(r, strconv.FormatBool(r.sed.Flags[label]))
		}
	}
	apply("sed_regex", sedLabelRegex)
	apply("sed_extended_regex", sedLabelExtendedRegex)
	apply("sed_case_insensitive", sedLabelCaseInsensitive)
	apply("sed_global", sedLabelGlobal)
	apply("sed_backup", sedLabelBackup)
}

// toggleSedFlag flips one flag's state and re-renders just that row —
// the same "selectedFunc flips state, then relabels" shape
// Root.toggleHidden already uses for the context menu's own toggle.
func (r *Root) toggleSedFlag(label string) {
	r.sed.Flags[label] = !r.sed.Flags[label]
	for i, l := range sedFlagOrder {
		if l == label {
			r.sed.FlagsList.SetItemText(i, sedFlagItemText(label, r.sed.Flags[label]), "")
			return
		}
	}
}

// newSedFlagsList builds sed.FlagsList once, from NewRoot — see
// newSedForm's own doc comment for why the five flag toggles live here
// instead of as Form checkboxes. Repopulated fresh on every open (see
// resetSedForm), the same as sed.Form's own fields.
func (r *Root) newSedFlagsList() *tview.List {
	l := tview.NewList().ShowSecondaryText(false)
	l.SetHighlightFullLine(true)
	l.SetDoneFunc(r.hideOverlay) // Escape
	// Tab/Backtab continue the chain sed.AdvancedField's own
	// SetInputCapture starts and sed.Actions' own SetInputCapture picks
	// back up below — a List has no SetExitFunc the way Button does,
	// so this is the only place to intercept them (the identical shape
	// rsync.FlagsList's own doc comment already establishes for Rsync);
	// Escape itself is deliberately left alone here (falls through via
	// the final "return event"), reaching the List's own native
	// handling and, through it, SetDoneFunc above. Previously missing
	// entirely — this whole dropdown was keyboard-unreachable past the
	// form's own three fields (feature_upgrades.txt Befund 7).
	l.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			r.app.SetFocus(r.sed.Actions)
			return nil
		case tcell.KeyBacktab:
			r.app.SetFocus(r.sed.AdvancedField)
			return nil
		}
		return event
	})
	return l
}

// newSedActions builds sed.Form's own action row once, from NewRoot — a
// List rather than Form.AddButton, purely for consistency with
// sed.FlagsList/sed.PreviewActions right above and below it (a Form's own
// buttons would have worked fine here since buttons aren't checkboxes
// and don't hit the per-item-background problem newSedForm's own doc
// comment describes — this is a style choice, not a workaround).
func (r *Root) newSedActions() *tview.List {
	l := tview.NewList().ShowSecondaryText(false)
	l.SetHighlightFullLine(true)
	l.AddItem("Preview", "", 0, r.runSedPreview)
	l.AddItem("Cancel", "", 0, r.hideOverlay)
	l.SetDoneFunc(r.hideOverlay) // Escape
	// Tab wraps back to the form's own first field — through
	// sed.Form.SetFocus(0) then r.app.SetFocus(r.sed.Form), not a direct
	// r.app.SetFocus(r.sed.FindField): naming the field directly here
	// would leave sed.Form's own internal focusedElement counter stale,
	// the identical real, user-reported bug connectdialog.go's own
	// connect.ConnectBtn.SetExitFunc found and fixed first (see its own
	// doc comment for the full tview.Form internals) — the next
	// Backtab from Find would then land on the wrong field instead of
	// wrapping back to Advanced script. Backtab continues the chain
	// sed.FlagsList's own SetInputCapture starts.
	l.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			r.sed.Form.SetFocus(0)
			r.app.SetFocus(r.sed.Form)
			return nil
		case tcell.KeyBacktab:
			r.app.SetFocus(r.sed.FlagsList)
			return nil
		}
		return event
	})
	return l
}

// newSedLayout wraps sed.TitleBar (" Sed Replace ", per the user's own
// explicit request that every pane/overlay/dialog in this app get one)
// above sed.ContentLayout — the same widget/layout split
// menu/menuTitleBar/menuLayout already established. Focus still
// cascades all the way down to sed.Form regardless of this extra
// nesting level: tview.Flex.Focus delegates to whichever item was added
// with focus=true, and Application.SetFocus's own callback recursively
// re-invokes itself with that delegate, so a Flex-of-a-Flex resolves
// exactly like a single flat one would (verified directly against
// tview's own flex.go/application.go, not assumed).
func (r *Root) newSedLayout() *tview.Flex {
	r.sed.TitleBar = newPlainTitleBar("Sed Replace")
	r.sed.ContentLayout = r.newSedContentLayout()
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.sed.TitleBar, 1, 0, false).
		AddItem(r.sed.ContentLayout, 0, 1, true)
}

// newSedContentLayout stacks sed.Form (Target/Find/Replace/advanced
// script), sed.FlagsList (the five toggles), and sed.Actions
// (Preview/Cancel) — see newSedForm's own doc comment for why the flags
// live in a separate List rather than as Form checkboxes. Initial focus
// goes to sed.Form: typing Find/Replace immediately, without an extra
// click first, is the common case: reaching the flags or the buttons
// instead is one click away, the same as moving between any two of this
// app's other independent widgets (e.g. panel and bashLine) already is.
func (r *Root) newSedContentLayout() *tview.Flex {
	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	layout.AddItem(r.sed.Form, 8, 0, true)
	layout.AddItem(r.sed.FlagsList, 5, 0, false)
	layout.AddItem(r.sed.Actions, 2, 0, false)
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
	extendedRegex = r.sed.Flags[sedLabelExtendedRegex]

	script = strings.TrimSpace(r.sed.AdvancedField.GetText())
	if script != "" {
		return script, extendedRegex, nil
	}

	script, err = replace.BuildScript(
		r.sed.FindField.GetText(), r.sed.ReplaceField.GetText(),
		r.sed.Flags[sedLabelRegex], extendedRegex,
		r.sed.Flags[sedLabelCaseInsensitive], r.sed.Flags[sedLabelGlobal],
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

// runSedPreview is sed.Actions' own "Preview": builds the sed script
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
	r.persistSedFlags()

	r.cancelSedPreview() // stop an earlier run still in flight, if any
	ctx, cancel := context.WithCancel(context.Background())
	r.sed.PreviewCancel = cancel

	targets := r.sed.Targets
	r.sed.PreviewTotal = len(targets)
	r.sed.PreviewProcessed = 0
	r.sed.PreviewAnimFrame = 0
	r.sed.PreviewCurrentPos = ""
	r.renderSedPreviewStatus()
	r.sed.PreviewTable.Clear()
	r.showOverlay(sedPreviewPage, r.sed.PreviewLayout)

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
				r.sed.PreviewProcessed++
				r.sed.PreviewCurrentPos = path
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
// share the same ctx/sed.PreviewCancel) — the same shape cancelSearch
// already has, so a slow preview started by an earlier, already-closed
// dialog never keeps updating a screen the user has moved on from.
func (r *Root) cancelSedPreview() {
	if r.sed.PreviewCancel != nil {
		r.sed.PreviewCancel()
		r.sed.PreviewCancel = nil
	}
}

// animateSedPreviewProgress advances sed.PreviewAnimFrame every
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
				r.sed.PreviewAnimFrame++
				r.renderSedPreviewStatus()
			})
		case <-ctx.Done():
			return
		}
	}
}

// renderSedPreviewStatus paints sed.PreviewStatus's one line while a
// preview is still running: the current animation frame, how many of
// the targets have been looked at so far, and whichever one is current
// — the "Zeilenzahl als Indikator" the user asked for, the same
// "N of M, here's where" shape renderSearchStatus already reports for a
// live search.
func (r *Root) renderSedPreviewStatus() {
	frame := hashAnimationFrames[r.sed.PreviewAnimFrame%len(hashAnimationFrames)]
	text := fmt.Sprintf("%s Checking %d of %d: %s", frame, r.sed.PreviewProcessed, r.sed.PreviewTotal, tview.Escape(r.sed.PreviewCurrentPos))
	r.sed.PreviewStatus.SetText(text)
}

// showSedPreviewResult renders sedPreviewFunc's own outcome. A bad sed
// script fails identically for every file (see replace.Preview's own
// doc comment), so that error closes the dialog and reports it via
// showError rather than showing an empty table; otherwise populates
// sed.PreviewTable (see renderSedPreviewTable) and remembers changes for
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
	r.sed.PendingChanges = changes
	r.renderSedPreviewTable(changes, skipped)
}

// sedPreviewRow is one line sed.PreviewTable actually shows — either one
// changed line within a file (name/line/excerpt all real), a summary row
// for a file that changed without any single line lining up positionally
// (see sedPreviewRows' own doc comment), or a skipped input (line "-",
// excerpt explaining why).
type sedPreviewRow struct {
	name, line, excerpt string
	skipped             bool
}

// sedPreviewRows computes sed.PreviewTable's own rows from Preview's
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

// renderSedPreviewTable fills sed.PreviewTable (Name/Line/Excerpt,
// header fixed via SetFixed so it never scrolls away) from
// sedPreviewRows — the "looks like the Find results" layout the user
// asked for, replacing the earlier free-form text summary.
func (r *Root) renderSedPreviewTable(changes []replace.FileChange, skipped map[string]string) {
	r.sed.PreviewTable.Clear()

	header := func(col int, text string) {
		r.sed.PreviewTable.SetCell(0, col, tview.NewTableCell(text).
			SetTextColor(r.theme.Text).SetAttributes(tcell.AttrBold).SetSelectable(false))
	}
	header(0, "Name")
	header(1, "Line")
	header(2, "Excerpt")
	r.sed.PreviewTable.SetFixed(1, 0)

	rows := sedPreviewRows(changes, skipped)
	for i, row := range rows {
		color := r.theme.Text
		if row.skipped {
			color = r.theme.PlaceholderText // dimmer, same role it already has elsewhere
		}
		r.sed.PreviewTable.SetCell(i+1, 0, tview.NewTableCell(row.name).SetTextColor(color))
		r.sed.PreviewTable.SetCell(i+1, 1, tview.NewTableCell(row.line).SetTextColor(color))
		r.sed.PreviewTable.SetCell(i+1, 2, tview.NewTableCell(row.excerpt).SetTextColor(color))
	}

	if len(rows) == 0 {
		r.sed.PreviewTable.SetCell(1, 0, tview.NewTableCell("No files would change.").SetTextColor(r.theme.PlaceholderText).SetSelectable(false))
	}
	r.sed.PreviewTable.ScrollToBeginning()

	r.sed.PreviewStatus.SetText(fmt.Sprintf("%d file(s) changed, %d skipped", len(changes), len(skipped)))
}

// newSedPreviewLayout builds Preview's own result screen once, from
// NewRoot: a one-line status (progress while running, a summary once
// done — see renderSedPreviewStatus/renderSedPreviewTable), the
// Name/Line/Excerpt table itself, and Apply/Back/Cancel below it — the
// same three-choice shape confirmDialog already has.
func (r *Root) newSedPreviewLayout() *tview.Flex {
	r.sed.PreviewStatus = tview.NewTextView()

	r.sed.PreviewTable = tview.NewTable()
	r.sed.PreviewTable.SetSelectable(true, false)

	r.sed.PreviewActions = tview.NewList().ShowSecondaryText(false)
	r.sed.PreviewActions.SetHighlightFullLine(true)
	r.sed.PreviewActions.AddItem("Apply", "", 0, r.confirmApplySed)
	r.sed.PreviewActions.AddItem("Back", "", 0, r.backToSedForm)
	r.sed.PreviewActions.AddItem("Cancel", "", 0, r.closeSedPreview)
	r.sed.PreviewActions.SetDoneFunc(r.backToSedForm) // Esc back to Sed, like the search results' own Esc

	r.sed.PreviewTitleBar = newPlainTitleBar("Sed Preview")

	content := tview.NewFlex().SetDirection(tview.FlexRow)
	content.AddItem(r.sed.PreviewStatus, 1, 0, false)
	content.AddItem(r.sed.PreviewTable, 0, 1, false)
	content.AddItem(r.sed.PreviewActions, 3, 0, true)

	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.sed.PreviewTitleBar, 1, 0, false).
		AddItem(content, 0, 1, true)
}

// backToSedForm is Preview's own "Back" (and Escape — see
// newSedPreviewLayout): stops any preview computation still running
// (see cancelSedPreview) and returns to sed.Layout with whatever the user
// already typed/toggled still in place (Clear/reset only happens on a
// fresh openSedReplace, not here) so a Find/Replace typo spotted in the
// preview can be fixed without starting over.
func (r *Root) backToSedForm() {
	r.cancelSedPreview()
	r.showOverlay(sedReplacePage, r.sed.Layout)
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
// see sed.FlagsList's own toggle — just as permanently as those.
func (r *Root) confirmApplySed() {
	changes := r.sed.PendingChanges
	if len(changes) == 0 {
		return
	}
	backup := r.sed.Flags[sedLabelBackup]

	note := " Originals will be kept as .bak files."
	if !backup {
		note = " No backup will be kept."
	}
	message := fmt.Sprintf("Apply sed changes to %d file(s)?%s", len(changes), note)

	r.openPurgeConfirm(message, func() {
		applied, err := replace.Apply(changes, backup)
		r.sed.PendingChanges = nil
		if err != nil {
			r.activity.Logger.Error(activitylog.CategoryTextOps, fmt.Sprintf("sed replace: %d of %d files updated, then: %v", applied, len(changes), err))
			r.showError(fmt.Errorf("sed replace: %d of %d files updated, then: %w", applied, len(changes), err))
			return
		}
		r.activity.Logger.Action(activitylog.CategoryTextOps, fmt.Sprintf("sed replace: updated %d file(s)", applied))
		r.reloadPanel(nil)
	})
}

// SedReplaceShortcut used to be Ctrl+S's global action — nothing in
// cmd/breakthrough calls it any more. Sed Replace's own real keyboard
// path is the plain-letter layer's "S" (see openSedReplace/keymap.go),
// which needs no such guard of its own (acceptsPlainKeyCommand already
// covers the same ground more precisely). Kept rather than deleted as
// an exported building block, the same shape bottombar.go's
// RenameShortcut/EditShortcut/etc. already have for exactly this reason.
func (r *Root) SedReplaceShortcut() {
	if r.acceptsGlobalShortcut() {
		r.openSedReplace()
	}
}

// applySedTheme themes Sed Replace. sed.Form and its siblings are built
// unconditionally during NewRoot (unlike Batch Rename's/Search's own
// lazily-built widgets), so no nil guard is needed — matches the
// unconditional access applyTheme's own call site already had.
func (r *Root) applySedTheme(theme config.ResolvedTheme) {
	r.sed.Form.SetBackgroundColor(theme.SurfaceBackground)
	r.sed.Form.SetLabelColor(theme.TextColor)
	r.sed.Form.SetFieldBackgroundColor(theme.InputFocusedBackground)
	r.sed.Form.SetFieldTextColor(theme.TextColor)
	styleList(r.sed.FlagsList, theme)
	styleList(r.sed.Actions, theme)
	// FocusedBackground, fixed — Sed Replace/Preview are single-layer,
	// always-modal dialogs nothing else ever stacks on top of, the same
	// reasoning confirmDialogTitleBar/quitConfirmTitleBar/chmodTitleBar
	// already follow.
	r.sed.TitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.sed.TitleBar.SetTextColor(theme.TextColor)
}
