package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
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

// TestTabFromTheIdentifierFieldReachesTheCancelButton pins a real,
// live-tmux-discovered bug: tview.Form.Focus unconditionally overwrites
// every item's own SetFinishedFunc whenever the form itself gains
// focus (verified directly against tview's own form.go), so a bare
// SetDoneFunc on the last field is silently discarded and Tab there
// just wraps back to the first field instead of ever reaching
// connectCancelBtn — see newConnectForm's own doc comment on why a
// SetInputCapture is what actually has to intercept it instead.
// connectIdentifierField is the last field now (added after
// connectPasswordField) — see newConnectForm's own doc comment for why
// this wiring moved with it.
func TestTabFromTheIdentifierFieldReachesTheCancelButton(t *testing.T) {
	r := newTestRootForConnect(t)
	noop := func(tview.Primitive) {}

	r.app.SetFocus(r.connectIdentifierField)
	r.connectIdentifierField.InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), noop)

	if !r.connectCancelBtn.HasFocus() {
		t.Error("Tab from the identifier field did not move focus to connectCancelBtn")
	}
}

// TestEnterInTheIdentifierFieldSubmitsTheForm pins the companion
// convenience: Enter in the last field runs Connect directly, the same
// "last field, Enter submits" shape a real login form has, rather than
// also just wrapping focus back to Host the way tview.Form's own
// default Enter handling would.
func TestEnterInTheIdentifierFieldSubmitsTheForm(t *testing.T) {
	r := newTestRootForConnect(t)
	r.connectHostField.SetText("") // deliberately invalid, so runConnect leaves an observable trace
	noop := func(tview.Primitive) {}

	r.app.SetFocus(r.connectIdentifierField)
	r.connectIdentifierField.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), noop)

	if !strings.Contains(r.connectStatus.GetText(true), "Host is required") {
		t.Errorf("status = %q, want runConnect to have actually run", r.connectStatus.GetText(true))
	}
}

// TestBacktabFromTheCancelButtonReturnsToTheForm is the reverse
// direction of the same fix — see
// TestTabFromTheIdentifierFieldReachesTheCancelButton's own doc comment.
func TestBacktabFromTheCancelButtonReturnsToTheForm(t *testing.T) {
	r := newTestRootForConnect(t)
	noop := func(tview.Primitive) {}

	r.app.SetFocus(r.connectCancelBtn)
	r.connectCancelBtn.InputHandler()(tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone), noop)

	if !r.connectForm.HasFocus() {
		t.Error("Backtab from connectCancelBtn did not return focus to the form")
	}
}

// TestTabCyclesFromCancelToConnectAndBacktabReturns pins the cycle
// between the dialog's own two buttons themselves.
func TestTabCyclesFromCancelToConnectAndBacktabReturns(t *testing.T) {
	r := newTestRootForConnect(t)
	noop := func(tview.Primitive) {}

	r.app.SetFocus(r.connectCancelBtn)
	r.connectCancelBtn.InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), noop)
	if !r.connectConnectBtn.HasFocus() {
		t.Fatal("Tab from connectCancelBtn did not move focus to connectConnectBtn")
	}

	r.connectConnectBtn.InputHandler()(tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone), noop)
	if !r.connectCancelBtn.HasFocus() {
		t.Error("Backtab from connectConnectBtn did not move focus back to connectCancelBtn")
	}
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

func TestFinishConnectOnSuccessLogsAnAction(t *testing.T) {
	withTestConfigHome(t)
	r := newTestRootForConnect(t)
	client := fakeConnectedClient()
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	readLog := attachTestActivityLog(t, r)

	r.finishConnect(r.panel, conn, client, nil)

	got := readLog()
	if !strings.Contains(got, string(activitylog.CategoryRemote)) || !strings.Contains(got, "connected to tester@example.com") {
		t.Errorf("log = %q, want a remote entry about the connection", got)
	}
}

