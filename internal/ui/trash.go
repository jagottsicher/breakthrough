package ui

import (
	"fmt"
	"path/filepath"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/session"
)

// trashDir resolves the trash directory to use right now, honoring the
// user's trash_persistent setting (see internal/config.Settings) — see
// session.TrashDir for what each mode actually means.
func (r *Root) trashDir() (string, error) {
	return session.TrashDir(r.settings.TrashPersistent)
}

// reloadPanel reloads the panel's current directory (the same pasteInto
// already does after Copy/Cut/Paste) and, if precedingErr is non-nil,
// surfaces it — precedingErr takes priority since a reload failure is
// rarer and less actionable than whatever fsops call actually failed.
func (r *Root) reloadPanel(precedingErr error) {
	if err := r.panel.load(r.panel.path); err != nil && precedingErr == nil {
		precedingErr = err
	}
	if precedingErr != nil {
		r.showError(precedingErr)
	}
}

// moveSelectionToTrash is the context menu's "Move to Trash", and
// (through TrashShortcut) Ctrl+T/Entf's action. No confirmation — per
// this project's own feature notes, moving to the trash is the reversible
// action by design, unlike Remove/Empty Trash below. A directory goes in
// whole, recursively, the same way a plain move always has — there is
// nothing to warn about since nothing is actually being destroyed yet.
func (r *Root) moveSelectionToTrash() {
	targets := r.selectedOrCurrentPaths()
	if len(targets) == 0 {
		return
	}
	dir, err := r.trashDir()
	if err != nil {
		r.showError(err)
		return
	}

	var firstErr error
	for _, src := range targets {
		if err := fsops.MoveToTrash(src, dir); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	r.panel.deselectAll()
	r.reloadPanel(firstErr)
}

// openTrash is the context menu's "Go to Trash": navigates the panel
// straight to the current trash's files/ subdirectory (see
// fsops.FilesDir) — the one place browsing/Restore actually works (see
// restoreSelectionFromTrash) — without the user needing to know or type
// its path, which for the session-scoped default includes a random
// per-run session ID buried under $XDG_RUNTIME_DIR.
func (r *Root) openTrash() {
	dir, err := r.trashDir()
	if err != nil {
		r.showError(err)
		return
	}
	if err := r.panel.load(fsops.FilesDir(dir)); err != nil {
		r.showError(err)
	}
}

// openRemoveConfirm is the context menu's "Remove", and (through
// PurgeShortcut) Ctrl+P/Ctrl+Entf's action: always asks first (Cancel
// preselected — see newPurgeConfirm), wording the message concretely for
// one file, one directory (with its real item count), or several targets
// at once.
func (r *Root) openRemoveConfirm() {
	targets := r.selectedOrCurrentPaths()
	if len(targets) == 0 {
		return
	}
	r.openPurgeConfirm(removeConfirmMessage(targets), func() {
		var firstErr error
		for _, target := range targets {
			if err := fsops.PurgeCompletely(target); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		r.panel.deselectAll()
		r.reloadPanel(firstErr)
	})
}

// removeConfirmMessage words the Remove confirmation for exactly what's
// about to happen: irreversible, and — for a single directory — with a
// real count instead of a vague "and its contents".
func removeConfirmMessage(targets []string) string {
	if len(targets) == 1 {
		return removeConfirmSingleMessage(targets[0])
	}
	return fmt.Sprintf("Permanently delete %d selected items?", len(targets))
}

func removeConfirmSingleMessage(target string) string {
	name := filepath.Base(target)
	if count, err := fsops.CountEntries(target); err == nil && count > 0 {
		return fmt.Sprintf("Permanently delete \"%s\" and %d items inside it?", name, count)
	}
	return fmt.Sprintf("Permanently delete \"%s\"?", name)
}

// restoreSelectionFromTrash is the context menu's "Restore from Trash":
// only meaningful while the panel is actually showing the trash's own
// files/ subdirectory (see fsops.FilesDir — trashDir itself only ever
// contains files/ and info/, never a trashed item directly) — anywhere
// else this reports a plain error via the same overlay every other fsops
// failure already uses, rather than silently doing nothing.
func (r *Root) restoreSelectionFromTrash() {
	dir, err := r.trashDir()
	if err != nil {
		r.showError(err)
		return
	}
	filesDir := fsops.FilesDir(dir)
	if filepath.Clean(r.panel.path) != filepath.Clean(filesDir) {
		r.showError(fmt.Errorf("restore only works while viewing the trash (%s)", filesDir))
		return
	}

	items, err := fsops.ListTrash(dir)
	if err != nil {
		r.showError(err)
		return
	}
	targets := r.selectedOrCurrentPaths()
	if len(targets) == 0 {
		return
	}

	byPath := make(map[string]fsops.TrashItem, len(items))
	for _, item := range items {
		byPath[filepath.Clean(item.Path(dir))] = item
	}

	var firstErr error
	for _, target := range targets {
		item, ok := byPath[filepath.Clean(target)]
		if !ok {
			continue
		}
		if err := fsops.RestoreFromTrash(item, dir); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	r.panel.deselectAll()
	r.reloadPanel(firstErr)
}

// openEmptyTrashConfirm is the context menu's "Empty Trash" — same
// Cancel-preselected confirmation as Remove, since it's equally
// irreversible.
func (r *Root) openEmptyTrashConfirm() {
	dir, err := r.trashDir()
	if err != nil {
		r.showError(err)
		return
	}
	items, err := fsops.ListTrash(dir)
	if err != nil {
		r.showError(err)
		return
	}
	if len(items) == 0 {
		return
	}

	r.openPurgeConfirm(fmt.Sprintf("Permanently empty the trash (%d items)?", len(items)), func() {
		_, err := fsops.EmptyTrash(dir)
		r.reloadPanel(err)
	})
}

// TrashShortcut and PurgeShortcut are Ctrl+T/Entf and Ctrl+P/Ctrl+Entf's
// global actions (see cmd/breakthrough and acceptsGlobalShortcut). Entf
// deliberately triggers the safe action (Trash), not Purge, matching
// both the physical key's own label and the near-universal file-manager
// convention (Windows/macOS/GNOME/Total Commander: the bare Delete key is
// always the reversible one, a modifier is required for the permanent
// variant). Ctrl+Delete for Purge is best-effort — see cmd/breakthrough's
// own comment on tcell's modifier-detection caveat; Ctrl+P is the
// reliable path regardless.
func (r *Root) TrashShortcut() {
	if r.acceptsGlobalShortcut() {
		r.moveSelectionToTrash()
	}
}

func (r *Root) PurgeShortcut() {
	if r.acceptsGlobalShortcut() {
		r.openRemoveConfirm()
	}
}

// newPurgeConfirm builds the shared Remove/Empty-Trash confirmation list,
// called once from NewRoot the same way quitConfirm is.
//
// Deliberately NOT SetCurrentItem(0) the way RequestQuit's quitConfirm
// is: Ctrl+Q is already a single, deliberate keypress toward quitting, so
// defaulting to "Quit" on Enter is low-friction and cheaply undone
// (restart the app). Remove/Empty Trash are unrecoverable data loss from
// a single stray Enter — see openPurgeConfirm, which always preselects
// index 1 ("Cancel") instead.
func (r *Root) newPurgeConfirm() *tview.List {
	l := tview.NewList().ShowSecondaryText(false)
	l.SetHighlightFullLine(true)
	l.SetBorderPadding(0, 0, 1, 1)
	l.AddItem("", "", 0, nil) // index 0: message, set fresh by openPurgeConfirm before every show
	l.AddItem("Cancel", "", 0, r.cancelPurge)
	l.AddItem("Yes, delete permanently", "", 0, r.confirmPurge)
	l.SetDoneFunc(r.cancelPurge) // Escape
	return l
}

// openPurgeConfirm shows message with Cancel preselected (see
// newPurgeConfirm) and runs action only if the user explicitly moves
// focus to and confirms "Yes, delete permanently" — pressing Enter again
// without moving focus, or pressing Escape, always lands on cancelPurge
// instead, never action.
func (r *Root) openPurgeConfirm(message string, action func()) {
	r.pendingPurge = action
	r.purgeConfirm.SetItemText(0, message, "")

	width, height := listSize(r.purgeConfirm)
	_, _, screenWidth, screenHeight := r.GetRect()
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2

	r.purgeConfirm.SetRect(x, y, width, height)
	r.purgeConfirm.SetCurrentItem(1) // "Cancel" - see newPurgeConfirm's own comment
	r.showOverlay(purgeConfirmPage, r.purgeConfirm)
}

func (r *Root) confirmPurge() {
	action := r.pendingPurge
	r.pendingPurge = nil
	r.hideOverlay()
	if action != nil {
		action()
	}
}

func (r *Root) cancelPurge() {
	r.pendingPurge = nil
	r.hideOverlay()
}
