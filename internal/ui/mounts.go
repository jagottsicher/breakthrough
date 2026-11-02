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

// mountsState holds the Mounts screen's widgets and last read result.
type mountsState struct {
	Layout    *tview.Flex
	TitleBar  *tview.TextView
	Hint      *tview.TextView
	HintSpans []listHintSpan
	Table     *tview.Table
	Scrollbar *verticalScrollbar
	Entries   []mounts.Entry
	Err       error
}

// Column indices within mounts.Table. Bind/Persistent/Type — this
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
	r.mounts.Table = tview.NewTable()
	r.mounts.Table.SetBorders(false)
	r.mounts.Table.SetBorderPadding(1, 0, 2, 1)
	r.mounts.Table.SetSelectable(true, false)
	// The header row (see renderMounts) never scrolls out of view, the
	// same fixed-header shape renderCompareTree already establishes for
	// its own table.
	r.mounts.Table.SetFixed(1, 0)
	r.mounts.Table.SetInputCapture(r.captureMountsKey)

	r.mounts.TitleBar = newPlainTitleBar("Mounts")
	r.mounts.TitleBar.SetMouseCapture(captureReloadCloseTitleBarMouse(r.mounts.TitleBar, r.reloadMounts, r.closeMounts))

	r.mounts.Hint = tview.NewTextView()
	r.mounts.Hint.SetWrap(false)
	r.mounts.Hint.SetDynamicColors(true)
	mountsHintText, hintSpans := buildListHint(r.theme, mountsHintEntries())
	r.mounts.Hint.SetText(mountsHintText)
	r.mounts.HintSpans = hintSpans
	r.mounts.Hint.SetMouseCapture(r.captureListHintMouse(r.mounts.Hint, &r.mounts.HintSpans))

	r.mounts.Scrollbar = newVerticalScrollbarForTable(r, r.mounts.Table)
	mountsTableRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.mounts.Table, 0, 1, true).
		AddItem(r.mounts.Scrollbar, 1, 0, false)
	mountsTableRow.SetMouseCapture(r.mounts.Scrollbar.captureMouse)

	r.mounts.Layout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.mounts.TitleBar, 1, 0, false).
		AddItem(mountsTableRow, 0, 1, true).
		AddItem(r.mounts.Hint, 1, 0, false)
}

// openMounts shows the Mounts screen, freshly read every time — there
// is nothing here worth remembering across opens, since the whole point
// is to reflect whatever is mounted right now.
func (r *Root) openMounts() {
	r.reloadMounts()
	r.showOverlay(mountsPage, r.mounts.Layout)
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
	r.mounts.Entries = entries
	r.mounts.Err = err
	r.renderMounts()
}

// renderMounts fills the table: a bold header row (see newMountsScreen's
// own SetFixed), then one row per currently mounted filesystem. A
// failed read (findmnt missing, or erroring outright) shows in place of
// any rows rather than silently leaving the screen empty — the same
// "report it, don't swallow it" principle every other action in this
// app already follows.
func (r *Root) renderMounts() {
	r.mounts.Table.Clear()

	renderReloadCloseTitleBar(r.mounts.TitleBar, " Mounts ", r.lastScreenWidth)

	header := func(col int, text string) {
		r.mounts.Table.SetCell(0, col,
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
	if r.mounts.Err != nil {
		showTablePlaceholder(r.mounts.Table, r.mounts.Err.Error(), r.theme.EntryError)
		return
	}

	// Real, selectable rows may exist below — reset again just below if
	// this directory turns out to have none.
	enableTableSelection(r.mounts.Table)

	for i, m := range r.mounts.Entries {
		row := i + 1

		r.mounts.Table.SetCell(row, mountsColTarget,
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
		r.mounts.Table.SetCell(row, mountsColBind,
			tview.NewTableCell(padRight(statusGlyphText(m.Bind), mountsColumnWidth(mountsColBind))).
				SetTextColor(r.theme.PlaceholderText).SetSelectable(true))
		r.mounts.Table.SetCell(row, mountsColPersistent,
			tview.NewTableCell(padRight(statusGlyphText(m.Persistent), mountsColumnWidth(mountsColPersistent))).
				SetTextColor(r.theme.PlaceholderText).SetSelectable(true))
		r.mounts.Table.SetCell(row, mountsColFstype,
			tview.NewTableCell(padRight(m.Fstype, mountsColumnWidth(mountsColFstype))).
				SetTextColor(r.theme.PlaceholderText).SetSelectable(true))
		r.mounts.Table.SetCell(row, mountsColSource,
			tview.NewTableCell(padRight(m.Source, mountsColumnWidth(mountsColSource))).
				SetTextColor(r.theme.Text).SetSelectable(true))
		r.mounts.Table.SetCell(row, mountsColOptions,
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
	if len(r.mounts.Entries) == 0 {
		showTablePlaceholder(r.mounts.Table, "No real storage mounted.", r.theme.PlaceholderText)
		return
	}
	if row, _ := r.mounts.Table.GetSelection(); row < 1 || row > len(r.mounts.Entries) {
		r.mounts.Table.Select(1, 0)
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
	// mounts.Table is this screen's only real focus target.
	if r.consigliere.Visible {
		if event.Key() == tcell.KeyTab {
			r.app.SetFocus(r.consigliere.Input)
			return nil
		}
		if event.Key() == tcell.KeyBacktab {
			if r.consigliere.Scrollbar.maxOffset() > 0 {
				r.app.SetFocus(r.consigliere.History)
			} else {
				r.app.SetFocus(r.consigliere.Input)
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
	if r.mounts.Table == nil {
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
	r.mounts.Layout.SetBackgroundColor(theme.PanelBackground)
	r.mounts.Table.SetBackgroundColor(theme.PanelBackground)
	// Without this, tview's own default selected-cell style just
	// reverses each cell's own foreground into a background instead of
	// using one consistent selection bar — the same fix already applied
	// to ssh.KeysTable/connectionMenuTable/sed.PreviewTable/tabSwitcher/
	// gitBreach's tables, for the identical reason.
	r.mounts.Table.SetSelectedStyle(tcell.StyleDefault.
		Background(theme.SelectionBackground).
		Foreground(theme.TextColor))

	// FocusedBackground, fixed — same reasoning toolbox.TitleBar's own
	// fixed FocusedBackground just above follows.
	r.mounts.TitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.mounts.TitleBar.SetTextColor(theme.TextColor)
	// SurfaceBackground/TextColor, matching the main screen's own
	// buttonBar/statusBar exactly — see mounts.Layout's own comment above.
	r.mounts.Hint.SetBackgroundColor(theme.SurfaceBackground)
	r.mounts.Hint.SetTextColor(theme.TextColor)
	mountsHintText, hintSpans := buildListHint(theme, mountsHintEntries())
	r.mounts.Hint.SetText(mountsHintText)
	r.mounts.HintSpans = hintSpans

	r.renderMounts() // cell colors are baked in per cell, not looked up live at draw time
}
