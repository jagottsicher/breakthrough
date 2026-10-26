package ui

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/git"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
	"github.com/jagottsicher/breakthrough/internal/viewer"
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

// gitBreachBranchRow scans r.gitBreachBranches for name and returns its
// own table row — the mirror image of gitBreachTableRowForPath, going
// through gitBreachBranchAt's own detached-HEAD row-offset logic (see
// its own doc comment) rather than assuming row == index, so a
// regression there would be caught here too.
func gitBreachBranchRow(r *Root, name string) (int, bool) {
	for row := 0; row < r.gitBreachBranchesTable.GetRowCount(); row++ {
		if b, ok := r.gitBreachBranchAt(row); ok && b.Name == name {
			return row, true
		}
	}
	return 0, false
}

func TestRenderGitBreachBranchesListsLocalBranchesWithCurrentMarked(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	runGitBreach(t, dir, "branch", "feature-x")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	if len(r.gitBreachBranches) != 2 {
		t.Fatalf("gitBreachBranches = %+v, want 2 (main, feature-x)", r.gitBreachBranches)
	}
	row, ok := gitBreachBranchRow(r, "main")
	if !ok {
		t.Fatal("main not found in the Branches table")
	}
	if b, _ := r.gitBreachBranchAt(row); !b.Current {
		t.Error("main.Current = false, want true (it's the checked-out branch)")
	}
}

// TestOpenGitBreachSelectsTheFirstBranchNotTheSecond pins a real,
// reported bug: the cursor landed on the second branch instead of the
// first the moment the Branches box had any real content at all.
// newGitBreachScreen's own construction-time render (baking in cell
// colors before reloadGitBreach has ever run, with gitBreachBranches
// still empty) takes the "No local branches found" placeholder path,
// whose showTablePlaceholder calls Select(1, 0) — tview's Table.Clear()
// never resets that selectedRow afterward. Files' own section headers
// happen to absorb that same leftover 1 onto a legitimately-first real
// file row, masking the identical bug there (see
// TestOpenGitBreachOnACleanRepoThenDirtyingItStillRefreshesTheDiff's own
// doc comment); Branches has no header row to absorb it, so the cursor
// landed squarely on the second branch (alphabetically after the
// first) the very first time real branches existed. Needs two branches
// sorted so the first one isn't already current, since
// openGitBreachCheckout's own current-branch guard would otherwise mask
// a wrong starting row as a correct no-op.
func TestOpenGitBreachSelectsTheFirstBranchNotTheSecond(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	runGitBreach(t, dir, "branch", "zzz-not-current")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	wantRow, ok := gitBreachBranchRow(r, "main")
	if !ok {
		t.Fatal("main not found in the Branches table")
	}
	gotRow, _ := r.gitBreachBranchesTable.GetSelection()
	if gotRow != wantRow {
		b, _ := r.gitBreachBranchAt(gotRow)
		t.Errorf("selected row %d (%q), want row %d (main, the alphabetically-first branch)", gotRow, b.Name, wantRow)
	}
}

// TestOpenGitBreachCheckoutSwitchesImmediatelyOnACleanTree pins the
// "don't ask what there's nothing to lose" half of the confirmation
// logic — openGitBreachCheckout's own doc comment.
func TestOpenGitBreachCheckoutSwitchesImmediatelyOnACleanTree(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	runGitBreach(t, dir, "branch", "feature-x")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachBranchesTable)

	row, ok := gitBreachBranchRow(r, "feature-x")
	if !ok {
		t.Fatal("feature-x not found in the Branches table")
	}
	r.gitBreachBranchesTable.Select(row, 0)

	r.openGitBreachCheckout()

	if r.activePage == confirmPage {
		t.Fatal("activePage = confirmPage, want an immediate checkout on a clean tree")
	}
	if b, _ := r.gitBreachBranchAt(row); !b.Current {
		t.Errorf("feature-x.Current = false after checkout, want true")
	}
}

// TestOpenGitBreachCheckoutConfirmsOnADirtyTree pins the other half:
// staged/unstaged/conflicted changes must not silently ride along onto
// a different branch without the user being asked first.
func TestOpenGitBreachCheckoutConfirmsOnADirtyTree(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	runGitBreach(t, dir, "branch", "feature-x")
	// Dirty the tree: a staged, uncommitted change.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachBranchesTable)

	row, ok := gitBreachBranchRow(r, "feature-x")
	if !ok {
		t.Fatal("feature-x not found in the Branches table")
	}
	r.gitBreachBranchesTable.Select(row, 0)

	r.openGitBreachCheckout()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirm dialog on a dirty tree", r.activePage)
	}
	if b, _ := r.gitBreachBranchAt(row); b.Current {
		t.Error("feature-x.Current = true before confirming, want the checkout to not have run yet")
	}

	r.pendingConfirm()
	if b, _ := r.gitBreachBranchAt(row); !b.Current {
		t.Error("feature-x.Current = false after confirming, want the checkout to have run")
	}
}

// TestOpenGitBreachCheckoutDoesNothingOnTheCurrentBranch pins a plain
// no-op guard: checking out the branch you're already on is never a
// real action (and would otherwise prompt for confirmation on a dirty
// tree for literally nothing).
func TestOpenGitBreachCheckoutDoesNothingOnTheCurrentBranch(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachBranchesTable)

	row, ok := gitBreachBranchRow(r, "main")
	if !ok {
		t.Fatal("main not found in the Branches table")
	}
	r.gitBreachBranchesTable.Select(row, 0)

	r.openGitBreachCheckout()

	if r.activePage == confirmPage {
		t.Error("activePage = confirmPage, want a no-op for the already-current branch")
	}
}

// TestOpenGitBreachDeleteBranchConfirmsThenDeletes pins the "always
// confirm" half: unlike checkout, deleting a branch is itself the
// irreversible action, not something that's only risky in some states.
func TestOpenGitBreachDeleteBranchConfirmsThenDeletes(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	runGitBreach(t, dir, "branch", "feature-x")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachBranchesTable)

	row, ok := gitBreachBranchRow(r, "feature-x")
	if !ok {
		t.Fatal("feature-x not found in the Branches table")
	}
	r.gitBreachBranchesTable.Select(row, 0)

	r.openGitBreachDeleteBranch()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirm dialog before deleting a branch", r.activePage)
	}
	if _, ok := gitBreachBranchRow(r, "feature-x"); !ok {
		t.Error("feature-x already gone before confirming, want the delete to not have run yet")
	}

	r.pendingConfirm()

	if _, ok := gitBreachBranchRow(r, "feature-x"); ok {
		t.Error("feature-x still present after confirming, want the delete to have run")
	}
}

// TestOpenGitBreachDeleteBranchDoesNothingOnTheCurrentBranch mirrors
// TestOpenGitBreachCheckoutDoesNothingOnTheCurrentBranch exactly:
// deleting the branch you're currently on is never a real action, and
// git itself would otherwise refuse it with a differently-worded error
// ("used by worktree") than this no-op guard's own silence.
func TestOpenGitBreachDeleteBranchDoesNothingOnTheCurrentBranch(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachBranchesTable)

	row, ok := gitBreachBranchRow(r, "main")
	if !ok {
		t.Fatal("main not found in the Branches table")
	}
	r.gitBreachBranchesTable.Select(row, 0)

	r.openGitBreachDeleteBranch()

	if r.activePage == confirmPage {
		t.Error("activePage = confirmPage, want a no-op for the already-current branch")
	}
}

// TestOpenGitBreachDeleteBranchSurfacesGitsOwnRefusal pins that an
// unmerged branch's own refusal (see git.DeleteBranch's own doc
// comment) reaches the user as a real error rather than being silently
// swallowed.
func TestOpenGitBreachDeleteBranchSurfacesGitsOwnRefusal(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	runGitBreach(t, dir, "checkout", "-q", "-b", "feature-x")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "unmerged change")
	runGitBreach(t, dir, "checkout", "-q", "main")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachBranchesTable)

	row, ok := gitBreachBranchRow(r, "feature-x")
	if !ok {
		t.Fatal("feature-x not found in the Branches table")
	}
	r.gitBreachBranchesTable.Select(row, 0)

	r.openGitBreachDeleteBranch()
	r.pendingConfirm()

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want the error overlay for git's own unmerged-branch refusal", r.activePage)
	}
	if _, ok := gitBreachBranchRow(r, "feature-x"); !ok {
		t.Error("feature-x is gone despite git's own refusal, want it to still exist")
	}
}

// TestToggleGitBreachFocusCyclesBetweenFilesAndBranches pins "Tab" —
// gitBreachFocusables' own doc comment on why this is a data-driven
// list rather than a hardcoded two-way toggle.
func TestToggleGitBreachFocusCyclesThroughFilesBranchesCommitsAndStash(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	if !r.gitBreachFilesTable.HasFocus() {
		t.Fatal("setup: Files should have focus right after opening")
	}

	r.toggleGitBreachFocus()
	if !r.gitBreachBranchesTable.HasFocus() {
		t.Error("after one Tab: Branches should have focus")
	}

	r.toggleGitBreachFocus()
	if !r.gitBreachCommitsTable.HasFocus() {
		t.Error("after a second Tab: Commits should have focus")
	}

	r.toggleGitBreachFocus()
	if !r.gitBreachStashTable.HasFocus() {
		t.Error("after a third Tab: Stash should have focus")
	}

	r.toggleGitBreachFocus()
	if !r.gitBreachFilesTable.HasFocus() {
		t.Error("after a fourth Tab: focus should be back on Files")
	}
}

