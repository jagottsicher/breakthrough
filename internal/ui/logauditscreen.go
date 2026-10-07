package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/logview"
)

// The Log Audit screen ("jL"): two full-screen catalog layers stacked
// via pushOverlay (see logaudit.go's own doc comment on
// logAuditSelectionLayout/logAuditViewerLayout), plus a small detail
// modal, the exact same three-piece shape Messages/messagedetail.go
// already establishes for an overlay-with-its-own-detail-view.
//
// Unlike every format/merge concern (internal/logview), everything in
// this file is screen-specific glue: turning Entry values into table
// rows, building the keyword/level highlighting, and the keymap each
// of the three pieces needs — the same split activitylogscreen.go
// already keeps between itself and internal/activitylog.

const (
	logAuditSelectionPage = "logaudit-select"
	logAuditViewerPage    = "logaudit-viewer"
	logAuditDetailPage    = "logaudit-detail"
)

const (
	logAuditSelName = iota
	logAuditSelInfo
)

const (
	logAuditColTime = iota
	logAuditColLevel
	logAuditColSource
	logAuditColMessage
)

// logAuditDetailMaxWidth mirrors messagesDetailMaxWidth's own reasoning
// — a centered, screen-sized modal for one entry's full text.
const logAuditDetailMaxWidth = 90

func logAuditSelectionHintEntries() []listHintEntry {
	return []listHintEntry{
		hintKey("Enter", "open log audit", func(r *Root) { r.openLogAuditViewer() }),
		hintKey("r", "re-scan directory", func(r *Root) { r.reloadLogAuditDiscovery() }),
		hintKey("Esc", "close", func(r *Root) { r.closeLogAuditSelection() }),
	}
}

func logAuditViewerHintEntries() []listHintEntry {
	return []listHintEntry{
		hintKey("Tab", "next field", simulateKeyOnFocused(tcell.KeyTab)),
		hintKey("e", "next error", func(r *Root) { r.navigateLogAuditLevel(logview.LevelError) }),
		hintKey("w", "next warning", func(r *Root) { r.navigateLogAuditLevel(logview.LevelWarn) }),
		hintKey("Enter", "details (while the list has focus)", func(r *Root) {
			row, _ := r.logAuditViewerTable.GetSelection()
			r.openLogAuditDetail(row)
		}),
		hintKey("r", "re-read files (while the list has focus)", func(r *Root) { r.reopenLogAuditViewer() }),
		hintKey("Esc", "back to file selection", func(r *Root) { r.closeLogAuditViewer() }),
	}
}

// newLogAuditScreen builds every piece of the Log Audit screen once, at
// startup — the same build-once/repopulate-on-open shape every other
// full-screen catalog in this package uses.
func (r *Root) newLogAuditScreen() {
	r.newLogAuditSelectionScreen()
	r.newLogAuditViewerScreen()
	r.newLogAuditDetailScreen()
}

func (r *Root) newLogAuditSelectionScreen() {
	r.logAuditTitleBar = newPlainTitleBar("Log Audit — select files")

	r.logAuditTable = tview.NewTable()
	r.logAuditTable.SetBorders(false)
	r.logAuditTable.SetBorderPadding(1, 0, 2, 1)
	r.logAuditTable.SetSelectable(true, false)
	r.logAuditTable.SetFixed(1, 0)
	r.logAuditTable.SetInputCapture(r.captureLogAuditSelectionTableKey)
	r.logAuditTable.SetSelectedFunc(func(row, col int) { r.openLogAuditViewer() })

	r.logAuditHint = tview.NewTextView()
	r.logAuditHint.SetWrap(false)
	r.logAuditHint.SetDynamicColors(true)
	hintText, hintSpans := buildListHint(r.theme, logAuditSelectionHintEntries())
	r.logAuditHint.SetText(hintText)
	r.logAuditHintSpans = hintSpans
	r.logAuditHint.SetMouseCapture(r.captureListHintMouse(r.logAuditHint, &r.logAuditHintSpans))

	r.logAuditSelectionLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.logAuditTitleBar, 1, 0, false).
		AddItem(r.logAuditTable, 0, 1, true).
		AddItem(r.logAuditHint, 1, 0, false)
}

