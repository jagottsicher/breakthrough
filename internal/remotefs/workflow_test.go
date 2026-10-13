package remotefs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// dialTestServer is the same Dial-against-startTestSFTPServer sequence
// every test in sftp_test.go already repeats inline — pulled out once
// here since every workflow test below needs it and gains nothing from
// repeating it a fifth time.
func dialTestServer(t *testing.T) *SFTPClient {
	t.Helper()
	addr := startTestSFTPServer(t, passwordServerConfig("tester", "s3cret"))
	client, err := Dial(context.Background(), DialOptions{
		Connection: Connection{Host: mustSplitHost(t, addr), Port: mustSplitPort(t, addr), User: "tester"},
		Auth: AuthOptions{
			IdentityFiles: []string{},
			Password:      func() (string, error) { return "s3cret", nil },
		},
		HostKeyPrompt:  noPromptHostKeyCallback,
		KnownHostsFile: testKnownHostsFile(t),
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// TestRecursiveDirectoryTreeUploadThenListBackMatches pins what a real
// "copy a directory onto a remote panel" actually reduces to at the
// SFTP protocol level: Mkdir for every directory, Create+Write for
// every file, in parent-before-child order — exactly what the UI
// layer's own remote paste/copy path (internal/ui/remotepaste.go,
// remoteops.go) does one call at a time against a real
// remotefs.Client, but exercised here over the real wire protocol
// instead of fakeRemoteClient's in-memory map (remoteconnection_test.go),
// which can't catch a real server's own path or encoding quirks.
func TestRecursiveDirectoryTreeUploadThenListBackMatches(t *testing.T) {
	root := t.TempDir()
	client := dialTestServer(t)

	tree := filepath.Join(root, "tree")
	if err := client.Mkdir(tree); err != nil {
		t.Fatalf("Mkdir(tree): %v", err)
	}
	sub := filepath.Join(tree, "sub")
	if err := client.Mkdir(sub); err != nil {
		t.Fatalf("Mkdir(sub): %v", err)
	}

	writeFile := func(p, content string) {
		w, err := client.Create(p)
		if err != nil {
			t.Fatalf("Create(%s): %v", p, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("Write(%s): %v", p, err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("Close(%s): %v", p, err)
		}
	}
	writeFile(filepath.Join(tree, "top.txt"), "top-level content")
	writeFile(filepath.Join(sub, "nested.txt"), "nested content")

	topEntries, err := client.ListDir(tree)
	if err != nil {
		t.Fatalf("ListDir(tree): %v", err)
	}
	wantTop := map[string]bool{"sub": true, "top.txt": true}
	if len(topEntries) != len(wantTop) {
		t.Fatalf("ListDir(tree) = %d entries, want %d: %+v", len(topEntries), len(wantTop), topEntries)
	}
	for _, e := range topEntries {
		if !wantTop[e.Name] {
			t.Errorf("unexpected entry %q in tree", e.Name)
		}
	}
	// Directories sort before files (see ListDir's own doc comment) —
	// "sub" must come first, not just be present somewhere.
	if topEntries[0].Name != "sub" || !topEntries[0].IsDir {
		t.Errorf("first entry = %+v, want the directory \"sub\" first", topEntries[0])
	}

	subEntries, err := client.ListDir(sub)
	if err != nil {
		t.Fatalf("ListDir(sub): %v", err)
	}
	if len(subEntries) != 1 || subEntries[0].Name != "nested.txt" {
		t.Fatalf("ListDir(sub) = %+v, want exactly [nested.txt]", subEntries)
	}

	r, err := client.Open(filepath.Join(sub, "nested.txt"))
	if err != nil {
		t.Fatalf("Open(nested.txt): %v", err)
	}
	defer func() { _ = r.Close() }()
	got, err := os.ReadFile(filepath.Join(sub, "nested.txt")) // same file, read back locally — proves the server really wrote it to disk, not just to an in-memory handle
	if err != nil {
		t.Fatalf("reading nested.txt back from disk: %v", err)
	}
	if string(got) != "nested content" {
		t.Errorf("nested.txt content = %q, want %q", got, "nested content")
	}
}

// TestRenameMovesAWholeDirectoryTreeAtomically pins that Rename on a
// directory moves everything underneath it in one step, the same
// contract a local mv/os.Rename already gives — the UI layer's own
// remote rename (finishRename, see remoteops_test.go) relies on this
// holding for a directory just as much as for a single file.
func TestRenameMovesAWholeDirectoryTreeAtomically(t *testing.T) {
	root := t.TempDir()
	client := dialTestServer(t)

	oldDir := filepath.Join(root, "old")
	if err := client.Mkdir(oldDir); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	w, err := client.Create(filepath.Join(oldDir, "inside.txt"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := w.Write([]byte("inside")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	newDir := filepath.Join(root, "new")
	if err := client.Rename(oldDir, newDir); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if _, err := client.Lstat(oldDir); err == nil {
		t.Error("the old directory path still exists after Rename")
	}
	entries, err := client.ListDir(newDir)
	if err != nil {
		t.Fatalf("ListDir(newDir): %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "inside.txt" {
		t.Fatalf("ListDir(newDir) = %+v, want exactly [inside.txt] to have moved with the directory", entries)
	}
}

// TestRemoveDirectoryFailsWhenNotEmptyButSucceedsOnceCleared pins the
// real SFTP server's own rejection of RemoveDirectory on a non-empty
// directory — the precondition removeRemoteRecursive
// (internal/ui/remoteops.go) depends on to decide it must remove
// children first, something fakeRemoteClient's own permissive stub
// can't be trusted to enforce honestly.
func TestRemoveDirectoryFailsWhenNotEmptyButSucceedsOnceCleared(t *testing.T) {
	root := t.TempDir()
	client := dialTestServer(t)

	dir := filepath.Join(root, "dir")
	if err := client.Mkdir(dir); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	filePath := filepath.Join(dir, "file.txt")
	w, err := client.Create(filePath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := client.RemoveDirectory(dir); err == nil {
		t.Error("RemoveDirectory should fail on a non-empty directory")
	}

	if err := client.Remove(filePath); err != nil {
		t.Fatalf("Remove(file.txt): %v", err)
	}
	if err := client.RemoveDirectory(dir); err != nil {
		t.Errorf("RemoveDirectory should succeed once the directory is empty: %v", err)
	}
	if _, err := client.Lstat(dir); err == nil {
		t.Error("the directory still exists after RemoveDirectory")
	}
}

// TestLstatOnASymlinkReportsTheLinkWithoutFollowingItWhileStatResolvesIt
// pins adaptLstatEntry's own two-round-trip symlink handling (see its
// doc comment) against a real symlink on the test server's own
// filesystem — the one thing a fake client's in-memory map has no
// honest way to reproduce, since it never has real inodes/links to
// begin with.
func TestLstatOnASymlinkReportsTheLinkWithoutFollowingItWhileStatResolvesIt(t *testing.T) {
	root := t.TempDir()
	client := dialTestServer(t)

	targetDir := filepath.Join(root, "realdir")
	if err := client.Mkdir(targetDir); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	linkPath := filepath.Join(root, "link-to-realdir")
	if err := os.Symlink(targetDir, linkPath); err != nil {
		t.Fatalf("os.Symlink: %v", err)
	}

	lstatEntry, err := client.Lstat(linkPath)
	if err != nil {
		t.Fatalf("Lstat(link): %v", err)
	}
	if lstatEntry.Type != fsops.TypeSymlinkDir {
		t.Errorf("Lstat(link).Type = %v, want TypeSymlinkDir", lstatEntry.Type)
	}
	if lstatEntry.LinkTarget != targetDir {
		t.Errorf("Lstat(link).LinkTarget = %q, want %q", lstatEntry.LinkTarget, targetDir)
	}
	if !lstatEntry.IsDir {
		t.Error("Lstat(link).IsDir should be true: it resolves to a directory")
	}

	statEntry, err := client.Stat(linkPath)
	if err != nil {
		t.Fatalf("Stat(link): %v", err)
	}
	if statEntry.Type != fsops.TypeDir {
		t.Errorf("Stat(link).Type = %v, want TypeDir (Stat follows the link)", statEntry.Type)
	}

	// A broken link — pointing nowhere real — must still be reported
	// rather than erroring Lstat out entirely: this is what lets the UI
	// layer show a dangling symlink instead of just failing to list it.
	brokenLink := filepath.Join(root, "link-to-nowhere")
	if err := os.Symlink(filepath.Join(root, "does-not-exist"), brokenLink); err != nil {
		t.Fatalf("os.Symlink (broken): %v", err)
	}
	brokenEntry, err := client.Lstat(brokenLink)
	if err != nil {
		t.Fatalf("Lstat(brokenLink): %v", err)
	}
	if brokenEntry.Type != fsops.TypeSymlinkBroken {
		t.Errorf("Lstat(brokenLink).Type = %v, want TypeSymlinkBroken", brokenEntry.Type)
	}
}
