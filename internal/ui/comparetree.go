package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/compare"
	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/replace"
)

const compareTreePage = "compare-tree"

// compareTreeHintEntries is this screen's own bottom hint bar (see
// buildListHint) — a function, not a var, per activityLogHintEntries'
// own doc comment.
func compareTreeHintEntries() []listHintEntry {
	return []listHintEntry{
		hintKey("Enter", "diff", func(r *Root) {
			row, _ := r.compare.TreeTable.GetSelection()
			r.activateCompareTreeRow(row)
		}),
		hintKey("c", "copy one-sided item across", func(r *Root) {
			row, _ := r.compare.TreeTable.GetSelection()
			r.copyCompareTreeRow(row)
		}),
		hintKey("m", "toggle mode", func(r *Root) { r.toggleCompareTreeMode() }),
		hintKey("i", "show/hide identical", func(r *Root) { r.toggleCompareTreeShowIdentical() }),
		hintKey("Esc", "close", func(r *Root) { r.closeCompareTree() }),
	}
}

// newCompareTreeScreen builds the directory-vs-directory full screen: a
// table of every compared/one-sided path (see renderCompareTree), a
// status line tallying Walk's own Stats, and a button row — the same
// list-plus-status-plus-buttons shape Batch Rename's own preview
// already establishes (batchrename.go), reused deliberately rather
// than invented fresh.
func (r *Root) newCompareTreeScreen() {
	r.compare.TreeTable = tview.NewTable()
	r.compare.TreeTable.SetBorders(false)
	r.compare.TreeTable.SetBorderPadding(1, 0, 2, 1)
	r.compare.TreeTable.SetSelectable(true, false)
	r.compare.TreeTable.SetFixed(1, 0)
	r.compare.TreeTable.SetInputCapture(r.captureCompareTreeKey)

	r.compare.TreeStatus = tview.NewTextView()
	r.compare.TreeStatus.SetWrap(false)
	r.compare.TreeStatus.SetDynamicColors(true)

	r.compare.TreeButtons = r.newCompareTreeButtons()

	r.compare.TreeTitleBar = newPlainTitleBar("Compare directories")
	r.compare.TreeHint = tview.NewTextView()
	r.compare.TreeHint.SetWrap(false)
	r.compare.TreeHint.SetDynamicColors(true)
	compareTreeHintText, hintSpans := buildListHint(r.theme, compareTreeHintEntries())
	r.compare.TreeHint.SetText(compareTreeHintText)
	r.compare.TreeHintSpans = hintSpans
	r.compare.TreeHint.SetMouseCapture(r.captureListHintMouse(r.compare.TreeHint, &r.compare.TreeHintSpans))

	r.compare.TreeScrollbar = newVerticalScrollbarForTable(r, r.compare.TreeTable)
	tableRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.compare.TreeTable, 0, 1, true).
		AddItem(r.compare.TreeScrollbar, 1, 0, false)
	tableRow.SetMouseCapture(r.compare.TreeScrollbar.captureMouse)

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tableRow, 0, 1, true).
		AddItem(r.compare.TreeStatus, 1, 0, false).
		AddItem(r.compare.TreeButtons, 1, 0, false)

	r.compare.TreeLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.compare.TreeTitleBar, 1, 0, false).
		AddItem(body, 0, 1, true).
		AddItem(r.compare.TreeHint, 1, 0, false)
}

// newCompareTreeButtons builds the tree screen's own button row: toggle
// between the quick size+time heuristic and a real hash (re-walks —
// see toggleCompareTreeMode), show/hide the identical rows (re-renders
// only — no need to re-walk, Walk already reported them), Close.
func (r *Root) newCompareTreeButtons() *tview.Flex {
	type buttonSpec struct {
		button **tview.Button
		label  string
		action func()
	}
	specs := []buttonSpec{
		{&r.compare.TreeModeBtn, "", r.toggleCompareTreeMode},
		{&r.compare.TreeShowSameBtn, "", r.toggleCompareTreeShowIdentical},
		{&r.compare.TreeCloseBtn, "Close", r.closeCompareTree},
	}
	row := tview.NewFlex().SetDirection(tview.FlexColumn)
	for _, spec := range specs {
		b := tview.NewButton(spec.label)
		b.SetSelectedFunc(spec.action)
		b.SetInputCapture(chainKeyCaptures(r.captureCompareTreeKey, spaceAlsoActivates(spec.action)))
		*spec.button = b
		row.AddItem(b, 0, 1, false)
	}
	return row
}

