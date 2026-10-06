package ui

import (
	"fmt"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/filelabels"
)

// labelsPersistPath is filelabels.DefaultPath, indirected through a
// package-level var the same way notifyPersistPath (notifybar.go)
// already is — so a test can point NewRoot's own
// filelabels.NewWithPersistence call somewhere isolated, or (as
// TestMain/bottombar_test.go's own override does by default) disable
// persistence outright by returning "", instead of every one of this
// package's hundreds of NewRoot calls touching whatever real file
// session.StateDir resolves to on the machine running `go test`.
var labelsPersistPath = filelabels.DefaultPath

// labelSwatch is the 3-character colored background every label-id
// surface shows next to a label's own name — labelMenuRows' own picker
// and context-menu rows, and labelStatusBarText's status-bar segment
// below — built once here rather than duplicated per surface. id must
// be 1-9; label 0 ("no label") has no color and is never passed here.
func labelSwatch(theme config.ResolvedTheme, id int) string {
	return fmt.Sprintf("[:%s:] %d [-:-:-]", colorTag(theme.LabelBackground(id)), id)
}

// labelMenuRow is one row the "zl" chord's own quick picker shows — see
// labelMenuRows.
type labelMenuRow struct {
	id   int
	text string
}

// labelMenuRows builds the ten rows every color-label picker shows — a
// plain "0  no label" first (label 0 has no color at all, and is
// fixed, never renamable — see config.Settings.LabelName's own doc
// comment), then "1" through "9", each as a 3-character swatch in that
// label's own resolved background color (" 1 ", " 2 ", ...) followed
// by its current display name — per the user's own explicit request
// and suggested format, after an earlier, plain-text-only revision
// read as "doof" (an unhelpful word-only list — a label is a color
// first, a name second, and the picker said nothing about the color at
// all). Both r.picker (see openLabelMenu) and the context menu's own
// "Label" submenu (see labelSubmenuEntries) render tview's "[...]"
// style tags directly in a tview.List's main text by default (verified
// directly against tview.List's own NewList/Draw — mainStyleTags
// defaults to true, not guessed), so the swatch is built once, here,
// rather than duplicated per surface.
//
// Built as a plain function returning data, not tied to either overlay
// widget, so labelSubmenuEntries can reuse this exact row list instead
// of duplicating the swatch formatting.
func labelMenuRows(theme config.ResolvedTheme, settings config.Settings) []labelMenuRow {
	rows := make([]labelMenuRow, 0, filelabels.MaxLabelID+1)
	rows = append(rows, labelMenuRow{0, " 0   no label"})
	for id := 1; id <= filelabels.MaxLabelID; id++ {
		rows = append(rows, labelMenuRow{id, fmt.Sprintf("%s %s", labelSwatch(theme, id), settings.LabelName(id))})
	}
	return rows
}

// errLabelsUnavailable is what openLabelMenu reports when r.labels is
// nil — session.StateDir() itself couldn't be resolved (see
// filelabels.DefaultPath's own doc comment), so there is nowhere to
// persist a label assignment at all. The chord still works in every
// other respect once that's fixed; this isn't a feature-disabled
// message, just an explanation for why nothing happened.
var errLabelsUnavailable = fmt.Errorf("color labels unavailable — no writable state directory found")

// labelTargets resolves what the "zl" chord (or, once built, the
// context menu's own "Label" entries) actually applies a label to —
// deliberately NOT selectedOrCurrentPaths' own "the checked selection
// if there is one, otherwise just the cursor row" convention every
// other chord-driven action in this app already follows (see Trash/
// Remove/Compress/Batch Rename/chmod/Sed Replace/Duplicate, all of
// which call that one directly): a checked selection you aren't
// currently pointing at (cursor row, or a right-click's own target)
// must stay completely out of it, per the user's own explicit, live-
// tested report — labeling six files one at a time, five already
// checked from an earlier, unrelated action, then right-clicking a
// sixth and unchecked one to label just that one, must never silently
// also relabel the five. Only once the cursor/right-click target is
// itself one of the checked rows does this fall back to the whole
// checked selection — the ordinary "act on everything I've marked"
// case every other action's convention already covers, just reached
// here through its own, narrower gate instead of unconditionally.
func (r *Root) labelTargets() []string {
	_, path, ok := r.panel.CurrentRowPath()
	if !ok {
		return nil
	}
	if r.panel.selected[path] {
		return r.panel.SelectedPathsInDisplayOrder()
	}
	return []string{path}
}