func (r *Root) newLogAuditViewerScreen() {
	r.logAuditViewerTitle = newPlainTitleBar("Log Audit")

	r.logAuditKeywordField = tview.NewInputField()
	r.logAuditKeywordField.SetLabel("Filter: ")
	r.logAuditKeywordField.SetChangedFunc(func(string) { r.renderLogAuditViewer() })
	r.logAuditKeywordField.SetDoneFunc(func(key tcell.Key) { r.logAuditViewerFieldDone(key) })
	r.logAuditKeywordField.SetFocusFunc(func() { styleInput(r.logAuditKeywordField, r.theme, true) })
	r.logAuditKeywordField.SetBlurFunc(func() { styleInput(r.logAuditKeywordField, r.theme, false) })

	r.logAuditViewerTable = tview.NewTable()
	r.logAuditViewerTable.SetBorders(false)
	r.logAuditViewerTable.SetBorderPadding(1, 0, 2, 1)
	r.logAuditViewerTable.SetSelectable(true, false)
	r.logAuditViewerTable.SetFixed(1, 0)
	r.logAuditViewerTable.SetInputCapture(r.captureLogAuditViewerTableKey)
	r.logAuditViewerTable.SetSelectedFunc(func(row, col int) { r.openLogAuditDetail(row) })

	r.logAuditViewerHint = tview.NewTextView()
	r.logAuditViewerHint.SetWrap(false)
	r.logAuditViewerHint.SetDynamicColors(true)
	hintText, hintSpans := buildListHint(r.theme, logAuditViewerHintEntries())
	r.logAuditViewerHint.SetText(hintText)
	r.logAuditViewerSpans = hintSpans
	r.logAuditViewerHint.SetMouseCapture(r.captureListHintMouse(r.logAuditViewerHint, &r.logAuditViewerSpans))

	r.logAuditViewerLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.logAuditViewerTitle, 1, 0, false).
		AddItem(r.logAuditKeywordField, 1, 0, false).
		AddItem(r.logAuditViewerTable, 0, 1, true).
		AddItem(r.logAuditViewerHint, 1, 0, false)
}

func (r *Root) newLogAuditDetailScreen() {
	r.logAuditDetailTitleBar = newPlainTitleBar("Log entry")

	r.logAuditDetailView = tview.NewTextView()
	r.logAuditDetailView.SetWrap(true)
	r.logAuditDetailView.SetDynamicColors(true)
	r.logAuditDetailView.SetBorderPadding(0, 0, 1, 1)
	r.logAuditDetailView.SetInputCapture(r.captureLogAuditDetailKey)

	r.logAuditDetailLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.logAuditDetailTitleBar, 1, 0, false).
		AddItem(r.logAuditDetailView, 0, 1, true)
}

// logAuditGroupInfo describes one FileGroup for the selection table's
// own Info column — file count, how many are compressed (gzip, xz,
// zstd, and bzip2 are all decompressed transparently — see
// logview.Open), and a call-out for any file whose compression Open
// doesn't recognize at all, which stays possible even though every
// extension familyBase/compressedExt currently parses out is
// supported today.
func logAuditGroupInfo(g logview.FileGroup) string {
	compressed, unsupported := 0, 0
	for _, f := range g.Files {
		if f.Compressed {
			compressed++
		}
		if !f.Supported {
			unsupported++
		}
	}
	info := strconv.Itoa(len(g.Files)) + " file"
	if len(g.Files) != 1 {
		info += "s"
	}
	if compressed > 0 {
		info += fmt.Sprintf(" (%d compressed)", compressed)
	}
	if unsupported > 0 {
		info += fmt.Sprintf(" — %d unsupported compression", unsupported)
	}
	return info
}

// renderLogAuditSelection fills the selection table: the family's own
// base name and logAuditGroupInfo's own summary — no checkbox column;
// Enter simply opens whichever row the cursor is already on (see
// openLogAuditViewer's own doc comment for why).
func (r *Root) renderLogAuditSelection() {
	r.logAuditTable.Clear()

	header := func(col int, text string) {
		r.logAuditTable.SetCell(0, col,
			tview.NewTableCell(text).
				SetTextColor(r.theme.Text).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false))
	}
	header(logAuditSelName, "Name")
	header(logAuditSelInfo, "Info")

	if r.logAuditDiscoverErr != nil {
		showTablePlaceholder(r.logAuditTable, r.logAuditDiscoverErr.Error(), r.theme.EntryError)
		return
	}
	if len(r.logAuditGroups) == 0 {
		showTablePlaceholder(r.logAuditTable, "No log files found in "+r.logAuditDir, r.theme.PlaceholderText)
		return
	}

	enableTableSelection(r.logAuditTable)
	for i, g := range r.logAuditGroups {
		row := i + 1
		r.logAuditTable.SetCell(row, logAuditSelName,
			tview.NewTableCell(g.Base).SetTextColor(r.theme.Text).SetSelectable(true))
		r.logAuditTable.SetCell(row, logAuditSelInfo,
			tview.NewTableCell(logAuditGroupInfo(g)).SetTextColor(r.theme.MutedTextColor).SetSelectable(true))
	}

	if cur, _ := r.logAuditTable.GetSelection(); cur < 1 || cur > len(r.logAuditGroups) {
		r.logAuditTable.Select(1, 0)
	}
}

