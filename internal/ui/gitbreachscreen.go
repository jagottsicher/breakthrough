package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// gitBreachStubHeight/gitBreachStatusHeight are the fixed row counts
// Branches/Commits/Stash and Status get in the left column's own Flex
// — a stub box only ever needs room for its one-line placeholder plus
// its own border, Status for its two-line summary plus border. Files,
// the one real list, gets whatever's left (see newGitBreachScreen's
// own AddItem proportions).
const (
	gitBreachStatusHeight = 4
	gitBreachStubHeight   = 3
)

// newGitBreachScreen builds the whole dashboard once — see
// gitBreachLayout's own doc comment on Root for why this is laid out
// as several bordered boxes rather than one full-screen list. Native
// tview borders (Box.SetBorder/SetTitle), not this app's own usual
// title-bar-TextView convention every single-list screen already uses
// elsewhere: this is the one screen that actually needs to show
// several distinct regions on screen *at once*, which a real border
// gives for free and a shared title-bar convention built for exactly
// one region at a time does not.
func (r *Root) newGitBreachScreen() {
	r.gitBreachTitleBar = newPlainTitleBar("Git breach")
	r.gitBreachTitleBar.SetMouseCapture(captureCloseTitleBarMouse(r.gitBreachTitleBar, r.closeGitBreach))

	r.gitBreachStatusView = newGitBreachBox("Status")

	r.gitBreachFilesTable = tview.NewTable()
	r.gitBreachFilesTable.SetBorder(true)
	r.gitBreachFilesTable.SetTitle(" Files ")
	r.gitBreachFilesTable.SetTitleAlign(tview.AlignLeft)
	r.gitBreachFilesTable.SetBorderPadding(0, 0, 1, 1)
	r.gitBreachFilesTable.SetSelectable(true, false)
	r.gitBreachFilesTable.SetInputCapture(r.captureGitBreachFilesTableKey)
	r.gitBreachFilesTable.SetSelectionChangedFunc(func(row, _ int) { r.startGitBreachDiff(row) })

	r.gitBreachBranchesView = newGitBreachBox("Branches (stub)")
	r.gitBreachBranchesView.SetText("kommt in einer späteren Ausbaustufe")
	r.gitBreachCommitsView = newGitBreachBox("Commits (stub)")
	r.gitBreachCommitsView.SetText("kommt in einer späteren Ausbaustufe")
	r.gitBreachStashView = newGitBreachBox("Stash (stub)")
	r.gitBreachStashView.SetText("kommt in einer späteren Ausbaustufe")

	r.gitBreachDiffView = newGitBreachBox("Main — Diff")
	r.gitBreachDiffView.SetWrap(false)
	r.gitBreachDiffView.SetDynamicColors(false)
	r.gitBreachDiffView.SetScrollable(true)

	r.gitBreachHint = tview.NewTextView()
	r.gitBreachHint.SetWrap(false)
	r.gitBreachHint.SetDynamicColors(true)
	hintText, hintSpans := buildListHint(r.theme, gitBreachHintEntries())
	r.gitBreachHint.SetText(hintText)
	r.gitBreachHintSpans = hintSpans
	r.gitBreachHint.SetMouseCapture(r.captureListHintMouse(r.gitBreachHint, &r.gitBreachHintSpans))

	leftColumn := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.gitBreachStatusView, gitBreachStatusHeight, 0, false).
		AddItem(r.gitBreachFilesTable, 0, 1, true).
		AddItem(r.gitBreachBranchesView, gitBreachStubHeight, 0, false).
		AddItem(r.gitBreachCommitsView, gitBreachStubHeight, 0, false).
		AddItem(r.gitBreachStashView, gitBreachStubHeight, 0, false)

	body := tview.NewFlex().
		AddItem(leftColumn, 0, 1, true).
		AddItem(r.gitBreachDiffView, 0, 2, false)

	r.gitBreachLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.gitBreachTitleBar, 1, 0, false).
		AddItem(body, 0, 1, true).
		AddItem(r.gitBreachHint, 1, 0, false)
}

// newGitBreachBox is every non-Files box's own shared construction —
// bordered, left-aligned title, no internal padding needed beyond the
// border itself for a couple of lines of plain text.
func newGitBreachBox(title string) *tview.TextView {
	v := tview.NewTextView()
	v.SetBorder(true)
	v.SetTitle(" " + title + " ")
	v.SetTitleAlign(tview.AlignLeft)
	v.SetWrap(true)
	return v
}