// TestReverseToggleGitBreachFocusCyclesBackwards pins Shift-Tab as the
// exact mirror image of Tab: starting from Files (wraparound in the
// other direction) and stepping backwards through Stash, Commits,
// Branches, back to Files.
func TestReverseToggleGitBreachFocusCyclesBackwards(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	if !r.gitBreachFilesTable.HasFocus() {
		t.Fatal("setup: Files should have focus right after opening")
	}

	r.reverseToggleGitBreachFocus()
	if !r.gitBreachStashTable.HasFocus() {
		t.Error("after one Shift-Tab from Files: Stash should have focus")
	}

	r.reverseToggleGitBreachFocus()
	if !r.gitBreachCommitsTable.HasFocus() {
		t.Error("after a second Shift-Tab: Commits should have focus")
	}

	r.reverseToggleGitBreachFocus()
	if !r.gitBreachBranchesTable.HasFocus() {
		t.Error("after a third Shift-Tab: Branches should have focus")
	}

	r.reverseToggleGitBreachFocus()
	if !r.gitBreachFilesTable.HasFocus() {
		t.Error("after a fourth Shift-Tab: focus should be back on Files")
	}
}

// TestCaptureGitBreachKeyBacktabReversesFocusFromEveryBox verifies all
// four per-table InputCapture functions wire KeyBacktab to
// reverseToggleGitBreachFocus, not just toggleGitBreachFocus's own
// KeyTab path.
func TestCaptureGitBreachKeyBacktabReversesFocusFromEveryBox(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	backtab := tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone)

	r.captureGitBreachFilesTableKey(backtab)
	if !r.gitBreachStashTable.HasFocus() {
		t.Error("Shift-Tab from Files should move focus to Stash")
	}

	r.captureGitBreachStashTableKey(backtab)
	if !r.gitBreachCommitsTable.HasFocus() {
		t.Error("Shift-Tab from Stash should move focus to Commits")
	}

	r.captureGitBreachCommitsTableKey(backtab)
	if !r.gitBreachBranchesTable.HasFocus() {
		t.Error("Shift-Tab from Commits should move focus to Branches")
	}

	r.captureGitBreachBranchesTableKey(backtab)
	if !r.gitBreachFilesTable.HasFocus() {
		t.Error("Shift-Tab from Branches should move focus to Files")
	}
}

func TestRenderGitBreachCommitsListsCommitsNewestFirst(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "first")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "second")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	if len(r.gitBreachCommits) != 2 {
		t.Fatalf("gitBreachCommits = %+v, want 2", r.gitBreachCommits)
	}
	if r.gitBreachCommits[0].Subject != "second" || r.gitBreachCommits[1].Subject != "first" {
		t.Errorf("gitBreachCommits order = %q, %q, want [second, first] (newest first)",
			r.gitBreachCommits[0].Subject, r.gitBreachCommits[1].Subject)
	}
}

// TestGitBreachAuthorColorIsStablePerEmail pins the exact identity
// gitBreachAuthorColor hashes on: the same email must always land on
// the same one of the theme's nine label colors, both across repeated
// calls and regardless of a changed display name — the whole point of
// hashing the email rather than the name (see its own doc comment).
func TestGitBreachAuthorColorIsStablePerEmail(t *testing.T) {
	first := gitBreachAuthorColor("jens@example.com")
	second := gitBreachAuthorColor("jens@example.com")
	if first != second {
		t.Errorf("gitBreachAuthorColor(same email) = %v, %v, want identical", first, second)
	}
}

// TestGitBreachAuthorColorUsesOneOfTheThemesNineLabelColors pins that
// the result always comes from the theme's own nine label colors
// (user-configurable, see labelFallbackColors in internal/config), not
// some separate, hardcoded palette of its own.
func TestGitBreachAuthorColorUsesOneOfTheTwelveAuthorColors(t *testing.T) {
	labels := map[tcell.Color]bool{}
	for _, c := range gitBreachAuthorColors {
		labels[c] = true
	}
	for _, email := range []string{"a@example.com", "b@example.com", "c@example.com"} {
		if got := gitBreachAuthorColor(email); !labels[got] {
			t.Errorf("gitBreachAuthorColor(%q) = %v, not one of the twelve author colors", email, got)
		}
	}
}

// TestRenderGitBreachCommitsColorsByAuthor pins the actual render,
// not just the hashing function in isolation: a commit's row in the
// table really does carry gitBreachAuthorColor's own result, and the
// author's name is visible in the row's own text (color alone isn't
// enough to identify who, for anyone who can't distinguish the nine
// label hues at a glance).
func TestRenderGitBreachCommitsColorsByAuthor(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	if len(r.gitBreachCommits) != 1 {
		t.Fatalf("gitBreachCommits = %+v, want 1", r.gitBreachCommits)
	}
	wantColor := gitBreachAuthorColor(r.gitBreachCommits[0].AuthorEmail)
	cell := r.gitBreachCommitsTable.GetCell(0, 0)
	// tview.NewTableCell's own Style already carries a non-default
	// Foreground/Background pair, so SetTextColor (see its own tview
	// source) stores the color in Style rather than the legacy Color
	// field — Decompose is how a cell's actual rendered foreground is
	// read back, not the Color field directly.
	gotColor, _, _ := cell.Style.Decompose()
	if gotColor != wantColor {
		t.Errorf("row color = %v, want %v (gitBreachAuthorColor for %q)", gotColor, wantColor, r.gitBreachCommits[0].AuthorEmail)
	}
	if !strings.Contains(cell.Text, r.gitBreachCommits[0].AuthorName) {
		t.Errorf("row text = %q, want it to contain the author's name %q", cell.Text, r.gitBreachCommits[0].AuthorName)
	}
}

// TestGitBreachGraphGlyphCoversEveryLaneGlyph pins the rune each
// git.LaneGlyph maps to — mostly to catch a future new LaneGlyph
// constant silently falling through to the default space instead of
// getting its own case here.
func TestGitBreachGraphGlyphCoversEveryLaneGlyph(t *testing.T) {
	cases := map[git.LaneGlyph]rune{
		git.LaneGlyphNone:       ' ',
		git.LaneGlyphPass:       '│',
		git.LaneGlyphNode:       '●',
		git.LaneGlyphHorizontal: '─',
		git.LaneGlyphForkRight:  '╮',
		git.LaneGlyphForkLeft:   '╭',
		git.LaneGlyphJoinRight:  '╯',
		git.LaneGlyphJoinLeft:   '╰',
	}
	for glyph, want := range cases {
		if got := gitBreachGraphGlyph(glyph); got != want {
			t.Errorf("gitBreachGraphGlyph(%v) = %q, want %q", glyph, got, want)
		}
	}
}

// TestGitBreachGraphPrefixColorsEachLaneAndPadsToWidth pins the two
// things gitBreachGraphPrefix owns beyond glyph lookup: each non-empty
// cell gets wrapped in its own lane's color tag (so a merge row's two
// differently colored lanes render correctly in one Table cell — see
// the tcell.SimulationScreen spike this whole feature started from),
// and the result is right-padded with plain, untagged spaces up to
// width so every row's hash/author/subject column lines up regardless
// of how many lanes that particular row needed.
func TestGitBreachGraphPrefixColorsEachLaneAndPadsToWidth(t *testing.T) {
	row := git.LaneRow{
		Cells: []git.LaneCell{
			{Glyph: git.LaneGlyphNode, Lane: 0},
			{Glyph: git.LaneGlyphForkRight, Lane: 1},
		},
	}
	got := gitBreachGraphPrefix(row, 4)
	wantColor0 := colorTag(gitBreachLaneColor(0))
	wantColor1 := colorTag(gitBreachLaneColor(1))
	want := fmt.Sprintf("[%s]●[-][%s]╮[-]  ", wantColor0, wantColor1)
	if got != want {
		t.Errorf("gitBreachGraphPrefix = %q, want %q", got, want)
	}
}

// TestGitBreachGraphPrefixLeavesEmptyColumnsUncolored pins that a
// LaneGlyphNone column renders as a bare space, never wrapped in a
// color tag of its own (there's no lane to color).
func TestGitBreachGraphPrefixLeavesEmptyColumnsUncolored(t *testing.T) {
	row := git.LaneRow{Cells: []git.LaneCell{{Glyph: git.LaneGlyphNone, Lane: -1}}}
	got := gitBreachGraphPrefix(row, 1)
	if got != " " {
		t.Errorf("gitBreachGraphPrefix for an empty column = %q, want a bare space", got)
	}
}

