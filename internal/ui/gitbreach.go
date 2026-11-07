// gitbreach.go is the Git breach dashboard's own glue between
// internal/git (Root/Fetch/Stage/Unstage/Commit/Diff — the real,
// UI-free business logic) and the multi-pane layout gitbreachscreen.go
// builds. See gitBreach.Layout's own doc comment on Root for the
// overall shape and why it's planned as several boxes from the start.
package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/compare"
	"github.com/jagottsicher/breakthrough/internal/git"
	"github.com/jagottsicher/breakthrough/internal/viewer"
)

const gitBreachPage = "gitBreach"

// gitBreachState holds the Git Breach dashboard's own widgets (per
// pane: Status/Files/Branches/Commits/Stash/Diff), the last read
// result for each pane, and the Diff pane's own split-view state (see
// gitbreach.go/gitbreachscreen.go/gitbreachdiffsplit.go).
type gitBreachState struct {
	Layout            *tview.Flex
	TitleBar          *tview.TextView
	StatusHeader      *tview.TextView
	StatusView        *tview.TextView
	FilesHeader       *tview.TextView
	FilesTable        *tview.Table
	FilesScrollbar    *verticalScrollbar
	BranchesHeader    *tview.TextView
	BranchesTable     *tview.Table
	BranchesScrollbar *verticalScrollbar
	CommitsHeader     *tview.TextView
	CommitsTable      *tview.Table
	CommitsScrollbar  *verticalScrollbar
	StashHeader       *tview.TextView
	StashTable        *tview.Table
	StashScrollbar    *verticalScrollbar
	DiffHeader        *tview.TextView
	DiffView          *tview.TextView
	DiffScrollbar     *verticalScrollbar
	DiffHScrollbar    *horizontalScrollbar
	DiffSpacer        *tview.Box
	Hint              *tview.TextView
	HintSpans         []listHintSpan
	Dir               string // the directory "jg" was invoked from
	Root              string // resolved repository root, see git.Root
	Status            git.Status
	Rows              []gitBreachRow
	RowIndex          map[int]int
	FetchErr          error
	FilesReady        bool
	Branches          []git.Branch
	BranchesErr       error
	BranchesReady     bool
	// BranchesShowTags switches the Branches box between its two
	// sub-tabs (Branches/Tags, see renderGitBreachBranchesHeader) —
	// one box, one table, two row kinds, the same "no new box" shape
	// feature_ideas.txt's own Box-Gruppierung plan calls for (Tags is
	// meant to join Branches/Remotes as tabs within this one box, not
	// become a fifth box of its own). Reset to false on every fresh
	// "jg" open (see openGitBreach), same as CommitsLimit.
	BranchesShowTags    bool
	Tags                []git.Tag
	TagsErr             error
	Commits             []git.LogEntry
	CommitsErr          error
	CommitsReady        bool
	CommitsLimit        int
	CommitsTotal        int
	Stash               []git.Stash
	StashErr            error
	StashReady          bool
	MainOwner           gitBreachMainOwnerKind
	DiffCancel          context.CancelFunc
	DiffScroll          map[string]int
	DiffScrollKey       string
	DiffSplit           bool
	DiffPages           *tview.Pages
	DiffSplitLeft       *tview.TextView
	DiffSplitRight      *tview.TextView
	DiffSplitSeparator  *tview.Box
	DiffSplitScrollbar  *verticalScrollbar
	DiffSplitHScrollbar *horizontalScrollbar
	DiffSplitSpacer     *tview.Box
	DiffRawText         string
	DiffFiles           []compare.FileDiff
}

// gitBreachFetchTimeout bounds how long "jg" itself, and every
// subsequent reload (see reloadGitBreach), waits on git before giving
// up — the same reasoning statusBarGitStatusTimeout already documents,
// just more generous (3s vs. 500ms): this is a deliberate, explicit
// action, not a once-a-second clock tick competing with other segments.
const gitBreachFetchTimeout = 3 * time.Second

