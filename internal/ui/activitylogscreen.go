package ui

import (
	"bufio"
	"os"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/filterexpr"
)

// The Activity Log screen ("jl"): a fifth full-screen, read-only catalog
// alongside Options/Toolbox/Mounts/Firewall — Stufe 3 of the activity
// log plan (see feature_ideas.txt), browsing the real log file
// internal/activitylog already writes rather than keeping a second,
// parallel record of it just for this screen. Reads it fresh via
// activitylog.ParseLine on every open and every "r", the same
// "reflect the real, current state" reasoning openMounts/openFirewall's
// own doc comments already give — a background Rsync/Compress job can
// still be logging while this screen is open.
//
// Keyword (full-text over the message) and time-range filtering narrow
// the listing live as you type, the same feel the panel's own filter
// dropdown already has — the time expression reuses filterexpr.ParseMtime
// verbatim (the same "before"/"after"/"between ... and ..."/relative
// syntax the panel's own Modified-time filter already accepts) rather
// than inventing a second date grammar for what is, structurally, the
// exact same kind of question.

const activityLogPage = "activitylog"

const (
	activityLogColTime = iota
	activityLogColLevel
	activityLogColCategory
	activityLogColMessage
)

// activityLogColumnWidth is each column's own padding floor — never a
// ceiling, the same reasoning mountsColumnWidth/firewallColumnWidth's
// own doc comments give. Message has no floor: nothing follows it.
func activityLogColumnWidth(col int) int {
	switch col {
	case activityLogColTime:
		return 19 // "2026-09-24 15:04:05" — see activityLogTimeText
	case activityLogColLevel:
		return 8 // "detailed" is the longest Level.String()
	case activityLogColCategory:
		return 11 // "permissions" is the longest Category
	default:
		return 0
	}
}

// activityLogHintEntries is this screen's own bottom hint bar (see
// buildListHint) — used both at construction and by applyTheme. A
// function, not a package-level var: a var whose own initializer
// closes over Root methods creates a real initialization cycle the
// moment any of those methods' own call graphs reaches back into this
// package (see optionCategories' own identical reasoning).
func activityLogHintEntries() []listHintEntry {
	return []listHintEntry{
		hintKey("Tab", "next field", simulateKeyOnFocused(tcell.KeyTab)),
		hintKey("r", "refresh (while the list has focus)", func(r *Root) { r.reloadActivityLog() }),
		hintKey("Esc", "close", func(r *Root) { r.closeActivityLog() }),
	}
}

// newActivityLogScreen builds the whole screen once, at startup — the
// same build-once/repopulate-on-open shape newMountsScreen/
// newFirewallScreen already establish.
func (r *Root) newActivityLogScreen() {
	r.activityLogTitleBar = newPlainTitleBar("Activity Log")

	r.activityLogKeywordField = tview.NewInputField()
	r.activityLogKeywordField.SetLabel("Keyword: ")
	r.activityLogKeywordField.SetChangedFunc(func(string) { r.renderActivityLog() })
	r.activityLogKeywordField.SetDoneFunc(func(key tcell.Key) { r.activityLogFieldDone(key, r.activityLogTimeField, r.activityLogTable) })
	r.activityLogKeywordField.SetFocusFunc(func() { r.activityLogSetFieldStyle(r.activityLogKeywordField, true) })
	r.activityLogKeywordField.SetBlurFunc(func() { r.activityLogSetFieldStyle(r.activityLogKeywordField, false) })

	r.activityLogTimeField = tview.NewInputField()
	r.activityLogTimeField.SetLabel("Time (e.g. \"last 7 days\", \"after 2026-09-01\"): ")
	r.activityLogTimeField.SetChangedFunc(func(string) { r.renderActivityLog() })
	r.activityLogTimeField.SetDoneFunc(func(key tcell.Key) { r.activityLogFieldDone(key, r.activityLogTable, r.activityLogKeywordField) })
	r.activityLogTimeField.SetFocusFunc(func() { r.activityLogSetFieldStyle(r.activityLogTimeField, true) })
	r.activityLogTimeField.SetBlurFunc(func() { r.activityLogSetFieldStyle(r.activityLogTimeField, false) })

	filterRow := tview.NewFlex().
		AddItem(r.activityLogKeywordField, 0, 1, true).
		AddItem(r.activityLogTimeField, 0, 2, false)

	r.activityLogTable = tview.NewTable()
	r.activityLogTable.SetBorders(false)
	r.activityLogTable.SetBorderPadding(1, 0, 2, 1)
	r.activityLogTable.SetSelectable(true, false)
	r.activityLogTable.SetFixed(1, 0)
	r.activityLogTable.SetInputCapture(r.captureActivityLogTableKey)

	r.activityLogHint = tview.NewTextView()
	r.activityLogHint.SetWrap(false)
	r.activityLogHint.SetDynamicColors(true)
	activityLogHintText, activityLogHintSpans := buildListHint(r.theme, activityLogHintEntries())
	r.activityLogHint.SetText(activityLogHintText)
	r.activityLogHintSpans = activityLogHintSpans
	r.activityLogHint.SetMouseCapture(r.captureListHintMouse(r.activityLogHint, &r.activityLogHintSpans))

	r.activityLogLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.activityLogTitleBar, 1, 0, false).
		AddItem(filterRow, 1, 0, false).
		AddItem(r.activityLogTable, 0, 1, true).
		AddItem(r.activityLogHint, 1, 0, false)
}

