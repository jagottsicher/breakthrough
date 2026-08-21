package ui

import (
	"fmt"
	"path/filepath"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

const (
	panelPage       = "panel"
	contextMenuPage = "context-menu"
	renamePage      = "rename"
	promptPage      = "prompt"
	quitConfirmPage = "quit-confirm"
)

// Root is breakthrough's top-level UI: the directory panel, plus a
// right-click context menu and the overlays it opens (Info, Rename, the
// single-line prompt behind Select +/-/chown/chmod), and a Ctrl+Q quit
// confirmation. Pages layers all of these as floating overlays on top of
// the still-visible panel; Root owns the logic for what appears where,
// giving each overlay real keyboard focus while it's shown (see
// showOverlay/hideOverlay), and closing whichever one is open when the
// user clicks outside it (captureOutsideClick).
//
// The context menu is grouped into three parts: Info/Rename, a
// "Selection" section (Select all/Deselect all/Select +/Select -,
// operating on the checkbox column), and a "Commands" section
// (Copy/Cut/Paste/chown/chmod). See menuSectionLabel for how the section
// dividers are drawn, and docs/whitepaper.md for the dialog-based
// Copy-to/Move-to planned as a possible later addition alongside the
// clipboard-style Copy/Cut/Paste built here.
type Root struct {
	*tview.Pages

	app *tview.Application

	panel       *Panel
	menu        *tview.List
	rename      *tview.InputField
	prompt      *tview.InputField
	info        *tview.TextView
	errorView   *tview.TextView
	quitConfirm *tview.List

	// promptSubmit is what the currently-open prompt overlay (see
	// openPrompt/finishPrompt) runs with the typed text if the user
	// confirms with Enter. Only meaningful while promptPage is active.
	promptSubmit func(text string)

	// clipboard holds the absolute paths Copy or Cut last captured (see
	// clipboardTargets) — the checkbox selection at the time, or just the
	// right-clicked target if nothing was checked. clipboardCut records
	// which of the two: Paste copies when false, moves (via fsops.Move)
	// when true.
	clipboard    []string
	clipboardCut bool

	// hiddenToggleIdx is the "Globals" section's hidden-files toggle
	// item's index in r.menu, set once in NewRoot — needed so
	// toggleHidden and showMenu can relabel that one item in place (see
	// hiddenToggleLabel) to describe what clicking it will do next,
	// rather than a static label that stops matching reality after the
	// first click.
	hiddenToggleIdx int

	// target is the absolute path the context menu / rename prompt is
	// currently acting on. targetRow is its table row *index* (0 = "..",
	// 1 = the first entry, ...; see Panel.rowIndexAt) — not a screen
	// coordinate, which is what a since-fixed bug here used to store
	// (see captureMouse's MouseRightClick case): openRename passes it
	// straight to Panel.nameCellRect, which indexes Table.GetCell with
	// it, so a screen y only happened to line up by coincidence, and
	// silently drifted out of sync with the table's own scroll offset as
	// soon as one existed. Only meaningful while one of the overlays
	// below is visible.
	target    string
	targetRow int

	// infoTarget/infoStat cache what the Info overlay is currently
	// showing, so computeHashes (triggered separately, after Info is
	// already open — see captureInfoKey/captureInfoMouse) knows what to
	// hash and can re-render the same text with the results appended,
	// without re-running fsops.Stat. hashSectionRow is the 0-based row,
	// within that text, where the hash hint/result line starts — set by
	// renderInfo, read by captureInfoMouse to tell whether a click landed
	// on it.
	infoTarget     string
	infoStat       fsops.Info
	hashSectionRow int

	// activePage/activeWidget track whichever overlay (context menu,
	// rename, info, quit confirm) is currently shown, if any — see
	// showOverlay/hideOverlay. This drives both explicit focus handling
	// and captureOutsideClick's "click outside the overlay closes it"
	// behavior.
	activePage   string
	activeWidget tview.Primitive

	// dragStartRow/dragCurrentRow/dragMoved/dragging track a right-button
	// drag in progress, live — see captureMouse's MouseRightDown/MouseMove/
	// MouseRightUp cases and advanceDrag.
	//
	// dragStartRow never changes once a drag begins. dragCurrentRow is
	// where the toggled range currently ends, updated on every MouseMove
	// so advanceDrag only has to toggle the rows that changed membership
	// since the last update (see Panel.applyDragDelta), not re-toggle the
	// whole range each time. dragMoved stays false until the mouse first
	// leaves the press row; nothing gets toggled before that, so a plain
	// right-click (no movement at all) still opens the context menu as
	// usual, matching what tview itself does: it only synthesizes
	// MouseRightClick when the release position matches the press
	// position, so a real drag never produces one.
	dragStartRow   int
	dragCurrentRow int
	dragMoved      bool
	dragging       bool
}

// NewRoot creates the top-level UI rooted at path. app is passed down to
// the Panel, which needs it to move keyboard focus into its header's edit
// field — see Panel.openEdit.
func NewRoot(app *tview.Application, path string) (*Root, error) {
	panel, err := NewPanel(app, path)
	if err != nil {
		return nil, err
	}

	r := &Root{
		Pages: tview.NewPages(),
		app:   app,
		panel: panel,
	}

	// No borders on the floating elements below — a background color set
	// apart from the plain panel does the same job without the
	// box-drawing look.
	r.menu = tview.NewList().ShowSecondaryText(false)
	r.menu.SetBackgroundColor(accentBackgroundColor)
	r.menu.SetMainTextColor(tcell.ColorWhite)
	r.menu.SetHighlightFullLine(true)
	r.menu.SetBorderPadding(0, 0, 1, 1)             // 1-char left/right padding; no border needed for this
	r.menu.AddItem("Properties", "", 0, r.openInfo) // first and default-selected; identifiers below still say "Info" pending the editable rework
	r.menu.AddItem("Rename", "", 0, r.openRename)
	r.menu.AddItem(menuSectionLabel("Selection"), "", 0, nil)
	r.menu.AddItem("Select all", "", 0, r.panel.selectAll)
	r.menu.AddItem("Deselect all", "", 0, r.panel.deselectAll)
	r.menu.AddItem("Select +", "", 0, r.openSelectPlus)
	r.menu.AddItem("Select -", "", 0, r.openSelectMinus)
	r.menu.AddItem(menuSectionLabel("Commands"), "", 0, nil)
	r.menu.AddItem("Copy", "", 0, r.copyToClipboard)
	r.menu.AddItem("Cut", "", 0, r.cutToClipboard)
	r.menu.AddItem("Paste", "", 0, r.pasteClipboard)
	r.menu.AddItem("chown", "", 0, r.openChown)
	r.menu.AddItem("chmod", "", 0, r.openChmod)
	r.menu.AddItem(menuSectionLabel("Globals"), "", 0, nil)
	// hiddenToggleIdx is computed rather than a hardcoded literal, so it
	// keeps pointing at the right row if another item is ever added above
	// it — see toggleHidden and showMenu, which both need it to relabel
	// this one item in place.
	r.hiddenToggleIdx = r.menu.GetItemCount()
	r.menu.AddItem(hiddenToggleLabel(r.panel.showHidden), "", 0, r.toggleHidden)
	r.menu.SetDoneFunc(r.closeMenu) // Escape

	// No label: this is positioned exactly over the target's row in
	// openRename, so it reads as "the row itself became editable" rather
	// than a separate prompt.
	r.rename = tview.NewInputField()
	r.rename.SetFieldBackgroundColor(accentBackgroundColor)
	r.rename.SetBackgroundColor(accentBackgroundColor)
	r.rename.SetLabelColor(tcell.ColorWhite)
	r.rename.SetFieldTextColor(tcell.ColorWhite)
	r.rename.SetDoneFunc(r.finishRename) // Enter or Escape

	// Backs Select +/-/chown/chmod: a single labelled field, centered on
	// screen (unlike rename, it's not tied to any one row) — see
	// openPrompt.
	r.prompt = tview.NewInputField()
	r.prompt.SetFieldBackgroundColor(accentBackgroundColor)
	r.prompt.SetBackgroundColor(accentBackgroundColor)
	r.prompt.SetLabelColor(tcell.ColorWhite)
	r.prompt.SetFieldTextColor(tcell.ColorWhite)
	r.prompt.SetDoneFunc(r.finishPrompt) // Enter or Escape

	r.info = r.newInfoView()
	r.errorView = r.newErrorView()

	// Panel reports its own failures (unreadable directory, bad path
	// typed into the header) through Root's error overlay.
	panel.onError = r.showError

	r.quitConfirm = tview.NewList().ShowSecondaryText(false)
	r.quitConfirm.SetBackgroundColor(accentBackgroundColor)
	r.quitConfirm.SetMainTextColor(tcell.ColorWhite)
	r.quitConfirm.SetHighlightFullLine(true)
	r.quitConfirm.SetBorderPadding(0, 0, 1, 1)
	r.quitConfirm.AddItem("Quit breakthrough", "", 0, r.confirmQuit)
	r.quitConfirm.AddItem("Cancel", "", 0, r.cancelQuit)
	r.quitConfirm.SetDoneFunc(r.cancelQuit) // Escape

	r.AddPage(panelPage, panel, true, true)
	r.AddPage(contextMenuPage, r.menu, false, false)
	r.AddPage(renamePage, r.rename, false, false)
	r.AddPage(promptPage, r.prompt, false, false)
	r.AddPage(infoPage, r.info, false, false)
	r.AddPage(errorPage, r.errorView, false, false)
	r.AddPage(quitConfirmPage, r.quitConfirm, false, false)

	panel.SetMouseCapture(r.captureMouse)
	r.SetMouseCapture(r.captureOutsideClick)

	return r, nil
}

// showOverlay reveals the named page, gives it real keyboard focus, and
// records it as the active overlay. Any previously active overlay is
// hidden first (there is only ever one at a time).
//
// Focus is set explicitly via Application.SetFocus rather than relying on
// Pages' own "re-focus the last visible page if already focused" behavior
// — the implicit version turned out to be fragile in practice (Escape and
// outside clicks not reliably reaching the shown overlay), the same
// reason Panel.openEdit does this explicitly too.
func (r *Root) showOverlay(page string, widget tview.Primitive) {
	r.hideOverlay()
	r.activePage = page
	r.activeWidget = widget
	r.ShowPage(page)
	r.app.SetFocus(widget)
}

// hideOverlay hides the currently active overlay, if any, and returns
// focus to the panel.
func (r *Root) hideOverlay() {
	if r.activePage == "" {
		return
	}
	r.HidePage(r.activePage)
	r.activePage = ""
	r.activeWidget = nil
	r.app.SetFocus(r.panel)
}

// captureOutsideClick keeps the panel underneath an open overlay inert:
// a click outside the overlay closes it (instead of leaving it stuck
// open) and is consumed rather than also acting on the panel, so it takes
// a second click to do anything else. Scrolling is swallowed outright
// while an overlay is open — letting it through would scroll the list out
// from under a menu that stays put, which both looks wrong and would
// leave targetRow (see openRename) pointing at a different file than the
// one the menu was opened for.
func (r *Root) captureOutsideClick(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if r.activePage == "" {
		return action, event // nothing open, nothing to do
	}

	x, y := event.Position()
	if primitiveContains(r.activeWidget, x, y) {
		return action, event // event landed on the open overlay itself
	}

	switch action {
	case tview.MouseLeftClick, tview.MouseRightClick:
		r.hideOverlay()
		return tview.MouseConsumed, nil
	case tview.MouseScrollUp, tview.MouseScrollDown, tview.MouseScrollLeft, tview.MouseScrollRight:
		return tview.MouseConsumed, nil
	default:
		return action, event
	}
}

// primitiveContains reports whether (x, y) falls within p's rectangle.
func primitiveContains(p tview.Primitive, x, y int) bool {
	rx, ry, w, h := p.GetRect()
	return x >= rx && x < rx+w && y >= ry && y < ry+h
}

// clampToPanel keeps an overlay of the given size fully inside the
// panel's inner rect, shrinking it first if it can't fit at all — a long
// path in the Info view is easily wider than an 80-column terminal, and
// without this the labelled left edge gets pushed off-screen.
func (r *Root) clampToPanel(x, y, width, height int) (int, int, int, int) {
	px, py, pw, ph := r.panel.GetInnerRect()
	if pw <= 0 || ph <= 0 {
		return x, y, width, height
	}

	if width > pw {
		width = pw
	}
	if height > ph {
		height = ph
	}
	if x+width > px+pw {
		x = px + pw - width
	}
	if y+height > py+ph {
		y = py + ph - height
	}
	if x < px {
		x = px
	}
	if y < py {
		y = py
	}

	return x, y, width, height
}

// RequestQuit shows a confirmation overlay instead of quitting right
// away — Ctrl+Q (see cmd/breakthrough) is easy to hit by accident, so the
// application only actually stops once the user picks "Quit breakthrough"
// from this overlay (or presses Enter, since it's the default selection).
func (r *Root) RequestQuit() {
	// Ctrl+Q is a global key capture, so it can arrive while the header
	// is mid-edit. Without this the edit field would stay on screen after
	// cancelling the quit, focused-looking but unreachable, since
	// hideOverlay hands focus to the panel's table rather than back to it.
	r.panel.cancelEdit()

	width, height := listSize(r.quitConfirm)

	_, _, screenWidth, screenHeight := r.GetRect() // Root fills the whole screen
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2

	r.quitConfirm.SetRect(x, y, width, height)
	r.quitConfirm.SetCurrentItem(0)
	r.showOverlay(quitConfirmPage, r.quitConfirm)
}

// RequestCancel is the Ctrl+Q sibling for Ctrl+C (see cmd/breakthrough):
// a global "back out of whatever is open" that behaves like Escape.
// Having it as a real key matters because Escape is deliberately inert
// while the path header is being edited — Ctrl+C is the keyboard way out
// of that, where otherwise only a mouse click would do.
//
// It never quits: stopping breakthrough is Ctrl+Q plus a confirmation.
func (r *Root) RequestCancel() {
	if r.activePage != "" {
		r.hideOverlay()
		return
	}
	r.panel.cancelEdit()
}

// confirmQuit is the quit overlay's "Quit breakthrough" action.
func (r *Root) confirmQuit() {
	r.app.Stop()
}

// cancelQuit hides the quit overlay without taking any action (Escape or
// "Cancel").
func (r *Root) cancelQuit() {
	r.hideOverlay()
}

// advanceDrag brings the live-toggled range up to date with row, the
// mouse's current (from MouseMove) or final (from MouseRightUp) position.
// The very first time row differs from dragStartRow, it also brings the
// press row itself into the toggle — it isn't toggled immediately on
// MouseRightDown, so that a plain click (down and up with no movement at
// all) never toggles anything, matching the same click-vs-drag test
// tview itself uses (see captureMouse's doc comment). Safe to call
// repeatedly (every MouseMove) or exactly once (MouseRightUp with no
// MouseMove in between): each call only toggles the delta since the last
// one, via Panel.applyDragDelta, so calling it more or fewer times along
// the same path never changes the end result.
func (r *Root) advanceDrag(row int) {
	if !r.dragMoved && row != r.dragStartRow {
		r.dragMoved = true
		r.panel.toggleCheckbox(r.dragStartRow)
	}
	if r.dragMoved && row != r.dragCurrentRow {
		r.panel.applyDragDelta(r.dragStartRow, r.dragCurrentRow, row)
	}
	r.dragCurrentRow = row
}

// captureMouse intercepts right-button activity on the panel: a plain
// right-click opens the context menu (unchanged from before); a
// right-button drag across rows instead toggles each of them live, as the
// drag reaches them, and does not open the menu. Everything else
// (left-click, scrolling) passes through unchanged to the panel's own
// handling — see Panel.activateRow for that.
//
// The click-vs-drag distinction is tview's own, not something tracked
// here: Application.fireMouseActions only synthesizes MouseRightClick
// when the release position matches the press position — a genuine drag
// simply never produces one, only MouseRightDown, MouseMove (repeatedly,
// while the button stays held — button state itself hasn't changed, so
// neither Down nor Up fires again for these), and MouseRightUp fire.
// MouseRightUp always runs first, for both a click and a drag; advanceDrag
// only actually toggles anything once the release position has moved off
// the press row (possibly having done so already via MouseMove, possibly
// only now if no MouseMove ever arrived — e.g. a fast drag some terminals
// report as just down+up with no intermediate positions). If nothing ever
// moved, the event is left unconsumed so the MouseRightClick that (per
// tview) is about to follow can open the menu as usual.
//
// This is also where Panel.captureOutsideEdit gets a chance to run first:
// only one SetMouseCapture can be installed on Panel at a time, and Root
// already needs that slot for right-click detection, so Root's capture
// delegates to Panel's own "was the header being edited and did this
// click land outside it" check before doing anything else.
func (r *Root) captureMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if r.panel.captureOutsideEdit(action, event) {
		return tview.MouseConsumed, nil
	}

	switch action {
	case tview.MouseRightDown:
		x, y := event.Position()
		if row, ok := r.panel.rowIndexAt(x, y); ok {
			r.dragStartRow = row
			r.dragCurrentRow = row
			r.dragMoved = false
			r.dragging = true
			r.panel.focusRow(row) // move the highlight to the press row right away, not just on release
		} else {
			r.dragging = false
		}
		return action, event // Table has no case for this action; harmless to pass through

	case tview.MouseMove:
		if !r.dragging {
			return action, event
		}
		x, y := event.Position()
		if row, ok := r.panel.rowIndexAt(x, y); ok {
			r.advanceDrag(row)
			r.panel.focusRow(row)
		}
		return tview.MouseConsumed, nil

	case tview.MouseRightUp:
		wasDragging := r.dragging
		r.dragging = false
		if !wasDragging {
			return action, event
		}
		x, y := event.Position()
		if endRow, ok := r.panel.rowIndexAt(x, y); ok {
			r.advanceDrag(endRow)
		}
		if !r.dragMoved {
			// Never actually moved off the press row: nothing was
			// toggled, let a same-position MouseRightClick, if tview
			// fires one right after this, open the menu as usual.
			return action, event
		}
		r.panel.focusRow(r.dragCurrentRow) // leave the highlight on the row the drag ended on
		return tview.MouseConsumed, nil

	case tview.MouseRightClick:
		x, y := event.Position()
		path, ok := r.panel.RowAt(x, y)
		if !ok {
			return action, event // nothing sensible to act on
		}
		row, ok := r.panel.rowIndexAt(x, y)
		if !ok {
			// RowAt just succeeded via this same lookup, so this
			// shouldn't happen — stay defensive rather than fall back to
			// a stale or wrong targetRow.
			return action, event
		}
		r.panel.focusRow(row) // the menu is about this row; the highlight should agree
		r.target = path
		r.targetRow = row
		r.showMenu(x, y)
		return tview.MouseConsumed, nil

	default:
		return action, event
	}
}