// openGitBreach is "jg" — resolves whichever repository contains the
// active panel's own current directory (its own parents included,
// exactly what git itself would find — see git.Root) and opens the
// dashboard on it. Always re-resolves fresh, the same "reflect the
// real, current state" reasoning openLogAudit's own doc comment
// already gives for its own directory: the active panel can have moved
// to a different repository entirely between two "jg" presses.
//
// Refuses, rather than silently showing the wrong (or no) repository,
// in exactly three cases: git itself isn't installed, the active panel
// is connected to a remote session, or it's browsing inside an archive
// — p.path in either of the latter two is a string that merely looks
// like a local path but doesn't name one on this machine's own
// filesystem (see p.isRemote/p.inArchiveView's own doc comments), the
// same class of bug PR #375's clipboard fix addressed for a different
// feature. git itself not being a working tree at all is reported the
// same way, once git.Root actually runs.
func (r *Root) openGitBreach() {
	if r.panel.isRemote() || r.panel.inArchiveView() {
		r.showError(fmt.Errorf("git breach: only available for a real local working tree, not a remote connection or an archive view"))
		return
	}
	if !git.Available() {
		r.showError(fmt.Errorf("git breach: git itself was not found on $PATH"))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	root, inRepo, err := git.Root(ctx, r.panel.path)
	if err != nil {
		r.showError(fmt.Errorf("git breach: %w", err))
		return
	}
	if !inRepo {
		r.showError(fmt.Errorf("git breach: %q is not inside a git working tree", r.panel.path))
		return
	}

	r.gitBreach.Dir = r.panel.path
	r.gitBreach.Root = root
	// Reset to the starting limit on every fresh "jg" open — unlike "r"
	// (reloadGitBreach on its own, see its own doc comment), which
	// deliberately leaves an already-expanded limit alone so a user who
	// scrolled to 600 commits and reloads doesn't lose that expansion.
	r.gitBreach.CommitsLimit = git.CommitLogLimit
	r.gitBreach.BranchesShowTags = false
	r.reloadGitBreach()
	r.showOverlayWithRestore(gitBreachPage, r.gitBreach.Layout, r.restoreGitBreachFocus)
}

// restoreGitBreachFocus sends real keyboard focus to the Files table —
// both on first open (showOverlayWithRestore's own initial call) and
// whenever something pushed on top of this dashboard closes back to it
// (the commit-message prompt — see openGitBreachCommitPrompt), the
// same "restore callback" shape restoreProperties already establishes.
// Always Files specifically, not "whichever box last had focus": a
// fresh "jg" (or returning from the commit prompt mid-session) should
// land somewhere predictable rather than wherever Tab (see
// toggleGitBreachFocus) happened to leave it last time.
func (r *Root) restoreGitBreachFocus() {
	r.app.SetFocus(r.gitBreach.FilesTable)
}

// reloadGitBreach re-fetches gitBreach.Root's own status and branch
// list, rebuilding every box's own content — "r" re-runs this
// directly, and every stage/unstage/commit/checkout does too
// afterward, the same "re-run the real read, don't just redraw"
// contract reloadLogAuditDiscovery already documents.
func (r *Root) reloadGitBreach() {
	// Save whatever is genuinely on screen right now under its own
	// real key, then clear the active key entirely — rebuilding each
	// table below can call Select(0, 0) on a row whose own selection
	// became invalid (see e.g. renderGitBreachCommits' own "the !ok
	// check alone isn't enough" comment), which fires
	// SetSelectionChangedFunc exactly like a real cursor move would.
	// Clearing the key first makes any such incidental reselection's own
	// gitBreachSaveDiffScroll a deliberate no-op instead of a second,
	// later save under a key this function's own save above already
	// handled correctly — defensive, not shown to change behavior in the
	// one reload-with-an-invalidated-selection scenario tried directly
	// (that path's own existing unconditional startGitBreach*DiffIfOwner
	// call already keeps Main in sync regardless), but cheap enough to
	// keep as a guard against a future change in that call order.
	r.gitBreachSaveDiffScroll()
	r.gitBreach.DiffScrollKey = ""

	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	status, err := git.Fetch(ctx, r.gitBreach.Root)
	r.gitBreach.Status = status
	r.gitBreach.FetchErr = err
	r.gitBreach.Rows = buildGitBreachRows(status)

	branches, branchesErr := git.Branches(ctx, r.gitBreach.Root)
	r.gitBreach.Branches = branches
	r.gitBreach.BranchesErr = branchesErr

	tags, tagsErr := git.Tags(ctx, r.gitBreach.Root)
	r.gitBreach.Tags = tags
	r.gitBreach.TagsErr = tagsErr

	commits, commitsErr := git.Log(ctx, r.gitBreach.Root, r.gitBreach.CommitsLimit)
	r.gitBreach.Commits = commits
	r.gitBreach.CommitsErr = commitsErr
	total, totalErr := git.CommitCount(ctx, r.gitBreach.Root)
	if totalErr == nil {
		r.gitBreach.CommitsTotal = total
	}

	stash, stashErr := git.StashList(ctx, r.gitBreach.Root)
	r.gitBreach.Stash = stash
	r.gitBreach.StashErr = stashErr

	r.renderGitBreach()
}

func (r *Root) closeGitBreach() {
	r.cancelGitBreachDiff()
	r.hideOverlay()
}

// gitBreachRowKind is which of Files' own four sections a gitBreachRow
// belongs to — the grouping the mockup's own "Staged (n)"/"Unstaged
// (n)" headers render, and what decides whether Space stages or
// unstages the row under the cursor (see toggleGitBreachStage).
type gitBreachRowKind int

const (
	gitBreachRowStaged gitBreachRowKind = iota
	gitBreachRowUnstaged
	gitBreachRowUntracked
	gitBreachRowConflict
)

// gitBreachRow is one row of the Files table — path/origPath/code
// mirror git.FileChange exactly for a Staged/Unstaged row; origPath/
// code are simply zero for Untracked/Conflict, which carry only a
// path (see git.Status.Untracked/Conflicts, both plain []string).
type gitBreachRow struct {
	kind     gitBreachRowKind
	path     string
	origPath string
	code     byte
}

// buildGitBreachRows flattens a git.Status into the Files table's own
// row order: Staged, then Unstaged, then Untracked, then Conflicts —
// matching the mockup's own section order. A path staged and unstaged
// at once (git's own "MM") appears once in each section, exactly as
// git.Status.Staged/Unstaged themselves already carry it twice (see
// their own doc comment in internal/git/status.go) — Files shows both,
// it doesn't collapse them into one ambiguous row.
func buildGitBreachRows(st git.Status) []gitBreachRow {
	var rows []gitBreachRow
	for _, c := range st.Staged {
		rows = append(rows, gitBreachRow{kind: gitBreachRowStaged, path: c.Path, origPath: c.OrigPath, code: c.Code})
	}
	for _, c := range st.Unstaged {
		rows = append(rows, gitBreachRow{kind: gitBreachRowUnstaged, path: c.Path, origPath: c.OrigPath, code: c.Code})
	}
	for _, p := range st.Conflicts {
		rows = append(rows, gitBreachRow{kind: gitBreachRowConflict, path: p})
	}
	for _, p := range st.Untracked {
		rows = append(rows, gitBreachRow{kind: gitBreachRowUntracked, path: p})
	}
	return rows
}

// toggleGitBreachStage is Space on the Files table — stages an
// Unstaged/Untracked row, unstages a Staged one, the same "Space
// toggles this row's own membership" convention the main panel's own
// checkbox column already establishes (see panel.go's own ' '
// handling), applied here to the index instead of the clipboard. A
// Conflict row does nothing yet — plain stage/unstage semantics on an
// unmerged path would just as likely hide a real conflict as resolve
// it; conflict resolution is explicitly out of scope for this first
// Ausbaustufe (see feature_ideas.txt's own #21).
func (r *Root) toggleGitBreachStage() {
	row, _ := r.gitBreach.FilesTable.GetSelection()
	gr, ok := r.gitBreachRowAt(row)
	if !ok || gr.kind == gitBreachRowConflict {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()

	var err error
	if gr.kind == gitBreachRowStaged {
		err = git.Unstage(ctx, r.gitBreach.Root, []string{gr.path})
	} else {
		err = git.Stage(ctx, r.gitBreach.Root, []string{gr.path})
	}
	if err != nil {
		r.showError(fmt.Errorf("git breach: %w", err))
		return
	}
	r.reloadGitBreach()
}

// gitBreachRowAt returns the gitBreachRow the Files table's own row
// currently shows — unlike most of this app's own table-row-to-data
// mappings (a fixed header offset, see e.g. logAuditGroupAt), this one
// goes through gitBreach.RowIndex (see renderGitBreachFiles) because the
// table also contains non-selectable section header rows interspersed
// with real file rows; row lands on one of those (or past the end)
// reports false, same as any other out-of-range row would.
func (r *Root) gitBreachRowAt(row int) (gitBreachRow, bool) {
	idx, ok := r.gitBreach.RowIndex[row]
	if !ok || idx < 0 || idx >= len(r.gitBreach.Rows) {
		return gitBreachRow{}, false
	}
	return r.gitBreach.Rows[idx], true
}

// openGitBreachCommitPrompt is "c" — a commit-message input pushed on
// top of the dashboard (pushOverlay, not showOverlay/openPrompt: the
// generic openPrompt closes every open overlay first via
// showOverlay's own closeAllOverlays, which would dismiss the
// dashboard itself rather than layer on top of it, the same "layer by
// layer" shape the Log Audit viewer's own detail modal already uses).
// Does nothing if nothing is actually staged — committing an empty
// index would just surface git's own "nothing to commit" error for no
// reason the user didn't already know about from the Files box itself.
func (r *Root) openGitBreachCommitPrompt() {
	if len(r.gitBreach.Status.Staged) == 0 {
		r.showError(fmt.Errorf("git breach: nothing staged to commit"))
		return
	}

	r.prompt.SetLabel("Commit message ")
	r.prompt.SetText("")
	r.promptSubmit = func(text string) {
		if text == "" {
			return
		}
		r.commitGitBreach(text)
	}

	const width, height = 60, 1
	_, _, screenWidth, screenHeight := r.GetRect()
	x, y, w, h := r.clampToScreen((screenWidth-width)/2, (screenHeight-height)/2, width, height)
	r.prompt.SetRect(x, y, w, h)
	r.pushOverlay(promptPage, r.prompt, nil)
}

// commitGitBreach runs the actual commit and reloads — errors (most
// commonly git's own pre-commit hook rejecting the message, or nothing
// staged if the state changed out from under the user between opening
// the prompt and submitting it) surface the same way every other
// failed action in this app does, via showError.
func (r *Root) commitGitBreach(message string) {
	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	if err := git.Commit(ctx, r.gitBreach.Root, message); err != nil {
		r.showError(fmt.Errorf("git breach: %w", err))
		return
	}
	r.reloadGitBreach()
}

// openGitBreachCheckout is Enter on the Branches table — checks out
// the branch under the cursor, confirming first if the working tree
// isn't clean. git itself already refuses a checkout outright when it
// would actually overwrite a modified file, but stays silent (and
// succeeds) whenever the target branch's own version of every modified
// file happens not to conflict — quietly carrying uncommitted changes
// onto a different branch than whoever made them was looking at. A
// clean tree (the common case) checks out immediately with no prompt,
// the same "don't ask what there's nothing to lose" restraint
// openRemoveConfirm's own TrashConfirm setting already applies
// elsewhere — this one isn't user-configurable, since unlike moving
// something to Trash, a clean-tree checkout has no irreversible side
// effect to weigh at all.
func (r *Root) openGitBreachCheckout() {
	row, _ := r.gitBreach.BranchesTable.GetSelection()
	branch, ok := r.gitBreachBranchAt(row)
	if !ok || branch.Current {
		return
	}

	// Untracked files deliberately don't trigger this: they're not part
	// of any branch's own content, so a checkout never silently carries
	// one across the way a tracked, uncommitted change could — git
	// itself still refuses loudly (surfaced via checkoutGitBreachBranch's
	// own showError) in the one case an untracked file actually would be
	// overwritten.
	st := r.gitBreach.Status
	if len(st.Staged) > 0 || len(st.Unstaged) > 0 || len(st.Conflicts) > 0 {
		r.openConfirm(
			fmt.Sprintf("Check out %q with uncommitted changes in the working tree?", branch.Name),
			"Yes, check out",
			func() { r.checkoutGitBreachBranch(branch.Name) },
		)
		return
	}
	r.checkoutGitBreachBranch(branch.Name)
}

// checkoutGitBreachBranch runs the actual checkout and reloads — both
// this dashboard's own state (the new branch's own status/Files/
// Branches) and the active panel underneath it, since a checkout can
// change which files exist on disk in the exact directory the panel
// is already showing; leaving its own listing stale after a checkout
// would be exactly the kind of silent staleness this project's own
// "reflect the real, current state" convention exists to avoid.
func (r *Root) checkoutGitBreachBranch(branch string) {
	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	if err := git.Checkout(ctx, r.gitBreach.Root, branch); err != nil {
		r.showError(fmt.Errorf("git breach: %w", err))
		return
	}
	r.reloadGitBreach()
	r.showError(r.panel.load(r.panel.path))
}

// openGitBreachDeleteBranch is "d" on the Branches table — always
// confirms first, unlike checkout's own "only ask if there's actually
// something to lose": deleting a branch is itself the irreversible
// action (the ref disappears outright; a merged branch's own commits
// stay reachable through whatever merged them, but the branch name
// itself is gone either way), not something that's only risky in some
// states the way an uncommitted-changes checkout is. Refuses outright
// on the current branch before ever calling git.DeleteBranch at all,
// matching openGitBreachCheckout's own established "a no-op, not an
// error" convention for the one case where the action makes no sense,
// rather than letting git's own real but differently-worded refusal
// ("used by worktree") surface instead.
func (r *Root) openGitBreachDeleteBranch() {
	row, _ := r.gitBreach.BranchesTable.GetSelection()
	branch, ok := r.gitBreachBranchAt(row)
	if !ok || branch.Current {
		return
	}
	r.openConfirm(
		fmt.Sprintf("Delete branch %q?", branch.Name),
		"Yes, delete",
		func() { r.deleteGitBreachBranch(branch.Name) },
	)
}

// deleteGitBreachBranch runs the actual delete and reloads — only
// Branches' own state changes here (unlike checkoutGitBreachBranch,
// nothing on disk in the active panel's own directory can change from
// deleting a ref that wasn't checked out to begin with), but
// reloadGitBreach is still the right call rather than hand-patching
// gitBreach.Branches: the same "re-run the real read, don't just
// redraw" contract every other git breach action already follows.
// git branch -d's own refusal on an unmerged branch (see
// git.DeleteBranch's own doc comment) surfaces as-is via showError,
// exactly the same way checkoutGitBreachBranch already surfaces git's
// own refusals rather than silently swallowing them.
func (r *Root) deleteGitBreachBranch(branch string) {
	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	if err := git.DeleteBranch(ctx, r.gitBreach.Root, branch); err != nil {
		r.showError(fmt.Errorf("git breach: %w", err))
		return
	}
	r.reloadGitBreach()
}

// gitBreachBranchAt returns gitBreach.Branches[row] — unlike Files'
// own gitBreachRowAt, the Branches table has no section-header rows
// interspersed (see renderGitBreachBranches), so the table row index
// maps directly onto gitBreach.Branches without an indirection map.
func (r *Root) gitBreachBranchAt(row int) (git.Branch, bool) {
	// renderGitBreachBranches puts a non-selectable "(detached at ...)"
	// row at row 0 first whenever gitBreach.Status.Detached is true,
	// shifting every real branch down by one — accounted for here so
	// gitBreachAt's own caller never has to know this box's own
	// row-0 special case exists at all.
	if r.gitBreach.Status.Detached {
		row--
	}
	if row < 0 || row >= len(r.gitBreach.Branches) {
		return git.Branch{}, false
	}
	return r.gitBreach.Branches[row], true
}

// gitBreachTagAt returns gitBreach.Tags[row] — no detached-HEAD row-0
// special case to account for here the way gitBreachBranchAt has:
// a tag is never "current" the way a branch is, so renderGitBreachTags
// never inserts a non-selectable row ahead of the real ones.
func (r *Root) gitBreachTagAt(row int) (git.Tag, bool) {
	if row < 0 || row >= len(r.gitBreach.Tags) {
		return git.Tag{}, false
	}
	return r.gitBreach.Tags[row], true
}

// toggleGitBreachBranchesTab switches the Branches box between its
// Branches/Tags sub-tabs (Left/Right while the box has focus — see
// captureGitBreachBranchesTableKey) and re-renders in place, landing
// on the first row of whichever table just became visible rather than
// keeping a selection index that belonged to the other one.
func (r *Root) toggleGitBreachBranchesTab() {
	r.gitBreach.BranchesShowTags = !r.gitBreach.BranchesShowTags
	r.renderGitBreachBranches()
	r.renderGitBreachBranchesHeader()
}

// openGitBreachCreateTag prompts for a name and creates a new,
// lightweight tag at HEAD — see git.CreateTag's own doc comment on
// why never annotated. Only reachable while the Tags sub-tab is
// showing (see captureGitBreachBranchesTableKey); nothing here needs
// a row under the cursor, unlike checkout/delete.
func (r *Root) openGitBreachCreateTag() {
	r.openPrompt("New tag name:", "", func(name string) {
		ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
		defer cancel()
		if err := git.CreateTag(ctx, r.gitBreach.Root, name, "HEAD"); err != nil {
			r.showError(fmt.Errorf("git breach: %w", err))
			return
		}
		r.reloadGitBreach()
	})
}

// openGitBreachDeleteTag confirms, then deletes the tag under the
// cursor — always confirms, the same reasoning deleteGitBreachBranch's
// own sibling "d" already has: deleting a tag is itself the
// irreversible action, with no "unmerged" concept for git to safely
// refuse on the way a branch delete sometimes does.
func (r *Root) openGitBreachDeleteTag() {
	row, _ := r.gitBreach.BranchesTable.GetSelection()
	tg, ok := r.gitBreachTagAt(row)
	if !ok {
		return
	}
	r.openConfirm(
		fmt.Sprintf("Delete tag %q?", tg.Name),
		"Yes, delete",
		func() { r.deleteGitBreachTag(tg.Name) },
	)
}

// deleteGitBreachTag runs the actual delete and reloads — the same
// "re-run the real read, don't hand-patch gitBreach.Tags" contract
// deleteGitBreachBranch already follows for its own box.
func (r *Root) deleteGitBreachTag(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	if err := git.DeleteTag(ctx, r.gitBreach.Root, name); err != nil {
		r.showError(fmt.Errorf("git breach: %w", err))
		return
	}
	r.reloadGitBreach()
}

// gitBreachDiffDebounce/gitBreachDiffTimeout mirror
// detailsGitStatusDebounce/detailsGitStatusTimeout's own reasoning:
// holding an arrow key through a long Files list should cost nothing,
// so a diff is only fetched once the cursor rests briefly, cancelled
// outright the instant it moves on again.
const (
	gitBreachDiffDebounce = 120 * time.Millisecond
	gitBreachDiffTimeout  = 2 * time.Second
)

// cancelGitBreachDiff stops whichever diff fetch is in flight for a row
// the cursor has since moved off — mirrors cancelDetailsGitStatus.
func (r *Root) cancelGitBreachDiff() {
	if r.gitBreach.DiffCancel != nil {
		r.gitBreach.DiffCancel()
		r.gitBreach.DiffCancel = nil
	}
}

// startGitBreachDiff fetches row's own diff (or, for an untracked row,
// its raw content — see git.UntrackedContent) in the background and
// shows it once ready, if the cursor is still on that same row — same
// shape as startDetailsGitStatus (debounce, cancellable context,
// safeGo, QueueUpdateDraw, a final check against the still-current
// selection since the cursor can have moved again by the time this
// runs), for the identical reason: triggered by mere cursor movement,
// not a deliberate keypress, and git diff can occasionally be slow
// against a huge changeset.
// gitBreachSaveDiffScroll remembers Main's own current scroll row
// under whatever key is currently active (gitBreach.DiffScrollKey) —
// called at the very start of every startGitBreach*Diff, before
// anything about the row being left behind changes, so it always
// captures the scroll position of whatever content is genuinely still
// on screen at that moment rather than content a slow previous fetch
// hasn't replaced yet.
func (r *Root) gitBreachSaveDiffScroll() {
	if r.gitBreach.DiffScrollKey == "" {
		return
	}
	if r.gitBreach.DiffScroll == nil {
		r.gitBreach.DiffScroll = make(map[string]int)
	}
	row, _ := r.gitBreach.DiffView.GetScrollOffset()
	r.gitBreach.DiffScroll[r.gitBreach.DiffScrollKey] = row
}

// gitBreachActivateDiffKey saves the outgoing key's own scroll
// position (see gitBreachSaveDiffScroll), switches gitBreach.DiffScrollKey
// to key, and restores whatever was last remembered for it —
// synchronously, before the row's own real content has even started
// fetching (see startGitBreachDiff and its two siblings' own debounce).
// Scrolling into momentarily-stale content this way is harmless: it's
// about to be replaced, and gitBreachSetDiffText re-applies the same
// restore once the real content lands anyway, the final word on where
// the view ends up. Without this earlier synchronous half, a quick
// glance within the debounce window would show the previous row's own
// scroll position still in effect for a beat, not the newly selected
// row's remembered one.
func (r *Root) gitBreachActivateDiffKey(key string) {
	r.gitBreachSaveDiffScroll()
	r.gitBreach.DiffScrollKey = key
	r.gitBreach.DiffView.ScrollTo(r.gitBreach.DiffScroll[key], 0)
}

// gitBreachSetDiffText sets Main's own text and restores whatever
// scroll position gitBreachSaveDiffScroll last remembered for key —
// 0 (the top) for a file/commit/stash entry seen for the first time
// this session. key becomes the new gitBreach.DiffScrollKey, so the
// next save (on whatever row comes after this one) attributes to the
// right entry regardless of how long this fetch took.
//
// rawDiff is the real unified-diff text text was colorized from (see
// gitBreachColorizeDiff) — "" for anything that isn't actually a
// diff (an untracked file's own raw content, an unresolved conflict's
// own notice, an error, "no diff to show") — kept so the split view
// (see comparediffsplit.go's own gitBreachRenderDiffSplit) can be
// re-rendered from it on toggle without a second git fetch, and so the
// split view re-renders immediately alongside Main whenever this
// function runs while split mode is already active, rather than
// showing stale content from whatever was selected before.
// gitBreachClearDiff empties Main — both the unified view and the
// split view, if that's currently showing — and forgets the raw diff
// text behind it. The shared "nothing selected" reset all three
// startGitBreach*Diff functions open with, since none of their own
// gitBreachSetDiffText calls below ever run at all in that case.
func (r *Root) gitBreachClearDiff() {
	r.gitBreach.DiffView.SetText("")
	r.gitBreach.DiffRawText = ""
	if r.gitBreach.DiffSplit {
		r.gitBreachRenderDiffSplit()
	}
}

func (r *Root) gitBreachSetDiffText(key, text, rawDiff string) {
	r.gitBreach.DiffScrollKey = key
	r.gitBreach.DiffView.SetText(text)
	r.gitBreach.DiffView.ScrollTo(r.gitBreach.DiffScroll[key], 0)
	r.gitBreach.DiffRawText = rawDiff
	if r.gitBreach.DiffSplit {
		r.gitBreachRenderDiffSplit()
		r.gitBreach.DiffSplitLeft.ScrollTo(r.gitBreach.DiffScroll[key], 0)
		r.gitBreach.DiffSplitRight.ScrollTo(r.gitBreach.DiffScroll[key], 0)
	}
}

func (r *Root) startGitBreachDiff(row int) {
	r.cancelGitBreachDiff()
	gr, ok := r.gitBreachRowAt(row)
	if !ok {
		r.gitBreachSaveDiffScroll()
		r.gitBreach.DiffScrollKey = ""
		r.gitBreachClearDiff()
		return
	}
	key := gitBreachFileDiffScrollKey(gr)
	r.gitBreachActivateDiffKey(key)

	if gr.kind == gitBreachRowConflict {
		r.gitBreachSetDiffText(key, tview.Escape(fmt.Sprintf("%s: unresolved conflict — resolving conflicts is not yet supported here.", gr.path)), "")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.gitBreach.DiffCancel = cancel
	root := r.gitBreach.Root

	r.safeGo("Git breach diff", r.cancelGitBreachDiff, func() {
		select {
		case <-ctx.Done():
			return // the cursor moved on before this even started
		case <-time.After(gitBreachDiffDebounce):
		}

		var text string
		var err error
		fetchCtx, fetchCancel := context.WithTimeout(ctx, gitBreachDiffTimeout)
		switch gr.kind {
		case gitBreachRowUntracked:
			text, err = git.UntrackedContent(root, gr.path)
			if err == nil && text == "" {
				text = fmt.Sprintf("%s: untracked, empty file.", gr.path)
			}
		default:
			text, err = git.Diff(fetchCtx, root, gr.path, gr.kind == gitBreachRowStaged)
			if err == nil && text == "" {
				text = fmt.Sprintf("%s: no diff to show.", gr.path)
			}
		}
		fetchCancel()
		if ctx.Err() != nil {
			return
		}

		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			curRow, _ := r.gitBreach.FilesTable.GetSelection()
			curGr, ok := r.gitBreachRowAt(curRow)
			if !ok || curGr != gr {
				return // the cursor moved to a different row before this landed
			}
			if err != nil {
				r.gitBreachSetDiffText(key, tview.Escape(fmt.Sprintf("%s: %v", gr.path, err)), "")
				return
			}
			if gr.kind == gitBreachRowUntracked {
				// Raw file content, not a diff — gitBreachColorizeDiff's
				// own +/- line coloring doesn't apply here at all (an
				// untracked file has no such lines), but the user's own
				// explicit follow-up request was that it shouldn't just
				// be plain white text either: real syntax highlighting,
				// the same internal/viewer.Highlight + renderSyntax
				// pipeline Look already uses (see viewer.go's own
				// showBuiltinLook) — auto-detects the language from
				// gr.path's own name, the same "don't ask the user,
				// figure it out" convention Look's own Highlight call
				// already establishes. Chroma's own dedicated Diff lexer
				// was tried for the diff case above and rejected (see
				// gitBreachColorizeDiff's own doc comment) — this is a
				// real source file, not a diff, so that rejection
				// doesn't apply here.
				r.gitBreachSetDiffText(key, renderSyntax(viewer.Highlight(gr.path, text), paletteFor(r.theme.SurfaceBackground)), "")
				return
			}
			r.gitBreachSetDiffText(key, gitBreachColorizeDiff(text, gr.path, r.theme), text)
		})
	})
}

// gitBreachFileDiffScrollKey identifies gr for gitBreach.DiffScroll's
// own per-entry memory — kind, not just path: the same path can appear
// twice at once (staged and unstaged), showing two different diffs
// that need their own separate remembered scroll position.
func gitBreachFileDiffScrollKey(gr gitBreachRow) string {
	return fmt.Sprintf("F:%d:%s", gr.kind, gr.path)
}

// startGitBreachBlame is "b" on the Files table — fetches row's own
// per-line commit history via git.Blame and shows it in Main, same
// shape as startGitBreachDiff (cancellable context, safeGo,
// QueueUpdateDraw, a final check against the still-current selection)
// minus the debounce: a deliberate keypress, not cursor-movement
// noise, so there's no "did the cursor only pass through" case to
// wait out. Its own scroll key ("B:", as opposed to diff's "F:") keeps
// blame's remembered scroll position separate from the same row's own
// diff. Moving the cursor afterward hands Main straight back to the
// ordinary diff view via Files' own SetSelectionChangedFunc
// (startGitBreachFilesDiffIfOwner) — blame is a one-shot look, not a
// persistent mode alongside diff.
func (r *Root) startGitBreachBlame() {
	row, _ := r.gitBreach.FilesTable.GetSelection()
	gr, ok := r.gitBreachRowAt(row)
	if !ok {
		return
	}
	r.cancelGitBreachDiff()

	key := "B:" + gr.path
	r.gitBreachActivateDiffKey(key)

	// Neither has a real blame to show: a conflict is unmerged (no
	// single HEAD-relative history to attribute lines to without
	// resolving it first — explicitly out of scope, same reasoning as
	// toggleGitBreachStage's own conflict exclusion), an untracked file
	// was never added to the index, so git has no history for it at
	// all yet.
	switch gr.kind {
	case gitBreachRowConflict:
		r.gitBreachSetDiffText(key, tview.Escape(fmt.Sprintf("%s: unresolved conflict — blame isn't meaningful on an unmerged path.", gr.path)), "")
		return
	case gitBreachRowUntracked:
		r.gitBreachSetDiffText(key, tview.Escape(fmt.Sprintf("%s: untracked — nothing to blame yet.", gr.path)), "")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.gitBreach.DiffCancel = cancel
	root := r.gitBreach.Root

	r.safeGo("Git breach blame", r.cancelGitBreachDiff, func() {
		fetchCtx, fetchCancel := context.WithTimeout(ctx, gitBreachFetchTimeout)
		lines, err := git.Blame(fetchCtx, root, gr.path)
		fetchCancel()
		if ctx.Err() != nil {
			return
		}

		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			curRow, _ := r.gitBreach.FilesTable.GetSelection()
			curGr, ok := r.gitBreachRowAt(curRow)
			if !ok || curGr != gr {
				return // the cursor moved to a different row before this landed
			}
			if err != nil {
				r.gitBreachSetDiffText(key, tview.Escape(fmt.Sprintf("%s: %v", gr.path, err)), "")
				return
			}
			r.gitBreachSetDiffText(key, gitBreachRenderBlame(lines), "")
		})
	})
}

