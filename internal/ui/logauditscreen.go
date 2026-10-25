package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/filterexpr"
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
	// Size/Modified describe the family's own newest file (see
	// logview.FileGroup.Newest) — per the user's own explicit request
	// to see those without opening the group first.
	logAuditSelSize
	logAuditSelModified
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

// logAuditViewerHintEntries: "e"/"w"/"Enter"/"r"/"f"/"s" are all bound
// on the table's own InputCapture (see
// newLogAuditViewerScreen/captureLogAuditViewerTableKey) — every one
// of them fires only while the list itself has focus, typed into a
// filter field otherwise. Rather than repeat "(while the list has
// focus)" on each of those six entries (what this used to do — per
// the user's own explicit report, both too long and inconsistently
// applied, since "e"/"w" never got the suffix either even though the
// same rule already covered them too), renderLogAuditViewer says this
// once, in the title bar, covering the whole group together.
func logAuditViewerHintEntries() []listHintEntry {
	return []listHintEntry{
		hintKey("Tab", "next field", simulateKeyOnFocused(tcell.KeyTab)),
		hintKey("e", "next error", func(r *Root) { r.navigateLogAuditLevel(logview.LevelError) }),
		hintKey("w", "next warning", func(r *Root) { r.navigateLogAuditLevel(logview.LevelWarn) }),
		hintKey("Enter", "details", func(r *Root) {
			row, _ := r.logAuditViewerTable.GetSelection()
			r.openLogAuditDetail(row)
		}),
		hintKey("r", "re-read files", func(r *Root) { r.reopenLogAuditViewer() }),
		hintKey("f", "toggle follow", func(r *Root) { r.toggleLogAuditFollow() }),
		hintKey("s", "statistics", func(r *Root) { r.openLogAuditStats() }),
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
	r.newLogAuditStatsScreen()
}

func (r *Root) newLogAuditSelectionScreen() {
	r.logAuditTitleBar = newPlainTitleBar("Log Holepunch — select files")
	// A clickable "✕" in the title bar's own top-right corner, per the
	// user's own explicit request — see closeTitleBarButtonCol/
	// renderCloseTitleBar/captureCloseTitleBarMouse (root.go). The
	// glyph itself is drawn by renderLogAuditSelection, every time it
	// renders (the same "render it fresh each time" shape
	// renderReloadTitleBar's own callers already use).
	r.logAuditTitleBar.SetMouseCapture(captureReloadCloseTitleBarMouse(r.logAuditTitleBar, r.reloadLogAuditDiscovery, r.closeLogAuditSelection))

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

	r.logAuditScrollbar = newVerticalScrollbarForTable(r, r.logAuditTable)
	logAuditTableRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.logAuditTable, 0, 1, true).
		AddItem(r.logAuditScrollbar, 1, 0, false)
	logAuditTableRow.SetMouseCapture(r.logAuditScrollbar.captureMouse)

	r.logAuditSelectionLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.logAuditTitleBar, 1, 0, false).
		AddItem(logAuditTableRow, 0, 1, true).
		AddItem(r.logAuditHint, 1, 0, false)
}

