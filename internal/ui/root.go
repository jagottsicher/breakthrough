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
)

// Root is breakthrough's top-level UI for Phase 1: the directory panel,
// plus a right-click context menu and the rename prompt it opens. Pages
// layers the menu/prompt as floating overlays on top of the still-visible
// panel; Root owns the logic for what appears where and how focus hands
// back and forth between them.
//
// Phase 1 intentionally has exactly one context menu action: Rename. See
// docs/whitepaper.md for the other actions (copy path, properties,
// copy-to/move-to) planned for later phases.
type Root struct {
	*tview.Pages

	panel  *Panel
	menu   *tview.List
	rename *tview.InputField

	// target is the absolute path the context menu / rename prompt is
	// currently acting on. Only meaningful while one of them is visible.
	target string
}

// NewRoot creates the top-level UI rooted at path.
func NewRoot(path string) (*Root, error) {
	panel, err := NewPanel(path)
	if err != nil {
		return nil, err
	}

	r := &Root{
		Pages: tview.NewPages(),
		panel: panel,
	}

	r.menu = tview.NewList().ShowSecondaryText(false)
	r.menu.SetBorder(true).SetTitle(" Actions ")
	r.menu.AddItem("Rename", "", 0, r.openRename)
	r.menu.SetDoneFunc(r.closeMenu) // Escape

	r.rename = tview.NewInputField().SetLabel("New name: ")
	r.rename.SetBorder(true).SetTitle(" Rename ")
	r.rename.SetDoneFunc(r.finishRename) // Enter or Escape

	r.AddPage(panelPage, panel, true, true)
	r.AddPage(contextMenuPage, r.menu, false, false)
	r.AddPage(renamePage, r.rename, false, false)

	panel.SetMouseCapture(r.captureMouse)

	return r, nil
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
	const width, height = 20, 3 // border + one "Rename" item

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

// closeMenu hides the context menu without taking any action (Escape) and
// hands focus back to the panel underneath.
func (r *Root) closeMenu() {
	r.HidePage(contextMenuPage)
}

// openRename is the context menu's "Rename" action: it swaps the menu for
// the rename prompt, pre-filled with the target's current name, positioned
// over the same area.
func (r *Root) openRename() {
	x, y, w, _ := r.menu.GetRect()

	r.rename.SetText(filepath.Base(r.target))
	r.rename.SetRect(x, y, w, 3)

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
