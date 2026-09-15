package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

func newTestRootForConnectionMenu(t *testing.T) *Root {
	t.Helper()
	withTestConfigHome(t)
	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	return r
}

func itemTexts(l *tview.List) []string {
	texts := make([]string, l.GetItemCount())
	for i := range texts {
		texts[i], _ = l.GetItemText(i)
	}
	return texts
}

func TestRenderConnectionMenuOnALocalPanelOffersOnlyNewConnection(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	r.renderConnectionMenu()

	texts := itemTexts(r.connectionMenuList)
	if len(texts) != 1 || !strings.Contains(texts[0], "New connection") {
		t.Errorf("items = %v, want exactly one \"New connection…\" row for a local panel", texts)
	}
}

// TestRenderConnectionMenuOnAConnectedPanelListsNewConnectionThenTheActiveEntry
// pins the fact that there's no longer a dedicated "Disconnect (...)"
// row of its own — disconnecting instead happens via
// connectionHistoryEjectGlyph on the active connection's own history
// row (see TestPressingEOnTheActiveConnectionRowDisconnects/
// TestClickingTheEjectGlyphDisconnectsTheActiveConnection).
func TestRenderConnectionMenuOnAConnectedPanelListsNewConnectionThenTheActiveEntry(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := r.panel.connectRemote(fakeConnectedClient(), conn); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}

	r.renderConnectionMenu()

	texts := itemTexts(r.connectionMenuList)
	if len(texts) != 2 {
		t.Fatalf("items = %v, want exactly \"New connection…\" plus one history row for the active connection", texts)
	}
	if !strings.Contains(texts[0], "New connection") {
		t.Errorf("items[0] = %q, want \"New connection…\" first — no separate \"Disconnect\" row anymore", texts[0])
	}
	if !strings.Contains(texts[1], conn.Label()) {
		t.Errorf("items[1] = %q, want the active connection's own history row", texts[1])
	}
	if !strings.Contains(texts[1], connectionHistoryEjectGlyph) {
		t.Errorf("items[1] = %q, want the active connection's row to carry the eject glyph", texts[1])
	}
	if r.connectionMenuActiveRow != 1 {
		t.Errorf("connectionMenuActiveRow = %d, want 1", r.connectionMenuActiveRow)
	}
}

func TestRenderConnectionMenuListsHistoryColoredByState(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	ok := remotefs.Connection{Host: "ok.example.com", User: "tester"}
	failed := remotefs.Connection{Host: "failed.example.com", User: "tester"}
	if err := remotefs.RecordAttempt(failed, true); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := remotefs.RecordAttempt(ok, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	r.renderConnectionMenu()

	texts := itemTexts(r.connectionMenuList)
	var okLine, failedLine string
	for _, text := range texts {
		if strings.Contains(text, ok.Label()) {
			okLine = text
		}
		if strings.Contains(text, failed.Label()) {
			failedLine = text
		}
	}
	if okLine == "" || failedLine == "" {
		t.Fatalf("items = %v, want both history entries listed", texts)
	}
	if !strings.Contains(failedLine, colorTag(r.theme.CriticalText)) {
		t.Errorf("failed entry %q does not carry the critical color tag", failedLine)
	}
	if strings.Contains(okLine, colorTag(r.theme.CriticalText)) {
		t.Errorf("successful entry %q incorrectly carries the critical color tag", okLine)
	}
}

func TestRenderConnectionMenuColorsTheCurrentlyActiveEntryGreen(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := r.panel.connectRemote(fakeConnectedClient(), conn); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}

	r.renderConnectionMenu()

	var line string
	for _, text := range itemTexts(r.connectionMenuList) {
		if strings.Contains(text, conn.Label()) {
			line = text
		}
	}
	if line == "" {
		t.Fatal("history row for the active connection not found")
	}
	if !strings.Contains(line, colorTag(r.theme.EntryExecutable)) {
		t.Errorf("active connection's history row %q does not carry the connected color tag", line)
	}
}

