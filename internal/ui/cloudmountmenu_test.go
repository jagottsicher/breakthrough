package ui

import (
	"strings"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/rclone"
)

// TestRemoveCloudMountAsksForConfirmationFirst pins the real, reported
// gap this closes: removing a cloud mount used to drop its own
// MountEntry immediately, with no way to ever remove the underlying
// rclone remote's own credentials at all. Calling removeCloudMount
// alone must never remove anything yet — only open the confirmation
// (see openConfirm's own doc comment on why deciding and acting are
// always two separate steps for a permanent deletion).
func TestRemoveCloudMountAsksForConfirmationFirst(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	if err := rclone.CreateStaticRemote("myremote", "local", nil); err != nil {
		t.Fatalf("CreateStaticRemote: %v", err)
	}
	entry := rclone.MountEntry{RemoteName: "myremote", Target: t.TempDir()}
	if err := rclone.AddMountEntry(entry); err != nil {
		t.Fatalf("AddMountEntry: %v", err)
	}

	r.removeCloudMount(entry)

	entries, err := rclone.LoadMountEntries()
	if err != nil {
		t.Fatalf("LoadMountEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want the MountEntry still there — nothing removed before confirming", entries)
	}
	remotes, err := rclone.ListRemotes()
	if err != nil {
		t.Fatalf("ListRemotes: %v", err)
	}
	found := false
	for _, rem := range remotes {
		if rem.Name == "myremote" {
			found = true
		}
	}
	if !found {
		t.Error("the rclone remote itself should still exist — nothing removed before confirming")
	}
	if r.pendingConfirm == nil {
		t.Error("removeCloudMount should have opened a confirmation, none is pending")
	}
}

// TestDeleteCloudMountRemovesEntryRemoteAndLogsIt pins the actual
// deletion, once confirmed: both the MountEntry and the underlying
// rclone remote are gone, and an activity log entry records it (see
// connectdialog.go's own "saved"/"connected to" actions for the
// established activitylog.CategoryRemote convention this follows).
func TestDeleteCloudMountRemovesEntryRemoteAndLogsIt(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	readLog := attachTestActivityLog(t, r)
	if err := rclone.CreateStaticRemote("myremote", "local", nil); err != nil {
		t.Fatalf("CreateStaticRemote: %v", err)
	}
	entry := rclone.MountEntry{RemoteName: "myremote", Target: t.TempDir()}
	if err := rclone.AddMountEntry(entry); err != nil {
		t.Fatalf("AddMountEntry: %v", err)
	}

	r.removeCloudMount(entry)
	r.acceptConfirm()

	entries, err := rclone.LoadMountEntries()
	if err != nil {
		t.Fatalf("LoadMountEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want none left after confirming", entries)
	}
	remotes, err := rclone.ListRemotes()
	if err != nil {
		t.Fatalf("ListRemotes: %v", err)
	}
	for _, rem := range remotes {
		if rem.Name == "myremote" {
			t.Error("the rclone remote itself should be gone after confirming")
		}
	}
	if logged := readLog(); !strings.Contains(logged, "myremote") {
		t.Errorf("activity log = %q, want it to mention the deleted remote", logged)
	}
}