func (r *Root) compareTreeButtonList() []*tview.Button {
	return []*tview.Button{r.compare.TreeModeBtn, r.compare.TreeShowSameBtn, r.compare.TreeCloseBtn}
}

// captureCompareTreeKey is the screen's own shared key handling,
// installed on the table and every button: Tab/Shift+Tab cycle focus
// between the table and the buttons, Escape closes, plus the screen's
// own single-letter actions (m/i/c), always available regardless of
// which widget currently has focus — the same centralize-on-every-
// widget shape captureBatchRenameKey already establishes.
func (r *Root) captureCompareTreeKey(event *tcell.EventKey) *tcell.EventKey {
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
	switch event.Key() {
	case tcell.KeyTab:
		r.cycleCompareTreeFocus(1)
		return nil
	case tcell.KeyBacktab:
		r.cycleCompareTreeFocus(-1)
		return nil
	case tcell.KeyEscape:
		r.closeCompareTree()
		return nil
	case tcell.KeyEnter:
		if r.compare.TreeTable.HasFocus() {
			row, _ := r.compare.TreeTable.GetSelection()
			r.activateCompareTreeRow(row)
			return nil
		}
	case tcell.KeyRune:
		switch event.Rune() {
		case 'm':
			r.toggleCompareTreeMode()
			return nil
		case 'i':
			r.toggleCompareTreeShowIdentical()
			return nil
		case 'c':
			row, _ := r.compare.TreeTable.GetSelection()
			r.copyCompareTreeRow(row)
			return nil
		case 'g':
			r.startConsigliereChord(nil)
			return nil
		}
	}
	return event
}

func (r *Root) cycleCompareTreeFocus(delta int) {
	ring := append([]tview.Primitive{r.compare.TreeTable}, compareTreeButtonPrimitives(r)...)
	for i, p := range ring {
		if !p.HasFocus() {
			continue
		}
		next := (i + delta + len(ring)) % len(ring)
		r.app.SetFocus(ring[next])
		return
	}
	r.app.SetFocus(ring[0])
}

func compareTreeButtonPrimitives(r *Root) []tview.Primitive {
	buttons := r.compareTreeButtonList()
	primitives := make([]tview.Primitive, len(buttons))
	for i, b := range buttons {
		primitives[i] = b
	}
	return primitives
}

// openCompareTree starts a fresh walk of dirA against dirB — always
// from ModeQuick and with identical rows hidden, the same "start from a
// clean slate every open" rule openBatchRename already follows, so a
// stale mode/filter from a previous comparison never silently carries
// over to an unrelated pair.
func (r *Root) openCompareTree(dirA, dirB string) {
	r.cancelCompareTreeWalk()
	r.compare.TreeA, r.compare.TreeB = dirA, dirB
	r.compare.TreeMode = compare.ModeQuick
	r.compare.TreeShowIdentical = false
	r.compare.TreeEntries = nil
	r.compare.TreeStats = compare.Stats{}

	r.showOverlayWithRestore(compareTreePage, r.compare.TreeLayout, func() { r.app.SetFocus(r.compare.TreeTable) })
	r.runCompareTreeWalk()
}

func (r *Root) closeCompareTree() {
	r.cancelCompareTreeWalk()
	r.hideOverlay()
}

