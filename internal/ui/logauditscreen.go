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
	r.logAuditTitleBar = newPlainTitleBar("Log Audit — select files")
	// A clickable "✕" in the title bar's own top-right corner, per the
	// user's own explicit request — see closeTitleBarButtonCol/
	// renderCloseTitleBar/captureCloseTitleBarMouse (root.go). The
	// glyph itself is drawn by renderLogAuditSelection, every time it
	// renders (the same "render it fresh each time" shape
	// renderReloadTitleBar's own callers already use).
	r.logAuditTitleBar.SetMouseCapture(captureCloseTitleBarMouse(r.logAuditTitleBar, r.closeLogAuditSelection))

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
	// Same clickable "✕" as the selection screen's own title bar (see
	// its own comment on this, just above in
	// newLogAuditSelectionScreen) — drawn by renderLogAuditViewer
	// itself, since this bar's own text is rebuilt fresh on every
	// render anyway (the running counts).
	r.logAuditViewerTitle.SetMouseCapture(captureCloseTitleBarMouse(r.logAuditViewerTitle, r.closeLogAuditViewer))

	r.logAuditKeywordField = tview.NewInputField()
	r.logAuditKeywordField.SetLabel("Filter: ")
	// Placeholder, not a parenthetical in the label — per the user's
	// own explicit request to move every field's own example into the
	// field itself (shown only while it's actually empty), the same
	// row Time/Level's own "(e.g. ...)" labels used to carry instead.
	r.logAuditKeywordField.SetPlaceholder("keyword")
	r.logAuditKeywordField.SetChangedFunc(func(string) { r.renderLogAuditViewer() })
	r.logAuditKeywordField.SetDoneFunc(func(key tcell.Key) {
		r.logAuditViewerFieldDone(key, r.logAuditTimeField, r.logAuditViewerTable)
	})
	r.logAuditKeywordField.SetFocusFunc(func() { r.restyleLogAuditFilterFields(r.logAuditKeywordField) })
	r.logAuditKeywordField.SetBlurFunc(func() { styleInput(r.logAuditKeywordField, r.theme, false) })

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
	r.logAuditTimeField.SetChangedFunc(func(string) { r.renderLogAuditViewer() })
	r.logAuditTimeField.SetDoneFunc(func(key tcell.Key) {
		r.logAuditViewerFieldDone(key, r.logAuditLevelField, r.logAuditKeywordField)
	})
	r.logAuditTimeField.SetFocusFunc(func() { r.restyleLogAuditFilterFields(r.logAuditTimeField) })
	r.logAuditTimeField.SetBlurFunc(func() { styleInput(r.logAuditTimeField, r.theme, false) })

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
		r.logAuditViewerFieldDone(key, r.logAuditViewerTable, r.logAuditTimeField)
	})
	r.logAuditLevelField.SetFocusFunc(func() { r.restyleLogAuditFilterFields(r.logAuditLevelField) })
	r.logAuditLevelField.SetBlurFunc(func() { styleInput(r.logAuditLevelField, r.theme, false) })

	r.logAuditFilterRow = tview.NewFlex().
		AddItem(r.logAuditKeywordField, 0, 2, true).
		AddItem(r.logAuditTimeField, 0, 2, false).
		AddItem(r.logAuditLevelField, 0, 1, false)

	r.logAuditTimelineView = tview.NewTextView()
	r.logAuditTimelineView.SetWrap(false)
	r.logAuditTimelineView.SetDynamicColors(true)

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
	r.logAuditViewerTable.SetFocusFunc(func() {
		r.renderLogAuditFocusIndicator(true)
		r.restyleLogAuditFilterFields(nil)
	})
	r.logAuditViewerTable.SetBlurFunc(func() { r.renderLogAuditFocusIndicator(false) })

	r.logAuditViewerHint = tview.NewTextView()
	r.logAuditViewerHint.SetWrap(false)
	r.logAuditViewerHint.SetDynamicColors(true)
	hintText, hintSpans := buildListHint(r.theme, logAuditViewerHintEntries())
	r.logAuditViewerHint.SetText(hintText)
	r.logAuditViewerSpans = hintSpans
	r.logAuditViewerHint.SetMouseCapture(r.captureListHintMouse(r.logAuditViewerHint, &r.logAuditViewerSpans))

	r.logAuditViewerLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.logAuditViewerTitle, 1, 0, false).
		AddItem(r.logAuditFilterRow, 1, 0, false).
		AddItem(r.logAuditTimelineView, 1, 0, false).
		AddItem(r.logAuditFocusIndicator, 1, 0, false).
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
	title := " Log Audit — select files"
	if r.panel.isRemote() {
		// Named explicitly, not just "(remote)" — per CLAUDE.md's own
		// "remote views must never describe local paths or metadata"
		// rule: this screen now genuinely reads through a remote
		// connection (see currentLogAuditSource, logauditsource.go),
		// so saying so, and which one, is the honest thing to show
		// right where the directory itself is already named.
		title += " — " + r.panel.remoteConn.Label()
	}
	renderCloseTitleBar(r.logAuditTitleBar, title+" ", r.lastScreenWidth)
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
// pair already uses): keyword over Source/Message, a time range via
// filterexpr.ParseMtime (the exact same grammar the Action Log's own
// Time field already accepts), and a minimum Level severity. An entry
// with LevelUnknown never passes a Level filter, even ">= trace" —
// "no level recognized" and "this severity or worse" are different
// claims, and the filter is explicitly about the latter.
func logAuditEntryVisible(e logview.Entry, keyword, timeExpr, levelText string, now time.Time) bool {
	if !logAuditMatchesKeyword(e, keyword) {
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

// logAuditFilterFields lists the Keyword/Time/Level fields together,
// for restyleLogAuditFilterFields.
func (r *Root) logAuditFilterFields() []*tview.InputField {
	return []*tview.InputField{r.logAuditKeywordField, r.logAuditTimeField, r.logAuditLevelField}
}

// restyleLogAuditFilterFields re-styles every Log Audit filter field,
// highlighting only focused (styleInput's own InputFocusedBackground)
// and graying out every other one (InputBackground) — nil highlights
// none, used when focus moves to the list instead of any field.
//
// Works around a real, confirmed tview gap rather than a bug in our
// own BlurFunc wiring: NewInputField's own constructor (verified
// directly against inputfield.go) forwards its embedded TextArea's
// Focus event to the InputField's own registered FocusFunc —
// "// Forward focus event to the input field." — but never adds the
// matching Blur forwarder. A Tab-driven focus change still blurs the
// *InputField itself (Root's own code calls r.app.SetFocus on the
// InputField value directly — see logAuditViewerFieldDone), so that
// path's own SetBlurFunc call still fires correctly. A mouse click,
// though, is handled by TextArea.MouseHandler (see its own
// MouseLeftDown case), which hands focus to the TextArea *value*, not
// the InputField wrapping it — so from that point on, Application's
// own a.focus.Blur() blurs the TextArea, which has no corresponding
// forwarder back out to the InputField's own BlurFunc, and the field
// stays visually "focused" forever after even once real focus has
// long since moved elsewhere — confirmed directly against a
// tcell.SimulationScreen with instrumented Focus/Blur callbacks and
// app.GetFocus() printed before/after each click, not assumed from
// reading tview's source alone. Reusing styleInput's existing Blur
// path as the single source of "which look applies" means a user-
// visible freshness check (the known-forwarded Focus event) rather
// than depending on the known-broken Blur chain is what each field's
// own FocusFunc now calls instead of styleInput directly.
func (r *Root) restyleLogAuditFilterFields(focused *tview.InputField) {
	for _, f := range r.logAuditFilterFields() {
		styleInput(f, r.theme, f == focused)
	}
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
	background := r.theme.SurfaceBackground
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
	timeExpr := r.logAuditTimeField.GetText()
	levelText := r.logAuditLevelField.GetText()
	now := time.Now()

	var shown []logview.Entry
	errCount, warnCount := 0, 0
	for _, e := range r.logAuditAllEntries {
		switch e.Level {
		case logview.LevelError, logview.LevelFatal:
			errCount++
		case logview.LevelWarn:
			warnCount++
		}
		if logAuditEntryVisible(e, keyword, timeExpr, levelText, now) {
			shown = append(shown, e)
		}
	}

	title := fmt.Sprintf(" Log Audit — %d events │ %d files │ %d skipped │ %d errors │ %d warnings",
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
	renderCloseTitleBar(r.logAuditViewerTitle, title+" ", r.lastScreenWidth)

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
		cell(logAuditColSource, logAuditHighlight(e.Source, keyword, bg), r.theme.Text)
		cell(logAuditColMessage, logAuditHighlight(e.Message, keyword, bg), r.theme.Text)
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

	keyword := r.logAuditKeywordField.GetText()
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

	text := tview.Escape(header) + logAuditHighlight(full[len(header):], keyword, bg)
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
// field (restarting the three-field cycle), Backtab to Level (the
// field right before the table in that cycle) — "e"/"w" jump to the
// next error/warning, "r" re-reads the same files, "f" toggles follow,
// "s" opens the Statistics modal, Escape closes the viewer (back to
// the selection screen, see closeLogAuditViewer/pushOverlay).
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
		r.app.SetFocus(r.logAuditLevelField)
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
	// logAuditFilterRow themed explicitly to the same SurfaceBackground
	// the table below it uses, rather than left at tview's own default
	// — see logAuditFilterRow's own doc comment (root.go) for the
	// visual mismatch this fixes.
	r.logAuditFilterRow.SetBackgroundColor(theme.SurfaceBackground)
	// logAuditTimelineView deliberately does NOT match SurfaceBackground
	// — per the user's own explicit report, a density strip that reads
	// as part of the same surface as the list below it is confusing;
	// it needs its own, visibly different tone. PopupBackground is
	// reused here rather than a plain hardcoded color: it is already
	// this app's own "distinct secondary surface" role (the Detail/
	// Statistics modals both use it — see applyLogAuditTheme below),
	// and darker than SurfaceBackground in the default scheme, fixing
	// the user's second report ("zu hell") in the same change.
	r.logAuditTimelineView.SetBackgroundColor(theme.PopupBackground)
	// logAuditFocusIndicator's own background comes entirely from
	// renderLogAuditFocusIndicator (SurfaceBackground or
	// InputFocusedBackground, depending on the list's current focus) —
	// no separate line needed here.
	r.renderLogAuditFocusIndicator(r.logAuditViewerTable.HasFocus())
	// Label color and background both come from styleInput itself —
	// see its own doc comment (theme.go) on the background mismatch
	// this used to leave in place.
	styleInput(r.logAuditKeywordField, theme, r.logAuditKeywordField.HasFocus())
	styleInput(r.logAuditTimeField, theme, r.logAuditTimeField.HasFocus())
	styleInput(r.logAuditLevelField, theme, r.logAuditLevelField.HasFocus())
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

	r.applyLogAuditStatsTheme(theme)
}
