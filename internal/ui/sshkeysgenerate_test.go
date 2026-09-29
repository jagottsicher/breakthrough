package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rivo/tview"
)

// sshHomeDir points $HOME at a fresh temp dir for the duration of one
// test, with an empty ~/.ssh already created — sshkeys.DefaultDir()
// reads os.UserHomeDir() directly and has no swappable var of its own
// (unlike readSSHKeys/readSSHAgent), so this is the only way to control
// where openSSHKeysGenerate/submitSSHKeysGenerate actually look without
// ever touching a real ~/.ssh.
func sshHomeDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Setenv("HOME", home)
	return home
}

// selectSSHKeysGenerateAlgorithm drives the real Algorithm dropdown's
// own SetCurrentOption — which fires its own SetOptions callback
// synchronously (see duplicateStrategyField's own doc comment in
// duplicate.go for the exact mechanism) — rather than duplicating that
// callback's own logic inline, so a test exercising it actually pins
// the real code path.
func selectSSHKeysGenerateAlgorithm(t *testing.T, r *Root, algorithm string) {
	t.Helper()
	dd, ok := r.sshKeysGenerateForm.GetFormItem(0).(*tview.DropDown)
	if !ok {
		t.Fatal("form item 0 is not the Algorithm dropdown")
	}
	for i, a := range sshKeysGenerateAlgorithmChoices {
		if a == algorithm {
			dd.SetCurrentOption(i)
			return
		}
	}
	t.Fatalf("no such algorithm choice %q", algorithm)
}

func TestOpenSSHKeysGenerateSetsHarmlessDefaults(t *testing.T) {
	sshHomeDir(t)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openSSHKeysGenerate()

	if r.activePage != sshKeysGeneratePage {
		t.Fatalf("activePage = %q, want %q", r.activePage, sshKeysGeneratePage)
	}
	if r.sshKeysGenerateAlgorithm != "ed25519" {
		t.Errorf("sshKeysGenerateAlgorithm = %q, want ed25519", r.sshKeysGenerateAlgorithm)
	}
	if r.sshKeysGenerateFilenameText == "" {
		t.Error("sshKeysGenerateFilenameText is empty, want a harmless default filename")
	}
}

func TestCancelSSHKeysGenerateClosesTheForm(t *testing.T) {
	sshHomeDir(t)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSSHKeysGenerate()

	r.cancelSSHKeysGenerate()

	if r.activePage == sshKeysGeneratePage {
		t.Error("cancelSSHKeysGenerate should have closed the form")
	}
}

func TestSubmitSSHKeysGenerateReportsAnInvalidSpecInPlace(t *testing.T) {
	sshHomeDir(t)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSSHKeysGenerate()
	r.sshKeysGenerateFilenameText = ""

	r.submitSSHKeysGenerate()

	if r.activePage != sshKeysGeneratePage {
		t.Errorf("activePage = %q, want to stay on the Generate key form after a validation error", r.activePage)
	}
	if got := r.sshKeysGenerateStatus.GetText(true); got == "" {
		t.Error("sshKeysGenerateStatus is empty, want the validation error reported in place")
	}
}

// TestSubmitSSHKeysGenerateRefusesAnAlreadyExistingFilename pins the
// whole point of sshkeys.FilenameAvailable being checked here: a
// filename already taken by an existing key pair must never be
// silently offered up to the confirm dialog and then overwritten.
func TestSubmitSSHKeysGenerateRefusesAnAlreadyExistingFilename(t *testing.T) {
	home := sshHomeDir(t)
	if err := os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519.pub"), []byte("ssh-ed25519 AAAA\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSSHKeysGenerate() // defaults its own filename to "id_ed25519"

	r.submitSSHKeysGenerate()

	if r.activePage != sshKeysGeneratePage {
		t.Errorf("activePage = %q, want to stay on the Generate key form after a filename collision", r.activePage)
	}
	if got := r.sshKeysGenerateStatus.GetText(true); !strings.Contains(got, "already exists") {
		t.Errorf("sshKeysGenerateStatus = %q, want it to mention the existing file", got)
	}
}

