package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/sshkeys"
)

// newSSHKeysCopyTestRoot builds a Root with $HOME pointed at a fresh
// ~/.ssh (see sshHomeDir) and pairs already loaded into sshKeysPairs
// and the table, so openSSHKeysCopy's own row-selection logic has
// something real to read — the same shape newFirewallAddRuleTestRoot
// establishes for its own screen's prerequisite state.
func newSSHKeysCopyTestRoot(t *testing.T, pairs []sshkeys.KeyPair) *Root {
	t.Helper()
	sshHomeDir(t)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.sshKeysErr = nil
	r.sshKeysPairs = pairs
	r.renderSSHKeys()
	return r
}

func TestOpenSSHKeysCopyRequiresASelectedKeyPair(t *testing.T) {
	r := newSSHKeysCopyTestRoot(t, nil)

	r.openSSHKeysCopy()

	if r.activePage == sshKeysCopyPage {
		t.Error("openSSHKeysCopy should have refused: nothing to select from an empty list")
	}
}

func TestOpenSSHKeysCopyRejectsAKeyWithNoPublicHalf(t *testing.T) {
	r := newSSHKeysCopyTestRoot(t, []sshkeys.KeyPair{
		{Name: "orphan_key", HasPrivate: true, HasPublic: false},
	})
	r.sshKeysTable.Select(1, 0)

	r.openSSHKeysCopy()

	if r.activePage == sshKeysCopyPage {
		t.Error("openSSHKeysCopy should refuse a key pair with no public half to copy")
	}
}

func TestOpenSSHKeysCopyPrefillsFromTheSelectedRow(t *testing.T) {
	r := newSSHKeysCopyTestRoot(t, []sshkeys.KeyPair{
		{Name: "id_ed25519", HasPrivate: true, HasPublic: true},
	})
	r.sshKeysTable.Select(1, 0)

	r.openSSHKeysCopy()

	if r.activePage != sshKeysCopyPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, sshKeysCopyPage)
	}
	if r.sshKeysCopyKeyName != "id_ed25519" {
		t.Errorf("sshKeysCopyKeyName = %q, want id_ed25519", r.sshKeysCopyKeyName)
	}
	if r.sshKeysCopyPortText != "22" {
		t.Errorf("sshKeysCopyPortText = %q, want the default port 22", r.sshKeysCopyPortText)
	}
}

func TestCancelSSHKeysCopyClosesTheForm(t *testing.T) {
	r := newSSHKeysCopyTestRoot(t, []sshkeys.KeyPair{{Name: "id_ed25519", HasPrivate: true, HasPublic: true}})
	r.sshKeysTable.Select(1, 0)
	r.openSSHKeysCopy()

	r.cancelSSHKeysCopy()

	if r.activePage == sshKeysCopyPage {
		t.Error("cancelSSHKeysCopy should have closed the form")
	}
}

func TestSubmitSSHKeysCopyRejectsAnInvalidPort(t *testing.T) {
	r := newSSHKeysCopyTestRoot(t, []sshkeys.KeyPair{{Name: "id_ed25519", HasPrivate: true, HasPublic: true}})
	r.sshKeysTable.Select(1, 0)
	r.openSSHKeysCopy()
	r.sshKeysCopyPortText = "not-a-port"
	r.sshKeysCopyHostText = "example.com"
	r.sshKeysCopyUserText = "jens"

	r.submitSSHKeysCopy()

	if r.activePage != sshKeysCopyPage {
		t.Errorf("activePage = %q, want to stay on the Copy to server form after a validation error", r.activePage)
	}
	if got := r.sshKeysCopyStatus.GetText(true); got == "" {
		t.Error("sshKeysCopyStatus is empty, want the port error reported in place")
	}
}

