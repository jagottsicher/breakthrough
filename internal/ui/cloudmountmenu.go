package ui

import (
	"fmt"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/rclone"
)

// The cloud-mounts dropdown: a plain list of every configured cloud
// mount (internal/rclone.MountEntry), reached from the "c" connect
// menu's own trailing "☁ Cloud mounts" row (see connectionmenu.go's
// own renderConnectionMenu/activateConnectionMenuCell) rather than
// folded into connectionMenuTable itself.
//
// Deliberately a separate dropdown, not a second section inside the
// existing one: a cloud mount is a different kind of entity with a
// different status axis (mounted/not-mounted, not connected/failed/
// untested) and a different row action (mount/unmount, not connect) —
// threading that through connectionMenuTable's own sort/filter/column
// machinery (sized for remotefs.Connection specifically) would have
// turned that machinery generic for no real benefit, where a second,
// much smaller Table costs far less. Same per-cell Table shape
// tabswitcher.go/connectionmenu.go already establish for their own
// dropdowns regardless.
//
// No "+ New" row yet: creating a brand-new rclone remote (provider
// selection, per-backend fields, calling rclone.CreateStaticRemote) is
// its own, separately scoped follow-up — this first slice only
// manages mounts for remotes already configured in rclone, by hand or
// by a later dialog.

// Column indices within cloudMountTable.
const (
	cloudMountColRemote = iota
	cloudMountColStatus
	cloudMountColRemove
)

const cloudMountMenuPage = "cloud-mount-menu"

// cloudMountRemoveGlyph mirrors connectionHistoryRemoveGlyph — the
// same "✕" every per-row remove action in this app already uses.
const cloudMountRemoveGlyph = connectionHistoryRemoveGlyph

// newCloudMountTable builds the dropdown's own Table — no border, one
// column of side padding, cell selection so the remove glyph is its
// own navigable target, the same shape newTabSwitcher/
// newConnectionMenuTable already establish.
func (r *Root) newCloudMountTable() *tview.Table {
	table := tview.NewTable()
	table.SetBorders(false)
	table.SetBorderPadding(0, 0, 1, 1)
	table.SetSelectable(true, true)
	table.SetSelectedFunc(func(row, column int) { r.activateCloudMountCell(row, column) })
	return table
}

// newCloudMountTitleBar mirrors newTabSwitcherTitleBar — a single
// static label, no expand/collapse glyph (this dropdown has no
// expanded mode of its own, there's nothing wide enough to need one).
func (r *Root) newCloudMountTitleBar() *tview.TextView {
	bar := tview.NewTextView()
	bar.SetDynamicColors(false)
	bar.SetText(" Cloud mounts ")
	return bar
}

// cloudMountStatusText reports target's own live status, by shelling
// out through internal/rclone.IsMounted — or, while pending is true
// for this target (a Mount/Unmount call is still running in the
// background, see toggleCloudMount), a transient "Mounting…"/
// "Unmounting…" label instead of a stale "mounted"/"not mounted" that
// would otherwise sit there, unexplained, for however long the real
// subprocess takes.
func (r *Root) cloudMountStatusText(entry rclone.MountEntry) string {
	if pending, ok := r.cloudMountPending[entry.Target]; ok {
		return pending
	}
	mounted, err := rclone.IsMounted(entry.Target)
	if err != nil {
		return "error"
	}
	if mounted {
		return "mounted"
	}
	return "not mounted"
}

// openCloudMountMenu shows the dropdown — reached from the connect
// menu's own trailing row (see connectionmenu.go's own
// activateConnectionMenuCell), which has already called hideOverlay on
// itself first, the same "close the one you came from, then open the
// next" sequence editConnectionHistoryRow already establishes.
func (r *Root) openCloudMountMenu() {
	r.renderCloudMountMenu()
	r.positionCloudMountMenu()
	r.showOverlay(cloudMountMenuPage, r.cloudMountLayout)
}

// positionCloudMountMenu anchors the dropdown at the exact same screen
// position connectionMenuAnchor already computes for the "@" button —
// this dropdown is reached from that same button's own menu, so
// opening in its place reads as "the next layer of the same menu," not
// as a dropdown that jumped somewhere unrelated.
func (r *Root) positionCloudMountMenu() {
	right, top := r.connectionMenuAnchor()
	width, height := r.cloudMountMenuSize()
	x, y, width, height := r.clampToPanel(right-width, top, width, height)
	r.cloudMountLayout.SetRect(x, y, width, height)
}

// cloudMountMenuSize mirrors connectionMenuSize's own non-expanded
// branch: height is the row count plus the title bar's own row, width
// is the widest real column content plus the single-column gaps tview
// leaves between cells plus the table's own left/right border padding.
func (r *Root) cloudMountMenuSize() (width, height int) {
	table := r.cloudMountTable
	rows := table.GetRowCount()
	height = rows + 1

	const columnCount = cloudMountColRemove + 1
	columnWidths := make([]int, columnCount)
	for row := 0; row < rows; row++ {
		for column := range columnWidths {
			cell := table.GetCell(row, column)
			if cell == nil {
				continue
			}
			if w := tview.TaggedStringWidth(cell.Text); w > columnWidths[column] {
				columnWidths[column] = w
			}
		}
	}
	for i, w := range columnWidths {
		width += w
		if i > 0 {
			width++
		}
	}
	width += 2

	const minWidth = 24 // "No cloud mounts configured" plus padding
	if width < minWidth {
		width = minWidth
	}
	return width, height
}

