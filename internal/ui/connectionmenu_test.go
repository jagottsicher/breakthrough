package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
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

// TestRenderConnectionMenuOnAConnectedPanelListsTheActiveEntryThenNewConnection
// pins two things at once: there's no longer a dedicated "Disconnect
// (...)" row of its own — disconnecting instead happens via the
// active connection's own eject cell (see
// TestPressingEOnTheActiveConnectionRowDisconnects/
// TestClickingTheEjectCellDisconnectsTheActiveConnection) — and
// "New connection…" is a trailing row, after every history entry, the
// same position tabswitcher.go's own "New tab" occupies.
func TestRenderConnectionMenuOnAConnectedPanelListsTheActiveEntryThenNewConnection(t *testing.T) {
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
		t.Fatalf("rows = %v, want exactly one history row for the active connection plus \"New connection…\"", texts)
	}
	if !strings.Contains(texts[0], conn.Label()) {
		t.Errorf("row 0 label = %q, want the active connection's own history row first", texts[0])
	}
	if !strings.Contains(texts[1], "New connection") {
		t.Errorf("row 1 label = %q, want \"New connection…\" trailing, after every history entry", texts[1])
	}
	ejectCell := r.connectionMenuTable.GetCell(0, connectionMenuColEject)
	if !strings.Contains(ejectCell.Text, connectionHistoryEjectGlyph) {
		t.Errorf("row 0 eject cell = %q, want it to carry the eject glyph", ejectCell.Text)
	}
	if r.connectionMenuActiveRow != 0 {
		t.Errorf("connectionMenuActiveRow = %d, want 0", r.connectionMenuActiveRow)
	}
}

// TestRenderConnectionMenuAppendsTheIdentifierToTheLabel pins Phase
// 1's own display rule: an entry's Identifier (see remotefs.Connection's
// own doc comment) shows up appended to its row's label, via
// connectionMenuIdentifierSuffix — not folded into Connection.Label()
// itself, which stays shared with the panel header button and
// activity-log messages (see that function's own doc comment).
func TestRenderConnectionMenuAppendsTheIdentifierToTheLabel(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester", Identifier: "build-1"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	r.renderConnectionMenu()

	texts := connectionMenuLabelTexts(r.connectionMenuTable)
	if len(texts) == 0 || !strings.Contains(texts[0], "build-1") {
		t.Errorf("row 0 label = %q, want it to mention the Identifier \"build-1\"", texts[0])
	}
}

// TestRenderConnectionMenuOmitsTheIdentifierSuffixWhenUnset pins the
// other half: an entry nobody ever named looks exactly as it did
// before this field existed, no trailing empty parentheses.
func TestRenderConnectionMenuOmitsTheIdentifierSuffixWhenUnset(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	r.renderConnectionMenu()

	texts := connectionMenuLabelTexts(r.connectionMenuTable)
	if len(texts) == 0 || texts[0] != conn.Label() {
		t.Errorf("row 0 label = %q, want exactly %q with no suffix", texts[0], conn.Label())
	}
}

// TestToggleConnectionMenuExpandedFlipsTheStoredSetting pins the same
// "flip, persist, apply if currently open" shape toggleSplitStacked's
// own test establishes for an identical kind of stored display
// preference.
func TestToggleConnectionMenuExpandedFlipsTheStoredSetting(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	if r.settings.ConnectionMenuExpanded {
		t.Fatal("ConnectionMenuExpanded should start false (the default)")
	}

	r.toggleConnectionMenuExpanded()
	if !r.settings.ConnectionMenuExpanded {
		t.Error("toggleConnectionMenuExpanded did not flip the setting on")
	}

	r.toggleConnectionMenuExpanded()
	if r.settings.ConnectionMenuExpanded {
		t.Error("a second toggle did not flip it back off")
	}
}

// TestConnectionMenuExpandedSurvivesIntoTheConfigFile mirrors
// TestSplitOrientationSurvivesIntoTheConfigFile (split_test.go) for
// this setting.
func TestConnectionMenuExpandedSurvivesIntoTheConfigFile(t *testing.T) {
	first := t.TempDir()
	configPath := isolateUserConfigFile(t)
	r, err := NewRoot(tview.NewApplication(), first)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.toggleConnectionMenuExpanded()

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading the config back: %v", err)
	}
	if got := string(data); !strings.Contains(got, "connection_menu_expanded = true") {
		t.Errorf("config = %q, want it to record connection_menu_expanded", got)
	}
}

