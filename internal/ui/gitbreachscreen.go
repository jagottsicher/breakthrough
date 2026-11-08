package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/git"
	"github.com/jagottsicher/breakthrough/internal/viewer"
)

// gitBreachStatusHeight is the fixed row count Status gets in the left
// column's own Flex — its own header plus a two-line summary, nothing
// more. Files/Branches/Commits/Stash, the real lists, share whatever's
// left proportionally (see newGitBreachScreen's own AddItem
// proportions) — none of them are a fixed-height stub anymore.
const gitBreachStatusHeight = 3

// gitBreachDiffAddedColor is a dedicated, brighter green for a diff's
// own added-line marker ("+") — per the user's own explicit request for
// something stronger than theme.EntryExecutable (the panel's own muted
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
//
// Only the marker glyph itself uses this color now — the rest of an
// added/removed line's own content is real per-language syntax
// coloring (see gitBreachColorizeDiff), which a single solid
// foreground color across the whole line would otherwise compete
// with. gitBreachDiffAddedBackground/gitBreachDiffRemovedBackground
// (below) are what actually marks a whole line as added/removed now,
// per the user's own explicit follow-up request.
var gitBreachDiffAddedColor = tcell.GetColor("#3cb44b")

// gitBreachDiffAddedBackground/gitBreachDiffRemovedBackground tint an
// added/removed diff line's own entire background — a muted green/red
// close in brightness to theme.SurfaceBackground (darkslategray,
// #2f4f4f, by default) and theme.PopupBackground (#263f3f), picked so
// every one of darkSyntaxPalette's own foreground colors
// (cornflowerblue, darksalmon, mediumpurple, paleturquoise, gold,
// gray, ...) still reads clearly on top — the same reasoning
// EntryExecutable's own doc comment already gives for keeping a
// background-adjacent color muted rather than bright: a vivid green/
// red wash across a whole line would fight the very syntax colors it's
// meant to sit behind. Fixed colors, not theme fields, for the same
// reason gitBreachDiffAddedColor above already is.
//
// Adjusted once already from an original pair (#1e3a28/#3a1e1e) with
// the same overall brightness as these but a muddying gray component
// mixed into the "wrong" channel — the user's own explicit report: on
// a terminal that doesn't advertise truecolor (iSH on iPadOS,
// specifically, though any 256-color terminal has the identical
// problem), tcell.FindColor's own CIE76 nearest-match degrades a dark,
// low-saturation color like that to whichever xterm-256 grayscale-ramp
// entry happens to sit closest — #1e3a28 and #3a1e1e both landed
// within one step of each other on that ramp, reading as "one is
// black, the other is gray", neither visibly red or green at all. The
// fix is saturation, not brightness: zeroing out the other two
// channels instead of raising the dominant one keeps these exactly as
// dark as before (the user's own second, equally explicit complaint,
// after a first fix that raised brightness instead — "extrem grell",
// hurting a muted/gray foreground's own readability on top) while
// giving tcell.FindColor's CIE76 search enough hue signal to prefer a
// real xterm-256 color-cube entry over the grayscale ramp. Verified
// directly against tcell.FindColor and the real 256-color palette
// (tcell.PaletteColor), not guessed: these two degrade to #5f0000 and
// #005f00 respectively — genuine, clearly distinct dark-red/dark-green
// cube entries, not the grayscale ramp — while the marker glyph itself
// (gitBreachDiffAddedColor/theme.CriticalText, unchanged) still
// contrasts visibly against its own background in both truecolor and
// degraded terminals alike.
//
// gitBreachDiffAddedBackground nudged one step darker again
// (#003a00 → #002d00) after the user's own live follow-up: a muted
// gray comment token (one of darkSyntaxPalette's own colors) still
// read as hard to make out on the green specifically, even though the
// green itself was now clearly recognizable. #002d00 is the darkest
// value that still degrades to #005f00 rather than the grayscale
// ramp — one step further (#002800) was verified to flip back to
// gray, so this sits right at that floor rather than leaving headroom
// to spare. gitBreachDiffRemovedBackground (the red) was reported as
// already fine and is untouched.
var (
	gitBreachDiffAddedBackground   = tcell.GetColor("#002d00")
	gitBreachDiffRemovedBackground = tcell.GetColor("#3a0000")
)

