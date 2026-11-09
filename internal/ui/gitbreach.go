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

// gitBreachBranchesTab is which of the Branches box's own sub-tabs is
// currently showing — see gitBreachState.BranchesTab's own doc
// comment. iota order is also tab order: Left/Right cycle backward/
// forward through it, wrapping at either end (see
// cycleGitBreachBranchesTab).
type gitBreachBranchesTab int

const (
	gitBreachBranchesTabBranches gitBreachBranchesTab = iota
	gitBreachBranchesTabTags
	gitBreachBranchesTabRemotes
)

// gitBreachBranchesTabSpan is one sub-tab label's own clickable region
// in BranchesHeader's rendered text — [start, end) is its half-open
// column range, relative to the header's own inner rect, the same
// shape tabStripSpan already establishes for the panel's own tab
// strip.
type gitBreachBranchesTabSpan struct {
	start, end int
	tab        gitBreachBranchesTab
}

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
	// FilesTab switches the Files box between its own sub-tabs —
	// see gitBreachFilesTab's own doc comment. Reset to
	// gitBreachFilesTabChanges on every fresh "jg" open, same as
	// BranchesTab.
	FilesTab      gitBreachFilesTab
	FilesTabSpans []gitBreachFilesTabSpan
	Branches      []git.Branch
	BranchesErr   error
	BranchesReady bool
	// BranchesTab switches the Branches box between its own sub-tabs —
	// Branches/Tags/Remotes, see renderGitBreachBranchesHeader — one
	// box, one table, several row kinds, the same "no new box" shape
	// feature_ideas.txt's own Box-Gruppierung plan calls for rather
	// than Remotes becoming a fifth box of its own. Reset to
	// gitBreachBranchesTabBranches on every fresh "jg" open (see
	// openGitBreach), same as CommitsLimit.
	BranchesTab  gitBreachBranchesTab
	Tags         []git.Tag
	TagsErr      error
	Remotes      []git.Remote
	RemotesErr   error
	Worktrees    []git.Worktree
	WorktreesErr error
	// BranchesTabSpans locates each sub-tab label's own clickable
	// column range in BranchesHeader's current text — recorded as
	// renderGitBreachBranchesHeader builds that text, the same
	// "record spans while building, don't re-measure the string
	// afterwards" shape tabStripSpan already establishes (see its own
	// doc comment on why: this avoids ever having to re-derive column
	// positions from rendered text whose own format could change).
	BranchesTabSpans []gitBreachBranchesTabSpan
	// FilesFilter/BranchesFilter/TagsFilter/StashFilter are each
	// box's own "/" filter — a plain, case-insensitive substring (see
	// gitBreachFilterMatches), deliberately not the panel's own
	// glob/regex filter contract, per Jens's own explicit "keep it
	// simple" (2026-11-08). Commits deliberately has none: its own
	// lane/branch graph (git.CommitLanes) is computed over the whole,
	// unfiltered list — filtering rows out of an already-drawn graph
	// would either break lane continuity or need the graph itself
	// recomputed around gaps, real complexity for a "keep it simple"
	// feature; noted in feature_ideas.txt as a separate, later step.
	// BranchesShown/TagsShown/StashShown are each box's own filtered
	// view, rebuilt by its own render function — gitBreachBranchAt/
	// gitBreachTagAt/gitBreachStashAt read these, not the raw
	// Branches/Tags/Stash slices, so a table row always resolves to
	// the same entry the filter actually shows at that position.
	// Files needs no equivalent: gitBreachRowAt already goes through
	// RowIndex, which renderGitBreachFiles only ever populates for
	// rows it actually drew — a filtered-out row simply never gets an
	// entry, no second slice needed.
	FilesFilter    string
	BranchesFilter string
	TagsFilter     string
	StashFilter    string
	BranchesShown  []git.Branch
	TagsShown      []git.Tag
	StashShown     []git.Stash
	Commits        []git.LogEntry
	CommitsErr     error
	CommitsReady   bool
	CommitsLimit   int
	CommitsTotal   int
	Stash          []git.Stash
	StashErr       error
	StashReady     bool
	// Operation is whichever merge/rebase/cherry-pick/revert (if any)
	// currently has the working tree mid-flight — see
	// git.InProgressOperation, re-checked on every reloadGitBreach the
	// same way Status itself is, never patched in place. Drives both
	// the Status box's own severity line (renderGitBreachStatus) and
	// whether "C"/"A"/"K" (Continue/Abort/Skip — see
	// gitbreachoperation.go) do anything at all.
	Operation           git.Operation
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