// openLabelMenu is the "z" chord family's own "l" member ("Label", see
// keymap.go): sets or clears a color label on whatever labelTargets
// resolves to — see its own doc comment for exactly which row(s) that
// is.
//
// Reuses r.picker (see picker.go's own doc comment on it being a
// generic, shared "pick one of a short list" overlay, already reused
// by the owner/group picker, the rsync tab picker, and the SSH key
// copy-to-tab picker) rather than a dedicated widget of its own — ten
// rows is exactly the shape that overlay already exists for.
func (r *Root) openLabelMenu() {
	if r.labels == nil {
		r.showError(errLabelsUnavailable)
		return
	}
	if r.panel.inArchiveView() {
		r.showError(errNotSupportedInArchive)
		return
	}
	if r.panel.isRemote() {
		r.showError(errNotSupportedRemote)
		return
	}
	targets := r.labelTargets()
	if len(targets) == 0 {
		return
	}

	r.picker.Clear()
	for _, row := range labelMenuRows(r.theme, r.settings) {
		row := row // captured per-iteration, not the shared loop variable
		r.picker.AddItem(row.text, "", 0, func() {
			r.hideOverlay()
			r.applyLabel(targets, row.id)
		})
	}
	r.picker.SetDoneFunc(func() { r.hideOverlay() })

	width, _ := listSize(r.picker)
	height := pickerHeight
	rows := len(labelMenuRows(r.theme, r.settings))
	if rows < height {
		height = rows
	}
	x, y := r.menuAnchorForCurrentRow()
	x, y, width, height = r.clampToPanel(x, y, width, height)
	r.picker.SetRect(x, y, width, height)
	r.picker.SetCurrentItem(0)

	r.pushOverlay(pickerPage, r.picker, nil)
}

// menuLabelAvailable gates the context menu's own "Label" group (see
// labelSubmenuEntries) — hidden entirely rather than shown-but-broken
// whenever openLabelMenu's own three preconditions wouldn't be met
// either: no writable state directory, a remote panel, or browsing
// inside an archive.
func menuLabelAvailable(r *Root) bool {
	return r.labels != nil && !r.panel.isRemote() && !r.panel.inArchiveView()
}

// labelSubmenuEntries builds the context menu's own "Label" submenu —
// the same ten rows labelMenuRows already renders for r.picker (see its
// own doc comment on the shared swatch format), just as menuEntry
// leaves instead of tview.List rows. dynamicLabel (not a fixed label)
// because the swatch/name both depend on live state (the active color
// scheme, a label's own current display name) that can change between
// one render of this menu and the next.
//
// Each entry's own action calls hideOverlay explicitly before
// applyLabel — unlike e.g. "Select all"/"Deselect all" (see their own,
// no-hide convention), picking a label is a single, final choice from
// a list of mutually exclusive options, the same shape Options' own
// color-scheme picker already closes on, not a toggle worth leaving
// the menu open to flip again immediately after.
func labelSubmenuEntries() []menuEntry {
	entries := make([]menuEntry, 0, filelabels.MaxLabelID+1)
	for id := 0; id <= filelabels.MaxLabelID; id++ {
		id := id // captured per-iteration, not the shared loop variable
		entries = append(entries, menuEntry{
			dynamicLabel: func(r *Root) string {
				return labelMenuRows(r.theme, r.settings)[id].text
			},
			action: func(r *Root) {
				r.hideOverlay()
				r.applyLabel(r.labelTargets(), id)
			},
		})
	}
	return entries
}

// applyLabel is openLabelMenu's own picker action: assigns id (0
// clears) to every one of targets in a single save (see
// filelabels.Store.SetMany), then repaints every open tab — the label
// store is shared app-wide, so the same path might be on screen in more
// than one tab (see Panel's own labels field doc comment).
func (r *Root) applyLabel(targets []string, id int) {
	if err := r.labels.SetMany(targets, id); err != nil {
		r.activityLog.Error(activitylog.CategoryFileOps, fmt.Sprintf("set color label: %v", err))
		r.showError(err)
		return
	}
	verb := "set"
	if id == 0 {
		verb = "cleared"
	}
	r.activityLog.Detail(activitylog.CategoryFileOps, fmt.Sprintf("%s color label on %d item(s)", verb, len(targets)))
	r.forEachTab(func(p *Panel) { p.repaintLabels() })
}

// labelStatusBarText is buildStatusBar's own color-label segment (see
// config.Settings.StatusBarShowLabel) — the cursor row's own label, as
// the same swatch-plus-name labelMenuRows already renders, or ok false
// when there's nothing to show: no current row, the row isn't labelable
// at all (see Panel.labelablePath — the ".." row, a remote panel, an
// archive-member hit, or browsing inside an archive), or it simply
// carries no label right now. The overwhelmingly common case, so
// quietly showing nothing (like every other optional status-bar
// segment already does) rather than a placeholder is the right default.
func labelStatusBarText(p *Panel, theme config.ResolvedTheme, settings config.Settings) (string, bool) {
	row, _ := p.table.GetSelection()
	ref, ok := p.rowRef(row)
	if !ok || !p.labelablePath(ref) {
		return "", false
	}
	id := p.labels.Get(ref.path)
	if id == 0 {
		return "", false
	}
	return fmt.Sprintf("%s %s", labelSwatch(theme, id), settings.LabelName(id)), true
}