// newGitBreachScreen builds the whole dashboard once — see
// gitBreach.Layout's own doc comment on Root for why this is laid out
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
	r.gitBreach.TitleBar = newPlainTitleBar("Git Breach")
	r.gitBreach.TitleBar.SetMouseCapture(captureReloadCloseTitleBarMouse(r.gitBreach.TitleBar, r.reloadGitBreach, r.closeGitBreach))

	r.gitBreach.StatusHeader, r.gitBreach.StatusView = newGitBreachBox("Status")

	r.gitBreach.FilesHeader = newGitBreachBoxHeader("Files")
	// The header is its own separate TextView from the table it labels
	// — a plain click on it would otherwise do nothing at all (see
	// gitBreachBlockFocusSteal's own doc comment for why every other
	// box's header is deliberately inert instead), so it gets its own
	// mouse capture that focuses Files explicitly, per the user's own
	// explicit request that the header line itself be clickable too,
	// not just the table beneath it.
	r.gitBreach.FilesHeader.SetMouseCapture(r.gitBreachFocusFilesOnClick)
	r.gitBreach.FilesTable = tview.NewTable()
	r.gitBreach.FilesTable.SetSelectable(true, false)
	r.gitBreach.FilesTable.SetInputCapture(r.captureGitBreachFilesTableKey)
	r.gitBreach.FilesTable.SetSelectionChangedFunc(func(row, _ int) {
		r.startGitBreachFilesDiffIfOwner()
	})
	// Files, Branches and Commits are this Ausbaustufe's own three real,
	// focusable boxes — Status/Stash/Main have nothing for keyboard
	// focus to ever land on yet, so their own headers/bodies stay
	// permanently in the "inactive" look applyGitBreachTheme's own
	// initial pass already gives every box. See gitBreachFocusables for
	// the shared, data-driven list Tab cycles through — adding a fourth
	// real box later means adding it there, not touching the cycling
	// logic itself.
	r.gitBreach.FilesTable.SetFocusFunc(func() {
		r.styleGitBreachFocus(r.gitBreach.FilesHeader, r.gitBreach.FilesTable, true)
		r.gitBreach.MainOwner = gitBreachMainOwnerFiles
		r.startGitBreachFilesDiffIfOwner()
		r.refreshGitBreachHint()
	})
	r.gitBreach.FilesTable.SetBlurFunc(func() { r.styleGitBreachFocus(r.gitBreach.FilesHeader, r.gitBreach.FilesTable, false) })

	r.gitBreach.StatusHeader.SetMouseCapture(gitBreachBlockFocusSteal)
	r.gitBreach.StatusView.SetMouseCapture(gitBreachBlockFocusSteal)

	r.gitBreach.BranchesHeader = newGitBreachBoxHeader("Branches")
	r.gitBreach.BranchesHeader.SetMouseCapture(r.gitBreachFocusBranchesOnClick)
	r.gitBreach.BranchesTable = tview.NewTable()
	r.gitBreach.BranchesTable.SetSelectable(true, false)
	r.gitBreach.BranchesTable.SetInputCapture(r.captureGitBreachBranchesTableKey)
	r.gitBreach.BranchesTable.SetFocusFunc(func() {
		r.styleGitBreachFocus(r.gitBreach.BranchesHeader, r.gitBreach.BranchesTable, true)
		r.refreshGitBreachHint()
	})
	r.gitBreach.BranchesTable.SetBlurFunc(func() { r.styleGitBreachFocus(r.gitBreach.BranchesHeader, r.gitBreach.BranchesTable, false) })

	r.gitBreach.CommitsHeader = newGitBreachBoxHeader("Commits")
	// SetDrawFunc, not a width computed once at render time: the header
	// TextView's own GetRect() is still zero-width the first time
	// renderGitBreach runs (applyGitBreachTheme bakes in cell colors
	// before the dashboard has ever actually been drawn to a real
	// screen), the same "ask the widget, not the screen" fix
	// filterField's own SetDrawFunc already establishes elsewhere in
	// this app for the identical reason. Re-renders the hint on every
	// draw, not just on reload, so a live terminal resize updates the
	// padding immediately rather than only catching up on the dashboard's
	// own next reload.
	r.gitBreach.CommitsHeader.SetDrawFunc(func(_ tcell.Screen, x, y, width, height int) (int, int, int, int) {
		r.renderGitBreachCommitsHeader(width)
		return x, y, width, height
	})
	r.gitBreach.CommitsHeader.SetMouseCapture(r.gitBreachFocusCommitsOnClick)
	r.gitBreach.CommitsTable = tview.NewTable()
	r.gitBreach.CommitsTable.SetSelectable(true, false)
	r.gitBreach.CommitsTable.SetInputCapture(r.captureGitBreachCommitsTableKey)
	r.gitBreach.CommitsTable.SetSelectionChangedFunc(func(row, _ int) {
		r.startGitBreachCommitsDiffIfOwner()
		r.maybeLoadMoreGitBreachCommits(row)
	})
	r.gitBreach.CommitsTable.SetFocusFunc(func() {
		r.styleGitBreachFocus(r.gitBreach.CommitsHeader, r.gitBreach.CommitsTable, true)
		r.gitBreach.MainOwner = gitBreachMainOwnerCommits
		r.startGitBreachCommitsDiffIfOwner()
		r.refreshGitBreachHint()
	})
	r.gitBreach.CommitsTable.SetBlurFunc(func() { r.styleGitBreachFocus(r.gitBreach.CommitsHeader, r.gitBreach.CommitsTable, false) })

	r.gitBreach.StashHeader = newGitBreachBoxHeader("Stash")
	r.gitBreach.StashHeader.SetMouseCapture(r.gitBreachFocusStashOnClick)
	r.gitBreach.StashTable = tview.NewTable()
	r.gitBreach.StashTable.SetSelectable(true, false)
	r.gitBreach.StashTable.SetInputCapture(r.captureGitBreachStashTableKey)
	r.gitBreach.StashTable.SetSelectionChangedFunc(func(row, _ int) {
		r.startGitBreachStashDiffIfOwner()
	})
	r.gitBreach.StashTable.SetFocusFunc(func() {
		r.styleGitBreachFocus(r.gitBreach.StashHeader, r.gitBreach.StashTable, true)
		r.gitBreach.MainOwner = gitBreachMainOwnerStash
		r.startGitBreachStashDiffIfOwner()
		r.refreshGitBreachHint()
	})
	r.gitBreach.StashTable.SetBlurFunc(func() { r.styleGitBreachFocus(r.gitBreach.StashHeader, r.gitBreach.StashTable, false) })

	r.gitBreach.DiffHeader, r.gitBreach.DiffView = newGitBreachBox("Main — Diff")
	r.gitBreach.DiffView.SetWrap(false)
	// Dynamic colors — per the user's own explicit request that a
	// diff's own removed/added lines be colored (see
	// gitBreachColorizeDiff, which every real diff shown here goes
	// through), not left as plain text.
	r.gitBreach.DiffView.SetDynamicColors(true)
	r.gitBreach.DiffView.SetScrollable(true)
	r.gitBreach.DiffView.SetInputCapture(r.captureGitBreachMainKey)
	// SetDrawFunc, not computed once at render time: the diff view's
	// own width/height are both needed fresh on every draw to keep the
	// header's own "Page X/Y" hint correct as the user scrolls or the
	// terminal resizes, the same "ask the widget, not the screen" fix
	// gitBreach.CommitsHeader's own SetDrawFunc already establishes for
	// an identical reason.
	r.gitBreach.DiffView.SetDrawFunc(func(_ tcell.Screen, x, y, width, height int) (int, int, int, int) {
		r.renderGitBreachDiffHeader(width, height)
		return x, y, width, height
	})
	// Main is reachable via Right/Enter/Space (see
	// activateGitBreachMain) and, once focused, styled/hinted the same
	// as the four real gitBreachFocusables entries — it's deliberately
	// still not one of them itself (see toggleGitBreachFocus's own
	// owner-relative handling for why Tab/Shift-Tab from here still
	// work correctly).
	r.gitBreach.DiffView.SetFocusFunc(func() {
		r.styleGitBreachFocus(r.gitBreach.DiffHeader, r.gitBreach.DiffView, true)
		r.styleGitBreachFocus(r.gitBreach.DiffHeader, r.gitBreach.DiffSpacer, true)
		r.refreshGitBreachHint()
	})
	r.gitBreach.DiffView.SetBlurFunc(func() {
		r.styleGitBreachFocus(r.gitBreach.DiffHeader, r.gitBreach.DiffView, false)
		r.styleGitBreachFocus(r.gitBreach.DiffHeader, r.gitBreach.DiffSpacer, false)
	})
	// Not the shared gitBreachBlockFocusSteal directly — same real bug
	// as gitBreachFocus{Files,Branches,Commits,Stash}OnClick's own fix
	// (see that doc comment): SetMouseCapture's callback runs for every
	// mouse event the box's own Flex loop hands to this header, not
	// just ones actually on the header's own row, so without an
	// explicit InRect check here, EVERY click anywhere in the whole
	// Main box — including the scrollbar column gitBreach.DiffPages now
	// has, in either the unified or split view — got swallowed here
	// first and never reached it at all.
	r.gitBreach.DiffHeader.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseLeftDown && r.gitBreach.DiffHeader.InRect(event.Position()) {
			return tview.MouseConsumed, nil
		}
		return action, event
	})
	// The diff view's own MouseLeftDown is blocked the same as every
	// other passive box's (see gitBreachBlockFocusSteal), but every
	// other mouse action still passes through unchanged — the wheel
	// scroll a long diff needs still works without ever taking
	// keyboard focus away from Files. Mouse-click focus on Main itself
	// is a later Ausbaustufe (keyboard-only for this one — see
	// activateGitBreachMain).
	r.gitBreach.DiffView.SetMouseCapture(gitBreachBlockFocusSteal)

	// The split view (see gitbreachdiffsplit.go) — built right after
	// the unified pager it shares a tview.Pages with, since
	// newGitBreachDiffSplit's own AddPage call needs r.gitBreach.DiffView
	// to already exist.
	r.newGitBreachDiffSplit()

	r.gitBreach.Hint = tview.NewTextView()
	r.gitBreach.Hint.SetWrap(false)
	r.gitBreach.Hint.SetDynamicColors(true)
	r.refreshGitBreachHint()
	r.gitBreach.Hint.SetMouseCapture(r.captureListHintMouse(r.gitBreach.Hint, &r.gitBreach.HintSpans))

	// Each table gets its own scrollbar, one column of its own box's own
	// body rather than the header — wrapping just the table, not the
	// whole gitBreachBoxFlex(header, ...) result, since the header is a
	// plain TextView these four boxes already treat specially (see
	// gitBreachFocusFilesOnClick and friends) and isn't itself
	// scrollable content.
	r.gitBreach.FilesScrollbar = newVerticalScrollbarForTable(r, r.gitBreach.FilesTable)
	r.gitBreach.FilesScrollbar.SetTrackColorFunc(r.gitBreachScrollbarTrackColor(r.gitBreach.FilesTable))
	filesRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.gitBreach.FilesTable, 0, 1, true).
		AddItem(r.gitBreach.FilesScrollbar, 1, 0, false)
	filesRow.SetMouseCapture(r.gitBreach.FilesScrollbar.captureMouse)

	r.gitBreach.BranchesScrollbar = newVerticalScrollbarForTable(r, r.gitBreach.BranchesTable)
	r.gitBreach.BranchesScrollbar.SetTrackColorFunc(r.gitBreachScrollbarTrackColor(r.gitBreach.BranchesTable))
	branchesRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.gitBreach.BranchesTable, 0, 1, true).
		AddItem(r.gitBreach.BranchesScrollbar, 1, 0, false)
	branchesRow.SetMouseCapture(r.gitBreach.BranchesScrollbar.captureMouse)

	r.gitBreach.CommitsScrollbar = newVerticalScrollbarForTable(r, r.gitBreach.CommitsTable)
	r.gitBreach.CommitsScrollbar.SetTrackColorFunc(r.gitBreachScrollbarTrackColor(r.gitBreach.CommitsTable))
	commitsRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.gitBreach.CommitsTable, 0, 1, true).
		AddItem(r.gitBreach.CommitsScrollbar, 1, 0, false)
	commitsRow.SetMouseCapture(r.gitBreach.CommitsScrollbar.captureMouse)

	r.gitBreach.StashScrollbar = newVerticalScrollbarForTable(r, r.gitBreach.StashTable)
	r.gitBreach.StashScrollbar.SetTrackColorFunc(r.gitBreachScrollbarTrackColor(r.gitBreach.StashTable))
	stashRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.gitBreach.StashTable, 0, 1, true).
		AddItem(r.gitBreach.StashScrollbar, 1, 0, false)
	stashRow.SetMouseCapture(r.gitBreach.StashScrollbar.captureMouse)

	leftColumn := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(gitBreachBoxFlex(r.gitBreach.StatusHeader, r.gitBreach.StatusView), gitBreachStatusHeight, 0, false).
		AddItem(gitBreachBoxFlex(r.gitBreach.FilesHeader, filesRow), 0, 2, true).
		AddItem(gitBreachBoxFlex(r.gitBreach.BranchesHeader, branchesRow), 0, 1, false).
		AddItem(gitBreachBoxFlex(r.gitBreach.CommitsHeader, commitsRow), 0, 2, false).
		AddItem(gitBreachBoxFlex(r.gitBreach.StashHeader, stashRow), 0, 1, false)

	body := tview.NewFlex().
		AddItem(leftColumn, 0, 1, true).
		AddItem(gitBreachBoxFlex(r.gitBreach.DiffHeader, r.gitBreach.DiffPages), 0, 2, false)

	r.gitBreach.Layout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.gitBreach.TitleBar, 1, 0, false).
		AddItem(body, 0, 1, true).
		AddItem(r.gitBreach.Hint, 1, 0, false)
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
//
// The InRect check matters more than it looks: SetMouseCapture's own
// callback runs unconditionally for every mouse event this box's own
// Flex loop ever hands to the header (see WrapMouseHandler), not just
// ones actually landing on the header's own one-row strip — without
// this check, any MouseLeftDown anywhere in the whole box (including a
// click on the table's own rows, or — the real, user-reported bug this
// fixed — the scrollbar column added alongside the table) got swallowed
// here first and never reached whatever was actually clicked.
func (r *Root) gitBreachFocusFilesOnClick(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action == tview.MouseLeftDown && r.gitBreach.FilesHeader.InRect(event.Position()) {
		r.app.SetFocus(r.gitBreach.FilesTable)
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

// gitBreachFocusedBodyLighten is how much brighter the currently
// focused box's own body reads than PopupBackground alone already
// gives it — per the user's own explicit follow-up report that the
// header color change plus PopupBackground's own already-subtle step
// up from PanelBackground (by design: it's a shared "distinct
// secondary surface" tone, not specifically a strong focus signal) left
// which box actually has focus too hard to tell at a glance. Applied on
// top of whatever PopupBackground the active color scheme happens to
// define, rather than a fixed replacement color, so this keeps working
// (and keeps reading as "one step brighter than this scheme's own
// surface tone") regardless of which scheme is active, light or dark.
const gitBreachFocusedBodyLighten = 28

// lightenColor returns c with each RGB channel raised by delta (clamped
// at 255) — a plain, scheme-independent "one shade lighter" step, not
// tied to any one color scheme's own specific values the way a second,
// hardcoded replacement color would be.
func lightenColor(c tcell.Color, delta int32) tcell.Color {
	r, g, b := c.RGB()
	lighten := func(ch int32) int32 {
		ch += delta
		if ch > 255 {
			return 255
		}
		return ch
	}
	return tcell.NewRGBColor(lighten(r), lighten(g), lighten(b))
}

// styleGitBreachFocus recolors one box's own header/body pair for
// whether it currently has real keyboard focus — the same two-state
// header scheme toolWindow's/Details' own title bars already establish
// (InputFocusedBackground while focused, InputBackground while not),
// per the user's own explicit request that every box in this dashboard
// follow it too, instead of the bordered-box look this screen started
// with. The body itself also tints (PopupBackground, this app's own
// already-established "distinct secondary surface" role — see
// log.TimelineView's own doc comment for the same reasoning
// applied there — vs. the baseline SurfaceBackground every box's body
// otherwise sits on) per the user's own further, explicitly optional
// request that an active box read as visually distinct beyond just its
// own header line.
// gitBreachScrollbarTrackColor mirrors styleGitBreachFocus's own
// focused/blurred background decision for content's own scrollbar —
// see verticalScrollbar.trackColor's own doc comment for why this has
// to be handed in rather than read back from content itself. content
// is whatever box body a scrollbar sits next to — a *tview.Table for
// the four catalog boxes, a *tview.TextView for Main's own unified/
// split diff views. Checked fresh on every Draw (content.HasFocus()),
// not cached, so it always agrees with whatever styleGitBreachFocus
// most recently set, through Tab/Shift-Tab, a mouse click, or a live
// theme switch alike.
func (r *Root) gitBreachScrollbarTrackColor(content tview.Primitive) func() tcell.Color {
	return func() tcell.Color {
		if content.HasFocus() {
			return lightenColor(r.theme.PopupBackground, gitBreachFocusedBodyLighten)
		}
		return r.theme.PanelBackground
	}
}

func (r *Root) styleGitBreachFocus(header *tview.TextView, body gitBreachBackgroundSetter, focused bool) {
	if focused {
		header.SetBackgroundColor(r.theme.InputFocusedBackground)
		body.SetBackgroundColor(lightenColor(r.theme.PopupBackground, gitBreachFocusedBodyLighten))
	} else {
		header.SetBackgroundColor(r.theme.InputBackground)
		// PanelBackground, not SurfaceBackground: every box's own body
		// now rests on PanelBackground by default (see applyGitBreachTheme's
		// own comment on gitBreach.Layout), so blurring one has to fall
		// back to that same color — SurfaceBackground here silently
		// undid the whole-screen change the moment any box lost focus,
		// a real, live-confirmed regression.
		body.SetBackgroundColor(r.theme.PanelBackground)
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
	if r.gitBreach.Layout == nil {
		return
	}
	// PanelBackground, not SurfaceBackground — see mounts.Layout's own
	// identical comment (mounts.go) for the full reasoning.
	r.gitBreach.Layout.SetBackgroundColor(theme.PanelBackground)
	r.gitBreach.TitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.gitBreach.TitleBar.SetTextColor(theme.TextColor)
	// SurfaceBackground/TextColor, matching the main screen's own
	// buttonBar/statusBar — see mounts.Hint's own identical comment.
	r.gitBreach.Hint.SetBackgroundColor(theme.SurfaceBackground)
	r.gitBreach.Hint.SetTextColor(theme.TextColor)
	r.refreshGitBreachHint()

	headers := []*tview.TextView{
		r.gitBreach.StatusHeader, r.gitBreach.FilesHeader, r.gitBreach.BranchesHeader,
		r.gitBreach.CommitsHeader, r.gitBreach.StashHeader, r.gitBreach.DiffHeader,
	}
	for _, h := range headers {
		h.SetBackgroundColor(theme.InputBackground)
		h.SetTextColor(theme.TextColor)
	}

	textBodies := []*tview.TextView{
		r.gitBreach.StatusView, r.gitBreach.DiffView,
		r.gitBreach.DiffSplitLeft, r.gitBreach.DiffSplitRight,
	}
	for _, v := range textBodies {
		// PanelBackground, not SurfaceBackground — see gitBreach.Layout's
		// own comment above for the full reasoning.
		v.SetBackgroundColor(theme.PanelBackground)
		v.SetTextColor(theme.Text)
	}
	r.gitBreach.DiffSplitSeparator.SetBackgroundColor(theme.PanelBackground)
	r.gitBreach.DiffSpacer.SetBackgroundColor(theme.PanelBackground)
	r.gitBreach.DiffSplitSpacer.SetBackgroundColor(theme.PanelBackground)
	if r.gitBreach.DiffRawText != "" {
		r.gitBreachRenderDiffSplit()
	}
	// PanelBackground, not SurfaceBackground — see gitBreach.Layout's own
	// comment above for the full reasoning.
	r.gitBreach.FilesTable.SetBackgroundColor(theme.PanelBackground)
	r.gitBreach.BranchesTable.SetBackgroundColor(theme.PanelBackground)
	r.gitBreach.CommitsTable.SetBackgroundColor(theme.PanelBackground)
	r.gitBreach.StashTable.SetBackgroundColor(theme.PanelBackground)

	// These tables mix several per-cell text colors (Staged/Unstaged/
	// Untracked rows each carry their own gitBreachRowColor; Branches'
	// current-branch row carries EntryExecutable) — without an explicit
	// SetSelectedStyle, tview's own default selection rendering reverses
	// whatever foreground a cell already has into the highlight's own
	// background, so the "selected row" color silently changes from row
	// to row instead of reading as one consistent selection bar. Same
	// fix, same reasoning, as ssh.KeysTable/connectionMenuTable/
	// sed.PreviewTable/tabSwitcher already apply.
	selStyle := tcell.StyleDefault.Background(theme.SelectionBackground).Foreground(theme.TextColor)
	r.gitBreach.FilesTable.SetSelectedStyle(selStyle)
	r.gitBreach.BranchesTable.SetSelectedStyle(selStyle)
	r.gitBreach.CommitsTable.SetSelectedStyle(selStyle)
	r.gitBreach.StashTable.SetSelectedStyle(selStyle)

	// Re-assert whichever box actually has real keyboard focus right now
	// on top of the uniform "inactive" pass above — the same "a live
	// theme switch must not silently lose a focus-dependent look" case
	// this function's own doc comment already gives. Main included: it
	// can hold real focus too (see activateGitBreachMain) but is
	// deliberately never one of gitBreachFocusables' own entries, so it
	// needs its own explicit check here rather than being covered by
	// the loop below.
	if r.gitBreachMainHasFocus() {
		if r.gitBreach.DiffSplit {
			r.styleGitBreachFocus(r.gitBreach.DiffHeader, r.gitBreach.DiffSplitLeft, true)
			r.styleGitBreachFocus(r.gitBreach.DiffHeader, r.gitBreach.DiffSplitRight, true)
			r.styleGitBreachFocus(r.gitBreach.DiffHeader, r.gitBreach.DiffSplitSpacer, true)
		} else {
			r.styleGitBreachFocus(r.gitBreach.DiffHeader, r.gitBreach.DiffView, true)
			r.styleGitBreachFocus(r.gitBreach.DiffHeader, r.gitBreach.DiffSpacer, true)
		}
	}
	for _, f := range r.gitBreachFocusables() {
		if f.body.HasFocus() {
			r.styleGitBreachFocus(f.header, f.body, true)
		}
	}

	r.renderGitBreach() // cell colors baked in per cell, not looked up live at draw time
}

// gitBreachHintEntries builds the hint bar's own entries for whichever
// box currently has real keyboard focus — per the user's own explicit
// point: Space (stage/unstage) only ever acts on a Files row, Enter
// only ever checks out a Branches row, and Enter/Space/Right only ever
// jump into Main from Commits specifically (see activateGitBreachMain's
// own doc comment for why Files/Stash don't also get Enter/Space), so
// advertising any of them while a different box has focus would show a
// key the keyboard wouldn't actually do that with. "c" (commit) and "r"
// (reload) are different: neither depends on which row is selected
// anywhere, so both work regardless of focus (see their own handling in
// every table's and Main's own InputCapture) and stay in the hint bar
// unconditionally, same as Tab. Esc is the one entry whose own label
// changes instead of merely appearing/disappearing: it closes Git
// breach everywhere except while Main has focus, where it only steps
// back out of the diff (see captureGitBreachMainKey's own doc comment).
func (r *Root) gitBreachHintEntries() []listHintEntry {
	entries := []listHintEntry{
		hintKey("Tab/Shift-Tab", "switch box", func(r *Root) { r.toggleGitBreachFocus() }),
	}
	switch {
	case r.gitBreach.FilesTable.HasFocus():
		entries = append(entries,
			hintKey("Space", "stage/unstage", func(r *Root) { r.toggleGitBreachStage() }),
			hintKey("b", "blame", func(r *Root) { r.startGitBreachBlame() }),
			hintKey("→", "view diff", func(r *Root) { r.activateGitBreachMain() }),
		)
	case r.gitBreach.BranchesTable.HasFocus():
		entries = append(entries, hintKey("←/→", "switch tab", func(r *Root) { r.toggleGitBreachBranchesTab() }))
		if r.gitBreach.BranchesShowTags {
			entries = append(entries,
				hintKey("a", "new tag", func(r *Root) { r.openGitBreachCreateTag() }),
				hintKey("d", "delete tag", func(r *Root) { r.openGitBreachDeleteTag() }),
			)
		} else {
			entries = append(entries,
				hintKey("Enter", "checkout", func(r *Root) { r.openGitBreachCheckout() }),
				hintKey("d", "delete branch", func(r *Root) { r.openGitBreachDeleteBranch() }),
			)
		}
	case r.gitBreach.CommitsTable.HasFocus():
		entries = append(entries,
			hintKey("Enter/Space/→", "view diff", func(r *Root) { r.activateGitBreachMain() }),
			hintKey("d", "diff against ref...", func(r *Root) { r.openGitBreachDiffAgainstRef() }),
		)
		if r.gitBreachBisectAvailable() {
			entries = append(entries, hintKey("B", "bisect start...", func(r *Root) { r.openGitBreachBisectStart() }))
		} else {
			entries = append(entries,
				hintKey("G", "mark good", func(r *Root) { r.openGitBreachBisectMark("good") }),
				hintKey("X", "mark bad", func(r *Root) { r.openGitBreachBisectMark("bad") }),
				hintKey("E", "end bisect", func(r *Root) { r.openGitBreachBisectEnd() }),
			)
		}
	case r.gitBreach.StashTable.HasFocus():
		entries = append(entries,
			hintKey("a", "apply stash", func(r *Root) { r.openGitBreachStashApply() }),
			hintKey("d", "drop stash", func(r *Root) { r.openGitBreachStashDrop() }),
			hintKey("→", "view diff", func(r *Root) { r.activateGitBreachMain() }),
		)
	case r.gitBreachMainHasFocus():
		entries = append(entries,
			listHintEntry{
				keys: []listHintKey{
					{"↑", simulateKeyOnFocused(tcell.KeyUp)},
					{"↓", simulateKeyOnFocused(tcell.KeyDown)},
					{"PgUp", simulateKeyOnFocused(tcell.KeyPgUp)},
					{"PgDn", simulateKeyOnFocused(tcell.KeyPgDn)},
				},
				label: "scroll diff",
			},
		)
		if r.gitBreach.DiffSplit {
			entries = append(entries,
				listHintEntry{
					keys: []listHintKey{
						{"←", simulateKeyOnFocused(tcell.KeyLeft)},
						{"→", simulateKeyOnFocused(tcell.KeyRight)},
					},
					label: "scroll sideways (← at the left edge leaves)",
				},
				hintKey("Enter/Space", "back", func(r *Root) { r.deactivateGitBreachMain() }),
				hintKey("v", "unified view", func(r *Root) { r.toggleGitBreachDiffSplit() }),
			)
		} else {
			entries = append(entries,
				hintKey("←/Enter/Space", "back", func(r *Root) { r.deactivateGitBreachMain() }),
				hintKey("v", "side by side", func(r *Root) { r.toggleGitBreachDiffSplit() }),
			)
		}
		if r.gitBreachMainShowsAStageableHunk() {
			entries = append(entries, hintKey("s", "stage hunk", func(r *Root) { r.openGitBreachStageHunk() }))
		}
	}
	entries = append(entries,
		hintKey("c", "commit", func(r *Root) { r.openGitBreachCommitPrompt() }),
		hintKey("r", "reload", func(r *Root) { r.reloadGitBreach() }),
	)
	if r.gitBreachMainHasFocus() {
		entries = append(entries, hintKey("Esc", "back", func(r *Root) { r.deactivateGitBreachMain() }))
	} else {
		entries = append(entries, hintKey("Esc", "close", func(r *Root) { r.closeGitBreach() }))
	}
	return entries
}

// refreshGitBreachHint rebuilds the hint bar's own text/spans from
// gitBreachHintEntries' own current, focus-dependent list — called
// whenever which box has focus changes (every table's own FocusFunc),
// not just on open/theme-apply, so the hint bar never shows a stale set
// of keys for whichever box the cursor just left.
func (r *Root) refreshGitBreachHint() {
	hintText, hintSpans := buildListHint(r.theme, r.gitBreachHintEntries())
	r.gitBreach.Hint.SetText(hintText)
	r.gitBreach.HintSpans = hintSpans
}

// gitBreachFocusable pairs one box's own header with its body — both
// *tview.Table and *tview.TextView already satisfy this (every real
// tview.Primitive does, plus SetBackgroundColor from the embedded
// Box), so the same pairing styleGitBreachFocus already colors works
// for either concrete type without its own switch.
type gitBreachFocusable interface {
	tview.Primitive
	gitBreachBackgroundSetter
}

type gitBreachFocusEntry struct {
	header *tview.TextView
	body   gitBreachFocusable
}

// gitBreachFocusables is the ordered list Tab cycles through (see
// toggleGitBreachFocus) and applyGitBreachTheme's own "which box
// currently has focus" re-check — Files, Branches, Commits and Stash,
// every real, focusable box this dashboard has today. A later
// Ausbaustufe giving some other view (Worktrees, Remotes, ...) real,
// navigable content adds its own entry here and nowhere else — the
// whole point of making this a function returning a slice rather than
// a fixed toggle.
func (r *Root) gitBreachFocusables() []gitBreachFocusEntry {
	return []gitBreachFocusEntry{
		{r.gitBreach.FilesHeader, r.gitBreach.FilesTable},
		{r.gitBreach.BranchesHeader, r.gitBreach.BranchesTable},
		{r.gitBreach.CommitsHeader, r.gitBreach.CommitsTable},
		{r.gitBreach.StashHeader, r.gitBreach.StashTable},
	}
}

// toggleGitBreachFocus is "Tab" — advances real keyboard focus to the
// next entry in gitBreachFocusables, wrapping back to the first past
// the last. Falls back to focusing the first entry outright if nothing
// in the list currently has focus (shouldn't happen in practice, but a
// real fallback costs nothing and avoids a silent no-op if it ever
// does).
func (r *Root) toggleGitBreachFocus() {
	focusables := r.gitBreachFocusables()
	if len(focusables) == 0 {
		return
	}
	if r.gitBreachMainHasFocus() {
		r.app.SetFocus(r.gitBreachFocusNext(focusables, r.gitBreachMainOwnerIndex()))
		return
	}
	for i, f := range focusables {
		if f.body.HasFocus() {
			r.app.SetFocus(r.gitBreachFocusNext(focusables, i))
			return
		}
	}
	r.app.SetFocus(focusables[0].body)
}

// gitBreachFocusNext/gitBreachFocusPrev are toggleGitBreachFocus/
// reverseToggleGitBreachFocus's own shared "advance by one, with
// wraparound" step — redirecting into the AI Consigliere sidebar
// ("gC", see startConsigliereChord in consigliere.go) instead of
// wrapping back around to the opposite end of gitBreachFocusables,
// whenever it's actually open. Without this, Tab/Backtab could never
// reach consigliere.Input/consigliere.History from here at all: the
// global focus cycle excludes them while any full-screen modal is
// active (see focusCycleStops' own doc comment in detailssidebar.go),
// so each host screen that can open the sidebar is responsible for
// its own entry point into it. consigliereExitFocus (see
// consigliere.go) is the matching way back out, landing on whatever
// had focus here when the sidebar was opened — not necessarily index
// i again, the same documented, accepted imprecision every other host
// screen's own entry point already has.
func (r *Root) gitBreachFocusNext(focusables []gitBreachFocusEntry, i int) tview.Primitive {
	if i == len(focusables)-1 && r.consigliere.Visible {
		return r.consigliere.Input
	}
	return focusables[(i+1)%len(focusables)].body
}

func (r *Root) gitBreachFocusPrev(focusables []gitBreachFocusEntry, i int) tview.Primitive {
	if i == 0 && r.consigliere.Visible {
		if r.consigliere.Scrollbar.maxOffset() > 0 {
			return r.consigliere.History
		}
		return r.consigliere.Input
	}
	return focusables[(i-1+len(focusables))%len(focusables)].body
}

// reverseToggleGitBreachFocus is "Shift-Tab" (tcell.KeyBacktab) — the
// mirror image of toggleGitBreachFocus, stepping to the previous entry
// in gitBreachFocusables with wraparound instead of the next one.
func (r *Root) reverseToggleGitBreachFocus() {
	focusables := r.gitBreachFocusables()
	if len(focusables) == 0 {
		return
	}
	if r.gitBreachMainHasFocus() {
		r.app.SetFocus(r.gitBreachFocusPrev(focusables, r.gitBreachMainOwnerIndex()))
		return
	}
	for i, f := range focusables {
		if f.body.HasFocus() {
			r.app.SetFocus(r.gitBreachFocusPrev(focusables, i))
			return
		}
	}
	r.app.SetFocus(focusables[len(focusables)-1].body)
}

// gitBreachMainOwnerIndex maps gitBreach.MainOwner to its own position
// in gitBreachFocusables' own fixed order (Files, Branches, Commits,
// Stash) — Tab/Shift-Tab from Main (see captureGitBreachMainKey) step
// from here exactly as if they'd been pressed from the owning box
// itself, the same "Main sits spatially at its owner's position in the
// ring" model toggleGitBreachFocus/reverseToggleGitBreachFocus's own
// Main-focused branch already relies on. Must stay in sync with
// gitBreachFocusables' own order; there's no Branches case since
// Branches never owns Main (see gitBreachMainOwnerKind) and so can
// never be the value read here.
func (r *Root) gitBreachMainOwnerIndex() int {
	switch r.gitBreach.MainOwner {
	case gitBreachMainOwnerCommits:
		return 2
	case gitBreachMainOwnerStash:
		return 3
	default: // gitBreachMainOwnerFiles
		return 0
	}
}

// activateGitBreachMain gives the Main — Diff box real keyboard focus
// — reachable via Right from Files/Commits/Stash (wherever there's a
// diff to look at) and additionally via Enter/Space from Commits only:
// Space already stages a row in Files, and Stash's own diff is already
// fully visible the moment a row is selected with "a"/"d" as its real
// actions, so a second way in would add nothing there — Commits is the
// one box with neither collision nor an already-visible full diff
// worth jumping into. Main deliberately never becomes a
// gitBreachFocusables entry of its own; it's reached spatially instead,
// the same way lazygit's own diff pane works, with
// deactivateGitBreachMain as the way back.
func (r *Root) activateGitBreachMain() {
	if r.gitBreach.DiffSplit {
		r.app.SetFocus(r.gitBreach.DiffSplitLeft)
		return
	}
	r.app.SetFocus(r.gitBreach.DiffView)
}

// gitBreachMainHasFocus reports whether Main currently holds real
// keyboard focus — either sub-view (see toggleGitBreachDiffSplit):
// gitBreach.DiffSplitRight never counts, since it never actually holds
// focus itself (see newGitBreachDiffSplit's own doc comment).
func (r *Root) gitBreachMainHasFocus() bool {
	focus := r.app.GetFocus()
	return focus == r.gitBreach.DiffView || focus == r.gitBreach.DiffSplitLeft
}

// deactivateGitBreachMain returns focus to whichever box currently
// owns Main's content (gitBreach.MainOwner) — Left, Enter, Space or Esc
// while Main has focus (see captureGitBreachMainKey), the uniform way
// back regardless of which key originally activated Main.
func (r *Root) deactivateGitBreachMain() {
	r.app.SetFocus(r.gitBreachFocusables()[r.gitBreachMainOwnerIndex()].body)
}

// captureGitBreachMainKey is Main's own InputCapture, once it can
// actually hold real keyboard focus. Only the keys that mean something
// different here than tview.TextView's own defaults are intercepted:
// Left/Enter/Space/Esc leave (Esc leaves without closing Git breach,
// unlike Esc everywhere else in this dashboard — the one
// context-dependent exception, since here it's "back out of the diff",
// not "close the dashboard"), Tab/Backtab continue the ring from the
// owning box's own position, "c"/"r" keep working here too since both
// are already documented as focus-independent, "g" starts the AI
// Consigliere chord (see below — a deliberate exception to the
// paragraph that follows, not an oversight), and "s" stages whichever
// hunk is at the top of the visible area (see
// openGitBreachStageHunk — a no-op unless Files' own unstaged diff is
// what's actually showing). Everything else
// (Up/Down/PgUp/PgDn/Right/Home/End, "G", and TextView's own vim-style
// h/j/k/l) is left untouched and falls through to tview.TextView's own
// already-correct scrolling — no forwarding hack needed now that Main
// can be the real focus target directly (compare the pre-focus-model
// approach this superseded, scrollGitBreachDiff, now removed
// entirely).
func (r *Root) captureGitBreachMainKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape, tcell.KeyEnter, tcell.KeyLeft:
		r.deactivateGitBreachMain()
		return nil
	case tcell.KeyTab:
		// gitBreachFocusNext, not the raw modulo arithmetic this used
		// to inline directly: redirects into the AI Consigliere
		// sidebar instead of wrapping back to Files once Main's own
		// owner is the last entry — see its own doc comment.
		r.app.SetFocus(r.gitBreachFocusNext(r.gitBreachFocusables(), r.gitBreachMainOwnerIndex()))
		return nil
	case tcell.KeyBacktab:
		r.app.SetFocus(r.gitBreachFocusPrev(r.gitBreachFocusables(), r.gitBreachMainOwnerIndex()))
		return nil
	case tcell.KeyRune:
		switch event.Rune() {
		case ' ':
			r.deactivateGitBreachMain()
			return nil
		case 'c':
			r.openGitBreachCommitPrompt()
			return nil
		case 'r':
			r.reloadGitBreach()
			return nil
		case 'v':
			r.toggleGitBreachDiffSplit()
			return nil
		case 's':
			r.openGitBreachStageHunk()
			return nil
		case 'g':
			// "gC" reaches the AI Consigliere — see
			// startConsigliereChord's own doc comment in
			// consigliere.go. A genuine, deliberate trade-off here, not
			// a free win the way it is on every pure *tview.Table pane
			// in this file: tview's own built-in vi-style bare "g"
			// jumps to the very top of a potentially long diff, and
			// this capture's own comment above used to document that as
			// correct, valued behavior. "G" (bottom) and every other
			// scroll key are untouched — only this one jump is the
			// price.
			r.startConsigliereChord(r.gitBreachFocusables()[0].body)
			return nil
		}
	}
	return event
}

// captureGitBreachFilesTableKey: Tab switches to the next box, Space
// toggles stage/unstage, "b" shows blame, "c" commits, "r" reloads,
// Escape closes — the
// same per-table InputCapture shape captureLogAuditSelectionTableKey
// already establishes elsewhere in this package. Tab is intercepted
// explicitly rather than left to bubble: tview's own Table.InputHandler
// treats KeyTab as a "done" key (calls SetDoneFunc, which this table
// has none of, then simply returns) rather than passing it through —
// verified directly against table.go, not guessed — so Tab would
// otherwise silently do nothing at all instead of switching boxes.
func (r *Root) captureGitBreachFilesTableKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeGitBreach()
		return nil
	}
	if event.Key() == tcell.KeyTab {
		r.toggleGitBreachFocus()
		return nil
	}
	if event.Key() == tcell.KeyBacktab {
		r.reverseToggleGitBreachFocus()
		return nil
	}
	if event.Key() == tcell.KeyRight {
		r.activateGitBreachMain()
		return nil
	}
	if event.Key() == tcell.KeyRune {
		switch event.Rune() {
		case 'g':
			// "gC" reaches the AI Consigliere — see
			// startConsigliereChord's own doc comment in
			// consigliere.go. gitBreach.FilesTable is a *tview.Table,
			// which has no built-in meaning for a bare 'g' (unlike
			// Main's own *tview.TextView — see
			// captureGitBreachMainKey's own case), so nothing existing
			// is lost by claiming it here.
			r.startConsigliereChord(r.gitBreachFocusables()[0].body)
			return nil
		case ' ':
			r.toggleGitBreachStage()
			return nil
		case 'b':
			r.startGitBreachBlame()
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

// captureGitBreachBranchesTableKey: Tab switches to the next box,
// Left/Right switch this box's own two sub-tabs (Branches/Tags — see
// toggleGitBreachBranchesTab), free on this table either way since
// activateGitBreachMain is deliberately never wired here (Branches
// never owns Main, see gitBreachMainOwnerKind). On the Branches
// sub-tab: Enter checks out the branch under the cursor, "d" deletes
// it (git branch -d only — see its own doc comment for why not -D).
// On the Tags sub-tab instead: "a" creates a new tag, "d" deletes the
// one under the cursor — Enter does nothing there (a tag has no
// equivalent "make this the active one" action the way a branch
// checkout is). "r" reloads and Escape closes either way.
func (r *Root) captureGitBreachBranchesTableKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeGitBreach()
		return nil
	}
	if event.Key() == tcell.KeyTab {
		r.toggleGitBreachFocus()
		return nil
	}
	if event.Key() == tcell.KeyBacktab {
		r.reverseToggleGitBreachFocus()
		return nil
	}
	if event.Key() == tcell.KeyLeft || event.Key() == tcell.KeyRight {
		r.toggleGitBreachBranchesTab()
		return nil
	}
	if event.Key() == tcell.KeyEnter {
		if !r.gitBreach.BranchesShowTags {
			r.openGitBreachCheckout()
		}
		return nil
	}
	if event.Key() == tcell.KeyRune {
		switch event.Rune() {
		case 'r':
			r.reloadGitBreach()
			return nil
		case 'c':
			r.openGitBreachCommitPrompt()
			return nil
		case 'a':
			if r.gitBreach.BranchesShowTags {
				r.openGitBreachCreateTag()
			}
			return nil
		case 'd':
			if r.gitBreach.BranchesShowTags {
				r.openGitBreachDeleteTag()
			} else {
				r.openGitBreachDeleteBranch()
			}
			return nil
		case 'g':
			// "gC" reaches the AI Consigliere — see
			// startConsigliereChord's own doc comment in
			// consigliere.go and captureGitBreachFilesTableKey's own
			// identical case (gitBreach.BranchesTable is also a plain
			// *tview.Table).
			r.startConsigliereChord(r.gitBreachFocusables()[0].body)
			return nil
		}
	}
	return event
}

