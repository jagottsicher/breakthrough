package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// newTestRemoteRoot builds a real Root (one local file, cursor already
// on it — the same fixture newTestRootWithFile's own doc comment
// describes) and then connects its active panel to a fakeRemoteClient,
// the same double remoteconnection_test.go already uses — every
// action under test here needs a genuinely remote panel, not a local
// one, to prove its own guard actually fires.
func newTestRemoteRoot(t *testing.T) *Root {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	client := &fakeRemoteClient{root: "/remote", entries: map[string][]fsops.Entry{
		"/remote": {{Name: "b.txt", Type: fsops.TypeFile}},
	}}
	if err := r.panel.connectRemote(client, remotefs.Connection{Host: "example.com"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	r.panel.focusRow(1) // off the "/remote" root row onto the one real entry
	if row, path, ok := r.panel.CurrentRowPath(); ok {
		r.target, r.targetRow = path, row
	}
	return r
}

// assertRefusedAsRemote runs action against a remote-connected Root
// and checks it reported errNotSupportedRemote through the ordinary
// error overlay — the same shape
// TestCutCurrentSelectionRefusesInArchiveView already uses for its own,
// structurally identical guard.
func assertRefusedAsRemote(t *testing.T, name string, action func(r *Root)) {
	t.Helper()
	r := newTestRemoteRoot(t)

	action(r)

	if r.activePage != errorPage {
		t.Fatalf("%s: expected an error overlay, activePage = %q", name, r.activePage)
	}
	got := strings.ReplaceAll(r.errorView.GetText(true), "\n", " ")
	if !strings.Contains(got, "not supported") || !strings.Contains(got, "remote") {
		t.Errorf("%s: error text = %q, want it to mention errNotSupportedRemote", name, got)
	}
}

func TestActionsRefuseARemoteConnection(t *testing.T) {
	cases := []struct {
		name   string
		action func(r *Root)
	}{
		{"openBatchRename", func(r *Root) { r.openBatchRename() }},
		{"editCurrentEntry", func(r *Root) { r.editCurrentEntry() }},
		{"renameCurrentEntry", func(r *Root) { r.renameCurrentEntry() }},
		{"openChmod", func(r *Root) { r.openChmod() }},
		{"openCompare", func(r *Root) { r.openCompare() }},
		{"cutCurrentSelection", func(r *Root) { r.cutCurrentSelection() }},
		{"copyToClipboard", func(r *Root) { r.copyToClipboard() }},
		{"cutToClipboard", func(r *Root) { r.cutToClipboard() }},
		{"pasteInto", func(r *Root) { r.pasteInto(r.panel.path, false) }},
		{"openChown", func(r *Root) { r.openChown() }},
		{"openSedReplace", func(r *Root) { r.openSedReplace() }},
		{"moveSelectionToTrash", func(r *Root) { r.moveSelectionToTrash() }},
		{"openRemoveConfirm", func(r *Root) { r.openRemoveConfirm() }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertRefusedAsRemote(t, c.name, c.action)
		})
	}
}

// TestCopyToClipboardStillWorksAfterDisconnecting is the negative case:
// the guard must be scoped to "this panel is remote right now", not
// stick around as some kind of permanently poisoned state once a
// connection that was never even used for anything is closed again.
func TestCopyToClipboardStillWorksAfterDisconnecting(t *testing.T) {
	r := newTestRemoteRoot(t)
	if err := r.panel.disconnectRemote(); err != nil {
		t.Fatalf("disconnectRemote: %v", err)
	}
	r.panel.focusRow(1)
	if row, path, ok := r.panel.CurrentRowPath(); ok {
		r.target, r.targetRow = path, row
	}

	r.copyToClipboard()

	if len(r.clipboard) == 0 {
		t.Error("copyToClipboard did nothing after disconnecting from the remote session")
	}
}