// TestRemoveCloudMountRefusesWhileStillMounted pins that the
// confirmation is never even offered for a mount that's still active
// — unmount first, the same "disconnect before removing" shape
// disconnectConnectionRow already enforces for an active SFTP
// connection. Uses a real mount (the "alias" backend, pointed at a
// temp dir) rather than simulating IsMounted, the same reasoning
// internal/rclone's own real-mount tests already give for not faking
// that half.
func TestRemoveCloudMountRefusesWhileStillMounted(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	srcDir := t.TempDir()
	if err := rclone.CreateStaticRemote("myremote", "alias", []string{"remote=" + srcDir}); err != nil {
		t.Fatalf("CreateStaticRemote: %v", err)
	}
	target := t.TempDir()
	entry := rclone.MountEntry{RemoteName: "myremote", Target: target}
	if err := rclone.AddMountEntry(entry); err != nil {
		t.Fatalf("AddMountEntry: %v", err)
	}
	if err := rclone.Mount("myremote:", target, rclone.MountOptions{}); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	defer func() { _ = rclone.Unmount(target) }()

	r.removeCloudMount(entry)

	if r.pendingConfirm != nil {
		t.Error("removeCloudMount should refuse outright while still mounted, not offer a confirmation")
	}
	entries, err := rclone.LoadMountEntries()
	if err != nil {
		t.Fatalf("LoadMountEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("entries = %+v, want the MountEntry untouched", entries)
	}
}

// TestEditCloudMountRowOpensDialogPrefilledForEditing pins the real,
// reported gap editCloudMountRow closes: there was previously no way
// to change an existing cloud mount's own Target/Identifier/Tags at
// all without removing it and starting over.
func TestEditCloudMountRowOpensDialogPrefilledForEditing(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	if err := rclone.CreateStaticRemote("myremote", "local", nil); err != nil {
		t.Fatalf("CreateStaticRemote: %v", err)
	}
	target := t.TempDir()
	entry := rclone.MountEntry{RemoteName: "myremote", Target: target, Identifier: "my-id", Tags: []string{"a", "b"}}
	if err := rclone.AddMountEntry(entry); err != nil {
		t.Fatalf("AddMountEntry: %v", err)
	}

	r.editCloudMountRow(entry)

	if r.activePage != cloudMountDialogPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, cloudMountDialogPage)
	}
	if got := r.cloudMountDialogTargetField.GetText(); got != target {
		t.Errorf("Target = %q, want %q", got, target)
	}
	if got := r.cloudMountDialogIdentifierField.GetText(); got != "my-id" {
		t.Errorf("Identifier = %q, want %q", got, "my-id")
	}
	if got := r.cloudMountDialogTagsField.GetText(); got != "a, b" {
		t.Errorf("Tags = %q, want %q", got, "a, b")
	}
	if _, text := r.cloudMountDialogRemoteField.GetCurrentOption(); strings.TrimRight(text, " ") != "myremote" {
		t.Errorf("selected Remote = %q, want %q", strings.TrimRight(text, " "), "myremote")
	}
}