// activityLogSetFieldStyle applies the shared normal/focused input
// contract (see styleInput) to one of this screen's own two filter
// fields — called once at build time and again on every focus/blur (see
// newActivityLogScreen's own SetFocusFunc/SetBlurFunc) and every theme
// change (see applyTheme), the same "which of several fields on one
// screen currently has focus" convention the filter menu's own checkbox
// rows already establish (see filterMenuRowStyle), rather than the
// fixed always-focused shape a single-field dialog like Toolbox's own
// input gets away with.
func (r *Root) activityLogSetFieldStyle(field *tview.InputField, focused bool) {
	styleInput(field, r.theme, focused)
}

// openActivityLog shows the Activity Log screen, freshly read every
// time — the same reasoning openMounts/openFirewall's own doc comments
// give — with the keyword field focused first, the most likely thing to
// type into right away.
func (r *Root) openActivityLog() {
	r.reloadActivityLog()
	r.showOverlay(activityLogPage, r.activityLogLayout)
	r.app.SetFocus(r.activityLogKeywordField)
}

// closeActivityLog hides the Activity Log screen. Nothing to save — a
// pure read-only view, same as Mounts/Firewall.
func (r *Root) closeActivityLog() {
	r.hideOverlay()
}

// reloadActivityLog re-reads the real log file and re-renders — the
// screen's own initial population, and "r"'s own manual refresh.
func (r *Root) reloadActivityLog() {
	entries, err := readActivityLogEntries()
	r.activityLogAllEntries = entries
	r.activityLogReadErr = err
	r.renderActivityLog()
}

// readActivityLogEntries reads the real activity log file back into
// Entry values via activitylog.ParseLine, newest first — a log is read
// chronologically as it's written, but browsed the other way around:
// "what did I just do" matters far more often here than reading from
// the beginning, the same reason `tail` rather than `head` is the usual
// tool for a live log. A log that doesn't exist yet (logging just
// enabled, or never used) is not an error — an empty result, same as a
// directory with nothing in it — only a genuine read failure (a
// permissions problem, ResolvePath itself failing) is.
func readActivityLogEntries() ([]activitylog.Entry, error) {
	path, _, err := activitylog.ResolvePath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var entries []activitylog.Entry
	scanner := bufio.NewScanner(f)
	// A one-off long Message (a batch Paste's own full source/destination
	// pair, say) can run past bufio.Scanner's default 64KiB token limit —
	// raised here, once, rather than have this screen silently drop the
	// rest of the file the moment it hits one.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if e, ok := activitylog.ParseLine(scanner.Text()); ok {
			entries = append(entries, e)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, nil
}

