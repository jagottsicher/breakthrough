package ui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/multiplex"
)

// isolateSessionsList swaps listMultiplexSessions for the duration of
// one test, restoring the original via t.Cleanup — the same "swap the
// package-level var, restore in Cleanup" idiom isolateFirewallRead
// already establishes in firewall_test.go.
func isolateSessionsList(t *testing.T, sessions []multiplex.Session, err error) {
	t.Helper()
	orig := listMultiplexSessions
	t.Cleanup(func() { listMultiplexSessions = orig })
	listMultiplexSessions = func() ([]multiplex.Session, error) { return sessions, err }
}

func newTestRootForSessions(t *testing.T) *Root {
	t.Helper()
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 200, 50) // wide enough that showError never word-wraps text this test asserts on verbatim
	return r
}

func TestOpenSessionsShowsThePage(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, nil, nil)

	r.openSessions()

	if r.activePage != sessionsPage {
		t.Fatalf("activePage = %q, want the Sessions screen", r.activePage)
	}
}

func TestReloadSessionsShowsAnErrorWhenNeitherBackendIsInstalled(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, nil, errors.New("multiplex: neither screen nor tmux is installed"))

	r.openSessions()

	if r.sessionsErr == nil {
		t.Error("sessionsErr is nil, want the read error preserved")
	}
}

func TestReloadSessionsWithNoSessionsIsNotAnError(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, nil, nil)

	r.openSessions()

	if r.sessionsErr != nil {
		t.Errorf("sessionsErr = %v, want nil for an empty-but-successful read", r.sessionsErr)
	}
}

// TestSessionsHintRButtonClickReloadsTheList pins the user's own
// explicit further request that these colored keys be real, clickable
// buttons: clicking "r" has to actually re-read the session list, not
// just look like a button — the same real-click contract
// TestActivityLogHintRButtonClickReloadsTheLog already pins for that
// screen.
func TestSessionsHintRButtonClickReloadsTheList(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, nil, nil)
	r.openSessions()
	if got := r.sessionsTable.GetRowCount(); got != 2 {
		t.Fatalf("setup: table has %d rows, want 2 (header + placeholder, no sessions yet)", got)
	}

	isolateSessionsList(t, []multiplex.Session{{Name: "work", Backend: multiplex.BackendTmux}}, nil)
	// sessionsHintEntries: {↑,↓,←,→} group, then {Enter,Space} group, then r, Esc.
	rSpan := r.sessionsHintSpans[6]
	clickListHint(t, r, r.sessionsHint, &r.sessionsHintSpans, rSpan.startCol)

	if got, want := r.sessionsTable.GetRowCount(), 2; got != want {
		t.Errorf("table has %d rows after clicking r, want %d (header + 1 session) — the click should have reloaded", got, want)
	}
}

func TestReloadSessionsSortsByBackendThenName(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, []multiplex.Session{
		{Name: "zeta", Backend: multiplex.BackendTmux},
		{Name: "beta", Backend: multiplex.BackendScreen},
		{Name: "alpha", Backend: multiplex.BackendScreen},
	}, nil)

	r.openSessions()

	want := []multiplex.Session{
		{Name: "alpha", Backend: multiplex.BackendScreen},
		{Name: "beta", Backend: multiplex.BackendScreen},
		{Name: "zeta", Backend: multiplex.BackendTmux},
	}
	if !reflect.DeepEqual(r.sessionsList, want) {
		t.Errorf("sessionsList = %+v, want %+v", r.sessionsList, want)
	}
}

// TestRenderSessionsRowShowsStatusAsAColoredGlyph pins the Status
// column's own glyph choice — a green ✔ for attached, a red ✘ for
// detached, per the user's own explicit choice, rather than the words
// "Attached"/"Detached" themselves.
func TestRenderSessionsRowShowsStatusAsAColoredGlyph(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, []multiplex.Session{
		{Name: "attached-one", Backend: multiplex.BackendTmux, Status: multiplex.StatusAttached},
		{Name: "detached-one", Backend: multiplex.BackendTmux, Status: multiplex.StatusDetached},
	}, nil)

	r.openSessions()

	attachedCell := r.sessionsTable.GetCell(1, sessionsColStatus)
	if !strings.Contains(attachedCell.Text, sessionsAttachedGlyph) {
		t.Errorf("attached row's Status cell = %q, want it to contain %q", attachedCell.Text, sessionsAttachedGlyph)
	}
	if got := tableCellForeground(attachedCell); got != r.theme.EntryExecutable {
		t.Errorf("attached row's Status color = %v, want theme.EntryExecutable (%v)", got, r.theme.EntryExecutable)
	}

	detachedCell := r.sessionsTable.GetCell(2, sessionsColStatus)
	if !strings.Contains(detachedCell.Text, sessionsDetachedGlyph) {
		t.Errorf("detached row's Status cell = %q, want it to contain %q", detachedCell.Text, sessionsDetachedGlyph)
	}
	if got := tableCellForeground(detachedCell); got != r.theme.CriticalText {
		t.Errorf("detached row's Status color = %v, want theme.CriticalText (%v)", got, r.theme.CriticalText)
	}
}

