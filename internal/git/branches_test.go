package git

import (
	"context"
	"testing"
)

func TestBranchesListsLocalBranchesWithCurrentMarked(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")
	runGit(t, dir, "branch", "feature-x")
	runGit(t, dir, "branch", "feature-y")

	branches, err := Branches(context.Background(), dir)
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	if len(branches) != 3 {
		t.Fatalf("Branches = %+v, want 3 (main, feature-x, feature-y)", branches)
	}

	byName := map[string]Branch{}
	for _, b := range branches {
		byName[b.Name] = b
	}
	if !byName["main"].Current {
		t.Errorf("main.Current = false, want true")
	}
	if byName["feature-x"].Current || byName["feature-y"].Current {
		t.Errorf("a non-checked-out branch reports Current = true: %+v", branches)
	}
}

func TestBranchesOnARepositoryWithOnlyOneBranch(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	branches, err := Branches(context.Background(), dir)
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	if len(branches) != 1 || branches[0].Name != "main" || !branches[0].Current {
		t.Errorf("Branches = %+v, want exactly [{main true}]", branches)
	}
}

func TestCheckoutSwitchesTheCurrentBranch(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")
	runGit(t, dir, "branch", "feature-x")

	if err := Checkout(context.Background(), dir, "feature-x"); err != nil {
		t.Fatalf("Checkout: %v", err)
	}

	branches, err := Branches(context.Background(), dir)
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	for _, b := range branches {
		if b.Name == "feature-x" && !b.Current {
			t.Error("feature-x.Current = false after checking it out, want true")
		}
		if b.Name == "main" && b.Current {
			t.Error("main.Current = true after switching away from it, want false")
		}
	}
}

func TestCheckoutFailsOnANonexistentBranch(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	if err := Checkout(context.Background(), dir, "does-not-exist"); err == nil {
		t.Error("Checkout(does-not-exist): err = nil, want an error")
	}
}

func TestDeleteBranchRemovesAMergedBranch(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")
	runGit(t, dir, "branch", "feature-x")

	if err := DeleteBranch(context.Background(), dir, "feature-x"); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}

	branches, err := Branches(context.Background(), dir)
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	for _, b := range branches {
		if b.Name == "feature-x" {
			t.Error("feature-x still exists after DeleteBranch")
		}
	}
}

// TestDeleteBranchFailsOnAnUnmergedBranch pins the exact reason this
// package only ever calls `git branch -d`, never `-D`: -d itself
// already refuses outright when branch has commits not reachable from
// anywhere else, the one real "would actually lose work" case a
// confirmation would otherwise exist to prevent.
func TestDeleteBranchFailsOnAnUnmergedBranch(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")
	runGit(t, dir, "checkout", "-q", "-b", "feature-x")
	writeFile(t, dir, "a.txt", "a changed")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "unmerged change")
	runGit(t, dir, "checkout", "-q", "main")

	if err := DeleteBranch(context.Background(), dir, "feature-x"); err == nil {
		t.Error("DeleteBranch(feature-x): err = nil, want an error (unmerged commits)")
	}
}

// TestDeleteBranchFailsOnTheCurrentBranch pins the exact behavior
// openGitBreachDeleteBranch's own current-branch guard exists to avoid
// ever having to surface: git itself already refuses this outright
// ("used by worktree"), a real error rather than a silent no-op.
func TestDeleteBranchFailsOnTheCurrentBranch(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	if err := DeleteBranch(context.Background(), dir, "main"); err == nil {
		t.Error("DeleteBranch(main) on the current branch: err = nil, want an error")
	}
}