// gitBreachNetworkTimeout bounds a real network call (Fetch, and later
// Push/Pull) — much more generous than gitBreachFetchTimeout's own 3s,
// which only ever waits on fast, local git operations. A slow, distant
// remote over a mediocre connection can legitimately take tens of
// seconds; this is a ceiling against an actually hung connection, not
// a tolerance for ordinary latency.
const gitBreachNetworkTimeout = 60 * time.Second

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
	r.gitBreach.BranchesTab = gitBreachBranchesTabBranches
	r.gitBreach.FilesTab = gitBreachFilesTabChanges
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
	r.gitBreach.Operation = git.InProgressOperation(r.gitBreach.Root)

	branches, branchesErr := git.Branches(ctx, r.gitBreach.Root)
	r.gitBreach.Branches = branches
	r.gitBreach.BranchesErr = branchesErr

	tags, tagsErr := git.Tags(ctx, r.gitBreach.Root)
	r.gitBreach.Tags = tags
	r.gitBreach.TagsErr = tagsErr

	remotes, remotesErr := git.Remotes(ctx, r.gitBreach.Root)
	r.gitBreach.Remotes = remotes
	r.gitBreach.RemotesErr = remotesErr

	worktrees, worktreesErr := git.Worktrees(ctx, r.gitBreach.Root)
	r.gitBreach.Worktrees = worktrees
	r.gitBreach.WorktreesErr = worktreesErr

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
// gitBreachFilesTab is which of the Files box's own sub-tabs is
// currently showing — the same "one box, several row kinds" shape
// gitBreachBranchesTab already establishes for Branches, applied here
// instead for Changes/Worktrees/Submodules (feature_ideas.txt's own
// later Ausbaustufen for this box). Changes is the box's own original,
// and still only, real content — Worktrees/Submodules start as plain
// placeholders (see renderGitBreachFiles' own doc comment) until their
// own later patches fill them in, the same "refactor first, content
// after" order Remotes' own BranchesTab work already followed.
type gitBreachFilesTab int

const (
	gitBreachFilesTabChanges gitBreachFilesTab = iota
	gitBreachFilesTabWorktrees
	gitBreachFilesTabSubmodules
)

// gitBreachFilesTabSpan mirrors gitBreachBranchesTabSpan exactly, for
// the Files box's own header instead of Branches'.
type gitBreachFilesTabSpan struct {
	start, end int
	tab        gitBreachFilesTab
}

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

// resolveGitBreachConflictSection is "o"/"t" while Main shows a
// Conflict row (see captureGitBreachMainKey) — always resolves section
// 0 of whatever git.ParseConflicts currently finds in the file (the
// topmost one Main's own gitBreachRenderConflict marks "> "), never a
// cursor-tracked position: Main is a plain *tview.TextView here, with
// no per-line selection of its own to hang "the current section" off
// of, and resolving top-down one at a time is unambiguous regardless.
// Uses showTransientErrorOverOverlay, not showError, on failure — see
// its own doc comment on the identical class of bug the Tags-tab
// actions already had to work around; Git breach's own longer-standing
// showError call sites (Checkout, Delete Branch, Commit, Stash Apply/
// Drop, Reload) likely share it too, deliberately left alone here as
// out of this change's own scope.
func (r *Root) resolveGitBreachConflictSection(side git.ConflictSide) {
	if !r.gitBreachMainShowsAConflict() {
		return
	}
	row, _ := r.gitBreach.FilesTable.GetSelection()
	gr, _ := r.gitBreachRowAt(row)

	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	if _, err := git.ResolveConflictSection(ctx, r.gitBreach.Root, gr.path, 0, side); err != nil {
		r.showTransientErrorOverOverlay(fmt.Errorf("git breach: %w", err))
		return
	}
	r.reloadGitBreach()
}