// writeTestKeyPair writes a minimal, real public key line under dir's
// own ~/.ssh — enough for submitSSHKeysCopy's own ReadPublicKeyLine
// call to succeed without needing a real ssh-keygen-generated key.
func writeTestKeyPair(t *testing.T, home, name string) {
	t.Helper()
	sshDir := filepath.Join(home, ".ssh")
	pub := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample " + name + "@host\n"
	if err := os.WriteFile(filepath.Join(sshDir, name+".pub"), []byte(pub), 0o644); err != nil {
		t.Fatalf("write .pub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, name), []byte("not a real private key\n"), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
}

func TestSubmitSSHKeysCopyShowsConfirmWithTheExactCommand(t *testing.T) {
	r := newSSHKeysCopyTestRoot(t, []sshkeys.KeyPair{{Name: "id_ed25519", HasPrivate: true, HasPublic: true}})
	writeTestKeyPair(t, os.Getenv("HOME"), "id_ed25519")
	r.sshKeysTable.Select(1, 0)
	r.openSSHKeysCopy()
	r.sshKeysCopyHostText = "example.com"
	r.sshKeysCopyUserText = "jens"

	r.submitSSHKeysCopy()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirm dialog", r.activePage)
	}
	got := r.confirmDialogTitleBar.GetText(true)
	if !strings.Contains(got, "ssh ") || !strings.Contains(got, "'jens@example.com'") {
		t.Errorf("confirm text = %q, want it to contain the exact ssh install command", got)
	}
	if r.pendingConfirm == nil {
		t.Error("pendingConfirm is nil, want the copy action armed")
	}
}

func TestApplySSHKeysCopyLogsActionAndClosesTheForm(t *testing.T) {
	r := newSSHKeysCopyTestRoot(t, []sshkeys.KeyPair{{Name: "id_ed25519", HasPrivate: true, HasPublic: true}})
	writeTestKeyPair(t, os.Getenv("HOME"), "id_ed25519")
	r.sshKeysTable.Select(1, 0)
	r.openSSHKeysCopy()
	readLog := attachTestActivityLog(t, r)

	orig := runSSHKeysTestCommand
	t.Cleanup(func() { runSSHKeysTestCommand = orig })
	runSSHKeysTestCommand = func(string) error { return nil } // simulate a working passwordless check

	// r.app.Suspend is a no-op without a real terminal (Application.Run
	// was never called), the same acknowledged limitation
	// TestApplyFirewallAddRuleLogsActionAndReloadsSnapshot's own doc
	// comment already notes — so this exercises applySSHKeysCopy's own
	// outcome handling, never a real ssh invocation for the install
	// step itself.
	r.applySSHKeysCopy("example.com", 22, "jens", filepath.Join(os.Getenv("HOME"), ".ssh", "id_ed25519"), "ssh 'jens@example.com' 'true'")

	if r.activePage == sshKeysCopyPage {
		t.Error("applySSHKeysCopy left the Copy to server form open, want it closed")
	}
	got := readLog()
	if !strings.Contains(got, "example.com") {
		t.Errorf("log = %q, want an entry mentioning the target host", got)
	}
}

func TestApplySSHKeysCopyReportsAFailedPasswordlessTest(t *testing.T) {
	r := newSSHKeysCopyTestRoot(t, []sshkeys.KeyPair{{Name: "id_ed25519", HasPrivate: true, HasPublic: true}})
	writeTestKeyPair(t, os.Getenv("HOME"), "id_ed25519")
	r.sshKeysTable.Select(1, 0)
	r.openSSHKeysCopy()
	readLog := attachTestActivityLog(t, r)

	orig := runSSHKeysTestCommand
	t.Cleanup(func() { runSSHKeysTestCommand = orig })
	runSSHKeysTestCommand = func(string) error { return errors.New("Permission denied") }

	r.applySSHKeysCopy("example.com", 22, "jens", filepath.Join(os.Getenv("HOME"), ".ssh", "id_ed25519"), "ssh 'jens@example.com' 'true'")

	got := readLog()
	if !strings.Contains(got, "not yet working") {
		t.Errorf("log = %q, want it to record the passwordless test's own failure", got)
	}
	if r.activePage != confirmPage {
		t.Errorf("activePage = %q, want the one-button acknowledgement dialog reporting the failed test", r.activePage)
	}
}
