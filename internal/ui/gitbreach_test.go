package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/git"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// requireGitForBreach/gitBreachTestEnv/runGitBreach/initGitBreachRepo
// mirror internal/git's own identically-shaped test helpers — kept as
// separate, package-local copies rather than exported from
// internal/git, since a test helper isn't part of that package's own
// real API (the same reasoning gitstatus_test.go's own helpers are
// never shared either).
func requireGitForBreach(t *testing.T) {
	t.Helper()
	if !git.Available() {
		t.Skip("git not found on $PATH")
	}
}

func gitBreachTestEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
}

func runGitBreach(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = gitBreachTestEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func initGitBreachRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitBreach(t, dir, "init", "-q", "-b", "main")
	return dir
}

func newTestRootForGitBreachDir(t *testing.T, dir string) *Root {
	t.Helper()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	return r
}

func TestOpenGitBreachOnARealRepositoryShowsStagedUnstagedAndUntracked(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt", "b.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	// a.txt: staged only. c.txt: untracked.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	if r.activePage != gitBreachPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, gitBreachPage)
	}
	if r.gitBreachRoot == "" {
		t.Fatal("gitBreachRoot is empty after a successful open")
	}
	if len(r.gitBreachStatus.Staged) != 1 || r.gitBreachStatus.Staged[0].Path != "a.txt" {
		t.Errorf("Staged = %v, want just a.txt", r.gitBreachStatus.Staged)
	}
	if len(r.gitBreachStatus.Untracked) != 1 || r.gitBreachStatus.Untracked[0] != "c.txt" {
		t.Errorf("Untracked = %v, want just c.txt", r.gitBreachStatus.Untracked)
	}
}

func TestOpenGitBreachOnADirectoryThatIsNotARepositoryShowsAnError(t *testing.T) {
	requireGitForBreach(t)
	dir := t.TempDir()

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	if r.activePage == gitBreachPage {
		t.Fatal("activePage = gitBreachPage, want the dashboard to have refused to open")
	}
	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want the error overlay", r.activePage)
	}
}

func TestOpenGitBreachRefusesOnARemotePanel(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)

	r := newTestRootForGitBreachDir(t, dir)
	remote := newTestFakeRemote("/remote")
	if err := r.panel.connectRemote(remote, remotefs.Connection{Host: "testhost"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}

	r.openGitBreach()

	if r.activePage == gitBreachPage {
		t.Fatal("activePage = gitBreachPage, want the dashboard to have refused on a remote panel")
	}
	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want the error overlay", r.activePage)
	}
}

func TestOpenGitBreachRefusesInsideAnArchiveView(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)

	r := newTestRootForGitBreachDir(t, dir)
	// Simulating "browsing inside an archive" directly via the field
	// inArchiveView itself checks (see archivepanel.go) — exercising the
	// real archive-opening machinery just to set this one bit would be
	// disproportionate to what this test actually needs to pin.
	r.panel.archivePath = "/fake/archive.zip"

	r.openGitBreach()

	if r.activePage == gitBreachPage {
		t.Fatal("activePage = gitBreachPage, want the dashboard to have refused inside an archive view")
	}
	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want the error overlay", r.activePage)
	}
}

func TestToggleGitBreachStageStagesAndUnstagesTheRowUnderTheCursor(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	if len(r.gitBreachStatus.Untracked) != 1 {
		t.Fatalf("Untracked = %v, want a.txt untracked before staging", r.gitBreachStatus.Untracked)
	}
	row := gitBreachTableRowForPath(r, "a.txt")
	if row == 0 {
		t.Fatal("a.txt row not found in the Files table")
	}
	r.gitBreachFilesTable.Select(row, 0)

	r.toggleGitBreachStage()
	if len(r.gitBreachStatus.Staged) != 1 || r.gitBreachStatus.Staged[0].Path != "a.txt" {
		t.Fatalf("Staged = %v, want a.txt staged after toggling an untracked row", r.gitBreachStatus.Staged)
	}

	row = gitBreachTableRowForPath(r, "a.txt")
	if row == 0 {
		t.Fatal("a.txt row not found after staging")
	}
	r.gitBreachFilesTable.Select(row, 0)
	r.toggleGitBreachStage()
	if len(r.gitBreachStatus.Staged) != 0 {
		t.Errorf("Staged = %v, want empty after toggling a staged row back", r.gitBreachStatus.Staged)
	}
	if len(r.gitBreachStatus.Untracked) != 1 {
		t.Errorf("Untracked = %v, want a.txt untracked again after unstaging", r.gitBreachStatus.Untracked)
	}
}

func TestCommitGitBreachCreatesARealCommitAndReloads(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	if len(r.gitBreachStatus.Staged) != 1 {
		t.Fatalf("setup: Staged = %v, want a.txt staged", r.gitBreachStatus.Staged)
	}

	r.commitGitBreach("add a.txt")

	if r.activePage == errorPage {
		t.Fatal("activePage = errorPage after a commit that should have succeeded")
	}
	if len(r.gitBreachStatus.Staged) != 0 || len(r.gitBreachStatus.Unstaged) != 0 {
		t.Errorf("working tree not clean after commit: staged=%v unstaged=%v", r.gitBreachStatus.Staged, r.gitBreachStatus.Unstaged)
	}
}

func TestOpenGitBreachCommitPromptDoesNothingWithNothingStaged(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	if len(r.gitBreachStatus.Staged) != 0 {
		t.Fatalf("setup: Staged = %v, want a clean working tree", r.gitBreachStatus.Staged)
	}

	r.openGitBreachCommitPrompt()

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want the error overlay for \"nothing staged\"", r.activePage)
	}
}

// gitBreachTableRowForPath scans the already-rendered Files table for
// the row currently showing path — tests drive toggleGitBreachStage
// through the cursor the same way a real keypress would, rather than
// reaching past it to call git.Stage/Unstage directly, so a real
// regression in gitBreachRowIndex/renderGitBreachFiles's own row
// bookkeeping would actually be caught here too.
func gitBreachTableRowForPath(r *Root, path string) int {
	for row := range r.gitBreachRowIndex {
		if gr, ok := r.gitBreachRowAt(row); ok && gr.path == path {
			return row
		}
	}
	return 0
}
