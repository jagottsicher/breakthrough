package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/git"
)

// TestOpenGitBreachPullRemoteConfirmsOnADirtyTree is deliberately the
// only test here that calls openGitBreachPullRemote itself rather
// than applyGitBreachPullRemote directly — safe specifically because
// a dirty tree always stops at openConfirm, never reaching
// startGitBreachPullRemote's own real dispatch at all (see
// gitbreachfetch_test.go's own package-level reasoning for why that
// dispatch is never exercised directly in this package's ordinary
// tests: it races t.TempDir() cleanup with a real background git
// call). The clean-tree branch (no confirm) is intentionally not
// covered the same way the Fetch/Push equivalent isn't — only the
// dirty-tree branch can be asserted on without ever reaching dispatch.
func TestOpenGitBreachPullRemoteConfirmsOnADirtyTree(t *testing.T) {
	requireGitForBreach(t)
	bareDir := t.TempDir()
	runGitBreach(t, bareDir, "init", "-q", "--bare", "-b", "main")

	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	runGitBreach(t, dir, "remote", "add", "origin", bareDir)
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "b.txt")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.toggleGitBreachBranchesTab()
	r.toggleGitBreachBranchesTab()

	r.openGitBreachPullRemote()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirm dialog on a dirty tree", r.activePage)
	}
}

func TestOpenGitBreachPullRemoteIsANoOpOffTheRemotesSubTab(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	r.openGitBreachPullRemote()

	if r.activePage == confirmPage {
		t.Error("activePage = confirmPage, want a silent no-op off the Remotes sub-tab")
	}
}

// TestApplyGitBreachPullRemoteOnARealConflictLeavesTheUserInTheConflictSurface
// mirrors the equivalent Merge test exactly — see git.PullRemote's
// own doc comment on why a conflicting pull shares Merge's own
// disambiguation.
func TestApplyGitBreachPullRemoteOnARealConflictLeavesTheUserInTheConflictSurface(t *testing.T) {
	requireGitForBreach(t)
	bareDir := t.TempDir()
	runGitBreach(t, bareDir, "init", "-q", "--bare", "-b", "main")

	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "base")
	runGitBreach(t, dir, "remote", "add", "origin", bareDir)
	runGitBreach(t, dir, "push", "-q", "origin", "main")

	otherDir := t.TempDir()
	runGitBreach(t, otherDir, "clone", "-q", bareDir, otherDir)
	runGitBreach(t, otherDir, "config", "user.name", "Test")
	runGitBreach(t, otherDir, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(otherDir, "a.txt"), []byte("line1\nSIDE\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, otherDir, "commit", "-q", "-am", "side change")
	runGitBreach(t, otherDir, "push", "-q", "origin", "main")

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line1\nMAIN\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "commit", "-q", "-am", "main change")

	// The real pull — the one call that actually produces the
	// conflict on disk — runs synchronously here via the git-layer
	// function directly, the same shape the goroutine inside
	// startGitBreachPullRemote would use. applyGitBreachPullRemote
	// itself only ever reloads/reports; it never runs git.PullRemote.
	if err := git.PullRemote(context.Background(), dir, "origin", "main"); err != nil {
		t.Fatalf("setup PullRemote: err = %v, want nil (conflicted, not failed)", err)
	}

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	r.applyGitBreachPullRemote(dir, "origin", nil)

	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage, want the dashboard to stay open with the conflict surface")
	}
	if r.gitBreach.Operation != git.OperationMerge {
		t.Errorf("Operation = %v, want OperationMerge", r.gitBreach.Operation)
	}
	if len(r.gitBreach.Status.Conflicts) != 1 || r.gitBreach.Status.Conflicts[0] != "a.txt" {
		t.Errorf("Conflicts = %v, want [a.txt]", r.gitBreach.Status.Conflicts)
	}
}

// TestApplyGitBreachPullRemoteDiscardsAStaleResult/
// TestApplyGitBreachPullRemoteOnFailureStillReloads mirror the
// equivalent Fetch/Push tests — see gitbreachfetch_test.go's own
// package-level reasoning on why the real async entry point is never
// called directly in this package's ordinary tests.
func TestApplyGitBreachPullRemoteDiscardsAStaleResult(t *testing.T) {
	r, _ := gitBreachFetchFixture(t)
	showTablePlaceholder(r.gitBreach.BranchesTable, `Pulling "origin"…`, r.theme.MutedTextColor)
	placeholderBefore := r.gitBreach.BranchesTable.GetCell(1, 0).Text

	r.applyGitBreachPullRemote("/some/other/repo/entirely", "origin", nil)

	if r.activePage == errorPage {
		t.Error("activePage = errorPage for a stale result, want it discarded silently")
	}
	if got := r.gitBreach.BranchesTable.GetCell(1, 0).Text; got != placeholderBefore {
		t.Errorf("placeholder text changed to %q after a stale apply, want it untouched", got)
	}
}

func TestApplyGitBreachPullRemoteOnFailureStillReloads(t *testing.T) {
	r, dir := gitBreachFetchFixture(t)

	r.applyGitBreachPullRemote(dir, "origin", fmt.Errorf("could not read from remote repository"))

	if _, ok := gitBreachRemoteRow(r, "origin"); !ok {
		t.Error("origin missing from the Remotes table — a failed pull should still reload")
	}
}
