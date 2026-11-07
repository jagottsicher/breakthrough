package git

import (
	"context"
	"testing"
)

func TestBlameAttributesEachLineToTheCommitThatLastTouchedIt(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "one\ntwo\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "first")
	writeFile(t, dir, "a.txt", "one\ntwo changed\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "second")

	lines, err := Blame(context.Background(), dir, "a.txt")
	if err != nil {
		t.Fatalf("Blame: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("Blame = %+v, want 2 lines", lines)
	}
	if lines[0].Content != "one" || lines[1].Content != "two changed" {
		t.Errorf("Blame content = %q, %q, want [one, two changed]", lines[0].Content, lines[1].Content)
	}
	if lines[0].Hash == lines[1].Hash {
		t.Error("Blame: unchanged line 1 and changed line 2 should come from two different commits")
	}
	if lines[1].Hash == "" || lines[1].Short == "" {
		t.Error("Blame: Hash/Short left empty")
	}
	if lines[1].AuthorName != "Test" || lines[1].AuthorEmail != "test@example.com" {
		t.Errorf("Blame: AuthorName/AuthorEmail = %q/%q, want Test/test@example.com", lines[1].AuthorName, lines[1].AuthorEmail)
	}
	if lines[1].When.IsZero() {
		t.Error("Blame: When left zero")
	}
	if lines[0].LineNo != 1 || lines[1].LineNo != 2 {
		t.Errorf("Blame LineNo = %d, %d, want [1, 2]", lines[0].LineNo, lines[1].LineNo)
	}
}

func TestBlameOnAnUncommittedFileAttributesToNotCommittedYet(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "one\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "first")
	writeFile(t, dir, "a.txt", "one\ntwo\n")

	lines, err := Blame(context.Background(), dir, "a.txt")
	if err != nil {
		t.Fatalf("Blame: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("Blame = %+v, want 2 lines", lines)
	}
	if lines[1].AuthorName != "Not Committed Yet" {
		t.Errorf("Blame: uncommitted line AuthorName = %q, want %q", lines[1].AuthorName, "Not Committed Yet")
	}
}