// applyGitBreachTheme colors every box the same SurfaceBackground/
// BorderColor/Text triple every other overlay in this app already
// uses, just applied to several bordered Box-es at once instead of one
// screen-wide background — see newGitBreachScreen's own doc comment
// for why this screen alone needs real borders.
func (r *Root) applyGitBreachTheme(theme config.ResolvedTheme) {
	if r.gitBreachLayout == nil {
		return
	}
	r.gitBreachLayout.SetBackgroundColor(theme.SurfaceBackground)
	r.gitBreachTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.gitBreachTitleBar.SetTextColor(theme.TextColor)
	r.gitBreachHint.SetBackgroundColor(theme.InputBackground)
	r.gitBreachHint.SetTextColor(theme.MutedTextColor)
	hintText, hintSpans := buildListHint(theme, gitBreachHintEntries())
	r.gitBreachHint.SetText(hintText)
	r.gitBreachHintSpans = hintSpans

	for _, box := range []*tview.TextView{r.gitBreachStatusView, r.gitBreachBranchesView, r.gitBreachCommitsView, r.gitBreachStashView, r.gitBreachDiffView} {
		box.SetBackgroundColor(theme.SurfaceBackground)
		box.SetBorderColor(theme.BorderColor)
		box.SetTitleColor(theme.Text)
		box.SetTextColor(theme.Text)
	}
	r.gitBreachFilesTable.SetBackgroundColor(theme.SurfaceBackground)
	r.gitBreachFilesTable.SetBorderColor(theme.BorderColor)
	r.gitBreachFilesTable.SetTitleColor(theme.Text)

	r.renderGitBreach() // cell colors baked in per cell, not looked up live at draw time
}

// gitBreachHintEntries: Space stages/unstages the row under the cursor,
// "c" opens the commit-message prompt, "r" re-reads the repository,
// Escape closes — see captureGitBreachFilesTableKey for where each is
// actually wired.
func gitBreachHintEntries() []listHintEntry {
	return []listHintEntry{
		hintKey("Space", "stage/unstage", func(r *Root) { r.toggleGitBreachStage() }),
		hintKey("c", "commit", func(r *Root) { r.openGitBreachCommitPrompt() }),
		hintKey("r", "reload", func(r *Root) { r.reloadGitBreach() }),
		hintKey("Esc", "close", func(r *Root) { r.closeGitBreach() }),
	}
}

// captureGitBreachFilesTableKey: Space toggles stage/unstage, "c"
// commits, "r" reloads, Escape closes — the same per-table
// InputCapture shape captureLogAuditSelectionTableKey already
// establishes elsewhere in this package.
func (r *Root) captureGitBreachFilesTableKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeGitBreach()
		return nil
	}
	if event.Key() == tcell.KeyRune {
		switch event.Rune() {
		case ' ':
			r.toggleGitBreachStage()
			return nil
		case 'c':
			r.openGitBreachCommitPrompt()
			return nil
		case 'r':
			r.reloadGitBreach()
			return nil
		}
	}
	return event
}

// renderGitBreach refreshes every box from r.gitBreachStatus/Rows —
// called by reloadGitBreach after every fetch, stage, unstage, and
// commit, the same "re-render from the current state, never patch it
// in place" convention every other screen in this package follows.
func (r *Root) renderGitBreach() {
	title := " Git breach"
	if r.gitBreachRoot != "" {
		title += " — " + r.gitBreachRoot
	}
	renderCloseTitleBar(r.gitBreachTitleBar, title+" ", r.lastScreenWidth)

	r.renderGitBreachStatus()
	r.renderGitBreachFiles()
}

// renderGitBreachStatus fills the Status box — branch, ahead/behind,
// and a one-line severity summary that mirrors gitStatusColor's own
// severity language (conflicts outrank being dirty, which outranks
// clean) applied to a line of text instead of a status-bar color.
func (r *Root) renderGitBreachStatus() {
	st := r.gitBreachStatus
	var lines []string

	branch := st.Branch
	if st.Detached {
		branch = "(detached) " + branch
	}
	if st.Ahead > 0 {
		branch += fmt.Sprintf("  ahead %d", st.Ahead)
	}
	if st.Behind > 0 {
		branch += fmt.Sprintf("  behind %d", st.Behind)
	}
	lines = append(lines, branch)

	switch {
	case len(st.Conflicts) > 0:
		lines = append(lines, fmt.Sprintf("%d conflict(s)", len(st.Conflicts)))
	case len(st.Staged)+len(st.Unstaged)+len(st.Untracked) == 0:
		lines = append(lines, "Working tree clean")
	default:
		lines = append(lines, fmt.Sprintf("%d staged, %d unstaged, %d untracked", len(st.Staged), len(st.Unstaged), len(st.Untracked)))
	}

	if r.gitBreachFetchErr != nil {
		lines = append(lines, r.gitBreachFetchErr.Error())
	}

	r.gitBreachStatusView.SetText(strings.Join(lines, "\n"))
}

