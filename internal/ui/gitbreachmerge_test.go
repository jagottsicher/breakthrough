package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/git"
)

func TestOpenGitBreachMergeOnACleanTreeMergesImmediately(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "base")
	runGitBreach(t, dir, "checkout", "-q", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("side"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "b.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "side change")
	runGitBreach(t, dir, "checkout", "-q", "main")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.BranchesTable)

	row, ok := gitBreachBranchRow(r, "side")
	if !ok {
		t.Fatal("side not found in the Branches table")
	}
	r.gitBreach.BranchesTable.Select(row, 0)

	r.openGitBreachMerge()

	if r.activePage == confirmPage {
		t.Fatal("activePage = confirmPage, want an immediate merge on a clean tree")
	}
	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage after a clean merge that should have succeeded")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Errorf("b.txt missing after merging side into main: %v", err)
	}
}

func TestOpenGitBreachMergeIsANoOpOnTheCurrentBranch(t *testing.T) {
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

	r.openGitBreachMerge()

	if r.activePage == confirmPage || r.activePage == errorPage {
		t.Errorf("activePage = %q, want a silent no-op merging the current branch into itself", r.activePage)
	}
}

func TestOpenGitBreachMergeIsANoOpOnTheTagsSubTab(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	runGitBreach(t, dir, "branch", "side")
	runGitBreach(t, dir, "tag", "v1")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.BranchesTable)
	r.toggleGitBreachBranchesTab() // now on Tags

	r.openGitBreachMerge()

	if r.activePage == confirmPage || r.activePage == errorPage {
		t.Errorf("activePage = %q, want a silent no-op on the Tags sub-tab", r.activePage)
	}
}

func TestOpenGitBreachMergeConfirmsOnADirtyTree(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	runGitBreach(t, dir, "branch", "side")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.BranchesTable)

	row, ok := gitBreachBranchRow(r, "side")
	if !ok {
		t.Fatal("side not found in the Branches table")
	}
	r.gitBreach.BranchesTable.Select(row, 0)

	r.openGitBreachMerge()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want %q (a confirm first on a dirty tree)", r.activePage, confirmPage)
	}
	r.pendingConfirm()

	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage after confirming a merge that should have succeeded")
	}
}

// TestOpenGitBreachMergeOnARealConflictLeavesTheUserInTheConflictSurface
// is the discriminating test for the whole feature: a conflicting
// merge must reload into Phase B's own conflict surface (Operation set,
// Conflicts populated, "C"/"A"/"K" live) rather than surfacing at the
// error overlay the way a genuine failure would.
func TestOpenGitBreachMergeOnARealConflictLeavesTheUserInTheConflictSurface(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "base")
	runGitBreach(t, dir, "checkout", "-q", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("side change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "commit", "-q", "-am", "side change")
	runGitBreach(t, dir, "checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("main change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "commit", "-q", "-am", "main change")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.BranchesTable)

	row, ok := gitBreachBranchRow(r, "side")
	if !ok {
		t.Fatal("side not found in the Branches table")
	}
	r.gitBreach.BranchesTable.Select(row, 0)

	r.openGitBreachMerge()

	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage on a conflicting merge, want the dashboard to stay open with the conflict surface")
	}
	if r.gitBreach.Operation != git.OperationMerge {
		t.Errorf("Operation = %v after a conflicting merge, want OperationMerge", r.gitBreach.Operation)
	}
	if len(r.gitBreach.Status.Conflicts) != 1 || r.gitBreach.Status.Conflicts[0] != "a.txt" {
		t.Errorf("Conflicts = %v, want [a.txt]", r.gitBreach.Status.Conflicts)
	}
}

func TestOpenGitBreachMergeOnAnUnknownBranchNameShowsAnError(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	// Merge a branch that no longer exists by the time the action runs
	// — the real shape of "the ref itself never resolves", exercised
	// directly against mergeGitBreachBranch rather than contriving a
	// stale row lookup through the table.
	r.mergeGitBreachBranch("no-such-branch")

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want %q for a merge that never starts", r.activePage, errorPage)
	}
}