// TestRenderSessionsRowShowsUnknownStatusAsAMutedDash covers Zellij's
// own real gap: `zellij list-sessions` never reports attach status at
// all (verified directly against a real zellij binary, not assumed —
// see multiplex.Status's own doc comment), so a Zellij session must
// never render as a false ✔ or ✘ — a muted dash instead, in the
// theme's own muted color, same as multiplex.StatusUnknown's zero value
// already defaults to.
func TestRenderSessionsRowShowsUnknownStatusAsAMutedDash(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, []multiplex.Session{
		{Name: "some-session", Backend: multiplex.BackendZellij, Status: multiplex.StatusUnknown},
	}, nil)

	r.openSessions()

	cell := r.sessionsTable.GetCell(1, sessionsColStatus)
	if !strings.Contains(cell.Text, sessionsUnknownGlyph) {
		t.Errorf("unknown-status row's Status cell = %q, want it to contain %q", cell.Text, sessionsUnknownGlyph)
	}
	if got := tableCellForeground(cell); got != r.theme.MutedTextColor {
		t.Errorf("unknown-status row's Status color = %v, want theme.MutedTextColor (%v)", got, r.theme.MutedTextColor)
	}
}

// TestRenderSessionsRowColorsBackendByStatusBarSegment pins the user's
// own explicit choice: each backend's own name in the Backend column
// uses the exact same fixed color its counterpart status-bar segment
// already does (see statusDiskColor/statusInodeColor/statusKernelColor
// in bottombar.go), so "which subsystem is this" reads consistently
// wherever this app already colors something by that same idea.
func TestRenderSessionsRowColorsBackendByStatusBarSegment(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, []multiplex.Session{
		{Name: "a", Backend: multiplex.BackendScreen},
		{Name: "b", Backend: multiplex.BackendTmux},
		{Name: "c", Backend: multiplex.BackendZellij},
	}, nil)

	r.openSessions()

	cases := []struct {
		row   int
		want  tcell.Color
		label string
	}{
		{1, statusDiskColor, "screen/statusDiskColor"},
		{2, statusInodeColor, "tmux/statusInodeColor"},
		{3, statusKernelColor, "zellij/statusKernelColor"},
	}
	for _, c := range cases {
		cell := r.sessionsTable.GetCell(c.row, sessionsColBackend)
		if got := tableCellForeground(cell); got != c.want {
			t.Errorf("%s: Backend color = %v, want %v", c.label, got, c.want)
		}
	}
}

// tableCellForeground reads back whatever SetTextColor actually stored
// — tview.NewTableCell seeds every cell with its own non-default Style
// (Foreground/Background already set to its own Styles.* defaults), so
// SetTextColor's own "only set .Color if Style is still exactly
// tcell.StyleDefault" branch (verified directly against tview's own
// table.go, not guessed) never applies here: the color always lands in
// .Style instead, and a test reading the deprecated .Color field
// straight would always see tcell.ColorDefault regardless of what was
// actually set.
func tableCellForeground(cell *tview.TableCell) tcell.Color {
	fg, _, _ := cell.Style.Decompose()
	return fg
}

// TestRenderSessionsKeepsColumnsSelectableWithRealRows is a regression
// test for a real, reported bug: renderSessions used to finish by
// calling the shared enableTableSelection (SetSelectable(true, false),
// rows only — right for Mounts/Firewall/Activity Log, which never need
// per-cell navigation), silently turning columnsSelectable back off
// even though newSessionsScreen had already turned it on. With columns
// not selectable, tview's own Table.InputHandler routes Left/Right to
// its horizontal-scroll-offset handling instead of moving the cell
// selection — invisible on a table narrow enough to need no scrolling,
// which read as "Left/Right do nothing at all" and made the Attach in
// new window/Close cells unreachable by keyboard.
func TestRenderSessionsKeepsColumnsSelectableWithRealRows(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, []multiplex.Session{{Name: "foo", Backend: multiplex.BackendTmux}}, nil)

	r.openSessions()

	rowsSelectable, columnsSelectable := r.sessionsTable.GetSelectable()
	if !rowsSelectable || !columnsSelectable {
		t.Errorf("GetSelectable() = (%v, %v), want (true, true) — Left/Right must move between a row's own action cells", rowsSelectable, columnsSelectable)
	}
}

