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
// plus a right-click context menu and the rename prompt it opens. Pages
// layers the menu/prompt as floating overlays on top of the still-visible
// panel; Root owns the logic for what appears where and how focus hands
// back and forth between them.
//
// Phase 1's context menu has two actions: Rename and Info. See
// docs/whitepaper.md for the other actions (copy path, copy-to/move-to)
// planned for later phases.
type Root struct {
	*tview.Pages

	app *tview.Application

	panel       *Panel
	menu        *tview.List
	rename      *tview.InputField
	info        *tview.TextView
	quitConfirm *tview.List

	// target is the absolute path the context menu / rename prompt is
	// currently acting on. Only meaningful while one of them is visible.
	target string
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
	r.menu.SetBorderPadding(0, 0, 1, 1) // 1-char left/right padding; no border needed for this
	r.menu.AddItem("Rename", "", 0, r.openRename)
	r.menu.AddItem("Info", "", 0, r.openInfo)
	r.menu.SetDoneFunc(r.closeMenu) // Escape

	r.rename = tview.NewInputField().SetLabel("New name: ")
	r.rename.SetFieldBackgroundColor(accentBackgroundColor)
	r.rename.SetBackgroundColor(accentBackgroundColor)
	r.rename.SetLabelColor(tcell.ColorWhite)
	r.rename.SetFieldTextColor(tcell.ColorWhite)
	r.rename.SetDoneFunc(r.finishRename) // Enter or Escape

	r.info = r.newInfoView()

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
	r.AddPage(quitConfirmPage, r.quitConfirm, false, false)

	panel.SetMouseCapture(r.captureMouse)

	return r, nil
}

// RequestQuit shows a confirmation overlay instead of quitting right
// away — Ctrl+X (see cmd/breakthrough) is easy to hit by accident, so the
// application only actually stops once the user picks "Quit breakthrough"
// from this overlay (or presses Enter, since it's the default selection).
func (r *Root) RequestQuit() {
	width, height := listSize(r.quitConfirm)

	_, _, screenWidth, screenHeight := r.GetRect() // Root fills the whole screen
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2

	r.quitConfirm.SetRect(x, y, width, height)
	r.quitConfirm.SetCurrentItem(0)
	r.HidePage(contextMenuPage)
	r.HidePage(renamePage)
	r.HidePage(infoPage)
	r.ShowPage(quitConfirmPage)
}

// confirmQuit is the quit overlay's "Quit breakthrough" action.
func (r *Root) confirmQuit() {
	r.app.Stop()
}

// cancelQuit hides the quit overlay without taking any action (Escape or
// "Cancel") and hands focus back to whatever was visible underneath.
func (r *Root) cancelQuit() {
	r.HidePage(quitConfirmPage)
}

// captureMouse intercepts right-clicks on the panel to open the context
// menu. Everything else (left-click, scrolling) passes through unchanged
// to the panel's own handling — see Panel.onSelect for that.
func (r *Root) captureMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseRightClick {
		return action, event
	}

	x, y := event.Position()
	name, ok := r.panel.EntryAt(y)
	if !ok || name == ".." {
		return action, event // nothing sensible to act on
	}

	r.target = filepath.Join(r.panel.path, name)
	r.showMenu(x, y)

	return tview.MouseConsumed, nil
}

// showMenu positions the context menu near (x, y), clamped to the panel's
// inner rect so it doesn't get drawn partly off-screen, and reveals it as
// an overlay on top of the still-visible panel.
func (r *Root) showMenu(x, y int) {
	width, height := listSize(r.menu)

	px, py, pw, ph := r.panel.GetInnerRect()
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

	r.menu.SetRect(x, y, width, height)
	r.menu.SetCurrentItem(0)
	r.HidePage(renamePage)
	r.ShowPage(contextMenuPage)
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

// closeMenu hides the context menu without taking any action (Escape) and
// hands focus back to the panel underneath.
func (r *Root) closeMenu() {
	r.HidePage(contextMenuPage)
}

// openRename is the context menu's "Rename" action: it swaps the menu for
// the rename prompt, pre-filled with the target's current name, positioned
// over the same area.
func (r *Root) openRename() {
	const width, height = 30, 1

	x, y, _, _ := r.menu.GetRect()
	if px, py, pw, ph := r.panel.GetInnerRect(); pw > 0 {
		if x+width > px+pw {
			x = px + pw - width
		}
		if y+height > py+ph {
			y = py + ph - height
		}
	}

	r.rename.SetText(filepath.Base(r.target))
	r.rename.SetRect(x, y, width, height)

	r.HidePage(contextMenuPage)
	r.ShowPage(renamePage)
}

// finishRename handles Enter (submit) and Escape/Tab (cancel) in the
// rename prompt, then always hands focus back to the panel.
func (r *Root) finishRename(key tcell.Key) {
	defer r.HidePage(renamePage)

	if key != tcell.KeyEnter {
		return // cancelled
	}

	newName := r.rename.GetText()
	if newName == "" || newName == filepath.Base(r.target) {
		return
	}

	// Errors (e.g. destination already exists) are swallowed for now:
	// Phase 1 has no error dialog yet. The panel reload below simply
	// shows whatever the directory actually looks like afterwards,
	// which for a refused rename means the old name is still there —
	// visible, if not explained.
	_, _ = fsops.Rename(r.target, newName)
	_ = r.panel.load(r.panel.path)
}