// gitBreachFocusBranchesOnClick is the Branches header's own mouse
// capture — clicking the header line itself focuses the table, the
// same as gitBreachFocusFilesOnClick already does for Files' own
// header (see its own doc comment for why a header needs this at all:
// it's its own separate TextView from the table it labels).
func (r *Root) gitBreachFocusBranchesOnClick(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action == tview.MouseLeftDown && r.gitBreach.BranchesHeader.InRect(event.Position()) {
		r.app.SetFocus(r.gitBreach.BranchesTable)
		return tview.MouseConsumed, nil
	}
	return action, event
}

// captureGitBreachCommitsTableKey: Tab switches to the next box, "r"
// reloads, "c" opens the commit-message prompt (per the user's own
// explicit point: committing what's already staged has nothing to do
// with which row is selected here, so it works regardless of focus,
// unlike Space/Enter which only ever act on a Files/Branches row),
// "d" diffs the row under the cursor against a picked ref (see
// openGitBreachDiffAgainstRef), "B" starts a bisect with the row
// under the cursor as "good" (only while none is already running —
// see gitBreachBisectAvailable), "G"/"X" mark the current bisect
// step good/bad and "E" ends it (all three only while one actually
// is running — see git.BisectInProgress), Escape closes — no Enter
// action beyond jumping into Main, since this first Ausbaustufe only
// ever shows a commit's own diff there (via Commits' own
// SetSelectionChangedFunc/FocusFunc), with no other action a commit
// row itself triggers (checkout/revert/cherry-pick/reset are
// explicitly later Ausbaustufen — see feature_ideas.txt's own list).
func (r *Root) captureGitBreachCommitsTableKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeGitBreach()
		return nil
	}
	if event.Key() == tcell.KeyTab {
		r.toggleGitBreachFocus()
		return nil
	}
	if event.Key() == tcell.KeyBacktab {
		r.reverseToggleGitBreachFocus()
		return nil
	}
	if event.Key() == tcell.KeyRight || event.Key() == tcell.KeyEnter {
		r.activateGitBreachMain()
		return nil
	}
	if event.Key() == tcell.KeyRune {
		switch event.Rune() {
		case ' ':
			r.activateGitBreachMain()
			return nil
		case 'r':
			r.reloadGitBreach()
			return nil
		case 'c':
			r.openGitBreachCommitPrompt()
			return nil
		case 'd':
			r.openGitBreachDiffAgainstRef()
			return nil
		case 'B':
			r.openGitBreachBisectStart()
			return nil
		case 'G':
			r.openGitBreachBisectMark("good")
			return nil
		case 'X':
			r.openGitBreachBisectMark("bad")
			return nil
		case 'E':
			r.openGitBreachBisectEnd()
			return nil
		case 'g':
			// "gC" reaches the AI Consigliere — see
			// startConsigliereChord's own doc comment in
			// consigliere.go and captureGitBreachFilesTableKey's own
			// identical case (gitBreach.CommitsTable is also a plain
			// *tview.Table).
			r.startConsigliereChord(r.gitBreachFocusables()[0].body)
			return nil
		}
	}
	return event
}