// TestRenderSessionsRowTreatsNameBackendStatusAsOneKeyboardStop pins the
// user's own explicit follow-up request: Name/Backend/Status should
// read as a single field for keyboard navigation, not three
// independent stops — only Name stays selectable (Backend/Status get
// SetSelectable(false)), so tview's own Left/Right skip straight over
// them to the next real stop (an action cell). Mouse clicks on
// Backend/Status still work exactly as before — see renderSessionsRow's
// own doc comment on why TableCell.Clicked never checks Selectable at
// all — so this only ever changes what the arrow keys land on, not
// what clicking does.
func TestRenderSessionsRowTreatsNameBackendStatusAsOneKeyboardStop(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, []multiplex.Session{{Name: "foo", Backend: multiplex.BackendTmux}}, nil)

	r.openSessions()

	for _, col := range []int{sessionsColBackend, sessionsColStatus} {
		if !r.sessionsTable.GetCell(1, col).NotSelectable {
			t.Errorf("column %d is keyboard-selectable, want it excluded (grouped under Name)", col)
		}
	}
	if r.sessionsTable.GetCell(1, sessionsColName).NotSelectable {
		t.Error("Name is not keyboard-selectable, want it to be the group's own one stop")
	}
}

func TestActivateSessionsCellCloseColumnOpensConfirm(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, []multiplex.Session{{Name: "12345.mysession", Backend: multiplex.BackendScreen}}, nil)
	r.openSessions()

	r.activateSessionsCell(1, sessionsColClose)

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirm dialog", r.activePage)
	}
	if got := r.confirmDialogTitleBar.GetText(true); !strings.Contains(got, "mysession") {
		t.Errorf("confirm text = %q, want it to mention the session name", got)
	}
}

func TestActivateSessionsCellNewWindowColumnShowsTransientNotice(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, []multiplex.Session{{Name: "foo", Backend: multiplex.BackendTmux}}, nil)
	r.openSessions()

	r.activateSessionsCell(1, sessionsColNewWindow)

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the transient notice", r.activePage)
	}
	if got := r.errorView.GetText(true); !strings.Contains(got, "not available yet") {
		t.Errorf("notice text = %q, want it to say the feature isn't available yet", got)
	}
}

// TestActivateSessionsCellNameColumnAttachesWithoutError pins that the
// Name cell (and, by the same default case, Backend/Status) triggers
// Attach — r.app.Suspend is a verified no-op without a real terminal
// (Application.Run was never called — see tview's own Application.Suspend,
// which bails out while its screen is still nil), the same acknowledged
// limitation TestRunBashCommandLogsAnAction's own doc comment already
// notes, so this exercises attachSession's own outcome handling, never a
// real screen/tmux invocation.
func TestActivateSessionsCellNameColumnAttachesWithoutError(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, []multiplex.Session{{Name: "foo", Backend: multiplex.BackendTmux}}, nil)
	r.openSessions()

	r.activateSessionsCell(1, sessionsColName)

	if r.activePage == errorPage {
		t.Errorf("activateSessionsCell(Name) reported an error: %q", r.errorView.GetText(true))
	}
}

func TestCaptureSessionsKeyXOpensCloseConfirmForTheSelectedRow(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, []multiplex.Session{{Name: "foo", Backend: multiplex.BackendScreen}}, nil)
	r.openSessions()
	r.sessionsTable.Select(1, sessionsColName)

	r.captureSessionsKey(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirm dialog", r.activePage)
	}
}

func TestCaptureSessionsKeyRRefreshesTheList(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, nil, nil)
	r.openSessions()

	calls := 0
	orig := listMultiplexSessions
	t.Cleanup(func() { listMultiplexSessions = orig })
	listMultiplexSessions = func() ([]multiplex.Session, error) {
		calls++
		return nil, nil
	}

	r.captureSessionsKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone))

	if calls != 1 {
		t.Errorf("listMultiplexSessions called %d times, want 1", calls)
	}
}

func TestCaptureSessionsKeyEscapeClosesTheScreen(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, nil, nil)
	r.openSessions()

	r.captureSessionsKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	if r.activePage == sessionsPage {
		t.Error("Escape left the Sessions screen open")
	}
}

