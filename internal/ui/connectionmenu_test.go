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

// connectionMenuLabelTexts reads every row's own label cell — the
// Table equivalent of the old List's itemTexts, now that each row is
// three independent cells (see connectionMenuCol* and this file's own
// doc comment on why) rather than one string.
func connectionMenuLabelTexts(table *tview.Table) []string {
	rows := table.GetRowCount()
	texts := make([]string, rows)
	for i := 0; i < rows; i++ {
		if cell := table.GetCell(i, connectionMenuColLabel); cell != nil {
			texts[i] = cell.Text
		}
	}
	return texts
}

// cellTextColor extracts the color connectionHistoryColor actually set
// on cell (see TableCell.SetTextColor's own doc comment: it lands in
// Style, not the deprecated Color field, once NewTableCell has already
// given the cell a non-default Style — which it always has here).
func cellTextColor(cell *tview.TableCell) tcell.Color {
	fg, _, _ := cell.Style.Decompose()
	return fg
}

func TestRenderConnectionMenuOnALocalPanelOffersOnlyNewConnection(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	r.renderConnectionMenu()

	texts := connectionMenuLabelTexts(r.connectionMenuTable)
	if len(texts) != 1 || !strings.Contains(texts[0], "New connection") {
		t.Errorf("rows = %v, want exactly one \"New connection…\" row for a local panel", texts)
	}
}

// TestRenderConnectionMenuOnAConnectedPanelListsNewConnectionThenTheActiveEntry
// pins the fact that there's no longer a dedicated "Disconnect (...)"
// row of its own — disconnecting instead happens via the active
// connection's own eject cell (see
// TestPressingEOnTheActiveConnectionRowDisconnects/
// TestClickingTheEjectCellDisconnectsTheActiveConnection).
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

	texts := connectionMenuLabelTexts(r.connectionMenuTable)
	if len(texts) != 2 {
		t.Fatalf("rows = %v, want exactly \"New connection…\" plus one history row for the active connection", texts)
	}
	if !strings.Contains(texts[0], "New connection") {
		t.Errorf("row 0 label = %q, want \"New connection…\" first — no separate \"Disconnect\" row anymore", texts[0])
	}
	if !strings.Contains(texts[1], conn.Label()) {
		t.Errorf("row 1 label = %q, want the active connection's own history row", texts[1])
	}
	ejectCell := r.connectionMenuTable.GetCell(1, connectionMenuColEject)
	if !strings.Contains(ejectCell.Text, connectionHistoryEjectGlyph) {
		t.Errorf("row 1 eject cell = %q, want it to carry the eject glyph", ejectCell.Text)
	}
	if r.connectionMenuActiveRow != 1 {
		t.Errorf("connectionMenuActiveRow = %d, want 1", r.connectionMenuActiveRow)
	}
}

func TestRenderConnectionMenuListsHistoryColoredByState(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	ok := remotefs.Connection{Host: "ok.example.com", User: "tester"}
	failed := remotefs.Connection{Host: "failed.example.com", User: "tester"}
	// failed must have connected successfully at least once before a
	// later failure will still keep it in history (see RecordAttempt's
	// own doc comment: a brand-new connection that fails right away is
	// never added at all).
	if err := remotefs.RecordAttempt(failed, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := remotefs.RecordAttempt(failed, true); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := remotefs.RecordAttempt(ok, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	r.renderConnectionMenu()

	okRow, failedRow := -1, -1
	for row, conn := range r.connectionMenuHistoryRows {
		switch conn {
		case ok:
			okRow = row
		case failed:
			failedRow = row
		}
	}
	if okRow == -1 || failedRow == -1 {
		t.Fatalf("history rows = %+v, want both entries listed", r.connectionMenuHistoryRows)
	}

	failedColor := cellTextColor(r.connectionMenuTable.GetCell(failedRow, connectionMenuColLabel))
	if failedColor != r.theme.CriticalText {
		t.Errorf("failed entry color = %v, want %v (theme.CriticalText)", failedColor, r.theme.CriticalText)
	}
	okColor := cellTextColor(r.connectionMenuTable.GetCell(okRow, connectionMenuColLabel))
	if okColor == r.theme.CriticalText {
		t.Errorf("successful entry incorrectly carries the critical color %v", okColor)
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

	row := historyRowFor(t, r, conn)
	got := cellTextColor(r.connectionMenuTable.GetCell(row, connectionMenuColLabel))
	if got != r.theme.EntryExecutable {
		t.Errorf("active connection's row color = %v, want %v (theme.EntryExecutable)", got, r.theme.EntryExecutable)
	}
}

// TestRenderConnectionMenuColorsAPreviouslySuccessfulEntryAMutedGreen
// pins the user's own explicit report: a connection that isn't
// currently active, isn't the last-failed one either, must still read
// as green (just a dimmer shade — see connectionHistorySuccessBlend's
// own doc comment) — never the dropdown's own default, uncolored text,
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

	row := historyRowFor(t, r, conn)
	got := cellTextColor(r.connectionMenuTable.GetCell(row, connectionMenuColLabel))
	if got == r.theme.EntryExecutable {
		t.Error("an inactive entry carries the full-brightness connected color, want a dimmer shade")
	}
	if got == r.theme.CriticalText {
		t.Error("a successful entry carries the failed/critical color")
	}
	want := blendToward(r.theme.EntryExecutable, r.theme.MutedTextColor, connectionHistorySuccessBlend)
	if got != want {
		t.Errorf("row color = %v, want the muted-green blend %v", got, want)
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
	r.connectionMenuTable.Select(row, connectionMenuColLabel)

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
	r.connectionMenuTable.Select(0, connectionMenuColLabel) // "New connection…" is always first when local

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

// TestClickingTheRemoveCellRemovesTheRow is the mouse equivalent of
// TestPressingXOnAHistoryRowRemovesItFromHistory — a direct call to
// the remove cell's own Clicked func, the same way tview's Table
// itself dispatches a real click (see clickConnectionMenuCell's own
// doc comment on why there's no more column-position math to drive
// this through instead).
func TestClickingTheRemoveCellRemovesTheRow(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	r.openConnectionMenu()
	row := historyRowFor(t, r, conn)

	r.connectionMenuTable.GetCell(row, connectionMenuColRemove).Clicked()

	history, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("history = %+v, want it empty after clicking the remove cell", history)
	}
	if r.activePage != connectionMenuPage {
		t.Errorf("activePage = %q, want the dropdown to stay open after removing one entry", r.activePage)
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
	r.connectionMenuTable.Select(r.connectionMenuActiveRow, connectionMenuColLabel)

	got := r.captureConnectionMenuKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))

	if got != nil {
		t.Error("captureConnectionMenuKey did not consume the \"e\" keypress on the active row")
	}
	if r.panel.remote != nil {
		t.Error("panel is still connected after pressing \"e\" on its own active row")
	}
}

// TestPressingEOnANonActiveHistoryRowOpensEditPrefilled pins the "e"
// key's own second meaning: on any history row that isn't the active
// connection, it opens the Connect dialog prefilled from that row's
// own entry instead of disconnecting anything — the active panel
// elsewhere is left untouched, only ever ejected by pressing "e" on
// its own row (see TestPressingEOnTheActiveConnectionRowDisconnects).
func TestPressingEOnANonActiveHistoryRowOpensEditPrefilled(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", Port: 2222, User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	r.openConnectionMenu()
	row := historyRowFor(t, r, conn)
	r.connectionMenuTable.Select(row, connectionMenuColLabel)

	got := r.captureConnectionMenuKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))

	if got != nil {
		t.Error("captureConnectionMenuKey did not consume \"e\" on a non-active history row")
	}
	if r.activePage != connectDialogPage {
		t.Errorf("activePage = %q, want the Connect dialog open", r.activePage)
	}
	if got := r.connectHostField.GetText(); got != conn.Host {
		t.Errorf("Host field = %q, want %q", got, conn.Host)
	}
	if got := r.connectPortField.GetText(); got != "2222" {
		t.Errorf("Port field = %q, want \"2222\"", got)
	}
	if got := r.connectUserField.GetText(); got != conn.User {
		t.Errorf("User field = %q, want %q", got, conn.User)
	}
	if r.panel.remote != nil {
		t.Error("editing a history row must not connect anything on its own")
	}
}