// TestRenderGitBreachCommitsDrawsAMergeWithTwoLanes exercises the real
// render path end to end against an actual merge commit — a feature
// branch with one commit, merged back — confirming the merge row's own
// cell carries two differently colored glyphs (its own node plus the
// fork into the feature lane) rather than just the plain, single-lane
// text the pre-graph version of this table used to show.
func TestRenderGitBreachCommitsDrawsAMergeWithTwoLanes(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "base")
	runGitBreach(t, dir, "checkout", "-q", "-b", "feature-x")
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "feature work")
	runGitBreach(t, dir, "checkout", "-q", "main")
	runGitBreach(t, dir, "merge", "-q", "--no-ff", "feature-x", "-m", "merge feature-x")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	if len(r.gitBreachCommits) != 3 {
		t.Fatalf("gitBreachCommits = %+v, want 3 (merge, feature work, base)", r.gitBreachCommits)
	}
	cell := r.gitBreachCommitsTable.GetCell(0, 0)
	if !strings.Contains(cell.Text, "●") {
		t.Errorf("merge row text = %q, want a node glyph", cell.Text)
	}
	if !strings.Contains(cell.Text, "╮") && !strings.Contains(cell.Text, "╭") {
		t.Errorf("merge row text = %q, want a fork glyph for the second parent", cell.Text)
	}
}

// TestRenderGitBreachCommitsEscapesBracketsInSubjectsAndNames pins the
// bug this feature would otherwise reintroduce: the graph prefix is
// built entirely out of this package's own "[color]...[-]" tags, so an
// unescaped "[" anywhere later in the same cell's text (a subject like
// "fix: strip [brackets]" is entirely plausible) would be parsed as
// more of the same tag syntax and corrupt the rest of the row instead
// of just failing to color one word.
func TestRenderGitBreachCommitsEscapesBracketsInSubjectsAndNames(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "fix: strip [brackets] from input")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	cell := r.gitBreachCommitsTable.GetCell(0, 0)
	if !strings.Contains(cell.Text, "[brackets[] from input") {
		t.Errorf("row text = %q, want tview.Escape's own escaped form of \"[brackets] from input\"", cell.Text)
	}
}

// TestOpenGitBreachSelectsTheFirstCommitNotTheSecond is the Commits
// box's own version of TestOpenGitBreachSelectsTheFirstBranchNotTheSecond
// — the identical construction-time-placeholder bug (see
// gitBreachCommitsReady's own doc comment on Root), a third instance of
// the same fix.
func TestOpenGitBreachSelectsTheFirstCommitNotTheSecond(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "first")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "second")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	gotRow, _ := r.gitBreachCommitsTable.GetSelection()
	if gotRow != 0 {
		c, _ := r.gitBreachCommitAt(gotRow)
		t.Errorf("selected row %d (%q), want row 0 (the newest commit)", gotRow, c.Subject)
	}
}

// TestGitBreachMainOwnerFollowsKeyboardFocus pins the real bug the
// advisor flagged before this box existed: Files' own unconditional
// refresh-Main-on-reload (renderGitBreachFiles' own tail) would
// otherwise clobber a commit's own diff with a file diff on every
// reload while Commits has focus, simply because Files re-asserts its
// own cursor position on every render regardless of which box the user
// is actually looking at. gitBreachMainOwner exists specifically so
// that doesn't happen — this test exercises the owner switch itself
// (Tab to Commits), not the diff fetch's own async result, the same
// restraint TestOpenGitBreachOnACleanRepoThenDirtyingItStillRefreshesTheDiff's
// own doc comment already explains for why "a fetch was started" (not
// "the fetch landed with this exact text") is what's actually testable
// deterministically here.
func TestGitBreachMainOwnerFollowsKeyboardFocus(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "first")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	if r.gitBreachMainOwner != gitBreachMainOwnerFiles {
		t.Fatalf("setup: gitBreachMainOwner = %v, want gitBreachMainOwnerFiles right after opening", r.gitBreachMainOwner)
	}

	r.toggleGitBreachFocus() // -> Branches
	r.toggleGitBreachFocus() // -> Commits
	if !r.gitBreachCommitsTable.HasFocus() {
		t.Fatal("setup: Commits should have focus after two Tabs")
	}
	if r.gitBreachMainOwner != gitBreachMainOwnerCommits {
		t.Error("gitBreachMainOwner did not switch to Commits when it gained keyboard focus")
	}

	r.toggleGitBreachFocus() // -> Stash
	if r.gitBreachMainOwner != gitBreachMainOwnerStash {
		t.Error("gitBreachMainOwner did not switch to Stash when it gained keyboard focus")
	}

	r.toggleGitBreachFocus() // -> Files
	if r.gitBreachMainOwner != gitBreachMainOwnerFiles {
		t.Error("gitBreachMainOwner did not switch back to Files when it regained keyboard focus")
	}
}

// TestStartGitBreachCommitsDiffIfOwnerDoesNothingWhenFilesOwnsMain pins
// the guard itself, directly: without it, renderGitBreachCommits'/
// renderGitBreachFiles' own unconditional tail calls (both needed for
// the real stale-diff bug those tails originally fixed) would clobber
// whichever box's diff Main is actually showing, on every reload,
// regardless of which box has real keyboard focus.
func TestStartGitBreachCommitsDiffIfOwnerDoesNothingWhenFilesOwnsMain(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "first")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.cancelGitBreachDiff() // openGitBreach's own Files-owned fetch already started one

	if r.gitBreachMainOwner != gitBreachMainOwnerFiles {
		t.Fatal("setup: gitBreachMainOwner should still be Files")
	}
	r.startGitBreachCommitsDiffIfOwner()
	if r.gitBreachDiffCancel != nil {
		t.Error("startGitBreachCommitsDiffIfOwner started a fetch while Files owns Main, want a no-op")
	}
}

func TestRenderGitBreachStashListsStashedChangesNewestFirst(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "stash", "push", "-q", "-m", "first stash")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "stash", "push", "-q", "-m", "second stash")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	if len(r.gitBreachStash) != 2 {
		t.Fatalf("gitBreachStash = %+v, want 2", r.gitBreachStash)
	}
	if !strings.Contains(r.gitBreachStash[0].Message, "second stash") {
		t.Errorf("gitBreachStash[0] = %+v, want the newest stash first", r.gitBreachStash[0])
	}
}

// TestOpenGitBreachSelectsTheFirstStashNotTheSecond is the Stash box's
// own version of TestOpenGitBreachSelectsTheFirstCommitNotTheSecond —
// the identical construction-time-placeholder bug (see
// gitBreachStashReady's own doc comment on Root), a fourth instance of
// the same fix.
func TestOpenGitBreachSelectsTheFirstStashNotTheSecond(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "stash", "push", "-q", "-m", "first stash")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "stash", "push", "-q", "-m", "second stash")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	gotRow, _ := r.gitBreachStashTable.GetSelection()
	if gotRow != 0 {
		s, _ := r.gitBreachStashAt(gotRow)
		t.Errorf("selected row %d (%q), want row 0 (the newest stash)", gotRow, s.Message)
	}
}

func TestGitBreachMainOwnerStashDoesNothingWhenFilesOwnsMain(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "first")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.cancelGitBreachDiff() // openGitBreach's own Files-owned fetch already started one

	if r.gitBreachMainOwner != gitBreachMainOwnerFiles {
		t.Fatal("setup: gitBreachMainOwner should still be Files")
	}
	r.startGitBreachStashDiffIfOwner()
	if r.gitBreachDiffCancel != nil {
		t.Error("startGitBreachStashDiffIfOwner started a fetch while Files owns Main, want a no-op")
	}
}

// TestGitBreachCommitPromptOpensFromAnyBox pins the user's own explicit
// point: committing whatever is already staged has nothing to do with
// which row is selected anywhere, so "c" must work no matter which box
// currently has keyboard focus — not just Files, the only one whose own
// key capture originally wired it up.
func TestGitBreachCommitPromptOpensFromAnyBox(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")

	cKey := tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModNone)

	for _, tc := range []struct {
		name    string
		capture func(r *Root) func(*tcell.EventKey) *tcell.EventKey
	}{
		{"Branches", func(r *Root) func(*tcell.EventKey) *tcell.EventKey { return r.captureGitBreachBranchesTableKey }},
		{"Commits", func(r *Root) func(*tcell.EventKey) *tcell.EventKey { return r.captureGitBreachCommitsTableKey }},
		{"Stash", func(r *Root) func(*tcell.EventKey) *tcell.EventKey { return r.captureGitBreachStashTableKey }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRootForGitBreachDir(t, dir)
			r.openGitBreach()

			if got := tc.capture(r)(cKey); got != nil {
				t.Errorf("capture(%q) = %v, want nil (consumed)", 'c', got)
			}
			if r.activePage != promptPage {
				t.Errorf("activePage = %q, want %q (the commit prompt)", r.activePage, promptPage)
			}
		})
	}
}

