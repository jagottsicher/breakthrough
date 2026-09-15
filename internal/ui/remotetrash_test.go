package ui

import (
	"testing"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// newTestTrashClient builds a fakeRemoteClient rooted at "/remote"
// with a single file, "a.txt", ready for moveToTrashRemote — every
// remotetrash.go primitive below needs client.entries to already have
// a slot for "/remote" itself (Mkdir's own "parent must already
// exist" contract, mirrored by the fake — see its own doc comment).
func newTestTrashClient(t *testing.T) *fakeRemoteClient {
	t.Helper()
	return &fakeRemoteClient{
		root: "/remote",
		entries: map[string][]fsops.Entry{
			"/remote": {{Name: "a.txt", Type: fsops.TypeFile}},
		},
		content: map[string][]byte{"/remote/a.txt": []byte("hello")},
	}
}

func TestMoveToTrashRemoteMovesThePayloadAndWritesASidecar(t *testing.T) {
	client := newTestTrashClient(t)

	if err := moveToTrashRemote(client, "/remote/a.txt"); err != nil {
		t.Fatalf("moveToTrashRemote: %v", err)
	}

	if _, err := client.Stat("/remote/a.txt"); err == nil {
		t.Error("a.txt still exists at its original path")
	}
	items, err := listRemoteTrash(client)
	if err != nil {
		t.Fatalf("listRemoteTrash: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].OriginalPath != "/remote/a.txt" {
		t.Errorf("OriginalPath = %q, want /remote/a.txt", items[0].OriginalPath)
	}
	got, err := client.Open(items[0].Path(client))
	if err != nil {
		t.Fatalf("Open trashed payload: %v", err)
	}
	defer func() { _ = got.Close() }()
}

// TestMoveToTrashRemoteRemovesTheSidecarIfTheMoveItselfFails pins the
// "never leave a phantom restore target behind" guarantee: a sidecar
// written for an item whose actual move then fails must not survive
// that failure — moveToTrashRemote's own doc comment explains why the
// reverse (move succeeds, sidecar write fails) is the safer of the two
// possible half-failures.
func TestMoveToTrashRemoteRemovesTheSidecarIfTheMoveItselfFails(t *testing.T) {
	client := newTestTrashClient(t)

	err := moveToTrashRemote(client, "/remote/does-not-exist.txt")

	if err == nil {
		t.Fatal("moveToTrashRemote succeeded despite the source not existing")
	}
	items, listErr := listRemoteTrash(client)
	if listErr != nil {
		t.Fatalf("listRemoteTrash: %v", listErr)
	}
	if len(items) != 0 {
		t.Errorf("items = %+v, want no phantom trash entry left behind after a failed move", items)
	}
}

func TestRestoreFromRemoteTrashMovesThePayloadBackAndRemovesTheSidecar(t *testing.T) {
	client := newTestTrashClient(t)
	if err := moveToTrashRemote(client, "/remote/a.txt"); err != nil {
		t.Fatalf("moveToTrashRemote: %v", err)
	}
	items, err := listRemoteTrash(client)
	if err != nil || len(items) != 1 {
		t.Fatalf("setup: listRemoteTrash = %+v, %v", items, err)
	}

	if err := restoreFromRemoteTrash(client, items[0]); err != nil {
		t.Fatalf("restoreFromRemoteTrash: %v", err)
	}

	if _, err := client.Stat("/remote/a.txt"); err != nil {
		t.Errorf("a.txt missing at its original path after restore: %v", err)
	}
	after, err := listRemoteTrash(client)
	if err != nil {
		t.Fatalf("listRemoteTrash: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("trash still lists %+v after restoring its only item", after)
	}
}

func TestRestoreFromRemoteTrashRefusesToOverwriteAnExistingDestination(t *testing.T) {
	client := newTestTrashClient(t)
	if err := moveToTrashRemote(client, "/remote/a.txt"); err != nil {
		t.Fatalf("moveToTrashRemote: %v", err)
	}
	items, err := listRemoteTrash(client)
	if err != nil || len(items) != 1 {
		t.Fatalf("setup: listRemoteTrash = %+v, %v", items, err)
	}
	// Something new now sits at the original path.
	client.entries["/remote"] = append(client.entries["/remote"], fsops.Entry{Name: "a.txt", Type: fsops.TypeFile})

	if err := restoreFromRemoteTrash(client, items[0]); err == nil {
		t.Error("restoreFromRemoteTrash overwrote an existing destination instead of refusing")
	}
}

// TestListRemoteTrashSelfHealsAnOrphanedSidecar pins ListTrash's own
// documented self-healing for the identical local case: a sidecar
// whose own payload no longer exists (removed from files/ by some
// other means) is dropped from the listing and its own now-meaningless
// file removed, rather than reported as a phantom entry.
func TestListRemoteTrashSelfHealsAnOrphanedSidecar(t *testing.T) {
	client := newTestTrashClient(t)
	if err := moveToTrashRemote(client, "/remote/a.txt"); err != nil {
		t.Fatalf("moveToTrashRemote: %v", err)
	}
	items, err := listRemoteTrash(client)
	if err != nil || len(items) != 1 {
		t.Fatalf("setup: listRemoteTrash = %+v, %v", items, err)
	}
	// Simulate the payload having been removed by some other means
	// (an outright Remove, or a plain sftp/scp session outside
	// breakthrough).
	if err := client.Remove(items[0].Path(client)); err != nil {
		t.Fatalf("setup Remove: %v", err)
	}

	after, err := listRemoteTrash(client)
	if err != nil {
		t.Fatalf("listRemoteTrash: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("items = %+v, want the orphaned sidecar dropped", after)
	}
	if _, err := client.Stat(items[0].infoPath(client)); err == nil {
		t.Error("the orphaned sidecar file itself still exists on disk")
	}
}

func TestEmptyRemoteTrashRemovesEveryItemAndItsSidecar(t *testing.T) {
	client := newTestTrashClient(t)
	client.entries["/remote"] = append(client.entries["/remote"], fsops.Entry{Name: "b.txt", Type: fsops.TypeFile})
	client.content["/remote/b.txt"] = []byte("world")
	if err := moveToTrashRemote(client, "/remote/a.txt"); err != nil {
		t.Fatalf("moveToTrashRemote(a.txt): %v", err)
	}
	if err := moveToTrashRemote(client, "/remote/b.txt"); err != nil {
		t.Fatalf("moveToTrashRemote(b.txt): %v", err)
	}

	if err := emptyRemoteTrash(client); err != nil {
		t.Fatalf("emptyRemoteTrash: %v", err)
	}

	items, err := listRemoteTrash(client)
	if err != nil {
		t.Fatalf("listRemoteTrash: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("items = %+v, want the trash fully emptied", items)
	}
	if entries := client.entries[remoteTrashInfoDir(client)]; len(entries) != 0 {
		t.Errorf("info/ still has %+v after emptying the trash", entries)
	}
}

// TestEmptyRemoteTrashNeverFollowsADirectorySymlinkIntoItsTarget pins
// the same symlink-safety guarantee every other recursive remote
// delete in this project already has (see
// TestRemoveRemoteRecursiveNeverFollowsASymlinkIntoItsTarget): a
// trashed item that is itself a symlink to a directory must be
// unlinked as itself, never resolved and recursed into.
func TestEmptyRemoteTrashNeverFollowsADirectorySymlinkIntoItsTarget(t *testing.T) {
	client := newTestTrashClient(t)
	client.entries["/remote"] = append(client.entries["/remote"], fsops.Entry{Name: "link", Type: fsops.TypeSymlinkDir, IsDir: true})
	client.entries["/precious"] = []fsops.Entry{{Name: "important.txt", Type: fsops.TypeFile}}
	if err := moveToTrashRemote(client, "/remote/link"); err != nil {
		t.Fatalf("moveToTrashRemote: %v", err)
	}

	if err := emptyRemoteTrash(client); err != nil {
		t.Fatalf("emptyRemoteTrash: %v", err)
	}

	if _, ok := client.entries["/precious"]; !ok {
		t.Fatal("emptying the trash deleted the directory the symlink pointed at")
	}
}

func TestDescribeRemoteTrashRowsRenamesAndRetimesTrashedEntries(t *testing.T) {
	client := newTestTrashClient(t)
	if err := moveToTrashRemote(client, "/remote/a.txt"); err != nil {
		t.Fatalf("moveToTrashRemote: %v", err)
	}
	items, err := listRemoteTrash(client)
	if err != nil || len(items) != 1 {
		t.Fatalf("setup: listRemoteTrash = %+v, %v", items, err)
	}

	descriptions, isTrash := describeRemoteTrashRows(client, remoteTrashFilesDir(client))

	if !isTrash {
		t.Fatal("describeRemoteTrashRows reported isTrashDir = false for the trash's own files dir")
	}
	desc, ok := descriptions[items[0].Path(client)]
	if !ok {
		t.Fatalf("descriptions = %+v, want an entry for %q", descriptions, items[0].Path(client))
	}
	if desc.name != "/remote/a.txt" {
		t.Errorf("name = %q, want the original path /remote/a.txt", desc.name)
	}
	if !desc.modTime.Equal(items[0].DeletedAt) {
		t.Errorf("modTime = %v, want the deletion time %v", desc.modTime, items[0].DeletedAt)
	}
}

func TestDescribeRemoteTrashRowsReportsFalseForAnOrdinaryDirectory(t *testing.T) {
	client := newTestTrashClient(t)

	_, isTrash := describeRemoteTrashRows(client, "/remote")

	if isTrash {
		t.Error("describeRemoteTrashRows reported isTrashDir = true for an ordinary remote directory")
	}
}
