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

func TestGitBreachHintShowsEnterOnlyWhileBranchesHasFocus(t *testing.T) {
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
		t.Error("hint entries still mention Enter after focus moved to Commits")
	}
}