// gitBreachHintHasKey reports whether entries contains one whose own
// single key (every gitBreachHintEntries entry has exactly one — see
// hintKey) matches key — checking the structured entries themselves
// rather than the rendered hint text avoids false positives from a key
// label's own letters coincidentally appearing elsewhere in the text
// (e.g. "switch box" contains a "c").
func gitBreachHintHasKey(entries []listHintEntry, key string) bool {
	for _, e := range entries {
		for _, k := range e.keys {
			if k.key == key {
				return true
			}
		}
	}
	return false
}

// TestGitBreachHintShowsSpaceOnlyWhileFilesHasFocus and its Branches
// counterpart below pin the user's own explicit point the other way:
// Space only ever acts on a Files row, and Enter only ever checks out a
// Branches row, so the hint bar shouldn't advertise either one while a
// different box has focus — unlike "c", which now always shows (see
// TestGitBreachCommitPromptOpensFromAnyBox).
func TestGitBreachHintShowsSpaceOnlyWhileFilesHasFocus(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	if !gitBreachHintHasKey(r.gitBreachHintEntries(), "Space") {
		t.Error("hint entries don't mention Space while Files has focus")
	}

	r.toggleGitBreachFocus() // -> Branches
	if gitBreachHintHasKey(r.gitBreachHintEntries(), "Space") {
		t.Error("hint entries still mention Space after focus moved to Branches")
	}
	if !gitBreachHintHasKey(r.gitBreachHintEntries(), "c") {
		t.Error("hint entries should still mention \"c\" (commit) regardless of focus")
	}
}

// TestGitBreachHintShowsEnterOnlyWhileBranchesOrCommitsHasFocus pins
// both of Enter's two real meanings in this dashboard — checkout in
// Branches, jump into Main's own diff in Commits (see
// activateGitBreachMain) — and that Stash, where Enter does nothing,
// advertises neither. Checks the full combined key label, not just
// bare "Enter": Commits' own entry is the compound "Enter/Space/→",
// which is a different key string entirely from Branches' own bare
// "Enter" as far as gitBreachHintHasKey's exact-match comparison is
// concerned.
func TestGitBreachHintShowsEnterOnlyWhileBranchesOrCommitsHasFocus(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	if gitBreachHintHasKey(r.gitBreachHintEntries(), "Enter") {
		t.Error("hint entries mention Enter before Branches has focus")
	}

	r.toggleGitBreachFocus() // -> Branches
	if !gitBreachHintHasKey(r.gitBreachHintEntries(), "Enter") {
		t.Error("hint entries don't mention Enter while Branches has focus")
	}

	r.toggleGitBreachFocus() // -> Commits
	if gitBreachHintHasKey(r.gitBreachHintEntries(), "Enter") {
		t.Error(`hint entries should use the compound "Enter/Space/→" key in Commits, not bare "Enter"`)
	}
	if !gitBreachHintHasKey(r.gitBreachHintEntries(), "Enter/Space/→") {
		t.Error("hint entries don't mention Enter/Space/→ while Commits has focus")
	}

	r.toggleGitBreachFocus() // -> Stash
	if gitBreachHintHasKey(r.gitBreachHintEntries(), "Enter") || gitBreachHintHasKey(r.gitBreachHintEntries(), "Enter/Space/→") {
		t.Error("hint entries still mention Enter after focus moved to Stash")
	}
}

// TestGitBreachTableKeysPassThroughPageKeys pins the inverted contract
// (see CLAUDE.md's own "replace, don't just delete, a changed contract"
// rule): PgUp/PgDn used to be swallowed by every table and forwarded
// straight to Main regardless of which box had focus (scrollGitBreachDiff,
// now removed) — the workaround for Main never being a real focus
// target at all. Now that Main can hold real keyboard focus on its own
// (see activateGitBreachMain), a list box's own PgUp/PgDn goes back to
// meaning what tview.Table already does with it natively: page through
// the table's own rows. Each capture must therefore let the event
// through unchanged (non-nil, same key) instead of consuming it.
func TestGitBreachTableKeysPassThroughPageKeys(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")

	for _, tc := range []struct {
		name    string
		capture func(r *Root) func(*tcell.EventKey) *tcell.EventKey
	}{
		{"Files", func(r *Root) func(*tcell.EventKey) *tcell.EventKey { return r.captureGitBreachFilesTableKey }},
		{"Branches", func(r *Root) func(*tcell.EventKey) *tcell.EventKey { return r.captureGitBreachBranchesTableKey }},
		{"Commits", func(r *Root) func(*tcell.EventKey) *tcell.EventKey { return r.captureGitBreachCommitsTableKey }},
		{"Stash", func(r *Root) func(*tcell.EventKey) *tcell.EventKey { return r.captureGitBreachStashTableKey }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRootForGitBreachDir(t, dir)
			r.openGitBreach()

			for _, key := range []tcell.Key{tcell.KeyPgUp, tcell.KeyPgDn} {
				event := tcell.NewEventKey(key, 0, tcell.ModNone)
				if got := tc.capture(r)(event); got != event {
					t.Errorf("capture(%v) = %v, want the same event passed through unchanged", key, got)
				}
			}
		})
	}
}

// gitBreachRepoWithCommits commits n trivial, individually distinct
// changes to a.txt, oldest first — a repo with a real, known commit
// count to test gitBreachCommitsLimit/maybeLoadMoreGitBreachCommits
// against without needing anywhere near git.CommitLogLimit's own real
// 200.
func gitBreachRepoWithCommits(t *testing.T, n int) string {
	t.Helper()
	dir := initGitBreachRepo(t)
	for i := 0; i < n; i++ {
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(fmt.Sprintf("line %d", i)), 0o644); err != nil {
			t.Fatal(err)
		}
		runGitBreach(t, dir, "add", "a.txt")
		runGitBreach(t, dir, "commit", "-q", "-m", fmt.Sprintf("commit %d", i))
	}
	return dir
}

// TestMaybeLoadMoreGitBreachCommitsExpandsLimitAtTheLastRow pins the
// user's own explicit request: reaching the bottom of what's currently
// loaded fetches another git.CommitLogLimit worth rather than the list
// just stopping. gitBreachCommitsLimit is set directly to an
// artificially small value and reloadGitBreach re-run (reloadGitBreach
// itself never resets the limit — only openGitBreach does, see its own
// doc comment) rather than creating git.CommitLogLimit-plus real
// commits, which would make this test disproportionately slow for what
// it needs to pin.
func TestMaybeLoadMoreGitBreachCommitsExpandsLimitAtTheLastRow(t *testing.T) {
	requireGitForBreach(t)
	dir := gitBreachRepoWithCommits(t, 5)

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.gitBreachCommitsLimit = 3
	r.reloadGitBreach()
	if len(r.gitBreachCommits) != 3 {
		t.Fatalf("setup: gitBreachCommits = %+v, want exactly 3 (the artificially small limit)", r.gitBreachCommits)
	}

	r.maybeLoadMoreGitBreachCommits(2) // the last loaded row (index 2 of 3)

	if want := 3 + git.CommitLogLimit; r.gitBreachCommitsLimit != want {
		t.Errorf("gitBreachCommitsLimit = %d, want %d after reaching the last loaded row", r.gitBreachCommitsLimit, want)
	}
	if len(r.gitBreachCommits) != 5 {
		t.Errorf("gitBreachCommits = %+v, want all 5 commits after loading more", r.gitBreachCommits)
	}
}

// TestMaybeLoadMoreGitBreachCommitsDoesNothingWhenNotAtTheLastRow pins
// the first guard: moving the cursor around anywhere above the actual
// last loaded row must never refetch.
func TestMaybeLoadMoreGitBreachCommitsDoesNothingWhenNotAtTheLastRow(t *testing.T) {
	requireGitForBreach(t)
	dir := gitBreachRepoWithCommits(t, 5)

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.gitBreachCommitsLimit = 3
	r.reloadGitBreach()

	r.maybeLoadMoreGitBreachCommits(0) // not the last row

	if r.gitBreachCommitsLimit != 3 {
		t.Errorf("gitBreachCommitsLimit = %d, want unchanged 3", r.gitBreachCommitsLimit)
	}
	if len(r.gitBreachCommits) != 3 {
		t.Errorf("gitBreachCommits = %+v, want unchanged (still 3)", r.gitBreachCommits)
	}
}

// TestMaybeLoadMoreGitBreachCommitsDoesNothingPastTheTrueEnd pins the
// second guard: once Log already returned fewer commits than the
// current limit (every real commit the repository has), reaching that
// last row must not refetch — there's genuinely nothing more to load.
func TestMaybeLoadMoreGitBreachCommitsDoesNothingPastTheTrueEnd(t *testing.T) {
	requireGitForBreach(t)
	dir := gitBreachRepoWithCommits(t, 2)

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach() // limit defaults to git.CommitLogLimit (200); only 2 commits exist

	r.maybeLoadMoreGitBreachCommits(1) // the last (and only) real row

	if r.gitBreachCommitsLimit != git.CommitLogLimit {
		t.Errorf("gitBreachCommitsLimit = %d, want unchanged %d", r.gitBreachCommitsLimit, git.CommitLogLimit)
	}
}

