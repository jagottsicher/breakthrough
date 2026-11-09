package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToggleToWorktreesShowsALinkedWorktree(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	runGitBreach(t, dir, "branch", "feature-x")
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	other := tmp + "/linked"
	runGitBreach(t, dir, "worktree", "add", "-q", other, "feature-x")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	if len(r.gitBreach.Worktrees) != 2 {
		t.Fatalf("setup: Worktrees = %+v, want 2", r.gitBreach.Worktrees)
	}

	r.cycleGitBreachFilesTab(1) // Changes -> Worktrees
	if r.gitBreach.FilesTab != gitBreachFilesTabWorktrees {
		t.Fatalf("FilesTab = %v, want Worktrees", r.gitBreach.FilesTab)
	}

	var found bool
	for row := 0; row < r.gitBreach.FilesTable.GetRowCount(); row++ {
		if wt, ok := r.gitBreachWorktreeAt(row); ok && wt.Branch == "feature-x" {
			found = true
			if text := r.gitBreach.FilesTable.GetCell(row, 0).Text; !strings.Contains(text, other) || !strings.Contains(text, "feature-x") {
				t.Errorf("row text = %q, want it to contain %q and %q", text, other, "feature-x")
			}
		}
	}
	if !found {
		t.Error("feature-x worktree not found in the table after switching to Worktrees")
	}
}

func TestRenderGitBreachWorktreesOnADetachedOneShowsItWithoutABranch(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	other := tmp + "/detached"
	runGitBreach(t, dir, "worktree", "add", "-q", "--detach", other)

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.cycleGitBreachFilesTab(1)

	var text string
	for row := 0; row < r.gitBreach.FilesTable.GetRowCount(); row++ {
		if wt, ok := r.gitBreachWorktreeAt(row); ok && wt.Path == other {
			text = r.gitBreach.FilesTable.GetCell(row, 0).Text
		}
	}
	if !strings.Contains(text, "(detached)") {
		t.Errorf("detached row text = %q, want it to contain %q", text, "(detached)")
	}
}

func TestOpenGitBreachCreateWorktreeAddsALinkedOneAndSwitchesThePanel(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	runGitBreach(t, dir, "branch", "feature-x")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.BranchesTable)

	row, ok := gitBreachBranchRow(r, "feature-x")
	if !ok {
		t.Fatal("feature-x not found in the Branches table")
	}
	r.gitBreach.BranchesTable.Select(row, 0)

	r.openGitBreachCreateWorktree()

	if r.activePage == errorPage {
		t.Fatalf("activePage = errorPage after creating a worktree, want it to have succeeded")
	}
	wantPath := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-feature-x")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("worktree directory %q missing: %v", wantPath, err)
	}
	if r.panel.path != wantPath {
		t.Errorf("panel.path = %q, want %q (switched into the new worktree)", r.panel.path, wantPath)
	}
}

func TestOpenGitBreachCreateWorktreeIsANoOpOnTheCurrentBranch(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.BranchesTable)

	row, ok := gitBreachBranchRow(r, "main")
	if !ok {
		t.Fatal("main not found in the Branches table")
	}
	r.gitBreach.BranchesTable.Select(row, 0)

	r.openGitBreachCreateWorktree()

	if r.activePage == errorPage {
		t.Errorf("activePage = errorPage, want a silent no-op on the already-checked-out branch")
	}
}
