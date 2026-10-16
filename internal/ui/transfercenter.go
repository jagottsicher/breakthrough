package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/operations"
)

// The Transfer-Zentrale screen: a full-screen, mostly-read-only view of
// internal/operations' own Tracker — every Paste and backgrounded Rsync
// currently running or recently finished, per feature_ideas.txt's own
// "3. Aufgaben- und Transferzentrale". Cancel and pause/resume are the
// only real actions this screen takes — Retry is deliberately out of
// scope (see internal/operations' own doc comment: Operation doesn't
// hold enough to re-run a job). Finished summaries do survive a
// restart now (see internal/operations/persist.go); a running/paused
// one never does, so it's correctly absent rather than shown as still
// running after an unclean exit.

const transferCenterPage = "transfer-center"

// operationsPersistPath is operations.DefaultPath, indirected through
// a package-level var the same way notifyPersistPath (notifybar.go) and
// labelsPersistPath already are — so a test can point NewRoot's own
// operations.NewTrackerWithPersistence call somewhere isolated instead
// of every one of this package's hundreds of NewRoot calls touching
// whatever real file session.StateDir resolves to on the machine
// running `go test`.
var operationsPersistPath = operations.DefaultPath

// Column indices within transferCenterTable. Source/Target come last —
// both unbounded (a long path, a remote connection label) and the two
// least essential for a quick scan — the same "wide, least essential
// field goes last so it's what needs horizontal scrolling instead of
// the short, more important columns" reasoning mountsColumnWidth's own
// doc comment already gives.
const (
	transferCenterColStatus = iota
	transferCenterColType
	transferCenterColStarted
	transferCenterColResult
	transferCenterColSource
	transferCenterColTarget
)

func transferCenterColumnWidth(col int) int {
	switch col {
	case transferCenterColStatus:
		return 10
	case transferCenterColType:
		return 8
	case transferCenterColStarted:
		return 10
	case transferCenterColResult:
		return 28
	case transferCenterColSource:
		return 24
	default:
		return 0
	}
}

// transferCenterHintEntries is this screen's own bottom hint bar (see
// mountsHintEntries' own doc comment on why this is a function, not a
// var).
func transferCenterHintEntries() []listHintEntry {
	return []listHintEntry{
		{keys: []listHintKey{
			{"↑", simulateKeyOnFocused(tcell.KeyUp)},
			{"↓", simulateKeyOnFocused(tcell.KeyDown)},
		}, label: "move"},
		hintKey("c", "cancel", func(r *Root) { r.cancelSelectedTransferCenterOperation() }),
		hintKey("p", "pause/resume", func(r *Root) { r.pauseResumeSelectedTransferCenterOperation() }),
		hintKey("r", "refresh", func(r *Root) { r.reloadTransferCenter() }),
		hintKey("Esc", "close", func(r *Root) { r.closeTransferCenter() }),
	}
}

// newTransferCenterScreen builds the whole screen once, at startup —
// the same build-once/repopulate-on-open shape newMountsScreen already
// establishes.
func (r *Root) newTransferCenterScreen() {
	r.transferCenterTable = tview.NewTable()
	r.transferCenterTable.SetBorders(false)
	r.transferCenterTable.SetBorderPadding(1, 0, 2, 1)
	r.transferCenterTable.SetSelectable(true, false)
	r.transferCenterTable.SetFixed(1, 0)
	r.transferCenterTable.SetInputCapture(r.captureTransferCenterKey)

	r.transferCenterTitleBar = newPlainTitleBar("Transfers")
	r.transferCenterTitleBar.SetMouseCapture(captureReloadCloseTitleBarMouse(r.transferCenterTitleBar, r.reloadTransferCenter, r.closeTransferCenter))

	r.transferCenterHint = tview.NewTextView()
	r.transferCenterHint.SetWrap(false)
	r.transferCenterHint.SetDynamicColors(true)
	hintText, hintSpans := buildListHint(r.theme, transferCenterHintEntries())
	r.transferCenterHint.SetText(hintText)
	r.transferCenterHintSpans = hintSpans
	r.transferCenterHint.SetMouseCapture(r.captureListHintMouse(r.transferCenterHint, &r.transferCenterHintSpans))

	r.transferCenterScrollbar = newVerticalScrollbarForTable(r, r.transferCenterTable)
	tableRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.transferCenterTable, 0, 1, true).
		AddItem(r.transferCenterScrollbar, 1, 0, false)
	tableRow.SetMouseCapture(r.transferCenterScrollbar.captureMouse)

	r.transferCenterLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.transferCenterTitleBar, 1, 0, false).
		AddItem(tableRow, 0, 1, true).
		AddItem(r.transferCenterHint, 1, 0, false)

	// Subscribe once, here, not in openTransferCenter: Start/SetState/
	// Finish can fire while this screen isn't even open (a Paste started
	// from plain browsing, say), and the very next open should already
	// reflect it — reloadTransferCenter's own List() call handles that
	// case, but this callback is what keeps an *already open* screen
	// live without the user having to press "r" themselves. Always
	// called from the UI goroutine already (see pushNotifyToast's own
	// doc comment for why every real trigger this fires from — Paste/
	// Rsync's start/finish/pause/resume points — already runs inside
	// r.app.QueueUpdateDraw or a direct UI-thread call), so this touches
	// tview directly, the same as pushNotifyToast does.
	r.operations.Subscribe(func(operations.Operation) {
		if r.activePage == transferCenterPage {
			r.reloadTransferCenter()
		}
	})
}

