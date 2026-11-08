package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/git"
)

func TestOpenGitBreachCherryPickOnACleanTreeAppliesImmediately(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "base")
	runGitBreach(t, dir, "checkout", "-q", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("side"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "b.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "side change")
	runGitBreach(t, dir, "checkout", "-q", "main")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.CommitsTable)

	// Commits is "main"'s own history only (the checked-out branch),
	// so "side change" isn't a row here at all — resolve its hash
	// directly, the same shape openGitBreachDiffAgainstRef's own
	// picker would eventually feed in, without needing that picker
	// wired up just for this test.
	sideCmd := exec.Command("git", "-C", dir, "rev-parse", "side")
	sideCmd.Env = gitBreachTestEnv()
	out, err := sideCmd.Output()
	if err != nil {
		t.Fatalf("rev-parse side: %v", err)
	}
	sideHash := strings.TrimSpace(string(out))

	r.cherryPickGitBreachCommit(sideHash)

	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage after a clean cherry-pick that should have succeeded")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Errorf("b.txt missing after cherry-picking side onto main: %v", err)
	}
}

// gitBreachPickerItemFor scans r.picker's own items for one whose main
// text starts with name — used here the same way
// TestOpenGitBreachBisectStartThenMarkThenEndRoundTrip hardcodes an
// index, except by name: the picker's own order (branches, then tags,
// then commits) makes a hardcoded index fragile once more than one of
// each exists.
func gitBreachPickerItemFor(r *Root, name string) int {
	for i := 0; i < r.picker.GetItemCount(); i++ {
		main, _ := r.picker.GetItemText(i)
		if strings.HasPrefix(main, name) {
			return i
		}
	}
	return -1
}

func TestOpenGitBreachCherryPickOpensTheRefPickerThenConfirmsOnADirtyTree(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "base")
	runGitBreach(t, dir, "checkout", "-q", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "b.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "side change")
	runGitBreach(t, dir, "checkout", "-q", "main")
	// Dirty the tree: an unrelated, unstaged change — deliberately not
	// staged, unlike openGitBreachMerge's own dirty-tree test setup.
	// Verified directly against git: unlike merge, `git cherry-pick`
	// refuses outright with *any* staged change present, even to a
	// file the picked commit never touches — only a plain, unstaged
	// worktree edit still lets a cherry-pick actually succeed.
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "c.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "c committed")
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c dirty"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.CommitsTable)

	r.openGitBreachCherryPick()
	if r.activePage != pickerPage {
		t.Fatalf("activePage = %q, want %q (the ref picker)", r.activePage, pickerPage)
	}

	item := gitBreachPickerItemFor(r, "side")
	if item < 0 {
		t.Fatal("\"side\" not found in the ref picker")
	}
	r.picker.SetCurrentItem(item)
	r.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirm dialog on a dirty tree", r.activePage)
	}
	r.pendingConfirm()

	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage after confirming a cherry-pick that should have succeeded")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Errorf("b.txt missing after cherry-picking side onto main: %v", err)
	}
}

// TestOpenGitBreachCherryPickOnARealConflictLeavesTheUserInTheConflictSurface
// mirrors the equivalent Merge/StashPop tests exactly.
func TestOpenGitBreachCherryPickOnARealConflictLeavesTheUserInTheConflictSurface(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "base")
	runGitBreach(t, dir, "checkout", "-q", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line1\nside change\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "commit", "-q", "-am", "side change")
	sideCmd := exec.Command("git", "-C", dir, "rev-parse", "side")
	sideCmd.Env = gitBreachTestEnv()
	out, err := sideCmd.Output()
	if err != nil {
		t.Fatalf("rev-parse side: %v", err)
	}
	sideHash := strings.TrimSpace(string(out))
	runGitBreach(t, dir, "checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line1\nmain change\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "commit", "-q", "-am", "main change")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreach.CommitsTable)

	r.cherryPickGitBreachCommit(sideHash)

	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage on a conflicting cherry-pick, want the dashboard to stay open with the conflict surface")
	}
	if r.gitBreach.Operation != git.OperationCherryPick {
		t.Errorf("Operation = %v after a conflicting cherry-pick, want OperationCherryPick", r.gitBreach.Operation)
	}
	if len(r.gitBreach.Status.Conflicts) != 1 || r.gitBreach.Status.Conflicts[0] != "a.txt" {
		t.Errorf("Conflicts = %v, want [a.txt]", r.gitBreach.Status.Conflicts)
	}
}

func TestOpenGitBreachCherryPickOnAnUnresolvableHashShowsAnError(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	r.cherryPickGitBreachCommit("0000000000000000000000000000000000000000")

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want %q for an unresolvable hash", r.activePage, errorPage)
	}
}
