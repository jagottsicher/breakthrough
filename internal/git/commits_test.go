package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestLogListsCommitsNewestFirst(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "first")
	writeFile(t, dir, "a.txt", "a changed")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "second")

	commits, err := Log(context.Background(), dir, CommitLogLimit)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("Log = %+v, want 2 commits", commits)
	}
	if commits[0].Subject != "second" || commits[1].Subject != "first" {
		t.Errorf("Log order = %q, %q, want [second, first] (newest first)", commits[0].Subject, commits[1].Subject)
	}
	if commits[0].Hash == "" || commits[0].Short == "" {
		t.Error("Log: Hash/Short left empty")
	}
	if commits[0].When.IsZero() {
		t.Error("Log: When left zero")
	}
	if commits[0].AuthorName != "Test" || commits[0].AuthorEmail != "test@example.com" {
		t.Errorf("Log: AuthorName/AuthorEmail = %q/%q, want Test/test@example.com", commits[0].AuthorName, commits[0].AuthorEmail)
	}
}

// TestLogOnARepositoryWithNoCommitsYet pins a real distinction: `git
// log` on a brand new repository fails outright (there's nothing to
// log), but that's not the same thing as a real error — see Log's own
// doc comment on why this needs to come back as (nil, nil) rather than
// surfacing git's own "does not have any commits yet" message as if
// something were actually broken.
func TestLogOnARepositoryWithNoCommitsYet(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)

	commits, err := Log(context.Background(), dir, CommitLogLimit)
	if err != nil {
		t.Fatalf("Log on an empty repository: err = %v, want nil", err)
	}
	if len(commits) != 0 {
		t.Errorf("Log on an empty repository = %+v, want none", commits)
	}
}

