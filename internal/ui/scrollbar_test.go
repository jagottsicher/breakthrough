package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/mounts"
)

// openLookWithLines writes a file with n short, fixed-width lines (well
// under width, so wrapping never kicks in and GetWrappedLineCount ==
// n), opens it through the real showBuiltinLook path, sizes the Look
// overlay to width×height, and draws it once on a SimulationScreen —
// the one real draw GetWrappedLineCount's own doc comment says is
// required before it reflects actual wrapping (see
// textViewContent.totalRows's own cache doc comment). Returns the
// scrollbar under test and the viewport row count it ends up with.
func openLookWithLines(t *testing.T, n, width, height int) (*verticalScrollbar, int) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	var sb strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&sb, "line %03d\n", i)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, width, height)

	if !r.showBuiltinLook(path) {
		t.Fatal("showBuiltinLook returned false")
	}

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(width, height)
	r.viewerLayout.Draw(screen)

	return r.viewerScrollbar, r.viewerScrollbar.viewportRows()
}

// TestVerticalScrollbarThumbFillsTrackWhenContentFits pins the no-scroll
// case: content shorter than the viewport leaves nothing to scroll, so
// the thumb spans the whole track and sits at its very top.
func TestVerticalScrollbarThumbFillsTrackWhenContentFits(t *testing.T) {
	sb, _ := openLookWithLines(t, 5, 60, 20)

	_, _, _, sbh := sb.GetRect()
	trackRows := sbh - 2
	thumbRows, thumbStart := sb.thumbGeometry(trackRows)

	if thumbRows != trackRows {
		t.Errorf("thumbRows = %d, want the full track (%d) since content fits", thumbRows, trackRows)
	}
	if thumbStart != 0 {
		t.Errorf("thumbStart = %d, want 0", thumbStart)
	}
	if got := sb.maxOffset(); got != 0 {
		t.Errorf("maxOffset = %d, want 0 (nothing to scroll)", got)
	}
}

// TestVerticalScrollbarThumbShrinksAndMovesWithContent pins the
// proportional case: a document much longer than the viewport gets a
// thumb well short of the full track, and scrolling to the very end
// moves the thumb to the track's own bottom edge.
func TestVerticalScrollbarThumbShrinksAndMovesWithContent(t *testing.T) {
	sb, viewport := openLookWithLines(t, 200, 60, 20)

	_, _, _, sbh := sb.GetRect()
	trackRows := sbh - 2
	thumbRows, thumbStart := sb.thumbGeometry(trackRows)

	if thumbRows >= trackRows {
		t.Fatalf("thumbRows = %d, want it shorter than the full track (%d) for content this much longer than the viewport", thumbRows, trackRows)
	}
	if thumbStart != 0 {
		t.Errorf("thumbStart = %d, want 0 before any scrolling", thumbStart)
	}

	sb.scrollBy(sb.maxOffset()) // jump to the very end
	_ = viewport
	_, newStart := sb.thumbGeometry(trackRows)
	if newStart != trackRows-thumbRows {
		t.Errorf("thumbStart after scrolling to the end = %d, want the track's own bottom edge (%d)", newStart, trackRows-thumbRows)
	}
}

// TestVerticalScrollbarThumbFloorsAtOneRow pins the user's own explicit
// requirement: however long the document, the thumb never shrinks below
// one character cell.
func TestVerticalScrollbarThumbFloorsAtOneRow(t *testing.T) {
	sb, _ := openLookWithLines(t, 5000, 60, 10)

	_, _, _, sbh := sb.GetRect()
	trackRows := sbh - 2
	thumbRows, _ := sb.thumbGeometry(trackRows)

	if thumbRows != 1 {
		t.Errorf("thumbRows = %d, want 1 for a document this much longer than the track", thumbRows)
	}
}

// TestCaptureViewerScrollbarMouseArrowStepsOneLine pins the up/down
// arrow buttons: one click moves the content by exactly one line.
func TestCaptureViewerScrollbarMouseArrowStepsOneLine(t *testing.T) {
	sb, _ := openLookWithLines(t, 200, 60, 20)
	sbx, sby, _, sbh := sb.GetRect()

	sb.captureMouse(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.Button1, 0))
	sb.stopRepeat() // the click itself already started auto-repeat (see startRepeat) — stop it so the test only observes the single immediate step
	sb.captureMouse(tview.MouseLeftUp, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.ButtonNone, 0))

	if row, _ := sb.content.scrollOffset(); row != 1 {
		t.Errorf("scroll offset after one down-arrow click = %d, want 1", row)
	}
}

