package ui

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/rclone"
)

// withTestRcloneConfig points every rclone.* call this package makes
// at a fresh, empty config file for the duration of t — never the
// real developer's own ~/.config/rclone/rclone.conf, the same
// isolation internal/rclone's own config_test.go already gives
// CreateStaticRemote/ListRemotes one package down.
func withTestRcloneConfig(t *testing.T) {
	t.Helper()
	t.Setenv("RCLONE_CONFIG", t.TempDir()+"/rclone.conf")
}

// newTestRootForCloudMount builds a plain, local Root with both
// breakthrough's own config dir and rclone's own config file isolated
// to fresh temp locations — the "New cloud mount"/"New cloud remote"
// dialogs are never opened automatically here (unlike
// newTestRootForConnect's Connect dialog): several tests below need
// to call openCloudMountCreateDialog themselves with a specific
// preselect argument.
func newTestRootForCloudMount(t *testing.T) *Root {
	t.Helper()
	withTestConfigHome(t)
	withTestRcloneConfig(t)
	// rclone.MountBase falls back to session.MountsDir() (XDG_DATA_HOME)
	// whenever CloudMountBaseDir is blank — which it always is on a
	// freshly constructed Root — and EnsureMountDir actually creates
	// directories there. Isolated here so these tests mkdir under a
	// temp directory, never the real developer's own
	// ~/.local/share/breakthrough/mounts.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	return r
}

// requireRclone skips t unless the real rclone binary is on PATH —
// every test below needs it for real: ListRemotes/CreateStaticRemote/
// EnsureMountDir/AddMountEntry are the actual subject under test, not
// something worth mocking at the UI layer (see cloudremotedialog_test.go's
// own pure cloudRemoteParams tests for the half that doesn't need it).
func requireRclone(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rclone"); err != nil {
		t.Skipf("rclone not available in this environment: %v", err)
	}
}

// TestOpenCloudMountCreateDialogAlwaysOffersCreateNewRemote pins the
// real fix for a real dead end: with zero remotes configured at all,
// the dialog used to refuse to open outright (errNoEligibleCloudRemotes).
// It must now always open, with cloudMountDialogCreateNewOption as its
// own first (and, with zero real remotes, only) choice.
func TestOpenCloudMountCreateDialogAlwaysOffersCreateNewRemote(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)

	r.openCloudMountCreateDialog("", nil)

	if r.activePage != cloudMountDialogPage {
		t.Fatalf("activePage = %q, want %q (dialog should have opened)", r.activePage, cloudMountDialogPage)
	}
	// TrimSpace — the dropdown's own options are right-padded to the
	// field's own width now (see openCloudMountCreateDialog's own doc
	// comment on why), so the raw text carries trailing spaces.
	if _, text := r.cloudMountDialogRemoteField.GetCurrentOption(); strings.TrimRight(text, " ") != cloudMountDialogCreateNewOption {
		t.Errorf("selected option = %q, want %q (the only choice with zero real remotes)", strings.TrimRight(text, " "), cloudMountDialogCreateNewOption)
	}
}

// TestCloudMountDialogButtonsHideDeleteForCreateNewSentinel pins the
// real, reported complaint that Delete used to always be in the
// button row, even while cloudMountDialogCreateNewOption — nothing
// real to delete yet — was the selected option. Opening with zero
// remotes configured leaves that sentinel selected, so Delete must not
// be among the buttons the Flex actually holds; selecting a real
// remote afterwards must bring it back.
func TestCloudMountDialogButtonsHideDeleteForCreateNewSentinel(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)

	r.openCloudMountCreateDialog("", nil)
	if hasFlexItem(r.cloudMountDialogButtons, r.cloudMountDialogDeleteBtn) {
		t.Error("Delete should not be in the button row while the \"+ Create new remote…\" sentinel is selected")
	}

	if err := rclone.CreateStaticRemote("real", "local", nil); err != nil {
		t.Fatalf("CreateStaticRemote: %v", err)
	}
	r.openCloudMountCreateDialog("real", nil)
	if !hasFlexItem(r.cloudMountDialogButtons, r.cloudMountDialogDeleteBtn) {
		t.Error("Delete should be in the button row once a real remote is selected")
	}
}

