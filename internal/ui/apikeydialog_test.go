package ui

import (
	"path/filepath"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/apikey"
)

// testAPIKeySession opens a fresh, isolated Session under t.TempDir()
// — never the real apiKeyStorePath (TestMain blanks that out to ""
// for every other test), since these two specifically need a real,
// non-nil Session to exercise the dialog's own unlock/set flow.
func testAPIKeySession(t *testing.T) *apikey.Session {
	t.Helper()
	session, err := apikey.OpenSession(filepath.Join(t.TempDir(), "apikeys.json"))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	return session
}

// TestAPIKeyDialogLayersOverOptionsWithoutClosingIt pins a real,
// reported bug: openAPIKeyDialog used to call showOverlay, which
// closes every open overlay first — including the Options screen this
// dialog is always opened from — so cancelling it left nothing on the
// stack to reveal, landing on the panel instead of back on Options.
// Mirrors TestOwnerGroupPickerLayersOverPropertiesWithoutClosingIt's
// own identical shape for an identical class of bug.
func TestAPIKeyDialogLayersOverOptionsWithoutClosingIt(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), t.TempDir())
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.apiKeySession = testAPIKeySession(t)

	r.openOptions()
	r.openAPIKeyDialog("anthropic")

	if r.activePage != apiKeyDialogPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, apiKeyDialogPage)
	}

	visible := r.GetPageNames(true)
	var hasOptions, hasDialog bool
	for _, p := range visible {
		hasOptions = hasOptions || p == optionsPage
		hasDialog = hasDialog || p == apiKeyDialogPage
	}
	if !hasOptions {
		t.Errorf("Options should still be visible underneath the dialog, visible pages: %v", visible)
	}
	if !hasDialog {
		t.Errorf("the API key dialog should be visible, visible pages: %v", visible)
	}

	r.cancelAPIKeyDialog()
	if r.activePage != optionsPage {
		t.Errorf("activePage = %q after cancelling the dialog, want Options to still be open", r.activePage)
	}
}

// TestAPIKeyDialogSetKeyReturnsToOptions pins the other exit from the
// dialog — successfully storing a key (not just cancelling) — landing
// back on Options the same way.
func TestAPIKeyDialogSetKeyReturnsToOptions(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), t.TempDir())
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.apiKeySession = testAPIKeySession(t)

	r.openOptions()
	r.openAPIKeyDialog("anthropic")

	r.apiKeyDialogPassphraseField.SetText("test-passphrase")
	if r.apiKeyDialogConfirmField != nil {
		r.apiKeyDialogConfirmField.SetText("test-passphrase")
	}
	r.runAPIKeyDialogOK()

	if r.apiKeyDialogStep != apiKeyDialogStepKey {
		t.Fatalf("after unlocking, step = %v, want apiKeyDialogStepKey", r.apiKeyDialogStep)
	}

	r.apiKeyDialogKeyField.SetText("sk-ant-test-value")
	r.runAPIKeyDialogOK()

	if r.activePage != optionsPage {
		t.Errorf("activePage = %q after storing a key, want Options to still be open", r.activePage)
	}
	if !r.apiKeySession.HasProvider("anthropic") {
		t.Error("the key should have been stored for \"anthropic\"")
	}
}