// menuSectionLabel renders a non-actionable divider row's text for the
// context menu — dim style tags (tview's own "[fg:bg:flags]" syntax,
// enabled by default for List item text) rather than a real, clickable
// item, so it reads as a section label. Paired with a nil selected func
// in the AddItem call that uses it (see NewRoot), which makes Enter/click
// on the row a no-op. Arrow-key navigation still stops on it for a
// moment, since tview.List has no "disabled item" concept to skip it with
// — a small, accepted quirk rather than hand-rolling navigation logic for
// what's cosmetic.
func menuSectionLabel(name string) string {
	return fmt.Sprintf("[::d]── %s ──[::-]", name)
}

// showMenu positions the context menu near (x, y), clamped to the panel's
// inner rect so it doesn't get drawn partly off-screen, and reveals it as
// an overlay on top of the still-visible panel.
func (r *Root) showMenu(x, y int) {
	// Defensive re-sync rather than trusting toggleHidden's own relabel
	// to always have run last: cheap, and keeps this correct even if
	// something else ever changes panel.showHidden directly.
	r.menu.SetItemText(r.hiddenToggleIdx, hiddenToggleLabel(r.panel.showHidden), "")

	width, height := listSize(r.menu)
	x, y, width, height = r.clampToPanel(x, y, width, height)

	r.menu.SetRect(x, y, width, height)
	r.menu.SetCurrentItem(0)
	r.showOverlay(contextMenuPage, r.menu)
}