// logAuditTimeText renders an Entry's own Time — same layout
// activityLogTimeText already uses elsewhere in this package.
func logAuditTimeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}

// logAuditLevelColor mirrors activityLogLevelColor's own "stop sign"
// convention: Error/Fatal red, Warn the app's own WarningText, Info/
// Debug/Trace muted, Unknown plain text (nothing to call out).
func logAuditLevelColor(theme config.ResolvedTheme, level logview.Level) tcell.Color {
	switch level {
	case logview.LevelError, logview.LevelFatal:
		return theme.CriticalText
	case logview.LevelWarn:
		return theme.WarningText
	case logview.LevelUnknown:
		return theme.Text
	default:
		return theme.MutedTextColor
	}
}

// logAuditMatchesKeyword reports whether e's own Source or Message
// contains keyword, case-insensitively — "" always matches, the same
// "blank filter shows everything" contract filterActivityLogEntries
// already has.
func logAuditMatchesKeyword(e logview.Entry, keyword string) bool {
	if keyword == "" {
		return true
	}
	keyword = strings.ToLower(keyword)
	return strings.Contains(strings.ToLower(e.Message), keyword) || strings.Contains(strings.ToLower(e.Source), keyword)
}

// logAuditHighlight escapes text for safe use inside a tview cell (see
// panel.go's own addRow doc comment on why escaping has to happen
// before any tag is added, not after) and, if keyword is non-empty,
// wraps every case-insensitive occurrence of it in a background tag
// (reusing nameHighlightTags — panel.go — exactly as-is: it already
// wraps an escaped string in "set background, then reset" tags) —
// matching is done against the original, unescaped text so the
// indices found are never thrown off by an escape sequence tview.Escape
// may have inserted.
func logAuditHighlight(text, keyword string, bg tcell.Color) string {
	if keyword == "" {
		return tview.Escape(text)
	}
	lower := strings.ToLower(text)
	lowerKey := strings.ToLower(keyword)

	var b strings.Builder
	rest := text
	restLower := lower
	for {
		idx := strings.Index(restLower, lowerKey)
		if idx < 0 {
			b.WriteString(tview.Escape(rest))
			break
		}
		b.WriteString(tview.Escape(rest[:idx]))
		match := rest[idx : idx+len(keyword)]
		b.WriteString(nameHighlightTags(tview.Escape(match), bg))
		rest = rest[idx+len(keyword):]
		restLower = restLower[idx+len(keyword):]
	}
	return b.String()
}

// renderLogAuditViewer fills the viewer table with every entry
// matching the keyword field, Time/Level/Source/Message columns, the
// matched keyword highlighted in Source/Message via logAuditHighlight
// — and updates the title bar with the running counts the original
// conversation's own mockup called for ("4,812 events │ 3 files │ 2
// compressed │ 1 error").
func (r *Root) renderLogAuditViewer() {
	r.logAuditViewerTable.Clear()

	header := func(col int, text string) {
		r.logAuditViewerTable.SetCell(0, col,
			tview.NewTableCell(text).
				SetTextColor(r.theme.Text).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false))
	}
	header(logAuditColTime, padRight("Time", 19))
	header(logAuditColLevel, padRight("Level", 5))
	header(logAuditColSource, "Source")
	header(logAuditColMessage, "Message")

	keyword := r.logAuditKeywordField.GetText()
	var shown []logview.Entry
	errCount, warnCount := 0, 0
	for _, e := range r.logAuditAllEntries {
		switch e.Level {
		case logview.LevelError, logview.LevelFatal:
			errCount++
		case logview.LevelWarn:
			warnCount++
		}
		if logAuditMatchesKeyword(e, keyword) {
			shown = append(shown, e)
		}
	}

	r.logAuditViewerTitle.SetText(fmt.Sprintf(" Log Audit — %d events │ %d files │ %d skipped │ %d errors │ %d warnings ",
		len(r.logAuditAllEntries), r.logAuditFiles, r.logAuditSkipped, errCount, warnCount))

	if r.logAuditParseErr != nil {
		showTablePlaceholder(r.logAuditViewerTable, r.logAuditParseErr.Error(), r.theme.EntryError)
		return
	}
	if len(shown) == 0 {
		placeholder := "No log entries."
		if len(r.logAuditAllEntries) > 0 {
			placeholder = "No entries match the current filter."
		}
		showTablePlaceholder(r.logAuditViewerTable, placeholder, r.theme.PlaceholderText)
		return
	}

	enableTableSelection(r.logAuditViewerTable)
	bg := r.theme.ButtonBackground
	for i, e := range shown {
		row := i + 1
		levelColor := logAuditLevelColor(r.theme, e.Level)
		cell := func(col int, text string, color tcell.Color) {
			r.logAuditViewerTable.SetCell(row, col,
				tview.NewTableCell(text).SetTextColor(color).SetSelectable(true))
		}
		cell(logAuditColTime, padRight(logAuditTimeText(e.Time), 19), r.theme.Text)
		cell(logAuditColLevel, padRight(e.Level.String(), 5), levelColor)
		cell(logAuditColSource, logAuditHighlight(e.Source, keyword, bg), r.theme.Text)
		cell(logAuditColMessage, logAuditHighlight(e.Message, keyword, bg), r.theme.Text)
		r.logAuditViewerTable.GetCell(row, logAuditColTime).SetReference(e)
	}

	if cur, _ := r.logAuditViewerTable.GetSelection(); cur < 1 || cur > len(shown) {
		r.logAuditViewerTable.Select(1, 0)
	}
}

