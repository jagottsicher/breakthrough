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
// — a stub box only ever needs room for its own one-row header plus a
// one-line placeholder, Status for its own header plus a two-line
// summary. Files, the one real list, gets whatever's left (see
// newGitBreachScreen's own AddItem proportions).
const (
	gitBreachStatusHeight = 3
	gitBreachStubHeight   = 2
)

// gitBreachDiffAddedColor is a dedicated, brighter green for a diff's
// own added lines — per the user's own explicit request for something
// stronger than theme.EntryExecutable (the panel's own muted
// "executable file name" green, #008000, deliberately kept dark
// against the dark panel background — see its own doc comment). A
// diff's own red/green convention is expected to read as vivid at a
// glance, unlike a sedate file-type indicator, so this is a fixed
// color rather than a theme field, the same "a specific feature gets
// its own named, hardcoded color outside the user-configurable
// scheme" precedent bottombar.go's own statusDiskColor/statusInodeColor
// etc. already establish. #3cb44b is not a new, arbitrary choice: it's
// the same green already vetted as one of this app's own nine
// maximally-distinct label colors (see labelFallbackColors in
// internal/config/theme.go), reused here rather than inventing another.
var gitBreachDiffAddedColor = tcell.GetColor("#3cb44b")

// newGitBreachScreen builds the whole dashboard once — see
// gitBreachLayout's own doc comment on Root for why this is laid out
// as several boxes at once rather than one full-screen list. Each box
// is a plain one-row header (newGitBreachBoxHeader) stacked over its
// own content, not a bordered tview.Box — per the user's own explicit
// request, matching this app's own established "no borders, a colored
// header line shows what's focused" convention (toolWindow's/Details'
// own title bars: InputFocusedBackground while that box has real
// keyboard focus, InputBackground while it doesn't — see
// styleGitBreachFocus). The user's own further request — the body
// itself also changes background, not just the header — is what
// styleGitBreachFocus's own PopupBackground/SurfaceBackground pair
// adds on top of that established scheme.
func (r *Root) newGitBreachScreen() {
	r.gitBreachTitleBar = newPlainTitleBar("Git breach")
	r.gitBreachTitleBar.SetMouseCapture(captureCloseTitleBarMouse(r.gitBreachTitleBar, r.closeGitBreach))

	r.gitBreachStatusHeader, r.gitBreachStatusView = newGitBreachBox("Status")

	r.gitBreachFilesHeader = newGitBreachBoxHeader("Files")
	// The header is its own separate TextView from the table it labels
	// — a plain click on it would otherwise do nothing at all (see
	// gitBreachBlockFocusSteal's own doc comment for why every other
	// box's header is deliberately inert instead), so it gets its own
	// mouse capture that focuses Files explicitly, per the user's own
	// explicit request that the header line itself be clickable too,
	// not just the table beneath it.
	r.gitBreachFilesHeader.SetMouseCapture(r.gitBreachFocusFilesOnClick)
	r.gitBreachFilesTable = tview.NewTable()
	r.gitBreachFilesTable.SetSelectable(true, false)
	r.gitBreachFilesTable.SetInputCapture(r.captureGitBreachFilesTableKey)
	r.gitBreachFilesTable.SetSelectionChangedFunc(func(row, _ int) { r.startGitBreachDiff(row) })
	// Files is the one real, focusable box in this first Ausbaustufe —
	// Status/Branches/Commits/Stash/Main have nothing for keyboard focus
	// to ever land on yet, so their own headers/bodies stay permanently
	// in the "inactive" look applyGitBreachTheme's own initial pass
	// already gives every box.
	r.gitBreachFilesTable.SetFocusFunc(func() { r.styleGitBreachFocus(r.gitBreachFilesHeader, r.gitBreachFilesTable, true) })
	r.gitBreachFilesTable.SetBlurFunc(func() { r.styleGitBreachFocus(r.gitBreachFilesHeader, r.gitBreachFilesTable, false) })

	r.gitBreachStatusHeader.SetMouseCapture(gitBreachBlockFocusSteal)
	r.gitBreachStatusView.SetMouseCapture(gitBreachBlockFocusSteal)

	r.gitBreachBranchesHeader, r.gitBreachBranchesView = newGitBreachBox("Branches (stub)")
	r.gitBreachBranchesView.SetText("kommt in einer späteren Ausbaustufe")
	r.gitBreachBranchesHeader.SetMouseCapture(gitBreachBlockFocusSteal)
	r.gitBreachBranchesView.SetMouseCapture(gitBreachBlockFocusSteal)
	r.gitBreachCommitsHeader, r.gitBreachCommitsView = newGitBreachBox("Commits (stub)")
	r.gitBreachCommitsView.SetText("kommt in einer späteren Ausbaustufe")
	r.gitBreachCommitsHeader.SetMouseCapture(gitBreachBlockFocusSteal)
	r.gitBreachCommitsView.SetMouseCapture(gitBreachBlockFocusSteal)
	r.gitBreachStashHeader, r.gitBreachStashView = newGitBreachBox("Stash (stub)")
	r.gitBreachStashView.SetText("kommt in einer späteren Ausbaustufe")
	r.gitBreachStashHeader.SetMouseCapture(gitBreachBlockFocusSteal)
	r.gitBreachStashView.SetMouseCapture(gitBreachBlockFocusSteal)

	r.gitBreachDiffHeader, r.gitBreachDiffView = newGitBreachBox("Main — Diff")
	r.gitBreachDiffView.SetWrap(false)
	// Dynamic colors — per the user's own explicit request that a
	// diff's own removed/added lines be colored (see
	// gitBreachColorizeDiff, which every real diff shown here goes
	// through), not left as plain text.
	r.gitBreachDiffView.SetDynamicColors(true)
	r.gitBreachDiffView.SetScrollable(true)
	r.gitBreachDiffHeader.SetMouseCapture(gitBreachBlockFocusSteal)
	// The diff view's own MouseLeftDown is blocked the same as every
	// other passive box's (see gitBreachBlockFocusSteal), but every
	// other mouse action still passes through unchanged — the wheel
	// scroll a long diff needs still works without ever taking
	// keyboard focus away from Files.
	r.gitBreachDiffView.SetMouseCapture(gitBreachBlockFocusSteal)

	r.gitBreachHint = tview.NewTextView()
	r.gitBreachHint.SetWrap(false)
	r.gitBreachHint.SetDynamicColors(true)
	hintText, hintSpans := buildListHint(r.theme, gitBreachHintEntries())
	r.gitBreachHint.SetText(hintText)
	r.gitBreachHintSpans = hintSpans
	r.gitBreachHint.SetMouseCapture(r.captureListHintMouse(r.gitBreachHint, &r.gitBreachHintSpans))

	leftColumn := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(gitBreachBoxFlex(r.gitBreachStatusHeader, r.gitBreachStatusView), gitBreachStatusHeight, 0, false).
		AddItem(gitBreachBoxFlex(r.gitBreachFilesHeader, r.gitBreachFilesTable), 0, 1, true).
		AddItem(gitBreachBoxFlex(r.gitBreachBranchesHeader, r.gitBreachBranchesView), gitBreachStubHeight, 0, false).
		AddItem(gitBreachBoxFlex(r.gitBreachCommitsHeader, r.gitBreachCommitsView), gitBreachStubHeight, 0, false).
		AddItem(gitBreachBoxFlex(r.gitBreachStashHeader, r.gitBreachStashView), gitBreachStubHeight, 0, false)

	body := tview.NewFlex().
		AddItem(leftColumn, 0, 1, true).
		AddItem(gitBreachBoxFlex(r.gitBreachDiffHeader, r.gitBreachDiffView), 0, 2, false)

	r.gitBreachLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.gitBreachTitleBar, 1, 0, false).
		AddItem(body, 0, 1, true).
		AddItem(r.gitBreachHint, 1, 0, false)
}