// TestGitBreachCommitsLimitPersistsAcrossReloadButResetsOnFreshOpen
// pins both halves of gitBreachCommitsLimit's own documented contract
// on Root: "r" (reloadGitBreach) must never lose an already-expanded
// limit, but a fresh "jg" (openGitBreach) must always start over at
// git.CommitLogLimit.
func TestGitBreachCommitsLimitPersistsAcrossReloadButResetsOnFreshOpen(t *testing.T) {
	requireGitForBreach(t)
	dir := gitBreachRepoWithCommits(t, 2)

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.gitBreachCommitsLimit = 3 + git.CommitLogLimit // simulate having loaded more already
	r.reloadGitBreach()
	if want := 3 + git.CommitLogLimit; r.gitBreachCommitsLimit != want {
		t.Errorf("reloadGitBreach changed gitBreachCommitsLimit to %d, want it to stay %d", r.gitBreachCommitsLimit, want)
	}

	r.openGitBreach()
	if r.gitBreachCommitsLimit != git.CommitLogLimit {
		t.Errorf("a fresh openGitBreach left gitBreachCommitsLimit at %d, want it reset to %d", r.gitBreachCommitsLimit, git.CommitLogLimit)
	}
}

// TestRenderGitBreachCommitsHeaderShowsHintOnlyWhileTruncated pins the
// header hint's own visibility rule: shown as "loaded/total" while
// there's genuinely more to scroll to, hidden once every commit is
// already loaded — showing "5/5" would just be noise once there's
// nothing left to page to.
func TestRenderGitBreachCommitsHeaderShowsHintOnlyWhileTruncated(t *testing.T) {
	requireGitForBreach(t)
	dir := gitBreachRepoWithCommits(t, 5)

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.gitBreachCommitsLimit = 3
	r.reloadGitBreach()

	r.renderGitBreachCommitsHeader(40)
	text := r.gitBreachCommitsHeader.GetText(true)
	if !strings.Contains(text, "3/5") {
		t.Errorf("header text = %q, want it to contain the 3/5 hint while truncated", text)
	}

	r.gitBreachCommitsLimit = 5
	r.reloadGitBreach()
	r.renderGitBreachCommitsHeader(40)
	text = r.gitBreachCommitsHeader.GetText(true)
	if strings.Contains(text, "/") {
		t.Errorf("header text = %q, want no hint once every commit is loaded", text)
	}
}

// TestOpenGitBreachStashApplyRestoresOnACleanTree pins the "don't ask
// what there's nothing to lose" half: applying a stash onto an already
// clean working tree has nothing to conflict with, so it runs
// immediately with no confirm — the same restraint
// openGitBreachCheckout's own doc comment already establishes for
// checkout.
func TestOpenGitBreachStashApplyRestoresOnACleanTree(t *testing.T) {
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
	runGitBreach(t, dir, "stash", "push", "-q", "-m", "wip")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachStashTable)
	r.gitBreachStashTable.Select(0, 0)

	r.openGitBreachStashApply()

	if r.activePage == confirmPage {
		t.Fatal("activePage = confirmPage, want an immediate apply on a clean tree")
	}
	content, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "a changed" {
		t.Errorf("a.txt = %q, want the stashed change restored", content)
	}
	if len(r.gitBreachStash) != 1 {
		t.Errorf("gitBreachStash = %+v, want the stash to still exist after apply", r.gitBreachStash)
	}
}

// TestOpenGitBreachStashApplyConfirmsOnADirtyTree pins the other half:
// staged/unstaged/conflicted changes must not silently receive a
// possibly-conflicting stash apply without the user being asked first.
func TestOpenGitBreachStashApplyConfirmsOnADirtyTree(t *testing.T) {
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
	runGitBreach(t, dir, "stash", "push", "-q", "-m", "wip")
	// Dirty the tree again: a different staged, uncommitted change.
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "b.txt")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachStashTable)
	r.gitBreachStashTable.Select(0, 0)

	r.openGitBreachStashApply()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirm dialog on a dirty tree", r.activePage)
	}
	content, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(content) == "a changed" {
		t.Error("a.txt already has the stashed change before confirming, want the apply to not have run yet")
	}

	r.pendingConfirm()

	content, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "a changed" {
		t.Errorf("a.txt = %q after confirming, want the stashed change restored", content)
	}
}

// TestOpenGitBreachStashDropConfirmsThenDrops pins the "always confirm"
// half: unlike apply, dropping a stash is itself the irreversible
// action.
func TestOpenGitBreachStashDropConfirmsThenDrops(t *testing.T) {
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
	runGitBreach(t, dir, "stash", "push", "-q", "-m", "wip")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachStashTable)
	r.gitBreachStashTable.Select(0, 0)

	r.openGitBreachStashDrop()

	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want the confirm dialog before dropping a stash", r.activePage)
	}
	if len(r.gitBreachStash) != 1 {
		t.Error("stash already gone before confirming, want the drop to not have run yet")
	}

	r.pendingConfirm()

	if len(r.gitBreachStash) != 0 {
		t.Errorf("gitBreachStash = %+v, want none after confirming the drop", r.gitBreachStash)
	}
}

// TestOpenGitBreachStashApplyDoesNothingWithNoStash pins a plain no-op
// guard: there's nothing under the cursor to apply on an empty stash
// stack, the same restraint gitBreachStashAt's own bounds check already
// gives every other Stash action.
func TestOpenGitBreachStashApplyDoesNothingWithNoStash(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachStashTable)

	r.openGitBreachStashApply()

	if r.activePage == confirmPage {
		t.Error("activePage = confirmPage, want a no-op with no stash entries")
	}
}

// TestActivateGitBreachMainFromFilesCommitsStashGivesMainFocus pins
// Right as the one activation key that applies uniformly to every box
// with a real diff of its own — Files, Commits and Stash — leaving
// Branches out (see its own dedicated test below), since Branches
// never owns Main's content in the first place (gitBreachMainOwnerKind
// has no Branches value at all).
func TestActivateGitBreachMainFromFilesCommitsStashGivesMainFocus(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	for _, tc := range []struct {
		name    string
		focus   func(r *Root) tview.Primitive
		capture func(r *Root) func(*tcell.EventKey) *tcell.EventKey
	}{
		{"Files", func(r *Root) tview.Primitive { return r.gitBreachFilesTable }, func(r *Root) func(*tcell.EventKey) *tcell.EventKey { return r.captureGitBreachFilesTableKey }},
		{"Commits", func(r *Root) tview.Primitive { return r.gitBreachCommitsTable }, func(r *Root) func(*tcell.EventKey) *tcell.EventKey { return r.captureGitBreachCommitsTableKey }},
		{"Stash", func(r *Root) tview.Primitive { return r.gitBreachStashTable }, func(r *Root) func(*tcell.EventKey) *tcell.EventKey { return r.captureGitBreachStashTableKey }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRootForGitBreachDir(t, dir)
			r.openGitBreach()
			r.app.SetFocus(tc.focus(r))

			if got := tc.capture(r)(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)); got != nil {
				t.Errorf("capture(KeyRight) = %v, want nil (consumed)", got)
			}
			if !r.gitBreachDiffView.HasFocus() {
				t.Error("Right did not move keyboard focus to Main")
			}
		})
	}
}

// TestGitBreachRightArrowFromBranchesDoesNothing pins the one box
// explicitly excluded from activateGitBreachMain: Branches never owns
// Main's content (no diff to jump into), so Right there is left
// unhandled and falls through to tview.Table's own default — which
// does nothing useful for a single-selectable-column table, but
// crucially does NOT move keyboard focus away from Branches.
func TestGitBreachRightArrowFromBranchesDoesNothingSpecial(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachBranchesTable)

	r.captureGitBreachBranchesTableKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))

	if r.gitBreachDiffView.HasFocus() {
		t.Error("Right from Branches moved focus to Main, but Branches never owns a diff")
	}
	if !r.gitBreachBranchesTable.HasFocus() {
		t.Error("Right from Branches should leave focus on Branches")
	}
}

// TestGitBreachEnterAndSpaceActivateMainOnlyFromCommits pins the
// collision resolution from the user's own explicit design chat: Space
// already stages a Files row and Enter already checks out a Branches
// row, so neither gains a second meaning there; Stash's own diff is
// already fully visible on selection with "a"/"d" as its real actions,
// so the user confirmed Enter/Space would have nothing to add there
// either. Commits is the one box where both are free.
func TestGitBreachEnterAndSpaceActivateMainOnlyFromCommits(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	for _, key := range []tcell.Key{tcell.KeyEnter, tcell.KeyRune} {
		event := tcell.NewEventKey(key, ' ', tcell.ModNone)
		if key != tcell.KeyRune {
			event = tcell.NewEventKey(key, 0, tcell.ModNone)
		}

		r := newTestRootForGitBreachDir(t, dir)
		r.openGitBreach()
		r.app.SetFocus(r.gitBreachCommitsTable)

		r.captureGitBreachCommitsTableKey(event)

		if !r.gitBreachDiffView.HasFocus() {
			t.Errorf("key %v from Commits did not activate Main", key)
		}
	}
}