// gitBreachRenderBlame formats lines for Main — each one prefixed with
// its short hash, the commit's own date and author (colored the same
// way the Commits box already colors its own rows, see
// gitBreachAuthorColor, so a line's author reads consistently whether
// seen in Commits or here), then the line's own content verbatim.
// Author names are padded to the widest one actually present, not a
// fixed guess, so the content column lines up regardless of how long
// names in this particular repository happen to be.
func gitBreachRenderBlame(lines []git.BlameLine) string {
	widestAuthor := 0
	for _, l := range lines {
		if n := len([]rune(l.AuthorName)); n > widestAuthor {
			widestAuthor = n
		}
	}
	var b strings.Builder
	for _, l := range lines {
		author := l.AuthorName
		if pad := widestAuthor - len([]rune(author)); pad > 0 {
			author += strings.Repeat(" ", pad)
		}
		prefix := fmt.Sprintf("%-7s %s %s  ", l.Short, l.When.Format("2006-01-02"), author)
		b.WriteString(wrapColor(gitBreachAuthorColor(l.AuthorEmail), tview.Escape(prefix)))
		b.WriteString(tview.Escape(l.Content))
		b.WriteString("\n")
	}
	return b.String()
}

// gitBreachCommitAt returns gitBreach.Commits[row] — the Commits table
// has no non-selectable header rows interspersed (same as Branches, see
// gitBreachBranchAt's own doc comment), so the table row index maps
// directly onto gitBreach.Commits without an indirection map.
func (r *Root) gitBreachCommitAt(row int) (git.LogEntry, bool) {
	if row < 0 || row >= len(r.gitBreach.Commits) {
		return git.LogEntry{}, false
	}
	return r.gitBreach.Commits[row], true
}