func (r *Root) newLogAuditViewerScreen() {
	r.logAuditViewerTitle = newPlainTitleBar("Log Holepunch")
	// Same clickable "✕" as the selection screen's own title bar (see
	// its own comment on this, just above in
	// newLogAuditSelectionScreen) — drawn by renderLogAuditViewer
	// itself, since this bar's own text is rebuilt fresh on every
	// render anyway (the running counts).
	r.logAuditViewerTitle.SetMouseCapture(captureReloadCloseTitleBarMouse(r.logAuditViewerTitle, r.reopenLogAuditViewer, r.closeLogAuditViewer))

	r.logAuditKeywordField = tview.NewInputField()
	r.logAuditKeywordField.SetLabel("Filter: ")
	// Placeholder, not a parenthetical in the label — per the user's
	// own explicit request to move every field's own example into the
	// field itself (shown only while it's actually empty), the same
	// row Time/Level's own "(e.g. ...)" labels used to carry instead.
	r.logAuditKeywordField.SetPlaceholder(`keyword, keyword1 AND (keyword2 OR keyword3)`)
	r.logAuditKeywordField.SetChangedFunc(func(string) { r.renderLogAuditViewer() })
	r.logAuditKeywordField.SetDoneFunc(func(key tcell.Key) {
		r.logAuditViewerFieldDone(key, r.logAuditTimeField, r.logAuditViewerTable)
	})
	r.logAuditKeywordField.SetFocusFunc(func() { r.restyleLogAuditFocusTargets(r.logAuditKeywordField) })

	// Time reuses filterexpr.ParseMtime verbatim (see
	// logAuditEntryVisible) — the exact same "before"/"after"/
	// "between ... and ..."/relative grammar the panel's own
	// Modified-time filter and the Action Log's own Time field
	// already accept, rather than a third date grammar for what is,
	// structurally, the same kind of question.
	r.logAuditTimeField = tview.NewInputField()
	r.logAuditTimeField.SetLabel("Time: ")
	// A third example covering "between", per the user's own explicit
	// request — filterexpr.ParseMtime's own grammar (see
	// logAuditEntryVisible's doc comment above) needs exactly two
	// moments joined by "and", unlike "last"/"after" which take just
	// one.
	r.logAuditTimeField.SetPlaceholder(`last 7 days, after 2026-09-01, between 2026-09-01 and 2026-09-30`)
	// Deliberately NOT a live SetChangedFunc the way Keyword/Level have
	// — per the user's own explicit report, re-bucketing the timeline
	// (and re-filtering the table) on every single keystroke made
	// typing into this field noticeably sluggish, unlike a keyword
	// substring match or a level-threshold compare, both cheap per
	// character. A date/time expression only means anything once it's
	// finished anyway (a half-typed "between 2026-09-0" doesn't parse),
	// so applying it on Enter/Tab/Backtab (SetDoneFunc below) or
	// whenever focus moves on to something else entirely (see
	// restyleLogAuditFocusTargets's own "focused != logAuditTimeField"
	// render) is both cheaper and more correct than live re-filtering
	// ever was here.
	r.logAuditTimeField.SetDoneFunc(func(key tcell.Key) {
		r.renderLogAuditViewer()
		r.logAuditViewerFieldDone(key, r.logAuditLevelField, r.logAuditKeywordField)
	})
	r.logAuditTimeField.SetFocusFunc(func() { r.restyleLogAuditFocusTargets(r.logAuditTimeField) })

	// Level is a *minimum* severity ("warn" shows WARN and everything
	// above it — ERROR, FATAL — not only exact WARN matches), the same
	// "t Zeitfilter"/severity-threshold idea the original mockup asked
	// for: a quick "show me the bad stuff" narrowing, not a precise
	// equality filter nobody actually wants while triaging.
	r.logAuditLevelField = tview.NewInputField()
	r.logAuditLevelField.SetLabel("Level >= ")
	r.logAuditLevelField.SetPlaceholder("warn")
	r.logAuditLevelField.SetChangedFunc(func(string) { r.renderLogAuditViewer() })
	r.logAuditLevelField.SetDoneFunc(func(key tcell.Key) {
		// next is the timeline, not the table directly — see
		// captureLogAuditTimelineKey's own doc comment on the Tab
		// chain this is now one link of.
		r.logAuditViewerFieldDone(key, r.logAuditTimelineView, r.logAuditTimeField)
	})
	r.logAuditLevelField.SetFocusFunc(func() { r.restyleLogAuditFocusTargets(r.logAuditLevelField) })

	r.logAuditFilterRow = tview.NewFlex().
		AddItem(r.logAuditKeywordField, 0, 2, true).
		AddItem(r.logAuditTimeField, 0, 2, false).
		AddItem(r.logAuditLevelField, 0, 1, false)

	r.logAuditTimelineView = tview.NewTextView()
	r.logAuditTimelineView.SetWrap(false)
	r.logAuditTimelineView.SetDynamicColors(true)
	r.logAuditTimelineView.SetInputCapture(r.captureLogAuditTimelineKey)
	r.logAuditTimelineView.SetMouseCapture(r.captureLogAuditTimelineMouse)
	// Can't route this one through restyleLogAuditFocusTargets:
	// *tview.TextView.Focus (verified directly against tview's own
	// textview.go) does "t.Lock(); defer t.Unlock()" *before* invoking
	// this very FocusFunc, and repaintLogAuditTimeline calls SetText on
	// this same TextView, which also locks it — a real, confirmed
	// self-deadlock on tview's own non-reentrant mutex, not a
	// hypothetical one (it hung TestLogAuditTimelineArrowKeysMoveAndCollapseSelection
	// outright before this was found). SetBackgroundColor is safe here
	// (Box's own override, never locks), so the focused look still
	// applies immediately; the selection highlight itself first appears
	// on whichever key or mouse event comes next — both already call
	// repaintLogAuditTimeline themselves, safely, from outside this
	// lock. restyleLogAuditFocusTargets itself stays the right function
	// for every other caller (the three fields, the table, and
	// applyLogAuditTheme's own refresh), none of which call back into
	// the same widget they're styling.
	r.logAuditTimelineView.SetFocusFunc(func() {
		// logAuditTimelineFocused = true comes FIRST, deliberately,
		// before the possible renderLogAuditViewer call below: that
		// render's own freeze-while-focused guard (renderLogAuditTimeline's
		// own doc comment) is what keeps it from calling SetText on this
		// same TextView from inside this very Focus() callback — the
		// identical self-deadlock the rest of this FocusFunc already
		// works around by skipping repaintLogAuditTimeline entirely.
		// Setting it after would let that one render slip through
		// unfrozen and hit the same lock. The table still updates
		// either way (renderLogAuditViewer's other half, a different
		// widget); only this one widget's own repaint stays deferred to
		// the next key or mouse event, same as always while frozen.
		r.logAuditTimelineFocused = true
		// Same timeFieldWasFocused check restyleLogAuditFocusTargets
		// itself does (see its own doc comment) — needed here too since
		// a mouse click can jump straight from the Time field to the
		// Timeline, skipping Level (the only stop restyleLogAuditFocusTargets
		// would otherwise have caught this transition through).
		if r.logAuditTimeFieldFocused {
			r.logAuditTimeFieldFocused = false
			r.renderLogAuditViewer()
		}
		r.logAuditTimelineView.SetBackgroundColor(r.theme.InputFocusedBackground)
		for _, f := range r.logAuditFilterFields() {
			styleInput(f, r.theme, false)
		}
		r.renderLogAuditFocusIndicator(false)
	})

	// logAuditFocusIndicator takes over the row the table's own top
	// border padding used to leave blank (padding dropped to 0 below) —
	// per the user's own explicit request to turn that previously-empty
	// space into a "the list has focus" indicator, rather than leaving
	// it an easy-to-miss cue buried in which row happens to be
	// highlighted.
	r.logAuditFocusIndicator = tview.NewTextView()
	r.logAuditFocusIndicator.SetWrap(false)

	r.logAuditViewerTable = tview.NewTable()
	r.logAuditViewerTable.SetBorders(false)
	r.logAuditViewerTable.SetBorderPadding(0, 0, 2, 1)
	r.logAuditViewerTable.SetSelectable(true, false)
	r.logAuditViewerTable.SetFixed(1, 0)
	r.logAuditViewerTable.SetInputCapture(r.captureLogAuditViewerTableKey)
	r.logAuditViewerTable.SetSelectedFunc(func(row, col int) { r.openLogAuditDetail(row) })
	r.logAuditViewerTable.SetFocusFunc(func() { r.restyleLogAuditFocusTargets(r.logAuditViewerTable) })

	r.logAuditViewerHint = tview.NewTextView()
	r.logAuditViewerHint.SetWrap(false)
	r.logAuditViewerHint.SetDynamicColors(true)
	hintText, hintSpans := buildListHint(r.theme, logAuditViewerHintEntries())
	r.logAuditViewerHint.SetText(hintText)
	r.logAuditViewerSpans = hintSpans
	r.logAuditViewerHint.SetMouseCapture(r.captureListHintMouse(r.logAuditViewerHint, &r.logAuditViewerSpans))

	r.logAuditViewerScrollbar = newVerticalScrollbarForTable(r, r.logAuditViewerTable)
	logAuditViewerTableRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.logAuditViewerTable, 0, 1, true).
		AddItem(r.logAuditViewerScrollbar, 1, 0, false)
	logAuditViewerTableRow.SetMouseCapture(r.logAuditViewerScrollbar.captureMouse)

	r.logAuditViewerLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.logAuditViewerTitle, 1, 0, false).
		AddItem(r.logAuditFilterRow, 1, 0, false).
		AddItem(r.logAuditTimelineView, 1, 0, false).
		AddItem(r.logAuditFocusIndicator, 1, 0, false).
		AddItem(logAuditViewerTableRow, 0, 1, true).
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
	title := " Log Holepunch — select files"
	if r.panel.isRemote() {
		// Named explicitly, not just "(remote)" — per this project's own
		// "a remote view must never describe local paths or metadata"
		// rule, read the other way: this screen now genuinely reads
		// through a remote connection (see currentLogAuditSource,
		// logauditsource.go), so saying so, and which one, is the
		// honest thing to show right where the directory itself is
		// already named.
		title += " — " + r.panel.remoteConn.Label()
	}
	renderReloadCloseTitleBar(r.logAuditTitleBar, title+" ", r.lastScreenWidth)
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
	header(logAuditSelSize, "Size")
	header(logAuditSelModified, "Modified")

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
		// Text, not MutedTextColor — per the user's own explicit report:
		// Name already uses Text (white), and Info/Size/Modified looked
		// inconsistently dimmer next to it for no real reason (none of
		// these three carry any "this is secondary" meaning Name
		// itself doesn't also have).
		r.logAuditTable.SetCell(row, logAuditSelInfo,
			tview.NewTableCell(logAuditGroupInfo(g)).SetTextColor(r.theme.Text).SetSelectable(true))
		sizeText, modText := "", ""
		if newest, ok := g.Newest(); ok {
			sizeText = humanSize(newest.Size)
			modText = logAuditTimeText(newest.ModTime)
		}
		r.logAuditTable.SetCell(row, logAuditSelSize,
			tview.NewTableCell(sizeText).SetTextColor(r.theme.Text).SetSelectable(true))
		r.logAuditTable.SetCell(row, logAuditSelModified,
			tview.NewTableCell(modText).SetTextColor(r.theme.Text).SetSelectable(true))
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

