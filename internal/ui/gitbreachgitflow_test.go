package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireGitFlowForBreach(t *testing.T) {
	t.Helper()
	requireGitForBreach(t)
	if _, err := exec.LookPath("git-flow"); err != nil {
		t.Skip("git-flow not found on $PATH")
	}
}

// initGitFlowBreachRepo returns a repo already `git flow init -d`-ed,
// with one commit on main/develop — the same baseline
// internal/git's own initGitFlowRepo establishes, rebuilt here via
// runGitBreach so it also picks up gitBreachTestEnv's own fixed
// author identity.
func initGitFlowBreachRepo(t *testing.T) string {
	t.Helper()
	requireGitFlowForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "base")
	runGitBreach(t, dir, "flow", "init", "-d")
	return dir
}

func TestOpenGitBreachOnAGitFlowRepoShowsTheNewFeatureHint(t *testing.T) {
	dir := initGitFlowBreachRepo(t)

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	if !r.gitBreach.GitFlowAvailable {
		t.Fatal("GitFlowAvailable = false on a `git flow init`-ed repo")
	}
	if r.gitBreach.GitFlowFeaturePrefix != "feature/" {
		t.Errorf("GitFlowFeaturePrefix = %q, want %q", r.gitBreach.GitFlowFeaturePrefix, "feature/")
	}
}

func TestGitFlowAvailableIsFalseOnAnOrdinaryRepo(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	if r.gitBreach.GitFlowAvailable {
		t.Error("GitFlowAvailable = true on a repo never `git flow init`-ed")
	}
}

func TestOpenGitBreachGitFlowFeatureStartIsANoOpOffTheBranchesSubTab(t *testing.T) {
	dir := initGitFlowBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.toggleGitBreachBranchesTab() // -> Tags

	r.openGitBreachGitFlowFeatureStart()

	if r.activePage == promptPage {
		t.Error("activePage = promptPage, want a silent no-op off the Branches sub-tab")
	}
}

func TestOpenGitBreachGitFlowFeatureStartPromptsThenCreatesTheBranch(t *testing.T) {
	dir := initGitFlowBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.BranchesTable)

	r.openGitBreachGitFlowFeatureStart()

	if r.activePage != promptPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, promptPage)
	}
	r.promptSubmit("my-feature")

	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage after a feature start that should have succeeded")
	}
	if _, ok := gitBreachBranchRow(r, "feature/my-feature"); !ok {
		t.Error("feature/my-feature not found in the Branches table after feature start")
	}
}

func TestOpenGitBreachGitFlowFeatureFinishIsANoOpOnANonFeatureBranch(t *testing.T) {
	dir := initGitFlowBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.BranchesTable)

	row, ok := gitBreachBranchRow(r, "develop")
	if !ok {
		t.Fatal("develop not found in the Branches table")
	}
	r.gitBreach.BranchesTable.Select(row, 0)

	r.openGitBreachGitFlowFeatureFinish()

	if r.activePage == confirmPage {
		t.Error("activePage = confirmPage, want a silent no-op on a non-feature branch")
	}
}

func TestOpenGitBreachGitFlowFeatureFinishConfirmsWithTheExactCommand(t *testing.T) {
	dir := initGitFlowBreachRepo(t)
	runGitBreach(t, dir, "flow", "feature", "start", "my-feature")
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.BranchesTable)

	row, ok := gitBreachBranchRow(r, "feature/my-feature")
	if !ok {
		t.Fatal("feature/my-feature not found in the Branches table")
	}
	r.gitBreach.BranchesTable.Select(row, 0)

	r.openGitBreachGitFlowFeatureFinish()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, confirmPage)
	}
	got := r.confirmDialogTitleBar.GetText(true)
	want := "git flow feature finish my-feature"
	if !strings.Contains(got, want) {
		t.Errorf("confirm dialog text = %q, want it to contain %q", got, want)
	}

	r.pendingConfirm()

	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage after a clean finish that should have succeeded")
	}
	if _, ok := gitBreachBranchRow(r, "feature/my-feature"); ok {
		t.Error("feature/my-feature still exists after a clean finish")
	}
}

func TestOpenGitBreachOperationContinueDeletesTheFeatureBranchAfterAConflictingFinish(t *testing.T) {
	dir := initGitFlowBreachRepo(t)
	runGitBreach(t, dir, "flow", "feature", "start", "side")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("side change"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "commit", "-q", "-am", "side change")
	runGitBreach(t, dir, "checkout", "-q", "develop")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("develop change"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "commit", "-q", "-am", "develop change")
	runGitBreach(t, dir, "checkout", "-q", "feature/side")
	// A direct `git flow feature finish` run outside the dashboard,
	// same as production's own GitFlowFeatureFinish would — sets up
	// the real MERGE_MSG/MERGE_HEAD state PendingGitFlowFinish reads.
	exec.Command("git", "-C", dir, "flow", "feature", "finish", "side").Run() //nolint:errcheck

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	if r.gitBreach.PendingGitFlowFinish != "side" {
		t.Fatalf("PendingGitFlowFinish = %q, want %q", r.gitBreach.PendingGitFlowFinish, "side")
	}
	runGitBreach(t, dir, "checkout", "--ours", "a.txt")
	runGitBreach(t, dir, "add", "a.txt")

	r.openGitBreachOperationContinue()

	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage after resolving and continuing")
	}
	if _, ok := gitBreachBranchRow(r, "feature/side"); ok {
		t.Error("feature/side still exists after Continue completed its own finish")
	}
}