// TestRenderConnectionMenuColorsAPreviouslySuccessfulEntryAMutedGreen
// pins the user's own explicit report: a connection that isn't
// currently active, isn't the last-failed one either, must still read
// as green (just a dimmer shade — see connectionHistorySuccessBlend's
// own doc comment) — never the dropdown's own plain, uncolored text,
// which reads as "unknown" rather than "this one has worked before".
func TestRenderConnectionMenuColorsAPreviouslySuccessfulEntryAMutedGreen(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	// Deliberately never connected in this panel — this exercises the
	// "succeeded before, not active right now" branch specifically,
	// not the "currently active" one.

	r.renderConnectionMenu()

	var line string
	for _, text := range itemTexts(r.connectionMenuList) {
		if strings.Contains(text, conn.Label()) {
			line = text
		}
	}
	if line == "" {
		t.Fatal("history row for the successful connection not found")
	}
	if strings.Contains(line, colorTag(r.theme.EntryExecutable)) {
		t.Error("an inactive entry carries the full-brightness connected color tag, want a dimmer shade")
	}
	if strings.Contains(line, colorTag(r.theme.CriticalText)) {
		t.Error("a successful entry carries the failed/critical color tag")
	}
	wantColor := blendToward(r.theme.EntryExecutable, colorBlack, connectionHistorySuccessBlend)
	if !strings.Contains(line, colorTag(wantColor)) {
		t.Errorf("row %q does not carry the expected muted-green color tag %q", line, colorTag(wantColor))
	}
}

func TestPressingXOnAHistoryRowRemovesItFromHistory(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	r.openConnectionMenu()
	row := historyRowFor(t, r, conn)
	r.connectionMenuList.SetCurrentItem(row)

	got := r.captureConnectionMenuKey(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))

	if got != nil {
		t.Error("captureConnectionMenuKey did not consume the \"x\" keypress")
	}
	history, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("history = %+v, want it empty after removing the only entry", history)
	}
}

func TestPressingXOnNewConnectionRowDoesNothing(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	if err := remotefs.RecordAttempt(remotefs.Connection{Host: "example.com"}, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	r.openConnectionMenu()
	r.connectionMenuList.SetCurrentItem(0) // "New connection…" is always first when local

	got := r.captureConnectionMenuKey(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))

	if got == nil {
		t.Error("captureConnectionMenuKey consumed \"x\" on a non-history row")
	}
	history, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 1 {
		t.Errorf("history = %+v, want the entry left untouched", history)
	}
}

// TestClickingTheRemoveGlyphRemovesTheRowClickingElsewhereReconnects
// pins captureConnectionMenuMouse's own column-based split: a click
// landing on the trailing "✕" removes the row without ever running its
// own reconnect action; a click anywhere else on the same row runs it
// normally.
func TestClickingTheRemoveGlyphRemovesTheRowClickingElsewhereReconnects(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	r.renderConnectionMenu()
	r.connectionMenuList.SetRect(0, 0, 40, r.connectionMenuList.GetItemCount())
	row := historyRowFor(t, r, conn)

	rectX, rectY, _, _ := r.connectionMenuList.GetInnerRect()
	mainText, _ := r.connectionMenuList.GetItemText(row)
	textWidth := tview.TaggedStringWidth(mainText)
	removeX := rectX + textWidth - 1 // squarely on the "✕" glyph itself
	y := rectY + row

	action, event := r.captureConnectionMenuMouse(tview.MouseLeftClick, tcell.NewEventMouse(removeX, y, tcell.Button1, 0))

	if event != nil || action != tview.MouseConsumed {
		t.Error("clicking the remove glyph did not consume the event")
	}
	history, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("history = %+v, want it empty after clicking the remove glyph", history)
	}
}

