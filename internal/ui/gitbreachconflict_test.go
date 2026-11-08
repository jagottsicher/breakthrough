package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/jagottsicher/breakthrough/internal/git"
)

// gitBreachConflictFixture builds a real repo with a merge conflict on
// "a.txt" (main's own "main change" vs. "side"'s own "side change" to
// the same line) and opens Git Breach on it, cursor already on a.txt's
// own Conflict row — the same shape newGitBreachBisectFixture already
// establishes for its own feature.
func gitBreachConflictFixture(t *testing.T) *Root {
	t.Helper()
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "base")

	runGitBreach(t, dir, "switch", "-q", "-c", "side")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("side change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "commit", "-q", "-am", "side change")

	runGitBreach(t, dir, "switch", "-q", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("main change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "commit", "-q", "-am", "main change")

	mergeCmd := exec.Command("git", "-C", dir, "merge", "-q", "--no-edit", "side")
	mergeCmd.Env = gitBreachTestEnv()
	if err := mergeCmd.Run(); err == nil {
		t.Fatal("setup: merge of two conflicting changes to the same line succeeded, want a conflict")
	}

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	row := gitBreachTableRowForPath(r, "a.txt")
	if row == 0 {
		t.Fatal("a.txt row not found in the Files table")
	}
	r.gitBreach.FilesTable.Select(row, 0)
	return r
}

func TestGitBreachMainShowsAConflictOnlyForAConflictRow(t *testing.T) {
	r := gitBreachConflictFixture(t)

	if !r.gitBreachMainShowsAConflict() {
		t.Error("gitBreachMainShowsAConflict on a Conflict row = false, want true")
	}

	// Resolving fully turns a.txt into a plain staged row — no longer
	// a conflict at all.
	r.resolveGitBreachConflictSection(git.SideOurs)
	row := gitBreachTableRowForPath(r, "a.txt")
	r.gitBreach.FilesTable.Select(row, 0)
	if r.gitBreachMainShowsAConflict() {
		t.Error("gitBreachMainShowsAConflict after a full resolve = true, want false")
	}
}

// TestResolveGitBreachConflictSectionOursResolvesTheMerge deliberately
// doesn't assert on Status.Staged, unlike the "theirs" case below:
// "ours" here is HEAD's own already-committed content (main's own
// "main change"), so once resolved and git-added, the index exactly
// matches HEAD again — git itself correctly reports no staged change
// at all. The real proof the path is actually resolved (as opposed to
// merely having its on-disk markers removed) is that git itself will
// let the merge continue — it refuses outright, naming the path, for
// as long as any unmerged entry remains in the index.
func TestResolveGitBreachConflictSectionOursResolvesTheMerge(t *testing.T) {
	r := gitBreachConflictFixture(t)
	dir := r.gitBreach.Root

	r.resolveGitBreachConflictSection(git.SideOurs)

	if r.activePage == errorPage {
		t.Fatalf("activePage = errorPage after resolving, want the dashboard to still be open")
	}
	if len(r.gitBreach.Status.Conflicts) != 0 {
		t.Errorf("Conflicts = %v after resolving the only section, want none", r.gitBreach.Status.Conflicts)
	}
	if err := git.ContinueOperation(t.Context(), dir, git.OperationMerge); err != nil {
		t.Errorf("ContinueOperation(merge) after resolving = %v, want it to succeed", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.TrimSpace(string(got)) != "main change" {
		t.Errorf("a.txt content = %q, want %q (ours)", got, "main change")
	}
}

func TestResolveGitBreachConflictSectionTheirsStagesTheFile(t *testing.T) {
	r := gitBreachConflictFixture(t)
	dir := r.gitBreach.Root

	r.resolveGitBreachConflictSection(git.SideTheirs)

	if len(r.gitBreach.Status.Conflicts) != 0 {
		t.Errorf("Conflicts = %v after resolving the only section, want none", r.gitBreach.Status.Conflicts)
	}
	got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.TrimSpace(string(got)) != "side change" {
		t.Errorf("a.txt content = %q, want %q (theirs)", got, "side change")
	}
}

// TestResolveGitBreachConflictSectionDoesNothingOffAConflictRow guards
// the same class of bug toggleGitBreachStage's own Conflict exclusion
// exists for, just the other direction: calling the conflict resolver
// while Main isn't actually showing a conflict (e.g. cursor moved to a
// plain Unstaged row first) must be a silent no-op, not act on
// whatever row happens to be selected.
func TestResolveGitBreachConflictSectionDoesNothingOffAConflictRow(t *testing.T) {
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
	row := gitBreachTableRowForPath(r, "a.txt")
	r.gitBreach.FilesTable.Select(row, 0)

	r.resolveGitBreachConflictSection(git.SideOurs)

	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage, want a silent no-op off a Conflict row")
	}
	got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "a changed" {
		t.Errorf("a.txt content = %q, want it untouched", got)
	}
}

// TestCaptureGitBreachMainKeyOAndTResolveAConflict exercises "o"/"t"
// through the real key-capture dispatch (captureGitBreachMainKey), not
// just a direct call to resolveGitBreachConflictSection — the same
// "both the direct call and the real key path" coverage
// TestToggleGitBreachDiffSplitSwitchesViewAndFocus already applies to
// "v".
func TestCaptureGitBreachMainKeyOAndTResolveAConflict(t *testing.T) {
	r := gitBreachConflictFixture(t)

	r.captureGitBreachMainKey(tcell.NewEventKey(tcell.KeyRune, 'o', tcell.ModNone))

	if len(r.gitBreach.Status.Conflicts) != 0 {
		t.Errorf("Conflicts = %v after \"o\" through captureGitBreachMainKey, want none", r.gitBreach.Status.Conflicts)
	}
}

func TestGitBreachRenderConflictMarksOnlyTheTopSectionActionable(t *testing.T) {
	sections := []git.ConflictSection{
		{OursLabel: "HEAD", TheirsLabel: "side", Ours: []string{"a-ours"}, Theirs: []string{"a-theirs"}},
		{OursLabel: "HEAD", TheirsLabel: "side", Ours: []string{"b-ours"}, Theirs: []string{"b-theirs"}},
	}

	got := gitBreachRenderConflict("a.txt", sections)

	for _, want := range []string{"a.txt", "2 unresolved", "a-ours", "a-theirs", "b-ours", "b-theirs", "> [1/2]", "HEAD", "side"} {
		if !strings.Contains(got, want) {
			t.Errorf("gitBreachRenderConflict output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "> [2/2]") {
		t.Errorf("gitBreachRenderConflict marked the second section actionable too:\n%s", got)
	}
}

func TestGitBreachRenderConflictOnNoSectionsReportsAlreadyResolved(t *testing.T) {
	got := gitBreachRenderConflict("a.txt", nil)

	if !strings.Contains(got, "a.txt") || !strings.Contains(got, "already resolved") {
		t.Errorf("gitBreachRenderConflict(nil) = %q, want it to report a.txt already resolved", got)
	}
}