// logAuditEntryAt returns the Entry backing row, if any — stashed on
// the Time cell via SetReference in renderLogAuditViewer, the same
// "reference the real data, don't re-derive it from displayed text"
// convention Panel's own addRow/CurrentRowPath already use.
func (r *Root) logAuditEntryAt(row int) (logview.Entry, bool) {
	cell := r.logAuditViewerTable.GetCell(row, logAuditColTime)
	if cell == nil {
		return logview.Entry{}, false
	}
	e, ok := cell.GetReference().(logview.Entry)
	return e, ok
}

// navigateLogAuditLevel moves the cursor to the next row at or below
// level's own severity (Error also matches Fatal — the same "e" key
// covers both, since both are equally worth stopping at) — wraps
// around to the top once it reaches the bottom, so "e"/"w" read as "the
// next one, cycling" rather than stalling once the last match has been
// seen.
func (r *Root) navigateLogAuditLevel(level logview.Level) {
	rowCount := r.logAuditViewerTable.GetRowCount()
	if rowCount <= 1 {
		return
	}
	cur, _ := r.logAuditViewerTable.GetSelection()
	matches := func(l logview.Level) bool {
		if level == logview.LevelError {
			return l == logview.LevelError || l == logview.LevelFatal
		}
		return l == level
	}
	for i := 1; i < rowCount; i++ {
		row := cur + i
		if row >= rowCount {
			row -= rowCount - 1
		}
		if e, ok := r.logAuditEntryAt(row); ok && matches(e.Level) {
			r.logAuditViewerTable.Select(row, 0)
			return
		}
	}
}

// openLogAuditDetail shows row's own full entry — File/Line, Time,
// Level, Source, and the complete Message with the current keyword
// highlighted — centered over the whole screen, the same shape
// openMessageDetail (messagedetail.go) already establishes.
func (r *Root) openLogAuditDetail(row int) {
	e, ok := r.logAuditEntryAt(row)
	if !ok {
		return
	}

	width := logAuditDetailMaxWidth
	if _, _, screenWidth, _ := r.GetRect(); screenWidth > 2 && width > screenWidth-2 {
		width = screenWidth - 2
	}

	keyword := r.logAuditKeywordField.GetText()
	bg := r.theme.ButtonBackground
	header := fmt.Sprintf("%s  %s  %s\n", logAuditTimeText(e.Time), e.Level.String(), e.File)
	body := strings.Join(wrapText(e.Message, width), "\n")
	text := tview.Escape(header) + logAuditHighlight(body, keyword, bg)
	r.logAuditDetailView.SetText(text)
	r.logAuditDetailTitleBar.SetText(" Log entry ")

	textWidth, textHeight := textSize(header + body)
	height := textHeight + 1 // +1 for the title bar row
	_, _, screenWidth, screenHeight := r.GetRect()
	x, y, boxWidth, boxHeight := r.clampToScreen((screenWidth-textWidth)/2, (screenHeight-height)/2, textWidth, height)

	r.logAuditDetailLayout.SetRect(x, y, boxWidth, boxHeight)
	r.pushOverlay(logAuditDetailPage, r.logAuditDetailLayout, nil)
}