// newGitBreachBoxHeader is every box's own one-row header line — plain
// text, no border, colored by styleGitBreachFocus/applyGitBreachTheme
// rather than here (construction time has no theme to color it with
// yet, same reason every other screen's own widgets in this package
// are colored by a separate applyXTheme pass instead of at construction).
func newGitBreachBoxHeader(title string) *tview.TextView {
	h := tview.NewTextView()
	h.SetWrap(false)
	h.SetText(" " + title + " ")
	return h
}

// gitBreachBlockFocusSteal is every passive box's own (and its own
// header's) mouse capture — Status, Branches/Commits/Stash, Main, and
// all five of their own headers. A plain tview.TextView's own default
// MouseHandler calls setFocus(itself) unconditionally on a
// MouseLeftDown anywhere inside it (verified directly against
// textview.go, not guessed) — fine for a box that's actually meant to
// receive keyboard focus, but a real, reported bug for one that isn't:
// an ordinary click anywhere on this dashboard outside the Files table
// silently moved real keyboard focus onto a box nothing could ever
// navigate inside, with no way back short of closing and reopening the
// whole dashboard. Swallowing MouseLeftDown here is what keeps that
// from happening; every other mouse action (wheel scroll, chiefly)
// still passes through unchanged, so the Main box's own diff can still
// be scrolled with the mouse without taking focus away from Files.
func gitBreachBlockFocusSteal(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action == tview.MouseLeftDown {
		return tview.MouseConsumed, nil
	}
	return action, event
}