func TestRunCloseSessionRunsTheCloseCommandAndReloads(t *testing.T) {
	r := newTestRootForSessions(t)
	s := multiplex.Session{Name: "12345.mysession", Backend: multiplex.BackendScreen}
	isolateSessionsList(t, []multiplex.Session{s}, nil)
	r.openSessions()

	var gotArgv []string
	origRun := runMultiplexCommand
	t.Cleanup(func() { runMultiplexCommand = origRun })
	runMultiplexCommand = func(argv []string) error {
		gotArgv = argv
		return nil
	}
	reloadCount := 0
	origList := listMultiplexSessions
	t.Cleanup(func() { listMultiplexSessions = origList })
	listMultiplexSessions = func() ([]multiplex.Session, error) {
		reloadCount++
		return nil, nil
	}

	r.runCloseSession(s)

	want := multiplex.CloseCommand(s)
	if !reflect.DeepEqual(gotArgv, want) {
		t.Errorf("runMultiplexCommand called with %v, want %v", gotArgv, want)
	}
	if reloadCount != 1 {
		t.Errorf("reload count = %d, want 1", reloadCount)
	}
}

func TestRunCloseSessionReportsAFailure(t *testing.T) {
	r := newTestRootForSessions(t)
	s := multiplex.Session{Name: "foo", Backend: multiplex.BackendTmux}
	isolateSessionsList(t, []multiplex.Session{s}, nil)
	r.openSessions()

	orig := runMultiplexCommand
	t.Cleanup(func() { runMultiplexCommand = orig })
	runMultiplexCommand = func([]string) error { return errors.New("no such session") }

	r.runCloseSession(s)

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the error overlay", r.activePage)
	}
}

// TestSessionsTitleBarReloadButtonClickReloads pins the user's own
// explicit request for a mouse-reachable reload button on the Sessions
// screen's own title bar, top right: clicking the exact glyph cell must
// re-read the session list, the same as pressing "r".
func TestSessionsTitleBarReloadButtonClickReloads(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, []multiplex.Session{{Name: "before", Backend: multiplex.BackendTmux}}, nil)
	r.openSessions()

	screen := simulationScreen(t, 100, 30)
	r.handleBeforeDraw(screen) // establishes lastScreenWidth, which renderReloadTitleBar's own column math needs
	r.renderSessions()
	r.sessionsTitleBar.SetRect(0, 0, 100, 1) // the same rect a real Draw would have left it at

	listMultiplexSessions = func() ([]multiplex.Session, error) {
		return []multiplex.Session{{Name: "after", Backend: multiplex.BackendTmux}}, nil
	}

	_, y, width, _ := r.sessionsTitleBar.GetRect()
	col := reloadTitleBarButtonCol(width)
	captured, _ := captureReloadTitleBarMouse(r.sessionsTitleBar, r.reloadSessions)(tview.MouseLeftClick, tcell.NewEventMouse(col, y, tcell.ButtonNone, 0))

	if captured != tview.MouseConsumed {
		t.Error("clicking the reload glyph should consume the click")
	}
	if len(r.sessionsList) != 1 || r.sessionsList[0].Name != "after" {
		t.Errorf("sessionsList = %v, want the click to have re-read the session list", r.sessionsList)
	}
}

// TestSessionsTitleBarClickElsewhereDoesNothing pins that only the exact
// reload-glyph cell does anything — the same "no drag, no other action"
// scope Help's own title bar already has (see
// TestHelpTitleBarClickElsewhereDoesNothing).
func TestSessionsTitleBarClickElsewhereDoesNothing(t *testing.T) {
	r := newTestRootForSessions(t)
	isolateSessionsList(t, []multiplex.Session{{Name: "before", Backend: multiplex.BackendTmux}}, nil)
	r.openSessions()

	screen := simulationScreen(t, 100, 30)
	r.handleBeforeDraw(screen)
	r.renderSessions()
	r.sessionsTitleBar.SetRect(0, 0, 100, 1)

	reloaded := false
	captured, _ := captureReloadTitleBarMouse(r.sessionsTitleBar, func() { reloaded = true })(tview.MouseLeftClick, tcell.NewEventMouse(0, 0, tcell.ButtonNone, 0))

	if captured == tview.MouseConsumed {
		t.Error("a click away from the reload glyph should not be consumed")
	}
	if reloaded {
		t.Error("a click away from the reload glyph should not have reloaded")
	}
}