// gitBreachFocusCommitsOnClick mirrors gitBreachFocusBranchesOnClick
// exactly, for Commits' own header.
func (r *Root) gitBreachFocusCommitsOnClick(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action == tview.MouseLeftDown && r.gitBreach.CommitsHeader.InRect(event.Position()) {
		r.app.SetFocus(r.gitBreach.CommitsTable)
		return tview.MouseConsumed, nil
	}
	return action, event
}

// captureGitBreachStashTableKey mirrors captureGitBreachCommitsTableKey,
// "c" included, plus "a" (apply the stash under the cursor, confirming
// first only on a dirty working tree) and "d" (drop it, always
// confirming — see openGitBreachStashApply/openGitBreachStashDrop's own
// doc comments). Deliberately no "pop": see git.StashApply's own doc
// comment for why apply and drop stay two separate, explicit actions.
func (r *Root) captureGitBreachStashTableKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeGitBreach()
		return nil
	}
	if event.Key() == tcell.KeyTab {
		r.toggleGitBreachFocus()
		return nil
	}
	if event.Key() == tcell.KeyBacktab {
		r.reverseToggleGitBreachFocus()
		return nil
	}
	if event.Key() == tcell.KeyRight {
		r.activateGitBreachMain()
		return nil
	}
	if event.Key() == tcell.KeyRune {
		switch event.Rune() {
		case 'r':
			r.reloadGitBreach()
			return nil
		case 'c':
			r.openGitBreachCommitPrompt()
			return nil
		case 'a':
			r.openGitBreachStashApply()
			return nil
		case 'd':
			r.openGitBreachStashDrop()
			return nil
		case 'g':
			// "gC" reaches the AI Consigliere — see
			// startConsigliereChord's own doc comment in
			// consigliere.go and captureGitBreachFilesTableKey's own
			// identical case (gitBreach.StashTable is also a plain
			// *tview.Table).
			r.startConsigliereChord(r.gitBreachFocusables()[0].body)
			return nil
		}
	}
	return event
}