// openTransferCenter shows the Transfer-Zentrale screen, freshly read
// every time — the same "always reflect what's real right now" reason
// openMounts does.
func (r *Root) openTransferCenter() {
	r.reloadTransferCenter()
	r.showOverlay(transferCenterPage, r.transferCenterLayout)
}

// closeTransferCenter hides the Transfer-Zentrale screen. Nothing to
// save — reading r.operations never mutates it.
func (r *Root) closeTransferCenter() {
	r.hideOverlay()
}

// reloadTransferCenter re-reads r.operations.List() — newest first,
// the more useful order for "what's happening now / just finished"
// than List()'s own oldest-first return, matching Messages'/Action
// Log's own newest-first convention — and re-renders.
func (r *Root) reloadTransferCenter() {
	ops := r.operations.List()
	r.transferCenterOps = make([]operations.Operation, len(ops))
	for i, op := range ops {
		r.transferCenterOps[len(ops)-1-i] = op
	}
	r.renderTransferCenter()
}

// transferCenterStatusText/transferCenterResultText render an
// Operation's own State/Outcome as this screen's own two short display
// columns — State while still live (Running/Paused), Outcome plus its
// counts/first error once Done.
func transferCenterStatusText(op operations.Operation) string {
	if op.State != operations.StateDone {
		return op.State.String()
	}
	return op.Outcome.String()
}

func transferCenterResultText(op operations.Operation) string {
	if op.State != operations.StateDone {
		return ""
	}
	switch op.Outcome {
	case operations.OutcomeCancelled:
		return "cancelled"
	case operations.OutcomeSucceeded:
		return fmt.Sprintf("%d succeeded", op.Succeeded)
	case operations.OutcomeFailed, operations.OutcomePartial:
		text := fmt.Sprintf("%d succeeded, %d failed", op.Succeeded, op.Failed)
		if len(op.Errors) > 0 {
			text += ": " + op.Errors[0]
		}
		return text
	default:
		return ""
	}
}

// transferCenterStatusColor mirrors notifyColor's own ✔/✘/⚠ color
// language (see its own doc comment) rather than inventing a fresh one
// for a screen showing largely the same three outcomes.
func transferCenterStatusColor(op operations.Operation, theme config.ResolvedTheme) tcell.Color {
	if op.State != operations.StateDone {
		return theme.Text
	}
	switch op.Outcome {
	case operations.OutcomeSucceeded:
		return theme.EntryExecutable
	case operations.OutcomeFailed:
		return theme.CriticalText
	case operations.OutcomePartial:
		return theme.WarningText
	default:
		return theme.PlaceholderText
	}
}

// renderTransferCenter fills the table from r.transferCenterOps — a
// bold header row, then one row per Operation, oldest-at-bottom (see
// reloadTransferCenter's own doc comment).
func (r *Root) renderTransferCenter() {
	r.transferCenterTable.Clear()

	renderReloadCloseTitleBar(r.transferCenterTitleBar, " Transfers ", r.lastScreenWidth)

	header := func(col int, text string) {
		r.transferCenterTable.SetCell(0, col,
			tview.NewTableCell(padRight(text, transferCenterColumnWidth(col))).
				SetTextColor(r.theme.Text).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false))
	}
	header(transferCenterColStatus, "Status")
	header(transferCenterColType, "Type")
	header(transferCenterColStarted, "Started")
	header(transferCenterColResult, "Result")
	header(transferCenterColSource, "Source")
	header(transferCenterColTarget, "Target")

	if len(r.transferCenterOps) == 0 {
		showTablePlaceholder(r.transferCenterTable, "Nothing running or recently finished.", r.theme.PlaceholderText)
		return
	}
	enableTableSelection(r.transferCenterTable)

	for i, op := range r.transferCenterOps {
		row := i + 1
		statusColor := transferCenterStatusColor(op, r.theme)

		r.transferCenterTable.SetCell(row, transferCenterColStatus,
			tview.NewTableCell(padRight(transferCenterStatusText(op), transferCenterColumnWidth(transferCenterColStatus))).
				SetTextColor(statusColor).SetSelectable(true))
		r.transferCenterTable.SetCell(row, transferCenterColType,
			tview.NewTableCell(padRight(op.Type, transferCenterColumnWidth(transferCenterColType))).
				SetTextColor(r.theme.Text).SetSelectable(true))
		r.transferCenterTable.SetCell(row, transferCenterColStarted,
			tview.NewTableCell(padRight(op.StartTime.Format("15:04:05"), transferCenterColumnWidth(transferCenterColStarted))).
				SetTextColor(r.theme.PlaceholderText).SetSelectable(true))
		r.transferCenterTable.SetCell(row, transferCenterColResult,
			tview.NewTableCell(padRight(transferCenterResultText(op), transferCenterColumnWidth(transferCenterColResult))).
				SetTextColor(statusColor).SetSelectable(true))
		r.transferCenterTable.SetCell(row, transferCenterColSource,
			tview.NewTableCell(padRight(op.Source, transferCenterColumnWidth(transferCenterColSource))).
				SetTextColor(r.theme.Text).SetSelectable(true))
		r.transferCenterTable.SetCell(row, transferCenterColTarget,
			tview.NewTableCell(op.Target).
				SetTextColor(r.theme.Text).SetSelectable(true))
	}

	if row, _ := r.transferCenterTable.GetSelection(); row < 1 || row > len(r.transferCenterOps) {
		r.transferCenterTable.Select(1, 0)
	}
}