// startGitBreachCommitDiff mirrors startGitBreachDiff exactly (debounce,
// cancellable context, safeGo, QueueUpdateDraw, a final check against
// the still-current selection) for a commit's own diff instead of a
// file's — git.CommitDiff's own single call already covers every file
// the commit touched in one combined diff, which gitBreachColorizeDiff
// now tracks per file via its own "diff --git" header parsing (see its
// own doc comment) rather than lexing the whole thing as one language.
func (r *Root) startGitBreachCommitDiff(row int) {
	r.cancelGitBreachDiff()
	c, ok := r.gitBreachCommitAt(row)
	if !ok {
		r.gitBreachSaveDiffScroll()
		r.gitBreach.DiffScrollKey = ""
		r.gitBreachClearDiff()
		return
	}
	key := "C:" + c.Hash
	r.gitBreachActivateDiffKey(key)

	ctx, cancel := context.WithCancel(context.Background())
	r.gitBreach.DiffCancel = cancel
	root := r.gitBreach.Root

	r.safeGo("Git breach commit diff", r.cancelGitBreachDiff, func() {
		select {
		case <-ctx.Done():
			return // the cursor moved on before this even started
		case <-time.After(gitBreachDiffDebounce):
		}

		fetchCtx, fetchCancel := context.WithTimeout(ctx, gitBreachDiffTimeout)
		text, err := git.CommitDiff(fetchCtx, root, c.Hash)
		fetchCancel()
		if ctx.Err() != nil {
			return
		}
		if err == nil && text == "" {
			text = fmt.Sprintf("%s: no diff to show.", c.Short)
		}

		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			curRow, _ := r.gitBreach.CommitsTable.GetSelection()
			curC, ok := r.gitBreachCommitAt(curRow)
			if !ok || curC.Hash != c.Hash {
				return // the cursor moved to a different row before this landed
			}
			if err != nil {
				r.gitBreachSetDiffText(key, tview.Escape(fmt.Sprintf("%s: %v", c.Short, err)), "")
				return
			}
			r.gitBreachSetDiffText(key, gitBreachColorizeDiff(text, "", r.theme), text)
		})
	})
}