// gitBreachMainShowsAConflict mirrors gitBreachMainShowsAStageableHunk's
// own shape exactly (same MainOwner guard, same Files-row lookup), for
// the one other case Main's own key capture and hint bar need to tell
// apart from a plain diff: a Conflict row, where "o"/"t" apply and "s"
// (stage hunk) does not.
func (r *Root) gitBreachMainShowsAConflict() bool {
	if r.gitBreach.MainOwner != gitBreachMainOwnerFiles {
		return false
	}
	row, _ := r.gitBreach.FilesTable.GetSelection()
	gr, ok := r.gitBreachRowAt(row)
	return ok && gr.kind == gitBreachRowConflict
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

// openGitBreachMerge is "m" on the Branches table's own Branches sub-
// tab — merges the row under the cursor into whichever branch is
// currently checked out. Mirrors openGitBreachCheckout's own guard
// shape exactly: a no-op, not an error, on the already-current branch
// (merging a branch into itself is meaningless), and only confirms
// first if the working tree actually has something to lose (a clean
// tree merges immediately, same restraint openGitBreachCheckout's own
// doc comment already explains). Deliberately not reachable from the
// Tags sub-tab at all — merging a tag is no different from merging
// whatever commit it points at, and feature_ideas.txt's own Phase C
// item scopes this to branches specifically.
func (r *Root) openGitBreachMerge() {
	if r.gitBreach.BranchesTab != gitBreachBranchesTabBranches {
		return
	}
	row, _ := r.gitBreach.BranchesTable.GetSelection()
	branch, ok := r.gitBreachBranchAt(row)
	if !ok || branch.Current {
		return
	}

	st := r.gitBreach.Status
	if len(st.Staged) > 0 || len(st.Unstaged) > 0 || len(st.Conflicts) > 0 {
		r.openConfirm(
			fmt.Sprintf("Merge %q with uncommitted changes in the working tree?", branch.Name),
			"Yes, merge",
			func() { r.mergeGitBreachBranch(branch.Name) },
		)
		return
	}
	r.mergeGitBreachBranch(branch.Name)
}

// mergeGitBreachBranch runs the actual merge and reloads — both this
// dashboard's own state and the active panel (same reasoning
// checkoutGitBreachBranch's own doc comment gives: a merge can change
// files on disk in the exact directory the panel is already showing).
// A conflicting merge (git.Merge's own InProgressOperation check
// already distinguishes this from a real failure) reloads exactly the
// same way a clean one does — reloadGitBreach picks up the new
// Conflicts rows and Operation state, landing the user in Phase B's
// own conflict surface (resolveGitBreachConflictSection above, and
// gitbreachoperation.go's Continue/Abort/Skip) rather than at an error
// overlay for what both git and this dashboard already treat as an
// ordinary, expected outcome. Uses showTransientErrorOverOverlay, not
// showError, for the one case that is a real failure — see its own
// doc comment on the bug class every *new* Git breach action
// deliberately avoids from the start, same as
// resolveGitBreachConflictSection.
func (r *Root) mergeGitBreachBranch(branch string) {
	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	if err := git.Merge(ctx, r.gitBreach.Root, branch); err != nil {
		r.showTransientErrorOverOverlay(fmt.Errorf("git breach: %w", err))
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
	if row < 0 || row >= len(r.gitBreach.BranchesShown) {
		return git.Branch{}, false
	}
	return r.gitBreach.BranchesShown[row], true
}

// gitBreachTagAt returns gitBreach.Tags[row] — no detached-HEAD row-0
// special case to account for here the way gitBreachBranchAt has:
// a tag is never "current" the way a branch is, so renderGitBreachTags
// never inserts a non-selectable row ahead of the real ones.
func (r *Root) gitBreachTagAt(row int) (git.Tag, bool) {
	if row < 0 || row >= len(r.gitBreach.TagsShown) {
		return git.Tag{}, false
	}
	return r.gitBreach.TagsShown[row], true
}

// gitBreachRemoteAt indexes r.gitBreach.Remotes directly by row — no
// filter on this sub-tab (see renderGitBreachRemotes' own doc
// comment), so there's no *Shown slice the way Branches/Tags each
// have one.
func (r *Root) gitBreachRemoteAt(row int) (git.Remote, bool) {
	if row < 0 || row >= len(r.gitBreach.Remotes) {
		return git.Remote{}, false
	}
	return r.gitBreach.Remotes[row], true
}

// gitBreachWorktreeAt indexes r.gitBreach.Worktrees directly by row —
// no filter on this sub-tab, same reasoning as gitBreachRemoteAt's
// own doc comment above.
func (r *Root) gitBreachWorktreeAt(row int) (git.Worktree, bool) {
	if row < 0 || row >= len(r.gitBreach.Worktrees) {
		return git.Worktree{}, false
	}
	return r.gitBreach.Worktrees[row], true
}

// openGitBreachFetchRemote is "f" on a Remotes row — no confirm
// (unlike Push, a future patch: fetch only ever updates this
// repository's own remote-tracking refs, never the remote itself, so
// there's nothing here to weigh the way an irreversible action would
// need).
func (r *Root) openGitBreachFetchRemote() {
	if r.gitBreach.BranchesTab != gitBreachBranchesTabRemotes {
		return
	}
	row, _ := r.gitBreach.BranchesTable.GetSelection()
	rm, ok := r.gitBreachRemoteAt(row)
	if !ok {
		return
	}
	r.startGitBreachFetchRemote(rm.Name)
}

// startGitBreachFetchRemote dispatches the real network call off the
// UI goroutine — see internal/ui's own async-ui conventions
// (startGitBreachDiff and friends): root/name are captured here, never
// re-read from r.gitBreach inside the goroutine itself, so a later
// "jg" on a different repository while this is still in flight can
// never apply its result to the wrong one (see applyGitBreachFetchRemote's
// own staleness guard). The Remotes table itself shows "Fetching
// %q…" for the duration — a real network call can take long enough
// that Merge/StashPop's own "the state just changes, no explicit
// feedback" convention would otherwise read as unresponsive.
func (r *Root) startGitBreachFetchRemote(name string) {
	root := r.gitBreach.Root
	showTablePlaceholder(r.gitBreach.BranchesTable, fmt.Sprintf("Fetching %q…", name), r.theme.MutedTextColor)

	ctx, cancel := context.WithTimeout(context.Background(), gitBreachNetworkTimeout)
	r.safeGo("Git breach fetch", cancel, func() {
		err := git.FetchRemote(ctx, root, name)
		cancel()
		r.app.QueueUpdateDraw(func() {
			r.applyGitBreachFetchRemote(root, name, err)
		})
	})
}

// applyGitBreachFetchRemote is startGitBreachFetchRemote's own testable
// apply half (see the async-ui skill's own "split into fetchX/applyX"
// point: QueueUpdateDraw blocks forever without a running
// Application.Run, which no test in this package has). Discards
// silently if gitBreach.Root has since changed — a later "jg" on a
// different repository, or this fetch simply outliving the dashboard
// being closed and reopened elsewhere — the same staleness guard
// every other async feature in this package already applies to
// whatever identifies "is this still the thing we asked for".
func (r *Root) applyGitBreachFetchRemote(root, name string, err error) {
	if r.gitBreach.Root != root {
		return
	}
	if err != nil {
		r.showTransientErrorOverOverlay(fmt.Errorf("git breach: fetch %q: %w", name, err))
	}
	r.reloadGitBreach()
}

// openGitBreachPushRemote is "p" on a Remotes row — always confirms
// first, showing the exact command about to run (feature_ideas.txt's
// own explicit requirement for this action: "ein Remote-Push ist ein
// Netzwerk-Schreibzugriff mit anderem Blast-Radius... eigener
// Bestätigungsdialog, exakter Befehl vorab sichtbar"). Refuses outright
// on a detached HEAD — there's no branch name to push at all in that
// state, the same "nothing sane to act on" reasoning Checkout/Merge's
// own guards already apply elsewhere in this box.
func (r *Root) openGitBreachPushRemote() {
	if r.gitBreach.BranchesTab != gitBreachBranchesTabRemotes {
		return
	}
	if r.gitBreach.Status.Detached || r.gitBreach.Status.Branch == "" {
		return
	}
	row, _ := r.gitBreach.BranchesTable.GetSelection()
	rm, ok := r.gitBreachRemoteAt(row)
	if !ok {
		return
	}
	branch := r.gitBreach.Status.Branch
	r.openConfirm(
		fmt.Sprintf("Run %q?", fmt.Sprintf("git push %s %s", rm.Name, branch)),
		"Yes, push",
		func() { r.startGitBreachPushRemote(rm.Name, branch) },
	)
}

// startGitBreachPushRemote/applyGitBreachPushRemote mirror
// startGitBreachFetchRemote/applyGitBreachFetchRemote exactly — same
// async dispatch shape, same staleness guard, same "Verb-ing %q…"
// placeholder. The one real difference is git.PushRemote's own: a
// rejected push is always a real, reportable error here, never
// reinterpreted as something else (see its own doc comment).
func (r *Root) startGitBreachPushRemote(remote, branch string) {
	root := r.gitBreach.Root
	showTablePlaceholder(r.gitBreach.BranchesTable, fmt.Sprintf("Pushing %q…", remote), r.theme.MutedTextColor)

	ctx, cancel := context.WithTimeout(context.Background(), gitBreachNetworkTimeout)
	r.safeGo("Git breach push", cancel, func() {
		err := git.PushRemote(ctx, root, remote, branch)
		cancel()
		r.app.QueueUpdateDraw(func() {
			r.applyGitBreachPushRemote(root, remote, err)
		})
	})
}

func (r *Root) applyGitBreachPushRemote(root, name string, err error) {
	if r.gitBreach.Root != root {
		return
	}
	if err != nil {
		r.showTransientErrorOverOverlay(fmt.Errorf("git breach: push %q: %w", name, err))
	}
	r.reloadGitBreach()
}

// openGitBreachPullRemote is "P" on a Remotes row — mirrors
// openGitBreachMerge's own guard shape, not Push's always-confirm:
// a conflicting pull lands in the same conflict surface a conflicting
// Merge already does (see git.PullRemote's own doc comment), so the
// risk this confirm exists to flag is the dirty-tree case, same as
// Merge, not "this writes to the remote" the way Push's own always-
// confirm is about. Refuses on a detached HEAD, same reasoning as
// Push.
func (r *Root) openGitBreachPullRemote() {
	if r.gitBreach.BranchesTab != gitBreachBranchesTabRemotes {
		return
	}
	if r.gitBreach.Status.Detached || r.gitBreach.Status.Branch == "" {
		return
	}
	row, _ := r.gitBreach.BranchesTable.GetSelection()
	rm, ok := r.gitBreachRemoteAt(row)
	if !ok {
		return
	}
	branch := r.gitBreach.Status.Branch

	st := r.gitBreach.Status
	if len(st.Staged) > 0 || len(st.Unstaged) > 0 || len(st.Conflicts) > 0 {
		r.openConfirm(
			fmt.Sprintf("Pull %q with uncommitted changes in the working tree?", rm.Name),
			"Yes, pull",
			func() { r.startGitBreachPullRemote(rm.Name, branch) },
		)
		return
	}
	r.startGitBreachPullRemote(rm.Name, branch)
}

// startGitBreachPullRemote/applyGitBreachPullRemote mirror Fetch/Push's
// own async dispatch shape exactly. applyGitBreachPullRemote always
// reloads on success too, the same as a conflicting result — a
// conflicting pull needs it to pick up the new Conflicts rows and
// Operation state, landing the user in Phase B's own conflict surface
// rather than at an error overlay, exactly like mergeGitBreachBranch's
// own identical reasoning.
func (r *Root) startGitBreachPullRemote(remote, branch string) {
	root := r.gitBreach.Root
	showTablePlaceholder(r.gitBreach.BranchesTable, fmt.Sprintf("Pulling %q…", remote), r.theme.MutedTextColor)

	ctx, cancel := context.WithTimeout(context.Background(), gitBreachNetworkTimeout)
	r.safeGo("Git breach pull", cancel, func() {
		err := git.PullRemote(ctx, root, remote, branch)
		cancel()
		r.app.QueueUpdateDraw(func() {
			r.applyGitBreachPullRemote(root, remote, err)
		})
	})
}

func (r *Root) applyGitBreachPullRemote(root, name string, err error) {
	if r.gitBreach.Root != root {
		return
	}
	if err != nil {
		r.showTransientErrorOverOverlay(fmt.Errorf("git breach: pull %q: %w", name, err))
	}
	r.reloadGitBreach()
}

// toggleGitBreachBranchesTab/reverseToggleGitBreachBranchesTab cycle
// the Branches box forward/backward through its own three sub-tabs —
// Branches → Tags → Remotes → Branches again, wrapping either
// direction (Right/Left while the box has focus — see
// captureGitBreachBranchesTableKey) — and re-render in place, landing
// on the first row of whichever table just became visible rather than
// keeping a selection index that belonged to a different one.
func (r *Root) toggleGitBreachBranchesTab() {
	r.cycleGitBreachBranchesTab(1)
}

func (r *Root) reverseToggleGitBreachBranchesTab() {
	r.cycleGitBreachBranchesTab(-1)
}

func (r *Root) cycleGitBreachBranchesTab(delta int) {
	const tabCount = 3
	r.setGitBreachBranchesTab(gitBreachBranchesTab((int(r.gitBreach.BranchesTab) + delta + tabCount) % tabCount))
}

// setGitBreachBranchesTab switches the Branches box directly to tab
// and re-renders — shared by cycleGitBreachBranchesTab (Left/Right)
// and gitBreachFocusBranchesOnClick (clicking a sub-tab label
// directly), so both land on identical state afterward rather than
// each re-deriving the same render/refresh sequence.
func (r *Root) setGitBreachBranchesTab(tab gitBreachBranchesTab) {
	r.gitBreach.BranchesTab = tab
	r.renderGitBreachBranches()
	r.renderGitBreachBranchesHeader()
	// Focus itself never changes here (still the Branches table before
	// and after), so refreshGitBreachHint's own usual trigger (a
	// FocusFunc) never fires on its own — has to be called directly,
	// or the hint bar would keep advertising Enter/checkout and "d
	// delete branch" after switching to Tags, a real, live-caught bug.
	r.refreshGitBreachHint()
}

// cycleGitBreachFilesTab/setGitBreachFilesTab mirror
// cycleGitBreachBranchesTab/setGitBreachBranchesTab exactly, for the
// Files box's own three sub-tabs instead of Branches'.
func (r *Root) cycleGitBreachFilesTab(delta int) {
	const tabCount = 3
	r.setGitBreachFilesTab(gitBreachFilesTab((int(r.gitBreach.FilesTab) + delta + tabCount) % tabCount))
}

func (r *Root) setGitBreachFilesTab(tab gitBreachFilesTab) {
	r.gitBreach.FilesTab = tab
	r.renderGitBreachFiles()
	r.refreshGitBreachHint()
}

// openGitBreachCreateTag prompts for a name and creates a new,
// lightweight tag at HEAD — see git.CreateTag's own doc comment on
// why never annotated. Only reachable while the Tags sub-tab is
// showing (see captureGitBreachBranchesTableKey); nothing here needs
// a row under the cursor, unlike checkout/delete.
//
// Built directly on r.pushOverlay rather than the shared openPrompt
// helper — same reasoning openGitBreachCommitPrompt's own identical
// shape already has: openPrompt's own showOverlay calls
// closeAllOverlays first, which would close Git Breach itself (also
// an overlay) before ever showing the prompt on top of it, a real,
// live-caught bug (Enter after typing a name closed the whole
// dashboard instead of creating the tag).
func (r *Root) openGitBreachCreateTag() {
	r.prompt.SetLabel("New tag name ")
	r.prompt.SetText("")
	r.promptSubmit = func(name string) {
		if name == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
		defer cancel()
		if err := git.CreateTag(ctx, r.gitBreach.Root, name, "HEAD"); err != nil {
			// showTransientErrorOverOverlay, not showError: Git Breach
			// is itself an overlay, and showError's own showOverlay
			// closes whatever's already open before showing anything
			// — a real, live-caught bug (an already-existing tag name
			// closed the whole dashboard instead of just reporting the
			// refusal).
			r.showTransientErrorOverOverlay(fmt.Errorf("git breach: %w", err))
			return
		}
		r.reloadGitBreach()
	}

	const width, height = 56, 1
	_, _, screenWidth, screenHeight := r.GetRect()
	x, y, w, h := r.clampToScreen((screenWidth-width)/2, (screenHeight-height)/2, width, height)
	r.prompt.SetRect(x, y, w, h)
	r.pushOverlay(promptPage, r.prompt, nil)
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
// deleteGitBreachBranch already follows for its own box. Same
// showTransientErrorOverOverlay reasoning as openGitBreachCreateTag's
// own doc comment on its own identical error path.
func (r *Root) deleteGitBreachTag(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	if err := git.DeleteTag(ctx, r.gitBreach.Root, name); err != nil {
		r.showTransientErrorOverOverlay(fmt.Errorf("git breach: %w", err))
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
		case gitBreachRowConflict:
			text, err = git.ReadConflictedFile(root, gr.path)
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
			if gr.kind == gitBreachRowConflict {
				r.gitBreachSetDiffText(key, gitBreachRenderConflict(gr.path, git.ParseConflicts(text)), "")
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

// openGitBreachDiffAgainstRef is "d" on the Commits table — opens
// r.picker listing every ref already on screen (every local branch,
// every tag, every loaded commit — see feature_ideas.txt's own #21
// item 5 for the "beliebige zwei Commits/Branches/Refs" item this
// implements), and diffs the row under the cursor against whichever
// one gets picked. git itself can resolve plenty more refs than these
// three boxes list (a remote-tracking ref, HEAD~3), but a second git
// call just to enumerate them isn't worth it when every ref a user
// would actually reach for here is already loaded and on screen.
func (r *Root) openGitBreachDiffAgainstRef() {
	row, _ := r.gitBreach.CommitsTable.GetSelection()
	c, ok := r.gitBreachCommitAt(row)
	if !ok {
		return
	}
	r.openGitBreachRefPicker(c.Hash, func(ref string) {
		r.startGitBreachRefDiff(c, ref)
	})
}

// openGitBreachCherryPick is "P" on the Commits table — opens the same
// ref picker "d"/"B" already use (every local branch, tag, or loaded
// commit) and applies whichever one gets picked onto whatever's
// currently checked out. Deliberately not "the row under the cursor":
// git.Log only ever walks HEAD's own ancestry, so every row the
// Commits table itself can show is already applied — cherry-picking
// one back onto the same HEAD is exactly the empty, refused pick
// excludeHash below guards the obvious case of (HEAD onto itself);
// the ref picker's branches/tags are where a genuinely different
// commit actually comes from. Confirms first only if the working tree
// isn't clean, same restraint openGitBreachMerge's own guard has,
// applied inside the picker's own onPick callback so the confirm
// dialog layers on top of the dashboard the same way openGitBreachCheckout's
// already does, not on top of the picker which has already closed.
func (r *Root) openGitBreachCherryPick() {
	var head string
	if len(r.gitBreach.Commits) > 0 {
		head = r.gitBreach.Commits[0].Hash
	}
	r.openGitBreachRefPicker(head, func(ref string) {
		st := r.gitBreach.Status
		if len(st.Staged) > 0 || len(st.Unstaged) > 0 || len(st.Conflicts) > 0 {
			r.openConfirm(
				fmt.Sprintf("Cherry-pick %q with uncommitted changes in the working tree?", ref),
				"Yes, cherry-pick",
				func() { r.cherryPickGitBreachCommit(ref) },
			)
			return
		}
		r.cherryPickGitBreachCommit(ref)
	})
}

// cherryPickGitBreachCommit runs the actual pick and reloads — same
// "conflicted is not a real failure" shape as mergeGitBreachBranch,
// since git.CherryPick already turns one into a nil error. ref is
// whatever the picker in openGitBreachCherryPick returned — a branch
// name, tag name, or commit hash, all equally valid arguments to `git
// cherry-pick`.
func (r *Root) cherryPickGitBreachCommit(ref string) {
	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	if err := git.CherryPick(ctx, r.gitBreach.Root, ref); err != nil {
		r.showTransientErrorOverOverlay(fmt.Errorf("git breach: %w", err))
		return
	}
	r.reloadGitBreach()
	r.showError(r.panel.load(r.panel.path))
}

// openGitBreachRefPicker opens r.picker listing every ref already on
// screen (every local branch, every tag, every loaded commit except
// excludeHash), calling onPick with whichever one gets chosen —
// shared by openGitBreachDiffAgainstRef (see feature_ideas.txt's own
// #21 item 5) and openGitBreachBisectStart (item 7), both of which
// need "pick any ref git itself can resolve" and differ only in what
// they do with the result. git itself can resolve plenty more refs
// than these three boxes list (a remote-tracking ref, HEAD~3), but a
// second git call just to enumerate them isn't worth it when every
// ref a user would actually reach for here is already loaded and on
// screen.
func (r *Root) openGitBreachRefPicker(excludeHash string, onPick func(ref string)) {
	r.picker.Clear()
	for _, b := range r.gitBreach.Branches {
		name := b.Name
		r.picker.AddItem(name, "", 0, func() {
			r.hideOverlay()
			onPick(name)
		})
	}
	for _, tg := range r.gitBreach.Tags {
		name := tg.Name
		r.picker.AddItem(name, "", 0, func() {
			r.hideOverlay()
			onPick(name)
		})
	}
	for _, other := range r.gitBreach.Commits {
		if other.Hash == excludeHash {
			continue // picking a commit against itself is never useful
		}
		hash, label := other.Hash, other.Short+" "+other.Subject
		r.picker.AddItem(label, "", 0, func() {
			r.hideOverlay()
			onPick(hash)
		})
	}
	r.picker.SetDoneFunc(func() { r.hideOverlay() })

	width, _ := listSize(r.picker)
	height := pickerHeight
	if n := r.picker.GetItemCount(); n < height {
		height = n
	}
	x, y := r.centeredOnScreen(width, height)
	x, y, width, height = r.clampToPanel(x, y, width, height)
	r.picker.SetRect(x, y, width, height)
	r.picker.SetCurrentItem(0)

	r.pushOverlay(pickerPage, r.picker, nil)
}

// startGitBreachRefDiff fetches c's own diff against ref (git diff
// c.Hash ref — c first, the direction a user who selected c and then
// picked ref to compare it *to* actually expects, live-caught to be
// the opposite of openGitBreachDiffAgainstRef's own (ref, c)
// parameter order; see git.DiffRefs' own doc comment on argument
// order and what a reverse diff would mean here) and shows it in
// Main, same
// async shape as startGitBreachCommitDiff minus the debounce: a
// deliberate action (picked from openGitBreachDiffAgainstRef's own
// overlay), not cursor-movement noise. Its own "R:" scroll key keeps
// a ref-diff's remembered position separate from that commit's own
// ordinary diff. Moving the cursor afterward hands Main straight back
// to the ordinary commit diff via Commits' own
// SetSelectionChangedFunc — a one-shot look, the same convention
// startGitBreachBlame already establishes for the Files box.
func (r *Root) startGitBreachRefDiff(c git.LogEntry, ref string) {
	r.cancelGitBreachDiff()
	key := "R:" + c.Hash + ":" + ref
	r.gitBreachActivateDiffKey(key)

	ctx, cancel := context.WithCancel(context.Background())
	r.gitBreach.DiffCancel = cancel
	root := r.gitBreach.Root

	r.safeGo("Git breach ref diff", r.cancelGitBreachDiff, func() {
		fetchCtx, fetchCancel := context.WithTimeout(ctx, gitBreachFetchTimeout)
		// c.Hash first, ref second: "what would change going from the
		// row under the cursor to the picked ref" — the direction a
		// user who selected a commit and then picked something to
		// compare it *to* actually expects, live-caught to be the
		// opposite of what picking argument order to match
		// openGitBreachDiffAgainstRef's own (ref, c) parameter order
		// would have given.
		text, err := git.DiffRefs(fetchCtx, root, c.Hash, ref)
		fetchCancel()
		if ctx.Err() != nil {
			return
		}
		if err == nil && text == "" {
			text = fmt.Sprintf("%s..%s: no diff to show.", c.Short, ref)
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
				// showTransientErrorOverOverlay, not showError — same
				// reasoning as openGitBreachCreateTag's own doc
				// comment: Git Breach is itself an overlay, and
				// showError's own showOverlay would close it first.
				r.showTransientErrorOverOverlay(fmt.Errorf("git breach: %w", err))
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
	if row < 0 || row >= len(r.gitBreach.StashShown) {
		return git.Stash{}, false
	}
	return r.gitBreach.StashShown[row], true
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

// openGitBreachStashPop is "p" on the Stash table — apply-then-drop in
// one step, now safe to offer alongside Apply/Drop since Phase B's
// conflict surface exists (see git.StashPop's own doc comment on
// exactly what this was withheld for before). Mirrors
// openGitBreachStashApply's own guard shape: a confirm first only on a
// dirty tree, immediate otherwise — Pop is no more destructive than
// Apply in the clean-tree case (nothing is lost: the stashed content
// ends up in the working tree either way, whether or not the entry
// itself survives), so it gets the same restraint rather than Drop's
// own always-confirm.
func (r *Root) openGitBreachStashPop() {
	row, _ := r.gitBreach.StashTable.GetSelection()
	s, ok := r.gitBreachStashAt(row)
	if !ok {
		return
	}

	st := r.gitBreach.Status
	if len(st.Staged) > 0 || len(st.Unstaged) > 0 || len(st.Conflicts) > 0 {
		r.openConfirm(
			fmt.Sprintf("Pop stash %q with uncommitted changes in the working tree?", s.Message),
			"Yes, pop",
			func() { r.popGitBreachStash(s.Ref) },
		)
		return
	}
	r.popGitBreachStash(s.Ref)
}

// popGitBreachStash runs the actual pop and reloads — same reasoning
// as applyGitBreachStash's own doc comment, plus mergeGitBreachBranch's
// own "a conflicting outcome is not a real failure" distinction:
// git.StashPop already turns a conflicting pop into a nil error, so
// reloadGitBreach here always runs on success, landing a conflict
// directly in Phase B's own conflict surface rather than at an error
// overlay. Uses showTransientErrorOverOverlay for the one case that is
// a real failure, same as mergeGitBreachBranch/
// resolveGitBreachConflictSection — unlike applyGitBreachStash's own
// plain showError, left alone as out of this change's own scope (see
// the Phase A tags-tab fix's own doc comment on this exact bug class).
func (r *Root) popGitBreachStash(ref string) {
	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	if err := git.StashPop(ctx, r.gitBreach.Root, ref); err != nil {
		r.showTransientErrorOverOverlay(fmt.Errorf("git breach: %w", err))
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
