package ui

import (
	"path/filepath"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

const (
	panelPage       = "panel"
	contextMenuPage = "context-menu"
	renamePage      = "rename"
	quitConfirmPage = "quit-confirm"
)

// Root is breakthrough's top-level UI for Phase 1: the directory panel,
// plus a right-click context menu and the overlays it opens (Info,
// Rename), and a Ctrl+X quit confirmation. Pages layers all of these as
// floating overlays on top of the still-visible panel; Root owns the
// logic for what appears where, giving each overlay real keyboard focus
// while it's shown (see showOverlay/hideOverlay), and closing whichever
// one is open when the user clicks outside it (captureOutsideClick).
//
// Phase 1's context menu has two actions, Info (first, default-selected)
// and Rename. See docs/whitepaper.md for the other actions (copy path,
// copy-to/move-to) planned for later phases.
type Root struct {
	*tview.Pages

	app *tview.Application

	panel       *Panel
	menu        *tview.List
	rename      *tview.InputField
	info        *tview.TextView
	errorView   *tview.TextView
	quitConfirm *tview.List

	// target is the absolute path the context menu / rename prompt is
	// currently acting on. targetRow is its screen row in the panel's
	// table, used to position the rename field exactly over that row.
	// Only meaningful while one of the overlays below is visible.
	target    string
	targetRow int

	// activePage/activeWidget track whichever overlay (context menu,
	// rename, info, quit confirm) is currently shown, if any — see
	// showOverlay/hideOverlay. This drives both explicit focus handling
	// and captureOutsideClick's "click outside the overlay closes it"
	// behavior.
	activePage   string
	activeWidget tview.Primitive

	// dragStartRow/dragging track a right-button drag in progress, set on
	// MouseRightDown and consumed on MouseRightUp (see captureMouse). A
	// plain right-click (no movement) still opens the context menu as
	// usual — dragging only kicks in once the mouse actually moved, which
	// tview itself already distinguishes: it only synthesizes
	// MouseRightClick when the position didn't change between down and
	// up, so a real drag never reaches that case at all.
	dragStartRow int
	dragging     bool
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
	r.menu.SetBorderPadding(0, 0, 1, 1)       // 1-char left/right padding; no border needed for this
	r.menu.AddItem("Info", "", 0, r.openInfo) // first and default-selected
	r.menu.AddItem("Rename", "", 0, r.openRename)
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
// away — Ctrl+X (see cmd/breakthrough) is easy to hit by accident, so the
// application only actually stops once the user picks "Quit breakthrough"
// from this overlay (or presses Enter, since it's the default selection).
func (r *Root) RequestQuit() {
	// Ctrl+X is a global key capture, so it can arrive while the header
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

// RequestCancel is the Ctrl+X sibling for Ctrl+C (see cmd/breakthrough):
// a global "back out of whatever is open" that behaves like Escape.
// Having it as a real key matters because Escape is deliberately inert
// while the path header is being edited — Ctrl+C is the keyboard way out
// of that, where otherwise only a mouse click would do.
//
// It never quits: stopping breakthrough is Ctrl+X plus a confirmation.
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

// captureMouse intercepts right-button activity on the panel: a plain
// right-click opens the context menu (unchanged from before); a
// right-button drag across rows instead checks all of them, from the
// press row through the release row inclusive, and does not open the
// menu. Everything else (left-click, scrolling) passes through unchanged
// to the panel's own handling — see Panel.activateRow for that.
//
// The click-vs-drag distinction is tview's own, not something tracked
// here: Application.fireMouseActions only synthesizes MouseRightClick
// when the release position matches the press position — a genuine drag
// simply never produces one, only MouseRightDown and MouseRightUp fire.
// So MouseRightUp always runs first, for both a click and a drag; it does
// the range-select and consumes the event only when the release row
// actually differs from the press row, and otherwise steps aside so the
// MouseRightClick that (per tview) is about to follow can open the menu.
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
			r.dragging = true
		} else {
			r.dragging = false
		}
		return action, event // Table has no case for this action; harmless to pass through

	case tview.MouseRightUp:
		wasDragging := r.dragging
		r.dragging = false
		if !wasDragging {
			return action, event
		}
		x, y := event.Position()
		endRow, ok := r.panel.rowIndexAt(x, y)
		if !ok || endRow == r.dragStartRow {
			// Not a real drag (or it ended off the table): let a
			// same-position MouseRightClick, if tview fires one right
			// after this, open the menu as usual.
			return action, event
		}
		r.panel.selectRange(r.dragStartRow, endRow)
		return tview.MouseConsumed, nil

	case tview.MouseRightClick:
		x, y := event.Position()
		path, ok := r.panel.RowAt(x, y)
		if !ok {
			return action, event // nothing sensible to act on
		}
		r.target = path
		r.targetRow = y
		r.showMenu(x, y)
		return tview.MouseConsumed, nil

	default:
		return action, event
	}
}

// showMenu positions the context menu near (x, y), clamped to the panel's
// inner rect so it doesn't get drawn partly off-screen, and reveals it as
// an overlay on top of the still-visible panel.
func (r *Root) showMenu(x, y int) {
	width, height := listSize(r.menu)
	x, y, width, height = r.clampToPanel(x, y, width, height)

	r.menu.SetRect(x, y, width, height)
	r.menu.SetCurrentItem(0)
	r.showOverlay(contextMenuPage, r.menu)
}

// listSize returns a no-border, no-secondary-text List's width — the
// widest item's text plus 1-char left/right padding (see the
// SetBorderPadding calls in NewRoot) — and its height, one row per item.
func listSize(l *tview.List) (width, height int) {
	for i := 0; i < l.GetItemCount(); i++ {
		main, _ := l.GetItemText(i)
		if w := len([]rune(main)); w > width {
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
// target's own row in the table — same x, width, and height as that row —
// so it reads as the row itself becoming editable in place, pre-filled
// with the current name.
func (r *Root) openRename() {
	x, _, width, _ := r.panel.table.GetInnerRect()
	x, y, width, height := r.clampToPanel(x, r.targetRow, width, 1)

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
