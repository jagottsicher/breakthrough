package ui

import (
	"os"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

func TestFinishRenameOnARemotePanelRenamesThroughTheClient(t *testing.T) {
	r := newTestRemoteRoot(t) // cursor on /remote/b.txt (see its own doc comment)

	r.rename.SetText("renamed.txt")
	r.finishRename(tcell.KeyEnter)

	client := r.panel.remote.(*fakeRemoteClient)
	if _, err := client.Stat("/remote/renamed.txt"); err != nil {
		t.Errorf("Stat(renamed.txt): %v, want it to exist after the rename", err)
	}
	if _, err := client.Stat("/remote/b.txt"); err == nil {
		t.Error("the old path still exists after the rename")
	}
}

func TestFinishRenameOnARemotePanelRefusesAnExistingDestination(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	client.entries["/remote"] = append(client.entries["/remote"], fsops.Entry{Name: "existing.txt", Type: fsops.TypeFile})

	r.rename.SetText("existing.txt")
	r.finishRename(tcell.KeyEnter)

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want an error overlay for the name collision", r.activePage)
	}
	if _, err := client.Stat("/remote/b.txt"); err != nil {
		t.Error("the original file was renamed away despite the destination already existing")
	}
}

func TestOpenRemoveConfirmOnARemotePanelDeletesThroughTheClient(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)

	r.openRemoveConfirm()
	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirm dialog to be open", r.activePage)
	}
	r.acceptConfirm()

	if _, err := client.Stat("/remote/b.txt"); err == nil {
		t.Error("b.txt still exists after confirming Remove")
	}
}

func TestOpenRemoveConfirmOnARemotePanelDeletesADirectoryRecursively(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	client.entries["/remote"] = append(client.entries["/remote"], fsops.Entry{Name: "sub", Type: fsops.TypeDir, IsDir: true})
	client.entries["/remote/sub"] = []fsops.Entry{{Name: "inside.txt", Type: fsops.TypeFile}}

	// Reload so the table actually reflects the newly added "sub" row —
	// mutating the fake's own entries map doesn't retroactively change
	// what's already on screen, the same as a real remote directory
	// changing underneath an already-loaded panel wouldn't either
	// (that's what "⭯"/zr Reload is for) — then focus it: dirs sort
	// before files (see fsops.ListDir's own sort order, which
	// SFTPClient.ListDir mirrors), so "sub" lands right after row 0's
	// "..", ahead of "b.txt".
	if err := r.panel.load(r.panel.path); err != nil {
		t.Fatalf("load: %v", err)
	}
	r.panel.focusRow(1)
	if _, p, ok := r.panel.CurrentRowPath(); !ok || p != "/remote/sub" {
		t.Fatalf("setup: cursor path = %q, ok=%v, want /remote/sub", p, ok)
	}

	r.openRemoveConfirm()
	r.acceptConfirm()

	if _, ok := client.entries["/remote/sub"]; ok {
		t.Error("/remote/sub still exists after confirming Remove")
	}
	if _, err := client.Stat("/remote/sub"); err == nil {
		t.Error("/remote/sub's own directory entry still exists after confirming Remove")
	}
}

// TestMoveSelectionToTrashOnARemotePanelRedirectsToRemove pins "d"'s
// own remote behavior: there's no remote trash to move into (see
// moveSelectionToTrash's own doc comment), so it goes straight to the
// same permanent-delete confirmation "D" already uses.
func TestMoveSelectionToTrashOnARemotePanelRedirectsToRemove(t *testing.T) {
	r := newTestRemoteRoot(t)

	r.moveSelectionToTrash()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want moveSelectionToTrash to redirect to the Remove confirmation on a remote panel", r.activePage)
	}
}

func TestApplyChmodDialogOnARemotePanelChmodsThroughTheClient(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)

	r.openChmod()
	r.stagedChmodMode = 0o600
	r.applyChmodDialog()

	entry, err := client.Stat("/remote/b.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if entry.Mode.Perm() != 0o600 {
		t.Errorf("mode = %o, want 0600", entry.Mode.Perm())
	}
}