// logAuditKeywordQuery parses the Keyword field's own boolean query
// expression (see internal/filterexpr's ParseQuery: terms, AND/OR/NAND/
// NOR/NOT, parentheses) — ok is false for a blank field or an
// unparseable expression, the same graceful-degradation contract
// logAuditMinLevel/the Time field (logAuditEntryVisible) already
// follow: that part of the filter simply isn't applied, no error state
// shown for it. Called once per render (see renderLogAuditViewer), not
// once per entry — an expression's own parse cost is the same whether
// the family holds ten lines or several million, so compiling it
// 5.7 million times over would be pure waste on exactly the large
// remote families 19a (feature_ideas.txt) is about.
func logAuditKeywordQuery(keyword string) (filterexpr.QueryFilter, bool) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return filterexpr.QueryFilter{}, false
	}
	q, err := filterexpr.ParseQuery(keyword)
	if err != nil {
		return filterexpr.QueryFilter{}, false
	}
	return q, true
}

// logAuditMatchesQuery reports whether e's own Source or Message
// satisfies query — hasQuery false (a blank or unparseable Keyword
// field, see logAuditKeywordQuery) always matches, the same "blank
// filter shows everything" contract filterActivityLogEntries already
// has.
func logAuditMatchesQuery(e logview.Entry, query filterexpr.QueryFilter, hasQuery bool) bool {
	if !hasQuery {
		return true
	}
	return query.Match(e.Source, e.Message)
}

// logAuditMinLevel parses levelText (e.g. "warn", "ERROR") into a
// minimum-severity threshold — ok is false for blank or unparseable
// text, the same graceful-degradation contract filterActivityLogEntries
// already has for an unparseable time expression: that half of the
// filter simply isn't applied, no error state shown for it.
func logAuditMinLevel(levelText string) (logview.Level, bool) {
	levelText = strings.TrimSpace(levelText)
	if levelText == "" {
		return logview.LevelUnknown, false
	}
	level := logview.ParseLevel(levelText)
	if level == logview.LevelUnknown {
		return logview.LevelUnknown, false
	}
	return level, true
}

// logAuditEntryVisible combines all three viewer filters (AND, the
// same combination rule filterActivityLogEntries' own keyword+time
// pair already uses): a boolean query over Source/Message (see
// internal/filterexpr's ParseQuery — query/hasQuery come from
// logAuditKeywordQuery, compiled once per render, not passed as a raw
// string here), a time range via filterexpr.ParseMtime (the exact same
// grammar the Action Log's own Time field already accepts), and a
// minimum Level severity. An entry with LevelUnknown never passes a
// Level filter, even ">= trace" — "no level recognized" and "this
// severity or worse" are different claims, and the filter is
// explicitly about the latter.
func logAuditEntryVisible(e logview.Entry, query filterexpr.QueryFilter, hasQuery bool, timeExpr, levelText string, now time.Time) bool {
	if !logAuditMatchesQuery(e, query, hasQuery) {
		return false
	}
	if strings.TrimSpace(timeExpr) != "" {
		if f, err := filterexpr.ParseMtime(timeExpr, now); err == nil && !f.Match(e.Time) {
			return false
		}
	}
	if minLevel, ok := logAuditMinLevel(levelText); ok {
		if e.Level == logview.LevelUnknown || e.Level < minLevel {
			return false
		}
	}
	return true
}

// logAuditMaxRenderedRows caps how many matching entries
// renderLogAuditViewer actually builds TableCells for — a real
// /var/log/nginx/access.log family can run into the hundreds of
// thousands of lines, and rebuilding that many tview.TableCell values
// on every keystroke in the filter field would make typing feel
// sluggish long before anything else about this screen does. The
// title bar's own count still reports the true total (see
// renderLogAuditViewer) so narrowing the filter further is always the
// visible next step, not a silent truncation.
const logAuditMaxRenderedRows = 2000

