package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

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

// inTrash reports whether the panel is currently browsing the current
// trash's own files/ subdirectory (see openTrash) — the one place
// Restore actually makes sense. Used by buildButtonBar to swap Trashbin
// for Restore and hide Trash entirely, and by moveSelectionToTrash to
// redirect an already-trashed selection to Remove instead of trying to
// trash it a second time. False, not an error to report, if the trash
// directory can't be resolved at all — the same "nothing special going
// on" reading buildButtonBar's own no-error-surfacing callers need.
func (r *Root) inTrash() bool {
	dir, err := r.trashDir()
	if err != nil {
		return false
	}
	return r.panel.path == fsops.FilesDir(dir)
}

// moveSelectionToTrash is the context menu's "Move to Trash", and
// (through TrashShortcut) Entf's action. No confirmation — per this
// project's own feature notes, moving to the trash is the reversible
// action by design, unlike Remove/Empty Trash below. A directory goes in
// whole, recursively, the same way a plain move always has — there is
// nothing to warn about since nothing is actually being destroyed yet.
//
// Redirects to openRemoveConfirm instead when r.inTrash(): an item
// that's already in the trash has nowhere sensible left to be "moved to
// trash" a second time, so this is the one case where Entf (and this
// same context menu entry, still wired here) means Remove instead — its
// own confirmation dialog is exactly the "are you sure" a second,
// otherwise-silent send-to-trash would need of its own anyway. The
// button bar hides its own "Trash" button entirely in this state
// instead of relabeling it (see buildButtonBar) — this redirect is what
// still fires if Entf gets pressed out of habit regardless.
func (r *Root) moveSelectionToTrash() {
	if r.inTrash() {
		r.openRemoveConfirm()
		return
	}

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
		} else if err == nil {
			// Cleared ("") rather than followed to its real new location:
			// that's an obscure, hash-named path under trashDir, not
			// somewhere worth showing Details pointed at — "(nothing
			// selected)" (see refreshDetailsIfShowing's own doc comment)
			// is the more honest answer once the entry Details was
			// showing simply isn't at src any more.
			r.refreshDetailsIfShowing(src, "")
		}
	}
	r.panel.deselectAll()
	r.reloadPanel(firstErr)
}

// openTrash is the context menu's "Go to Trash", and (through
// TrashbinShortcut) Ctrl+B's action: navigates the panel straight to
// the current trash's files/ subdirectory (see fsops.FilesDir) — the
// one place browsing/Restore actually works (see
// restoreSelectionFromTrash) — without the user needing to know or type
// its path, which for the session-scoped (opt-in, trash_persistent =
// false) mode includes a random per-run session ID buried under
// $XDG_RUNTIME_DIR. Goes through Panel.navigate, not a plain load, so
// this excursion is a real history entry too — per the user's own
// explicit request that visiting the trash not be invisible to
// Back/Forward the way it used to be, unlike a real directory.
func (r *Root) openTrash() {
	dir, err := r.trashDir()
	if err != nil {
		r.showError(err)
		return
	}
	if err := r.panel.navigate(fsops.FilesDir(dir)); err != nil {
		r.showError(err)
	}
}