// toggleHidden is the context menu's "Globals" hidden-files toggle: flips
// whether dotfile entries are shown (see Panel.showHidden), reloads the
// current directory so the change takes effect immediately, and relabels
// the menu item itself to describe what clicking it will do next time.
func (r *Root) toggleHidden() {
	r.panel.showHidden = !r.panel.showHidden
	r.showError(r.panel.load(r.panel.path))
	r.menu.SetItemText(r.hiddenToggleIdx, hiddenToggleLabel(r.panel.showHidden), "")
}

// hiddenToggleLabel renders the hidden-files toggle's label as the
// action clicking it performs next, not its current state — e.g. it
// reads "Show hidden files" while they're hidden, the same convention
// most file managers use for a toggle like this.
func hiddenToggleLabel(showHidden bool) string {
	if showHidden {
		return "Hide hidden files"
	}
	return "Show hidden files"
}

// listSize returns a no-border, no-secondary-text List's width — the
// widest item's rendered text plus 1-char left/right padding (see the
// SetBorderPadding calls in NewRoot) — and its height, one row per item.
// tview.TaggedStringWidth, not a plain rune count, since section-header
// items (see menuSectionLabel) carry style tags that aren't part of what
// actually gets drawn.
func listSize(l *tview.List) (width, height int) {
	for i := 0; i < l.GetItemCount(); i++ {
		main, _ := l.GetItemText(i)
		if w := tview.TaggedStringWidth(main); w > width {
			width = w
		}
	}
	return width + 2, l.GetItemCount() // +2: 1-char padding on each side
}

