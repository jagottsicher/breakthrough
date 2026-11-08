package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/git"
)

func TestOpenGitBreachOperationContinueResolvesAndCompletesTheMerge(t *testing.T) {
	r := gitBreachConflictFixture(t)
	dir := r.gitBreach.Root

	r.resolveGitBreachConflictSection(git.SideOurs)
	if r.gitBreach.Operation != git.OperationMerge {
		t.Fatalf("setup: Operation = %v, want OperationMerge still in progress after resolving", r.gitBreach.Operation)
	}

	r.openGitBreachOperationContinue()

	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage after Continue on a fully resolved merge")
	}
	if r.gitBreach.Operation != git.OperationNone {
		t.Errorf("Operation = %v after Continue, want OperationNone", r.gitBreach.Operation)
	}
	if git.InProgressOperation(dir) != git.OperationNone {
		t.Error("InProgressOperation still reports one in progress after Continue succeeded")
	}
}

func TestOpenGitBreachOperationContinueIsANoOpWithoutOneInProgress(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	if r.gitBreach.Operation != git.OperationNone {
		t.Fatalf("setup: Operation = %v, want OperationNone on a clean repo", r.gitBreach.Operation)
	}

	r.openGitBreachOperationContinue()

	if r.activePage == errorPage {
		t.Error("activePage = errorPage, want a silent no-op with no operation in progress")
	}
}

func TestOpenGitBreachOperationAbortOpensAConfirmThenAborts(t *testing.T) {
	r := gitBreachConflictFixture(t)
	dir := r.gitBreach.Root

	r.openGitBreachOperationAbort()
	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q after Abort, want %q (a confirm first)", r.activePage, confirmPage)
	}

	r.pendingConfirm()

	if git.InProgressOperation(dir) != git.OperationNone {
		t.Error("InProgressOperation still reports a merge in progress after confirming Abort")
	}
	if len(r.gitBreach.Status.Conflicts) != 0 {
		t.Errorf("Conflicts = %v after Abort, want none", r.gitBreach.Status.Conflicts)
	}
}

func TestOpenGitBreachOperationSkipRefusesOnAMerge(t *testing.T) {
	r := gitBreachConflictFixture(t)
	dir := r.gitBreach.Root

	r.openGitBreachOperationSkip()

	if r.activePage != errorPage {
		t.Error("activePage after Skip on a merge, want the error overlay — git merge has no --skip")
	}
	if git.InProgressOperation(dir) != git.OperationMerge {
		t.Error("InProgressOperation changed after a refused Skip, want it still OperationMerge")
	}
}

// gitBreachRebaseConflictFixture builds a repo with a real rebase
// conflict (replaying "pick" onto "base-moved", both touching the same
// line) and opens Git Breach on it — Skip is only meaningfully
// testable against an operation that actually supports it, unlike the
// merge fixture every other test in this file reuses.
func gitBreachRebaseConflictFixture(t *testing.T) *Root {
	t.Helper()
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "base")

	runGitBreach(t, dir, "switch", "-q", "-c", "pick")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("picked change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "commit", "-q", "-am", "picked change")

	runGitBreach(t, dir, "switch", "-q", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("moved base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "commit", "-q", "-am", "moved base")

	rebaseCmd := exec.Command("git", "-C", dir, "rebase", "main", "pick")
	rebaseCmd.Env = gitBreachTestEnv()
	if err := rebaseCmd.Run(); err == nil {
		t.Fatal("setup: rebase of a conflicting change succeeded, want a conflict")
	}
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	if r.gitBreach.Operation != git.OperationRebase {
		t.Fatalf("setup: Operation = %v, want OperationRebase", r.gitBreach.Operation)
	}
	return r
}

func TestOpenGitBreachOperationSkipOnARebaseAbandonsTheConflictingCommit(t *testing.T) {
	r := gitBreachRebaseConflictFixture(t)
	dir := r.gitBreach.Root

	r.openGitBreachOperationSkip()

	if r.activePage == errorPage {
		t.Fatalf("activePage = errorPage after Skip on a rebase")
	}
	if git.InProgressOperation(dir) != git.OperationNone {
		t.Error("InProgressOperation still reports one in progress after Skip finished the rebase")
	}
}

func TestRenderGitBreachStatusShowsTheOperationInProgress(t *testing.T) {
	r := gitBreachConflictFixture(t)

	got := r.gitBreach.StatusView.GetText(true)
	if !strings.Contains(got, "merge in progress") {
		t.Errorf("StatusView = %q, want it to mention \"merge in progress\"", got)
	}
}

func TestGitBreachHintBarShowsContinueAbortButNotSkipDuringAMerge(t *testing.T) {
	r := gitBreachConflictFixture(t)

	entries := r.gitBreachHintEntries()
	if !gitBreachHintHasKey(entries, "C") {
		t.Error("hint bar missing \"C\" (continue) while a merge is in progress")
	}
	if !gitBreachHintHasKey(entries, "A") {
		t.Error("hint bar missing \"A\" (abort) while a merge is in progress")
	}
	if gitBreachHintHasKey(entries, "K") {
		t.Error("hint bar shows \"K\" (skip) during a merge, want it hidden — git merge has no --skip")
	}
}