// renderGitBreachFiles rebuilds the Files table: one bold, non-
// selectable header row per non-empty section ("Staged (n)" etc.,
// Conflicts first — the same "most urgent first" ordering
// gitStatusColor's own severity language already establishes, ahead of
// Staged/Unstaged/Untracked, which otherwise follow git's own usual
// reading order), each section's own rows beneath it. gitBreachRowIndex
// maps each resulting table row back to its own index in
// r.gitBreachRows — a section header row has no entry in it at all, so
// gitBreachRowAt correctly reports "no row" for one rather than
// pointing at the wrong file.
func (r *Root) renderGitBreachFiles() {
	r.gitBreachFilesTable.Clear()
	r.gitBreachRowIndex = map[int]int{}

	type section struct {
		label string
		kind  gitBreachRowKind
	}
	sections := []section{
		{"Conflicts", gitBreachRowConflict},
		{"Staged", gitBreachRowStaged},
		{"Unstaged", gitBreachRowUnstaged},
		{"Untracked", gitBreachRowUntracked},
	}

	row := 0
	for _, sec := range sections {
		var indices []int
		for i, gr := range r.gitBreachRows {
			if gr.kind == sec.kind {
				indices = append(indices, i)
			}
		}
		if len(indices) == 0 {
			continue
		}

		r.gitBreachFilesTable.SetCell(row, 0,
			tview.NewTableCell(fmt.Sprintf("%s (%d)", sec.label, len(indices))).
				SetTextColor(r.theme.Text).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false))
		row++

		for _, idx := range indices {
			gr := r.gitBreachRows[idx]
			r.gitBreachFilesTable.SetCell(row, 0,
				tview.NewTableCell(gitBreachRowText(gr)).SetTextColor(gitBreachRowColor(gr, r.theme)))
			r.gitBreachRowIndex[row] = idx
			row++
		}
	}

	if r.gitBreachFetchErr != nil {
		showTablePlaceholder(r.gitBreachFilesTable, r.gitBreachFetchErr.Error(), r.theme.EntryError)
		return
	}
	if row == 0 {
		showTablePlaceholder(r.gitBreachFilesTable, "Working tree clean.", r.theme.MutedTextColor)
		return
	}

	// Real rows exist past this point — re-assert SetSelectable(true,
	// false) the same way renderSessions' own tail already does, for
	// the same reason: showTablePlaceholder (used above, and by the
	// very first render before any real git status has even been
	// fetched yet) turns row selection off entirely as its own fix for
	// a real tview v0.42.0 freeze (see its own doc comment), and never
	// turns it back on by itself — without this, Up/Down would stay
	// permanently inert from that first empty render onward, even once
	// real, selectable rows exist.
	r.gitBreachFilesTable.SetSelectable(true, false)

	// Keep the cursor on a real file row, not a non-selectable section
	// header — the same restraint renderActivityLog's own tail already
	// shows after a refresh changes how many rows there are.
	cur, _ := r.gitBreachFilesTable.GetSelection()
	if _, ok := r.gitBreachRowIndex[cur]; !ok {
		r.gitBreachFilesTable.Select(gitBreachFirstDataRow(r.gitBreachRowIndex), 0)
	}
}

// gitBreachFirstDataRow returns the lowest table row present in
// rowIndex — renderGitBreachFiles builds section headers and file rows
// in top-to-bottom order, so this is always the very first real file
// row, whichever section it belongs to.
func gitBreachFirstDataRow(rowIndex map[int]int) int {
	first := -1
	for row := range rowIndex {
		if first == -1 || row < first {
			first = row
		}
	}
	if first == -1 {
		return 0
	}
	return first
}

// gitBreachRowText renders one Files row — the status letter git
// itself uses (M/A/D/R/C/T for Staged/Unstaged, synthesized '?'/'U' for
// Untracked/Conflict, neither of which git.FileChange.Code actually
// carries since Status.Untracked/Conflicts are plain []string — see
// internal/git/status.go) followed by the path, or "old -> new" for a
// rename/copy (OrigPath set).
func gitBreachRowText(gr gitBreachRow) string {
	code := gr.code
	switch gr.kind {
	case gitBreachRowUntracked:
		code = '?'
	case gitBreachRowConflict:
		code = 'U'
	}
	label := gr.path
	if gr.origPath != "" {
		label = gr.origPath + " -> " + gr.path
	}
	return fmt.Sprintf(" %c  %s", code, label)
}

// gitBreachRowColor mirrors gitStatusColor's own severity language
// (internal/ui/gitstatus.go) at the level of one row instead of one
// whole status-bar segment: a conflict is critical, a staged change is
// "ready, healthy" green, an unstaged one is a warning still needing
// attention, untracked is merely informational.
func gitBreachRowColor(gr gitBreachRow, theme config.ResolvedTheme) tcell.Color {
	switch gr.kind {
	case gitBreachRowConflict:
		return theme.CriticalText
	case gitBreachRowStaged:
		return theme.EntryExecutable
	case gitBreachRowUnstaged:
		return theme.WarningText
	default:
		return theme.MutedTextColor
	}
}