// openRemoveConfirm is the context menu's "Remove", and (through
// PurgeShortcut) Ctrl+R/Ctrl+Entf's action: always asks first (Cancel
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
			} else if err == nil {
				r.refreshDetailsIfShowing(target, "") // permanently gone — see refreshDetailsIfShowing's own doc comment
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
		} else if err == nil {
			// item.Path(dir), not target: target is already
			// filepath.Clean(target), but the map (and so what Details
			// could actually have keyed itself on while browsing the
			// trash) is built from item.Path(dir) specifically — the two
			// only differ if target itself wasn't already clean, but
			// matching the same value used to look item up here is the
			// robust way to say that rather than assuming they agree.
			r.refreshDetailsIfShowing(item.Path(dir), item.OriginalPath)
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

// TrashShortcut and PurgeShortcut are Entf and Ctrl+R/Ctrl+Entf's global
// actions (see cmd/breakthrough and acceptsGlobalShortcut). Entf
// deliberately triggers the safe action (Trash), not Purge, matching
// both the physical key's own label and the near-universal file-manager
// convention (Windows/macOS/GNOME/Total Commander: the bare Delete key is
// always the reversible one, a modifier is required for the permanent
// variant). Ctrl+Delete for Purge is best-effort — see cmd/breakthrough's
// own comment on tcell's modifier-detection caveat; Ctrl+R is the
// reliable path regardless. Unlike Entf, Ctrl+R needs no fallthrough
// guard at the cmd/breakthrough dispatch level: nothing in bashLine
// binds it, so it joins Edit/Look/Rename/etc.'s "always consumed, no-op
// internally if the precondition fails" group instead.
//
// Trash no longer has a Ctrl-letter binding of its own at all — Entf
// already covered it on its own, matching the physical key's own label,
// which freed Ctrl+T to become a second keyboard path to the tab
// switcher instead (see cmd/breakthrough's own KeyCtrlT case) per the
// user's own explicit request, rather than that binding sitting unused
// as a pure duplicate of a key that already existed.
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

// TrashbinShortcut is Ctrl+B's global action (see cmd/breakthrough and
// acceptsGlobalShortcut) — "B" for "Bin", the one Ctrl-letter mnemonic
// for Go to Trash that was actually still free. Needs the same
// dispatch-level AcceptsGlobalShortcut check Ctrl+P/T/S do rather than
// joining TrashShortcut/PurgeShortcut's "always consumed" group above:
// tview's TextArea already binds Ctrl+B to its own PgUp-style movement
// (see acceptsGlobalShortcut's own doc comment), so bashLine needs to
// keep seeing it while it has focus.
func (r *Root) TrashbinShortcut() {
	if r.acceptsGlobalShortcut() {
		r.openTrash()
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

// pruneTrashAtStartup applies the user's own age/quota trash policy
// (see config.Settings' trash_max_age_days/trash_quota_percent, and
// fsops.PruneTrash's own doc comment for why once per run rather than
// on every trash operation) — called once from NewRoot. Returns a
// one-time notice for NewRoot to combine with its own other startup
// notices (see its caller — config warnings use the same showError
// overlay, and calling that twice in a row would just silently
// overwrite whichever came first) — "" if nothing was removed.
//
// Best-effort like the rest of this file's own background bookkeeping
// (see appendBashHistory): a failure to even resolve or read the trash
// directory is silently ignored here rather than greeting a fresh run
// with an error dialog over something that hasn't actually happened
// yet. A partial removal failure (see PruneTrash's own return) is
// swallowed the same way — whatever it did manage to remove is still
// worth reporting, and there's no actionable fix a dialog could offer
// for e.g. one stuck file at startup anyway.
func (r *Root) pruneTrashAtStartup() string {
	dir, err := r.trashDir()
	if err != nil {
		return ""
	}

	opts := fsops.PruneTrashOptions{
		MaxAge:       time.Duration(r.settings.TrashMaxAgeDays) * 24 * time.Hour,
		QuotaPercent: r.settings.TrashQuotaPercent,
	}
	result, _ := fsops.PruneTrash(dir, opts)
	if result.Removed() == 0 {
		return ""
	}
	return trashPruneMessage(result)
}

// trashPruneMessage words pruneTrashAtStartup's own one-time notice —
// only ever called once Removed() > 0 has already confirmed there's
// something to report. Irreversibly removing files, even ones the user
// already sent to the trash, earns the same attention-grabbing
// treatment a real error would (see showError, the only "surface a
// message" channel this app has) — "klar kommuniziert", not just
// quietly logged.
func trashPruneMessage(result fsops.PruneTrashResult) string {
	var parts []string
	if result.RemovedByAge > 0 {
		parts = append(parts, fmt.Sprintf("%d older than the configured age limit", result.RemovedByAge))
	}
	if result.RemovedByQuota > 0 {
		parts = append(parts, fmt.Sprintf("%d over the configured quota", result.RemovedByQuota))
	}
	return fmt.Sprintf("Trash cleanup removed %d item(s) on startup: %s", result.Removed(), strings.Join(parts, ", "))
}

// describeTrashRows is Root's own Panel.onDescribeRows (see its own doc
// comment) — wired once in NewRoot. Reports isTrashDir true exactly
// when dir is the current trash's own files/ subdirectory (the same
// check inTrash makes against r.panel.path, just against whatever
// directory load() is about to render instead — load() itself hasn't
// updated r.panel.path yet by the time this runs), regardless of
// whether ListTrash finds anything in it: a brand-new, empty trash is
// still "the trash" as far as the Modified column's own label is
// concerned.
//
// A ListTrash failure (or the trash directory failing to resolve at
// all) degrades to isTrashDir false, nil descriptions rather than an
// error: load() itself has no error-reporting path for what its own
// row-description hook thinks, and falling back to the raw on-disk
// name/mtime is no worse than what browsing the trash always showed
// before this existed.
func (r *Root) describeTrashRows(dir string) (map[string]rowDescription, bool) {
	trashDir, err := r.trashDir()
	if err != nil {
		return nil, false
	}
	if filepath.Clean(dir) != filepath.Clean(fsops.FilesDir(trashDir)) {
		return nil, false
	}

	items, err := fsops.ListTrash(trashDir)
	if err != nil {
		return nil, true // still the trash — just couldn't read its own contents
	}

	descriptions := make(map[string]rowDescription, len(items))
	for _, item := range items {
		descriptions[item.Path(trashDir)] = rowDescription{name: item.OriginalPath, modTime: item.DeletedAt}
	}
	return descriptions, true
}