// gitBreachStashAt returns gitBreach.Stash[row] — mirrors
// gitBreachCommitAt exactly (no non-selectable header rows in the
// Stash table either).
func (r *Root) gitBreachStashAt(row int) (git.Stash, bool) {
	if row < 0 || row >= len(r.gitBreach.Stash) {
		return git.Stash{}, false
	}
	return r.gitBreach.Stash[row], true
}

// startGitBreachStashDiff mirrors startGitBreachCommitDiff exactly, for
// a stash entry's own diff (git.StashDiff) instead of a commit's.
func (r *Root) startGitBreachStashDiff(row int) {
	r.cancelGitBreachDiff()
	s, ok := r.gitBreachStashAt(row)
	if !ok {
		r.gitBreachSaveDiffScroll()
		r.gitBreach.DiffScrollKey = ""
		r.gitBreachClearDiff()
		return
	}
	key := "S:" + s.Ref
	r.gitBreachActivateDiffKey(key)

	ctx, cancel := context.WithCancel(context.Background())
	r.gitBreach.DiffCancel = cancel
	root := r.gitBreach.Root

	r.safeGo("Git breach stash diff", r.cancelGitBreachDiff, func() {
		select {
		case <-ctx.Done():
			return // the cursor moved on before this even started
		case <-time.After(gitBreachDiffDebounce):
		}

		fetchCtx, fetchCancel := context.WithTimeout(ctx, gitBreachDiffTimeout)
		text, err := git.StashDiff(fetchCtx, root, s.Ref)
		fetchCancel()
		if ctx.Err() != nil {
			return
		}
		if err == nil && text == "" {
			text = fmt.Sprintf("%s: no diff to show.", s.Ref)
		}

		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			curRow, _ := r.gitBreach.StashTable.GetSelection()
			curS, ok := r.gitBreachStashAt(curRow)
			if !ok || curS != s {
				return // the cursor moved to a different row before this landed
			}
			if err != nil {
				r.gitBreachSetDiffText(key, tview.Escape(fmt.Sprintf("%s: %v", s.Ref, err)), "")
				return
			}
			r.gitBreachSetDiffText(key, gitBreachColorizeDiff(text, "", r.theme), text)
		})
	})
}

