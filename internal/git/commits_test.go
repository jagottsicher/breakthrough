package git

import (
	"context"
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

	commits, err := Log(context.Background(), dir)
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

	commits, err := Log(context.Background(), dir)
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

	commits, err := Log(context.Background(), dir)
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

// TestCommitDiffOnTheRootCommit pins the exact reason CommitDiff uses
// `git show`, not `git diff <hash>^ <hash>`: the root commit has no
// parent to diff against, which the latter form fails on outright.
func TestCommitDiffOnTheRootCommit(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "line one\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "root commit")

	commits, err := Log(context.Background(), dir)
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
