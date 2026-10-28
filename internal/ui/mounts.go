package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/mounts"
)

// The Mounts screen: a full-screen, read-only view of every currently
// mounted filesystem — a third full-screen catalog alongside Options
// and the Toolbox, per the user's own explicit request to keep it
// separate rather than folding it into the Toolbox's own command list,
// since this one shows live, structured data rather than a catalog of
// commands to run.
//
// The actual findmnt shelling-out, JSON parsing and fstab
// cross-referencing live in internal/mounts (see its own doc comment);
// this file only renders whatever []mounts.Entry it returns.

const mountsPage = "mounts"

// Column indices within mountsTable. Bind/Persistent/Type — this
// screen's own short, fixed-width, at-a-glance columns — deliberately
// come right after Target, before Source and Options: a live check
// against a real system with a mounted CIFS share showed a single long
// network-share path in Source pushing every column after it well past
// a normal terminal's own width, exactly the class of problem
// optionsColInfo's own doc comment already describes for a too-narrow
// row ("the rightmost column is the one that gets clipped"). Source and
// Options — both unbounded (see mountsColumnWidth) and the two least
// essential for a quick scan — are what ends up needing a horizontal
// scroll on a narrow terminal instead, never Bind/Persistent.
const (
	mountsColTarget = iota
	mountsColBind
	mountsColPersistent
	mountsColFstype
	mountsColSource
	mountsColOptions
)

// mountsColumnWidth is each column's own padding target — a floor, not
// a ceiling: padRight (see renderMounts) never truncates a value longer
// than it, only pads a shorter one out to it, so a long value simply
// pushes whatever comes after it rather than being clipped, the same
// reasoning panel.go's own Size/Modified columns already follow.
// Source's own floor is deliberately modest (just enough gap before
// Options for a short local device path like "/dev/md0") rather than a
// real column width, since a CIFS/NFS share's own path routinely runs
// far longer than any fixed width would help with anyway. Options has
// no floor at all: nothing follows it, so there's nothing to keep a gap
// before.
func mountsColumnWidth(col int) int {
	switch col {
	case mountsColTarget:
		return 32
	case mountsColBind:
		return 6
	case mountsColPersistent:
		return 12
	case mountsColFstype:
		return 8
	case mountsColSource:
		return 20
	default:
		return 0
	}
}

// mountsHintEntries is this screen's own bottom hint bar (see
// buildListHint) — a function, not a var, per activityLogHintEntries'
// own doc comment.
func mountsHintEntries() []listHintEntry {
	return []listHintEntry{
		{keys: []listHintKey{
			{"↑", simulateKeyOnFocused(tcell.KeyUp)},
			{"↓", simulateKeyOnFocused(tcell.KeyDown)},
		}, label: "move"},
		hintKey("r", "refresh", func(r *Root) { r.reloadMounts() }),
		hintKey("Esc", "close", func(r *Root) { r.closeMounts() }),
	}
}

// newMountsScreen builds the whole screen once, at startup — the same
// build-once/repopulate-on-open shape newToolboxScreen already
// establishes.
func (r *Root) newMountsScreen() {
	r.mountsTable = tview.NewTable()
	r.mountsTable.SetBorders(false)
	r.mountsTable.SetBorderPadding(1, 0, 2, 1)
	r.mountsTable.SetSelectable(true, false)
	// The header row (see renderMounts) never scrolls out of view, the
	// same fixed-header shape renderCompareTree already establishes for
	// its own table.
	r.mountsTable.SetFixed(1, 0)
	r.mountsTable.SetInputCapture(r.captureMountsKey)

	r.mountsTitleBar = newPlainTitleBar("Mounts")
	r.mountsTitleBar.SetMouseCapture(captureReloadCloseTitleBarMouse(r.mountsTitleBar, r.reloadMounts, r.closeMounts))

	r.mountsHint = tview.NewTextView()
	r.mountsHint.SetWrap(false)
	r.mountsHint.SetDynamicColors(true)
	mountsHintText, mountsHintSpans := buildListHint(r.theme, mountsHintEntries())
	r.mountsHint.SetText(mountsHintText)
	r.mountsHintSpans = mountsHintSpans
	r.mountsHint.SetMouseCapture(r.captureListHintMouse(r.mountsHint, &r.mountsHintSpans))

	r.mountsScrollbar = newVerticalScrollbarForTable(r, r.mountsTable)
	mountsTableRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.mountsTable, 0, 1, true).
		AddItem(r.mountsScrollbar, 1, 0, false)
	mountsTableRow.SetMouseCapture(r.mountsScrollbar.captureMouse)

	r.mountsLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.mountsTitleBar, 1, 0, false).
		AddItem(mountsTableRow, 0, 1, true).
		AddItem(r.mountsHint, 1, 0, false)
}