// TestGitBreachSpaceStillStagesInFilesNotActivateMain and its Branches
// counterpart below are the regression guard for the collision
// resolution above: wiring Enter/Space into Commits must not have
// changed what either key already does in Files/Branches.
func TestGitBreachSpaceStillStagesInFilesNotActivateMain(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachFilesTable)

	r.captureGitBreachFilesTableKey(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone))

	if r.gitBreachDiffView.HasFocus() {
		t.Error("Space in Files activated Main instead of staging — collides with its own existing meaning")
	}
}

func TestGitBreachEnterStillChecksOutInBranchesNotActivateMain(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")
	runGitBreach(t, dir, "branch", "feature-x")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachBranchesTable)
	r.gitBreachBranchesTable.Select(1, 0) // row 0 is the current branch; pick a different one

	r.captureGitBreachBranchesTableKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if r.gitBreachDiffView.HasFocus() {
		t.Error("Enter in Branches activated Main instead of checking out — collides with its own existing meaning")
	}
}

// TestCaptureGitBreachMainKeyReturnsFocusToTheOwner pins the uniform
// "way back" from Main — Left, Enter and Space all return focus to
// whichever box activated it (gitBreachMainOwner), regardless of which
// key was used to get in.
func TestCaptureGitBreachMainKeyReturnsFocusToTheOwner(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")

	for _, key := range []tcell.Key{tcell.KeyLeft, tcell.KeyEnter, tcell.KeyRune} {
		event := tcell.NewEventKey(key, 0, tcell.ModNone)
		if key == tcell.KeyRune {
			event = tcell.NewEventKey(key, ' ', tcell.ModNone)
		}

		r := newTestRootForGitBreachDir(t, dir)
		r.openGitBreach()
		r.app.SetFocus(r.gitBreachCommitsTable)
		r.activateGitBreachMain()
		if !r.gitBreachDiffView.HasFocus() {
			t.Fatalf("setup: activateGitBreachMain didn't give Main focus for key %v", key)
		}

		if got := r.captureGitBreachMainKey(event); got != nil {
			t.Errorf("captureGitBreachMainKey(%v) = %v, want nil (consumed)", key, got)
		}
		if !r.gitBreachCommitsTable.HasFocus() {
			t.Errorf("key %v from Main didn't return focus to Commits (the owner)", key)
		}
	}
}

// TestCaptureGitBreachMainKeyEscReturnsToOwnerWithoutClosing pins the
// one deliberately context-dependent key in this dashboard: Esc closes
// Git breach everywhere except while Main has focus, where it only
// steps back out of the diff — confirmed here does NOT close
// (activePage stays unchanged / Git breach stays open) and DOES return
// focus to the owner, both in the same assertion the other three keys
// above already share.
func TestCaptureGitBreachMainKeyEscReturnsToOwnerWithoutClosing(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachCommitsTable)
	r.activateGitBreachMain()

	if got := r.captureGitBreachMainKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); got != nil {
		t.Errorf("captureGitBreachMainKey(Esc) = %v, want nil (consumed)", got)
	}
	if !r.gitBreachCommitsTable.HasFocus() {
		t.Error("Esc from Main didn't return focus to Commits")
	}
	if r.activePage != gitBreachPage {
		t.Error("Esc from Main closed Git breach entirely — it should only back out of the diff")
	}
}

// TestCaptureGitBreachMainKeyEscDifferentFromListBoxEsc is the
// companion check from the other side: Esc on any of the four list
// boxes still closes Git breach exactly as before, unaffected by Esc's
// new, Main-only meaning.
func TestCaptureGitBreachMainKeyEscDifferentFromListBoxEsc(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachCommitsTable)

	r.captureGitBreachCommitsTableKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	if r.activePage == gitBreachPage {
		t.Error("Esc from Commits (a list box) should still close Git breach")
	}
}

// TestCaptureGitBreachMainKeyTabAdvancesFromTheOwnersPosition and its
// Shift-Tab counterpart below pin the "Main sits spatially at its
// owner's position in the ring" model: Tab/Shift-Tab from Main behave
// exactly as if pressed from the owning box itself, continuing the
// cycle rather than resetting it.
func TestCaptureGitBreachMainKeyTabAdvancesFromTheOwnersPosition(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachCommitsTable) // gitBreachFocusables index 2
	r.activateGitBreachMain()

	r.captureGitBreachMainKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))

	if !r.gitBreachStashTable.HasFocus() {
		t.Error("Tab from Main (owned by Commits) should land on Stash, the next box after Commits")
	}
}

func TestCaptureGitBreachMainKeyBacktabRetreatsFromTheOwnersPosition(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachCommitsTable) // gitBreachFocusables index 2
	r.activateGitBreachMain()

	r.captureGitBreachMainKey(tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone))

	if !r.gitBreachBranchesTable.HasFocus() {
		t.Error("Shift-Tab from Main (owned by Commits) should land on Branches, the box before Commits")
	}
}

// TestToggleGitBreachFocusFromMainUsesTheOwnersPosition and its
// reverse counterpart pin the exact same owner-relative behavior
// reached through the shared toggleGitBreachFocus/
// reverseToggleGitBreachFocus functions directly (real Tab/Shift-Tab
// presses on any other box already go through these, so this is the
// same code path, not a separate one), using Stash as the owner this
// time to also prove it isn't hardcoded to Commits.
func TestToggleGitBreachFocusFromMainUsesTheOwnersPosition(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachStashTable) // gitBreachFocusables index 3
	r.activateGitBreachMain()

	r.toggleGitBreachFocus()

	if !r.gitBreachFilesTable.HasFocus() {
		t.Error("Tab from Main (owned by Stash) should wrap to Files, the box after Stash")
	}
}

func TestReverseToggleGitBreachFocusFromMainUsesTheOwnersPosition(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachStashTable) // gitBreachFocusables index 3
	r.activateGitBreachMain()

	r.reverseToggleGitBreachFocus()

	if !r.gitBreachCommitsTable.HasFocus() {
		t.Error("Shift-Tab from Main (owned by Stash) should land on Commits, the box before Stash")
	}
}

// TestCaptureGitBreachMainKeyCommitAndReloadWorkFromMainToo pins "c"
// and "r" as the two keys already documented as focus-independent
// everywhere else in this dashboard — Main is now a real focus target
// too, so both must keep working from there instead of silently going
// dead the moment the cursor is actually reading a diff.
func TestCaptureGitBreachMainKeyCommitAndReloadWorkFromMainToo(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachCommitsTable)
	r.activateGitBreachMain()

	r.captureGitBreachMainKey(tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModNone))
	if r.activePage != promptPage {
		t.Error(`"c" from Main didn't open the commit prompt`)
	}

	r2 := newTestRootForGitBreachDir(t, dir)
	r2.openGitBreach()
	r2.app.SetFocus(r2.gitBreachCommitsTable)
	r2.activateGitBreachMain()

	before := len(r2.gitBreachCommits)
	r2.captureGitBreachMainKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone))
	if len(r2.gitBreachCommits) != before {
		t.Error(`"r" from Main should still reload without erroring`)
	}
}

// TestCaptureGitBreachMainKeyLetsScrollKeysThrough pins the "only
// override what actually means something different here" contract:
// Up/Down/PgUp/PgDn and Right must all fall through unchanged to
// tview.TextView's own already-correct scrolling, not get swallowed by
// this capture function the way Left/Enter/Space/Esc/Tab/Backtab/c/r
// deliberately are.
func TestCaptureGitBreachMainKeyLetsScrollKeysThrough(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	for _, key := range []tcell.Key{tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyRight, tcell.KeyHome, tcell.KeyEnd} {
		event := tcell.NewEventKey(key, 0, tcell.ModNone)
		if got := r.captureGitBreachMainKey(event); got != event {
			t.Errorf("captureGitBreachMainKey(%v) = %v, want the same event passed through unchanged", key, got)
		}
	}
}

// TestRenderGitBreachDiffHeaderShowsPageHint pins the "Page X/Y" math
// for the simplest case: scrolled to the very top, total pages is the
// line count divided by page height, rounded up (ceil), since a
// partially filled last page — the user's own explicit example, 487
// lines at 50 rows/page is still 10 pages, not 9.
func TestRenderGitBreachDiffHeaderShowsPageHint(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	lines := make([]string, 487)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	r.gitBreachDiffView.SetText(strings.Join(lines, "\n"))

	r.renderGitBreachDiffHeader(40, 50)

	got := r.gitBreachDiffHeader.GetText(true)
	if !strings.Contains(got, "Page 1/10") {
		t.Errorf("header = %q, want it to contain %q", got, "Page 1/10")
	}
}