// logAuditHighlight escapes text for safe use inside a tview cell (see
// panel.go's own addRow doc comment on why escaping has to happen
// before any tag is added, not after) and, if terms is non-empty,
// wraps every case-insensitive occurrence of any of them in a
// background tag (reusing nameHighlightTags — panel.go — exactly
// as-is: it already wraps an escaped string in "set background, then
// reset" tags) — matching is done against the original, unescaped text
// so the indices found are never thrown off by an escape sequence
// tview.Escape may have inserted.
//
// terms is a list, not a single keyword, since the Keyword field's own
// boolean query (internal/filterexpr's ParseQuery) can carry several
// terms at once — see QueryFilter.PositiveTerms's own doc comment for
// why only the positively-matching ones (never one under NOT/NAND/NOR)
// ever reach here. At each position, the longest term that matches
// right there wins — so a shorter term that happens to be a prefix of
// a longer one at the same spot (both given as terms) doesn't fire
// twice or split the longer match apart.
func logAuditHighlight(text string, terms []string, bg tcell.Color) string {
	lowerTerms := make([]string, 0, len(terms))
	for _, t := range terms {
		if t != "" {
			lowerTerms = append(lowerTerms, strings.ToLower(t))
		}
	}
	if len(lowerTerms) == 0 {
		return tview.Escape(text)
	}

	lower := strings.ToLower(text)
	var b strings.Builder
	rest := text
	restLower := lower
	for {
		bestIdx, bestLen := -1, 0
		for _, lt := range lowerTerms {
			idx := strings.Index(restLower, lt)
			if idx < 0 {
				continue
			}
			if bestIdx < 0 || idx < bestIdx || (idx == bestIdx && len(lt) > bestLen) {
				bestIdx, bestLen = idx, len(lt)
			}
		}
		if bestIdx < 0 {
			b.WriteString(tview.Escape(rest))
			break
		}
		b.WriteString(tview.Escape(rest[:bestIdx]))
		match := rest[bestIdx : bestIdx+bestLen]
		b.WriteString(nameHighlightTags(tview.Escape(match), bg))
		rest = rest[bestIdx+bestLen:]
		restLower = restLower[bestIdx+bestLen:]
	}
	return b.String()
}

// logAuditFilterFields lists the Keyword/Time/Level fields together,
// for restyleLogAuditFilterFields.
func (r *Root) logAuditFilterFields() []*tview.InputField {
	return []*tview.InputField{r.logAuditKeywordField, r.logAuditTimeField, r.logAuditLevelField}
}

// restyleLogAuditFocusTargets re-styles every focusable stop on this
// screen's filter row (the three fields, the timeline, and the table's
// own focus indicator) from one shared place, given whichever one
// (identified by interface identity, not type — they don't share one
// beyond tview.Primitive) actually just gained focus: highlighting
// that one and graying out every other. One function instead of each
// of the five FocusFunc callbacks separately remembering to also
// unstyle the other four — the Timeline's own addition is exactly the
// kind of thing a scattered version would have been easy to forget at
// one of the other four call sites.
//
// Works around a real, confirmed tview gap rather than a bug in our
// own BlurFunc wiring, for the three *tview.InputField targets
// specifically: NewInputField's own constructor (verified directly
// against inputfield.go) forwards its embedded TextArea's Focus event
// to the InputField's own registered FocusFunc — "// Forward focus
// event to the input field." — but never adds the matching Blur
// forwarder. A Tab-driven focus change still blurs the *InputField
// itself (Root's own code calls r.app.SetFocus on the InputField value
// directly — see logAuditViewerFieldDone), so that path's own blur
// would still fire correctly if anything were still listening for it.
// A mouse click, though, is handled by TextArea.MouseHandler (see its
// own MouseLeftDown case), which hands focus to the TextArea *value*,
// not the InputField wrapping it — so from that point on,
// Application's own a.focus.Blur() blurs the TextArea, which has no
// corresponding forwarder back out to the InputField's own BlurFunc,
// and a Blur-driven restyle would stay visually "focused" forever
// after even once real focus has long since moved elsewhere —
// confirmed directly against a tcell.SimulationScreen with
// instrumented Focus/Blur callbacks and app.GetFocus() printed
// before/after each click, not assumed from reading tview's source
// alone. This is exactly why every restyle here happens from a
// FocusFunc (the known-forwarded event) rather than any BlurFunc (the
// known-broken one for InputField) — not one of the five callers below
// registers a BlurFunc of its own at all.
func (r *Root) restyleLogAuditFocusTargets(focused tview.Primitive) {
	// timeFieldWasFocused, not r.logAuditTimeField.HasFocus(): the
	// latter is exactly the unreliable signal this function's own doc
	// comment already explains tview never updates on a mouse-driven
	// focus change away from an InputField (Blur fires on its embedded
	// TextArea, never forwarded back to the InputField's own Box) — own
	// bookkeeping, updated on every call here (which, unlike Blur, this
	// function always reliably is, driven by the next target's own
	// Focus event), is what correctly detects "the Time field just lost
	// focus" so its own debounced render (see its own SetDoneFunc
	// comment on why Time isn't live like Keyword/Level) still applies
	// when the user leaves it with a mouse click rather than Tab.
	timeFieldWasFocused := r.logAuditTimeFieldFocused
	for _, f := range r.logAuditFilterFields() {
		styleInput(f, r.theme, tview.Primitive(f) == focused)
	}
	r.logAuditTimeFieldFocused = focused == r.logAuditTimeField
	if timeFieldWasFocused && !r.logAuditTimeFieldFocused {
		r.renderLogAuditViewer()
	}
	r.logAuditTimelineFocused = focused == r.logAuditTimelineView
	background := r.theme.PopupBackground
	if r.logAuditTimelineFocused {
		background = r.theme.InputFocusedBackground
	}
	r.logAuditTimelineView.SetBackgroundColor(background)
	r.repaintLogAuditTimeline()
	r.renderLogAuditFocusIndicator(focused == r.logAuditViewerTable)
}