// openMounts shows the Mounts screen, freshly read every time — there
// is nothing here worth remembering across opens, since the whole point
// is to reflect whatever is mounted right now.
func (r *Root) openMounts() {
	r.reloadMounts()
	r.showOverlay(mountsPage, r.mountsLayout)
}

// closeMounts hides the Mounts screen. Nothing to save — this is a pure
// read-only view.
func (r *Root) closeMounts() {
	r.hideOverlay()
}

// reloadMounts re-reads the live mount table and re-renders — the
// screen's own initial population, and "r"'s own manual refresh, since
// a mount can appear or disappear (a USB stick, a network share) while
// this screen is open and nothing here otherwise notices on its own.
func (r *Root) reloadMounts() {
	entries, err := mounts.Read()
	r.mountsEntries = entries
	r.mountsErr = err
	r.renderMounts()
}

// renderMounts fills the table: a bold header row (see newMountsScreen's
// own SetFixed), then one row per currently mounted filesystem. A
// failed read (findmnt missing, or erroring outright) shows in place of
// any rows rather than silently leaving the screen empty — the same
// "report it, don't swallow it" principle every other action in this
// app already follows.
func (r *Root) renderMounts() {
	r.mountsTable.Clear()

	renderReloadCloseTitleBar(r.mountsTitleBar, " Mounts ", r.lastScreenWidth)

	header := func(col int, text string) {
		r.mountsTable.SetCell(0, col,
			tview.NewTableCell(padRight(text, mountsColumnWidth(col))).
				SetTextColor(r.theme.Text).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false))
	}
	header(mountsColTarget, "Target")
	header(mountsColBind, "Bind")
	header(mountsColPersistent, "Persistent")
	header(mountsColFstype, "Type")
	header(mountsColSource, "Source")
	header(mountsColOptions, "Options")

	// See showTablePlaceholder's own doc comment for why this — and the
	// zero-entries case below — go through it rather than a bare
	// SetCell: it's the fix for a real, reported freeze, not just a
	// message.
	if r.mountsErr != nil {
		showTablePlaceholder(r.mountsTable, r.mountsErr.Error(), r.theme.EntryError)
		return
	}

	// Real, selectable rows may exist below — reset again just below if
	// this directory turns out to have none.
	enableTableSelection(r.mountsTable)

	for i, m := range r.mountsEntries {
		row := i + 1

		r.mountsTable.SetCell(row, mountsColTarget,
			tview.NewTableCell(padRight(m.Target, mountsColumnWidth(mountsColTarget))).
				SetTextColor(r.theme.Text).SetSelectable(true))
		// statusGlyphText, not checkboxText — bind/persistent are plain
		// facts about the mount this screen can't change, not a
		// clickable setting, so they get the same ✔/✘ shape Sessions'
		// own Status column uses for the identical reason instead of
		// the ●/○ pair reserved for an actual checkbox. Color
		// unchanged (PlaceholderText, same as Type/Source/Options) —
		// see statusGlyphText's own doc comment on why this stays
		// plain rather than green/red.
		r.mountsTable.SetCell(row, mountsColBind,
			tview.NewTableCell(padRight(statusGlyphText(m.Bind), mountsColumnWidth(mountsColBind))).
				SetTextColor(r.theme.PlaceholderText).SetSelectable(true))
		r.mountsTable.SetCell(row, mountsColPersistent,
			tview.NewTableCell(padRight(statusGlyphText(m.Persistent), mountsColumnWidth(mountsColPersistent))).
				SetTextColor(r.theme.PlaceholderText).SetSelectable(true))
		r.mountsTable.SetCell(row, mountsColFstype,
			tview.NewTableCell(padRight(m.Fstype, mountsColumnWidth(mountsColFstype))).
				SetTextColor(r.theme.PlaceholderText).SetSelectable(true))
		r.mountsTable.SetCell(row, mountsColSource,
			tview.NewTableCell(padRight(m.Source, mountsColumnWidth(mountsColSource))).
				SetTextColor(r.theme.Text).SetSelectable(true))
		r.mountsTable.SetCell(row, mountsColOptions,
			tview.NewTableCell(m.Options).
				SetTextColor(r.theme.PlaceholderText).SetSelectable(true))
	}

	// Keep the cursor in range (and off the header row) after a refresh
	// changed how many rows there are — landing on the first real row
	// the very first time this ever renders, left alone otherwise, the
	// same restraint renderToolbox's own tail already shows. The
	// zero-entries case (no real storage mounted at all — vanishingly
	// unlikely in practice, but not impossible) is just as vulnerable to
	// the freeze showTablePlaceholder's own doc comment describes as the
	// error branch above, so it goes through the same helper — and,
	// same as renderFirewall's "No rules configured.", reports the
	// empty read explicitly instead of just looking like one.
	if len(r.mountsEntries) == 0 {
		showTablePlaceholder(r.mountsTable, "No real storage mounted.", r.theme.PlaceholderText)
		return
	}
	if row, _ := r.mountsTable.GetSelection(); row < 1 || row > len(r.mountsEntries) {
		r.mountsTable.Select(1, 0)
	}
}