// TestRenderGitBreachDiffHeaderUpdatesWithScrollOffset pins that the
// current page follows the real scroll position, not just the total —
// scrolling to row 100 at 50 rows/page should read as page 3 (rows
// 100-149 are the third page, 0-indexed row/height + 1).
func TestRenderGitBreachDiffHeaderUpdatesWithScrollOffset(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	lines := make([]string, 487)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	r.gitBreachDiffView.SetText(strings.Join(lines, "\n"))
	r.gitBreachDiffView.ScrollTo(100, 0)

	r.renderGitBreachDiffHeader(40, 50)

	got := r.gitBreachDiffHeader.GetText(true)
	if !strings.Contains(got, "Page 3/10") {
		t.Errorf("header = %q, want it to contain %q", got, "Page 3/10")
	}
}

// TestRenderGitBreachDiffHeaderClampsAtTheLastPage pins that
// scrolling past what GetScrollOffset would naively divide into a page
// beyond the real total (tview clamps scroll itself in practice, but
// the math here must not silently show "page 11/10" if it ever didn't)
// clamps back down to the real last page instead.
func TestRenderGitBreachDiffHeaderClampsAtTheLastPage(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	lines := make([]string, 487)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	r.gitBreachDiffView.SetText(strings.Join(lines, "\n"))
	r.gitBreachDiffView.ScrollTo(10000, 0) // far past the real end

	r.renderGitBreachDiffHeader(40, 50)

	got := r.gitBreachDiffHeader.GetText(true)
	if !strings.Contains(got, "Page 10/10") {
		t.Errorf("header = %q, want it clamped to %q", got, "Page 10/10")
	}
}

// TestRenderGitBreachDiffHeaderHidesHintWithoutRealHeightOrText pins
// the two "nothing to page through yet" guards: height still zero
// (before the first real Draw, the same construction-time-placeholder
// reason every other SetDrawFunc-based header in this package already
// documents) and an empty diff (nothing loaded/selected).
func TestRenderGitBreachDiffHeaderHidesHintWithoutRealHeightOrText(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	r.gitBreachDiffView.SetText("some text")
	r.renderGitBreachDiffHeader(40, 0)
	if got := r.gitBreachDiffHeader.GetText(true); strings.Contains(got, "Page") {
		t.Errorf("header = %q, want no page hint with zero height", got)
	}

	r.gitBreachDiffView.SetText("")
	r.renderGitBreachDiffHeader(40, 50)
	if got := r.gitBreachDiffHeader.GetText(true); strings.Contains(got, "Page") {
		t.Errorf("header = %q, want no page hint with no diff text", got)
	}
}

// TestGitBreachDiffPageReachesTheLastPageOnUnevenContent pins the
// user's own explicit bug report: at a box height of 10 with 29 lines
// of content (not a multiple of the height), the maximum row
// tview.TextView ever actually scrolls to is 19 (29-10 -- there's
// nothing further below to show), which the old row/height+1 formula
// reported as page 2 of 3, a last page that could never actually be
// reached no matter how far down the user scrolled. gitBreachDiffPage
// maps row proportionally across the real reachable range instead, so
// the true maximum row always reads as the real last page.
func TestGitBreachDiffPageReachesTheLastPageOnUnevenContent(t *testing.T) {
	const height = 10
	const totalLines = 29
	maxRow := totalLines - height // 19 -- the real ceiling tview itself clamps scrolling to

	page, totalPages := gitBreachDiffPage(maxRow, totalLines, height)

	if totalPages != 3 {
		t.Fatalf("totalPages = %d, want 3 (ceil(29/10))", totalPages)
	}
	if page != totalPages {
		t.Errorf("page at the true maximum scroll row = %d, want %d (the last page must be reachable)", page, totalPages)
	}
}

// TestGitBreachDiffPageStartsAtOneAndNeverExceedsTotal pins the two
// edges gitBreachDiffPage must never miss regardless of how row/height
// divide: row 0 is always page 1, and a row past the real maximum
// (tview itself should never hand one, but a defensive clamp costs
// nothing) never reports a page past totalPages.
func TestGitBreachDiffPageStartsAtOneAndNeverExceedsTotal(t *testing.T) {
	if page, _ := gitBreachDiffPage(0, 29, 10); page != 1 {
		t.Errorf("page at row 0 = %d, want 1", page)
	}

	page, totalPages := gitBreachDiffPage(10_000, 29, 10)
	if page != totalPages {
		t.Errorf("page past the real maximum row = %d, want it clamped to %d", page, totalPages)
	}
}

// TestGitBreachSelectingADifferentCommitStartsItFreshThenRemembersIt
// pins the corrected shape of the user's own original bug report: a
// commit genuinely never visited before starts at the top (not
// commit A's own leftover scroll position), but once you've actually
// scrolled into a commit, returning to it later in the same session
// picks back up exactly where you left off — the user's own explicit
// follow-up correction after an earlier, wrong reading of "don't carry
// the scroll over" as "always reset to the top", which broke the
// actual real-world use this was asked for in the first place:
// comparing a spot on page 4 of one commit against page 5 of another
// by flipping back and forth.
func TestGitBreachSelectingADifferentCommitStartsItFreshThenRemembersIt(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "first")
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "second")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachCommitsTable)

	r.gitBreachDiffView.ScrollTo(50, 0)
	if row, _ := r.gitBreachDiffView.GetScrollOffset(); row != 50 {
		t.Fatalf("setup: ScrollTo(50, 0) left scroll offset at row %d, want 50", row)
	}

	r.gitBreachCommitsTable.Select(1, 0) // a different commit, never visited before

	if row, _ := r.gitBreachDiffView.GetScrollOffset(); row != 0 {
		t.Errorf("scroll offset = %d on a commit visited for the first time, want 0", row)
	}

	r.gitBreachDiffView.ScrollTo(7, 0)
	r.gitBreachCommitsTable.Select(0, 0) // back to the first commit

	if row, _ := r.gitBreachDiffView.GetScrollOffset(); row != 50 {
		t.Errorf("scroll offset = %d after returning to the first commit, want its own remembered 50", row)
	}

	r.gitBreachCommitsTable.Select(1, 0) // and back to the second

	if row, _ := r.gitBreachDiffView.GetScrollOffset(); row != 7 {
		t.Errorf("scroll offset = %d after returning to the second commit, want its own remembered 7", row)
	}
}

// TestGitBreachRefocusingTheSameRowKeepsMainScroll is the regression
// guard the other way: merely leaving Main and coming back to the same
// still-selected row (no row change at all) must not reset the scroll
// position — only an actual selection change should (see the test
// above), the exact distinction the user's own bug report drew.
func TestGitBreachRefocusingTheSameRowKeepsMainScroll(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachCommitsTable)
	r.activateGitBreachMain()
	r.gitBreachDiffView.ScrollTo(5, 0)

	r.deactivateGitBreachMain()
	r.activateGitBreachMain()

	if row, _ := r.gitBreachDiffView.GetScrollOffset(); row != 5 {
		t.Errorf("scroll offset = %d after refocusing the same row, want 5 (unchanged)", row)
	}
}

// TestGitBreachFileDiffScrollKeyDistinguishesStagedFromUnstaged pins
// the subtlest part of per-entry scroll memory: the same path can
// appear twice at once (staged and unstaged), showing two genuinely
// different diffs — they must never share one remembered scroll
// position just because the path string is identical.
func TestGitBreachFileDiffScrollKeyDistinguishesStagedFromUnstaged(t *testing.T) {
	staged := gitBreachRow{kind: gitBreachRowStaged, path: "a.txt"}
	unstaged := gitBreachRow{kind: gitBreachRowUnstaged, path: "a.txt"}

	if gitBreachFileDiffScrollKey(staged) == gitBreachFileDiffScrollKey(unstaged) {
		t.Errorf("staged and unstaged %q share one scroll key: %q", "a.txt", gitBreachFileDiffScrollKey(staged))
	}
}

// TestGitBreachStashScrollIsRememberedPerEntry mirrors the Commits
// scroll-memory test above, for Stash.
func TestGitBreachStashScrollIsRememberedPerEntry(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "stash", "push", "-q", "-u", "-m", "first stash")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "stash", "push", "-q", "-u", "-m", "second stash")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachStashTable)

	r.gitBreachDiffView.ScrollTo(3, 0)
	r.gitBreachStashTable.Select(1, 0) // the other stash entry

	if row, _ := r.gitBreachDiffView.GetScrollOffset(); row != 0 {
		t.Errorf("scroll offset = %d on a stash entry visited for the first time, want 0", row)
	}

	r.gitBreachStashTable.Select(0, 0) // back to the first

	if row, _ := r.gitBreachDiffView.GetScrollOffset(); row != 3 {
		t.Errorf("scroll offset = %d after returning to the first stash entry, want its own remembered 3", row)
	}
}

// TestGitBreachMainGetsFocusStyleWhenActivated pins that Main's own
// header follows the same focused/InputFocusedBackground vs.
// unfocused/InputBackground scheme every other box's header already
// gets from styleGitBreachFocus — without this, focus moving into the
// diff pane would be invisible beyond the selected row staying
// highlighted, since Main isn't one of gitBreachFocusables' own
// entries and so isn't covered by the existing per-box FocusFunc loop.
func TestGitBreachMainGetsFocusStyleWhenActivated(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.app.SetFocus(r.gitBreachCommitsTable)

	unfocused := r.gitBreachDiffHeader.GetBackgroundColor()

	r.activateGitBreachMain()

	if r.gitBreachDiffHeader.GetBackgroundColor() == unfocused {
		t.Error("Main's own header background didn't change when it gained keyboard focus")
	}
}

