package git

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestInProgressOperationIsNoneOnACleanRepository(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "base")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "base")

	if op := InProgressOperation(dir); op != OperationNone {
		t.Errorf("InProgressOperation = %v, want OperationNone", op)
	}
}

// conflictedMergeRepo builds a repo with a real, git-produced merge
// conflict on a.txt and leaves the merge in progress — the same setup
// TestFetchReportsAConflict already uses, factored out here since every
// test below needs it.
func conflictedMergeRepo(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "base\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "base")

	runGit(t, dir, "switch", "-q", "-c", "side")
	writeFile(t, dir, "a.txt", "side change\n")
	runGit(t, dir, "commit", "-q", "-am", "side change")

	runGit(t, dir, "switch", "-q", "main")
	writeFile(t, dir, "a.txt", "main change\n")
	runGit(t, dir, "commit", "-q", "-am", "main change")

	mergeCmd := exec.Command("git", "-C", dir, "merge", "-q", "--no-edit", "side")
	mergeCmd.Env = gitTestEnv()
	if err := mergeCmd.Run(); err == nil {
		t.Fatal("setup: merge of two conflicting changes to the same line succeeded, want a conflict")
	}
	return dir
}

func TestInProgressOperationDetectsAMerge(t *testing.T) {
	requireGit(t)
	dir := conflictedMergeRepo(t)

	if op := InProgressOperation(dir); op != OperationMerge {
		t.Errorf("InProgressOperation = %v, want OperationMerge", op)
	}
}

func TestInProgressOperationDetectsACherryPick(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "base\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "base")

	runGit(t, dir, "switch", "-q", "-c", "side")
	writeFile(t, dir, "a.txt", "side change\n")
	runGit(t, dir, "commit", "-q", "-am", "side change")
	pickCmd := exec.Command("git", "-C", dir, "rev-parse", "side")
	pickCmd.Env = gitTestEnv()
	out, err := pickCmd.Output()
	if err != nil {
		t.Fatalf("rev-parse side: %v", err)
	}
	pickHash := strings.TrimSpace(string(out))

	runGit(t, dir, "switch", "-q", "main")
	writeFile(t, dir, "a.txt", "main change\n")
	runGit(t, dir, "commit", "-q", "-am", "main change")

	cherryCmd := exec.Command("git", "-C", dir, "cherry-pick", pickHash)
	cherryCmd.Env = gitTestEnv()
	if err := cherryCmd.Run(); err == nil {
		t.Fatal("setup: cherry-pick of a conflicting change succeeded, want a conflict")
	}

	if op := InProgressOperation(dir); op != OperationCherryPick {
		t.Errorf("InProgressOperation = %v, want OperationCherryPick", op)
	}
}

func TestParseConflictsFindsOneSectionWithItsLabels(t *testing.T) {
	content := "line1\n<<<<<<< HEAD\nours1\nours2\n=======\ntheirs1\n>>>>>>> side\nline2\n"

	sections := ParseConflicts(content)
	if len(sections) != 1 {
		t.Fatalf("len(sections) = %d, want 1", len(sections))
	}
	sec := sections[0]
	if sec.OursLabel != "HEAD" {
		t.Errorf("OursLabel = %q, want %q", sec.OursLabel, "HEAD")
	}
	if sec.TheirsLabel != "side" {
		t.Errorf("TheirsLabel = %q, want %q", sec.TheirsLabel, "side")
	}
	if got := strings.Join(sec.Ours, "|"); got != "ours1|ours2" {
		t.Errorf("Ours = %q, want %q", got, "ours1|ours2")
	}
	if got := strings.Join(sec.Theirs, "|"); got != "theirs1" {
		t.Errorf("Theirs = %q, want %q", got, "theirs1")
	}
}

func TestParseConflictsFindsTwoIndependentSections(t *testing.T) {
	content := strings.Join([]string{
		"start",
		"<<<<<<< HEAD", "a-ours", "=======", "a-theirs", ">>>>>>> side",
		"middle",
		"<<<<<<< HEAD", "b-ours", "=======", "b-theirs", ">>>>>>> side",
		"end",
	}, "\n")

	sections := ParseConflicts(content)
	if len(sections) != 2 {
		t.Fatalf("len(sections) = %d, want 2", len(sections))
	}
	if got := strings.Join(sections[0].Ours, ""); got != "a-ours" {
		t.Errorf("sections[0].Ours = %q, want %q", got, "a-ours")
	}
	if got := strings.Join(sections[1].Theirs, ""); got != "b-theirs" {
		t.Errorf("sections[1].Theirs = %q, want %q", got, "b-theirs")
	}
}

