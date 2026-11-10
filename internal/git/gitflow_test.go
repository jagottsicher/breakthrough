package git

import (
	"context"
	"os/exec"
	"testing"
)

func requireGitFlow(t *testing.T) {
	t.Helper()
	requireGit(t)
	if _, err := exec.LookPath("git-flow"); err != nil {
		t.Skip("git-flow not found on $PATH")
	}
}

// initGitFlowRepo returns a repo already `git flow init -d`-ed
// (accepting every default prefix/branch name) with one commit on
// main and on develop, matching the state a real `git flow init`
// leaves a fresh repository in.
func initGitFlowRepo(t *testing.T) string {
	t.Helper()
	requireGitFlow(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "base")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "base")
	runGit(t, dir, "flow", "init", "-d")
	return dir
}

func TestGitFlowAvailableOnAnUninitializedRepoIsFalse(t *testing.T) {
	requireGitFlow(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "base")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "base")

	if GitFlowAvailable(context.Background(), dir) {
		t.Error("GitFlowAvailable = true on a repo never `git flow init`-ed")
	}
}

func TestGitFlowAvailableOnAnInitializedRepoIsTrue(t *testing.T) {
	dir := initGitFlowRepo(t)

	if !GitFlowAvailable(context.Background(), dir) {
		t.Error("GitFlowAvailable = false on a `git flow init`-ed repo")
	}
}

func TestGitFlowConfigReadsTheRealPrefixAndDevelopBranch(t *testing.T) {
	dir := initGitFlowRepo(t)

	develop, prefix, ok := GitFlowConfig(context.Background(), dir)
	if !ok {
		t.Fatal("GitFlowConfig: ok = false on an initialized repo")
	}
	if develop != "develop" || prefix != "feature/" {
		t.Errorf("GitFlowConfig = (%q, %q), want (develop, feature/)", develop, prefix)
	}
}

func TestGitFlowFeatureStartCreatesAndChecksOutTheBranch(t *testing.T) {
	dir := initGitFlowRepo(t)

	if err := GitFlowFeatureStart(context.Background(), dir, "my-feature"); err != nil {
		t.Fatalf("GitFlowFeatureStart: %v", err)
	}

	branches, err := Branches(context.Background(), dir)
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	var found, current bool
	for _, b := range branches {
		if b.Name == "feature/my-feature" {
			found = true
			current = b.Current
		}
	}
	if !found {
		t.Error("feature/my-feature not found after GitFlowFeatureStart")
	}
	if !current {
		t.Error("feature/my-feature not checked out after GitFlowFeatureStart")
	}
}

func TestGitFlowFeatureFinishOnACleanMergeDeletesTheBranch(t *testing.T) {
	dir := initGitFlowRepo(t)
	if err := GitFlowFeatureStart(context.Background(), dir, "my-feature"); err != nil {
		t.Fatalf("GitFlowFeatureStart: %v", err)
	}
	writeFile(t, dir, "b.txt", "feature content")
	runGit(t, dir, "add", "b.txt")
	runGit(t, dir, "commit", "-q", "-m", "feature work")

	if err := GitFlowFeatureFinish(context.Background(), dir, "my-feature"); err != nil {
		t.Fatalf("GitFlowFeatureFinish: %v", err)
	}

	branches, err := Branches(context.Background(), dir)
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	for _, b := range branches {
		if b.Name == "feature/my-feature" {
			t.Error("feature/my-feature still exists after a clean GitFlowFeatureFinish")
		}
	}
}

func TestGitFlowFeatureFinishOnARealConflictLeavesAMergeInProgress(t *testing.T) {
	dir := initGitFlowRepo(t)
	if err := GitFlowFeatureStart(context.Background(), dir, "side"); err != nil {
		t.Fatalf("GitFlowFeatureStart: %v", err)
	}
	writeFile(t, dir, "a.txt", "side change")
	runGit(t, dir, "commit", "-q", "-am", "side change")
	runGit(t, dir, "checkout", "-q", "develop")
	writeFile(t, dir, "a.txt", "develop change")
	runGit(t, dir, "commit", "-q", "-am", "develop change")
	runGit(t, dir, "checkout", "-q", "feature/side")

	err := GitFlowFeatureFinish(context.Background(), dir, "side")
	if err != nil {
		t.Fatalf("GitFlowFeatureFinish on a conflict, want nil (conflict is not a failure): %v", err)
	}

	if InProgressOperation(dir) != OperationMerge {
		t.Error("InProgressOperation != OperationMerge after a conflicting finish")
	}
	name, ok := PendingGitFlowFinish(dir)
	if !ok || name != "side" {
		t.Errorf("PendingGitFlowFinish = (%q, %v), want (\"side\", true)", name, ok)
	}
}

func TestPendingGitFlowFinishIsFalseWithNoMergeInProgress(t *testing.T) {
	dir := initGitFlowRepo(t)

	if _, ok := PendingGitFlowFinish(dir); ok {
		t.Error("PendingGitFlowFinish = true with no merge in progress at all")
	}
}

func TestPendingGitFlowFinishIsFalseForAnOrdinaryMergeConflict(t *testing.T) {
	dir := initGitFlowRepo(t)
	runGit(t, dir, "checkout", "-q", "-b", "plain-side", "develop")
	writeFile(t, dir, "a.txt", "plain side change")
	runGit(t, dir, "commit", "-q", "-am", "plain side change")
	runGit(t, dir, "checkout", "-q", "develop")
	writeFile(t, dir, "a.txt", "plain develop change")
	runGit(t, dir, "commit", "-q", "-am", "plain develop change")

	cmd := exec.Command("git", "-C", dir, "merge", "--no-edit", "plain-side")
	_ = cmd.Run() // expected to fail with a conflict

	if InProgressOperation(dir) != OperationMerge {
		t.Fatal("setup: expected a real merge conflict in progress")
	}
	if _, ok := PendingGitFlowFinish(dir); ok {
		t.Error("PendingGitFlowFinish = true for an ordinary, non-git-flow merge conflict")
	}
}
