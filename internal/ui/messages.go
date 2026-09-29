// messages.go is the Messages screen ("gm", see keymap.go) — Stufen
// 3-5 of feature_ideas.txt's own "3a. Benachrichtigungen": a full-
// screen catalog over r.notify's own ring buffer (internal/notify),
// styled after Sessions' own per-row action-cell table (sessions.go)
// rather than Mounts/Firewall's own plain read-only rows — cell
// navigation (SetSelectable(true, true)), since every row here carries
// its own Auswahl/Neu checkboxes plus a Close action, not just one
// row-wide "select it".
//
// The status bar's own badge (bottombar.go's notifyBadgeText/
// notifyBadgeSpan) is this screen's other real entry point besides
// "gm" — both open the same screen, and the badge's own unread count
// is read directly from r.notify.UnreadCount(), never a second,
// independently maintained counter.
package ui

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/notify"
)

const messagesPage = "messages"
const messagesDetailPage = "messages-detail"

// messagesDwellDelay is how long the table's own cursor has to sit on
// an unread row before it's marked read automatically — feature_ideas.
// txt's own "Dwell-Schwelle", left an open implementation decision
// there ("fest oder... einstellbar"). A fixed 1.5s (the middle of the
// spec's own "1-2 Sekunden" range), not wired into Options: unlike the
// chord timeout it's compared to there, nothing about this app's own
// established settings surface suggests this specific value needs to
// vary per user — revisit if real feedback says otherwise.
const messagesDwellDelay = 1500 * time.Millisecond

// Column indices within messagesTable, left to right per
// feature_ideas.txt's own Stufe 4: Auswahl (bulk-action checkbox),
// Neu (unread indicator, independently toggleable), Zeit, Message
// (the variable, truncated column), Close.
const (
	messagesColSelect = iota
	messagesColNew
	messagesColTime
	messagesColMessage
	messagesColClose
)

// messagesCloseGlyph reuses Sessions' own Close glyph/meaning exactly
// (sessionsCloseGlyph, "✕") — the same shared "this is what closing/
// removing a row looks like" visual language, not a fresh one.
const messagesCloseGlyph = sessionsCloseGlyph

// messagesColSelectWidth/messagesColNewWidth/messagesColTimeWidth/
// messagesColCloseWidth are this screen's own four fixed-width
// columns' own padding floor — messagesMessageColumnWidth is what
// turns these into the Message column's own real, computed width.
const (
	messagesColSelectWidth = 3  // " ○ "
	messagesColNewWidth    = 3  // " ○ "
	messagesColTimeWidth   = 19 // "2006-01-02 15:04:05"
	messagesColCloseWidth  = 3  // " ✕ "
)

// messagesMessageColumnWidth computes the Message column's own real
// width from r.lastScreenWidth — this screen's own deliberate
// exception to the "padRight is a floor, never a ceiling" rule every
// other catalog screen here follows (see renderMessagesRow's own doc
// comment): the Message column is the last variable column before a
// fixed action column, and a message needs to stay one line, so it
// truncates instead of wrapping or pushing Close off screen. The
// subtracted margin is an approximation of the table's own border
// padding (SetBorderPadding(1, 0, 2, 1), see newMessagesScreen) plus a
// little headroom, not a pixel-exact measurement — the same
// imprecision this app's own other column-width floors already accept.
func (r *Root) messagesMessageColumnWidth() int {
	const margin = 12
	width := r.lastScreenWidth - messagesColSelectWidth - messagesColNewWidth - messagesColTimeWidth - messagesColCloseWidth - margin
	if width < 10 {
		width = 10
	}
	return width
}

// messagesLevelColor is notifyColor's own reuse here — the Message
// cell's own text is colored by its Level exactly the same way the
// transient toast bar already colors its one line (notifybar.go), per
// this whole feature's own "keine neue visuelle Sprache erfinden"
// principle: success green, error red, info (today: the firewall
// self-lockout rollback firing) the same warning color rsync.go's own
// remote-relay notice already uses.
func messagesLevelColor(level notify.Level, theme config.ResolvedTheme) tcell.Color {
	return notifyColor(level, theme)
}

