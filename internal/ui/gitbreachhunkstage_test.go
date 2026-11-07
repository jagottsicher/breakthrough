package ui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/git"
)

// twoHunkDiff is a real, hand-verified two-hunk unified diff (the
// shape `git diff` produces for a file with two far-apart changes) —
// shared by every test below that needs one.
const twoHunkDiff = `diff --git a/a.txt b/a.txt
index 1111111..2222222 100644
--- a/a.txt
+++ b/a.txt
@@ -1,5 +1,5 @@
 line 1
-line 2
+line 2 changed
 line 3
 line 4
 line 5
@@ -15,6 +15,6 @@ line 14
 line 15
 line 16
 line 17
-line 18
+line 18 changed
 line 19
 line 20`

func TestGitBreachHunkAtPicksTheRightHunkForEveryLineIndex(t *testing.T) {
	lines := strings.Split(twoHunkDiff, "\n")
	// Hunk 0's own "@@" line is index 4 (0-indexed: diff --git, index,
	// ---, +++, then @@) and spans through index 10; hunk 1's "@@"
	// line is index 11 (verified directly against
	// strings.Split(twoHunkDiff, "\n"), not hand-counted).
	cases := []struct {
		name      string
		lineIndex int
		want      int
	}{
		{"before the first hunk (still on the file header)", 2, 0},
		{"exactly on the first hunk's own \"@@\" line", 4, 0},
		{"a content line inside the first hunk", 6, 0},
		{"exactly on the second hunk's own \"@@\" line", 11, 1},
		{"a content line inside the second hunk", 13, 1},
		{"past the last line entirely", 999, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := gitBreachHunkAt(lines, c.lineIndex)
			if !ok {
				t.Fatal("gitBreachHunkAt = false, want a hunk found")
			}
			if got != c.want {
				t.Errorf("gitBreachHunkAt(%d) = %d, want %d", c.lineIndex, got, c.want)
			}
		})
	}
}

func TestGitBreachHunkAtOnANonHunkedTextReportsFalse(t *testing.T) {
	if _, ok := gitBreachHunkAt([]string{"no diff to show."}, 0); ok {
		t.Error("gitBreachHunkAt on text with no \"@@\" line = true, want false")
	}
}

// TestGitBreachHunkPatchBuildsAByteExactSingleHunkPatch is the
// discriminating test: the patch for each hunk, applied for real via
// git.ApplyPatchCached against a real repo, must be valid on its own
// — a correctness property pure string assertions can't fully stand
// in for. Checked both ways: structural content, and that git itself
// accepts it (a separate, fresh repo per hunk — applying a hunk is
// destructive to the index, so the two can't share one without the
// first hunk's own application affecting the second's).
func TestGitBreachHunkPatchBuildsAByteExactSingleHunkPatch(t *testing.T) {
	requireGitForBreach(t)
	cases := []struct {
		hunkIndex int
		wantLine  string
	}{
		{0, "+line 2 changed"},
		{1, "+line 18 changed"},
	}
	for _, c := range cases {
		patch, ok := gitBreachHunkPatch(twoHunkDiff, c.hunkIndex)
		if !ok {
			t.Fatalf("gitBreachHunkPatch(%d) = false, want a patch", c.hunkIndex)
		}
		if !strings.HasPrefix(patch, "diff --git a/a.txt b/a.txt\n") {
			t.Errorf("hunk %d patch doesn't start with the file header:\n%s", c.hunkIndex, patch)
		}
		if !strings.Contains(patch, c.wantLine) {
			t.Errorf("hunk %d patch missing %q:\n%s", c.hunkIndex, c.wantLine, patch)
		}
		if !strings.HasSuffix(patch, "\n") {
			t.Errorf("hunk %d patch doesn't end in a newline", c.hunkIndex)
		}

		dir := t.TempDir()
		runGitBreach(t, dir, "init", "-q", "-b", "main")
		runGitBreach(t, dir, "config", "user.name", "Test")
		runGitBreach(t, dir, "config", "user.email", "test@example.com")
		var original []string
		for i := 1; i <= 20; i++ {
			original = append(original, "line "+strconv.Itoa(i))
		}
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(strings.Join(original, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGitBreach(t, dir, "add", "a.txt")
		runGitBreach(t, dir, "commit", "-q", "-m", "initial")
		// The working tree needs the matching unstaged change too —
		// `git apply --cached` applies against the index, but a
		// worktree that still reads the *original* content would
		// otherwise disagree with the index it just updated, the
		// same "MM"-shaped state a real hunk-stage leaves behind.
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(strings.ReplaceAll(strings.ReplaceAll(
			strings.Join(original, "\n")+"\n", "line 2\n", "line 2 changed\n"), "line 18\n", "line 18 changed\n")), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := git.ApplyPatchCached(context.Background(), dir, patch); err != nil {
			t.Errorf("hunk %d: ApplyPatchCached: %v\npatch:\n%s", c.hunkIndex, err, patch)
		}
	}
}

func TestGitBreachHunkPatchOnAnOutOfRangeIndexReportsFalse(t *testing.T) {
	if _, ok := gitBreachHunkPatch(twoHunkDiff, 2); ok {
		t.Error("gitBreachHunkPatch(2) on a two-hunk diff = true, want false (only hunks 0/1 exist)")
	}
	if _, ok := gitBreachHunkPatch(twoHunkDiff, -1); ok {
		t.Error("gitBreachHunkPatch(-1) = true, want false")
	}
}

// TestGitBreachMainShowsAStageableHunkOnlyForFilesUnstagedRows pins
// the shared guard both openGitBreachStageHunk and the hint bar rely
// on: true only for Files' own unstaged diff, false for every other
// combination (wrong MainOwner, or a Staged row under the Files
// cursor).
func TestGitBreachMainShowsAStageableHunkOnlyForFilesUnstagedRows(t *testing.T) {
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

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	if r.gitBreach.MainOwner != gitBreachMainOwnerFiles {
		t.Fatal("setup: MainOwner isn't Files by default")
	}
	row := gitBreachTableRowForPath(r, "a.txt")
	r.gitBreach.FilesTable.Select(row, 0)

	if !r.gitBreachMainShowsAStageableHunk() {
		t.Error("gitBreachMainShowsAStageableHunk on an unstaged row = false, want true")
	}

	r.toggleGitBreachStage() // now staged, not unstaged
	if r.gitBreachMainShowsAStageableHunk() {
		t.Error("gitBreachMainShowsAStageableHunk on a staged row = true, want false")
	}
}