// gitBreachFocusStashOnClick mirrors gitBreachFocusCommitsOnClick
// exactly, for Stash's own header.
func (r *Root) gitBreachFocusStashOnClick(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action == tview.MouseLeftDown && r.gitBreach.StashHeader.InRect(event.Position()) {
		r.app.SetFocus(r.gitBreach.StashTable)
		return tview.MouseConsumed, nil
	}
	return action, event
}

// renderGitBreach refreshes every box from r.gitBreach.Status/Rows —
// called by reloadGitBreach after every fetch, stage, unstage, and
// commit, the same "re-render from the current state, never patch it
// in place" convention every other screen in this package follows.
func (r *Root) renderGitBreach() {
	title := " Git Breach"
	if r.gitBreach.Root != "" {
		title += " — " + r.gitBreach.Root
	}
	renderReloadCloseTitleBar(r.gitBreach.TitleBar, title+" ", r.lastScreenWidth)

	r.renderGitBreachStatus()
	r.renderGitBreachFiles()
	r.renderGitBreachBranches()
	r.renderGitBreachCommits()
	r.renderGitBreachStash()
}

// renderGitBreachStatus fills the Status box — branch, ahead/behind,
// and a one-line severity summary that mirrors gitStatusColor's own
// severity language (conflicts outrank being dirty, which outranks
// clean) applied to a line of text instead of a status-bar color.
func (r *Root) renderGitBreachStatus() {
	st := r.gitBreach.Status
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

	if r.gitBreach.FetchErr != nil {
		lines = append(lines, r.gitBreach.FetchErr.Error())
	}

	r.gitBreach.StatusView.SetText(strings.Join(lines, "\n"))
}

