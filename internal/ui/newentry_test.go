package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

func newTestRootForNewEntry(t *testing.T) (r *Root, dir string) {
	t.Helper()
	dir = fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	return r, dir
}

func TestOpenNewFileCreatesAnEmptyFileInThePanelsDirectory(t *testing.T) {
	r, dir := newTestRootForNewEntry(t)

	r.openNewFile()
	r.prompt.SetText("fresh.txt")
	r.finishPrompt(tcell.KeyEnter)

	want := filepath.Join(dir, "fresh.txt")
	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("new file not found: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("new file size = %d, want 0", info.Size())
	}
}

func TestOpenNewFileRefusesAnExistingName(t *testing.T) {
	r, _ := newTestRootForNewEntry(t)

	r.openNewFile()
	r.prompt.SetText("apple.txt") // already exists, see fixtureDir
	r.finishPrompt(tcell.KeyEnter)

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want %q after refusing an existing name", r.activePage, errorPage)
	}
}

func TestOpenNewDirCreatesADirectoryInThePanelsDirectory(t *testing.T) {
	r, dir := newTestRootForNewEntry(t)

	r.openNewDir()
	r.prompt.SetText("fresh-dir")
	r.finishPrompt(tcell.KeyEnter)

	want := filepath.Join(dir, "fresh-dir")
	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("new directory not found: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("new entry is not a directory")
	}
}

// TestOpenNewFileOnARemotePanelCreatesThroughTheClient pins that New
// file dispatches to the connected client instead of this machine —
// the same remote-awareness Rename already has (see renameRemote).
func TestOpenNewFileOnARemotePanelCreatesThroughTheClient(t *testing.T) {
	r, _ := newTestRootForNewEntry(t)
	client := &fakeRemoteClient{root: "/remote", entries: map[string][]fsops.Entry{
		"/remote": {{Name: "b.txt", Type: fsops.TypeFile}},
	}}
	if err := r.panel.connectRemote(client, remotefs.Connection{Host: "example.com", User: "tester"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}

	r.openNewFile()
	r.prompt.SetText("fresh.txt")
	r.finishPrompt(tcell.KeyEnter)

	if _, err := client.Stat("/remote/fresh.txt"); err != nil {
		t.Errorf("new file not found on the remote client: %v", err)
	}
}

func TestOpenNewDirRefusesInsideAnArchive(t *testing.T) {
	r, dir := newTestRootForNewEntry(t)
	r.panel.archivePath = "/somewhere.zip" // enough for inArchiveView() to report true

	r.openNewDir()

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want %q inside an archive", r.activePage, errorPage)
	}
	if _, err := os.Stat(filepath.Join(dir, "fresh-dir")); err == nil {
		t.Errorf("directory should not have been created inside an archive view")
	}
}