// gitBreachFocusFilesOnClick is the Files header's own mouse capture —
// clicking the header line itself focuses the Files table, the same as
// clicking anywhere in the table's own body already does via Table's
// default MouseHandler, per the user's own explicit request that the
// header be clickable too, not just the content beneath it.
func (r *Root) gitBreachFocusFilesOnClick(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action == tview.MouseLeftDown {
		r.app.SetFocus(r.gitBreachFilesTable)
		return tview.MouseConsumed, nil
	}
	return action, event
}

// newGitBreachBox is every TextView-bodied box's own shared
// construction (Status, the three stubs, Main) — Files is the one
// exception, built inline in newGitBreachScreen since its own body is
// a Table, not a TextView.
func newGitBreachBox(title string) (header, body *tview.TextView) {
	header = newGitBreachBoxHeader(title)
	body = tview.NewTextView()
	body.SetWrap(true)
	return header, body
}

// gitBreachBoxFlex stacks one box's own header over its own body — the
// one shape every box in this dashboard shares, Files' Table body
// included (tview.Primitive is satisfied by both).
func gitBreachBoxFlex(header *tview.TextView, body tview.Primitive) *tview.Flex {
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(body, 0, 1, true)
}

// gitBreachBackgroundSetter is the shape both *tview.TextView (which
// overrides SetBackgroundColor to also touch its own internal style)
// and *tview.Table (which inherits it verbatim from the embedded Box)
// already share — both still return *tview.Box, verified directly
// against tview's own source rather than guessed — letting
// styleGitBreachFocus restyle either kind of box body with one shared
// function instead of one per concrete type.
type gitBreachBackgroundSetter interface {
	SetBackgroundColor(tcell.Color) *tview.Box
}

// styleGitBreachFocus recolors one box's own header/body pair for
// whether it currently has real keyboard focus — the same two-state
// header scheme toolWindow's/Details' own title bars already establish
// (InputFocusedBackground while focused, InputBackground while not),
// per the user's own explicit request that every box in this dashboard
// follow it too, instead of the bordered-box look this screen started
// with. The body itself also tints (PopupBackground, this app's own
// already-established "distinct secondary surface" role — see
// logAuditTimelineView's own doc comment for the same reasoning
// applied there — vs. the baseline SurfaceBackground every box's body
// otherwise sits on) per the user's own further, explicitly optional
// request that an active box read as visually distinct beyond just its
// own header line.
func (r *Root) styleGitBreachFocus(header *tview.TextView, body gitBreachBackgroundSetter, focused bool) {
	if focused {
		header.SetBackgroundColor(r.theme.InputFocusedBackground)
		body.SetBackgroundColor(r.theme.PopupBackground)
	} else {
		header.SetBackgroundColor(r.theme.InputBackground)
		body.SetBackgroundColor(r.theme.SurfaceBackground)
	}
}