// TestPressingEOnANonActiveHistoryRowDoesNotDisconnectTheActivePanel
// covers the one thing TestPressingEOnANonActiveHistoryRowOpensEditPrefilled
// doesn't: a highlighted, non-active history row's own "e" must never
// reach for whatever connection some *other* panel happens to be
// running, even though editConnectionHistoryRow now makes "e" do
// something on that row instead of nothing.
func TestPressingEOnANonActiveHistoryRowDoesNotDisconnectTheActivePanel(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	active := remotefs.Connection{Host: "active.example.com", User: "tester"}
	other := remotefs.Connection{Host: "other.example.com", User: "tester"}
	if err := remotefs.RecordAttempt(active, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := remotefs.RecordAttempt(other, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := r.panel.connectRemote(fakeConnectedClient(), active); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	r.openConnectionMenu()
	row := historyRowFor(t, r, other)
	r.connectionMenuTable.Select(row, connectionMenuColLabel)

	r.captureConnectionMenuKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))

	if r.panel.remote == nil {
		t.Error("editing an unrelated history row disconnected the active panel")
	}
}

// TestClickingTheEditCellOpensEditPrefilled is the mouse equivalent of
// TestPressingEOnANonActiveHistoryRowOpensEditPrefilled.
func TestClickingTheEditCellOpensEditPrefilled(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	r.openConnectionMenu()
	row := historyRowFor(t, r, conn)

	editCell := r.connectionMenuTable.GetCell(row, connectionMenuColEject)
	if !strings.Contains(editCell.Text, connectionHistoryEditGlyph) {
		t.Fatalf("non-active row's own eject-column cell = %q, want it to carry the edit glyph", editCell.Text)
	}
	editCell.Clicked()

	if r.activePage != connectDialogPage {
		t.Errorf("activePage = %q, want the Connect dialog open", r.activePage)
	}
	if got := r.connectHostField.GetText(); got != conn.Host {
		t.Errorf("Host field = %q, want %q", got, conn.Host)
	}
}

// TestClickingTheEjectCellDisconnectsTheActiveConnection is the mouse
// equivalent of TestPressingEOnTheActiveConnectionRowDisconnects — the
// small "⏏" this project's own doc comments compare to a USB drive's
// own eject icon, replacing what used to be a whole separate
// "Disconnect (...)" list item (see
// TestRenderConnectionMenuOnAConnectedPanelListsNewConnectionThenTheActiveEntry).
func TestClickingTheEjectCellDisconnectsTheActiveConnection(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := r.panel.connectRemote(fakeConnectedClient(), conn); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	r.openConnectionMenu()
	row := r.connectionMenuActiveRow

	r.connectionMenuTable.GetCell(row, connectionMenuColEject).Clicked()

	if r.panel.remote != nil {
		t.Error("panel is still connected after clicking the eject cell")
	}
	if r.activePage == connectionMenuPage {
		t.Error("activePage still the dropdown after ejecting — want it dismissed, matching the old Disconnect row's own behavior")
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
