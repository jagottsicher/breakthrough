package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// gitBreachFetchFixture opens Git Breach on a repo with one remote
// ("origin", pointed at a real local bare repo so a genuine fetch
// would succeed — not exercised directly here, see this file's own
// package doc comment on why: QueueUpdateDraw blocks forever without
// a running Application.Run), cursor on that remote's own row.
func gitBreachFetchFixture(t *testing.T) (*Root, string) {
	t.Helper()
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

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.BranchesTable)
	r.toggleGitBreachBranchesTab() // Branches -> Tags
	r.toggleGitBreachBranchesTab() // Tags -> Remotes

	row, ok := gitBreachRemoteRow(r, "origin")
	if !ok {
		t.Fatal("origin not found in the Remotes table")
	}
	r.gitBreach.BranchesTable.Select(row, 0)
	return r, dir
}

func TestOpenGitBreachFetchRemoteIsANoOpOffTheRemotesSubTab(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	if r.gitBreach.BranchesTab != gitBreachBranchesTabBranches {
		t.Fatal("setup: BranchesTab != Branches, want a fresh \"jg\" to start there")
	}
	before := r.gitBreach.BranchesTable.GetCell(0, 0).Text

	r.openGitBreachFetchRemote()

	if got := r.gitBreach.BranchesTable.GetCell(0, 0).Text; got != before {
		t.Errorf("row 0 text changed to %q, want a silent no-op off the Remotes sub-tab", got)
	}
}

// TestApplyGitBreachFetchRemoteDiscardsAStaleResult pins the
// staleness guard the async-ui skill's own point 5 requires: a result
// for a repository that's no longer the one gitBreach.Root currently
// names (a later "jg" elsewhere while this fetch was still in flight)
// must never surface an error or reload. Deliberately calls
// applyGitBreachFetchRemote directly rather than going through
// openGitBreachFetchRemote — see this file's own package doc comment
// on why: the latter dispatches a real goroutine running a real `git
// fetch` against this test's own bareDir, which can still be running
// when t.TempDir() tries to remove it at test end (a real, CI-caught
// "directory not empty" failure, not hypothetical).
func TestApplyGitBreachFetchRemoteDiscardsAStaleResult(t *testing.T) {
	r, _ := gitBreachFetchFixture(t)
	showTablePlaceholder(r.gitBreach.BranchesTable, `Fetching "origin"…`, r.theme.MutedTextColor)
	placeholderBefore := r.gitBreach.BranchesTable.GetCell(1, 0).Text

	r.applyGitBreachFetchRemote("/some/other/repo/entirely", "origin", nil)

	if r.activePage == errorPage {
		t.Error("activePage = errorPage for a stale result, want it discarded silently")
	}
	if got := r.gitBreach.BranchesTable.GetCell(1, 0).Text; got != placeholderBefore {
		t.Errorf("placeholder text changed to %q after a stale apply, want it untouched (no reload should have run)", got)
	}
}

func TestApplyGitBreachFetchRemoteOnSuccessReloads(t *testing.T) {
	r, dir := gitBreachFetchFixture(t)

	r.applyGitBreachFetchRemote(dir, "origin", nil)

	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage after a successful fetch")
	}
	if row, ok := gitBreachRemoteRow(r, "origin"); !ok {
		t.Error("origin missing from the Remotes table after reload")
	} else if got := r.gitBreach.BranchesTable.GetCell(row, 0).Text; got == "" {
		t.Error("origin row empty after reload, want its own name/URL restored")
	}
}

func TestApplyGitBreachFetchRemoteOnFailureShowsAnErrorThenReloads(t *testing.T) {
	r, dir := gitBreachFetchFixture(t)

	r.applyGitBreachFetchRemote(dir, "origin", fmt.Errorf("could not read from remote repository"))

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want %q after a failed fetch", r.activePage, errorPage)
	}
	if _, ok := gitBreachRemoteRow(r, "origin"); !ok {
		t.Error("origin missing from the Remotes table — a failed fetch should still reload")
	}
}