// maybeLoadMoreGitBreachCommits is Commits' own SetSelectionChangedFunc
// tail — the user's own explicit request: reaching the bottom of what's
// currently loaded should fetch another git.CommitLogLimit worth
// rather than the list just silently stopping. Two guards, in order:
// len(r.gitBreach.Commits) != r.gitBreach.CommitsLimit means Log already
// returned fewer than it was asked for last time, i.e. this already is
// every commit the repository has — nothing more exists to load,
// regardless of which row the cursor is on. row is only "the last
// loaded row", not merely "a low row number", so moving the cursor
// around below the true end (a short list) never spuriously refetches.
//
// Deliberately synchronous, like the rest of reloadGitBreach: `git log`
// against a larger limit is still fast purely locally, and a second,
// separate async-fetch surface here would duplicate
// startGitBreachCommitDiff's own cancellation machinery for a case that
// doesn't actually need it.
//
// On failure, shows the error via showError and leaves
// gitBreach.Commits/gitBreach.CommitsLimit untouched rather than
// replacing an already-successfully-loaded list with an error
// placeholder — a transient git failure while paging for more
// shouldn't cost the user everything already on screen.
//
// Never re-triggers itself: renderGitBreachCommits' own tail only
// calls Select() when the current row is no longer valid (see its own
// doc comment on gitBreach.CommitsReady) — row itself doesn't change
// here, so tview's own Select() never fires SetSelectionChangedFunc a
// second time (it only fires on an actual value change), and this
// function never recurses.
func (r *Root) maybeLoadMoreGitBreachCommits(row int) {
	if len(r.gitBreach.Commits) == 0 || len(r.gitBreach.Commits) != r.gitBreach.CommitsLimit {
		return
	}
	if row != len(r.gitBreach.Commits)-1 {
		return
	}

	newLimit := r.gitBreach.CommitsLimit + git.CommitLogLimit
	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	commits, err := git.Log(ctx, r.gitBreach.Root, newLimit)
	if err != nil {
		r.showError(fmt.Errorf("git breach: %w", err))
		return
	}
	r.gitBreach.CommitsLimit = newLimit
	r.gitBreach.Commits = commits
	r.renderGitBreachCommits()
}