// filterActivityLogEntries narrows entries to those matching keyword
// (a case-insensitive substring of the message — "full-text over the
// message", per feature_ideas.txt) and timeExpr (a filterexpr.ParseMtime
// expression matched against each entry's own Time). Either being
// blank, or timeExpr failing to parse, means that half of the filter is
// simply not applied — the same graceful-degradation contract the
// panel's own filterByMtime already has for exactly the same expression
// grammar, rather than this screen inventing a different error path for
// an invalid expression than the one already established.
func filterActivityLogEntries(entries []activitylog.Entry, keyword, timeExpr string, now time.Time) []activitylog.Entry {
	keyword = strings.ToLower(strings.TrimSpace(keyword))

	var timeFilter filterexpr.MtimeFilter
	hasTimeFilter := false
	if strings.TrimSpace(timeExpr) != "" {
		if f, err := filterexpr.ParseMtime(timeExpr, now); err == nil {
			timeFilter = f
			hasTimeFilter = true
		}
	}

	var out []activitylog.Entry
	for _, e := range entries {
		if keyword != "" && !strings.Contains(strings.ToLower(e.Message), keyword) {
			continue
		}
		if hasTimeFilter && !timeFilter.Match(e.Time) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// activityLogTimeText renders an Entry's own Time the same
// "2006-01-02 15:04:05" layout the panel's own full mtime display and
// the Properties overlay already use (see panel.go/properties.go) —
// one further place that would otherwise need to agree with those two
// by coincidence instead of by sharing the one literal.
func activityLogTimeText(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

// activityLogLevelColor picks Errors' own red and Actions' own green —
// the same "stop sign"/"all clear" convention actionColor (firewall.go)
// and percentStatusColor already use elsewhere in this app — and a
// muted color for Detailed/Debug, which are finer-grained, expected
// noise rather than something worth the same visual weight.
func activityLogLevelColor(theme config.ResolvedTheme, level activitylog.Level) tcell.Color {
	switch level {
	case activitylog.LevelErrors:
		return theme.CriticalText
	case activitylog.LevelActions:
		return theme.EntryExecutable
	default:
		return theme.MutedTextColor
	}
}

// renderActivityLog fills the table: a bold header row, then one row
// per entry currently matching the keyword/time fields (see
// filterActivityLogEntries) — a read failure, or nothing matching,
// shows in place of any rows rather than leaving the screen looking
// like an empty, successful read, the same "report it, don't swallow
// it" principle renderMounts/renderFirewall already follow.
func (r *Root) renderActivityLog() {
	r.activityLogTable.Clear()

	header := func(col int, text string) {
		r.activityLogTable.SetCell(0, col,
			tview.NewTableCell(padRight(text, activityLogColumnWidth(col))).
				SetTextColor(r.theme.Text).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false))
	}
	header(activityLogColTime, "Time")
	header(activityLogColLevel, "Level")
	header(activityLogColCategory, "Category")
	header(activityLogColMessage, "Message")

	// See showTablePlaceholder's own doc comment for why both early
	// returns below go through it rather than a bare SetCell — it's the
	// fix for a real, reported freeze, not just a message.
	if r.activityLogReadErr != nil {
		showTablePlaceholder(r.activityLogTable, r.activityLogReadErr.Error(), r.theme.EntryError)
		return
	}

	entries := filterActivityLogEntries(r.activityLogAllEntries, r.activityLogKeywordField.GetText(), r.activityLogTimeField.GetText(), time.Now())
	if len(entries) == 0 {
		placeholder := "No activity logged yet."
		if len(r.activityLogAllEntries) > 0 {
			placeholder = "No entries match the current filter."
		}
		showTablePlaceholder(r.activityLogTable, placeholder, r.theme.PlaceholderText)
		return
	}

	// Real, selectable rows exist again — see showTablePlaceholder's own
	// doc comment for why this isn't optional cosmetic tidiness.
	enableTableSelection(r.activityLogTable)

	for i, e := range entries {
		row := i + 1
		levelColor := activityLogLevelColor(r.theme, e.Level)
		cell := func(col int, text string, color tcell.Color) {
			r.activityLogTable.SetCell(row, col,
				tview.NewTableCell(padRight(text, activityLogColumnWidth(col))).
					SetTextColor(color).SetSelectable(true))
		}
		cell(activityLogColTime, activityLogTimeText(e.Time), r.theme.Text)
		cell(activityLogColLevel, e.Level.String(), levelColor)
		cell(activityLogColCategory, string(e.Category), r.theme.Text)
		cell(activityLogColMessage, e.Message, r.theme.Text)
	}

	// Keep the cursor in range after a refresh changed how many rows
	// there are — the same restraint renderMounts/renderFirewall's own
	// tail already shows.
	if cur, _ := r.activityLogTable.GetSelection(); cur < 1 || cur > len(entries) {
		r.activityLogTable.Select(1, 0)
	}
}

// activityLogFieldDone is the keyword/time fields' own shared
// SetDoneFunc: Tab moves focus to next, Backtab (Shift+Tab) to prev,
// Enter behaves like Tab (there's nothing to "submit" — filtering
// already happens live via SetChangedFunc), and Escape closes the whole
// screen regardless of which field it was pressed in.
func (r *Root) activityLogFieldDone(key tcell.Key, next, prev tview.Primitive) {
	switch key {
	case tcell.KeyEscape:
		r.closeActivityLog()
	case tcell.KeyTab, tcell.KeyEnter:
		r.app.SetFocus(next)
	case tcell.KeyBacktab:
		r.app.SetFocus(prev)
	}
}

// captureActivityLogTableKey is the table's own key handling: Escape
// closes the screen, Tab/Backtab cycle focus the same way the two
// fields' own activityLogFieldDone does, and "r" re-reads the real log
// file — bound here rather than globally, so typing an actual "r" into
// either filter field types the letter instead of triggering a reload
// (the same "never while typing in a filter box" principle keymap.go's
// own package doc comment establishes for the panel's plain-letter
// layer).
func (r *Root) captureActivityLogTableKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape:
		r.closeActivityLog()
		return nil
	case tcell.KeyTab:
		r.app.SetFocus(r.activityLogKeywordField)
		return nil
	case tcell.KeyBacktab:
		r.app.SetFocus(r.activityLogTimeField)
		return nil
	}
	if event.Key() == tcell.KeyRune && event.Rune() == 'r' {
		r.reloadActivityLog()
		return nil
	}
	return event
}