// TestEditCloudMountRowThenOKReplacesTheOldEntry pins
// saveCloudMountEntry's own edit-mode behavior: Target left unchanged
// (the common case — only Identifier/Tags actually change) must not
// trip AddMountEntry's own duplicate-Target refusal, and must end up
// as exactly one entry, not two.
func TestEditCloudMountRowThenOKReplacesTheOldEntry(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	if err := rclone.CreateStaticRemote("myremote", "local", nil); err != nil {
		t.Fatalf("CreateStaticRemote: %v", err)
	}
	target := t.TempDir()
	entry := rclone.MountEntry{RemoteName: "myremote", Target: target, Identifier: "old-id"}
	if err := rclone.AddMountEntry(entry); err != nil {
		t.Fatalf("AddMountEntry: %v", err)
	}

	r.editCloudMountRow(entry)
	r.cloudMountDialogIdentifierField.SetText("new-id")
	r.runSaveCloudMount()

	entries, err := rclone.LoadMountEntries()
	if err != nil {
		t.Fatalf("LoadMountEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Identifier != "new-id" {
		t.Errorf("entries = %+v, want exactly one with Identifier %q", entries, "new-id")
	}
}

// TestEditCloudMountRowThenOKReplacesAPlaceholderEntry pins the real,
// reported gap this closed for a placeholder entry specifically (see
// cloudMountDialogUnconfiguredRemoteName's own doc comment): its own
// RemoteName never matches any real rclone remote, so
// openCloudMountCreateDialog's own preselect-by-name loop can never
// find it in the dropdown's own real-remote list — editing one must
// still land on the sentinel and save correctly, not get stuck
// unable to save at all (the exact complaint this fixes).
func TestEditCloudMountRowThenOKReplacesAPlaceholderEntry(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	target := t.TempDir()
	entry := rclone.MountEntry{RemoteName: cloudMountDialogUnconfiguredRemoteName, Target: target, Identifier: "old-id"}
	if err := rclone.AddMountEntry(entry); err != nil {
		t.Fatalf("AddMountEntry: %v", err)
	}

	r.editCloudMountRow(entry)
	if _, text := r.cloudMountDialogRemoteField.GetCurrentOption(); strings.TrimRight(text, " ") != cloudMountDialogCreateNewOption {
		t.Errorf("selected option = %q, want the sentinel %q (no real remote named %q exists)", strings.TrimRight(text, " "), cloudMountDialogCreateNewOption, cloudMountDialogUnconfiguredRemoteName)
	}
	if got := r.cloudMountDialogTargetField.GetText(); got != target {
		t.Errorf("Target = %q, want %q", got, target)
	}
	r.cloudMountDialogIdentifierField.SetText("new-id")
	r.runSaveCloudMount()

	if r.activePage == cloudMountDialogPage {
		t.Error("dialog should have closed after a successful save")
	}
	entries, err := rclone.LoadMountEntries()
	if err != nil {
		t.Fatalf("LoadMountEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].RemoteName != cloudMountDialogUnconfiguredRemoteName || entries[0].Identifier != "new-id" {
		t.Errorf("entries = %+v, want exactly one with RemoteName %q and Identifier %q", entries, cloudMountDialogUnconfiguredRemoteName, "new-id")
	}
}

// TestActivateConnectionMenuCellEjectColumnEditsWhenNotMounted and
// TestActivateConnectionMenuCellEjectColumnTogglesWhenMounted pin the
// eject column's own cloud-mount shape (see renderConnectionMenu's own
// doc comment): edit while not mounted — nothing else in the merged
// list ever opened a way to change an existing entry's own fields —
// toggle (unmount) while mounted, mirroring the SFTP branch right next
// to it exactly.
func TestActivateConnectionMenuCellEjectColumnEditsWhenNotMounted(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	if err := rclone.CreateStaticRemote("myremote", "local", nil); err != nil {
		t.Fatalf("CreateStaticRemote: %v", err)
	}
	target := t.TempDir()
	entry := rclone.MountEntry{RemoteName: "myremote", Target: target}
	if err := rclone.AddMountEntry(entry); err != nil {
		t.Fatalf("AddMountEntry: %v", err)
	}
	r.connectionMenuCloudMountRows = map[int]rclone.MountEntry{0: entry}

	r.activateConnectionMenuCell(0, connectionMenuEjectColumn(r.settings.ConnectionMenuExpanded))

	if r.activePage != cloudMountDialogPage {
		t.Errorf("activePage = %q, want %q (eject column should open edit while not mounted)", r.activePage, cloudMountDialogPage)
	}
}

func TestActivateConnectionMenuCellEjectColumnTogglesWhenMounted(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	srcDir := t.TempDir()
	if err := rclone.CreateStaticRemote("myremote", "alias", []string{"remote=" + srcDir}); err != nil {
		t.Fatalf("CreateStaticRemote: %v", err)
	}
	target := t.TempDir()
	entry := rclone.MountEntry{RemoteName: "myremote", Target: target}
	if err := rclone.AddMountEntry(entry); err != nil {
		t.Fatalf("AddMountEntry: %v", err)
	}
	if err := rclone.Mount("myremote:", target, rclone.MountOptions{}); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	defer func() { _ = rclone.Unmount(target) }()
	r.connectionMenuCloudMountRows = map[int]rclone.MountEntry{0: entry}

	r.activateConnectionMenuCell(0, connectionMenuEjectColumn(r.settings.ConnectionMenuExpanded))

	if r.activePage == cloudMountDialogPage {
		t.Error("eject column should unmount, not open the edit dialog, while mounted")
	}
	// toggleCloudMount runs the real Unmount via safeGo in the
	// background — see its own doc comment on why this is never
	// synchronously observable here (same reasoning pasteconflict_test.go
	// already documents for QueueUpdateDraw-dependent outcomes), so this
	// only pins that the dialog didn't open, not the unmount's own
	// eventual result.
}