// closeMenu hides the context menu without taking any action (Escape).
func (r *Root) closeMenu() {
	r.hideOverlay()
}

// openRename is the context menu's "Rename" action. Rather than a prompt
// floating near the menu, it positions the rename field exactly over the
// target's own name cell — not the whole row: the checkbox column is
// deliberately left uncovered, so the row's current checked state stays
// visible (without becoming editable itself) while renaming. It reads as
// just the name becoming editable in place, pre-filled with the current
// one.
func (r *Root) openRename() {
	x, y, width, ok := r.panel.nameCellRect(r.targetRow)
	if !ok {
		return // targetRow came from a right-click just validated by RowAt
	}
	x, y, width, height := r.clampToPanel(x, y, width, 1)

	r.rename.SetText(filepath.Base(r.target))
	r.rename.SetRect(x, y, width, height)

	r.showOverlay(renamePage, r.rename)
}

// finishRename handles Enter (submit) and Escape/Tab (cancel) in the
// rename prompt.
//
// The rename field is closed up front rather than in a defer: a failed
// rename opens the error overlay, and hiding "the active overlay"
// afterwards would close that error again before the user ever saw it.
func (r *Root) finishRename(key tcell.Key) {
	newName := r.rename.GetText()
	r.hideOverlay()

	if key != tcell.KeyEnter {
		return // cancelled
	}
	if newName == "" || newName == filepath.Base(r.target) {
		return
	}

	if _, err := fsops.Rename(r.target, newName); err != nil {
		r.showError(err)
		return
	}
	r.showError(r.panel.load(r.panel.path))
}

