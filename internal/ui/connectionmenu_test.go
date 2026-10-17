package ui

import (
	"fmt"
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

// connectionMenuLabelTexts reconstructs every row's own "user@host"
// label by joining the User/@/Host-IP columns back together — the
// Table equivalent of the old List's itemTexts, now that each row's
// label is three independent cells (see connectionMenuCol* and that
// const block's own doc comment on why) rather than one string. Omits
// Identifier deliberately: it's a fourth, expanded-mode-only column,
// not part of what used to be "the label" — tests that care about it
// read connectionMenuColIdentifier directly instead.
// Starts at row 1, skipping row 0 — the column header Phase 3 added
// (see renderConnectionMenu's own doc comment) — so every existing
// caller's texts[0] still means "the first real row", exactly as
// before the header existed, rather than needing every call site
// updated for the shift.
func connectionMenuLabelTexts(table *tview.Table) []string {
	rows := table.GetRowCount()
	if rows == 0 {
		return nil
	}
	texts := make([]string, rows-1)
	for i := 1; i < rows; i++ {
		user := table.GetCell(i, connectionMenuColUser)
		at := table.GetCell(i, connectionMenuColAt)
		host := table.GetCell(i, connectionMenuColHost)
		if user == nil {
			continue
		}
		text := user.Text
		if at != nil {
			text += at.Text
		}
		if host != nil {
			text += host.Text
		}
		texts[i-1] = text
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

// cellBackgroundColor is cellTextColor's own counterpart for the
// background half of a cell's Style.
func cellBackgroundColor(cell *tview.TableCell) tcell.Color {
	_, bg, _ := cell.Style.Decompose()
	return bg
}

// connectionMenuStripMnemonicMarkup strips the "[:ButtonBackground:]"
// mnemonic markup connectionMenuSetHeaderCell wraps its own first rune
// in (see that method's own doc comment), leaving everything else —
// including a trailing sortArrow suffix, if any — untouched.
func connectionMenuStripMnemonicMarkup(text string) string {
	text = strings.ReplaceAll(text, "[-:-:-]", "")
	if i := strings.Index(text, "]"); i >= 0 && strings.HasPrefix(text, "[:") {
		text = text[i+1:]
	}
	return text
}

// connectionMenuHeaderText is connectionMenuStripMnemonicMarkup's own
// further reduction to just the plain word, also dropping any trailing
// sortArrow suffix — for tests that only care about the label itself,
// not whether (or which way) that column happens to be sorted right
// now.
func connectionMenuHeaderText(text string) string {
	text = connectionMenuStripMnemonicMarkup(text)
	return strings.NewReplacer(" ↑", "", " ↓", "").Replace(text)
}

// TestRenderConnectionMenuHeaderRowShowsColumnLabels pins Phase 3's
// own first step: a real column header, labelling User/Host-IP in
// collapsed mode and adding Identifier once expanded — see
// renderConnectionMenu's own doc comment on why this exists
// (the user's own explicit report: "die Spalten haben keine
// Überschrift").
func TestRenderConnectionMenuHeaderRowShowsColumnLabels(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	r.renderConnectionMenu()

	table := r.connectionMenuTable
	if got, want := connectionMenuHeaderText(table.GetCell(0, connectionMenuColUser).Text), "User"; got != want {
		t.Errorf("header User column = %q, want %q", got, want)
	}
	if got, want := connectionMenuHeaderText(table.GetCell(0, connectionMenuColHost).Text), "Host/IP"; got != want {
		t.Errorf("header Host/IP column = %q, want %q", got, want)
	}
	// connectionMenuColIdentifier's own raw index coincides with
	// connectionMenuEjectColumn(false) in collapsed mode (same reason
	// connectionMenuRemoveColumn(false)+1 is the right column-count
	// check elsewhere in this file) — the header's blank eject cell
	// legitimately occupies that index, so column count is what
	// actually distinguishes "no Identifier column" here.
	if got, want := table.GetColumnCount(), connectionMenuRemoveColumn(false)+1; got != want {
		t.Errorf("header column count = %d, want %d (no Identifier column while collapsed)", got, want)
	}

	r.setConnectionMenuExpanded(true)
	r.renderConnectionMenu()
	if got, want := connectionMenuHeaderText(table.GetCell(0, connectionMenuColIdentifier).Text), "Identifier"; got != want {
		t.Errorf("header Identifier column (expanded) = %q, want %q", got, want)
	}
}

// TestRenderConnectionMenuHeaderHighlightsItsOwnMnemonicLetter pins the
// user's own explicit request: the U/H/I a header sorts by (see
// captureConnectionMenuKey) gets the same ButtonBackground chip every
// other keyboard shortcut in this app already uses (bottombar.go's own
// highlightKey) — colored in place as the label's own first rune,
// since that's always the mnemonic itself.
func TestRenderConnectionMenuHeaderHighlightsItsOwnMnemonicLetter(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	r.renderConnectionMenu()

	want := fmt.Sprintf("[:%s:]U[-:-:-]ser", colorTag(r.theme.ButtonBackground))
	if got := r.connectionMenuTable.GetCell(0, connectionMenuColUser).Text; got != want {
		t.Errorf("header User column = %q, want %q", got, want)
	}
}

// TestConnectionMenuSelectionChangeDoesNotEraseTheHostIdentifierHeaderMnemonic
// pins a real, user-reported bug: connectionMenuRepaintLabelGroups runs
// on every selection change (including the one table.Select fires at
// the end of renderConnectionMenu itself) and used to loop over row 0
// too, overwriting Host-IP/Identifier's own embedded mnemonic markup
// with a plain SetBackgroundColor/SetTextColor call — User never
// showed the bug since it isn't one of the columns that function ever
// touches, which is exactly why only "U" stayed highlighted while "H"
// and "I" silently lost theirs.
func TestConnectionMenuSelectionChangeDoesNotEraseTheHostIdentifierHeaderMnemonic(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester", Identifier: "build-1"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	r.setConnectionMenuExpanded(true)
	r.renderConnectionMenu()

	// Force another selection change after the render, the same way a
	// real arrow-key press or click would — this is what used to erase
	// the markup.
	r.connectionMenuTable.Select(1, connectionMenuEjectColumn(true))

	wantHost := fmt.Sprintf("[:%s:]H[-:-:-]ost/IP", colorTag(r.theme.ButtonBackground))
	if got := r.connectionMenuTable.GetCell(0, connectionMenuColHost).Text; got != wantHost {
		t.Errorf("header Host/IP column = %q, want %q (markup erased by the selection change)", got, wantHost)
	}
	wantIdentifier := fmt.Sprintf("[:%s:]I[-:-:-]dentifier", colorTag(r.theme.ButtonBackground))
	if got := r.connectionMenuTable.GetCell(0, connectionMenuColIdentifier).Text; got != wantIdentifier {
		t.Errorf("header Identifier column = %q, want %q (markup erased by the selection change)", got, wantIdentifier)
	}
}

// TestRenderConnectionMenuHeaderRowIsNotSelectable pins the other half
// of Phase 3's first step: the header is inert for now (sorting is a
// later step) — every cell in row 0 must be unreachable by keyboard
// navigation, so Up from the first real row never lands there and
// Right/Left skip it entirely, the same way @/Host-IP already skip
// within the label group.
func TestRenderConnectionMenuHeaderRowIsNotSelectable(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	if err := remotefs.RecordAttempt(remotefs.Connection{Host: "example.com", User: "tester"}, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	r.renderConnectionMenu()

	for _, col := range []int{connectionMenuColUser, connectionMenuColAt, connectionMenuColHost, connectionMenuEjectColumn(false), connectionMenuRemoveColumn(false)} {
		cell := r.connectionMenuTable.GetCell(0, col)
		if cell == nil {
			t.Fatalf("header column %d is nil", col)
		}
		if !cell.NotSelectable {
			t.Errorf("header column %d is selectable, want inert for now (sorting is a later step)", col)
		}
	}
}

// TestClickingAHeaderCellDoesNotMoveTheSelection pins a real,
// user-reported bug: NotSelectable alone does NOT stop a click from
// moving the real selection there — tview's own Table.MouseHandler
// calls t.Select(row, column) on any clicked cell whose own Clicked
// func is nil, regardless of NotSelectable (verified against table.go,
// not guessed: NotSelectable only gates the keyboard forward/backward
// scan). Without connectionMenuSetHeaderCell's own SetClickedFunc
// returning true, clicking "User" in the header moved selectedRow to
// 0, and connectionMenuRepaintLabelGroups then highlighted the header
// itself.
func TestClickingAHeaderCellDoesNotMoveTheSelection(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	r.renderConnectionMenu()

	beforeRow, beforeCol := r.connectionMenuTable.GetSelection()

	// The return value itself is the actual contract tview's own
	// MouseHandler relies on (true = "noSelect", see table.go's own
	// MouseLeftClick case) — asserting it directly, not just the
	// selection's own before/after state, pins the real mechanism
	// rather than one of its effects.
	if noSelect := r.connectionMenuTable.GetCell(0, connectionMenuColUser).Clicked(); !noSelect {
		t.Error("header cell's own Clicked() returned false, want true (tview's own \"noSelect\" signal)")
	}

	afterRow, afterCol := r.connectionMenuTable.GetSelection()
	if afterRow != beforeRow || afterCol != beforeCol {
		t.Errorf("selection moved to (%d, %d) after clicking the header, want it to stay at (%d, %d)", afterRow, afterCol, beforeRow, beforeCol)
	}
}

// TestClickingTheUserHeaderSortsByUserAscendingThenDescending pins
// setConnectionMenuSortKey's own "click a new column: ascending; click
// the active one again: reverse" convention (mirroring
// Panel.setSortKey), driven through the header cell's own
// SetClickedFunc exactly as a real click would.
func TestClickingTheUserHeaderSortsByUserAscendingThenDescending(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	for _, u := range []string{"web128", "jens", "pi"} {
		if err := remotefs.RecordAttempt(remotefs.Connection{Host: u + ".example.com", User: u}, false); err != nil {
			t.Fatalf("RecordAttempt: %v", err)
		}
	}
	r.renderConnectionMenu()

	r.connectionMenuTable.GetCell(0, connectionMenuColUser).Clicked()
	texts := connectionMenuLabelTexts(r.connectionMenuTable)
	if len(texts) < 3 || texts[0][:4] != "jens" {
		t.Fatalf("after ascending sort, rows = %v, want jens first", texts)
	}
	if got, want := connectionMenuStripMnemonicMarkup(r.connectionMenuTable.GetCell(0, connectionMenuColUser).Text), "User ↑"; got != want {
		t.Errorf("User header = %q, want the ascending sortArrow suffix", got)
	}

	r.connectionMenuTable.GetCell(0, connectionMenuColUser).Clicked()
	texts = connectionMenuLabelTexts(r.connectionMenuTable)
	if len(texts) < 3 || texts[0][:4] != "web1" {
		t.Fatalf("after a second click (descending), rows = %v, want web128 first", texts)
	}
	if got, want := connectionMenuStripMnemonicMarkup(r.connectionMenuTable.GetCell(0, connectionMenuColUser).Text), "User ↓"; got != want {
		t.Errorf("User header = %q, want the descending sortArrow suffix", got)
	}
}

// TestPressingHSortsByHost pins the keyboard equivalent of clicking the
// Host/IP header — captureConnectionMenuKey's own "H" handling.
func TestPressingHSortsByHost(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	for _, h := range []string{"zzz.example.com", "aaa.example.com"} {
		if err := remotefs.RecordAttempt(remotefs.Connection{Host: h, User: "tester"}, false); err != nil {
			t.Fatalf("RecordAttempt: %v", err)
		}
	}
	r.renderConnectionMenu()

	got := r.captureConnectionMenuKey(tcell.NewEventKey(tcell.KeyRune, 'H', tcell.ModNone))
	if got != nil {
		t.Error("captureConnectionMenuKey did not consume \"H\"")
	}

	texts := connectionMenuLabelTexts(r.connectionMenuTable)
	if len(texts) < 2 || !strings.Contains(texts[0], "aaa.example.com") {
		t.Errorf("rows = %v, want aaa.example.com sorted first", texts)
	}
}

// TestPressingIWhileCollapsedDoesNothing pins
// setConnectionMenuSortKey's own no-op guard: there's no Identifier
// column to sort by while collapsed, so "I" must leave the history
// order untouched rather than silently setting a sort key that has no
// visible column to show its own arrow on.
func TestPressingIWhileCollapsedDoesNothing(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	for _, c := range []remotefs.Connection{
		{Host: "b.example.com", User: "tester", Identifier: "zzz"},
		{Host: "a.example.com", User: "tester", Identifier: "aaa"},
	} {
		if err := remotefs.RecordAttempt(c, false); err != nil {
			t.Fatalf("RecordAttempt: %v", err)
		}
	}
	r.renderConnectionMenu()
	before := connectionMenuLabelTexts(r.connectionMenuTable)

	r.captureConnectionMenuKey(tcell.NewEventKey(tcell.KeyRune, 'I', tcell.ModNone))

	if r.connectionMenuSortKey != connectionMenuSortNone {
		t.Errorf("connectionMenuSortKey = %v, want connectionMenuSortNone while collapsed", r.connectionMenuSortKey)
	}
	after := connectionMenuLabelTexts(r.connectionMenuTable)
	if len(after) != len(before) || after[0] != before[0] {
		t.Errorf("row order = %v, want it unchanged by \"I\" while collapsed (got %v before)", after, before)
	}
}

// TestOpenConnectionMenuResetsAnyPreviousSort pins the plan's own
// "transient, not persisted" decision for sorting: a sort set during
// one open must not survive into the next.
func TestOpenConnectionMenuResetsAnyPreviousSort(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	r.connectionMenuSortKey = connectionMenuSortHost
	r.connectionMenuSortDescending = true

	r.openConnectionMenu()

	if r.connectionMenuSortKey != connectionMenuSortNone {
		t.Errorf("connectionMenuSortKey = %v, want connectionMenuSortNone reset on open", r.connectionMenuSortKey)
	}
	if r.connectionMenuSortDescending {
		t.Error("connectionMenuSortDescending = true, want reset to false on open")
	}
}

func TestRenderConnectionMenuOnALocalPanelOffersOnlyNewConnection(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	r.renderConnectionMenu()

	texts := connectionMenuLabelTexts(r.connectionMenuTable)
	if len(texts) != 1 || !strings.Contains(texts[0], "New") {
		t.Errorf("rows = %v, want exactly one \"New\" row for a local panel", texts)
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
	if !strings.Contains(texts[1], "New") {
		t.Errorf("row 1 label = %q, want the trailing \"New\" row, after every history entry", texts[1])
	}
	ejectCell := r.connectionMenuTable.GetCell(1, connectionMenuEjectColumn(false))
	if !strings.Contains(ejectCell.Text, connectionHistoryEjectGlyph) {
		t.Errorf("row 1 eject cell = %q, want it to carry the eject glyph", ejectCell.Text)
	}
	if r.connectionMenuActiveRow != 1 {
		t.Errorf("connectionMenuActiveRow = %d, want 1 (the first real row, after the header)", r.connectionMenuActiveRow)
	}
}

// TestRenderConnectionMenuCollapsedNeverShowsTheIdentifierColumn pins
// the user's own explicit, later-revised design: collapsed mode is a
// real User/@/Host-IP column table (not a single merged string — see
// connectionMenuLabelTexts' own doc comment), but Identifier is only
// ever a column at all in expanded mode. An Identifier set on the
// connection must not leak into collapsed mode in any form — neither
// appended to another column's own text, nor as a column of its own.
// connectionMenuColIdentifier itself is not a useful index to probe
// here: its raw number (3) coincides with connectionMenuEjectColumn's
// own collapsed-mode value, so GetCell at that index legitimately
// returns the Eject/Edit cell instead of nil — the real column count
// is what actually distinguishes the two modes.
func TestRenderConnectionMenuCollapsedNeverShowsTheIdentifierColumn(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester", Identifier: "build-1"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	r.renderConnectionMenu()

	texts := connectionMenuLabelTexts(r.connectionMenuTable)
	if len(texts) == 0 || strings.Contains(texts[0], "build-1") {
		t.Errorf("row 0 label = %q, want it to never mention the Identifier in collapsed mode", texts[0])
	}
	if got, want := r.connectionMenuTable.GetColumnCount(), connectionMenuRemoveColumn(false)+1; got != want {
		t.Errorf("column count = %d, want %d (User/@/Host-IP/Eject/Remove, no Identifier column)", got, want)
	}
}

// TestRenderConnectionMenuCollapsedLabelIsExactlyUserAtHost pins the
// reconstructed label's own exact shape: collapsed mode's three real
// columns (User/@/Host-IP) joined back together must read identically
// to Connection.Label() — same content, just split across cells
// instead of one string.
func TestRenderConnectionMenuCollapsedLabelIsExactlyUserAtHost(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	r.renderConnectionMenu()

	texts := connectionMenuLabelTexts(r.connectionMenuTable)
	if len(texts) == 0 || texts[0] != conn.Label() {
		t.Errorf("row 0 label = %q, want exactly %q", texts[0], conn.Label())
	}
}

// TestConnectionMenuSelectingTheLabelGroupHighlightsAllItsColumns pins
// the user's own explicit, three-times-repeated request: the
// highlighted background (and matching white-ish foreground — see
// connectionMenuRepaintLabelGroups' own doc comment) must span every
// visible column of the label group (User/@/Host-IP, Identifier too
// when expanded), not just the single User cell tview's own native
// per-cell selection would otherwise paint alone.
func TestConnectionMenuSelectingTheLabelGroupHighlightsAllItsColumns(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	r.setConnectionMenuExpanded(true)

	r.renderConnectionMenu()
	r.connectionMenuTable.Select(1, connectionMenuColUser)

	for _, col := range []int{connectionMenuColAt, connectionMenuColHost, connectionMenuColIdentifier} {
		cell := r.connectionMenuTable.GetCell(1, col)
		if got, want := cellBackgroundColor(cell), r.theme.SelectionBackground; got != want {
			t.Errorf("column %d background = %v, want SelectionBackground %v", col, got, want)
		}
		if got, want := cellTextColor(cell), r.theme.TextColor; got != want {
			t.Errorf("column %d text color = %v, want TextColor %v", col, got, want)
		}
	}
}

// TestConnectionMenuSelectingEjectDoesNotHighlightTheLabelGroup is the
// negative case: once the selection has moved off the label group
// (onto ✎/✕), that row's own @/Host-IP/Identifier cells must revert to
// their real, row-specific color (connectionMenuRowColors) and the
// table's own normal background — not stay highlighted just because
// they're still in the same row.
func TestConnectionMenuSelectingEjectDoesNotHighlightTheLabelGroup(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	r.renderConnectionMenu()
	r.connectionMenuTable.Select(1, connectionMenuEjectColumn(false))

	atCell := r.connectionMenuTable.GetCell(1, connectionMenuColAt)
	if got, want := cellBackgroundColor(atCell), r.theme.SurfaceBackground; got != want {
		t.Errorf("@ column background = %v, want the table's own normal SurfaceBackground %v, not left highlighted", got, want)
	}
	if got, want := cellTextColor(atCell), r.connectionMenuRowColors[1]; got != want {
		t.Errorf("@ column text color = %v, want the row's own real color %v restored", got, want)
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
	if got, want := table.GetCell(1, connectionMenuColUser).Text, "tester"; got != want {
		t.Errorf("User column = %q, want %q", got, want)
	}
	if got, want := table.GetCell(1, connectionMenuColAt).Text, "@"; got != want {
		t.Errorf("@ column = %q, want %q", got, want)
	}
	if got, want := table.GetCell(1, connectionMenuColHost).Text, "example.com:2222"; got != want {
		t.Errorf("Host/IP column = %q, want %q", got, want)
	}
	if got, want := table.GetCell(1, connectionMenuColIdentifier).Text, "build-1"; got != want {
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

	if got := r.connectionMenuTable.GetCell(1, connectionMenuColAt).Text; got != "" {
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

	ejectCell := r.connectionMenuTable.GetCell(1, connectionMenuEjectColumn(true))
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

	removeCell := r.connectionMenuTable.GetCell(1, connectionMenuRemoveColumn(true))
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
	// A real, generous rect: the default test rect is tiny (15 wide),
	// which would make this assertion pass even if expanded mode fell
	// back to collapsed-mode sizing by accident, since both would
	// happen to clamp to roughly the same small number. 120 is wide
	// enough that collapsed mode's own much narrower content-driven
	// width (a single "tester@example.com" label) could never
	// coincidentally match it.
	r.SetRect(0, 0, 120, 30)
	r.setConnectionMenuExpanded(true)
	r.renderConnectionMenu()

	r.positionConnectionMenu()

	if _, _, gotWidth, _ := r.connectionMenuLayout.GetRect(); gotWidth != 120 {
		t.Errorf("connectionMenuLayout width = %d, want the full screen width 120", gotWidth)
	}
}

// TestPositionConnectionMenuStaysPanelWidthWhenCollapsed is the
// companion negative case: collapsed mode must still use clampToPanel,
// not accidentally inherit the expanded path's full-screen width.
func TestPositionConnectionMenuStaysPanelWidthWhenCollapsed(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	r.SetRect(0, 0, 120, 30)
	r.renderConnectionMenu()

	r.positionConnectionMenu()

	if _, _, gotWidth, _ := r.connectionMenuLayout.GetRect(); gotWidth == 120 {
		t.Error("connectionMenuLayout width = 120, want collapsed mode to stay content-sized, not full screen width")
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
func TestRenderConnectionMenuSelectsTheFirstRealRowByDefaultWithoutAnActiveConnection(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	if err := remotefs.RecordAttempt(remotefs.Connection{Host: "example.com", User: "tester"}, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	r.renderConnectionMenu()

	row, _ := r.connectionMenuTable.GetSelection()
	if row != 1 {
		t.Errorf("default selected row = %d, want 1 (no active connection to prefer, row 0 is the header)", row)
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

	failedColor := cellTextColor(r.connectionMenuTable.GetCell(failedRow, connectionMenuColUser))
	if failedColor != r.theme.CriticalText {
		t.Errorf("failed entry color = %v, want %v (theme.CriticalText)", failedColor, r.theme.CriticalText)
	}
	okColor := cellTextColor(r.connectionMenuTable.GetCell(okRow, connectionMenuColUser))
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
	got := cellTextColor(r.connectionMenuTable.GetCell(row, connectionMenuColUser))
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
	got := cellTextColor(r.connectionMenuTable.GetCell(row, connectionMenuColUser))
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
	r.connectionMenuTable.Select(row, connectionMenuColUser)

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
	r.connectionMenuTable.Select(2, connectionMenuColUser) // "New connection…" trails the one history row (row 0 is the header, row 1 the history row)

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

	r.connectionMenuTable.GetCell(row, connectionMenuRemoveColumn(false)).Clicked()

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
	r.connectionMenuTable.Select(r.connectionMenuActiveRow, connectionMenuColUser)

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
	r.connectionMenuTable.Select(r.connectionMenuActiveRow, connectionMenuColUser)
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
	r.connectionMenuTable.Select(row, connectionMenuColUser)

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
	r.connectionMenuTable.Select(row, connectionMenuColUser)

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

	editCell := r.connectionMenuTable.GetCell(row, connectionMenuEjectColumn(false))
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

	r.connectionMenuTable.GetCell(row, connectionMenuEjectColumn(false)).Clicked()

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