// selectedTransferCenterOperation returns whichever Operation the
// cursor currently sits on, if any — false once the table is showing
// the empty placeholder, the same guard captureMountsKey's own row
// actions would need if it had any.
func (r *Root) selectedTransferCenterOperation() (operations.Operation, bool) {
	row, _ := r.transferCenterTable.GetSelection()
	if row < 1 || row > len(r.transferCenterOps) {
		return operations.Operation{}, false
	}
	return r.transferCenterOps[row-1], true
}

// cancelSelectedTransferCenterOperation is "c"'s own action — maps the
// selected row's own Operation.ID back to whichever live job it belongs
// to, if any, and calls that job's own existing cancel function rather
// than building a second ID→job registry this package would have to
// keep in sync: Root already knows (r.pasteJob.opID/r.rsyncJob.opID),
// and there's at most one of each live at a time. A row with no live
// job behind it (already finished, or belongs to neither) is simply
// left alone.
func (r *Root) cancelSelectedTransferCenterOperation() {
	op, ok := r.selectedTransferCenterOperation()
	if !ok || op.State == operations.StateDone {
		return
	}
	switch {
	case r.pasteJob != nil && r.pasteJob.opID == op.ID:
		r.cancelPasteJob()
	case r.rsyncJob != nil && r.rsyncJob.opID == op.ID:
		r.cancelRsyncJob()
	}
}

// pauseResumeSelectedTransferCenterOperation is "p"'s own action —
// Paste only (see internal/operations' own doc comment on why
// StatePaused exists at all): Rsync has nothing to pause mid-run (see
// pastepause.go's own package doc comment on why this was built for
// Paste specifically). Running pauses (the same dialog Ctrl+C already
// opens); Paused resumes (the same action that dialog's own "Continue"
// answer already runs).
func (r *Root) pauseResumeSelectedTransferCenterOperation() {
	op, ok := r.selectedTransferCenterOperation()
	if !ok || r.pasteJob == nil || r.pasteJob.opID != op.ID {
		return
	}
	switch op.State {
	case operations.StateRunning:
		r.requestPastePause()
	case operations.StatePaused:
		r.resumePastePause()
	}
}

// captureTransferCenterKey is the Transfer-Zentrale screen's own key
// handling: Escape closes it, "r" re-reads r.operations, "c"/"p" act on
// the selected row (see their own doc comments).
func (r *Root) captureTransferCenterKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeTransferCenter()
		return nil
	}
	if event.Key() == tcell.KeyRune {
		switch event.Rune() {
		case 'r':
			r.reloadTransferCenter()
			return nil
		case 'c':
			r.cancelSelectedTransferCenterOperation()
			return nil
		case 'p':
			r.pauseResumeSelectedTransferCenterOperation()
			return nil
		}
	}
	return event
}

// applyTransferCenterTheme themes the Transfer-Zentrale screen — split
// out of applyTheme (see its own doc comment). Guarded the same way
// applyMountsTheme is.
func (r *Root) applyTransferCenterTheme(theme config.ResolvedTheme) {
	if r.transferCenterTable == nil {
		return
	}
	r.transferCenterLayout.SetBackgroundColor(theme.PanelBackground)
	r.transferCenterTable.SetBackgroundColor(theme.PanelBackground)
	r.transferCenterTable.SetSelectedStyle(tcell.StyleDefault.
		Background(theme.SelectionBackground).
		Foreground(theme.TextColor))

	r.transferCenterTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.transferCenterTitleBar.SetTextColor(theme.TextColor)
	r.transferCenterHint.SetBackgroundColor(theme.SurfaceBackground)
	r.transferCenterHint.SetTextColor(theme.TextColor)
	hintText, hintSpans := buildListHint(theme, transferCenterHintEntries())
	r.transferCenterHint.SetText(hintText)
	r.transferCenterHintSpans = hintSpans

	r.renderTransferCenter()
}