// captureMountsKey is the Mounts screen's own key handling: Escape
// closes it, "r" re-reads the live mount table.
func (r *Root) captureMountsKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeMounts()
		return nil
	}
	// Tab/Backtab reach the AI Consigliere sidebar — see
	// startConsigliereChord's own doc comment in consigliere.go and
	// captureLogAuditSelectionTableKey's own identical shape.
	// mountsTable is this screen's only real focus target.
	if r.consigliereVisible {
		if event.Key() == tcell.KeyTab {
			r.app.SetFocus(r.consigliereInput)
			return nil
		}
		if event.Key() == tcell.KeyBacktab {
			if r.consigliereScrollbar.maxOffset() > 0 {
				r.app.SetFocus(r.consigliereHistory)
			} else {
				r.app.SetFocus(r.consigliereInput)
			}
			return nil
		}
	}
	if event.Key() == tcell.KeyRune && event.Rune() == 'r' {
		r.reloadMounts()
		return nil
	}
	// "gC" reaches the AI Consigliere from here too — see
	// startConsigliereChord's own doc comment in consigliere.go.
	if event.Key() == tcell.KeyRune && event.Rune() == 'g' {
		r.startConsigliereChord(nil)
		return nil
	}
	return event
}

// applyMountsTheme themes the Mounts screen — split out of applyTheme
// (see its own doc comment). Guarded the same way applyOptionsTheme is.
func (r *Root) applyMountsTheme(theme config.ResolvedTheme) {
	if r.mountsTable == nil {
		return
	}
	// PanelBackground/SurfaceBackground, not SurfaceBackground/
	// InputBackground: Mounts is a full-bleed, always-modal catalog that
	// never needs to read as "floating over" anything — nothing is ever
	// visible behind it to float over — so it adopts the exact same
	// two-layer structure the main screen itself already uses (body
	// darker than its own bottom bar — see buttonBar/statusBar in
	// applyTheme) instead of a shade meant for genuinely floating
	// chrome, per the user's own explicit request to bring every
	// full-screen catalog in line with the main screen it fully covers.
	r.mountsLayout.SetBackgroundColor(theme.PanelBackground)
	r.mountsTable.SetBackgroundColor(theme.PanelBackground)
	// Without this, tview's own default selected-cell style just
	// reverses each cell's own foreground into a background instead of
	// using one consistent selection bar — the same fix already applied
	// to sshKeysTable/connectionMenuTable/sedPreviewTable/tabSwitcher/
	// gitBreach's tables, for the identical reason.
	r.mountsTable.SetSelectedStyle(tcell.StyleDefault.
		Background(theme.SelectionBackground).
		Foreground(theme.TextColor))

	// FocusedBackground, fixed — same reasoning toolboxTitleBar's own
	// fixed FocusedBackground just above follows.
	r.mountsTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.mountsTitleBar.SetTextColor(theme.TextColor)
	// SurfaceBackground/TextColor, matching the main screen's own
	// buttonBar/statusBar exactly — see mountsLayout's own comment above.
	r.mountsHint.SetBackgroundColor(theme.SurfaceBackground)
	r.mountsHint.SetTextColor(theme.TextColor)
	mountsHintText, mountsHintSpans := buildListHint(theme, mountsHintEntries())
	r.mountsHint.SetText(mountsHintText)
	r.mountsHintSpans = mountsHintSpans

	r.renderMounts() // cell colors are baked in per cell, not looked up live at draw time
}
