package git

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestStashListOnARepositoryWithNoStashes(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	stashes, err := StashList(context.Background(), dir)
	if err != nil {
		t.Fatalf("StashList: %v", err)
	}
	if len(stashes) != 0 {
		t.Errorf("StashList = %+v, want none", stashes)
	}
}

func TestStashListNewestFirst(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	writeFile(t, dir, "a.txt", "b")
	runGit(t, dir, "stash", "push", "-q", "-m", "first stash")
	writeFile(t, dir, "a.txt", "c")
	runGit(t, dir, "stash", "push", "-q", "-m", "second stash")

	stashes, err := StashList(context.Background(), dir)
	if err != nil {
		t.Fatalf("StashList: %v", err)
	}
	if len(stashes) != 2 {
		t.Fatalf("StashList = %+v, want 2", stashes)
	}
	if stashes[0].Ref != "stash@{0}" || !strings.Contains(stashes[0].Message, "second stash") {
		t.Errorf("stashes[0] = %+v, want ref stash@{0} and message containing %q", stashes[0], "second stash")
	}
	if stashes[1].Ref != "stash@{1}" || !strings.Contains(stashes[1].Message, "first stash") {
		t.Errorf("stashes[1] = %+v, want ref stash@{1} and message containing %q", stashes[1], "first stash")
	}
}

func TestStashDiffShowsTheStashedChange(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "line one\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	writeFile(t, dir, "a.txt", "line one\nline two\n")
	runGit(t, dir, "stash", "push", "-q", "-m", "wip")

	stashes, err := StashList(context.Background(), dir)
	if err != nil {
		t.Fatalf("StashList: %v", err)
	}
	if len(stashes) != 1 {
		t.Fatalf("StashList = %+v, want 1", stashes)
	}

	diff, err := StashDiff(context.Background(), dir, stashes[0].Ref)
	if err != nil {
		t.Fatalf("StashDiff: %v", err)
	}
	if !strings.Contains(diff, "+line two") {
		t.Errorf("StashDiff = %q, want it to contain the stashed line", diff)
	}
}

func TestStashApplyRestoresTheStashedChangeAndKeepsTheStash(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "line one\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")
	writeFile(t, dir, "a.txt", "line one\nline two\n")
	runGit(t, dir, "stash", "push", "-q", "-m", "wip")

	stashes, err := StashList(context.Background(), dir)
	if err != nil {
		t.Fatalf("StashList: %v", err)
	}
	if len(stashes) != 1 {
		t.Fatalf("StashList = %+v, want 1", stashes)
	}

	if err := StashApply(context.Background(), dir, stashes[0].Ref); err != nil {
		t.Fatalf("StashApply: %v", err)
	}

	content, err := os.ReadFile(dir + "/a.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(content), "line two") {
		t.Errorf("a.txt after StashApply = %q, want it to contain the stashed line", content)
	}

	// Apply, deliberately, must never drop the stash — see its own doc
	// comment, and TestStashPopRestoresAndDropsOnACleanApply below for
	// the sibling that does.
	stashes, err = StashList(context.Background(), dir)
	if err != nil {
		t.Fatalf("StashList after apply: %v", err)
	}
	if len(stashes) != 1 {
		t.Errorf("StashList after apply = %+v, want the stash to still exist", stashes)
	}
}

func TestStashDropRemovesTheStash(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")
	writeFile(t, dir, "a.txt", "a changed")
	runGit(t, dir, "stash", "push", "-q", "-m", "wip")

	stashes, err := StashList(context.Background(), dir)
	if err != nil {
		t.Fatalf("StashList: %v", err)
	}
	if len(stashes) != 1 {
		t.Fatalf("StashList = %+v, want 1", stashes)
	}

	if err := StashDrop(context.Background(), dir, stashes[0].Ref); err != nil {
		t.Fatalf("StashDrop: %v", err)
	}

	stashes, err = StashList(context.Background(), dir)
	if err != nil {
		t.Fatalf("StashList after drop: %v", err)
	}
	if len(stashes) != 0 {
		t.Errorf("StashList after drop = %+v, want none", stashes)
	}
}

func TestStashPopRestoresAndDropsOnACleanApply(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "line one\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")
	writeFile(t, dir, "a.txt", "line one\nline two\n")
	runGit(t, dir, "stash", "push", "-q", "-m", "wip")

	stashes, err := StashList(context.Background(), dir)
	if err != nil {
		t.Fatalf("StashList: %v", err)
	}
	if len(stashes) != 1 {
		t.Fatalf("StashList = %+v, want 1", stashes)
	}

	if err := StashPop(context.Background(), dir, stashes[0].Ref); err != nil {
		t.Fatalf("StashPop: %v", err)
	}

	content, err := os.ReadFile(dir + "/a.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(content), "line two") {
		t.Errorf("a.txt after StashPop = %q, want it to contain the stashed line", content)
	}

	stashes, err = StashList(context.Background(), dir)
	if err != nil {
		t.Fatalf("StashList after pop: %v", err)
	}
	if len(stashes) != 0 {
		t.Errorf("StashList after a clean pop = %+v, want the stash gone", stashes)
	}
}