// resetCompareTreeScrollPosition puts the table's own cursor and
// scroll back to row 1 (row 0 is the header) — not wherever they
// happened to be left from a previous comparison, since the table
// widget itself is shared (see newCompareTreeScreen) and neither
// Table.Clear() nor renderCompareTree ever resets either one on its
// own. Called once real results have actually landed (see
// runCompareTreeWalk's own completion callback), not right when the
// screen opens (openCompareTree): the "Comparing…" placeholder shown
// until then is a single row anyway, so there's nothing yet to have
// scrolled away from — and not from a mere re-render like toggling
// show-identical either, which shouldn't yank the view out from under
// someone reviewing a specific row over data that hasn't actually
// changed. Per the user's own explicit request that a fresh comparison
// always start showing its own top — the same reset Batch Rename's own
// preview already does on open (see its own Select(1, 0) in
// openBatchRename).
func (r *Root) resetCompareTreeScrollPosition() {
	r.compare.TreeTable.Select(1, 0)
	r.compare.TreeTable.ScrollToBeginning()
}

// runCompareTreeWalk runs compare.Walk on a background goroutine —
// the same context.WithCancel + safeGo + QueueUpdateDraw shape
// computeHashes (properties.go) and computeCompareHash (compare.go)
// already establish, just over a whole tree instead of one or two
// files. hash wraps the shared hashFile var (see properties.go) down
// to just its SHA256 field, since that's the one digest Walk's own
// ModeHash needs — no reason to also compute the other four for every
// file in a possibly enormous tree.
func (r *Root) runCompareTreeWalk() {
	ctx, cancel := context.WithCancel(context.Background())
	r.compare.TreeCancel = cancel
	r.compare.TreeRunning = true
	r.compare.TreeAnim = 0
	r.renderCompareTree()

	dirA, dirB, mode := r.compare.TreeA, r.compare.TreeB, r.compare.TreeMode
	hash := func(ctx context.Context, path string) (string, error) {
		h, err := hashFile(ctx, path, nil)
		return h.SHA256, err
	}
	var progress int64 // atomic-free: only ever touched from the walk goroutine and read via QueueUpdateDraw below, same single-writer contract hashBytesRead documents
	onProgress := func(count int) { progress = int64(count) }

	onPanic := func() {
		r.cancelCompareTreeWalk()
		r.renderCompareTree()
	}
	r.safeGo("Compare tree progress animation", onPanic, func() { r.animateCompareTree(ctx, &progress) })
	r.safeGo("Compare tree walk", onPanic, func() {
		entries, stats, err := compare.Walk(ctx, dirA, dirB, mode, hash, onProgress)
		if ctx.Err() != nil {
			return
		}
		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			r.cancelCompareTreeWalk()
			r.compare.TreeEntries, r.compare.TreeStats = entries, stats
			if err != nil {
				r.showError(fmt.Errorf("compare: %w", err))
			}
			r.renderCompareTree()
			r.resetCompareTreeScrollPosition()
		})
	})
}

// animateCompareTree advances compare.TreeAnim and re-renders the status
// line with progress's own live count until ctx is done — mirrors
// animateHashProgress/animateCompareHash for the same reason.
func (r *Root) animateCompareTree(ctx context.Context, progress *int64) {
	ticker := time.NewTicker(hashAnimationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			r.app.QueueUpdateDraw(func() {
				if ctx.Err() != nil {
					return
				}
				r.compare.TreeAnim++
				r.compare.TreeStatus.SetText(compareTreeProgressText(r.compare.TreeAnim, *progress))
			})
		case <-ctx.Done():
			return
		}
	}
}

func compareTreeProgressText(animFrame int, count int64) string {
	return fmt.Sprintf(" %s comparing… (%d so far)", hashAnimationFrames[animFrame%len(hashAnimationFrames)], count)
}

// cancelCompareTreeWalk stops whatever runCompareTreeWalk call is in
// flight, if any — mirrors cancelHashComputation/
// cancelCompareHashComputation.
func (r *Root) cancelCompareTreeWalk() {
	if r.compare.TreeCancel != nil {
		r.compare.TreeCancel()
		r.compare.TreeCancel = nil
	}
	r.compare.TreeRunning = false
}

// toggleCompareTreeMode switches between ModeQuick and ModeHash and
// re-walks from scratch: unlike toggleCompareTreeShowIdentical, this
// genuinely changes what Walk itself would report (a ModeQuick
// Uncertain row might resolve either way under ModeHash), so the old
// Entries can't just be re-filtered — see runCompareTreeWalk.
func (r *Root) toggleCompareTreeMode() {
	if r.compare.TreeRunning {
		return
	}
	if r.compare.TreeMode == compare.ModeQuick {
		r.compare.TreeMode = compare.ModeHash
	} else {
		r.compare.TreeMode = compare.ModeQuick
	}
	r.runCompareTreeWalk()
}

