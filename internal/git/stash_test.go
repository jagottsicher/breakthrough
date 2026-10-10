package git

import (
	"context"
	"os"
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
	// comment on why this package offers apply/drop separately rather
	// than pop.
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