// TestChmodDirsRecursiveRemoteAppliesToDirectoriesOnlyNotFiles pins
// fsops.ChmodDirsRecursive's own remote counterpart against a small
// tree: a subdirectory, a plain file, and a symlink whose own Type is
// TypeSymlinkDir (points at a directory) — only the real directories
// (the root and the subdirectory) should ever be chmod'd; the symlink
// must be left alone even though it resolves to one.
func TestChmodDirsRecursiveRemoteAppliesToDirectoriesOnlyNotFiles(t *testing.T) {
	client := &fakeRemoteClient{entries: map[string][]fsops.Entry{
		// "/tree" itself needs an entry in its own parent's list too —
		// Stat/Chmod both look a path up by finding it as a named child
		// of path.Dir(p), the same way a real filesystem (and the real
		// SFTP server behind SFTPClient) always would.
		"/": {{Name: "tree", Type: fsops.TypeDir, IsDir: true}},
		"/tree": {
			{Name: "sub", Type: fsops.TypeDir, IsDir: true},
			{Name: "file.txt", Type: fsops.TypeFile},
			{Name: "link", Type: fsops.TypeSymlinkDir, IsDir: true},
		},
		"/tree/sub": nil,
	}}

	if err := chmodDirsRecursiveRemote(client, "/tree", 0o700); err != nil {
		t.Fatalf("chmodDirsRecursiveRemote: %v", err)
	}

	for path, want := range map[string]os.FileMode{"/tree": 0o700, "/tree/sub": 0o700} {
		entry, err := client.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%s): %v", path, err)
		}
		if entry.Mode.Perm() != want {
			t.Errorf("%s mode = %o, want %o", path, entry.Mode.Perm(), want)
		}
	}
	for _, path := range []string{"/tree/file.txt", "/tree/link"} {
		entry, err := client.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%s): %v", path, err)
		}
		if entry.Mode.Perm() == 0o700 {
			t.Errorf("%s was chmod'd, want only real directories touched", path)
		}
	}
}

func TestChmodFilesRecursiveRemoteAppliesToRegularFilesOnlyNotSymlinks(t *testing.T) {
	client := &fakeRemoteClient{entries: map[string][]fsops.Entry{
		"/tree": {
			{Name: "sub", Type: fsops.TypeDir, IsDir: true},
			{Name: "file.txt", Type: fsops.TypeFile},
			{Name: "link.txt", Type: fsops.TypeSymlinkFile},
		},
		"/tree/sub": {
			{Name: "nested.txt", Type: fsops.TypeFile},
		},
	}}

	if err := chmodFilesRecursiveRemote(client, "/tree", 0o600); err != nil {
		t.Fatalf("chmodFilesRecursiveRemote: %v", err)
	}

	for _, path := range []string{"/tree/file.txt", "/tree/sub/nested.txt"} {
		entry, err := client.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%s): %v", path, err)
		}
		if entry.Mode.Perm() != 0o600 {
			t.Errorf("%s mode = %o, want 0600", path, entry.Mode.Perm())
		}
	}
	if entry, err := client.Stat("/tree/link.txt"); err == nil && entry.Mode.Perm() == 0o600 {
		t.Error("the symlink was chmod'd, want only real regular files touched")
	}
}

func TestRemoveRemoteRecursiveNeverFollowsASymlinkIntoItsTarget(t *testing.T) {
	client := &fakeRemoteClient{entries: map[string][]fsops.Entry{
		"/tree": {
			{Name: "link", Type: fsops.TypeSymlinkDir, IsDir: true},
		},
		"/precious": {
			{Name: "important.txt", Type: fsops.TypeFile},
		},
	}}

	// The symlink's own Type (TypeSymlinkDir) is what removeRemoteRecursive
	// is told about it — never fsops.TypeDir — so it must be Removed
	// (unlinked) directly, never RemoveDirectory'd or recursed into.
	if err := removeRemoteRecursive(client, "/tree/link", false); err != nil {
		t.Fatalf("removeRemoteRecursive: %v", err)
	}

	if _, ok := client.entries["/precious"]; !ok {
		t.Fatal("removing the symlink deleted the directory it pointed at")
	}
}

// TestRemoveRemoteRecursiveSkipsASymlinkChildDuringRecursion is the
// recursive-descent half of the same guarantee: a directory being
// deleted that happens to *contain* a symlink to another directory
// must unlink just that symlink, never follow it into "/precious" and
// delete that instead. Distinct from
// TestRemoveRemoteRecursiveNeverFollowsASymlinkIntoItsTarget, whose own
// top-level call never reaches the recursive branch at all.
func TestRemoveRemoteRecursiveSkipsASymlinkChildDuringRecursion(t *testing.T) {
	client := &fakeRemoteClient{entries: map[string][]fsops.Entry{
		"/": {{Name: "tree", Type: fsops.TypeDir, IsDir: true}},
		"/tree": {
			{Name: "link", Type: fsops.TypeSymlinkDir, IsDir: true},
			{Name: "file.txt", Type: fsops.TypeFile},
		},
		"/precious": {
			{Name: "important.txt", Type: fsops.TypeFile},
		},
	}}

	if err := removeRemoteRecursive(client, "/tree", true); err != nil {
		t.Fatalf("removeRemoteRecursive: %v", err)
	}

	if _, ok := client.entries["/precious"]; !ok {
		t.Fatal("recursing into /tree deleted /precious via its symlink")
	}
	if _, ok := client.entries["/tree"]; ok {
		t.Error("/tree itself still exists after being removed")
	}
}
