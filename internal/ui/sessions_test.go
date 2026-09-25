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