// logAuditCurrentlyFocused reports whichever of the five
// restyleLogAuditFocusTargets stops (three fields, the timeline, or
// the table) currently has real keyboard focus, or nil if none do
// (e.g. a different overlay entirely). Only applyLogAuditTheme needs
// this — every focus *transition* already knows which target it is
// without asking (see each SetFocusFunc above), the same "pass the
// known state in, don't re-derive it" shape renderLogAuditFocusIndicator's
// own doc comment already establishes; a theme refresh is the one case
// that isn't itself a transition and has nothing else to go on.
func (r *Root) logAuditCurrentlyFocused() tview.Primitive {
	switch {
	case r.logAuditKeywordField.HasFocus():
		return r.logAuditKeywordField
	case r.logAuditTimeField.HasFocus():
		return r.logAuditTimeField
	case r.logAuditLevelField.HasFocus():
		return r.logAuditLevelField
	case r.logAuditTimelineView.HasFocus():
		return r.logAuditTimelineView
	case r.logAuditViewerTable.HasFocus():
		return r.logAuditViewerTable
	}
	return nil
}

// renderLogAuditFocusIndicator paints the row itself in
// theme.InputFocusedBackground while focused is true (logAuditViewer-
// Table itself has keyboard focus), theme.SurfaceBackground otherwise
// — the user's own explicit request for a visible cue that Tab-cycling
// has landed back on the list (as opposed to one of the three filter
// fields above it), rather than only being inferable from which row
// happens to be highlighted. No text of its own (an earlier "● List"
// label was dropped per the user's own explicit, later report that it
// added nothing the background color alone doesn't already say).
// Takes focused explicitly rather than querying logAuditViewerTable.
// HasFocus() itself: tview.Box.Blur() (verified directly against its
// own box.go) runs the blur callback *before* flipping its own
// hasFocus flag, so a blur-triggered call here would otherwise still
// see focus as true — the exact same "pass the known state in, don't
// re-derive it" shape styleInput's own FocusFunc/BlurFunc pairs
// already use elsewhere in this file. Called from the table's own
// SetFocusFunc/SetBlurFunc (see newLogAuditViewerScreen) and once from
// applyLogAuditTheme (passing the table's own current, settled
// HasFocus — safe there, since that call never happens from inside a
// Blur callback) so a live theme switch repaints it in the right
// color either way.
func (r *Root) renderLogAuditFocusIndicator(focused bool) {
	// PanelBackground, not SurfaceBackground — matches the table's own
	// new background (see logAuditViewerTable's own comment) so the
	// indicator still blends into the table it's a focus cue for while
	// unfocused.
	background := r.theme.PanelBackground
	if focused {
		background = r.theme.InputFocusedBackground
	}
	r.logAuditFocusIndicator.SetBackgroundColor(background)
}

