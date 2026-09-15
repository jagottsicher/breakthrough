package ui

import (
	"fmt"
	"os"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// TestLoadDetailsTargetOnARemotePanelStatsThroughTheClient pins a real,
// previously-broken gap: loadDetailsTarget used to call the local
// fsops.Stat unconditionally, so Details ("i"/"I") on any remote file
// always failed — the sidebar's own per-file stat block never worked
// at all on a remote connection, not merely lacked some polish.
func TestLoadDetailsTargetOnARemotePanelStatsThroughTheClient(t *testing.T) {
	r := newTestRemoteRoot(t) // cursor on /remote/b.txt
	r.SetRect(0, 0, 100, 40)

	r.loadDetailsTarget("/remote/b.txt")

	if r.detailsStatErr != nil {
		t.Fatalf("detailsStatErr = %v, want the remote file stat'd successfully", r.detailsStatErr)
	}
	if r.detailsStat.Name != "b.txt" || r.detailsStat.Path != "/remote/b.txt" {
		t.Errorf("detailsStat = %+v, want Name=b.txt Path=/remote/b.txt", r.detailsStat)
	}
	if r.detailsStat.IsDir {
		t.Error("detailsStat.IsDir = true for a plain remote file")
	}
}

// TestLoadDetailsTargetOnARemoteDirectoryReportsItAsADirectory covers
// the other basic classification loadDetailsTarget's own dispatch must
// get right — classifyKind (see detailsStatLines) reads IsDir directly.
func TestLoadDetailsTargetOnARemoteDirectoryReportsItAsADirectory(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	client.entries["/remote"] = append(client.entries["/remote"], fsops.Entry{Name: "sub", Type: fsops.TypeDir, IsDir: true})
	r.SetRect(0, 0, 100, 40)

	r.loadDetailsTarget("/remote/sub")

	if r.detailsStatErr != nil {
		t.Fatalf("detailsStatErr = %v", r.detailsStatErr)
	}
	if !r.detailsStat.IsDir {
		t.Error("detailsStat.IsDir = false for a remote directory")
	}
}

// TestRemoteInfoFromEntryMapsEachSymlinkTypeCorrectly pins
// remoteInfoFromEntry's own field mapping for all three symlink
// EntryTypes plus a plain file — classifyKind (detailssidebar.go) reads
// exactly IsSymlink/LinkBroken/LinkIsDir/IsDir to decide what to show,
// so getting these wrong would silently mislabel a remote symlink's own
// kind in Details.
func TestRemoteInfoFromEntryMapsEachSymlinkTypeCorrectly(t *testing.T) {
	cases := []struct {
		name           string
		entry          fsops.Entry
		wantIsSymlink  bool
		wantLinkBroken bool
		wantLinkIsDir  bool
		wantIsDir      bool
	}{
		{name: "plain file", entry: fsops.Entry{Type: fsops.TypeFile}},
		{name: "plain dir", entry: fsops.Entry{Type: fsops.TypeDir}, wantIsDir: true},
		{name: "symlink to file", entry: fsops.Entry{Type: fsops.TypeSymlinkFile, LinkTarget: "x"}, wantIsSymlink: true},
		{name: "symlink to dir", entry: fsops.Entry{Type: fsops.TypeSymlinkDir, LinkTarget: "x"}, wantIsSymlink: true, wantLinkIsDir: true},
		{name: "broken symlink", entry: fsops.Entry{Type: fsops.TypeSymlinkBroken, LinkTarget: "x"}, wantIsSymlink: true, wantLinkBroken: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			info := remoteInfoFromEntry("/remote/thing", c.entry)
			if info.IsSymlink != c.wantIsSymlink {
				t.Errorf("IsSymlink = %v, want %v", info.IsSymlink, c.wantIsSymlink)
			}
			if info.LinkBroken != c.wantLinkBroken {
				t.Errorf("LinkBroken = %v, want %v", info.LinkBroken, c.wantLinkBroken)
			}
			if info.LinkIsDir != c.wantLinkIsDir {
				t.Errorf("LinkIsDir = %v, want %v", info.LinkIsDir, c.wantLinkIsDir)
			}
			if info.IsDir != c.wantIsDir {
				t.Errorf("IsDir = %v, want %v", info.IsDir, c.wantIsDir)
			}
		})
	}
}