// TestRenderConnectionMenuExpandedSplitsTheLabelIntoFourColumns pins
// Phase 2's own column rework: User | @ | Host/IP | Identifier,
// instead of collapsed mode's single Label column.
func TestRenderConnectionMenuExpandedSplitsTheLabelIntoFourColumns(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", Port: 2222, User: "tester", Identifier: "build-1"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	r.setConnectionMenuExpanded(true)

	r.renderConnectionMenu()

	table := r.connectionMenuTable
	if got, want := table.GetCell(0, connectionMenuColUser).Text, "tester"; got != want {
		t.Errorf("User column = %q, want %q", got, want)
	}
	if got, want := table.GetCell(0, connectionMenuColAt).Text, "@"; got != want {
		t.Errorf("@ column = %q, want %q", got, want)
	}
	if got, want := table.GetCell(0, connectionMenuColHost).Text, "example.com:2222"; got != want {
		t.Errorf("Host/IP column = %q, want %q", got, want)
	}
	if got, want := table.GetCell(0, connectionMenuColIdentifier).Text, "build-1"; got != want {
		t.Errorf("Identifier column = %q, want %q", got, want)
	}
}

// TestRenderConnectionMenuExpandedOmitsAtForAUserlessConnection pins
// connectionMenuAtColumnText's own rule: no User, no "@" — the exact
// same condition Connection.Label() already uses to decide whether to
// show "@" at all.
func TestRenderConnectionMenuExpandedOmitsAtForAUserlessConnection(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	r.setConnectionMenuExpanded(true)

	r.renderConnectionMenu()

	if got := r.connectionMenuTable.GetCell(0, connectionMenuColAt).Text; got != "" {
		t.Errorf("@ column = %q, want empty for a connection with no User", got)
	}
}

// TestClickingTheEjectCellInExpandedModeDisconnectsNotConnects pins the
// regression connectionMenuEjectColumn/connectionMenuRemoveColumn
// exist to prevent: in expanded mode, Eject/Remove sit at a different
// absolute column index than in collapsed mode (four label columns
// precede them instead of one), so a click there must still route to
// disconnectConnectionRow — not fall through to the default "connect"
// case the way comparing against the collapsed-mode constants directly
// would.
func TestClickingTheEjectCellInExpandedModeDisconnectsNotConnects(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := r.panel.connectRemote(fakeConnectedClient(), conn); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	r.setConnectionMenuExpanded(true)
	r.renderConnectionMenu()

	ejectCell := r.connectionMenuTable.GetCell(0, connectionMenuColExpandedEject)
	if !strings.Contains(ejectCell.Text, connectionHistoryEjectGlyph) {
		t.Fatalf("expanded eject cell = %q, want it to carry the eject glyph", ejectCell.Text)
	}
	ejectCell.Clicked()

	if r.panel.remote != nil {
		t.Error("clicking the expanded-mode eject cell did not disconnect — it fell through to connect/reconnect instead")
	}
}

// TestClickingTheRemoveCellInExpandedModeRemovesNotConnects is the
// companion regression test for the Remove column's own shifted index.
func TestClickingTheRemoveCellInExpandedModeRemovesNotConnects(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	r.setConnectionMenuExpanded(true)
	r.renderConnectionMenu()

	removeCell := r.connectionMenuTable.GetCell(0, connectionMenuColExpandedRemove)
	removeCell.Clicked()

	history, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("history = %+v, want the entry removed, not reconnected to", history)
	}
}

// TestRenderConnectionMenuTitleBarShowsTheExpandGlyphWhenCollapsed and
// its companion below pin the title bar's own toggle glyph (see
// renderConnectionMenuTitleBar's own doc comment on the direction
// convention).
func TestRenderConnectionMenuTitleBarShowsTheExpandGlyphWhenCollapsed(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	r.renderConnectionMenuTitleBar(40)
	if got := r.connectionMenuTitleBar.GetText(true); !strings.Contains(got, connectionMenuExpandGlyph) {
		t.Errorf("title bar = %q, want it to carry the expand glyph %q while collapsed", got, connectionMenuExpandGlyph)
	}
}