// gitBreach256Palette builds the real xterm 256-color palette once,
// the same lookup tcell.FindColor itself degrades truecolor against on
// a terminal that doesn't advertise truecolor support (see
// gitBreachDiffAddedBackground/gitBreachDiffRemovedBackground's own
// doc comment for the real report this is pinning against: iSH on
// iPadOS, though any 256-color terminal has the identical problem).
func gitBreach256Palette() []tcell.Color {
	pal := make([]tcell.Color, 256)
	for i := range pal {
		pal[i] = tcell.PaletteColor(i)
	}
	return pal
}

// gitBreachIsGrayish reports whether c's own R/G/B channels are close
// enough to each other to read as a shade of gray rather than a real
// hue — the exact failure mode a 256-color degraded removed/added
// background collapsed into before this fix (see the two colors' own
// doc comment).
func gitBreachIsGrayish(c tcell.Color) bool {
	r, g, b := c.RGB()
	maxCh, minCh := r, r
	for _, v := range []int32{g, b} {
		if v > maxCh {
			maxCh = v
		}
		if v < minCh {
			minCh = v
		}
	}
	return maxCh-minCh < 10
}

// TestGitBreachDiffBackgroundsStayDistinctOnA256ColorTerminal pins the
// actual user-reported bug: on a terminal without truecolor support,
// tcell quantizes both gitBreachDiffAddedBackground and
// gitBreachDiffRemovedBackground against the real 256-color palette —
// before this fix, both landed on neighboring, nearly identical
// grayscale-ramp entries ("one looks black, the other gray", neither
// visibly red or green). Pinned directly against tcell.FindColor and
// the real palette, not against the raw truecolor hex values
// themselves, since it's specifically the degraded result that was
// broken.
func TestGitBreachDiffBackgroundsStayDistinctOnA256ColorTerminal(t *testing.T) {
	pal := gitBreach256Palette()

	addedFit := tcell.FindColor(gitBreachDiffAddedBackground, pal)
	removedFit := tcell.FindColor(gitBreachDiffRemovedBackground, pal)

	if gitBreachIsGrayish(addedFit) {
		t.Errorf("gitBreachDiffAddedBackground degrades to %v on a 256-color terminal, want a real green, not a shade of gray", addedFit)
	}
	if gitBreachIsGrayish(removedFit) {
		t.Errorf("gitBreachDiffRemovedBackground degrades to %v on a 256-color terminal, want a real red, not a shade of gray", removedFit)
	}
	if addedFit == removedFit {
		t.Errorf("added and removed backgrounds both degrade to the same %v on a 256-color terminal, want them visibly distinct", addedFit)
	}
}

// TestGitBreachDiffMarkersStayVisibleAgainstTheirOwnBackground pins the
// user's own explicit follow-up concern when asked to brighten these
// backgrounds: the "+"/"-" marker glyph itself must not end up the
// same color as the now-brighter background it sits on, in either a
// truecolor or a degraded 256-color terminal.
func TestGitBreachDiffMarkersStayVisibleAgainstTheirOwnBackground(t *testing.T) {
	pal := gitBreach256Palette()

	if gitBreachDiffAddedColor == gitBreachDiffAddedBackground {
		t.Error("added marker color is identical to its own background")
	}
	if theme := tcell.GetColor("red"); theme == gitBreachDiffRemovedBackground {
		t.Error("removed marker color is identical to its own background")
	}

	addedMarkerFit := tcell.FindColor(gitBreachDiffAddedColor, pal)
	addedBGFit := tcell.FindColor(gitBreachDiffAddedBackground, pal)
	if addedMarkerFit == addedBGFit {
		t.Errorf("added marker and its own background both degrade to the same %v on a 256-color terminal", addedMarkerFit)
	}

	removedMarkerFit := tcell.FindColor(tcell.GetColor("red"), pal)
	removedBGFit := tcell.FindColor(gitBreachDiffRemovedBackground, pal)
	if removedMarkerFit == removedBGFit {
		t.Errorf("removed marker and its own background both degrade to the same %v on a 256-color terminal", removedMarkerFit)
	}
}

// gitBreachContrastRatio is the plain WCAG 2.x relative-luminance
// contrast formula — used here to pin a minimum readability bar for
// the comment color against Git breach's own degraded diff
// backgrounds, the same formula (re-implemented, not imported — this
// package has no existing contrast helper) the standalone spike script
// that picked #a8a8a8 used to verify it before shipping.
func gitBreachContrastRatio(c1, c2 tcell.Color) float64 {
	srgb := func(v int32) float64 {
		x := float64(v) / 255.0
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	luminance := func(c tcell.Color) float64 {
		r, g, b := c.RGB()
		return 0.2126*srgb(r) + 0.7152*srgb(g) + 0.0722*srgb(b)
	}
	l1, l2 := luminance(c1), luminance(c2)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// TestGitBreachDiffCommentColorStaysReadableOnA256ColorTerminal pins
// the user's own second live follow-up to the background-color fix
// above: a muted comment token must still clear a real readability bar
// against Git breach's own degraded added/removed backgrounds, not
// just avoid being literally invisible. tcell.ColorGray (#808080, the
// previous darkSyntaxPalette comment color) only cleared 2.02:1
// against the degraded green background — this pins the real floor
// (3:1, the WCAG large-text/non-text threshold — a comment color was
// never meant to clear full 4.5:1 body-text contrast) so a future
// palette tweak can't silently regress this fix the way the original
// backgrounds themselves regressed unnoticed.
func TestGitBreachDiffCommentColorStaysReadableOnA256ColorTerminal(t *testing.T) {
	pal := gitBreach256Palette()
	commentFit := tcell.FindColor(darkSyntaxPalette[viewer.KindComment], pal)
	addedBGFit := tcell.FindColor(gitBreachDiffAddedBackground, pal)
	removedBGFit := tcell.FindColor(gitBreachDiffRemovedBackground, pal)

	const minContrast = 3.0
	if got := gitBreachContrastRatio(commentFit, addedBGFit); got < minContrast {
		t.Errorf("degraded comment color vs degraded added background = %.2f, want >= %.1f", got, minContrast)
	}
	if got := gitBreachContrastRatio(commentFit, removedBGFit); got < minContrast {
		t.Errorf("degraded comment color vs degraded removed background = %.2f, want >= %.1f", got, minContrast)
	}
}

// TestGCReachesAIConsigliereFromEveryGitBreachPane pins "gC" reaching
// the AI Consigliere from inside Git Breach — see startConsigliereChord's
// own doc comment in consigliere.go. acceptsPlainKeyCommand (see
// keymap.go) never passes while Git Breach is the active full-screen
// overlay, so "g" alone could never have started the chord without
// each pane's own InputCapture explicitly doing it; this exercises
// that for every pane, not just one.
func TestGCReachesAIConsigliereFromEveryGitBreachPane(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	for _, tc := range []struct {
		name    string
		focus   func(r *Root) tview.Primitive
		capture func(r *Root) func(*tcell.EventKey) *tcell.EventKey
	}{
		{"Files", func(r *Root) tview.Primitive { return r.gitBreachFilesTable }, func(r *Root) func(*tcell.EventKey) *tcell.EventKey { return r.captureGitBreachFilesTableKey }},
		{"Branches", func(r *Root) tview.Primitive { return r.gitBreachBranchesTable }, func(r *Root) func(*tcell.EventKey) *tcell.EventKey { return r.captureGitBreachBranchesTableKey }},
		{"Commits", func(r *Root) tview.Primitive { return r.gitBreachCommitsTable }, func(r *Root) func(*tcell.EventKey) *tcell.EventKey { return r.captureGitBreachCommitsTableKey }},
		{"Stash", func(r *Root) tview.Primitive { return r.gitBreachStashTable }, func(r *Root) func(*tcell.EventKey) *tcell.EventKey { return r.captureGitBreachStashTableKey }},
		{"Main", func(r *Root) tview.Primitive { return r.gitBreachDiffView }, func(r *Root) func(*tcell.EventKey) *tcell.EventKey { return r.captureGitBreachMainKey }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRootForGitBreachDir(t, dir)
			r.openGitBreach()
			r.app.SetFocus(tc.focus(r))

			if got := tc.capture(r)(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone)); got != nil {
				t.Fatalf("capture('g') = %v, want nil (consumed, chord started)", got)
			}
			if r.pendingChord != 'g' {
				t.Fatalf("pendingChord = %q, want 'g'", string(r.pendingChord))
			}

			if !r.HandlePlainKey(tcell.NewEventKey(tcell.KeyRune, 'C', tcell.ModNone)) {
				t.Fatal("the resolving 'C' should have been consumed")
			}
			if !r.consigliereVisible {
				t.Error("\"gC\" should have opened the AI Consigliere sidebar")
			}
		})
	}
}