func TestParseConflictsIgnoresAnUnterminatedMarker(t *testing.T) {
	content := "start\n<<<<<<< HEAD\nours\n=======\ntheirs\n"

	if sections := ParseConflicts(content); len(sections) != 0 {
		t.Errorf("len(sections) = %d, want 0 for a conflict with no closing \">>>>>>>\"", len(sections))
	}
}

func TestParseConflictsDoesNotMatchAShorterRunOfAngleBrackets(t *testing.T) {
	content := "<<< not a real marker\n"

	if sections := ParseConflicts(content); len(sections) != 0 {
		t.Errorf("len(sections) = %d, want 0 for a run shorter than seven characters", len(sections))
	}
}

func TestResolveConflictPicksOneSideAndLeavesTheOtherSectionAlone(t *testing.T) {
	content := strings.Join([]string{
		"start",
		"<<<<<<< HEAD", "a-ours", "=======", "a-theirs", ">>>>>>> side",
		"middle",
		"<<<<<<< HEAD", "b-ours", "=======", "b-theirs", ">>>>>>> side",
		"end",
	}, "\n")
	sections := ParseConflicts(content)

	resolved, err := ResolveConflict(content, sections, 0, SideOurs)
	if err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}
	want := strings.Join([]string{
		"start",
		"a-ours",
		"middle",
		"<<<<<<< HEAD", "b-ours", "=======", "b-theirs", ">>>>>>> side",
		"end",
	}, "\n")
	if resolved != want {
		t.Errorf("ResolveConflict(0, SideOurs) =\n%q\nwant\n%q", resolved, want)
	}
}

func TestResolveConflictTheirsSide(t *testing.T) {
	content := "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> side\n"
	sections := ParseConflicts(content)

	resolved, err := ResolveConflict(content, sections, 0, SideTheirs)
	if err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}
	if resolved != "theirs\n" {
		t.Errorf("ResolveConflict(0, SideTheirs) = %q, want %q", resolved, "theirs\n")
	}
}

func TestResolveConflictRejectsAnOutOfRangeIndex(t *testing.T) {
	content := "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> side\n"
	sections := ParseConflicts(content)

	if _, err := ResolveConflict(content, sections, 1, SideOurs); err == nil {
		t.Error("ResolveConflict with an out-of-range index succeeded, want an error")
	}
}

func TestContinueOperationAfterResolvingAMergeConflict(t *testing.T) {
	requireGit(t)
	dir := conflictedMergeRepo(t)

	writeFile(t, dir, "a.txt", "resolved\n")
	if err := Stage(context.Background(), dir, []string{"a.txt"}); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	if err := ContinueOperation(context.Background(), dir, OperationMerge); err != nil {
		t.Fatalf("ContinueOperation: %v", err)
	}
	if op := InProgressOperation(dir); op != OperationNone {
		t.Errorf("InProgressOperation = %v after ContinueOperation, want OperationNone", op)
	}
}

func TestAbortOperationOnAMergeConflict(t *testing.T) {
	requireGit(t)
	dir := conflictedMergeRepo(t)

	if err := AbortOperation(context.Background(), dir, OperationMerge); err != nil {
		t.Fatalf("AbortOperation: %v", err)
	}
	if op := InProgressOperation(dir); op != OperationNone {
		t.Errorf("InProgressOperation = %v after AbortOperation, want OperationNone", op)
	}
}

func TestSkipOperationRefusesOnAMerge(t *testing.T) {
	requireGit(t)
	dir := conflictedMergeRepo(t)

	if err := SkipOperation(context.Background(), dir, OperationMerge); err == nil {
		t.Error("SkipOperation(OperationMerge) succeeded, want an error — git merge has no --skip")
	}
	if op := InProgressOperation(dir); op != OperationMerge {
		t.Errorf("InProgressOperation = %v after a refused SkipOperation, want it still OperationMerge", op)
	}
}

func TestContinueOperationWithNoneInProgressFails(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "base")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "base")

	if err := ContinueOperation(context.Background(), dir, OperationNone); err == nil {
		t.Error("ContinueOperation(OperationNone) succeeded, want an error")
	}
}