// renderLogAuditViewer fills the viewer table with every entry
// matching the keyword field, Time/Level/Source/Message columns, the
// matched keyword highlighted in Source/Message via logAuditHighlight
// — and updates the title bar with the running counts the original
// conversation's own mockup called for ("4,812 events │ 3 files │ 2
// compressed │ 1 error"). The Time column header is clickable (see
// above) and toggles logAuditNewestFirst, the same "click to sort,
// click again to reverse" convention the panel's own column headers
// already use (see sortArrow, panel.go) — applied here by reversing
// shown before logAuditMaxRenderedRows truncates it, so a truncated
// view always keeps the chronological end that direction actually
// means to show (the newest entries, not whichever happened to come
// first in file order) rather than silently biasing toward one end
// regardless of what the arrow says.
func (r *Root) renderLogAuditViewer() {
	r.logAuditViewerTable.Clear()

	header := func(col int, text string) {
		r.logAuditViewerTable.SetCell(0, col,
			tview.NewTableCell(text).
				SetTextColor(r.theme.Text).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false))
	}
	timeLabel := padRight("Time"+sortArrow(r.logAuditNewestFirst), 19)
	r.logAuditViewerTable.SetCell(0, logAuditColTime,
		tview.NewTableCell(timeLabel).
			SetTextColor(r.theme.Text).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false).
			SetClickedFunc(func() bool {
				r.logAuditNewestFirst = !r.logAuditNewestFirst
				r.renderLogAuditViewer()
				return false
			}))
	header(logAuditColLevel, padRight("Level", 5))
	header(logAuditColSource, "Source")
	header(logAuditColMessage, "Message")

	keyword := r.logAuditKeywordField.GetText()
	query, hasQuery := logAuditKeywordQuery(keyword)
	timeExpr := r.logAuditTimeField.GetText()
	levelText := r.logAuditLevelField.GetText()
	now := time.Now()

	var highlightTerms []string
	if hasQuery {
		highlightTerms = query.PositiveTerms()
	}

	var shown []logview.Entry
	errCount, warnCount := 0, 0
	for _, e := range r.logAuditAllEntries {
		switch e.Level {
		case logview.LevelError, logview.LevelFatal:
			errCount++
		case logview.LevelWarn:
			warnCount++
		}
		if logAuditEntryVisible(e, query, hasQuery, timeExpr, levelText, now) {
			shown = append(shown, e)
		}
	}

	title := fmt.Sprintf(" Log Holepunch — %d events │ %d files │ %d skipped │ %d errors │ %d warnings",
		len(r.logAuditAllEntries), r.logAuditFiles, r.logAuditSkipped, errCount, warnCount)
	if len(shown) != len(r.logAuditAllEntries) {
		title += fmt.Sprintf(" │ %d matching", len(shown))
	}
	if len(shown) > logAuditMaxRenderedRows {
		// Without this, a file past the cap silently showed only a
		// fraction of what the counts above claim — a real, user-
		// reported point of confusion ("es werden doch auch alle logs
		// ... richtig angezeigt", i.e. the file counts looked complete,
		// so the missing rows read as a bug rather than a documented
		// limit).
		title += fmt.Sprintf(" │ showing first %d (of %d — narrow the filter to see the rest)", logAuditMaxRenderedRows, len(shown))
	}
	if r.logAuditFollowing {
		title += " │ ● following"
	}
	// A parse error only blocks the whole view when there is nothing
	// else to show — readLogAuditGroup/logaudit.go keeps every entry
	// that DID parse from a group's other files even when one of them
	// failed (a corrupted .gz, a permission error on an older
	// rotation, ...), so showing just the error page here used to
	// silently hide all of those too, a real, independently-discovered
	// bug directly on point for "nicht alle Logeinträge sichtbar": an
	// unrelated problem with one rotation of a file could erase every
	// entry from the whole family, successfully-read ones included.
	if r.logAuditParseErr != nil && len(r.logAuditAllEntries) > 0 {
		title += fmt.Sprintf(" │ 1 file failed: %s", r.logAuditParseErr.Error())
	}
	// One general note instead of repeating it on every affected hint
	// — see logAuditViewerHintEntries' own doc comment for why.
	title += " │ e/w/Enter/r/f/s need the list focused"
	renderReloadCloseTitleBar(r.logAuditViewerTitle, title+" ", r.lastScreenWidth)

	r.renderLogAuditTimeline(shown)

	if r.logAuditParseErr != nil && len(r.logAuditAllEntries) == 0 {
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

	// logAuditNewestFirst reverses the table's own order, not shown
	// itself — renderLogAuditTimeline above already ran against the
	// real chronological order, which its own density strip always
	// reads left-to-right regardless of which way the table currently
	// sorts.
	rendered := shown
	if r.logAuditNewestFirst {
		rendered = slices.Clone(rendered)
		slices.Reverse(rendered)
	}
	// logAuditMaxRenderedRows caps the table itself, not the counts
	// above or the timeline (both still reflect every matching entry)
	// — see its own doc comment for why. Truncating after the reverse
	// above, not before: the first logAuditMaxRenderedRows of whichever
	// order is currently showing is always the relevant end to keep —
	// the newest entries when sorted newest-first, not whichever
	// happened to come first in file order regardless of the arrow.
	if len(rendered) > logAuditMaxRenderedRows {
		rendered = rendered[:logAuditMaxRenderedRows]
	}

	enableTableSelection(r.logAuditViewerTable)
	bg := r.theme.ButtonBackground
	for i, e := range rendered {
		row := i + 1
		levelColor := logAuditLevelColor(r.theme, e.Level)
		cell := func(col int, text string, color tcell.Color) {
			r.logAuditViewerTable.SetCell(row, col,
				tview.NewTableCell(text).SetTextColor(color).SetSelectable(true))
		}
		cell(logAuditColTime, padRight(logAuditTimeText(e.Time), 19), r.theme.Text)
		cell(logAuditColLevel, padRight(e.Level.String(), 5), levelColor)
		cell(logAuditColSource, logAuditHighlight(e.Source, highlightTerms, bg), r.theme.Text)
		cell(logAuditColMessage, logAuditHighlight(e.Message, highlightTerms, bg), r.theme.Text)
		r.logAuditViewerTable.GetCell(row, logAuditColTime).SetReference(e)
	}

	if cur, _ := r.logAuditViewerTable.GetSelection(); cur < 1 || cur > len(rendered) {
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

	query, hasQuery := logAuditKeywordQuery(r.logAuditKeywordField.GetText())
	var highlightTerms []string
	if hasQuery {
		highlightTerms = query.PositiveTerms()
	}
	bg := r.theme.ButtonBackground
	header := fmt.Sprintf("%s  %s  %s\n", logAuditTimeText(e.Time), e.Level.String(), e.File)
	body := strings.Join(wrapText(e.Message, width), "\n")
	full := header + body

	// Raw only earns its own section when it actually differs from
	// Message — identical for FormatPlain (see Entry.Raw's own doc
	// comment), where showing it twice would be noise, not help.
	if e.Raw != e.Message {
		rawBody := strings.Join(wrapText(e.Raw, width), "\n")
		full += "\n\nRaw:\n" + rawBody
	}

	text := tview.Escape(header) + logAuditHighlight(full[len(header):], highlightTerms, bg)
	r.logAuditDetailView.SetText(text)
	r.logAuditDetailTitleBar.SetText(" Log entry ")

	textWidth, textHeight := textSize(full)
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

// logAuditViewerFieldDone is each filter field's own SetDoneFunc — the
// same shape activityLogFieldDone already establishes for Activity
// Log's own keyword/time pair, just cycling through three fields
// (Keyword → Time → Level → table) instead of two: Tab/Enter moves to
// next, Backtab to prev, Escape closes the viewer regardless of which
// field it was pressed in.
func (r *Root) logAuditViewerFieldDone(key tcell.Key, next, prev tview.Primitive) {
	switch key {
	case tcell.KeyEscape:
		r.closeLogAuditViewer()
	case tcell.KeyTab, tcell.KeyEnter:
		r.app.SetFocus(next)
	case tcell.KeyBacktab:
		r.app.SetFocus(prev)
	}
}

// captureLogAuditViewerTableKey: Tab returns focus to the Keyword
// field (restarting the Keyword→Time→Level→Timeline→Table cycle),
// Backtab to the Timeline (the stop right before the table in that
// cycle — see captureLogAuditTimelineKey) — "e"/"w" jump to the next
// error/warning, "r" re-reads the same files, "f" toggles follow, "s"
// opens the Statistics modal, Escape closes the viewer (back to the
// selection screen, see closeLogAuditViewer/pushOverlay).
func (r *Root) captureLogAuditViewerTableKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeLogAuditViewer()
		return nil
	}
	if event.Key() == tcell.KeyTab {
		r.app.SetFocus(r.logAuditKeywordField)
		return nil
	}
	if event.Key() == tcell.KeyBacktab {
		r.app.SetFocus(r.logAuditTimelineView)
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
		case 'f':
			r.toggleLogAuditFollow()
			return nil
		case 's':
			r.openLogAuditStats()
			return nil
		}
	}
	return event
}