func TestClickingTheRestOfAHistoryRowDoesNotRemoveIt(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	r.renderConnectionMenu()
	r.connectionMenuList.SetRect(0, 0, 40, r.connectionMenuList.GetItemCount())
	row := historyRowFor(t, r, conn)
	rectX, rectY, _, _ := r.connectionMenuList.GetInnerRect()
	sentEvent := tcell.NewEventMouse(rectX, rectY+row, tcell.Button1, 0)

	action, event := r.captureConnectionMenuMouse(tview.MouseLeftClick, sentEvent)

	// A click on the row but off the remove glyph must pass the event
	// through unconsumed, so tview's own List still gets to select and
	// fire that row's real reconnect action natively.
	if action != tview.MouseLeftClick || event != sentEvent {
		t.Errorf("action, event = %v, %v, want the original click passed through unconsumed", action, event)
	}
	history, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 1 {
		t.Errorf("history = %+v, want the entry left untouched by a click elsewhere on the row", history)
	}
}

func TestPressingEOnTheActiveConnectionRowDisconnects(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := r.panel.connectRemote(fakeConnectedClient(), conn); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	r.openConnectionMenu()
	r.connectionMenuList.SetCurrentItem(r.connectionMenuActiveRow)

	got := r.captureConnectionMenuKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))

	if got != nil {
		t.Error("captureConnectionMenuKey did not consume the \"e\" keypress on the active row")
	}
	if r.panel.remote != nil {
		t.Error("panel is still connected after pressing \"e\" on its own active row")
	}
}

// TestPressingEOnANonActiveHistoryRowDoesNothing pins that "e" is only
// ever wired to the one row wearing connectionHistoryEjectGlyph — an
// ordinary history row that merely happens to be highlighted must not
// disconnect whatever the currently active panel is doing elsewhere.
func TestPressingEOnANonActiveHistoryRowDoesNothing(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	r.openConnectionMenu()
	row := historyRowFor(t, r, conn)
	r.connectionMenuList.SetCurrentItem(row)

	got := r.captureConnectionMenuKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))

	if got == nil {
		t.Error("captureConnectionMenuKey consumed \"e\" on a row that isn't the active connection")
	}
}

// TestClickingTheEjectGlyphDisconnectsTheActiveConnection pins the
// mouse equivalent of TestPressingEOnTheActiveConnectionRowDisconnects
// — the small "⏏" this project's own doc comments compare to a USB
// drive's own eject icon, replacing what used to be a whole separate
// "Disconnect (...)" list item (see
// TestRenderConnectionMenuOnAConnectedPanelListsNewConnectionThenTheActiveEntry).
func TestClickingTheEjectGlyphDisconnectsTheActiveConnection(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := r.panel.connectRemote(fakeConnectedClient(), conn); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	r.renderConnectionMenu()
	r.connectionMenuList.SetRect(0, 0, 40, r.connectionMenuList.GetItemCount())
	row := r.connectionMenuActiveRow

	rectX, rectY, _, _ := r.connectionMenuList.GetInnerRect()
	mainText, _ := r.connectionMenuList.GetItemText(row)
	textWidth := tview.TaggedStringWidth(mainText)
	removeGlyphWidth := tview.TaggedStringWidth(connectionHistoryRemoveGlyph)
	ejectGlyphWidth := tview.TaggedStringWidth(connectionHistoryEjectGlyph)
	ejectStart := rectX + textWidth - removeGlyphWidth - 2 - ejectGlyphWidth // squarely on the "⏏" glyph itself
	y := rectY + row

	action, event := r.captureConnectionMenuMouse(tview.MouseLeftClick, tcell.NewEventMouse(ejectStart, y, tcell.Button1, 0))

	if event != nil || action != tview.MouseConsumed {
		t.Error("clicking the eject glyph did not consume the event")
	}
	if r.panel.remote != nil {
		t.Error("panel is still connected after clicking the eject glyph")
	}
}

// historyRowFor returns the row index renderConnectionMenu assigned to
// conn — fails the test outright if conn isn't currently a history row
// at all, so a caller never silently operates on the wrong row.
func historyRowFor(t *testing.T, r *Root, conn remotefs.Connection) int {
	t.Helper()
	for row, c := range r.connectionMenuHistoryRows {
		if c == conn {
			return row
		}
	}
	t.Fatalf("connection %+v is not a history row right now", conn)
	return -1
}
