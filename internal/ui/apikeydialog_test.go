package ui

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/apikey"
	"github.com/jagottsicher/breakthrough/internal/consigliere"
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

// TestAPIKeyDialogSetKeyRetriesTheModelFetch pins the real, reported
// bug: Model/Effort's own choices stayed stuck on just
// "(default: ...)" after storing a key through this dialog while
// already in the "AI Consigliere" category, because nothing retried
// refreshConsigliereModelCache until the category was left and
// re-entered.
func TestAPIKeyDialogSetKeyRetriesTheModelFetch(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), t.TempDir())
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.apiKeySession = testAPIKeySession(t)
	r.openOptions()
	selectOptionCategory(t, r, "AI Consigliere")
	r.openAPIKeyDialog("anthropic")

	called := make(chan struct{}, 1)
	withFakeConsigliereModelList(t, func(ctx context.Context, providerName, key string) ([]consigliere.ModelInfo, error) {
		called <- struct{}{}
		return []consigliere.ModelInfo{{ID: "claude-opus-5-5"}}, nil
	})

	r.apiKeyDialogPassphraseField.SetText("test-passphrase")
	if r.apiKeyDialogConfirmField != nil {
		r.apiKeyDialogConfirmField.SetText("test-passphrase")
	}
	r.runAPIKeyDialogOK()
	r.apiKeyDialogKeyField.SetText("sk-ant-test-value")
	r.runAPIKeyDialogOK()

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("storing the key should have retried the model fetch")
	}
}

// TestAPIKeyDialogCancelAfterUnlockRetriesTheModelFetch pins the same
// bug's other exit: cancelling right after a successful Unlock (an
// existing key already stored, confirmed but not replaced) must also
// retry the model fetch, since the session just became usable for the
// first time this run.
func TestAPIKeyDialogCancelAfterUnlockRetriesTheModelFetch(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), t.TempDir())
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.apiKeySession = testAPIKeySession(t)
	if err := r.apiKeySession.Unlock("test-passphrase"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if err := r.apiKeySession.Set("anthropic", "sk-ant-already-stored"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	r.apiKeySession.Lock()
	r.openOptions()
	selectOptionCategory(t, r, "AI Consigliere")
	r.openAPIKeyDialog("anthropic")

	called := make(chan struct{}, 1)
	withFakeConsigliereModelList(t, func(ctx context.Context, providerName, key string) ([]consigliere.ModelInfo, error) {
		called <- struct{}{}
		return []consigliere.ModelInfo{{ID: "claude-opus-5-5"}}, nil
	})

	r.apiKeyDialogPassphraseField.SetText("test-passphrase")
	r.runAPIKeyDialogOK() // unlock only — moves to the key step without replacing anything
	r.cancelAPIKeyDialog()

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelling right after an unlock should have retried the model fetch")
	}
}