// TestStashPopOnARealConflictReportsNoErrorButKeepsTheStash is the
// discriminating test for StashPop's own doc comment: a conflicting
// pop must report nil (not a failure, Phase B's own conflict surface
// handles it), and — matching real `git stash pop` semantics exactly
// — the stash entry itself must still be there afterward, unlike a
// clean pop above.
func TestStashPopOnARealConflictReportsNoErrorButKeepsTheStash(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "line1\nline2\nline3\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")
	writeFile(t, dir, "a.txt", "line1\nSTASHED\nline3\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "stash", "push", "-q", "-m", "wip")
	// A second, independent, staged change to the exact same line —
	// conflicts with the stash's own change to it once popped. Staged,
	// not just a plain worktree edit: unlike a real branch merge, `git
	// stash pop` only attempts its own 3-way merge (producing real
	// conflict markers) against the index, refusing outright with a
	// different error ("would be overwritten by merge") for a merely
	// unstaged worktree change — verified directly against git itself,
	// not guessed.
	writeFile(t, dir, "a.txt", "line1\nWORKTREE\nline3\n")
	runGit(t, dir, "add", "a.txt")

	stashes, err := StashList(context.Background(), dir)
	if err != nil {
		t.Fatalf("StashList: %v", err)
	}
	if len(stashes) != 1 {
		t.Fatalf("StashList = %+v, want 1", stashes)
	}

	if err := StashPop(context.Background(), dir, stashes[0].Ref); err != nil {
		t.Fatalf("StashPop on a conflicting pop: err = %v, want nil (conflicted, not failed)", err)
	}

	st, err := Fetch(context.Background(), dir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(st.Conflicts) != 1 || st.Conflicts[0] != "a.txt" {
		t.Errorf("Conflicts = %v after a conflicting pop, want [a.txt]", st.Conflicts)
	}

	stashes, err = StashList(context.Background(), dir)
	if err != nil {
		t.Fatalf("StashList after a conflicting pop: %v", err)
	}
	if len(stashes) != 1 {
		t.Errorf("StashList after a conflicting pop = %+v, want the stash kept (matching real `git stash pop`)", stashes)
	}
}

// TestStashPopRefusesWithPreExistingConflicts pins the edge case
// StashPop's own doc comment calls out: git itself refuses a pop
// outright (a real failure) when the working tree already has
// unmerged paths before the pop is even attempted — the before/after
// conflict-count comparison must not mistake those pre-existing
// conflicts for ones this call caused.
func TestStashPopRefusesWithPreExistingConflicts(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "line1\nline2\nline3\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	// Stash something unrelated *before* any conflict exists — `git
	// stash push` itself already refuses outright while any path is
	// unmerged, verified directly against git, so the only way to have
	// a stash ready to pop once a conflict exists is to have pushed it
	// beforehand.
	writeFile(t, dir, "unrelated.txt", "unrelated")
	runGit(t, dir, "add", "unrelated.txt")
	runGit(t, dir, "stash", "push", "-q", "-m", "wip", "--", "unrelated.txt")
	stashes, err := StashList(context.Background(), dir)
	if err != nil {
		t.Fatalf("StashList: %v", err)
	}
	if len(stashes) != 1 {
		t.Fatalf("StashList = %+v, want 1", stashes)
	}

	runGit(t, dir, "switch", "-q", "-c", "side")
	writeFile(t, dir, "a.txt", "line1\nside\nline3\n")
	runGit(t, dir, "commit", "-q", "-am", "side change")
	runGit(t, dir, "switch", "-q", "main")
	writeFile(t, dir, "a.txt", "line1\nmain\nline3\n")
	runGit(t, dir, "commit", "-q", "-am", "main change")
	mergeCmd := exec.Command("git", "-C", dir, "merge", "-q", "--no-edit", "side")
	mergeCmd.Env = gitTestEnv()
	if err := mergeCmd.Run(); err == nil {
		t.Fatal("setup: merge of two conflicting changes succeeded, want a conflict")
	}

	if err := StashPop(context.Background(), dir, stashes[0].Ref); err == nil {
		t.Error("StashPop with pre-existing unmerged paths: err = nil, want a real error")
	}
}