// applyGitBreachTheme colors every box's own header/body pair — an
// initial "nothing focused yet" pass across all of them, then
// re-asserts Files' own focused look on top if it's actually what
// currently has real keyboard focus, the same "a live theme switch
// must not silently lose a focus-dependent look" case this app's other
// focus-aware widgets already handle (see e.g.
// updateOverlayTitleBarColors).
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

	headers := []*tview.TextView{
		r.gitBreachStatusHeader, r.gitBreachFilesHeader, r.gitBreachBranchesHeader,
		r.gitBreachCommitsHeader, r.gitBreachStashHeader, r.gitBreachDiffHeader,
	}
	for _, h := range headers {
		h.SetBackgroundColor(theme.InputBackground)
		h.SetTextColor(theme.TextColor)
	}

	textBodies := []*tview.TextView{
		r.gitBreachStatusView, r.gitBreachBranchesView, r.gitBreachCommitsView,
		r.gitBreachStashView, r.gitBreachDiffView,
	}
	for _, v := range textBodies {
		v.SetBackgroundColor(theme.SurfaceBackground)
		v.SetTextColor(theme.Text)
	}
	r.gitBreachFilesTable.SetBackgroundColor(theme.SurfaceBackground)

	if r.gitBreachFilesTable.HasFocus() {
		r.styleGitBreachFocus(r.gitBreachFilesHeader, r.gitBreachFilesTable, true)
	}

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
		r.cancelGitBreachDiff()
		r.gitBreachDiffView.SetText("")
		return
	}
	if row == 0 {
		showTablePlaceholder(r.gitBreachFilesTable, "Working tree clean.", r.theme.MutedTextColor)
		r.cancelGitBreachDiff()
		r.gitBreachDiffView.SetText("")
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
		cur = gitBreachFirstDataRow(r.gitBreachRowIndex)
		r.gitBreachFilesTable.Select(cur, 0)
	}
	// Explicitly (re)fetch the diff for whatever row is now current — a
	// real, reported bug otherwise: Select() only invokes
	// SetSelectionChangedFunc when tview's own internal selectedRow
	// actually changes value, not when the *data* a stable row number
	// now refers to has changed underneath it. After any reload, a row
	// number that happened to already be selected but now points at a
	// completely different file (or, the very first time this screen
	// ever renders, a still-building row index with no file at all yet
	// — the "Working tree clean" placeholder path's own Select(1, 0),
	// which fires before a single real row has been added) would
	// otherwise leave the Main box showing a stale or empty diff
	// forever, never refreshed, since nothing else ever asked it to be.
	r.startGitBreachDiff(cur)
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

// gitBreachColorizeDiff turns a plain `git diff` into tview markup —
// per the user's own explicit request that removed lines read red and
// added ones green. Removed lines reuse theme.CriticalText verbatim
// (gitBreachRowColor's own conflict color); added lines use
// gitBreachDiffAddedColor, a dedicated brighter green — see its own
// doc comment for why added needed its own color but removed didn't
// (a muted-red follow-up request was tried and reverted). Requires
// gitBreachDiffView.SetDynamicColors(true) (see newGitBreachScreen) to
// actually render; every line is escaped first (tview.Escape — diff
// content routinely contains "[", e.g. Go's own "[]byte", which would
// otherwise be swallowed as a style tag instead of shown — the same
// reason renderSyntax already escapes file content before coloring it)
// so the diff's own text is never itself misread as markup.
//
// "+++"/"---" (the diff's own old/new file-header lines) are checked
// before the plain "+"/"-" cases below, which would otherwise also
// match their own leading character and color a file header like a
// single added/removed line.
func gitBreachColorizeDiff(diff string, theme config.ResolvedTheme) string {
	lines := strings.Split(diff, "\n")
	var b strings.Builder
	for i, line := range lines {
		escaped := tview.Escape(line)
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			b.WriteString(wrapColor(theme.MutedTextColor, escaped))
		case strings.HasPrefix(line, "+"):
			b.WriteString(wrapColor(gitBreachDiffAddedColor, escaped))
		case strings.HasPrefix(line, "-"):
			b.WriteString(wrapColor(theme.CriticalText, escaped))
		case strings.HasPrefix(line, "@@"), strings.HasPrefix(line, "diff --git"), strings.HasPrefix(line, "index "):
			b.WriteString(wrapColor(theme.MutedTextColor, escaped))
		default:
			b.WriteString(escaped)
		}
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}