// messagesHintEntries is this screen's own bottom hint bar (see
// sessionsHintEntries' own doc comment for why this is a function, not
// a var). "a"/"r"/"d" are the three bulk actions Stufe 4 asks for
// (select all/none, toggle read/unread, delete), each following the
// same "acts on the Auswahl, or — nothing marked — just the current
// row" fallback rule selectedOrCurrentPaths already establishes for
// the file panel's own Trash/Remove/Sed Replace. "x"/Delete is
// separate: Sessions' own per-row Close shortcut, always just the
// focused row regardless of any selection — the two overlap in
// behavior whenever nothing is selected, which is expected, not a bug.
func messagesHintEntries() []listHintEntry {
	activate := func(r *Root) {
		row, col := r.messagesTable.GetSelection()
		r.activateMessagesCell(row, col)
	}
	return []listHintEntry{
		{keys: []listHintKey{
			{"↑", simulateKeyOnFocused(tcell.KeyUp)},
			{"↓", simulateKeyOnFocused(tcell.KeyDown)},
			{"←", simulateKeyOnFocused(tcell.KeyLeft)},
			{"→", simulateKeyOnFocused(tcell.KeyRight)},
		}, label: "move"},
		{keys: []listHintKey{
			{"Enter", activate},
			{"Space", activate},
		}, suffix: "/click", label: "activate"},
		hintKey("a", "select all/none", func(r *Root) { r.toggleSelectAllMessages() }),
		hintKey("r", "read/unread", func(r *Root) { r.toggleReadForMessagesSelection() }),
		hintKey("d", "delete selection", func(r *Root) { r.deleteMessagesSelection() }),
		hintKey("x", "delete row", func(r *Root) { r.deleteCurrentMessageRow() }),
		hintKey("Esc", "close", func(r *Root) { r.closeMessages() }),
	}
}

// newMessagesScreen builds the whole screen once, at startup — the
// same build-once/repopulate-on-open shape newSessionsScreen already
// establishes. SetSelectable(true, true): cell-level selection, since
// every one of this row's five cells is independently reachable, per
// feature_ideas.txt's own explicit "Zellen-Navigation... Links/Rechts
// zwischen den Zellen einer Zeile" — unlike Sessions, which collapses
// three of its own six columns into one keyboard stop, nothing here
// is collapsed.
func (r *Root) newMessagesScreen() {
	r.messagesSelected = make(map[uint64]bool)

	r.messagesTable = tview.NewTable()
	r.messagesTable.SetBorders(false)
	r.messagesTable.SetBorderPadding(1, 0, 2, 1)
	r.messagesTable.SetSelectable(true, true)
	r.messagesTable.SetFixed(1, 0)
	r.messagesTable.SetInputCapture(r.captureMessagesKey)
	r.messagesTable.SetSelectedFunc(func(row, column int) { r.activateMessagesCell(row, column) })
	r.messagesTable.SetSelectionChangedFunc(func(row, column int) { r.armMessagesDwell(row) })

	r.messagesTitleBar = newPlainTitleBar("Messages")

	r.messagesHint = tview.NewTextView()
	r.messagesHint.SetWrap(false)
	r.messagesHint.SetDynamicColors(true)
	messagesHintText, messagesHintSpans := buildListHint(r.theme, messagesHintEntries())
	r.messagesHint.SetText(messagesHintText)
	r.messagesHintSpans = messagesHintSpans
	r.messagesHint.SetMouseCapture(r.captureListHintMouse(r.messagesHint, &r.messagesHintSpans))

	r.messagesLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.messagesTitleBar, 1, 0, false).
		AddItem(r.messagesTable, 0, 1, true).
		AddItem(r.messagesHint, 1, 0, false)
}

// openMessages shows the Messages screen, freshly read every time —
// the same reasoning openSessions/openMounts already give: a message
// can arrive from a background trigger at any moment, including while
// this screen isn't open.
func (r *Root) openMessages() {
	r.reloadMessages()
	r.showOverlay(messagesPage, r.messagesLayout)
}

func (r *Root) closeMessages() {
	r.endMessagesShiftSelect()
	r.hideOverlay()
}

