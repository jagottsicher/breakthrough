package ui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// withTestConfigHome points remotefs.LoadHistory/SaveHistory (via
// config.UserDir) at a fresh, empty temp directory for the duration of
// t — never the real developer's own ~/.config/breakthrough, the same
// isolation internal/remotefs's own history_test.go already gives
// those same calls one package down.
func withTestConfigHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

// newTestRootForConnect builds a plain, local Root with the Connect
// dialog already open — every test below starts from here, the same
// "open the dialog, then drive its fields/actions directly" shape
// sedreplace_test.go's own tests already use for its dialog.
func newTestRootForConnect(t *testing.T) *Root {
	t.Helper()
	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openConnectDialog(remotefs.Connection{})
	return r
}

func TestRunConnectRejectsAnEmptyHost(t *testing.T) {
	r := newTestRootForConnect(t)
	r.connectHostField.SetText("")

	r.runConnect()

	if r.activePage != connectDialogPage {
		t.Fatalf("activePage = %q, want the dialog to stay open", r.activePage)
	}
	if !strings.Contains(r.connectStatus.GetText(true), "Host is required") {
		t.Errorf("status = %q, want it to mention the missing host", r.connectStatus.GetText(true))
	}
}

func TestRunConnectRejectsAMalformedPort(t *testing.T) {
	r := newTestRootForConnect(t)
	r.connectHostField.SetText("example.com")
	r.connectPortField.SetText("not-a-number")

	r.runConnect()

	if !strings.Contains(r.connectStatus.GetText(true), "Port must be a number") {
		t.Errorf("status = %q, want it to mention the malformed port", r.connectStatus.GetText(true))
	}
}

func TestRunConnectRejectsAnOutOfRangePort(t *testing.T) {
	r := newTestRootForConnect(t)
	r.connectHostField.SetText("example.com")
	r.connectPortField.SetText("99999")

	r.runConnect()

	if !strings.Contains(r.connectStatus.GetText(true), "Port must be a number") {
		t.Errorf("status = %q, want it to mention the out-of-range port", r.connectStatus.GetText(true))
	}
}

func fakeConnectedClient() *fakeRemoteClient {
	return &fakeRemoteClient{root: "/remote", entries: map[string][]fsops.Entry{
		"/remote": {{Name: "b.txt", Type: fsops.TypeFile}},
	}}
}

func TestFinishConnectOnSuccessAttachesClientAndClosesTheDialog(t *testing.T) {
	withTestConfigHome(t)
	r := newTestRootForConnect(t)
	client := fakeConnectedClient()
	conn := remotefs.Connection{Host: "example.com", User: "tester"}

	r.finishConnect(r.panel, conn, client, nil)

	if r.panel.remote != client {
		t.Error("finishConnect did not attach the client to the panel")
	}
	if r.activePage == connectDialogPage {
		t.Error("the Connect dialog is still open after a successful connection")
	}

	history, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 1 || history[0].LastFailed {
		t.Errorf("history = %+v, want one successful entry", history)
	}
}

func TestFinishConnectOnFailureShowsTheErrorAndKeepsTheDialogOpen(t *testing.T) {
	withTestConfigHome(t)
	r := newTestRootForConnect(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}

	r.finishConnect(r.panel, conn, nil, errAuthFailedForTest)

	if r.panel.remote != nil {
		t.Error("finishConnect attached a client despite a Dial failure")
	}
	if r.activePage != connectDialogPage {
		t.Errorf("activePage = %q, want the dialog to stay open so the user can retry", r.activePage)
	}
	if !strings.Contains(r.connectStatus.GetText(true), errAuthFailedForTest.Error()) {
		t.Errorf("status = %q, want it to show the Dial error", r.connectStatus.GetText(true))
	}

	history, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 1 || !history[0].LastFailed {
		t.Errorf("history = %+v, want one failed entry", history)
	}
}

func TestFinishConnectClosesTheClientWhenItsTabAlreadyClosed(t *testing.T) {
	withTestConfigHome(t)
	r := newTestRootForConnect(t)
	r.newTab(r.panel.path) // now two tabs; capture the first one below
	firstPanel := r.tabs[0]
	r.closeTab(0) // firstPanel is no longer in r.tabs at all

	client := fakeConnectedClient()
	r.finishConnect(firstPanel, remotefs.Connection{Host: "example.com"}, client, nil)

	if !client.closed {
		t.Error("finishConnect left the client open after its own tab had already closed")
	}
	if firstPanel.remote != nil {
		t.Error("finishConnect attached a client to a panel that's no longer a real tab")
	}
}

var errAuthFailedForTest = &testDialError{"ssh: handshake failed: authentication failed"}

type testDialError struct{ msg string }

func (e *testDialError) Error() string { return e.msg }