// renderCloudMountMenu (re)builds cloudMountTable fresh — the same
// "rebuilt every open, not kept in sync" shape every other dropdown in
// this app already follows (see renderConnectionMenu's own doc
// comment). A read failure from LoadMountEntries is treated as "no
// mounts configured" rather than surfaced here, the same "absence
// isn't an error" contract its own doc comment promises.
func (r *Root) renderCloudMountMenu() {
	table := r.cloudMountTable
	table.Clear()
	r.cloudMountRows = map[int]rclone.MountEntry{}

	entries, _ := rclone.LoadMountEntries()
	if len(entries) == 0 {
		table.SetCell(0, cloudMountColRemote,
			tview.NewTableCell("No cloud mounts configured").
				SetTextColor(r.theme.MutedTextColor).
				SetSelectable(false))
		table.SetCell(0, cloudMountColStatus, blankConnectionMenuCell())
		table.SetCell(0, cloudMountColRemove, blankConnectionMenuCell())
		return
	}

	for i, entry := range entries {
		entry := entry
		row := i
		label := entry.RemoteName
		if entry.Identifier != "" {
			label += "  (" + entry.Identifier + ")"
		}
		table.SetCell(row, cloudMountColRemote,
			tview.NewTableCell(label).
				SetTextColor(r.theme.Text).
				SetSelectable(true).
				SetClickedFunc(r.clickCloudMountCell(row, cloudMountColRemote)))
		table.SetCell(row, cloudMountColStatus,
			tview.NewTableCell(" "+r.cloudMountStatusText(entry)+" ").
				SetTextColor(r.theme.MutedTextColor).
				SetSelectable(false))
		table.SetCell(row, cloudMountColRemove,
			tview.NewTableCell(" "+cloudMountRemoveGlyph+" ").
				SetTextColor(r.theme.MutedTextColor).
				SetSelectable(true).
				SetClickedFunc(r.clickCloudMountCell(row, cloudMountColRemove)))

		r.cloudMountRows[row] = entry
	}
	table.Select(0, cloudMountColRemote)
}

// activateCloudMountCell is Enter, Space, or a click on one cell — the
// same per-column dispatch shape activateConnectionMenuCell/
// activateTabSwitcherCell already establish.
func (r *Root) activateCloudMountCell(row, column int) {
	entry, ok := r.cloudMountRows[row]
	if !ok {
		return // the "No cloud mounts configured" placeholder row
	}
	switch column {
	case cloudMountColRemove:
		r.removeCloudMount(entry)
	default:
		r.toggleCloudMount(entry)
	}
}

// clickCloudMountCell mirrors clickConnectionMenuCell/
// clickTabSwitcherCell exactly — tview's own Table does not run a
// row's selected function on a click at all, only moves the selection
// there, so every actionable cell needs its own ClickedFunc too.
func (r *Root) clickCloudMountCell(row, column int) func() bool {
	return func() bool {
		r.activateCloudMountCell(row, column)
		return true
	}
}

// toggleCloudMount mounts entry if it isn't currently mounted, or
// unmounts it if it is — run via safeGo so a slow network backend's
// own real Mount/Unmount latency (unlike the local, near-instant case
// this package's own integration test exercises) never blocks the UI
// thread, the same non-blocking-background-work rule this project
// applies to every other potentially slow external-tool call (see
// runConnect's own identical reasoning for SFTP). Deliberately no
// cancellation yet, unlike runConnect's own animated, cancelable
// progress line — internal/rclone has no way to kill an in-flight
// rclone process yet either, so a cancel button here could only ever
// be decorative; left for whenever that's actually built, rather than
// faked now.
func (r *Root) toggleCloudMount(entry rclone.MountEntry) {
	mounted, err := rclone.IsMounted(entry.Target)
	if err != nil {
		r.showError(err)
		return
	}

	if r.cloudMountPending == nil {
		r.cloudMountPending = map[string]string{}
	}
	if mounted {
		r.cloudMountPending[entry.Target] = "unmounting…"
	} else {
		r.cloudMountPending[entry.Target] = "mounting…"
	}
	r.renderCloudMountMenu()

	onPanic := func() {
		delete(r.cloudMountPending, entry.Target)
		r.renderCloudMountMenu()
	}
	r.safeGo("cloud mount toggle", onPanic, func() {
		var opErr error
		if mounted {
			opErr = rclone.Unmount(entry.Target)
		} else {
			if err := rclone.EnsureMountDir(entry.Target); err != nil {
				opErr = err
			} else {
				opErr = rclone.Mount(entry.RemoteName+":", entry.Target, rclone.MountOptions{
					CacheDir: rclone.MountBase(r.settings) + "/.cache/" + entry.RemoteName,
				})
			}
		}
		r.app.QueueUpdateDraw(func() {
			delete(r.cloudMountPending, entry.Target)
			r.renderCloudMountMenu()
			if opErr != nil {
				r.showError(fmt.Errorf("cloud mount %q: %w", entry.RemoteName, opErr))
			}
		})
	})
}

// removeCloudMount drops entry from the persisted list — refuses
// outright while it's still mounted (unmount first, the same
// "disconnect before removing" shape disconnectConnectionRow/
// removeConnectionHistoryRow already enforce for an active SFTP
// connection) rather than leaving an orphaned live mount nothing in
// breakthrough's own UI can reach anymore.
func (r *Root) removeCloudMount(entry rclone.MountEntry) {
	mounted, err := rclone.IsMounted(entry.Target)
	if err != nil {
		r.showError(err)
		return
	}
	if mounted {
		r.showError(fmt.Errorf("unmount %q before removing it", entry.RemoteName))
		return
	}
	if err := rclone.RemoveMountEntry(entry.Target); err != nil {
		r.showError(err)
		return
	}
	r.renderCloudMountMenu()
}
