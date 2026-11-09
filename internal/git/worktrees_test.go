package git

import (
	"context"
	"path/filepath"
	"testing"
)

// realTempDir resolves symlinks in t.TempDir() — on macOS it's under
// /var/folders, itself a symlink to /private/var/folders, and `git
// worktree list` reports the resolved form, so a raw t.TempDir()
// comparison fails there despite being the same directory.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	return dir
}

func TestWorktreesListsJustTheMainOneByDefault(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "base")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "base")

	worktrees, err := Worktrees(context.Background(), dir)
	if err != nil {
		t.Fatalf("Worktrees: %v", err)
	}
	if len(worktrees) != 1 {
		t.Fatalf("Worktrees = %+v, want 1 (just the main one)", worktrees)
	}
	if worktrees[0].Branch != "main" || worktrees[0].Detached {
		t.Errorf("worktrees[0] = %+v, want Branch=main, Detached=false", worktrees[0])
	}
}

func TestWorktreesListsALinkedWorktreeOnABranch(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "base")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "base")
	runGit(t, dir, "branch", "feature-x")
	otherDir := realTempDir(t) + "/linked"
	runGit(t, dir, "worktree", "add", "-q", otherDir, "feature-x")

	worktrees, err := Worktrees(context.Background(), dir)
	if err != nil {
		t.Fatalf("Worktrees: %v", err)
	}
	if len(worktrees) != 2 {
		t.Fatalf("Worktrees = %+v, want 2 (main plus the linked one)", worktrees)
	}

	var linked *Worktree
	for i := range worktrees {
		if worktrees[i].Branch == "feature-x" {
			linked = &worktrees[i]
		}
	}
	if linked == nil {
		t.Fatalf("no worktree found on feature-x among %+v", worktrees)
	}
	if linked.Path != otherDir {
		t.Errorf("linked.Path = %q, want %q", linked.Path, otherDir)
	}
	if linked.Head == "" {
		t.Error("linked.Head is empty, want a real commit hash")
	}
}

func TestWorktreesReportsADetachedOne(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "base")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "base")
	otherDir := realTempDir(t) + "/detached"
	runGit(t, dir, "worktree", "add", "-q", "--detach", otherDir)

	worktrees, err := Worktrees(context.Background(), dir)
	if err != nil {
		t.Fatalf("Worktrees: %v", err)
	}

	var detached *Worktree
	for i := range worktrees {
		if worktrees[i].Path == otherDir {
			detached = &worktrees[i]
		}
	}
	if detached == nil {
		t.Fatalf("no worktree found at %q among %+v", otherDir, worktrees)
	}
	if !detached.Detached || detached.Branch != "" {
		t.Errorf("detached = %+v, want Detached=true, Branch=\"\"", detached)
	}
}

func TestWorktreesReportsALockedOne(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "base")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "base")
	runGit(t, dir, "branch", "feature-x")
	otherDir := realTempDir(t) + "/locked"
	runGit(t, dir, "worktree", "add", "-q", otherDir, "feature-x")
	runGit(t, dir, "worktree", "lock", "--reason", "testing", otherDir)

	worktrees, err := Worktrees(context.Background(), dir)
	if err != nil {
		t.Fatalf("Worktrees: %v", err)
	}

	var locked *Worktree
	for i := range worktrees {
		if worktrees[i].Path == otherDir {
			locked = &worktrees[i]
		}
	}
	if locked == nil {
		t.Fatalf("no worktree found at %q among %+v", otherDir, worktrees)
	}
	if !locked.Locked || locked.LockReason != "testing" {
		t.Errorf("locked = %+v, want Locked=true, LockReason=testing", locked)
	}
}