// TestCaptureViewerScrollbarMousePageJumpOnTrackClick pins clicking the
// track below the thumb: one click jumps a full viewport height, not
// a single line, and stops at maxOffset rather than overshooting.
func TestCaptureViewerScrollbarMousePageJumpOnTrackClick(t *testing.T) {
	sb, viewport := openLookWithLines(t, 200, 60, 20)
	sbx, sby, _, sbh := sb.GetRect()

	// The track's own last row, just above both arrow buttons (which
	// now sit together at the bottom two rows — see Draw) — still part
	// of the track, below the thumb.
	trackLastRow := sby + sbh - 3
	sb.captureMouse(tview.MouseLeftDown, tcell.NewEventMouse(sbx, trackLastRow, tcell.Button1, 0))
	sb.stopRepeat()
	sb.captureMouse(tview.MouseLeftUp, tcell.NewEventMouse(sbx, trackLastRow, tcell.ButtonNone, 0))

	row, _ := sb.content.scrollOffset()
	if row != viewport {
		t.Errorf("scroll offset after one page-jump click = %d, want one viewport height (%d)", row, viewport)
	}
}

// TestVerticalScrollbarDragMovesContentProportionally pins hold-and-drag
// on the thumb itself: dragging it down moves the content forward, and
// releasing the mouse ends the drag so a later unrelated mouse move
// doesn't keep scrolling.
func TestVerticalScrollbarDragMovesContentProportionally(t *testing.T) {
	sb, _ := openLookWithLines(t, 200, 60, 20)
	sbx, sby, _, sbh := sb.GetRect()

	// The track now starts at the scrollbar's own top row (no arrow
	// above it — both sit at the bottom, see Draw), so the thumb's own
	// top row, at rest, is sby itself.
	sb.captureMouse(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby, tcell.Button1, 0))
	if !sb.dragging {
		t.Fatal("clicking the thumb itself should start a drag")
	}

	sb.captureMouse(tview.MouseMove, tcell.NewEventMouse(sbx, sby+sbh-2, tcell.Button1, 0))
	movedRow, _ := sb.content.scrollOffset()
	if movedRow <= 0 {
		t.Errorf("scroll offset after dragging the thumb most of the way down = %d, want it clearly advanced", movedRow)
	}

	sb.captureMouse(tview.MouseLeftUp, tcell.NewEventMouse(sbx, sby+sbh-2, tcell.ButtonNone, 0))
	if sb.dragging {
		t.Error("MouseLeftUp should end the drag")
	}

	sb.captureMouse(tview.MouseMove, tcell.NewEventMouse(sbx, sby, tcell.ButtonNone, 0))
	if afterRow, _ := sb.content.scrollOffset(); afterRow != movedRow {
		t.Errorf("scroll offset moved to %d after the drag already ended, want it to stay at %d", afterRow, movedRow)
	}
}

// TestCaptureViewerScrollbarMousePassesThroughOutsideScrollbar pins
// that a click elsewhere in the Look overlay (e.g. on its own content)
// is left alone — this hook must not swallow mouse events that have
// nothing to do with the scrollbar.
func TestCaptureViewerScrollbarMousePassesThroughOutsideScrollbar(t *testing.T) {
	sb, _ := openLookWithLines(t, 5, 60, 20)

	event := tcell.NewEventMouse(0, 0, tcell.Button1, 0)
	action, gotEvent := sb.captureMouse(tview.MouseLeftDown, event)

	if action != tview.MouseLeftDown || gotEvent != event {
		t.Errorf("captureMouse(outside click) = (%v, %v), want the event passed through unchanged", action, gotEvent)
	}
}

// TestCaptureViewerScrollbarMouseConsumesTrailingClick is a real,
// user-reported regression test: tview derives a trailing
// MouseLeftClick from the very same physical click that already fired
// MouseLeftDown (see Application.fireMouseActions), and Look is
// maximized to the full screen (see viewerSize's own doc comment), so
// that second action landing on the scrollbar's own column must also
// be consumed here — otherwise it falls through Pages' own dispatch to
// whatever's drawn at the same screen cell on the page stacked right
// behind it (here: the main panel's own Details slide-out button, see
// panel.go/detailsExpandBtn).
func TestCaptureViewerScrollbarMouseConsumesTrailingClick(t *testing.T) {
	sb, _ := openLookWithLines(t, 5, 60, 20)
	sbx, sby, _, _ := sb.GetRect()

	sb.captureMouse(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby, tcell.Button1, 0))
	sb.stopRepeat()
	sb.captureMouse(tview.MouseLeftUp, tcell.NewEventMouse(sbx, sby, tcell.ButtonNone, 0))

	action, event := sb.captureMouse(tview.MouseLeftClick, tcell.NewEventMouse(sbx, sby, tcell.ButtonNone, 0))
	if action != tview.MouseConsumed || event != nil {
		t.Errorf("captureMouse(trailing MouseLeftClick on the scrollbar) = (%v, %v), want it consumed (MouseConsumed, nil) rather than passed through to whatever's underneath", action, event)
	}
}

