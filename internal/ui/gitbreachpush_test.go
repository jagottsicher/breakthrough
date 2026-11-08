package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenGitBreachPushRemoteOpensAConfirmWithTheExactCommand(t *testing.T) {
	r, _ := gitBreachFetchFixture(t)

	r.openGitBreachPushRemote()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, confirmPage)
	}
	got := r.confirmDialogTitleBar.GetText(true)
	want := "git push origin main"
	if !strings.Contains(got, want) {
		t.Errorf("confirm dialog text = %q, want it to contain %q", got, want)
	}
}

func TestOpenGitBreachPushRemoteIsANoOpOffTheRemotesSubTab(t *testing.T) {
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

	r.openGitBreachPushRemote()

	if r.activePage == confirmPage {
		t.Error("activePage = confirmPage, want a silent no-op off the Remotes sub-tab")
	}
}

func TestOpenGitBreachPushRemoteIsANoOpOnADetachedHead(t *testing.T) {
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
	runGitBreach(t, dir, "checkout", "-q", "--detach", "HEAD")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	if !r.gitBreach.Status.Detached {
		t.Fatal("setup: Status.Detached = false, want a detached HEAD")
	}
	r.toggleGitBreachBranchesTab()
	r.toggleGitBreachBranchesTab()

	r.openGitBreachPushRemote()

	if r.activePage == confirmPage {
		t.Error("activePage = confirmPage, want a silent no-op on a detached HEAD")
	}
}

// TestApplyGitBreachPushRemoteDiscardsAStaleResult/
// TestApplyGitBreachPushRemoteOnSuccessReloads mirror the equivalent
// Fetch tests exactly, including deliberately calling
// applyGitBreachPushRemote directly rather than going through
// openGitBreachPushRemote/startGitBreachPushRemote — see
// gitbreachfetch_test.go's own package-level reasoning on why the
// real async entry point is never called directly in this package's
// ordinary tests.
func TestApplyGitBreachPushRemoteDiscardsAStaleResult(t *testing.T) {
	r, _ := gitBreachFetchFixture(t)
	showTablePlaceholder(r.gitBreach.BranchesTable, `Pushing "origin"…`, r.theme.MutedTextColor)
	placeholderBefore := r.gitBreach.BranchesTable.GetCell(1, 0).Text

	r.applyGitBreachPushRemote("/some/other/repo/entirely", "origin", nil)

	if r.activePage == errorPage {
		t.Error("activePage = errorPage for a stale result, want it discarded silently")
	}
	if got := r.gitBreach.BranchesTable.GetCell(1, 0).Text; got != placeholderBefore {
		t.Errorf("placeholder text changed to %q after a stale apply, want it untouched", got)
	}
}

func TestApplyGitBreachPushRemoteOnSuccessReloads(t *testing.T) {
	r, dir := gitBreachFetchFixture(t)

	r.applyGitBreachPushRemote(dir, "origin", nil)

	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage after a successful push")
	}
	if _, ok := gitBreachRemoteRow(r, "origin"); !ok {
		t.Error("origin missing from the Remotes table after reload")
	}
}

func TestApplyGitBreachPushRemoteOnFailureStillReloads(t *testing.T) {
	r, dir := gitBreachFetchFixture(t)

	r.applyGitBreachPushRemote(dir, "origin", fmt.Errorf("non-fast-forward"))

	if _, ok := gitBreachRemoteRow(r, "origin"); !ok {
		t.Error("origin missing from the Remotes table — a failed push should still reload")
	}
}