// openPrompt shows a single-line input overlay labelled label, pre-filled
// with prefill, centered on screen (unlike rename, it isn't tied to any
// one row — Select +/-/chown/chmod all act on either the checkbox
// selection or a single right-clicked target, not a visible row range).
// onSubmit runs with whatever was typed if the user confirms with Enter;
// see finishPrompt for what happens on Escape/Tab instead.
func (r *Root) openPrompt(label, prefill string, onSubmit func(text string)) {
	r.prompt.SetLabel(label + " ")
	r.prompt.SetText(prefill)
	r.promptSubmit = onSubmit

	width := tview.TaggedStringWidth(label) + 26 // label plus room to type
	const height = 1
	_, _, screenWidth, screenHeight := r.GetRect() // Root fills the whole screen
	if width > screenWidth {
		width = screenWidth
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	x, y, width, clampedHeight := r.clampToPanel(x, y, width, height)

	r.prompt.SetRect(x, y, width, clampedHeight)
	r.showOverlay(promptPage, r.prompt)
}

// finishPrompt handles Enter (submit) and Escape/Tab (cancel) in the
// generic prompt overlay (see openPrompt) — the same DoneFunc pattern
// finishRename uses, generalized since this overlay backs several
// different actions rather than just one.
func (r *Root) finishPrompt(key tcell.Key) {
	text := r.prompt.GetText()
	submit := r.promptSubmit
	r.hideOverlay()
	r.promptSubmit = nil

	if key != tcell.KeyEnter || text == "" || submit == nil {
		return // cancelled, or nothing typed
	}
	submit(text)
}

// openSelectPlus is the context menu's "Select +": prompts for a glob
// pattern and checks every currently-listed entry that matches it, in
// addition to whatever was already checked.
func (r *Root) openSelectPlus() {
	r.openPrompt("Select + (glob pattern):", "", func(text string) {
		if _, err := r.panel.selectByPattern(text, true); err != nil {
			r.showError(err)
		}
	})
}

// openSelectMinus is "Select -": the same pattern prompt as Select+, but
// unchecks matches instead of checking them.
func (r *Root) openSelectMinus() {
	r.openPrompt("Select - (glob pattern):", "", func(text string) {
		if _, err := r.panel.selectByPattern(text, false); err != nil {
			r.showError(err)
		}
	})
}

// clipboardTargets is what Copy/Cut capture: the current checkbox
// selection if there is one, otherwise just the entry the context menu
// was opened on — so Copy/Cut/Paste on a single, unmarked file needs no
// separate "select it first" step.
func (r *Root) clipboardTargets() []string {
	if paths := r.panel.SelectedPaths(); len(paths) > 0 {
		return paths
	}
	if r.target != "" {
		return []string{r.target}
	}
	return nil
}

// copyToClipboard is the context menu's "Copy": remembers the current
// clipboard targets (see clipboardTargets) for a later Paste, which will
// copy them, leaving these where they are.
func (r *Root) copyToClipboard() {
	r.clipboard = r.clipboardTargets()
	r.clipboardCut = false
}

// cutToClipboard is "Cut": same as Copy, except the later Paste will move
// the targets (removing them from here) instead of copying them.
func (r *Root) cutToClipboard() {
	r.clipboard = r.clipboardTargets()
	r.clipboardCut = true
}

// pasteClipboard is "Paste": copies or moves (per clipboardCut) whatever
// Copy/Cut last captured into the directory currently on screen. A no-op
// if nothing was ever copied/cut.
//
// Each target that would collide with an existing entry in the current
// directory is skipped with an error — asking "overwrite?" once per
// colliding file in a multi-file paste isn't built yet (a known
// simplification; fsops.Copy/Move's force parameter is where that would
// hook in). Only the first error is reported, to avoid stacking one error
// overlay per failed file; the rest of the paste still runs to
// completion rather than stopping at the first collision.
func (r *Root) pasteClipboard() {
	if len(r.clipboard) == 0 {
		return
	}

	var firstErr error
	for _, src := range r.clipboard {
		dst := filepath.Join(r.panel.path, filepath.Base(src))
		var err error
		if r.clipboardCut {
			err = fsops.Move(src, dst, false)
		} else {
			err = fsops.Copy(src, dst, false)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if err := r.panel.load(r.panel.path); err != nil {
		firstErr = err // the reload failing is more urgent to report than a copy conflict
	} else if r.clipboardCut && firstErr == nil {
		r.clipboard = nil // moved away cleanly; nothing left to paste again
	}

	if firstErr != nil {
		r.showError(firstErr)
	}
}

// openChown is the context menu's "chown": prompts for chown(1)'s own
// "owner[:group]" syntax and applies it to the target. target is captured
// up front rather than read from r.target inside the callback — nothing
// else changes it while the prompt is open in this single-threaded UI,
// but reading it early avoids relying on that staying true.
func (r *Root) openChown() {
	target := r.target
	r.openPrompt("chown (owner[:group]):", "", func(text string) {
		uid, gid, err := fsops.ParseOwnerGroup(text)
		if err != nil {
			r.showError(err)
			return
		}
		if err := fsops.Chown(target, uid, gid); err != nil {
			r.showError(err)
			return
		}
		r.showError(r.panel.load(r.panel.path))
	})
}

// openChmod is the context menu's "chmod": prompts for an octal
// permission string (e.g. "755") and applies it to the target.
func (r *Root) openChmod() {
	target := r.target
	r.openPrompt("chmod (octal, e.g. 755):", "", func(text string) {
		mode, err := fsops.ParseMode(text)
		if err != nil {
			r.showError(err)
			return
		}
		if err := fsops.Chmod(target, mode); err != nil {
			r.showError(err)
			return
		}
		r.showError(r.panel.load(r.panel.path))
	})
}