// captureLogAuditTimelineKey handles keyboard interaction once the
// Timeline itself holds focus: Left/Right move the cursor one bucket
// and collapse the selection (anchor = cursor); Shift+Left/Right
// extend it — leaving the anchor exactly where it already was, never
// re-seeding it on "the first Shift press", since every *plain* move
// already keeps anchor and cursor equal, so whatever anchor was
// holding from the last plain move is already the correct start point
// the moment a Shift sequence begins (see moveLogAuditTimelineCursor).
// Enter/Space commit the current selection (single bucket or range)
// into the Time field, the same as a mouse release after a drag (see
// commitLogAuditTimelineSelection). Tab/Backtab move on to the table/
// Level field respectively — logAuditTimelineView is a
// *tview.TextView, not an *tview.InputField, so neither key routes
// through SetDoneFunc the way the three fields above it do; this is
// the only place either is handled at all for this widget. Escape
// closes the whole viewer, matching every other stop on this screen.
func (r *Root) captureLogAuditTimelineKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape:
		r.closeLogAuditViewer()
		return nil
	case tcell.KeyTab:
		r.app.SetFocus(r.logAuditViewerTable)
		return nil
	case tcell.KeyBacktab:
		r.app.SetFocus(r.logAuditLevelField)
		return nil
	case tcell.KeyEnter:
		r.commitLogAuditTimelineSelection()
		return nil
	case tcell.KeyLeft:
		r.moveLogAuditTimelineCursor(-1, event.Modifiers()&tcell.ModShift != 0)
		return nil
	case tcell.KeyRight:
		r.moveLogAuditTimelineCursor(1, event.Modifiers()&tcell.ModShift != 0)
		return nil
	case tcell.KeyRune:
		if event.Rune() == ' ' {
			r.commitLogAuditTimelineSelection()
			return nil
		}
	}
	return event
}

// moveLogAuditTimelineCursor moves the cursor by delta (±1 bucket),
// clamped to [0, bucketCount-1]. extend=false (a plain arrow)
// collapses the selection onto the new position (anchor = cursor);
// extend=true (Shift held) leaves the anchor untouched, so repeated
// Shift+arrow presses keep extending the same range from the same
// starting point.
func (r *Root) moveLogAuditTimelineCursor(delta int, extend bool) {
	n := r.logAuditTimelineBucketCount
	if n == 0 {
		return
	}
	c := r.logAuditTimelineCursor + delta
	if c < 0 {
		c = 0
	}
	if c >= n {
		c = n - 1
	}
	r.logAuditTimelineCursor = c
	if !extend {
		r.logAuditTimelineAnchor = c
	}
	r.repaintLogAuditTimeline()
}

// logAuditTimelineBucketAt maps an absolute screen column (as a mouse
// event reports it) to a bucket index, using the bar's own column
// logAuditTimelineLayout actually computed at the last unfrozen render
// (see logAuditTimelineBarCol's own doc comment on root.go) — never
// re-derived from the rendered string itself. ok is false for a column
// before the bar's own start, past its end, or while there's nothing
// to select at all (bucketCount == 0, e.g. the "no meaningful span"
// case).
func (r *Root) logAuditTimelineBucketAt(x int) (bucket int, ok bool) {
	if r.logAuditTimelineBucketCount == 0 {
		return 0, false
	}
	viewX, _, _, _ := r.logAuditTimelineView.GetInnerRect()
	col := x - viewX - r.logAuditTimelineBarCol
	if col < 0 || col >= r.logAuditTimelineBucketCount {
		return 0, false
	}
	return col, true
}

// captureLogAuditTimelineMouse drives the Timeline's own click/drag
// selection. MouseLeftDown on a bucket gives the Timeline real
// keyboard focus (a click alone doesn't otherwise move it there — the
// same gap dragSearchMouseCapture's own click-to-focus handling closes
// for the Find dialog) and starts a new single-bucket selection.
// MouseMove while a drag is in progress only moves the cursor and
// repaints the highlight — no commit, no re-bucketing, so the
// selection can be dragged leftward just as freely as rightward and
// the timeline never shifts under the cursor mid-drag (see
// logAuditTimelineDragging's own doc comment on root.go). MouseLeftUp,
// or a stray MouseMove that arrives with the button already released
// (the drag ended somewhere this capture never saw the button go up,
// e.g. outside the terminal's own reported bounds), both commit —
// mirroring Enter/Space — via commitLogAuditTimelineSelection, which
// is also what turns logAuditTimelineDragging back off.
func (r *Root) captureLogAuditTimelineMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	x, y := event.Position()

	if r.logAuditTimelineDragging {
		switch action {
		case tview.MouseMove:
			if event.Buttons()&tcell.ButtonPrimary == 0 {
				r.commitLogAuditTimelineSelection()
				return tview.MouseConsumed, nil
			}
			if b, ok := r.logAuditTimelineBucketAt(x); ok {
				r.logAuditTimelineCursor = b
				r.repaintLogAuditTimeline()
			}
			return tview.MouseConsumed, nil
		case tview.MouseLeftUp:
			r.commitLogAuditTimelineSelection()
			return tview.MouseConsumed, nil
		}
		return tview.MouseConsumed, nil // swallow everything else for the duration of the drag
	}

	if action == tview.MouseLeftDown {
		_, viewY, _, _ := r.logAuditTimelineView.GetInnerRect()
		if y != viewY {
			return action, event
		}
		b, ok := r.logAuditTimelineBucketAt(x)
		if !ok {
			return action, event
		}
		r.app.SetFocus(r.logAuditTimelineView)
		r.logAuditTimelineDragging = true
		r.logAuditTimelineAnchor = b
		r.logAuditTimelineCursor = b
		r.repaintLogAuditTimeline()
		return tview.MouseConsumed, nil
	}

	return action, event
}

