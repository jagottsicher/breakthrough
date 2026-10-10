package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
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

// initGitBreachRepo sets a repo-local identity and disables gpgsign
// explicitly — gitBreachTestEnv's own vars only cover this test file's
// own setup calls (runGitBreach), never git.Commit itself, which
// builds its own os.Environ()-based Env and would otherwise inherit
// whatever (or no) global git identity/signing config happens to exist
// on the machine actually running the test (see internal/git's own
// identically-reasoned initRepo).
func initGitBreachRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitBreach(t, dir, "init", "-q", "-b", "main")
	runGitBreach(t, dir, "config", "user.name", "Test")
	runGitBreach(t, dir, "config", "user.email", "test@example.com")
	runGitBreach(t, dir, "config", "commit.gpgsign", "false")
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

// TestOpenGitBreachOnACleanRepoThenDirtyingItStillRefreshesTheDiff pins
// a real, reported bug: the Main box's own diff never refreshed past
// the very first open, no matter which row the cursor later moved to.
// The very first render — before any real git status has been fetched
// — takes the "Working tree clean" placeholder path, whose own
// showTablePlaceholder calls Select(1, 0) while gitBreachRowIndex is
// still empty, so the one diff fetch that call triggers finds no row
// and clears the view. Once real status lands and row 1 happens to
// already be a real file (as it is here), tview's own
// SetSelectionChangedFunc never fires again — Select() only invokes it
// when the selected row *number* changes, not when the data a stable
// number refers to does — so nothing else ever asked for a real diff
// either, had renderGitBreachFiles not been fixed to ask explicitly.
// Reproduced here the same way it actually happened: open on an
// already-clean repo (the "Working tree clean" path, row 0, no real
// files at all), then dirty it and reload — exactly the gap between
// opening a dashboard on a quiet repo and a file changing moments
// later that the real bug report came from.
func TestOpenGitBreachOnACleanRepoThenDirtyingItStillRefreshesTheDiff(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach() // clean repo — takes the "Working tree clean" path
	if len(r.gitBreachRows) != 0 {
		t.Fatalf("setup: gitBreachRows = %v, want none on a clean repo", r.gitBreachRows)
	}

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	r.reloadGitBreach()

	if len(r.gitBreachRows) != 1 || r.gitBreachRows[0].path != "a.txt" {
		t.Fatalf("setup: gitBreachRows = %v, want just a.txt staged", r.gitBreachRows)
	}
	if r.gitBreachDiffCancel == nil {
		t.Error("gitBreachDiffCancel is nil — reloadGitBreach never started fetching a diff for the now-real row")
	}
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

// TestGitBreachBlockFocusStealSwallowsLeftDown pins the user's own
// explicit bug report: an ordinary click anywhere on one of the
// passive boxes (Status, Branches/Commits/Stash, Main) used to steal
// real keyboard focus onto a box nothing could ever navigate inside,
// with no way back short of closing and reopening the whole dashboard
// — tview's own TextView.MouseHandler calls setFocus(itself)
// unconditionally on MouseLeftDown otherwise.
func TestGitBreachBlockFocusStealSwallowsLeftDown(t *testing.T) {
	action, event := gitBreachBlockFocusSteal(tview.MouseLeftDown, tcell.NewEventMouse(0, 0, tcell.ButtonNone, 0))
	if event != nil {
		t.Errorf("event = %v, want nil (swallowed, never reaching TextView's own default handler)", event)
	}
	if action != tview.MouseConsumed {
		t.Errorf("action = %v, want MouseConsumed", action)
	}
}

// TestGitBreachBlockFocusStealPassesThroughOtherActions pins the other
// half of the same fix: only MouseLeftDown (the one action that steals
// focus) is swallowed — a wheel scroll over the Main box's own long
// diff must still reach TextView's own default scroll handling.
func TestGitBreachBlockFocusStealPassesThroughOtherActions(t *testing.T) {
	orig := tcell.NewEventMouse(0, 0, tcell.ButtonNone, 0)
	action, event := gitBreachBlockFocusSteal(tview.MouseScrollDown, orig)
	if event != orig {
		t.Errorf("event = %v, want the original event passed through unchanged", event)
	}
	if action != tview.MouseScrollDown {
		t.Errorf("action = %v, want MouseScrollDown passed through unchanged", action)
	}
}

// TestGitBreachFocusFilesOnClickFocusesTheTable pins the user's own
// further explicit request: the Files header line itself must be
// clickable too, not just the table beneath it — before this, clicking
// the header did nothing at all (a header is its own separate TextView
// from the table it labels).
func TestGitBreachFocusFilesOnClickFocusesTheTable(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	r.app.SetFocus(r.gitBreachStatusView) // simulate focus having landed somewhere else
	if r.gitBreachFilesTable.HasFocus() {
		t.Fatal("setup: Files should not have focus yet")
	}

	action, event := r.gitBreachFocusFilesOnClick(tview.MouseLeftDown, tcell.NewEventMouse(0, 0, tcell.ButtonNone, 0))

	if !r.gitBreachFilesTable.HasFocus() {
		t.Error("clicking the Files header should focus the Files table")
	}
	if event != nil || action != tview.MouseConsumed {
		t.Errorf("action=%v event=%v, want the click consumed", action, event)
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
