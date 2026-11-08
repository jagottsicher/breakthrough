package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/git"
)

func TestOpenGitBreachStashPopRestoresAndDropsOnACleanTree(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "stash", "push", "-q", "-m", "wip")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.StashTable)
	r.gitBreach.StashTable.Select(0, 0)

	r.openGitBreachStashPop()

	if r.activePage == confirmPage {
		t.Fatal("activePage = confirmPage, want an immediate pop on a clean tree")
	}
	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage after a clean pop that should have succeeded")
	}
	content, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "a changed" {
		t.Errorf("a.txt = %q, want the stashed change restored", content)
	}
	if len(r.gitBreach.Stash) != 0 {
		t.Errorf("gitBreach.Stash = %+v, want the stash gone after a clean pop", r.gitBreach.Stash)
	}
}

func TestOpenGitBreachStashPopConfirmsOnADirtyTree(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "stash", "push", "-q", "-m", "wip")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "b.txt")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.StashTable)
	r.gitBreach.StashTable.Select(0, 0)

	r.openGitBreachStashPop()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirm dialog on a dirty tree", r.activePage)
	}
	r.pendingConfirm()

	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage after confirming a pop that should have succeeded")
	}
	if len(r.gitBreach.Stash) != 0 {
		t.Errorf("gitBreach.Stash = %+v, want the stash gone after confirming a clean pop", r.gitBreach.Stash)
	}
}

func TestOpenGitBreachStashPopDoesNothingWithNoStash(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.StashTable)

	r.openGitBreachStashPop()

	if r.activePage == confirmPage || r.activePage == errorPage {
		t.Errorf("activePage = %q, want a silent no-op with no stash entries", r.activePage)
	}
}

// TestOpenGitBreachStashPopOnARealConflictLeavesTheUserInTheConflictSurface
// mirrors the equivalent Merge test exactly: a conflicting pop must
// land the user in Phase B's own conflict surface, not at an error
// overlay.
func TestOpenGitBreachStashPopOnARealConflictLeavesTheUserInTheConflictSurface(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line1\nSTASHED\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "stash", "push", "-q", "-m", "wip")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line1\nWORKTREE\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.StashTable)
	r.gitBreach.StashTable.Select(0, 0)

	r.openGitBreachStashPop()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirm dialog on a dirty (staged) tree", r.activePage)
	}
	r.pendingConfirm()

	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage on a conflicting pop, want the dashboard to stay open with the conflict surface")
	}
	if len(r.gitBreach.Status.Conflicts) != 1 || r.gitBreach.Status.Conflicts[0] != "a.txt" {
		t.Errorf("Conflicts = %v, want [a.txt]", r.gitBreach.Status.Conflicts)
	}
	if len(r.gitBreach.Stash) != 1 {
		t.Errorf("gitBreach.Stash = %+v, want the stash kept after a conflicting pop", r.gitBreach.Stash)
	}
}

func TestOpenGitBreachStashPopWithPreExistingConflictsShowsAnError(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("unrelated"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "unrelated.txt")
	runGitBreach(t, dir, "stash", "push", "-q", "-m", "wip", "--", "unrelated.txt")

	runGitBreach(t, dir, "switch", "-q", "-c", "side")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line1\nside\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "commit", "-q", "-am", "side change")
	runGitBreach(t, dir, "switch", "-q", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line1\nmain\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "commit", "-q", "-am", "main change")
	mergeCmd := exec.Command("git", "-C", dir, "merge", "-q", "--no-edit", "side")
	mergeCmd.Env = gitBreachTestEnv()
	if err := mergeCmd.Run(); err == nil {
		t.Fatal("setup: merge of two conflicting changes succeeded, want a conflict")
	}

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.StashTable)
	r.gitBreach.StashTable.Select(0, 0)

	r.openGitBreachStashPop()
	if r.activePage == confirmPage {
		r.pendingConfirm()
	}

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want %q for a pop refused by pre-existing unmerged paths", r.activePage, errorPage)
	}
	if r.gitBreach.Operation != git.OperationMerge {
		t.Errorf("Operation = %v, want the pre-existing merge still in progress, untouched by the refused pop", r.gitBreach.Operation)
	}
}