// hasFlexItem reports whether item is one of flex's own direct
// children right now — tview.Flex exposes no public lookup for this,
// so GetItem/GetItemCount (both real tview.Flex methods) are walked by
// hand.
func hasFlexItem(flex *tview.Flex, item tview.Primitive) bool {
	for i := 0; i < flex.GetItemCount(); i++ {
		if flex.GetItem(i) == item {
			return true
		}
	}
	return false
}

// TestOpenCloudMountCreateDialogPreselectsByName pins
// runCreateCloudRemote's own hand-off contract: reopening with a real
// remote name selects that remote, not whatever would otherwise be
// the default (index 1, the first real one).
func TestOpenCloudMountCreateDialogPreselectsByName(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)

	for _, name := range []string{"alpha", "beta"} {
		if err := rclone.CreateStaticRemote(name, "local", nil); err != nil {
			t.Fatalf("CreateStaticRemote(%q): %v", name, err)
		}
	}

	r.openCloudMountCreateDialog("beta", nil)

	if _, text := r.cloudMountDialogRemoteField.GetCurrentOption(); strings.TrimRight(text, " ") != "beta" {
		t.Errorf("selected option = %q, want %q", strings.TrimRight(text, " "), "beta")
	}
}

// TestRunAddCloudMountSavesAPlaceholderForTheCreateNewSentinel pins
// the real, reported gap this now closes: a user with zero real
// remotes configured (or who simply never changed the default
// selection) can still save a MountEntry — naming a Target the same
// way SFTP's own free-text Host field lets you save against a host
// that doesn't answer yet. The saved entry's RemoteName is the fixed
// cloudMountDialogUnconfiguredRemoteName placeholder, not a remote
// that doesn't exist in rclone's own config.
func TestRunAddCloudMountSavesAPlaceholderForTheCreateNewSentinel(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	r.openCloudMountCreateDialog("", nil)

	r.runSaveCloudMount()

	if r.activePage == cloudMountDialogPage {
		t.Error("dialog should have closed after a successful save")
	}
	entries, err := rclone.LoadMountEntries()
	if err != nil {
		t.Fatalf("LoadMountEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].RemoteName != cloudMountDialogUnconfiguredRemoteName {
		t.Errorf("entries = %+v, want exactly one with RemoteName %q", entries, cloudMountDialogUnconfiguredRemoteName)
	}
}

// TestRunAddCloudMountSavesASecondPlaceholderWithAUniqueTarget pins
// the real, reported gap closed alongside the test above: every
// placeholder shares cloudMountDialogUnconfiguredRemoteName as its own
// RemoteName, so reopening fresh (same sentinel, Target never hand-
// edited) used to compute the exact same default Target every time —
// AddMountEntry's own duplicate-Target refusal then silently blocked
// a second placeholder from ever being saved at all.
func TestRunAddCloudMountSavesASecondPlaceholderWithAUniqueTarget(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	r.openCloudMountCreateDialog("", nil)
	r.runSaveCloudMount()

	r.openCloudMountCreateDialog("", nil)
	r.runSaveCloudMount()

	if r.activePage == cloudMountDialogPage {
		t.Error("second save should have succeeded and closed the dialog too")
	}
	entries, err := rclone.LoadMountEntries()
	if err != nil {
		t.Fatalf("LoadMountEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want exactly two placeholders", entries)
	}
	if entries[0].Target == entries[1].Target {
		t.Errorf("entries = %+v, want two distinct Targets", entries)
	}
}

// TestRemoveCloudMountWorksForAPlaceholderEntry pins that a saved
// placeholder MountEntry (see the test above) can still be removed
// through the merged list's own "✕" (removeCloudMount) — DeleteRemote
// already treats a nonexistent remote name as a harmless no-op (see
// its own doc comment), so the RemoteName never backing a real rclone
// remote must not block removing the MountEntry itself.
func TestRemoveCloudMountWorksForAPlaceholderEntry(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	r.openCloudMountCreateDialog("", nil)
	r.runSaveCloudMount()

	entries, err := rclone.LoadMountEntries()
	if err != nil {
		t.Fatalf("LoadMountEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want exactly one", entries)
	}

	r.removeCloudMount(entries[0])
	r.acceptConfirm()

	entries, err = rclone.LoadMountEntries()
	if err != nil {
		t.Fatalf("LoadMountEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want none after removing the placeholder", entries)
	}
}

// TestRunAddCloudMountPersistsEntryAndCloses pins the success path:
// picking a real remote, leaving Target at its own computed default,
// and submitting creates the target directory, persists the
// MountEntry, and closes the dialog.
func TestRunAddCloudMountPersistsEntryAndCloses(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	if err := rclone.CreateStaticRemote("myremote", "local", nil); err != nil {
		t.Fatalf("CreateStaticRemote: %v", err)
	}
	r.openCloudMountCreateDialog("", nil)

	r.runSaveCloudMount()

	if r.activePage == cloudMountDialogPage {
		t.Error("dialog should have closed after a successful Add")
	}
	entries, err := rclone.LoadMountEntries()
	if err != nil {
		t.Fatalf("LoadMountEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].RemoteName != "myremote" {
		t.Errorf("entries = %+v, want exactly one for %q", entries, "myremote")
	}
}

// TestRunAddCloudMountRefusesDuplicateTarget pins that AddMountEntry's
// own duplicate-Target refusal (see internal/rclone/config.go) is
// actually surfaced here, not swallowed — a second Add for a
// different remote but the same Target must leave the first entry
// exactly as it was.
func TestRunAddCloudMountRefusesDuplicateTarget(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	for _, name := range []string{"first", "second"} {
		if err := rclone.CreateStaticRemote(name, "local", nil); err != nil {
			t.Fatalf("CreateStaticRemote(%q): %v", name, err)
		}
	}

	r.openCloudMountCreateDialog("first", nil)
	r.runSaveCloudMount()

	entries, err := rclone.LoadMountEntries()
	if err != nil {
		t.Fatalf("LoadMountEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries after first Add = %+v, want exactly one", entries)
	}
	firstTarget := entries[0].Target

	r.openCloudMountCreateDialog("second", nil)
	r.cloudMountDialogTargetField.SetText(firstTarget)
	r.runSaveCloudMount()

	entries, err = rclone.LoadMountEntries()
	if err != nil {
		t.Fatalf("LoadMountEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].RemoteName != "first" {
		t.Errorf("entries = %+v, want still just the first one, unchanged", entries)
	}
}

// TestCloudMountDialogSyncTargetToSelectionOnlyWhileStillDefault pins
// the "never silently overwrite what the user actually typed" rule
// (see cloudMountDialogSyncTargetToSelection's own doc comment).
func TestCloudMountDialogSyncTargetToSelectionOnlyWhileStillDefault(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	for _, name := range []string{"alpha", "beta"} {
		if err := rclone.CreateStaticRemote(name, "local", nil); err != nil {
			t.Fatalf("CreateStaticRemote(%q): %v", name, err)
		}
	}
	r.openCloudMountCreateDialog("alpha", nil)

	// Still the unmodified default for "alpha" — switching to "beta"
	// should follow.
	r.cloudMountDialogSyncTargetToSelection("beta")
	if got, want := r.cloudMountDialogTargetField.GetText(), r.cloudMountDialogDefaultTarget("beta"); got != want {
		t.Errorf("Target = %q, want %q (should follow the selection)", got, want)
	}

	// Now the user types something real — a further selection change
	// must leave it alone.
	r.cloudMountDialogTargetField.SetText("/my/own/custom/path")
	r.cloudMountDialogSyncTargetToSelection("alpha")
	if got := r.cloudMountDialogTargetField.GetText(); got != "/my/own/custom/path" {
		t.Errorf("Target = %q, want it left untouched once the user typed something real", got)
	}
}

// TestRunSaveAndMountCloudMountActuallyMounts pins the "Mount"
// button's own real difference from "OK": it saves the entry exactly
// like OK does, then actually mounts it — a real FUSE mount, checked
// via rclone.IsMounted, not simulated, the same reasoning
// internal/rclone's own TestIsMountedDetectsARealRcloneFUSEMount
// already gives for not simulating that half either. toggleCloudMount
// runs the real Mount call via safeGo in a background goroutine; a
// short, bounded poll absorbs that, the same pattern that package's
// own integration test already establishes for the identical race.
func TestRunSaveAndMountCloudMountActuallyMounts(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	srcDir := t.TempDir()
	if err := rclone.CreateStaticRemote("mountme", "alias", []string{"remote=" + srcDir}); err != nil {
		t.Fatalf("CreateStaticRemote: %v", err)
	}
	r.openCloudMountCreateDialog("mountme", nil)

	r.runSaveAndMountCloudMount()

	if r.activePage == cloudMountDialogPage {
		t.Error("dialog should have closed immediately, same as Mount for SFTP dials immediately")
	}
	entries, err := rclone.LoadMountEntries()
	if err != nil {
		t.Fatalf("LoadMountEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].RemoteName != "mountme" {
		t.Fatalf("entries = %+v, want exactly one for %q", entries, "mountme")
	}
	target := entries[0].Target

	for attempt := 0; attempt < 30; attempt++ {
		mounted, err := rclone.IsMounted(target)
		if err != nil {
			t.Fatalf("IsMounted: %v", err)
		}
		if mounted {
			_ = rclone.Unmount(target)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("Mount should have actually mounted the entry, not just saved it")
}

// TestRunDeleteSelectedCloudRemoteWithoutMountEntry pins the real,
// reported gap this button closes: a remote that was created (or
// saved leniently via OK) but never turned into a MountEntry — the
// merged list's own "✕" has no row for it at all, so Delete is the
// only way to remove it. Confirmed to ask first, same as
// removeCloudMount's own "✕" does.
func TestRunDeleteSelectedCloudRemoteWithoutMountEntry(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	if err := rclone.CreateStaticRemote("orphan", "local", nil); err != nil {
		t.Fatalf("CreateStaticRemote: %v", err)
	}
	r.openCloudMountCreateDialog("orphan", nil)

	r.runDeleteSelectedCloudRemote()
	if r.pendingConfirm == nil {
		t.Fatal("runDeleteSelectedCloudRemote should have opened a confirmation, none is pending")
	}
	remotes, err := rclone.ListRemotes()
	if err != nil {
		t.Fatalf("ListRemotes: %v", err)
	}
	found := false
	for _, rem := range remotes {
		if rem.Name == "orphan" {
			found = true
		}
	}
	if !found {
		t.Error("the remote should still exist before confirming")
	}

	r.acceptConfirm()

	// A real, reported bug: the confirm closure used to call hideOverlay
	// itself on top of acceptConfirm's own hideOverlay, closing this
	// dialog too instead of just the confirm — pin that it stays open.
	if r.activePage != cloudMountDialogPage {
		t.Errorf("activePage = %q, want %q (deleting a remote should not close the mount dialog itself)", r.activePage, cloudMountDialogPage)
	}

	remotes, err = rclone.ListRemotes()
	if err != nil {
		t.Fatalf("ListRemotes: %v", err)
	}
	for _, rem := range remotes {
		if rem.Name == "orphan" {
			t.Error("the remote should be gone after confirming")
		}
	}
}

// TestRunDeleteSelectedCloudRemoteIgnoresTheCreateNewSentinel pins
// that Delete is a no-op while cloudMountDialogCreateNewOption is
// still the selected option — there's no real remote there to delete.
func TestRunDeleteSelectedCloudRemoteIgnoresTheCreateNewSentinel(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	r.openCloudMountCreateDialog("", nil)

	r.runDeleteSelectedCloudRemote()

	if r.pendingConfirm != nil {
		t.Error("Delete should be a no-op for the \"+ Create new remote…\" sentinel")
	}
}