// TestTableContentScrollbarOnMounts proves the generalized scrollbar
// (scrollableContent/tableContent, see scrollbar.go) on an actual
// *tview.Table, not just the TextView it was piloted on — the first
// widget converted in the "wirklich überall" rollout, per the user's
// own explicit request to see it on a real catalog screen next. A
// click on the down arrow must step the table forward by one row via
// Table.SetOffset, the same contract textViewContent already has
// through TextView.ScrollTo.
func TestTableContentScrollbarOnMounts(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 60, 10) // short on purpose: a handful of mount rows should already overflow a 10-row screen once the title bar/hint take their own row each

	entries := make([]mounts.Entry, 50)
	for i := range entries {
		entries[i] = mounts.Entry{Target: fmt.Sprintf("/mnt/%02d", i), Fstype: "ext4"}
	}
	r.mountsEntries = entries
	r.mountsErr = nil
	// Not r.openMounts(): that calls reloadMounts(), which immediately
	// overwrites mountsEntries above with a real mounts.Read() of
	// whatever machine runs this test — on a real workstation that's
	// often still enough rows to accidentally overflow a 10-row screen
	// anyway, but a CI container can easily have far fewer, making the
	// test flaky for a reason that has nothing to do with the
	// scrollbar itself (a real, CI-caught failure). renderMounts +
	// showOverlay is the same "set state, then just render/show it"
	// shape TestRenderMountsShowsTheErrorInPlaceOfRows already uses.
	r.renderMounts()
	r.showOverlay(mountsPage, r.mountsLayout)

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(60, 10)
	r.mountsLayout.Draw(screen)

	sb := r.mountsScrollbar
	if sb.maxOffset() <= 0 {
		t.Fatal("maxOffset <= 0, want real overflow to scroll through for 50 mount rows on a 10-row screen")
	}

	sbx, sby, _, sbh := sb.GetRect()
	sb.captureMouse(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.Button1, 0))
	sb.stopRepeat()
	sb.captureMouse(tview.MouseLeftUp, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.ButtonNone, 0))

	if row, _ := r.mountsTable.GetOffset(); row != 1 {
		t.Errorf("mountsTable offset after one down-arrow click = %d, want 1", row)
	}
}

// TestVerticalScrollbarHiddenWhenContentFits pins the user's own
// explicit request: the scrollbar only shows up once content is
// actually taller than the box — a click where the down arrow would
// otherwise be must do nothing while everything already fits, not
// silently "work" on an invisible control.
func TestVerticalScrollbarHiddenWhenContentFits(t *testing.T) {
	sb, _ := openLookWithLines(t, 5, 60, 20)

	if got := sb.maxOffset(); got != 0 {
		t.Fatalf("maxOffset = %d, want 0 (5 short lines easily fit a 20-row viewport)", got)
	}

	sbx, sby, _, sbh := sb.GetRect()
	action, event := sb.captureMouse(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.Button1, 0))
	if action != tview.MouseConsumed || event != nil {
		t.Errorf("captureMouse(click where the down arrow would be) = (%v, %v), want it consumed (MouseConsumed, nil) rather than acted on", action, event)
	}
	if row, _ := sb.content.scrollOffset(); row != 0 {
		t.Errorf("scroll offset after clicking a hidden scrollbar = %d, want 0 (nothing should have happened)", row)
	}
	if sb.dragging {
		t.Error("clicking a hidden scrollbar's own thumb-sized track must not start a drag — there is no thumb to grab")
	}
}

// TestScrollbarThumbColorStaysVisibleOnAVeryDarkTrack is a real,
// user-reported regression test: Git Breach's own unfocused track
// (PanelBackground, #1c3232) darkened by a plain percentage factor
// landed so close to black that the thumb became indistinguishable
// from the track — "kann man nicht erkennen, wo die Leiste ist". The
// thumb must differ from the track by at least scrollbarThumbContrastDelta
// in at least one channel, regardless of how dark the track already is.
func TestScrollbarThumbColorStaysVisibleOnAVeryDarkTrack(t *testing.T) {
	track := tcell.NewRGBColor(28, 50, 50) // PanelBackground, #1c3232
	thumb := scrollbarThumbColor(track)

	tr, tg, tb := track.RGB()
	hr, hg, hb := thumb.RGB()
	maxDelta := func(a, b int32) int32 {
		d := a - b
		if d < 0 {
			d = -d
		}
		return d
	}
	biggest := maxDelta(tr, hr)
	if d := maxDelta(tg, hg); d > biggest {
		biggest = d
	}
	if d := maxDelta(tb, hb); d > biggest {
		biggest = d
	}
	if biggest < scrollbarThumbContrastDelta {
		t.Errorf("thumb %v vs track %v differ by only %d in the largest channel, want at least %d", thumb, track, biggest, scrollbarThumbContrastDelta)
	}
}

// TestScrollbarThumbColorKeepsDarkeningAMidToneTrack pins the other
// half of the fix: a track bright enough that darkening it doesn't
// clip at 0 should still darken (matching the user's own original "one
// shade darker" request, and Git Breach's own focused track — already
// confirmed to look right) rather than flip to lightening just because
// a generic luminance threshold happens to call it "dark".
func TestScrollbarThumbColorKeepsDarkeningAMidToneTrack(t *testing.T) {
	track := tcell.NewRGBColor(66, 91, 91) // Git Breach's own focused track (PopupBackground + the focus lighten)
	thumb := scrollbarThumbColor(track)

	tr, _, _ := track.RGB()
	hr, _, _ := thumb.RGB()
	if hr >= tr {
		t.Errorf("thumb red channel %d is not darker than track's own %d, want darkening (not lightening) for a track this far from clipping at 0", hr, tr)
	}
}