// reloadMessages re-reads r.notify's own ring buffer into
// r.messagesList, newest first (the same "tail, not head" logic
// Activity Log's own doc comment already establishes for itself —
// r.notify.Messages() itself returns oldest-first, this just reverses
// it for display), and re-renders. Called on open, and after every
// mutating action (a delete, a read/unread toggle) — the same
// "re-derive from the real source of truth rather than patch a local
// copy" shape reloadSessions/reloadFirewall already use.
func (r *Root) reloadMessages() {
	messages := r.notify.Messages()
	r.messagesList = make([]notify.Message, len(messages))
	for i, m := range messages {
		r.messagesList[len(messages)-1-i] = m
	}
	r.pruneMessagesSelection()
	r.renderMessages()
	r.refreshStatusBar() // the badge's own unread count may have just changed
}

// pruneMessagesSelection drops any Auswahl entry whose own Message no
// longer exists (deleted since it was checked) — messagesSelected is
// keyed by ID forever otherwise, slowly accumulating stale entries
// across a long session.
func (r *Root) pruneMessagesSelection() {
	live := make(map[uint64]bool, len(r.messagesList))
	for _, m := range r.messagesList {
		live[m.ID] = true
	}
	for id := range r.messagesSelected {
		if !live[id] {
			delete(r.messagesSelected, id)
		}
	}
}

// renderMessages fills the table: a bold header row, then one row per
// currently held Message — see showTablePlaceholder's own doc comment
// for why an empty list goes through it rather than a bare SetCell.
func (r *Root) renderMessages() {
	r.messagesTable.Clear()

	header := func(col int, text string) {
		r.messagesTable.SetCell(0, col,
			tview.NewTableCell(text).
				SetTextColor(r.theme.Text).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false))
	}
	header(messagesColSelect, padRight("", messagesColSelectWidth))
	header(messagesColNew, padRight("", messagesColNewWidth))
	header(messagesColTime, padRight("Time", messagesColTimeWidth))
	header(messagesColMessage, "Message")
	header(messagesColClose, padRight("", messagesColCloseWidth))

	r.messagesTitleBar.SetText(fmt.Sprintf(" Messages — %d, %d unread ", len(r.messagesList), r.notify.UnreadCount()))

	if len(r.messagesList) == 0 {
		showTablePlaceholder(r.messagesTable, "No messages yet.", r.theme.PlaceholderText)
		return
	}

	for i, m := range r.messagesList {
		r.renderMessagesRow(i+1, m)
	}
	r.messagesTable.SetSelectable(true, true) // see renderSessions' own doc comment on why this is re-asserted after Clear

	if cur, _ := r.messagesTable.GetSelection(); cur < 1 || cur > len(r.messagesList) {
		r.messagesTable.Select(1, messagesColSelect)
	}
}

// renderMessagesRow fills one Message's own row. Auswahl/Neu are
// checkboxText's own ●/○ pair (panel.go) — the same glyphs the file
// panel's own checkbox column already uses, reused directly rather
// than reinvented. Message is truncateMiddle'd to
// messagesMessageColumnWidth — this screen's own one deliberate
// exception to every other catalog screen's "padRight floor, never a
// ceiling" rule (see messagesMessageColumnWidth's own doc comment for
// why), colored by its own Level throughout (messagesLevelColor).
func (r *Root) renderMessagesRow(row int, m notify.Message) {
	selectCell := tview.NewTableCell(padRight(checkboxText(r.messagesSelected[m.ID]), messagesColSelectWidth)).
		SetTextColor(r.theme.Text).
		SetSelectable(true).
		SetClickedFunc(r.clickMessagesCell(row, messagesColSelect))
	r.messagesTable.SetCell(row, messagesColSelect, selectCell)

	newCell := tview.NewTableCell(padRight(checkboxText(!m.Read), messagesColNewWidth)).
		SetTextColor(r.theme.Text).
		SetSelectable(true).
		SetClickedFunc(r.clickMessagesCell(row, messagesColNew))
	r.messagesTable.SetCell(row, messagesColNew, newCell)

	timeCell := tview.NewTableCell(padRight(activityLogTimeText(m.Time), messagesColTimeWidth)).
		SetTextColor(r.theme.MutedTextColor).
		SetSelectable(true).
		SetClickedFunc(r.clickMessagesCell(row, messagesColTime))
	r.messagesTable.SetCell(row, messagesColTime, timeCell)

	text := truncateMiddle(m.Text, r.messagesMessageColumnWidth())
	messageCell := tview.NewTableCell(text).
		SetTextColor(messagesLevelColor(m.Level, r.theme)).
		SetSelectable(true).
		SetClickedFunc(r.clickMessagesCell(row, messagesColMessage))
	r.messagesTable.SetCell(row, messagesColMessage, messageCell)

	closeCell := tview.NewTableCell(" " + messagesCloseGlyph + " ").
		SetTextColor(r.theme.MutedTextColor).
		SetSelectable(true).
		SetClickedFunc(r.clickMessagesCell(row, messagesColClose))
	r.messagesTable.SetCell(row, messagesColClose, closeCell)
}