// renderGitBreachFiles rebuilds the Files table: one bold, non-
// selectable header row per non-empty section ("Staged (n)" etc.,
// Conflicts first — the same "most urgent first" ordering
// gitStatusColor's own severity language already establishes, ahead of
// Staged/Unstaged/Untracked, which otherwise follow git's own usual
// reading order), each section's own rows beneath it. gitBreach.RowIndex
// maps each resulting table row back to its own index in
// r.gitBreach.Rows — a section header row has no entry in it at all, so
// gitBreachRowAt correctly reports "no row" for one rather than
// pointing at the wrong file.
func (r *Root) renderGitBreachFiles() {
	r.gitBreach.FilesTable.Clear()
	r.gitBreach.RowIndex = map[int]int{}

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
		for i, gr := range r.gitBreach.Rows {
			if gr.kind == sec.kind {
				indices = append(indices, i)
			}
		}
		if len(indices) == 0 {
			continue
		}

		r.gitBreach.FilesTable.SetCell(row, 0,
			tview.NewTableCell(fmt.Sprintf("%s (%d)", sec.label, len(indices))).
				SetTextColor(r.theme.Text).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false))
		row++

		for _, idx := range indices {
			gr := r.gitBreach.Rows[idx]
			r.gitBreach.FilesTable.SetCell(row, 0,
				tview.NewTableCell(gitBreachRowText(gr)).SetTextColor(gitBreachRowColor(gr, r.theme)))
			r.gitBreach.RowIndex[row] = idx
			row++
		}
	}

	if r.gitBreach.FetchErr != nil {
		showTablePlaceholder(r.gitBreach.FilesTable, r.gitBreach.FetchErr.Error(), r.theme.EntryError)
		r.cancelGitBreachDiff()
		r.gitBreach.DiffView.SetText("")
		return
	}
	if row == 0 {
		showTablePlaceholder(r.gitBreach.FilesTable, "Working tree clean.", r.theme.MutedTextColor)
		r.cancelGitBreachDiff()
		r.gitBreach.DiffView.SetText("")
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
	r.gitBreach.FilesTable.SetSelectable(true, false)

	// Keep the cursor on a real file row, not a non-selectable section
	// header — the same restraint renderActivityLog's own tail already
	// shows after a refresh changes how many rows there are. The !ok
	// check alone isn't enough the very first time real rows exist:
	// see gitBreach.FilesReady's own doc comment on Root for why a
	// leftover selectedRow from the construction-time placeholder
	// render can pass as "valid" by sheer coincidence.
	cur, _ := r.gitBreach.FilesTable.GetSelection()
	if _, ok := r.gitBreach.RowIndex[cur]; !ok || !r.gitBreach.FilesReady {
		cur = gitBreachFirstDataRow(r.gitBreach.RowIndex)
		r.gitBreach.FilesTable.Select(cur, 0)
	}
	r.gitBreach.FilesReady = true
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
	// Gated by startGitBreachFilesDiffIfOwner's own ownership check —
	// see gitBreach.MainOwner's doc comment on Root — so this reload
	// doesn't clobber Main with a file diff while Commits actually owns
	// it right now.
	r.startGitBreachFilesDiffIfOwner()
}

// renderGitBreachBranches rebuilds the Branches table — unlike Files,
// no section headers at all (every row is the same kind of thing, a
// branch), so gitBreachBranchAt indexes r.gitBreach.Branches directly
// by row rather than needing Files' own gitBreach.RowIndex indirection.
// renderGitBreachBranchesHeader shows which of Branches/Tags is
// currently the visible sub-tab in the box's own header — a "●"
// before whichever name is active, the same marker convention Batch
// Rename's own steps list already uses for "this one currently
// changes something". No SetDynamicColors here (unlike DiffHeader/
// Hint): a bold/colored active label would need it turned on for this
// header specifically, for one glyph's worth of difference.
func (r *Root) renderGitBreachBranchesHeader() {
	title := " ● Branches   Tags "
	if r.gitBreach.BranchesShowTags {
		title = "   Branches  ● Tags "
	}
	r.gitBreach.BranchesHeader.SetText(title)
}

func (r *Root) renderGitBreachBranches() {
	r.gitBreach.BranchesTable.Clear()
	r.renderGitBreachBranchesHeader()

	if r.gitBreach.BranchesShowTags {
		r.renderGitBreachTags()
		return
	}

	if r.gitBreach.BranchesErr != nil {
		showTablePlaceholder(r.gitBreach.BranchesTable, r.gitBreach.BranchesErr.Error(), r.theme.EntryError)
		return
	}

	// A detached HEAD matches no entry in r.gitBreach.Branches at all
	// (git.Branches only ever lists refs/heads/, which a detached HEAD
	// isn't one of) — said so explicitly here via gitBreach.Status's own
	// Detached/Branch fields (already fetched by the very same Fetch
	// call that filled Status/Files), rather than letting "no row shows
	// as current" read as a bug instead of the real, if unusual, state
	// it actually is.
	row := 0
	if r.gitBreach.Status.Detached {
		r.gitBreach.BranchesTable.SetCell(row, 0,
			tview.NewTableCell(fmt.Sprintf("(detached at %s)", r.gitBreach.Status.Branch)).
				SetTextColor(r.theme.MutedTextColor).
				SetSelectable(false))
		row++
	}

	// Current branch in EntryExecutable (this app's own established
	// "healthy/ready" green, the same one gitBreachRowColor already
	// uses for a Staged row), every other branch in the box's own
	// plain text color — the current branch reads as the single
	// obvious answer to "which one am I on" at a glance rather than
	// needing a separate marker column.
	for _, b := range r.gitBreach.Branches {
		text, color := "  "+b.Name, r.theme.Text
		if b.Current {
			text, color = "* "+b.Name, r.theme.EntryExecutable
		}
		r.gitBreach.BranchesTable.SetCell(row, 0, tview.NewTableCell(text).SetTextColor(color))
		row++
	}

	if len(r.gitBreach.Branches) == 0 {
		showTablePlaceholder(r.gitBreach.BranchesTable, "No local branches found.", r.theme.MutedTextColor)
		return
	}

	// Real rows exist past this point — re-assert SetSelectable(true,
	// false) the same reason renderGitBreachFiles's own tail already
	// documents: showTablePlaceholder turns it off as its own fix for a
	// real tview v0.42.0 freeze, and never turns it back on by itself.
	r.gitBreach.BranchesTable.SetSelectable(true, false)

	// The !ok check alone isn't enough the very first time real rows
	// exist — see gitBreach.BranchesReady's own doc comment on Root for
	// why a leftover selectedRow from the construction-time placeholder
	// render can pass as "valid" here too, landing on the wrong branch
	// instead of the first one.
	cur, _ := r.gitBreach.BranchesTable.GetSelection()
	if _, ok := r.gitBreachBranchAt(cur); !ok || !r.gitBreach.BranchesReady {
		firstRow := 0
		if r.gitBreach.Status.Detached {
			firstRow = 1 // row 0 is the non-selectable "(detached at ...)" row
		}
		r.gitBreach.BranchesTable.Select(firstRow, 0)
	}
	r.gitBreach.BranchesReady = true
}

// renderGitBreachTags fills the same BranchesTable with
// gitBreach.Tags instead, while the Tags sub-tab is showing — no
// detached-HEAD row-0 offset the way Branches has (see
// gitBreachTagAt's own doc comment), so the table row index maps
// directly onto gitBreach.Tags.
func (r *Root) renderGitBreachTags() {
	if r.gitBreach.TagsErr != nil {
		showTablePlaceholder(r.gitBreach.BranchesTable, r.gitBreach.TagsErr.Error(), r.theme.EntryError)
		return
	}
	if len(r.gitBreach.Tags) == 0 {
		showTablePlaceholder(r.gitBreach.BranchesTable, "No tags found.", r.theme.MutedTextColor)
		return
	}

	for row, tg := range r.gitBreach.Tags {
		r.gitBreach.BranchesTable.SetCell(row, 0, tview.NewTableCell("  "+tg.Name).SetTextColor(r.theme.Text))
	}

	// Same reasoning as renderGitBreachBranches' own identical tail:
	// showTablePlaceholder's SetSelectable(false) has to be undone
	// once real rows exist, and a selection index left over from
	// whichever table (or placeholder) this box showed before has to
	// be checked against the table that's actually visible now, not
	// assumed still valid.
	r.gitBreach.BranchesTable.SetSelectable(true, false)
	cur, _ := r.gitBreach.BranchesTable.GetSelection()
	if _, ok := r.gitBreachTagAt(cur); !ok {
		r.gitBreach.BranchesTable.Select(0, 0)
	}
}

// gitBreachAuthorColors are twelve hues, evenly spaced around the hue
// wheel, each one's own lightness individually tuned (not one shared
// lightness for all twelve) so every single one contrasts at roughly
// the same ~4.5:1 (WCAG AA for normal text) against this app's own
// default SurfaceBackground (#2f4f4f) — a flat lightness across all
// twelve hues badly fails this for blue/purple specifically (blue's own
// luminance coefficient is the smallest of the three channels, so a
// blue needs noticeably more raw lightness than a yellow or green to
// read as equally bright), live-confirmed against a real terminal
// after the user's own report that an earlier candidate (reusing
// internal/config's existing nine label colors) read as too dark,
// purple worst of all. Git-breach-local and fixed, not a
// ResolvedTheme/config.Theme field: the same "a specific feature gets
// its own named, hardcoded color(s) outside the user-configurable
// scheme" precedent gitBreachDiffAddedColor's own doc comment already
// establishes, for the same reason — commit-author identity needs a
// color guaranteed readable on this app's own dark surface, not
// whatever an arbitrary third-party scheme's own label colors happen
// to be. Twelve, not nine or more: past about a dozen, adjacent hues
// in a narrow table cell stop reading as reliably distinct from each
// other; a repository with more distinct authors than this reuses
// colors (see gitBreachAuthorColor's own doc comment on why that's an
// accepted, harmless outcome rather than something this table needs to
// grow arbitrarily large to avoid).
var gitBreachAuthorColors = [12]tcell.Color{
	tcell.GetColor("#f7a2a2"), // 0°   red
	tcell.GetColor("#f2a95f"), // 30°  orange
	tcell.GetColor("#bfbf0f"), // 60°  yellow-olive
	tcell.GetColor("#6fcf10"), // 90°  yellow-green
	tcell.GetColor("#11d611"), // 120° green
	tcell.GetColor("#11d472"), // 150° green-teal
	tcell.GetColor("#10cdcd"), // 180° teal
	tcell.GetColor("#85bdf5"), // 210° light blue
	tcell.GetColor("#b2b2f8"), // 240° blue-purple
	tcell.GetColor("#cfa8f7"), // 270° purple
	tcell.GetColor("#f696f6"), // 300° pink-purple
	tcell.GetColor("#f79dca"), // 330° pink
}

// gitBreachAuthorColor picks one of gitBreachAuthorColors for email,
// deterministically — the same author always gets the same color
// across a render and across reloads, without this package having to
// track "which author got which color already" state of its own
// anywhere. The actual email→bucket hashing (FNV-1a; see its own doc
// comment in internal/git/diff.go for why) is internal/git.
// AuthorColorIndex, kept UI-free and testable without tcell; only the
// mapping from a bucket index to one of *this file's own* twelve
// tcell.Color values lives here.
func gitBreachAuthorColor(email string) tcell.Color {
	return gitBreachAuthorColors[git.AuthorColorIndex(email, len(gitBreachAuthorColors))]
}

// gitBreachLaneColor reuses the same twelve contrast-verified colors
// gitBreachAuthorColor already picks from, indexed by git.LaneRow's
// own stable lane id instead of a hashed author email — a different
// axis (which branch path a commit's line belongs to, not who wrote
// it) read from a different part of the same row (the graph prefix on
// the left, the author name further right), so the two meanings never
// have to be told apart at a glance. A negative lane id (an empty
// column) has no color of its own; the caller never wraps that glyph
// in a color tag to begin with.
func gitBreachLaneColor(lane int) tcell.Color {
	return gitBreachAuthorColors[lane%len(gitBreachAuthorColors)]
}

