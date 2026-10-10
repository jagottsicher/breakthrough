// gitbreach.go is the Git breach dashboard's own glue between
// internal/git (Root/Fetch/Stage/Unstage/Commit/Diff — the real,
// UI-free business logic) and the multi-pane layout gitbreachscreen.go
// builds. See gitBreachLayout's own doc comment on Root for the
// overall shape and why it's planned as several boxes from the start.
package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/git"
	"github.com/jagottsicher/breakthrough/internal/viewer"
)

const gitBreachPage = "gitBreach"

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

	r.gitBreachDir = r.panel.path
	r.gitBreachRoot = root
	r.reloadGitBreach()
	r.showOverlayWithRestore(gitBreachPage, r.gitBreachLayout, r.restoreGitBreachFocus)
}

// restoreGitBreachFocus sends real keyboard focus to the Files table —
// the dashboard's own only interactive box in this first Ausbaustufe
// (Status is a read-only summary, Branches/Commits/Stash are stubs) —
// both on first open (showOverlayWithRestore's own initial call) and
// whenever something pushed on top of this dashboard closes back to it
// (the commit-message prompt — see openGitBreachCommitPrompt), the
// same "restore callback" shape restoreProperties already establishes.
func (r *Root) restoreGitBreachFocus() {
	r.app.SetFocus(r.gitBreachFilesTable)
}

// reloadGitBreach re-fetches gitBreachRoot's own status and rebuilds
// every box's own content — "r" re-runs this directly, and every
// stage/unstage/commit does too afterward, the same "re-run the real
// read, don't just redraw" contract reloadLogAuditDiscovery already
// documents.
func (r *Root) reloadGitBreach() {
	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	status, err := git.Fetch(ctx, r.gitBreachRoot)
	r.gitBreachStatus = status
	r.gitBreachFetchErr = err
	r.gitBreachRows = buildGitBreachRows(status)
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
	row, _ := r.gitBreachFilesTable.GetSelection()
	gr, ok := r.gitBreachRowAt(row)
	if !ok || gr.kind == gitBreachRowConflict {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()

	var err error
	if gr.kind == gitBreachRowStaged {
		err = git.Unstage(ctx, r.gitBreachRoot, []string{gr.path})
	} else {
		err = git.Stage(ctx, r.gitBreachRoot, []string{gr.path})
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
// goes through gitBreachRowIndex (see renderGitBreachFiles) because the
// table also contains non-selectable section header rows interspersed
// with real file rows; row lands on one of those (or past the end)
// reports false, same as any other out-of-range row would.
func (r *Root) gitBreachRowAt(row int) (gitBreachRow, bool) {
	idx, ok := r.gitBreachRowIndex[row]
	if !ok || idx < 0 || idx >= len(r.gitBreachRows) {
		return gitBreachRow{}, false
	}
	return r.gitBreachRows[idx], true
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
	if len(r.gitBreachStatus.Staged) == 0 {
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
	if err := git.Commit(ctx, r.gitBreachRoot, message); err != nil {
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
	if r.gitBreachDiffCancel != nil {
		r.gitBreachDiffCancel()
		r.gitBreachDiffCancel = nil
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
func (r *Root) startGitBreachDiff(row int) {
	r.cancelGitBreachDiff()
	gr, ok := r.gitBreachRowAt(row)
	if !ok {
		r.gitBreachDiffView.SetText("")
		return
	}

	if gr.kind == gitBreachRowConflict {
		r.gitBreachDiffView.SetText(tview.Escape(fmt.Sprintf("%s: unresolved conflict — resolving conflicts is not yet supported here.", gr.path)))
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.gitBreachDiffCancel = cancel
	root := r.gitBreachRoot

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
			curRow, _ := r.gitBreachFilesTable.GetSelection()
			curGr, ok := r.gitBreachRowAt(curRow)
			if !ok || curGr != gr {
				return // the cursor moved to a different row before this landed
			}
			if err != nil {
				r.gitBreachDiffView.SetText(tview.Escape(fmt.Sprintf("%s: %v", gr.path, err)))
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
				r.gitBreachDiffView.SetText(renderSyntax(viewer.Highlight(gr.path, text), paletteFor(r.theme.SurfaceBackground)))
				return
			}
			r.gitBreachDiffView.SetText(gitBreachColorizeDiff(text, gr.path, r.theme))
		})
	})
}