// messageAt returns the row's own Message — false for the header row
// or any row past the end, the same defensiveness sessionAt already
// establishes.
func (r *Root) messageAt(row int) (notify.Message, bool) {
	i := row - 1
	if i < 0 || i >= len(r.messagesList) {
		return notify.Message{}, false
	}
	return r.messagesList[i], true
}

// activateMessagesCell is Enter, Space, or a click on one cell —
// dispatches by column, the same shape activateSessionsCell already
// establishes. Message opens the detail view (Stufe 5) for the
// focused row regardless of any Auswahl marking, per feature_ideas.
// txt's own explicit "die eigene Auswahl-Markierung... wird komplett
// ignoriert, auch bei mehreren markierten Zeilen zählt immer nur der
// aktuelle Fokus".
func (r *Root) activateMessagesCell(row, column int) {
	if _, ok := r.messageAt(row); !ok {
		return
	}
	switch column {
	case messagesColSelect:
		r.toggleMessageCheckboxRow(row)
	case messagesColNew:
		r.toggleMessageReadRow(row)
	case messagesColMessage:
		r.openMessageDetail(row)
	case messagesColClose:
		r.deleteMessageRow(row)
	}
}

// clickMessagesCell is one cell's own mouse action — see
// clickSessionsCell's own doc comment for why this has to be per-cell
// rather than a table-wide mouse capture.
func (r *Root) clickMessagesCell(row, column int) func() bool {
	return func() bool {
		r.activateMessagesCell(row, column)
		return true
	}
}

// toggleMessageCheckboxRow flips row's own Auswahl mark and re-renders
// just that one cell — called both directly (a click/Space/Enter on
// the Select column) and from applyMessagesDragDelta (Shift+Up/Down).
func (r *Root) toggleMessageCheckboxRow(row int) {
	m, ok := r.messageAt(row)
	if !ok {
		return
	}
	r.messagesSelected[m.ID] = !r.messagesSelected[m.ID]
	r.messagesTable.SetCell(row, messagesColSelect,
		tview.NewTableCell(padRight(checkboxText(r.messagesSelected[m.ID]), messagesColSelectWidth)).
			SetTextColor(r.theme.Text).
			SetSelectable(true).
			SetClickedFunc(r.clickMessagesCell(row, messagesColSelect)))
}

// toggleMessageReadRow flips one row's own Read status by hand — Neu
// column's own click/Space/Enter action, independent of the dwell
// mechanism (armMessagesDwell). Goes through reloadMessages rather
// than a targeted cell update: r.notify is the single source of
// truth (see notify.Store.SetRead's own doc comment), and this also
// keeps the title bar's own unread count and the status bar badge in
// sync without a second code path to keep correct.
func (r *Root) toggleMessageReadRow(row int) {
	m, ok := r.messageAt(row)
	if !ok {
		return
	}
	r.notify.SetRead(m.ID, !m.Read)
	r.reloadMessages()
}

// deleteMessageRow removes row's own Message outright — no
// confirmation dialog, per feature_ideas.txt's own explicit reasoning:
// a Message is flüchtiger Sitzungszustand, not file or system state,
// so Trash/Remove's own "confirm first" threshold doesn't apply here.
func (r *Root) deleteMessageRow(row int) {
	m, ok := r.messageAt(row)
	if !ok {
		return
	}
	r.notify.Delete(m.ID)
	delete(r.messagesSelected, m.ID)
	r.reloadMessages()
}

// deleteCurrentMessageRow is "x"/Delete's own action — always the
// currently focused row, regardless of any Auswahl marking (see
// messagesHintEntries' own doc comment on why this is deliberately
// distinct from deleteMessagesSelection).
func (r *Root) deleteCurrentMessageRow() {
	row, _ := r.messagesTable.GetSelection()
	r.deleteMessageRow(row)
}