// toggleCompareTreeShowIdentical just re-renders from the Entries Walk
// already reported — no reason to touch the filesystem again for a
// display-only filter.
func (r *Root) toggleCompareTreeShowIdentical() {
	r.compare.TreeShowIdentical = !r.compare.TreeShowIdentical
	r.renderCompareTree()
}

// renderCompareTree fills the table from compare.TreeEntries (hiding
// Identical rows unless compare.TreeShowIdentical is on — everything
// else always shows, the same "an unresolved question stays visible"
// reasoning Uncertain rows exist for at all), the status line's own
// tally, and both toggle buttons' current-state labels.
func (r *Root) renderCompareTree() {
	r.compare.TreeModeBtn.SetLabel(compareTreeModeLabel(r.compare.TreeMode))
	r.compare.TreeShowSameBtn.SetLabel(compareTreeShowIdenticalLabel(r.compare.TreeShowIdentical))

	r.compare.TreeTable.Clear()
	header := func(col int, text string) {
		r.compare.TreeTable.SetCell(0, col, tview.NewTableCell(text).
			SetTextColor(r.theme.Text).SetAttributes(tcell.AttrBold).SetSelectable(false))
	}
	header(0, "")
	header(1, "Path")
	header(2, "A")
	header(3, "B")

	if r.compare.TreeRunning {
		r.compare.TreeTable.SetCell(1, 0, tview.NewTableCell("Comparing…").SetTextColor(r.theme.PlaceholderText).SetSelectable(false))
		return
	}

	row := 0
	for _, e := range r.compare.TreeEntries {
		if e.Verdict == compare.Identical && !r.compare.TreeShowIdentical {
			continue
		}
		row++
		mark, color := compareTreeRowMark(r.theme, e)
		r.compare.TreeTable.SetCell(row, 0, tview.NewTableCell(mark).SetTextColor(color).SetReference(e))
		r.compare.TreeTable.SetCell(row, 1, tview.NewTableCell(e.RelPath).SetTextColor(color))
		r.compare.TreeTable.SetCell(row, 2, tview.NewTableCell(compareTreeSideCell(e, true, r.settings.MtimeUnix)).SetTextColor(color))
		r.compare.TreeTable.SetCell(row, 3, tview.NewTableCell(compareTreeSideCell(e, false, r.settings.MtimeUnix)).SetTextColor(color))
	}
	if row == 0 {
		note := "Nothing to show — everything compared identical."
		if len(r.compare.TreeEntries) == 0 {
			note = "The two directories are identical."
		}
		r.compare.TreeTable.SetCell(1, 0, tview.NewTableCell(note).SetTextColor(r.theme.PlaceholderText).SetSelectable(false))
	}

	r.compare.TreeStatus.SetText(compareTreeStatusText(r.compare.TreeStats))
}

func compareTreeModeLabel(mode compare.Mode) string {
	if mode == compare.ModeHash {
		return "Mode: Hash (exact)"
	}
	return "Mode: Quick (size+time)"
}

func compareTreeShowIdenticalLabel(show bool) string {
	if show {
		return "Hide identical"
	}
	return "Show identical"
}

// compareTreeRowMark is one row's leading glyph and color — the same
// traffic-light convention Options/Batch Rename's own checkboxes and
// active-marks already use, just with more than two states.
func compareTreeRowMark(theme config.ResolvedTheme, e compare.Entry) (string, tcell.Color) {
	switch e.Verdict {
	case compare.Identical:
		return "=", theme.PlaceholderText
	case compare.Uncertain:
		return "?", theme.WarningText
	case compare.OnlyInA:
		return "< A", theme.WarningText
	case compare.OnlyInB:
		return "> B", theme.WarningText
	case compare.Errored:
		return "!", theme.EntryError
	default: // Differs
		return "≠", theme.EntryError
	}
}