func (r *Root) closeLogAuditDetail() {
	r.hideOverlay()
}

// captureLogAuditSelectionTableKey: "r" re-scans the directory, Escape
// closes — the same shape captureActivityLogTableKey already
// establishes. Enter is handled by SetSelectedFunc, not here (see
// newLogAuditSelectionScreen).
func (r *Root) captureLogAuditSelectionTableKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeLogAuditSelection()
		return nil
	}
	if event.Key() == tcell.KeyRune && event.Rune() == 'r' {
		r.reloadLogAuditDiscovery()
		return nil
	}
	return event
}

// logAuditViewerFieldDone is the keyword field's own SetDoneFunc: Tab/
// Enter moves focus to the table, Escape closes the viewer — there is
// only one field here, unlike Activity Log's keyword/time pair, so
// there's no Backtab/"previous field" case to handle.
func (r *Root) logAuditViewerFieldDone(key tcell.Key) {
	switch key {
	case tcell.KeyEscape:
		r.closeLogAuditViewer()
	case tcell.KeyTab, tcell.KeyBacktab, tcell.KeyEnter:
		r.app.SetFocus(r.logAuditViewerTable)
	}
}

// captureLogAuditViewerTableKey: Tab returns focus to the keyword
// field, "e"/"w" jump to the next error/warning, "r" re-reads the same
// files, Escape closes the viewer (back to the selection screen, see
// closeLogAuditViewer/pushOverlay).
func (r *Root) captureLogAuditViewerTableKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeLogAuditViewer()
		return nil
	}
	if event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab {
		r.app.SetFocus(r.logAuditKeywordField)
		return nil
	}
	if event.Key() == tcell.KeyRune {
		switch event.Rune() {
		case 'e':
			r.navigateLogAuditLevel(logview.LevelError)
			return nil
		case 'w':
			r.navigateLogAuditLevel(logview.LevelWarn)
			return nil
		case 'r':
			r.reopenLogAuditViewer()
			return nil
		}
	}
	return event
}

// captureLogAuditDetailKey: Escape is the only key this modal itself
// handles — same shape captureMessageDetailKey already establishes.
func (r *Root) captureLogAuditDetailKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeLogAuditDetail()
		return nil
	}
	return event
}

// applyLogAuditTheme themes all three Log Audit pieces — split out of
// applyTheme the same way applyActivityLogTheme already is.
func (r *Root) applyLogAuditTheme(theme config.ResolvedTheme) {
	if r.logAuditTable == nil {
		return
	}
	r.logAuditSelectionLayout.SetBackgroundColor(theme.SurfaceBackground)
	r.logAuditTable.SetBackgroundColor(theme.SurfaceBackground)
	r.logAuditTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.logAuditTitleBar.SetTextColor(theme.TextColor)
	r.logAuditHint.SetBackgroundColor(theme.InputBackground)
	r.logAuditHint.SetTextColor(theme.MutedTextColor)
	hintText, hintSpans := buildListHint(theme, logAuditSelectionHintEntries())
	r.logAuditHint.SetText(hintText)
	r.logAuditHintSpans = hintSpans
	r.renderLogAuditSelection() // cell colors are baked in per cell, not looked up live at draw time

	r.logAuditViewerLayout.SetBackgroundColor(theme.SurfaceBackground)
	r.logAuditViewerTable.SetBackgroundColor(theme.SurfaceBackground)
	r.logAuditViewerTitle.SetBackgroundColor(theme.InputFocusedBackground)
	r.logAuditViewerTitle.SetTextColor(theme.TextColor)
	styleInput(r.logAuditKeywordField, theme, r.logAuditKeywordField.HasFocus())
	r.logAuditKeywordField.SetLabelColor(theme.TextColor)
	r.logAuditViewerHint.SetBackgroundColor(theme.InputBackground)
	r.logAuditViewerHint.SetTextColor(theme.MutedTextColor)
	viewerHintText, viewerHintSpans := buildListHint(theme, logAuditViewerHintEntries())
	r.logAuditViewerHint.SetText(viewerHintText)
	r.logAuditViewerSpans = viewerHintSpans
	r.renderLogAuditViewer()

	r.logAuditDetailLayout.SetBackgroundColor(theme.PopupBackground)
	r.logAuditDetailView.SetBackgroundColor(theme.PopupBackground)
	r.logAuditDetailView.SetTextColor(theme.TextColor)
	r.logAuditDetailTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.logAuditDetailTitleBar.SetTextColor(theme.TextColor)
}