// gitBreachGraphGlyph maps one git.LaneGlyph to the rune it draws —
// rounded box-drawing corners for forks/joins (a merge commit's extra
// parents branching out, two lanes converging back on a shared
// ancestor) read as distinctly "line bends here" against the plain
// vertical bar of an unrelated pass-through lane, the same visual
// language git log --graph/lazygit/gitk all already use.
func gitBreachGraphGlyph(g git.LaneGlyph) rune {
	switch g {
	case git.LaneGlyphPass:
		return '│'
	case git.LaneGlyphNode:
		return '●'
	case git.LaneGlyphHorizontal:
		return '─'
	case git.LaneGlyphForkRight:
		return '╮'
	case git.LaneGlyphForkLeft:
		return '╭'
	case git.LaneGlyphJoinRight:
		return '╯'
	case git.LaneGlyphJoinLeft:
		return '╰'
	default:
		return ' '
	}
}

// gitBreachGraphPrefix renders one git.LaneRow's own Cells as a
// tview-tagged string — each glyph individually color-wrapped by its
// own lane id, a single Table cell drawing several differently
// colored lanes side by side exactly the way tview's own tag-aware
// cell rendering already supports (verified directly against a
// tcell.SimulationScreen before this was built, not assumed) — then
// right-pads with plain spaces to width columns, so every row's own
// hash/author/subject lines up regardless of how many lanes that
// particular row happened to need.
func gitBreachGraphPrefix(row git.LaneRow, width int) string {
	var b strings.Builder
	for _, c := range row.Cells {
		if c.Glyph == git.LaneGlyphNone {
			b.WriteByte(' ')
			continue
		}
		b.WriteString(wrapColor(gitBreachLaneColor(c.Lane), string(gitBreachGraphGlyph(c.Glyph))))
	}
	for i := len(row.Cells); i < width; i++ {
		b.WriteByte(' ')
	}
	return b.String()
}

// renderGitBreachCommits rebuilds the Commits table — same shape as
// renderGitBreachBranches (no section headers, one row per entry), same
// gitBreach.CommitsReady fix for the same construction-time-placeholder
// reason (see its own doc comment on Root).
// renderGitBreachCommitsHeader rebuilds the Commits header's own text,
// right-padding a "loaded/total" hint against width — the user's own
// explicit request, after asking whether the Commits box really shows
// every commit (it doesn't past git.CommitLogLimit without scrolling,
// see maybeLoadMoreGitBreachCommits) and proposing this exact fix.
// Hidden once every commit is already loaded (gitBreach.Commits is no
// shorter than gitBreach.CommitsTotal) or if gitBreach.CommitsTotal
// itself is still its zero value (CommitCount hasn't succeeded yet, or
// genuinely found none) — showing "0/0" or a hint that's wrong because
// the real total failed to fetch would be worse than showing nothing.
func (r *Root) renderGitBreachCommitsHeader(width int) {
	title := " Commits "
	loaded := len(r.gitBreach.Commits)
	if r.gitBreach.CommitsTotal == 0 || loaded >= r.gitBreach.CommitsTotal {
		r.gitBreach.CommitsHeader.SetText(title)
		return
	}
	hint := fmt.Sprintf("%d/%d ", loaded, r.gitBreach.CommitsTotal)
	padding := width - tview.TaggedStringWidth(title) - tview.TaggedStringWidth(hint)
	if padding < 0 {
		padding = 0
	}
	r.gitBreach.CommitsHeader.SetText(title + strings.Repeat(" ", padding) + hint)
}

// renderGitBreachDiffHeader rebuilds Main's own header text, appending
// a right-aligned "Page X/Y" hint — the user's own explicit request,
// after noticing a scrolled-down diff gave no indication of where in
// the overall text the current view actually sat. X/Y are measured in
// pages of the box's own current content height (height, as handed in
// by SetDrawFunc — the diff view's own visible row count), not raw
// line numbers: a 487-line diff at 50 rows per page is 10 pages (the
// last one partially filled), matching how a human would describe
// "page 4 of 10" rather than "line 150 of 487". Hidden (bare title)
// whenever there's nothing to page through yet — height still zero
// before the first real Draw, or no diff text loaded at all.
func (r *Root) renderGitBreachDiffHeader(width, height int) {
	title := " Main — Diff "
	if height <= 0 {
		r.gitBreach.DiffHeader.SetText(title)
		return
	}
	view := r.gitBreach.DiffView
	totalLines := r.gitBreach.DiffView.GetWrappedLineCount()
	if r.gitBreach.DiffSplit {
		// The two panes are supposed to always have exactly the same
		// row count (see gitBreachRenderDiffSplit's own doc comment:
		// one row per LinePair on both sides, always) — but "Page X/Y"
		// uses whichever is actually longer regardless, a cheap
		// safeguard against that invariant ever quietly breaking
		// (a future rendering change, an edge case missed here) rather
		// than trusting it blindly.
		view = r.gitBreach.DiffSplitLeft
		totalLines = view.GetWrappedLineCount()
		if rightLines := r.gitBreach.DiffSplitRight.GetWrappedLineCount(); rightLines > totalLines {
			view = r.gitBreach.DiffSplitRight
			totalLines = rightLines
		}
	}
	if totalLines == 0 {
		r.gitBreach.DiffHeader.SetText(title)
		return
	}
	row, _ := view.GetScrollOffset()
	currentPage, totalPages := gitBreachDiffPage(row, totalLines, height)
	hint := fmt.Sprintf("Page %d/%d ", currentPage, totalPages)
	padding := width - tview.TaggedStringWidth(title) - tview.TaggedStringWidth(hint)
	if padding < 0 {
		padding = 0
	}
	r.gitBreach.DiffHeader.SetText(title + strings.Repeat(" ", padding) + hint)
}

// gitBreachDiffPage maps a scroll row to a 1-based "page" number out
// of totalPages, given the box's own current height — tview-specific
// naming kept here (the box/scroll framing is this package's own),
// but the actual proportional-mapping math (see its own doc comment
// in internal/git/diff.go for why it isn't simply row/height+1) is
// internal/git.DiffPage, shared with any other caller that ever needs
// the same "always reach the true last page" guarantee.
func gitBreachDiffPage(row, totalLines, height int) (currentPage, totalPages int) {
	return git.DiffPage(row, totalLines, height)
}

func (r *Root) renderGitBreachCommits() {
	r.gitBreach.CommitsTable.Clear()

	if r.gitBreach.CommitsErr != nil {
		showTablePlaceholder(r.gitBreach.CommitsTable, r.gitBreach.CommitsErr.Error(), r.theme.EntryError)
		return
	}
	if len(r.gitBreach.Commits) == 0 {
		showTablePlaceholder(r.gitBreach.CommitsTable, "No commits yet.", r.theme.MutedTextColor)
		return
	}

	lanes := git.CommitLanes(r.gitBreach.Commits)
	width := 0
	for _, l := range lanes {
		if len(l.Cells) > width {
			width = len(l.Cells)
		}
	}
	for row, l := range lanes {
		c := l.Commit
		// AuthorName/Subject are escaped: the graph prefix just ahead
		// of them is built entirely out of this package's own color
		// tags, and an unescaped "[" in either (a subject like "fix:
		// strip [brackets]" is entirely plausible) would otherwise be
		// parsed as more of the same tag syntax and corrupt the rest
		// of the row instead of just failing to color one word.
		text := fmt.Sprintf("%s %s %s %s", gitBreachGraphPrefix(l, width), c.Short, tview.Escape(c.AuthorName), tview.Escape(c.Subject))
		r.gitBreach.CommitsTable.SetCell(row, 0, tview.NewTableCell(text).SetTextColor(gitBreachAuthorColor(c.AuthorEmail)))
	}

	// Real rows exist past this point — re-assert SetSelectable(true,
	// false) the same reason renderGitBreachFiles's/
	// renderGitBreachBranches's own tails already document:
	// showTablePlaceholder turns it off as its own fix for a real tview
	// v0.42.0 freeze, and never turns it back on by itself.
	r.gitBreach.CommitsTable.SetSelectable(true, false)

	// The !ok check alone isn't enough the very first time real rows
	// exist — see gitBreach.CommitsReady's own doc comment on Root for
	// why a leftover selectedRow from the construction-time placeholder
	// render can pass as "valid" here too, the same bug Branches had.
	cur, _ := r.gitBreach.CommitsTable.GetSelection()
	if cur < 0 || cur >= len(r.gitBreach.Commits) || !r.gitBreach.CommitsReady {
		r.gitBreach.CommitsTable.Select(0, 0)
	}
	r.gitBreach.CommitsReady = true
	r.startGitBreachCommitsDiffIfOwner()
}

// renderGitBreachStash rebuilds the Stash table — same shape as
// renderGitBreachCommits (no section headers, one row per entry), same
// gitBreach.StashReady fix for the same construction-time-placeholder
// reason (see its own doc comment on Root).
func (r *Root) renderGitBreachStash() {
	r.gitBreach.StashTable.Clear()

	if r.gitBreach.StashErr != nil {
		showTablePlaceholder(r.gitBreach.StashTable, r.gitBreach.StashErr.Error(), r.theme.EntryError)
		return
	}
	if len(r.gitBreach.Stash) == 0 {
		showTablePlaceholder(r.gitBreach.StashTable, "No stashed changes.", r.theme.MutedTextColor)
		return
	}

	for row, s := range r.gitBreach.Stash {
		text := fmt.Sprintf("%s %s", s.Ref, s.Message)
		r.gitBreach.StashTable.SetCell(row, 0, tview.NewTableCell(text).SetTextColor(r.theme.Text))
	}

	// Real rows exist past this point — re-assert SetSelectable(true,
	// false) the same reason renderGitBreachFiles's/
	// renderGitBreachBranches's/renderGitBreachCommits's own tails
	// already document: showTablePlaceholder turns it off as its own
	// fix for a real tview v0.42.0 freeze, and never turns it back on
	// by itself.
	r.gitBreach.StashTable.SetSelectable(true, false)

	// The !ok check alone isn't enough the very first time real rows
	// exist — see gitBreach.StashReady's own doc comment on Root for why
	// a leftover selectedRow from the construction-time placeholder
	// render can pass as "valid" here too, the same bug Branches/
	// Commits had.
	cur, _ := r.gitBreach.StashTable.GetSelection()
	if cur < 0 || cur >= len(r.gitBreach.Stash) || !r.gitBreach.StashReady {
		r.gitBreach.StashTable.Select(0, 0)
	}
	r.gitBreach.StashReady = true
	r.startGitBreachStashDiffIfOwner()
}

// gitBreachMainOwnerKind names which box's own selection the Main —
// Diff box currently follows — see gitBreach.MainOwner's own doc
// comment on Root.
type gitBreachMainOwnerKind int

const (
	gitBreachMainOwnerFiles gitBreachMainOwnerKind = iota
	gitBreachMainOwnerCommits
	gitBreachMainOwnerStash
)

// startGitBreachFilesDiffIfOwner refreshes Main with Files' own
// currently-selected row's diff, but only when Files actually owns
// Main right now — called from Files' own SetSelectionChangedFunc,
// FocusFunc, and renderGitBreachFiles' own tail (the real, reported bug
// that tail originally fixed: Select() only invokes
// SetSelectionChangedFunc when the selected row *number* changes, not
// when the data a stable number refers to does). Without the owner
// check, any of those three paths firing while Commits has real focus
// would silently clobber a commit's own diff with a file diff.
func (r *Root) startGitBreachFilesDiffIfOwner() {
	if r.gitBreach.MainOwner != gitBreachMainOwnerFiles {
		return
	}
	row, _ := r.gitBreach.FilesTable.GetSelection()
	r.startGitBreachDiff(row)
}

