package ui

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// stripText is renderTabStrip's own text with the color tags stripped —
// what actually lands on screen. Most assertions here care about the
// visible characters, not which of them carries the highlight (that has
// its own test below).
func stripText(count, active int) string {
	text, _ := renderTabStrip(count, active, tcell.ColorWhite, tcell.ColorBlack)
	var b strings.Builder
	inTag := false
	for _, r := range text {
		switch {
		case r == '[':
			inTag = true
		case r == ']':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestRenderTabStripNumbersEveryTab pins the basic shape: one number per
// tab, 1-based (matching Ctrl+1..Ctrl+0), separated by single spaces,
// with the trailing "+" button after them.
func TestRenderTabStripNumbersEveryTab(t *testing.T) {
	if got, want := stripText(3, 0), "1 2 3 +"; got != want {
		t.Errorf("renderTabStrip(3, 0) = %q, want %q", got, want)
	}
}

// TestRenderTabStripHighlightsOnlyTheActiveTab pins that exactly one
// number carries the active highlight, and that it's the right one —
// the strip's whole job.
func TestRenderTabStripHighlightsOnlyTheActiveTab(t *testing.T) {
	active := tcell.ColorRed
	text, _ := renderTabStrip(4, 2, tcell.ColorWhite, active)

	tag := ":" + colorTag(active) + "]"
	if got := strings.Count(text, tag); got != 1 {
		t.Fatalf("found %d highlighted tabs in %q, want exactly 1", got, text)
	}
	// The highlight tag must immediately precede the third number.
	idx := strings.Index(text, tag)
	if rest := text[idx+len(tag):]; !strings.HasPrefix(rest, "3") {
		t.Errorf("the highlight precedes %q, want it on tab 3", rest[:1])
	}
}

// TestRenderTabStripSpansMapClicksToTabs pins the click map — the part
// that turns a column into a tab index, and the easiest thing here to
// get subtly wrong by one.
func TestRenderTabStripSpansMapClicksToTabs(t *testing.T) {
	text, spans := renderTabStrip(3, 0, tcell.ColorWhite, tcell.ColorBlack)
	plain := stripText(3, 0)

	if got, want := len(spans), 4; got != want { // three tabs plus "+"
		t.Fatalf("got %d spans, want %d — %q", got, want, text)
	}
	for i, span := range spans[:3] {
		if span.tab != i {
			t.Errorf("span %d targets tab %d, want %d", i, span.tab, i)
		}
		if got, want := plain[span.start:span.end], string(rune('1'+i)); got != want {
			t.Errorf("span %d covers %q, want %q", i, got, want)
		}
	}
	last := spans[3]
	if last.tab != tabStripSpanNew {
		t.Errorf("last span targets tab %d, want tabStripSpanNew", last.tab)
	}
	if got := plain[last.start:last.end]; got != tabStripNewLabel {
		t.Errorf("last span covers %q, want %q", got, tabStripNewLabel)
	}
}

// TestRenderTabStripWindowsPastTheVisibleLimit pins the windowing: past
// tabStripMaxVisible the strip shows a slice around the active tab with
// markers for what's out of view, rather than growing across a header
// row that has a path to display too.
func TestRenderTabStripWindowsPastTheVisibleLimit(t *testing.T) {
	const count = 20
	// Active in the middle: both markers should appear.
	got := stripText(count, 10)
	if !strings.Contains(got, tabStripMoreBefore) || !strings.Contains(got, tabStripMoreAfter) {
		t.Errorf("strip = %q, want both overflow markers with the active tab mid-list", got)
	}
	if !strings.Contains(got, "11") {
		t.Errorf("strip = %q, want it to contain the active tab's own number", got)
	}

	// Active at the very start: nothing is scrolled off to the left.
	got = stripText(count, 0)
	if strings.Contains(got, tabStripMoreBefore) {
		t.Errorf("strip = %q, want no leading marker with the first tab active", got)
	}
	if !strings.HasPrefix(got, "1 ") {
		t.Errorf("strip = %q, want it to start at tab 1", got)
	}

	// Active at the very end: nothing is scrolled off to the right, and
	// the last tab is visible.
	got = stripText(count, count-1)
	if strings.Contains(got, tabStripMoreAfter) {
		t.Errorf("strip = %q, want no trailing marker with the last tab active", got)
	}
	if !strings.Contains(got, "20") {
		t.Errorf("strip = %q, want the last tab's own number visible", got)
	}
}

// TestTabStripWindowNeverExceedsMaxVisible pins the window size itself
// across every active position — the invariant the strip's own width
// budget depends on.
func TestTabStripWindowNeverExceedsMaxVisible(t *testing.T) {
	const count = 25
	for active := 0; active < count; active++ {
		first, last := tabStripWindow(count, active)
		if n := last - first + 1; n != tabStripMaxVisible {
			t.Errorf("active %d: window holds %d tabs, want %d", active, n, tabStripMaxVisible)
		}
		if active < first || active > last {
			t.Errorf("active %d: window [%d,%d] doesn't contain it", active, first, last)
		}
		if first < 0 || last > count-1 {
			t.Errorf("active %d: window [%d,%d] runs outside [0,%d]", active, first, last, count-1)
		}
	}
}

// TestRenderTabStripHandlesDegenerateInput pins that a zero/negative
// count and an out-of-range active index can't panic or produce a
// nonsense highlight — both are reachable from a corrupted saved
// layout (count itself is never actually 0 in practice — there's
// always at least one tab — this only pins that it doesn't panic).
func TestRenderTabStripHandlesDegenerateInput(t *testing.T) {
	if got, want := stripText(0, 0), " "+tabStripNewLabel; got != want {
		t.Errorf("renderTabStrip(0, 0) = %q, want %q", got, want)
	}
	if got, want := stripText(2, 99), "1 2 +"; got != want {
		t.Errorf("out-of-range active: %q, want %q (highlight falls back to the first)", got, want)
	}
}

// TestRenderTabStripWithASingleTabStillGetsARealNumber pins the current
// contract (see renderTabStrip's own doc comment on why the earlier
// "+"-only version of this wasn't enough once Terminals could cover the
// whole screen): a single tab still draws its own real "1", a genuine
// clickable target to leave Terminal view and land back on it, not just
// the "+".
func TestRenderTabStripWithASingleTabStillGetsARealNumber(t *testing.T) {
	_, spans := renderTabStrip(1, 0, tcell.ColorWhite, tcell.ColorBlack)
	// Tab 0 is also the active one here, so its own "1" carries the
	// highlight color tag — stripText strips it, the same as every
	// other test in this file that only cares about the plain glyphs.
	if want := "1 " + tabStripNewLabel; stripText(1, 0) != want {
		t.Errorf("renderTabStrip(1, 0) plain text = %q, want %q", stripText(1, 0), want)
	}
	if len(spans) != 2 {
		t.Fatalf("spans = %v, want 2 (the tab, then +)", spans)
	}
	if spans[0].tab != 0 {
		t.Errorf("first span targets %d, want tab 0", spans[0].tab)
	}
	if spans[1].tab != tabStripSpanNew {
		t.Errorf("last span targets %d, want tabStripSpanNew", spans[1].tab)
	}
}

// TestTabStripWidthMatchesRenderedText pins that the width reserved in
// the header row is the width actually drawn — a mismatch would either
// clip the "+" or leave a gap before the Details button.
func TestTabStripWidthMatchesRenderedText(t *testing.T) {
	for _, count := range []int{2, 5, 10, 15} {
		text, _ := renderTabStrip(count, 0, tcell.ColorWhite, tcell.ColorBlack)
		if got, want := tabStripWidth(count, 0), tview.TaggedStringWidth(text); got != want {
			t.Errorf("count %d: tabStripWidth = %d, want %d", count, got, want)
		}
	}
}

// TestPanelTabStripShowsANumberAndPlusWithASingleTab pins the current
// contract (see renderTabStrip's own doc comment): one tab still shows
// its own real "1", not just "+" — a real, clickable way back to it
// once a Terminal is covering the screen.
func TestPanelTabStripShowsANumberAndPlusWithASingleTab(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	// The widget's own drawn text carries a headerTabStripGap-wide leading
	// space in front of the real content (see refreshTabStrip's own doc
	// comment on why that's baked into the text, not a separate spacer
	// item) — spans are shifted the same amount so a click still maps to
	// the right column.
	want := strings.Repeat(" ", headerTabStripGap) + "1 " + tabStripNewLabel
	if got := r.panel.tabStrip.GetText(true); got != want {
		t.Errorf("tab strip with one tab = %q, want %q", got, want)
	}
	if len(r.panel.tabStripSpans) != 2 {
		t.Fatalf("tab strip spans = %v, want 2 (the tab, then +)", r.panel.tabStripSpans)
	}
	if r.panel.tabStripSpans[0].tab != 0 {
		t.Errorf("first span targets %d, want tab 0", r.panel.tabStripSpans[0].tab)
	}
	if r.panel.tabStripSpans[1].tab != tabStripSpanNew {
		t.Errorf("last span targets %d, want tabStripSpanNew", r.panel.tabStripSpans[1].tab)
	}
	if got, want := r.panel.tabStripSpans[0].start, headerTabStripGap; got != want {
		t.Errorf("the \"1\" span starts at column %d, want %d (right after the leading gap)", got, want)
	}
}

// TestTabStripPlusClickWithASingleTabOpensANewTab pins the mouse path
// this whole fix is for: clicking the lone "+" with only one tab open
// actually creates a second one.
func TestTabStripPlusClickWithASingleTabOpensANewTab(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.tabStrip.SetRect(0, 0, 10, 1)

	plus := r.panel.tabStripSpans[len(r.panel.tabStripSpans)-1]
	if plus.tab != tabStripSpanNew {
		t.Fatalf("setup: last span targets %d, want tabStripSpanNew", plus.tab)
	}
	r.panel.captureTabStripMouse(
		tview.MouseLeftClick,
		tcell.NewEventMouse(plus.start, 0, tcell.ButtonNone, 0),
	)

	if got := r.tabCount(); got != 2 {
		t.Errorf("tab count after clicking the lone + = %d, want 2", got)
	}
}

// TestTabStripNumberClickWithASingleTabLeavesTerminalView pins the real
// bug this whole fix is for: with only one tab open and a Terminal on
// screen, clicking that tab's own "1" has to actually leave Terminal
// view and show the panel again — not a no-op just because the active
// tab index never changes (see switchToTab's own doc comment).
func TestTabStripNumberClickWithASingleTabLeavesTerminalView(t *testing.T) {
	r := newTestRootForShells(t)
	stubShellSession(t, func() *exec.Cmd { return exec.Command("sh", "-c", "sleep 5") })
	r.openNewShellEmbedded(testShellConn("one"))
	defer func() { _ = r.shells[0].session.Close() }()

	r.panel.headerRow.SetRect(0, 0, 40, 1)
	r.panel.tabStrip.SetRect(0, 0, 10, 1)

	one := r.panel.tabStripSpans[0]
	if one.tab != 0 {
		t.Fatalf("setup: first span targets %d, want tab 0", one.tab)
	}
	r.panel.captureTabStripMouse(
		tview.MouseLeftClick,
		tcell.NewEventMouse(one.start, 0, tcell.ButtonNone, 0),
	)

	if r.viewingShells {
		t.Error("viewingShells still true after clicking the tab's own number")
	}
}

// TestTabStripLeadingGapOpensTheSwitcherNotANewTab pins that the gap
// itself behaves like any other non-glyph column in the strip (see
// captureTabStripMouse's own doc comment) — it's part of the tab
// indicator visually, but a click there isn't a click on "+" and
// shouldn't be mistaken for one.
func TestTabStripLeadingGapOpensTheSwitcherNotANewTab(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.tabStrip.SetRect(0, 0, 10, 1)

	r.panel.captureTabStripMouse(
		tview.MouseLeftClick,
		tcell.NewEventMouse(0, 0, tcell.ButtonNone, 0), // inside the leading gap
	)

	if got := r.tabCount(); got != 1 {
		t.Errorf("tab count after clicking the leading gap = %d, want 1 (no new tab)", got)
	}
	if r.activePage != tabSwitcherPage {
		t.Errorf("activePage = %q, want %q", r.activePage, tabSwitcherPage)
	}
}

// TestPanelTabStripAppearsWithASecondTab is the other half: as soon as
// there's something to switch between, the strip shows up.
func TestPanelTabStripAppearsWithASecondTab(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.newTab(dir)

	got := r.panel.tabStrip.GetText(true)
	if !strings.Contains(got, "1") || !strings.Contains(got, "2") {
		t.Errorf("tab strip with two tabs = %q, want both numbers", got)
	}
	if len(r.panel.tabStripSpans) == 0 {
		t.Error("tab strip has no click spans with two tabs open")
	}
}

// TestTabStripClickSwitchesTabs pins the mouse path: clicking a number
// switches to that tab.
func TestTabStripClickSwitchesTabs(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.newTab(dir) // now on tab 2
	if r.activeTab != 1 {
		t.Fatalf("setup: activeTab = %d, want 1", r.activeTab)
	}

	// Give the strip a real rect so the click's column can be resolved.
	r.panel.tabStrip.SetRect(0, 0, 20, 1)
	span := r.panel.tabStripSpans[0] // tab 1
	captured, _ := r.panel.captureTabStripMouse(
		tview.MouseLeftClick,
		tcell.NewEventMouse(span.start, 0, tcell.ButtonNone, 0),
	)

	if captured != tview.MouseConsumed {
		t.Error("a click on the tab strip should be consumed")
	}
	if r.activeTab != 0 {
		t.Errorf("activeTab after clicking tab 1 = %d, want 0", r.activeTab)
	}
}

// TestTabStripPlusClickOpensANewTab pins the "+" button.
func TestTabStripPlusClickOpensANewTab(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.newTab(dir) // the strip only exists once there are two tabs
	r.panel.tabStrip.SetRect(0, 0, 20, 1)

	plus := r.panel.tabStripSpans[len(r.panel.tabStripSpans)-1]
	if plus.tab != tabStripSpanNew {
		t.Fatalf("setup: last span targets %d, want tabStripSpanNew", plus.tab)
	}
	r.panel.captureTabStripMouse(
		tview.MouseLeftClick,
		tcell.NewEventMouse(plus.start, 0, tcell.ButtonNone, 0),
	)

	if got := r.tabCount(); got != 3 {
		t.Errorf("tab count after clicking + = %d, want 3", got)
	}
}

// TestTabStripClickInTheGapsOpensTheSwitcher pins the user's own stated
// intent for the strip as a whole: clicking "the tab area" — anywhere
// that isn't a specific number or the "+" — offers the full list, since
// the numbers themselves deliberately don't say what any tab holds.
func TestTabStripClickInTheGapsOpensTheSwitcher(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.newTab(dir)
	r.panel.tabStrip.SetRect(0, 0, 20, 1)

	// The single space between tab 1 and tab 2 belongs to no span.
	gap := r.panel.tabStripSpans[0].end
	r.panel.captureTabStripMouse(
		tview.MouseLeftClick,
		tcell.NewEventMouse(gap, 0, tcell.ButtonNone, 0),
	)

	if r.activePage != tabSwitcherPage {
		t.Errorf("activePage = %q, want %q", r.activePage, tabSwitcherPage)
	}
}