// openGitBreachStashApply is "a" on the Stash table — restores the
// entry under the cursor onto the working tree, confirming first only
// when there's actually something uncommitted to conflict with, the
// same "only ask if there's something to lose" restraint
// openGitBreachCheckout's own dirty-tree branch already establishes.
// Deliberately apply, never pop — see git.StashApply's own doc comment
// for why: pop can leave the stash behind in a conflicted state Git
// breach has no UI to resolve, while apply always keeps the stash
// either way, recoverable by hand regardless of how the apply itself
// goes. Dropping the entry afterward, if desired, is "d" (see
// openGitBreachStashDrop), a separate, explicit action — never implicit
// here.
func (r *Root) openGitBreachStashApply() {
	row, _ := r.gitBreach.StashTable.GetSelection()
	s, ok := r.gitBreachStashAt(row)
	if !ok {
		return
	}

	st := r.gitBreach.Status
	if len(st.Staged) > 0 || len(st.Unstaged) > 0 || len(st.Conflicts) > 0 {
		r.openConfirm(
			fmt.Sprintf("Apply stash %q with uncommitted changes in the working tree?", s.Message),
			"Yes, apply",
			func() { r.applyGitBreachStash(s.Ref) },
		)
		return
	}
	r.applyGitBreachStash(s.Ref)
}