func TestFinishConnectOnFailureLogsAnError(t *testing.T) {
	withTestConfigHome(t)
	r := newTestRootForConnect(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	readLog := attachTestActivityLog(t, r)

	r.finishConnect(r.panel, conn, nil, errAuthFailedForTest)

	got := readLog()
	if !strings.Contains(got, string(activitylog.CategoryRemote)) || !strings.Contains(got, errAuthFailedForTest.Error()) {
		t.Errorf("log = %q, want a remote error entry mentioning the Dial failure", got)
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

	// conn has never connected successfully before, so this failure
	// must not create a history entry at all (see RecordAttempt's own
	// doc comment) — an entry that's only ever going to show up red is
	// clutter, not a useful "reconnect to this" shortcut.
	history, err := remotefs.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("history = %+v, want a brand-new connection's failed attempt left out of history", history)
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

// TestFinishConnectOnHostKeyMismatchOffersConnectAnywayWithoutSaving
// pins the Connect dialog's own reaction to remotefs.HostKeyMismatchError —
// a real host-key rotation case (see internal/remotefs's own
// hostkey.go doc comment on why Dial itself never offers a bypass):
// the Connect button relabels into the explicit retry, the fingerprint
// comparison lands in the status line, and the bypass is armed only
// for this exact Connection, never persisted anywhere by finishConnect
// itself.
func TestFinishConnectOnHostKeyMismatchOffersConnectAnywayWithoutSaving(t *testing.T) {
	withTestConfigHome(t)
	r := newTestRootForConnect(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	mismatchErr := &remotefs.HostKeyMismatchError{
		Hostname:        "example.com:22",
		NewKeyType:      "ssh-ed25519",
		NewFingerprint:  "SHA256:newkey",
		OldFingerprints: []string{"SHA256:oldkey"},
	}

	r.finishConnect(r.panel, conn, nil, mismatchErr)

	if r.activePage != connectDialogPage {
		t.Errorf("activePage = %q, want the dialog to stay open so the user can retry", r.activePage)
	}
	if got := r.connectConnectBtn.GetLabel(); got != "Connect anyway" {
		t.Errorf("Connect button label = %q, want %q", got, "Connect anyway")
	}
	status := r.connectStatus.GetText(true)
	if !strings.Contains(status, "SHA256:newkey") || !strings.Contains(status, "SHA256:oldkey") {
		t.Errorf("status = %q, want both the old and new fingerprints shown for comparison", status)
	}
	if !r.connectPendingSkipHostKey || !r.connectPendingSkipHostKeyConn.Equal(conn) {
		t.Error("finishConnect should arm the bypass for exactly this Connection")
	}
}

// TestOpenConnectDialogClearsAnyPendingConnectAnywayState pins that
// reopening the dialog (a different connection attempt, possibly a
// different host entirely) never inherits an earlier attempt's own
// accepted-but-unconsumed bypass — see runConnect's own doc comment on
// why that guard exists at all.
func TestOpenConnectDialogClearsAnyPendingConnectAnywayState(t *testing.T) {
	withTestConfigHome(t)
	r := newTestRootForConnect(t)
	r.connectPendingSkipHostKey = true
	r.connectPendingSkipHostKeyConn = remotefs.Connection{Host: "example.com", User: "tester"}
	r.connectConnectBtn.SetLabel("Connect anyway")

	r.openConnectDialog(remotefs.Connection{})

	if r.connectPendingSkipHostKey {
		t.Error("openConnectDialog should clear a pending bypass from an earlier attempt")
	}
	if got := r.connectConnectBtn.GetLabel(); got != "Connect" {
		t.Errorf("Connect button label = %q, want the plain %q after reopening", got, "Connect")
	}
}

// TestOpenConnectDialogClearsTheIdentifierFieldForABlankPrefill pins
// the same "never inherits a stale previous open" rule for
// connectIdentifierField specifically: reopening for a brand-new
// connection must not leave an earlier one's Identifier sitting in the
// field.
func TestOpenConnectDialogClearsTheIdentifierFieldForABlankPrefill(t *testing.T) {
	r := newTestRootForConnect(t)
	r.openConnectDialog(remotefs.Connection{Host: "a.example.com", Identifier: "build-1"})
	if got := r.connectIdentifierField.GetText(); got != "build-1" {
		t.Fatalf("connectIdentifierField = %q, want %q (prefilled)", got, "build-1")
	}

	r.openConnectDialog(remotefs.Connection{Host: "b.example.com"})

	if got := r.connectIdentifierField.GetText(); got != "" {
		t.Errorf("connectIdentifierField = %q, want empty after reopening for a different, unnamed connection", got)
	}
}

// TestConnectIdentifierAcceptanceFuncAllowsTheDocumentedCharacterSet
// pins the field's own live-typed constraint (see its doc comment):
// letters, digits, underscore, hyphen — nothing else, no spaces.
func TestConnectIdentifierAcceptanceFuncAllowsTheDocumentedCharacterSet(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"", true},
		{"build-1", true},
		{"Build_Runner-02", true},
		{"has space", false},
		{"slash/not-allowed", false},
		{"dot.not-allowed", false},
		{strings.Repeat("a", connectIdentifierMaxLength), true},
		{strings.Repeat("a", connectIdentifierMaxLength+1), false},
	}
	for _, c := range cases {
		if got := connectIdentifierAcceptanceFunc(c.text, 0); got != c.want {
			t.Errorf("connectIdentifierAcceptanceFunc(%q, _) = %v, want %v", c.text, got, c.want)
		}
	}
}