// TestSubmitSSHKeysGenerateShowsConfirmWithTheExactCommand pins that a
// valid, available spec goes to the confirm dialog showing the literal
// ssh-keygen command, never running anything before that's accepted.
func TestSubmitSSHKeysGenerateShowsConfirmWithTheExactCommand(t *testing.T) {
	sshHomeDir(t)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSSHKeysGenerate()
	r.sshKeysGenerateCommentText = ""

	r.submitSSHKeysGenerate()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirm dialog", r.activePage)
	}
	if got := r.confirmDialogTitleBar.GetText(true); !strings.Contains(got, "ssh-keygen -t ed25519") {
		t.Errorf("confirm text = %q, want it to contain the exact ssh-keygen command", got)
	}
	if r.pendingConfirm == nil {
		t.Error("pendingConfirm is nil, want the generate action armed")
	}
}

// TestSubmitSSHKeysGenerateNeverMentionsDashN pins the same "no
// passphrase on the command line" guarantee
// sshkeys.TestGenerateCommandNeverEmitsDashN pins at the package level,
// end to end through the actual form submission.
func TestSubmitSSHKeysGenerateNeverMentionsDashN(t *testing.T) {
	sshHomeDir(t)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSSHKeysGenerate()

	r.submitSSHKeysGenerate()

	if got := r.confirmDialogTitleBar.GetText(true); strings.Contains(got, "-N") {
		t.Errorf("confirm text = %q, must never pass a passphrase (-N) on the command line", got)
	}
}

// TestSSHKeysGenerateAlgorithmChangeUpdatesAnUntouchedFilename pins that
// switching Algorithm re-suggests a matching filename ("id_rsa" instead
// of "id_ed25519") as long as the user hasn't typed one of their own —
// live-confirmed against a real run leaving a stale "id_ed25519"
// filename next to a newly chosen rsa algorithm otherwise.
func TestSSHKeysGenerateAlgorithmChangeUpdatesAnUntouchedFilename(t *testing.T) {
	sshHomeDir(t)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSSHKeysGenerate() // ed25519, filename defaults to "id_ed25519"

	selectSSHKeysGenerateAlgorithm(t, r, "rsa")

	if r.sshKeysGenerateFilenameText != "id_rsa" {
		t.Errorf("sshKeysGenerateFilenameText = %q, want it to follow the algorithm change to id_rsa", r.sshKeysGenerateFilenameText)
	}
}

// TestSSHKeysGenerateAlgorithmChangeKeepsATypedFilename is the same
// check's other half: once the user has typed their own filename, an
// Algorithm change must never silently overwrite it.
func TestSSHKeysGenerateAlgorithmChangeKeepsATypedFilename(t *testing.T) {
	sshHomeDir(t)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSSHKeysGenerate()
	r.sshKeysGenerateFilenameText = "my_custom_key"

	selectSSHKeysGenerateAlgorithm(t, r, "rsa")

	if r.sshKeysGenerateFilenameText != "my_custom_key" {
		t.Errorf("sshKeysGenerateFilenameText = %q, want the user's own typed filename left alone", r.sshKeysGenerateFilenameText)
	}
}

func TestRenderSSHKeysGenerateFormShowsBitsOnlyForRSAAndECDSA(t *testing.T) {
	sshHomeDir(t)
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSSHKeysGenerate() // ed25519, the default

	// Algorithm + Filename + Comment, no Bits field.
	if got := r.sshKeysGenerateForm.GetFormItemCount(); got != 3 {
		t.Errorf("form item count for ed25519 = %d, want 3 (no Bits field)", got)
	}

	r.sshKeysGenerateAlgorithm = "rsa"
	r.sshKeysGenerateBits = RSABitsChoices[len(RSABitsChoices)-1]
	r.renderSSHKeysGenerateForm()

	if got := r.sshKeysGenerateForm.GetFormItemCount(); got != 4 {
		t.Errorf("form item count for rsa = %d, want 4 (Algorithm, Bits, Filename, Comment)", got)
	}
}