// selectedOrCurrentMessageIDs is this screen's own
// selectedOrCurrentPaths (panel.go): every checked Auswahl entry, or —
// nothing checked — just the currently focused row's own single
// Message, the same fallback rule the file panel's Trash/Remove/Sed
// Replace already establish.
func (r *Root) selectedOrCurrentMessageIDs() []uint64 {
	if len(r.messagesSelected) > 0 {
		ids := make([]uint64, 0, len(r.messagesSelected))
		for id := range r.messagesSelected {
			ids = append(ids, id)
		}
		return ids
	}
	row, _ := r.messagesTable.GetSelection()
	if m, ok := r.messageAt(row); ok {
		return []uint64{m.ID}
	}
	return nil
}

// toggleSelectAllMessages is the "a" bulk action: every row checked if
// any were previously unchecked, otherwise every row cleared — the
// same "toggle" shape toggleSelectAllViaHeader (panel.go) already
// gives the file panel's own header checkbox.
func (r *Root) toggleSelectAllMessages() {
	allSelected := len(r.messagesSelected) == len(r.messagesList) && len(r.messagesList) > 0
	if allSelected {
		r.messagesSelected = make(map[uint64]bool)
	} else {
		for _, m := range r.messagesList {
			r.messagesSelected[m.ID] = true
		}
	}
	r.renderMessages()
}

// toggleReadForMessagesSelection is the "r" bulk action: flips every
// selected (or, if none, the current) row's own Read status — "flips",
// not "always marks read", per feature_ideas.txt's own plain
// "Gelesen/Neu togglen" wording; a mixed selection of read and unread
// rows all end up unread, the simplest rule that still reads as one
// single, predictable toggle rather than needing its own "what does
// mixed mean" decision.
func (r *Root) toggleReadForMessagesSelection() {
	for _, id := range r.selectedOrCurrentMessageIDs() {
		for _, m := range r.messagesList {
			if m.ID == id {
				r.notify.SetRead(id, !m.Read)
				break
			}
		}
	}
	r.reloadMessages()
}

// deleteMessagesSelection is the "d" bulk action: deletes every
// selected (or, if none, the current) row's own Message — the same
// effect as messagesColClose/deleteCurrentMessageRow, just for a whole
// Auswahl at once. No confirmation, same reasoning as
// deleteMessageRow.
func (r *Root) deleteMessagesSelection() {
	for _, id := range r.selectedOrCurrentMessageIDs() {
		r.notify.Delete(id)
		delete(r.messagesSelected, id)
	}
	r.reloadMessages()
}

// applyMessagesDragDelta toggles exactly the rows whose membership in
// [start, to] differs from their membership in [start, from] —
// Panel's own applyDragDelta (panel.go), scoped to this screen's own
// checkbox set instead of Panel's.
func (r *Root) applyMessagesDragDelta(start, from, to int) {
	oldLo, oldHi := min(start, from), max(start, from)
	newLo, newHi := min(start, to), max(start, to)

	for row := min(oldLo, newLo); row <= max(oldHi, newHi); row++ {
		inOld := row >= oldLo && row <= oldHi
		inNew := row >= newLo && row <= newHi
		if inOld != inNew {
			r.toggleMessageCheckboxRow(row)
		}
	}
}

// advanceMessagesShiftSelect is Shift+Up/Down's own action —
// Panel's own advanceShiftSelect (panel.go), scoped the same way
// applyMessagesDragDelta is.
func (r *Root) advanceMessagesShiftSelect(row int) {
	if !r.messagesShiftSelecting {
		r.messagesShiftAnchorRow, _ = r.messagesTable.GetSelection()
		r.messagesShiftCurrentRow = r.messagesShiftAnchorRow
		r.messagesShiftSelecting = true
		r.toggleMessageCheckboxRow(r.messagesShiftAnchorRow)
	}
	if row != r.messagesShiftCurrentRow {
		r.applyMessagesDragDelta(r.messagesShiftAnchorRow, r.messagesShiftCurrentRow, row)
	}
	r.messagesShiftCurrentRow = row
}

// endMessagesShiftSelect ends any Shift+Up/Down range-selection
// session in progress — Panel's own endShiftSelect.
func (r *Root) endMessagesShiftSelect() {
	r.messagesShiftSelecting = false
}

