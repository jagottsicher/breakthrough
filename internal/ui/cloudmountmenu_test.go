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
	defer rclone.Unmount(target)

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
