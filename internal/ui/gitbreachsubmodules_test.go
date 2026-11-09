package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func initGitBreachRepoWithSubmodule(t *testing.T) string {
	t.Helper()
	requireGitForBreach(t)
	subDir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(subDir, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, subDir, "add", "x.txt")
	runGitBreach(t, subDir, "commit", "-q", "-m", "base")

	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "base")
	runGitBreach(t, dir, "-c", "protocol.file.allow=always", "submodule", "add", "-q", subDir, "sub")
	runGitBreach(t, dir, "commit", "-q", "-m", "add submodule")
	return dir
}

func TestToggleToSubmodulesShowsItsUpToDateState(t *testing.T) {
	dir := initGitBreachRepoWithSubmodule(t)

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	if len(r.gitBreach.Submodules) != 1 {
		t.Fatalf("setup: Submodules = %+v, want 1", r.gitBreach.Submodules)
	}

	r.cycleGitBreachFilesTab(-1) // Changes -> Submodules (wraps backward)
	if r.gitBreach.FilesTab != gitBreachFilesTabSubmodules {
		t.Fatalf("FilesTab = %v, want Submodules", r.gitBreach.FilesTab)
	}

	var found bool
	for row := 0; row < r.gitBreach.FilesTable.GetRowCount(); row++ {
		if sm, ok := r.gitBreachSubmoduleAt(row); ok && sm.Path == "sub" {
			found = true
			if text := r.gitBreach.FilesTable.GetCell(row, 0).Text; !strings.Contains(text, "sub") || !strings.Contains(text, "up to date") {
				t.Errorf("row text = %q, want it to contain %q and %q", text, "sub", "up to date")
			}
		}
	}
	if !found {
		t.Error("sub submodule not found in the table after switching to Submodules")
	}
}

func TestRenderGitBreachSubmodulesOnNoneShowsAPlaceholder(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.cycleGitBreachFilesTab(-1)

	if got, want := r.gitBreach.FilesTable.GetCell(1, 0).Text, "No submodules."; got != want {
		t.Errorf("placeholder text = %q, want %q", got, want)
	}
}

func TestOpenGitBreachSubmoduleUpdateIsANoOpOffTheSubmodulesSubTab(t *testing.T) {
	dir := initGitBreachRepoWithSubmodule(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	if r.gitBreach.FilesTab != gitBreachFilesTabChanges {
		t.Fatal("setup: FilesTab != Changes, want a fresh \"jg\" to start there")
	}
	before := r.gitBreach.FilesTable.GetCell(0, 0).Text

	r.openGitBreachSubmoduleUpdate()

	if got := r.gitBreach.FilesTable.GetCell(0, 0).Text; got != before {
		t.Errorf("row 0 text changed to %q, want a silent no-op off the Submodules sub-tab", got)
	}
}

func TestApplyGitBreachSubmoduleUpdateDiscardsAStaleResult(t *testing.T) {
	dir := initGitBreachRepoWithSubmodule(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.cycleGitBreachFilesTab(-1)
	showTablePlaceholder(r.gitBreach.FilesTable, `Updating "sub"…`, r.theme.MutedTextColor)
	placeholderBefore := r.gitBreach.FilesTable.GetCell(1, 0).Text

	r.applyGitBreachSubmoduleUpdate("/some/other/repo/entirely", "sub", nil)

	if r.activePage == errorPage {
		t.Error("activePage = errorPage for a stale result, want it discarded silently")
	}
	if got := r.gitBreach.FilesTable.GetCell(1, 0).Text; got != placeholderBefore {
		t.Errorf("placeholder text changed to %q after a stale apply, want it untouched (no reload should have run)", got)
	}
}

func TestApplyGitBreachSubmoduleUpdateOnSuccessReloads(t *testing.T) {
	dir := initGitBreachRepoWithSubmodule(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.cycleGitBreachFilesTab(-1)

	r.applyGitBreachSubmoduleUpdate(dir, "sub", nil)

	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage after a successful update")
	}
	var found bool
	for row := 0; row < r.gitBreach.FilesTable.GetRowCount(); row++ {
		if sm, ok := r.gitBreachSubmoduleAt(row); ok && sm.Path == "sub" {
			found = true
		}
	}
	if !found {
		t.Error("sub missing from the Submodules table after reload")
	}
}

func TestApplyGitBreachSubmoduleUpdateOnFailureStillReloads(t *testing.T) {
	dir := initGitBreachRepoWithSubmodule(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.cycleGitBreachFilesTab(-1)

	r.applyGitBreachSubmoduleUpdate(dir, "sub", fmt.Errorf("could not read from remote repository"))

	var found bool
	for row := 0; row < r.gitBreach.FilesTable.GetRowCount(); row++ {
		if sm, ok := r.gitBreachSubmoduleAt(row); ok && sm.Path == "sub" {
			found = true
		}
	}
	if !found {
		t.Error("sub missing from the Submodules table — a failed update should still reload")
	}
}