// armMessagesDwell is SetSelectionChangedFunc's own callback (see
// newMessagesScreen): every time the table's own cursor lands on a new
// row, arms a fresh messagesDwellDelay timer that marks it read once
// it fires, unless the cursor has since moved to a different row (see
// messagesDwellGeneration's own doc comment) — feature_ideas.txt's own
// "länger als 1-2 Sekunden Fokus auf einer Zeile markiert sie als
// gelesen". A no-op for an already-read row or an invalid one (the
// header row, "row" 0): nothing for the timer to do.
func (r *Root) armMessagesDwell(row int) {
	r.messagesDwellGeneration++
	generation := r.messagesDwellGeneration
	m, ok := r.messageAt(row)
	if !ok || m.Read {
		return
	}
	id := m.ID
	time.AfterFunc(messagesDwellDelay, func() {
		r.app.QueueUpdateDraw(func() { r.applyMessagesDwell(id, generation) })
	})
}

// applyMessagesDwell is armMessagesDwell's own timer callback, split
// out so a test can call it directly instead of waiting on a real
// messagesDwellDelay — the same shape autoHideError/autoHideNotifyToast
// already use for their own timers.
func (r *Root) applyMessagesDwell(id uint64, generation int) {
	if r.messagesDwellGeneration != generation {
		return // the cursor moved on before this timer fired — stale
	}
	if r.notify.SetRead(id, true) {
		r.reloadMessages()
	}
}

// captureMessagesKey is the Messages screen's own key handling: Escape
// closes it, Space activates the focused cell (the same as Enter —
// see activateMessagesCell), "a"/"r"/"d" run the three bulk actions,
// "x"/Delete delete the focused row from anywhere in it (the same
// "from-anywhere-in-the-row" convenience captureSessionsKey's own "x"
// already gives Close), and Shift+Up/Down extend or shrink a range
// selection — Panel's own captureTableKey, scoped to this screen.
func (r *Root) captureMessagesKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeMessages()
		return nil
	}
	if event.Modifiers()&tcell.ModShift != 0 && (event.Key() == tcell.KeyUp || event.Key() == tcell.KeyDown) {
		delta := 1
		if event.Key() == tcell.KeyUp {
			delta = -1
		}
		row, col := r.messagesTable.GetSelection()
		next := row + delta
		if next < 1 {
			next = 1
		}
		if last := len(r.messagesList); next > last {
			next = last
		}
		r.messagesTable.Select(next, col)
		r.advanceMessagesShiftSelect(next)
		return nil
	}
	r.endMessagesShiftSelect()

	if event.Key() == tcell.KeyRune {
		switch event.Rune() {
		case ' ':
			row, col := r.messagesTable.GetSelection()
			r.activateMessagesCell(row, col)
			return nil
		case 'a':
			r.toggleSelectAllMessages()
			return nil
		case 'r':
			r.toggleReadForMessagesSelection()
			return nil
		case 'd':
			r.deleteMessagesSelection()
			return nil
		case 'x':
			r.deleteCurrentMessageRow()
			return nil
		}
	}
	if event.Key() == tcell.KeyDelete {
		r.deleteCurrentMessageRow()
		return nil
	}
	return event
}

// applyMessagesTheme themes the Messages screen and its own detail
// modal — split out of applyTheme (see its own doc comment), guarded
// the same way applySessionsTheme is.
func (r *Root) applyMessagesTheme(theme config.ResolvedTheme) {
	if r.messagesTable == nil {
		return
	}
	r.messagesLayout.SetBackgroundColor(theme.SurfaceBackground)
	r.messagesTable.SetBackgroundColor(theme.SurfaceBackground)

	r.messagesTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.messagesTitleBar.SetTextColor(theme.TextColor)
	r.messagesHint.SetBackgroundColor(theme.InputBackground)
	r.messagesHint.SetTextColor(theme.MutedTextColor)
	messagesHintText, messagesHintSpans := buildListHint(theme, messagesHintEntries())
	r.messagesHint.SetText(messagesHintText)
	r.messagesHintSpans = messagesHintSpans

	r.renderMessages() // cell colors are baked in per cell, not looked up live at draw time

	r.applyMessageDetailTheme(theme)
}