// compareTreeSideCell is one side's own cell text for a row: size and
// modification time for a side that exists, "—" for a side that
// doesn't (a one-sided entry, or the error message for an Errored one).
func compareTreeSideCell(e compare.Entry, sideA bool, unixMode bool) string {
	if e.Verdict == compare.Errored {
		if e.Err != nil {
			return e.Err.Error()
		}
		return "error"
	}
	if e.Verdict == compare.OnlyInA && !sideA {
		return "—"
	}
	if e.Verdict == compare.OnlyInB && sideA {
		return "—"
	}
	size, when := e.SizeA, e.ModTimeA
	if !sideA {
		size, when = e.SizeB, e.ModTimeB
	}
	if e.Note != "" {
		return e.Note
	}
	return fmt.Sprintf("%s  %s", humanSize(size), formatModTimeCell(when, unixMode))
}

func compareTreeStatusText(s compare.Stats) string {
	return fmt.Sprintf(" %d differ · %d only in A · %d only in B · %d uncertain · %d identical · %d error(s)",
		s.Differs, s.OnlyInA, s.OnlyInB, s.Uncertain, s.Identical, s.Errored)
}

// compareTreeEntryAt is the Entry shown on row of the table (stored on
// its own leading-glyph cell by renderCompareTree), or ok=false for the
// header/a placeholder row.
func (r *Root) compareTreeEntryAt(row int) (compare.Entry, bool) {
	cell := r.compare.TreeTable.GetCell(row, 0)
	if cell == nil {
		return compare.Entry{}, false
	}
	e, ok := cell.GetReference().(compare.Entry)
	return e, ok
}

// activateCompareTreeRow is Enter on a table row: a text pair that
// actually differs opens the side-by-side diff view (see
// openCompareDiff, reused here against the two real on-disk paths this
// row names — nothing tree-specific about it); anything else —
// identical, a directory, a one-sided path, a type mismatch, a binary
// pair — has no diff to show.
func (r *Root) activateCompareTreeRow(row int) {
	e, ok := r.compareTreeEntryAt(row)
	if !ok || e.Verdict == compare.OnlyInA || e.Verdict == compare.OnlyInB || e.Verdict == compare.Identical || e.Note != "" {
		return
	}
	pathA := filepath.Join(r.compare.TreeA, filepath.FromSlash(e.RelPath))
	pathB := filepath.Join(r.compare.TreeB, filepath.FromSlash(e.RelPath))
	if binA, _ := replace.LooksBinaryFile(pathA); binA {
		return
	}
	if binB, _ := replace.LooksBinaryFile(pathB); binB {
		return
	}
	r.compare.A, r.compare.B = pathA, pathB
	r.openCompareDiff()
}

// copyCompareTreeRow is "c" on a one-sided row: copies that file or
// directory across to where its counterpart would be on the other
// side, the same plain fsops.Copy (no special options — a one-sided
// destination is by definition not already occupied) every other copy
// path in this app already uses. Re-walks afterward rather than just
// patching the one row: a directory copy can turn one OnlyInA row into
// any number of new Identical ones underneath it, which only a fresh
// Walk can discover.
func (r *Root) copyCompareTreeRow(row int) {
	e, ok := r.compareTreeEntryAt(row)
	if !ok || (e.Verdict != compare.OnlyInA && e.Verdict != compare.OnlyInB) {
		return
	}
	var src, dst string
	if e.Verdict == compare.OnlyInA {
		src = filepath.Join(r.compare.TreeA, filepath.FromSlash(e.RelPath))
		dst = filepath.Join(r.compare.TreeB, filepath.FromSlash(e.RelPath))
	} else {
		src = filepath.Join(r.compare.TreeB, filepath.FromSlash(e.RelPath))
		dst = filepath.Join(r.compare.TreeA, filepath.FromSlash(e.RelPath))
	}
	r.openConfirm(
		fmt.Sprintf("Copy %s to the other side?", e.RelPath),
		"Yes, copy",
		func() {
			if err := fsops.Copy(src, dst, fsops.CopyOptions{}); err != nil {
				r.showError(fmt.Errorf("compare: %w", err))
				return
			}
			r.reloadPanel(nil)
			r.runCompareTreeWalk()
		},
	)
}