// commitLogAuditTimelineSelection writes the current selection
// ([min(anchor,cursor), max(anchor,cursor)], always direction-
// independent regardless of which direction it was built in) into the
// Time field
// as "between <von> and <bis>" — the exact grammar
// filterexpr.ParseMtime already accepts elsewhere on this screen (see
// logAuditTimeField's own placeholder), with both ends floored/ceiled
// to the whole second: ParseMtime's own "between" is inclusive on both
// ends already (!t.Before(start) && !t.After(end)), but a bucket
// boundary can fall mid-second while every real timestamp here is
// second-granular, so rounding outward is what actually guarantees no
// entry at the edge is excluded regardless.
//
// Moves focus to the Time field *before* setting its text, not after:
// that fires restyleLogAuditFocusTargets (clearing logAuditTimelineFocused)
// before the explicit renderLogAuditViewer call below runs, so the
// timeline's own freeze-while-focused check (renderLogAuditTimeline)
// is already lifted by the time the newly-narrowed range needs to
// re-bucket and zoom in — a commit must never be blocked by the same
// freeze that protects an in-progress selection from follow mode.
//
// Renders immediately, unlike ordinary typing into this same field
// (see its own SetDoneFunc comment on why Time isn't live) — per the
// user's own explicit report that having to press Enter a second time
// after an already-deliberate mouse/keyboard selection was pure
// friction: a commit is already the equivalent of "done editing", the
// same one-shot action Enter itself is, not a partial keystroke.
func (r *Root) commitLogAuditTimelineSelection() {
	r.logAuditTimelineDragging = false
	if r.logAuditTimelineBucketCount == 0 {
		return
	}
	start := min(r.logAuditTimelineAnchor, r.logAuditTimelineCursor)
	end := max(r.logAuditTimelineAnchor, r.logAuditTimelineCursor)
	from, _ := logAuditBucketTimeRange(r.logAuditTimelineMinT, r.logAuditTimelineMaxT, r.logAuditTimelineBucketCount, start)
	_, to := logAuditBucketTimeRange(r.logAuditTimelineMinT, r.logAuditTimelineMaxT, r.logAuditTimelineBucketCount, end)
	from = from.Truncate(time.Second)
	if toFloor := to.Truncate(time.Second); !toFloor.Equal(to) {
		to = toFloor.Add(time.Second)
	} else {
		to = toFloor
	}

	r.app.SetFocus(r.logAuditTimeField)
	r.logAuditTimeField.SetText(fmt.Sprintf("between %s and %s",
		from.Format("2006-01-02 15:04:05"), to.Format("2006-01-02 15:04:05")))
	r.renderLogAuditViewer()
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
	// PanelBackground, not SurfaceBackground — see mountsLayout's own
	// identical comment (mounts.go) for the full reasoning.
	r.logAuditSelectionLayout.SetBackgroundColor(theme.PanelBackground)
	r.logAuditTable.SetBackgroundColor(theme.PanelBackground)
	// Without this, tview's own default selected-cell style just
	// reverses each cell's own foreground into a background instead of
	// using one consistent selection bar — the same fix already applied
	// to sshKeysTable/connectionMenuTable/sedPreviewTable/tabSwitcher/
	// gitBreach's tables, for the identical reason.
	r.logAuditTable.SetSelectedStyle(tcell.StyleDefault.
		Background(theme.SelectionBackground).
		Foreground(theme.TextColor))
	r.logAuditTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.logAuditTitleBar.SetTextColor(theme.TextColor)
	// SurfaceBackground/TextColor, matching the main screen's own
	// buttonBar/statusBar — see mountsHint's own identical comment.
	r.logAuditHint.SetBackgroundColor(theme.SurfaceBackground)
	r.logAuditHint.SetTextColor(theme.TextColor)
	hintText, hintSpans := buildListHint(theme, logAuditSelectionHintEntries())
	r.logAuditHint.SetText(hintText)
	r.logAuditHintSpans = hintSpans
	r.renderLogAuditSelection() // cell colors are baked in per cell, not looked up live at draw time

	// PanelBackground, not SurfaceBackground — see mountsLayout's own
	// identical comment (mounts.go) for the full reasoning.
	r.logAuditViewerLayout.SetBackgroundColor(theme.PanelBackground)
	r.logAuditViewerTable.SetBackgroundColor(theme.PanelBackground)
	// Same fix as logAuditTable just above, for the same reason.
	r.logAuditViewerTable.SetSelectedStyle(tcell.StyleDefault.
		Background(theme.SelectionBackground).
		Foreground(theme.TextColor))
	r.logAuditViewerTitle.SetBackgroundColor(theme.InputFocusedBackground)
	r.logAuditViewerTitle.SetTextColor(theme.TextColor)
	// logAuditFilterRow themed explicitly to the same PanelBackground
	// the table below it now uses, rather than left at tview's own
	// default — see logAuditFilterRow's own doc comment (root.go) for
	// the visual mismatch this fixes.
	r.logAuditFilterRow.SetBackgroundColor(theme.PanelBackground)
	// logAuditTimelineView deliberately does NOT match SurfaceBackground
	// while unfocused — per the user's own explicit report, a density
	// strip that reads as part of the same surface as the list below it
	// is confusing; it needs its own, visibly different tone.
	// PopupBackground is reused here rather than a plain hardcoded
	// color: it is already this app's own "distinct secondary surface"
	// role (the Detail/Statistics modals both use it — see
	// applyLogAuditTheme below), and darker than SurfaceBackground in
	// the default scheme, fixing the user's second report ("zu hell")
	// in the same change. Focused/unfocused styling for all five stops
	// (three fields, the timeline, the table's own focus indicator) now
	// goes through one shared place — restyleLogAuditFocusTargets — fed
	// here with whichever one actually has focus right now, the one
	// case that isn't itself a focus transition and so has nothing else
	// to pass in (see logAuditCurrentlyFocused's own doc comment).
	r.restyleLogAuditFocusTargets(r.logAuditCurrentlyFocused())
	// SurfaceBackground/TextColor, matching the main screen's own
	// buttonBar/statusBar — see mountsHint's own identical comment.
	r.logAuditViewerHint.SetBackgroundColor(theme.SurfaceBackground)
	r.logAuditViewerHint.SetTextColor(theme.TextColor)
	viewerHintText, viewerHintSpans := buildListHint(theme, logAuditViewerHintEntries())
	r.logAuditViewerHint.SetText(viewerHintText)
	r.logAuditViewerSpans = viewerHintSpans
	r.renderLogAuditViewer()

	r.logAuditDetailLayout.SetBackgroundColor(theme.PopupBackground)
	r.logAuditDetailView.SetBackgroundColor(theme.PopupBackground)
	r.logAuditDetailView.SetTextColor(theme.TextColor)
	r.logAuditDetailTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.logAuditDetailTitleBar.SetTextColor(theme.TextColor)

	r.applyLogAuditStatsTheme(theme)
}