// applyGitBreachStash runs the actual apply and reloads — both this
// dashboard's own state (Files' own now-possibly-changed status) and
// the active panel underneath it, the same reasoning
// checkoutGitBreachBranch's own doc comment already gives: an apply can
// change which files exist on disk in the exact directory the panel is
// already showing.
func (r *Root) applyGitBreachStash(ref string) {
	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	if err := git.StashApply(ctx, r.gitBreach.Root, ref); err != nil {
		r.showError(fmt.Errorf("git breach: %w", err))
		return
	}
	r.reloadGitBreach()
	r.showError(r.panel.load(r.panel.path))
}

// openGitBreachStashDrop is "d" on the Stash table — always confirms
// first, unlike Apply: dropping a stash is itself the irreversible
// action (the stashed changes are simply gone), not something that's
// only risky in some working-tree states, the same distinction
// openGitBreachDeleteBranch's own doc comment draws for Branches.
// Scoped to the Stash table's own key capture, same as Branches' own
// "d" is scoped to its own table — the two never collide.
func (r *Root) openGitBreachStashDrop() {
	row, _ := r.gitBreach.StashTable.GetSelection()
	s, ok := r.gitBreachStashAt(row)
	if !ok {
		return
	}
	r.openConfirm(
		fmt.Sprintf("Drop stash %q?", s.Message),
		"Yes, drop",
		func() { r.dropGitBreachStash(s.Ref) },
	)
}

// dropGitBreachStash runs the actual drop and reloads Git breach's own
// state — nothing on disk in the active panel's own directory can
// change from dropping a stash entry that was never applied to begin
// with, but reloadGitBreach is still the right call rather than
// hand-patching gitBreach.Stash: the same "re-run the real read, don't
// just redraw" contract every other Git breach action already follows,
// and the one safe way to get every later entry's own ref correctly
// renumbered (stash@{N} is positional — dropping stash@{0} shifts every
// entry below it down by one).
func (r *Root) dropGitBreachStash(ref string) {
	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	if err := git.StashDrop(ctx, r.gitBreach.Root, ref); err != nil {
		r.showError(fmt.Errorf("git breach: %w", err))
		return
	}
	r.reloadGitBreach()
}
