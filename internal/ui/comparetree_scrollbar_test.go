package ui

import (
	"fmt"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/compare"
)

// TestCompareTreeScrollbarMouseClickThroughRealDispatch mirrors the
// same regression test shape used for every other table-backed
// scrollbar in this rollout (Mounts, Firewall, ...): a click on the
// scrollbar's own down arrow must actually scroll, driven through the
// real dispatch chain (compare.TreeLayout's own MouseHandler), not the
// scrollbar's captureMouse directly.
func TestCompareTreeScrollbarMouseClickThroughRealDispatch(t *testing.T) {
	r, _ := newCompareRoot(t)
	r.SetRect(0, 0, 100, 20)

	entries := make([]compare.Entry, 50)
	for i := range entries {
		entries[i] = compare.Entry{RelPath: fmt.Sprintf("file%02d.txt", i), Verdict: compare.Differs}
	}
	r.compare.TreeEntries = entries
	r.renderCompareTree()
	r.resetCompareTreeScrollPosition()
	r.showOverlay(compareTreePage, r.compare.TreeLayout)

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(100, 20)
	r.compare.TreeLayout.Draw(screen)

	sb := r.compare.TreeScrollbar
	if sb.maxOffset() <= 0 {
		t.Fatalf("maxOffset = %d, want real overflow for 50 entries on a 20-row screen", sb.maxOffset())
	}
	sbx, sby, _, sbh := sb.GetRect()
	before, _ := r.compare.TreeTable.GetOffset()

	setFocus := func(p tview.Primitive) { r.app.SetFocus(p) }
	handler := r.compare.TreeLayout.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.Button1, 0), setFocus)
	sb.stopRepeat()
	handler(tview.MouseLeftUp, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.ButtonNone, 0), setFocus)

	after, _ := r.compare.TreeTable.GetOffset()
	if after != before+1 {
		t.Errorf("compare.TreeTable offset after clicking the down arrow = %d, want %d (before=%d)", after, before+1, before)
	}
}