func TestRenderConnectionMenuTitleBarShowsTheCollapseGlyphWhenExpanded(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	r.setConnectionMenuExpanded(true)
	r.renderConnectionMenuTitleBar(40)
	if got := r.connectionMenuTitleBar.GetText(true); !strings.Contains(got, connectionMenuCollapseGlyph) {
		t.Errorf("title bar = %q, want it to carry the collapse glyph %q while expanded", got, connectionMenuCollapseGlyph)
	}
}

// TestPositionConnectionMenuSpansTheFullScreenWidthWhenExpanded pins
// the width strategy itself: expanded mode must use clampToScreen
// (the whole terminal), not clampToPanel (only the active panel) —
// see positionConnectionMenu's own doc comment for why collapsed mode
// couldn't simply reuse clampToPanel's own result here.
func TestPositionConnectionMenuSpansTheFullScreenWidthWhenExpanded(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	r.setConnectionMenuExpanded(true)
	r.renderConnectionMenu()

	r.positionConnectionMenu()

	_, _, screenWidth, _ := r.GetRect()
	_, _, gotWidth, _ := r.connectionMenuLayout.GetRect()
	if gotWidth != screenWidth {
		t.Errorf("connectionMenuLayout width = %d, want the full screen width %d", gotWidth, screenWidth)
	}
}

// TestRenderConnectionMenuSelectsTheActiveConnectionRowByDefault pins
// the same default-selection rule openTabSwitcher's own
// r.openTabSwitcher(r.activeTab) already establishes for the tab
// switcher: land on whatever's already relevant (the active
// connection's own row here), never on the trailing "New connection"
// fallback action, per the user's own explicit request that the two
// dropdowns behave the same way.
func TestRenderConnectionMenuSelectsTheActiveConnectionRowByDefault(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	other := remotefs.Connection{Host: "other.example.com", User: "tester"}
	active := remotefs.Connection{Host: "active.example.com", User: "tester"}
	if err := remotefs.RecordAttempt(other, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := remotefs.RecordAttempt(active, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := r.panel.connectRemote(fakeConnectedClient(), active); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}

	r.renderConnectionMenu()

	row, _ := r.connectionMenuTable.GetSelection()
	if row != r.connectionMenuActiveRow {
		t.Errorf("default selected row = %d, want the active connection's own row %d", row, r.connectionMenuActiveRow)
	}
}

// TestRenderConnectionMenuSelectsRowZeroByDefaultWithoutAnActiveConnection
// is the flip side: an unconnected panel has no active row to prefer,
// so the default lands on row 0 — the first history entry, exactly
// like the tab switcher's own r.activeTab defaults to 0 for the first
// tab when nothing else is more specifically "active".
func TestRenderConnectionMenuSelectsRowZeroByDefaultWithoutAnActiveConnection(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	if err := remotefs.RecordAttempt(remotefs.Connection{Host: "example.com", User: "tester"}, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	r.renderConnectionMenu()

	row, _ := r.connectionMenuTable.GetSelection()
	if row != 0 {
		t.Errorf("default selected row = %d, want 0 (no active connection to prefer)", row)
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
	r.connectionMenuTable.Select(1, connectionMenuColLabel) // "New connection…" trails the one history row

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

func TestDisconnectConnectionRowLogsAnAction(t *testing.T) {
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
	readLog := attachTestActivityLog(t, r)

	if !r.disconnectConnectionRow(r.connectionMenuActiveRow) {
		t.Fatal("disconnectConnectionRow did not fire for the active row")
	}

	got := readLog()
	if !strings.Contains(got, string(activitylog.CategoryRemote)) || !strings.Contains(got, "disconnected from tester@example.com") {
		t.Errorf("log = %q, want a remote entry about the disconnection", got)
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
	conn := remotefs.Connection{Host: "example.com", Port: 2222, User: "tester", Identifier: "build-1"}
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
	if got := r.connectIdentifierField.GetText(); got != conn.Identifier {
		t.Errorf("Identifier field = %q, want %q", got, conn.Identifier)
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