func TestCommitDiffShowsTheCommitsOwnChange(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "line one\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "first")
	writeFile(t, dir, "a.txt", "line one\nline two\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "second")

	commits, err := Log(context.Background(), dir, CommitLogLimit)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	diff, err := CommitDiff(context.Background(), dir, commits[0].Hash)
	if err != nil {
		t.Fatalf("CommitDiff: %v", err)
	}
	if !strings.Contains(diff, "+line two") {
		t.Errorf("CommitDiff = %q, want it to contain the added line", diff)
	}
}

// TestDiffRefsComparesTwoArbitraryRefsDirectly pins that DiffRefs
// isn't limited to parent/child the way CommitDiff is: a tag and a
// branch, neither one the other's ancestor, diff cleanly against each
// other.
func TestDiffRefsComparesTwoArbitraryRefsDirectly(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "line one\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "first")
	runGit(t, dir, "tag", "v1")
	writeFile(t, dir, "a.txt", "line one\nline two\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "second")

	diff, err := DiffRefs(context.Background(), dir, "v1", "main")
	if err != nil {
		t.Fatalf("DiffRefs: %v", err)
	}
	if !strings.Contains(diff, "+line two") {
		t.Errorf("DiffRefs(v1, main) = %q, want it to contain the added line", diff)
	}
}

// TestDiffRefsReversedSwapsAddedAndRemoved pins the whole of this
// package's own "reverse diff" feature: swapping the two arguments,
// nothing else.
func TestDiffRefsReversedSwapsAddedAndRemoved(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "line one\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "first")
	runGit(t, dir, "tag", "v1")
	writeFile(t, dir, "a.txt", "line one\nline two\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "second")

	reversed, err := DiffRefs(context.Background(), dir, "main", "v1")
	if err != nil {
		t.Fatalf("DiffRefs: %v", err)
	}
	if !strings.Contains(reversed, "-line two") {
		t.Errorf("DiffRefs(main, v1) = %q, want the added line to show as removed", reversed)
	}
}

// TestDiffRefsOnAnUnresolvableRefSurfacesGitsOwnError pins that an
// invalid ref name comes back as a real error, not an empty diff —
// the likely failure mode once this is reachable from a free-text
// prompt rather than only a picker of refs git already knows about.
func TestDiffRefsOnAnUnresolvableRefSurfacesGitsOwnError(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	if _, err := DiffRefs(context.Background(), dir, "main", "no-such-ref"); err == nil {
		t.Error("DiffRefs with an unresolvable ref = nil error, want git's own refusal")
	}
}

// TestCommitDiffOnTheRootCommit pins the exact reason CommitDiff uses
// `git show`, not `git diff <hash>^ <hash>`: the root commit has no
// parent to diff against, which the latter form fails on outright.
func TestCommitDiffOnTheRootCommit(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "line one\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "root commit")

	commits, err := Log(context.Background(), dir, CommitLogLimit)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("Log = %+v, want exactly the root commit", commits)
	}

	diff, err := CommitDiff(context.Background(), dir, commits[0].Hash)
	if err != nil {
		t.Fatalf("CommitDiff on the root commit: %v", err)
	}
	if !strings.Contains(diff, "+line one") {
		t.Errorf("CommitDiff on the root commit = %q, want it to contain the added line", diff)
	}
}

// TestLogRespectsASmallerLimit pins Log's own second argument actually
// bounding how many commits come back — internal/ui's own
// maybeLoadMoreGitBreachCommits relies on Log returning fewer than
// limit as the signal that it just fetched every commit there is.
func TestLogRespectsASmallerLimit(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	for i := 0; i < 5; i++ {
		writeFile(t, dir, "a.txt", fmt.Sprintf("line %d", i))
		runGit(t, dir, "add", "a.txt")
		runGit(t, dir, "commit", "-q", "-m", fmt.Sprintf("commit %d", i))
	}

	commits, err := Log(context.Background(), dir, 3)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(commits) != 3 {
		t.Fatalf("Log with limit 3 = %+v, want exactly 3", commits)
	}
	if commits[0].Subject != "commit 4" {
		t.Errorf("Log with limit 3, newest = %q, want %q", commits[0].Subject, "commit 4")
	}
}

func TestCommitCountMatchesTheRealCommitCount(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	for i := 0; i < 4; i++ {
		writeFile(t, dir, "a.txt", fmt.Sprintf("line %d", i))
		runGit(t, dir, "add", "a.txt")
		runGit(t, dir, "commit", "-q", "-m", fmt.Sprintf("commit %d", i))
	}

	count, err := CommitCount(context.Background(), dir)
	if err != nil {
		t.Fatalf("CommitCount: %v", err)
	}
	if count != 4 {
		t.Errorf("CommitCount = %d, want 4", count)
	}
}

// TestCommitCountOnARepositoryWithNoCommitsYet mirrors
// TestLogOnARepositoryWithNoCommitsYet exactly, for the same reason:
// `git rev-list --count HEAD` on a brand new repository fails outright,
// but that's not a real error, just nothing to count yet.
func TestCommitCountOnARepositoryWithNoCommitsYet(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)

	count, err := CommitCount(context.Background(), dir)
	if err != nil {
		t.Fatalf("CommitCount on an empty repository: err = %v, want nil", err)
	}
	if count != 0 {
		t.Errorf("CommitCount on an empty repository = %d, want 0", count)
	}
}

func TestCherryPickAppliesACleanCommitOntoCurrentHead(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "base")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "base")
	runGit(t, dir, "checkout", "-q", "-b", "side")
	writeFile(t, dir, "b.txt", "side")
	runGit(t, dir, "add", "b.txt")
	runGit(t, dir, "commit", "-q", "-m", "side change")
	sideCmd := exec.Command("git", "-C", dir, "rev-parse", "side")
	sideCmd.Env = gitTestEnv()
	out, err := sideCmd.Output()
	if err != nil {
		t.Fatalf("rev-parse side: %v", err)
	}
	sideHash := strings.TrimSpace(string(out))
	runGit(t, dir, "checkout", "-q", "main")

	if err := CherryPick(context.Background(), dir, sideHash); err != nil {
		t.Fatalf("CherryPick: %v", err)
	}
	if InProgressOperation(dir) != OperationNone {
		t.Error("InProgressOperation = true after a clean cherry-pick, want OperationNone")
	}
	content, err := os.ReadFile(dir + "/b.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "side" {
		t.Errorf("b.txt = %q, want the cherry-picked content", content)
	}
}

// TestCherryPickOnARealConflictReportsNoErrorButLeavesOneInProgress
// mirrors Merge's own discriminating test exactly.
func TestCherryPickOnARealConflictReportsNoErrorButLeavesOneInProgress(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "base\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "base")
	runGit(t, dir, "checkout", "-q", "-b", "side")
	writeFile(t, dir, "a.txt", "side change\n")
	runGit(t, dir, "commit", "-q", "-am", "side change")
	sideCmd := exec.Command("git", "-C", dir, "rev-parse", "side")
	sideCmd.Env = gitTestEnv()
	out, err := sideCmd.Output()
	if err != nil {
		t.Fatalf("rev-parse side: %v", err)
	}
	sideHash := strings.TrimSpace(string(out))
	runGit(t, dir, "checkout", "-q", "main")
	writeFile(t, dir, "a.txt", "main change\n")
	runGit(t, dir, "commit", "-q", "-am", "main change")

	if err := CherryPick(context.Background(), dir, sideHash); err != nil {
		t.Fatalf("CherryPick on a conflicting pick: err = %v, want nil (conflicted, not failed)", err)
	}
	if InProgressOperation(dir) != OperationCherryPick {
		t.Error("InProgressOperation = false after a conflicting cherry-pick, want OperationCherryPick")
	}
}

func TestCherryPickOnAnUnknownHashFails(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "base")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "base")

	if err := CherryPick(context.Background(), dir, "0000000000000000000000000000000000000000"); err == nil {
		t.Error("CherryPick(unknown hash): err = nil, want an error")
	}
	if InProgressOperation(dir) != OperationNone {
		t.Error("InProgressOperation = true after a pick that never started, want OperationNone")
	}
}