// TestDiskUsageForOnARemotePanelUsesTheClientNotLocalDf pins another
// previously-broken gap alongside Details' own Stat: the status bar's
// Disk/Inodes segment used to call the local fsops.FetchDiskUsage
// (shelling out to `df`) unconditionally, so it silently vanished from
// the status bar entirely on a remote panel — `df` against a path that
// only exists on the other end always fails.
func TestDiskUsageForOnARemotePanelUsesTheClientNotLocalDf(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	client.diskUsage = fsops.DiskUsage{UsedBytes: 100, AvailBytes: 900, UsePercent: 10}

	got, ok := diskUsageFor(r.panel)

	if !ok {
		t.Fatal("diskUsageFor reported not-ok despite the fake client returning a usable value")
	}
	if got != client.diskUsage {
		t.Errorf("diskUsageFor = %+v, want the remote client's own figure %+v", got, client.diskUsage)
	}
}

// TestDiskUsageForOnARemotePanelFailsGracefullyWhenTheClientErrors
// mirrors fsops.FetchDiskUsage's own "ok=false, not an error surfaced
// to the user" contract for the remote side too — the status bar
// simply omits the segment, the same as when `df` itself fails
// locally.
func TestDiskUsageForOnARemotePanelFailsGracefullyWhenTheClientErrors(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	client.diskUsageErr = fmt.Errorf("statvfs@openssh.com not supported")

	_, ok := diskUsageFor(r.panel)

	if ok {
		t.Error("diskUsageFor reported ok despite the client returning an error")
	}
}

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

// TestMoveSelectionToTrashOnARemotePanelMovesIntoTheRemoteTrash pins
// "d"'s own current remote behavior: a remote connection has its own
// trash now (see remotetrash.go), so "d" moves the target there —
// reversible, no confirmation by default, exactly like the local case
// — rather than the permanent delete it used to redirect to before
// that existed.
func TestMoveSelectionToTrashOnARemotePanelMovesIntoTheRemoteTrash(t *testing.T) {
	r := newTestRemoteRoot(t) // cursor on /remote/b.txt
	client := r.panel.remote.(*fakeRemoteClient)

	r.moveSelectionToTrash()

	if r.activePage == confirmPage {
		t.Fatal("moveSelectionToTrash opened a confirmation dialog despite TrashConfirm being off by default")
	}
	if _, err := client.Stat("/remote/b.txt"); err == nil {
		t.Error("b.txt still exists at its original path after being moved to the remote trash")
	}
	items, err := listRemoteTrash(client)
	if err != nil {
		t.Fatalf("listRemoteTrash: %v", err)
	}
	if len(items) != 1 || items[0].OriginalPath != "/remote/b.txt" {
		t.Errorf("remote trash contents = %+v, want exactly one item for /remote/b.txt", items)
	}
}

// TestMoveSelectionToTrashOnARemotePanelAsksFirstWhenTrashConfirmIsOn
// mirrors the identical local behavior: settings.TrashConfirm, off by
// default, still gates the move behind a confirmation for anyone who
// turned it on, remote or not.
func TestMoveSelectionToTrashOnARemotePanelAsksFirstWhenTrashConfirmIsOn(t *testing.T) {
	r := newTestRemoteRoot(t)
	r.settings.TrashConfirm = true

	r.moveSelectionToTrash()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want a confirmation dialog with TrashConfirm on", r.activePage)
	}
}

// TestMoveSelectionToTrashOnAnAlreadyTrashedRemoteItemRedirectsToRemove
// mirrors the local inTrash() redirect: an item already sitting in the
// remote trash has nowhere sensible left to be "moved to trash" a
// second time, so "d" means Remove there instead, exactly like
// browsing the local trash already does.
func TestMoveSelectionToTrashOnAnAlreadyTrashedRemoteItemRedirectsToRemove(t *testing.T) {
	r := newTestRemoteRoot(t)
	client := r.panel.remote.(*fakeRemoteClient)
	if err := moveToTrashRemote(client, "/remote/b.txt"); err != nil {
		t.Fatalf("moveToTrashRemote: %v", err)
	}
	if err := r.panel.load(remoteTrashFilesDir(client)); err != nil {
		t.Fatalf("load: %v", err)
	}
	r.panel.focusRow(1)
	if row, path, ok := r.panel.CurrentRowPath(); ok {
		r.target, r.targetRow = path, row
	}

	r.moveSelectionToTrash()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want moveSelectionToTrash to redirect to the Remove confirmation for an already-trashed item", r.activePage)
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