// startGitBreachCommitsDiffIfOwner mirrors
// startGitBreachFilesDiffIfOwner exactly, for Commits.
func (r *Root) startGitBreachCommitsDiffIfOwner() {
	if r.gitBreach.MainOwner != gitBreachMainOwnerCommits {
		return
	}
	row, _ := r.gitBreach.CommitsTable.GetSelection()
	r.startGitBreachCommitDiff(row)
}

// startGitBreachStashDiffIfOwner mirrors startGitBreachFilesDiffIfOwner
// exactly, for Stash.
func (r *Root) startGitBreachStashDiffIfOwner() {
	if r.gitBreach.MainOwner != gitBreachMainOwnerStash {
		return
	}
	row, _ := r.gitBreach.StashTable.GetSelection()
	r.startGitBreachStashDiff(row)
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

// gitBreachColorizeDiff turns a plain `git diff` into tview markup.
// Line *classification* (file header / hunk header / added / removed /
// context) is still this function's own hand-rolled "+"/"-"/"@@"
// prefix check, not chroma's — see the "Deliberately hand-rolled"
// paragraph below. An added/removed line's own content isn't painted
// one single solid green/red foreground: its whole background tints
// (gitBreachDiffAddedBackground/gitBreachDiffRemovedBackground — their
// own doc comment explains the color choice) and its content is real,
// per-language syntax highlighting via the exact same
// internal/viewer.Highlight + renderSyntax pipeline Look and the
// untracked-file case already use — background carries "this line
// changed", foreground carries "what this code actually is", the same
// two-channel split a real code-review tool's diff view already uses,
// instead of one channel fighting the other. Context lines get the
// same per-language coloring too (no background tint — nothing
// changed on them), so a diff doesn't read as "colorful lines, plain
// white lines" alternating for no reason.
//
// Each file's own old and new content is reconstructed from the diff's
// hunks first (oldLines/newLines below — every context/removed line
// feeds oldLines, every context/added line feeds newLines) and lexed
// once per file, in full, via renderReconstructedFiles — not line by
// line the way an earlier version of this function did. A multi-line
// construct (a block comment, a multi-line string) needs real
// surrounding context to classify correctly; a lexer given one line
// at a time never sees it and silently falls back to KindPlain for
// the whole construct (verified directly against chroma's own Go
// lexer, not assumed). Reconstructing only from the hunks actually
// present in this diff, not the whole file from disk: every shape
// this function already has to handle (unstaged, staged, a commit, a
// stash entry) comes as this same diff text already, with no further
// git call needed to get a full file body right for each one.
//
// "+++"/"---" (the diff's own old/new file-header lines) are checked
// before the plain "+"/"-" cases below, which would otherwise also
// match their own leading character and treat a file header as a
// single added/removed line.
//
// Deliberately hand-rolled rather than routed through chroma's own
// dedicated Diff lexer for the line *classification* itself: tried it
// directly and found it strictly worse for this exact job — it colors
// "---"/"+++" file-header lines as a removed/added line each (wrong:
// they're path headers, not content changes, exactly the miscoloring
// this function's own early "+++"/"---" check above exists to avoid),
// and it collapses an entire run of context lines plus the "@@" hunk
// marker into one single undifferentiated token instead of coloring
// each line on its own — no per-line granularity at all, which this
// function needs regardless of where the per-language coloring within
// each line comes from.
// path is the lexer path to use before the first "diff --git" header
// line, if any, is seen — the only path a single-file diff (Files'/
// Branches' own staged/unstaged diff) ever has. A commit's own diff
// (see startGitBreachCommitDiff) can span several files in one combined
// diff text; each "diff --git a/X b/Y" header line updates which path
// every following line is lexed against, via gitBreachDiffHeaderPath,
// rather than lexing every file in the commit as whatever the one
// fixed path argument says. oldLines/newLines are keyed by this same
// path, so a multi-file commit diff reconstructs — and lexes — each
// file separately rather than concatenating unrelated files together.
func gitBreachColorizeDiff(diff, path string, theme config.ResolvedTheme) string {
	palette := paletteFor(theme.SurfaceBackground)
	rawLines := strings.Split(diff, "\n")

	// First pass: classify every raw line and, for a content line,
	// reconstruct it into its file's own old/new content — oldIndex/
	// newIndex below are this line's own position in that
	// reconstruction, resolved against the second pass's rendered
	// result once that's ready.
	plan := make([]gitBreachDiffLinePlan, len(rawLines))
	oldLines := map[string][]string{}
	newLines := map[string][]string{}
	currentPath := path
	for i, line := range rawLines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			if p, ok := gitBreachDiffHeaderPath(line); ok {
				currentPath = p
			}
			plan[i] = gitBreachDiffLinePlan{raw: line, kind: gitBreachDiffLineHeader}
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"),
			strings.HasPrefix(line, "@@"), strings.HasPrefix(line, "index "):
			plan[i] = gitBreachDiffLinePlan{raw: line, kind: gitBreachDiffLineHeader}
		case strings.HasPrefix(line, "+"):
			newLines[currentPath] = append(newLines[currentPath], line[1:])
			plan[i] = gitBreachDiffLinePlan{raw: line, kind: gitBreachDiffLineAdded, path: currentPath, newIndex: len(newLines[currentPath]) - 1}
		case strings.HasPrefix(line, "-"):
			oldLines[currentPath] = append(oldLines[currentPath], line[1:])
			plan[i] = gitBreachDiffLinePlan{raw: line, kind: gitBreachDiffLineRemoved, path: currentPath, oldIndex: len(oldLines[currentPath]) - 1}
		default:
			// A context line — real file content on both sides at
			// once, so it feeds both reconstructions; either one
			// renders identically, since nothing about it changed.
			oldLines[currentPath] = append(oldLines[currentPath], line)
			newLines[currentPath] = append(newLines[currentPath], line)
			plan[i] = gitBreachDiffLinePlan{
				raw: line, kind: gitBreachDiffLineContext, path: currentPath,
				oldIndex: len(oldLines[currentPath]) - 1, newIndex: len(newLines[currentPath]) - 1,
			}
		}
	}

	oldRendered := renderReconstructedFiles(oldLines, palette)
	newRendered := renderReconstructedFiles(newLines, palette)

	var b strings.Builder
	for i, pl := range plan {
		switch pl.kind {
		case gitBreachDiffLineHeader:
			b.WriteString(wrapColor(theme.MutedTextColor, tview.Escape(pl.raw)))
		case gitBreachDiffLineAdded:
			b.WriteString(gitBreachWrapDiffLine(pl.raw[:1], newRendered[pl.path][pl.newIndex], gitBreachDiffAddedColor, gitBreachDiffAddedBackground))
		case gitBreachDiffLineRemoved:
			b.WriteString(gitBreachWrapDiffLine(pl.raw[:1], oldRendered[pl.path][pl.oldIndex], theme.CriticalText, gitBreachDiffRemovedBackground))
		default: // gitBreachDiffLineContext
			b.WriteString(newRendered[pl.path][pl.newIndex])
		}
		if i < len(plan)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// gitBreachDiffLineKind is one raw diff line's own role —
// gitBreachColorizeDiff's first pass classifies every line into
// exactly one of these before the second pass renders any of them.
type gitBreachDiffLineKind int

const (
	gitBreachDiffLineHeader  gitBreachDiffLineKind = iota // file/hunk header — shown muted, verbatim
	gitBreachDiffLineAdded                                // "+" line
	gitBreachDiffLineRemoved                              // "-" line
	gitBreachDiffLineContext                              // unchanged line
)

// gitBreachDiffLinePlan is gitBreachColorizeDiff's own first-pass
// result for one raw diff line — enough to either emit it unchanged
// (a header) or look up its already-highlighted content from the
// second pass's per-file reconstruction (oldIndex/newIndex index into
// oldRendered[path]/newRendered[path] respectively; only the one(s)
// that apply to this line's own kind are ever actually set).
type gitBreachDiffLinePlan struct {
	raw      string
	kind     gitBreachDiffLineKind
	path     string
	oldIndex int
	newIndex int
}

// renderReconstructedFiles highlights each file's own reconstructed
// content once — not line by line — for correct multi-line-construct
// classification (see gitBreachColorizeDiff's own doc comment), then
// splits the result back into one already-rendered string per line
// via splitHighlightedTokensByLine, so gitBreachColorizeDiff's second
// pass can still render each diff line separately. Falls back to
// lexing that one file's lines individually — the previous, always-
// correct-count behavior — if splitHighlightedTokensByLine's own
// line-count invariant doesn't hold for it: that invariant rests on
// viewer.Highlight's own contract, not provable here for every lexer
// chroma ships, and a wrong count would silently mis-color every
// following line in that file rather than just losing the multi-line
// improvement for it.
func renderReconstructedFiles(lines map[string][]string, palette syntaxPalette) map[string][]string {
	rendered := make(map[string][]string, len(lines))
	for path, ls := range lines {
		split := splitHighlightedTokensByLine(viewer.Highlight(path, strings.Join(ls, "\n")), palette)
		if len(split) != len(ls) {
			split = make([]string, len(ls))
			for i, l := range ls {
				split[i] = renderSyntax(viewer.Highlight(path, l), palette)
			}
		}
		rendered[path] = split
	}
	return rendered
}

// gitBreachDiffHeaderPath extracts the post-change ("b/...") path out
// of a "diff --git a/X b/Y" header line — pure string parsing, no UI
// dependency at all, so the logic itself lives in internal/git.
// DiffHeaderPath (see its own doc comment there for the one known,
// accepted edge case); this is only the name gitBreachColorizeDiff's
// own call site already uses.
func gitBreachDiffHeaderPath(line string) (string, bool) {
	return git.DiffHeaderPath(line)
}

// gitBreachWrapDiffLine renders one added/removed line: markerColor
// for the leading "+"/"-" glyph itself (gitBreachDiffAddedColor/
// theme.CriticalText — not code, so it stays outside the per-language
// coloring already baked into highlightedContent), then
// highlightedContent as-is (the rest of the line's own content,
// already rendered by renderReconstructedFiles). The one difference
// an added/removed line gets at all is the background tint wrapping
// both pieces together, via nameHighlightTags's own "[:#rrggbb:]...
// [-:-:-]" background-only tag shape (panel.go) — an empty foreground
// field leaves whatever's already set alone, so the background tint
// and the per-language foreground coloring never fight over the same
// tag.
func gitBreachWrapDiffLine(marker, highlightedContent string, markerColor, background tcell.Color) string {
	highlighted := wrapColor(markerColor, tview.Escape(marker)) + highlightedContent
	return fmt.Sprintf("[:#%06x:]%s[-:-:-]", uint32(background.Hex())&0xffffff, highlighted)
}
