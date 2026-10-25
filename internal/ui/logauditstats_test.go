package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/logview"
)

func TestLogAuditTimelineBucketsNotEnoughRange(t *testing.T) {
	if _, _, _, _, ok := logAuditTimelineBuckets(nil, 10); ok {
		t.Error("no entries should not produce buckets")
	}
	same := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
	entries := []logview.Entry{{Time: same}, {Time: same}}
	if _, _, _, _, ok := logAuditTimelineBuckets(entries, 10); ok {
		t.Error("every entry sharing the exact same Time should not produce buckets")
	}
}

func TestLogAuditTimelineBucketsSpreadsAcrossRange(t *testing.T) {
	base := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
	entries := []logview.Entry{
		{Time: base, Level: logview.LevelInfo},
		{Time: base.Add(9 * time.Minute), Level: logview.LevelError},
	}
	counts, severity, minT, maxT, ok := logAuditTimelineBuckets(entries, 10)
	if !ok {
		t.Fatal("two distinct timestamps should produce buckets")
	}
	if counts[0] != 1 || counts[len(counts)-1] != 1 {
		t.Errorf("counts = %v, want the first and last bucket each holding one entry", counts)
	}
	if severity[len(severity)-1] != logview.LevelError {
		t.Errorf("severity[last] = %v, want LevelError (the later entry)", severity[len(severity)-1])
	}
	if !minT.Equal(base) || !maxT.Equal(base.Add(9*time.Minute)) {
		t.Errorf("minT,maxT = %v,%v, want %v,%v", minT, maxT, base, base.Add(9*time.Minute))
	}
}

func TestLogAuditBucketTimeRangeDividesSpanEvenly(t *testing.T) {
	minT := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
	maxT := minT.Add(100 * time.Second)

	from, to := logAuditBucketTimeRange(minT, maxT, 10, 0)
	if !from.Equal(minT) {
		t.Errorf("bucket 0 from = %v, want minT %v", from, minT)
	}
	if want := minT.Add(10 * time.Second); !to.Equal(want) {
		t.Errorf("bucket 0 to = %v, want %v", to, want)
	}

	from, to = logAuditBucketTimeRange(minT, maxT, 10, 5)
	if want := minT.Add(50 * time.Second); !from.Equal(want) {
		t.Errorf("bucket 5 from = %v, want %v", from, want)
	}
	if want := minT.Add(60 * time.Second); !to.Equal(want) {
		t.Errorf("bucket 5 to = %v, want %v", to, want)
	}
}

// TestLogAuditBucketTimeRangeLastBucketClosesAtMaxT pins the one case
// that must never drift from logAuditTimelineBuckets' own clamp: an
// entry at exactly maxT lands in the last bucket (idx clamped to
// bucketCount-1), so the last bucket's own range must close exactly at
// maxT, not at the arithmetically rounded minT+bucketCount*span/bucketCount
// — floating-point division could otherwise land a hair short of maxT
// and exclude the very entry that defines it.
func TestLogAuditBucketTimeRangeLastBucketClosesAtMaxT(t *testing.T) {
	minT := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
	maxT := minT.Add(7 * time.Second) // deliberately not evenly divisible by bucketCount

	_, to := logAuditBucketTimeRange(minT, maxT, 3, 2)
	if !to.Equal(maxT) {
		t.Errorf("last bucket's to = %v, want exactly maxT %v", to, maxT)
	}
}

func TestLogAuditBucketTimeRangeSingleBucketSpansWholeRange(t *testing.T) {
	minT := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
	maxT := minT.Add(42 * time.Second)

	from, to := logAuditBucketTimeRange(minT, maxT, 1, 0)
	if !from.Equal(minT) || !to.Equal(maxT) {
		t.Errorf("from,to = %v,%v, want minT,maxT %v,%v", from, to, minT, maxT)
	}
}

func TestLogAuditTimelineTextColorsErrorBucketRed(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	base := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
	entries := []logview.Entry{
		{Time: base, Level: logview.LevelInfo},
		{Time: base.Add(time.Minute), Level: logview.LevelError},
	}
	got := logAuditTimelineText(entries, 10, theme)
	if got == "" {
		t.Fatal("two distinct timestamps should render a non-empty timeline")
	}
	if !strings.Contains(got, colorTag(theme.CriticalText)) {
		t.Errorf("timeline = %q, want the error bucket colored with CriticalText", got)
	}
}

// TestLogAuditTimelineBuildTextSelectionDoesNotBleedToLaterBuckets pins
// a real, reported bug: tview's own tag syntax treats an omitted
// attrs field as "leave it unchanged from before", not "reset to
// none", so a bare "[color]" tag on every non-selected bucket let the
// "::r" reverse attribute one selected bucket set bleed forward into
// every bucket rendered after it in the same string — moving the
// keyboard selection made the entire rest of the bar past that point
// look selected too, not just the one bucket actually under the
// cursor.
func TestLogAuditTimelineBuildTextSelectionDoesNotBleedToLaterBuckets(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	counts := []int{1, 1, 1, 1, 1}
	severity := []logview.Level{logview.LevelInfo, logview.LevelInfo, logview.LevelInfo, logview.LevelInfo, logview.LevelInfo}

	got := logAuditTimelineBuildText(counts, severity, "start", "end", theme, 2, 2)

	if n := strings.Count(got, "::r"); n != 1 {
		t.Errorf("text = %q, want exactly one \"::r\" tag (the single selected bucket), got %d", got, n)
	}
	if !strings.Contains(got, "::-") {
		t.Errorf("text = %q, want every non-selected bucket to explicitly reset its own attrs with \"::-\", not merely omit the field", got)
	}
}

// TestLogAuditTimelineTextIncludesStartAndEndLabels pins the user's
// own explicit report: the density bar alone said nothing about what
// time range or scale it actually covered, confusing on a sparse log
// where most of the width is blank between a few real bursts.
func TestLogAuditTimelineTextIncludesStartAndEndLabels(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	base := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
	entries := []logview.Entry{
		{Time: base},
		{Time: base.Add(9 * time.Minute)},
	}
	got := logAuditTimelineText(entries, 60, theme)
	if !strings.Contains(got, "16:00:00") {
		t.Errorf("timeline = %q, want the start time 16:00:00 labeled", got)
	}
	if !strings.Contains(got, "16:09:00") {
		t.Errorf("timeline = %q, want the end time 16:09:00 labeled", got)
	}
}

// TestLogAuditTimelineLabelIncludesDateOnlyAcrossDayBoundary pins
// logAuditTimelineLabel's own sameDay contract: bare time when both
// endpoints share a calendar day (the common case), the full date too
// once the span crosses one, where the bare time alone would be
// ambiguous about which day it belongs to.
func TestLogAuditTimelineLabelIncludesDateOnlyAcrossDayBoundary(t *testing.T) {
	t1 := time.Date(2026, 10, 7, 23, 59, 0, 0, time.UTC)
	if got := logAuditTimelineLabel(t1, true); got != "23:59:00" {
		t.Errorf("sameDay label = %q, want bare time 23:59:00", got)
	}
	if got := logAuditTimelineLabel(t1, false); got != "2026-10-07 23:59:00" {
		t.Errorf("cross-day label = %q, want the full date too", got)
	}
}

// TestRenderLogAuditTimelineIgnoresUnlaidOutDefaultWidth pins a real,
// user-reported bug: a freshly-opened group's very first timeline
// render used tview.Box's own un-laid-out default width (15 — see
// logAuditTimelineUnlaidOutWidth's own doc comment) as if it were the
// real screen width, producing a narrow bar that didn't match the
// width every later render of the exact same entries correctly used —
// confusingly different-looking for no reason the user had any way to
// know about.
func TestRenderLogAuditTimelineIgnoresUnlaidOutDefaultWidth(t *testing.T) {
	dir := t.TempDir()
	writeLogAuditFixture(t, dir)
	r := newTestRootForLogAudit(t, dir)
	r.SetRect(0, 0, 200, 50)
	r.logAuditTable.Select(1, 0)

	// logAuditTimelineView has never been drawn at this point — still
	// at tview's own NewBox default (0,0,15,10) — the exact state a
	// freshly-opened group's first render actually runs under (see
	// openLogAuditViewer: renderLogAuditViewer runs before pushOverlay,
	// and pushOverlay alone never cascades a real rect to a child —
	// only a real Draw() does).
	if _, _, w, _ := r.logAuditTimelineView.GetRect(); w != 15 {
		t.Fatalf("setup: logAuditTimelineView width = %d, want tview's own un-laid-out default 15", w)
	}

	r.openLogAuditViewer()

	got := r.logAuditTimelineView.GetText(true)
	if got == "" {
		t.Fatal("expected a non-empty timeline for two distinct timestamps")
	}
	if len(got) < 60 {
		t.Errorf("timeline = %q (%d chars), want it sized against the real screen width (200), not tview's own un-laid-out default (15)", got, len(got))
	}
}

func TestLogAuditCountsByLevelOmitsZeroAndOrdersBySeverity(t *testing.T) {
	entries := []logview.Entry{
		{Level: logview.LevelError}, {Level: logview.LevelError}, {Level: logview.LevelInfo},
	}
	got := logAuditCountsByLevel(entries)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (Error, Info — Warn/Debug/etc. omitted)", len(got))
	}
	if got[0].Level != logview.LevelError || got[0].Count != 2 {
		t.Errorf("got[0] = %+v, want {LevelError 2} first (worse severity first)", got[0])
	}
	if got[1].Level != logview.LevelInfo || got[1].Count != 1 {
		t.Errorf("got[1] = %+v, want {LevelInfo 1}", got[1])
	}
}

func TestLogAuditCountsBySourceSortsByCountThenName(t *testing.T) {
	entries := []logview.Entry{
		{Source: "nginx"}, {Source: "nginx"}, {Source: "app"}, {Source: ""},
	}
	got := logAuditCountsBySource(entries)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (blank Source excluded)", len(got))
	}
	if got[0].Source != "nginx" || got[0].Count != 2 {
		t.Errorf("got[0] = %+v, want {nginx 2} first (higher count)", got[0])
	}
}

func TestLogAuditStatsTextReportsTotalsAndNoSourceFallback(t *testing.T) {
	entries := []logview.Entry{{Level: logview.LevelError, Source: ""}}
	got := logAuditStatsText(entries)
	if !strings.Contains(got, "1 events") {
		t.Errorf("logAuditStatsText = %q, want the total event count", got)
	}
	if !strings.Contains(got, "no Source recognized") {
		t.Errorf("logAuditStatsText = %q, want the no-Source fallback line", got)
	}
}

// openLogAuditStatsOverflowing opens the Statistics modal on a small
// screen with entries engineered to overflow both axes: every Level (7
// rows) plus one very long Source name, on a screen too small to show
// either without scrolling.
func openLogAuditStatsOverflowing(t *testing.T, width, height int) *Root {
	t.Helper()
	r, err := NewRoot(tview.NewApplication(), t.TempDir())
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, width, height)

	entries := make([]logview.Entry, 0, len(logAuditLevelOrder))
	for _, lvl := range logAuditLevelOrder {
		entries = append(entries, logview.Entry{Level: lvl, Source: strings.Repeat("x", 100)})
	}
	r.logAuditAllEntries = entries
	r.openLogAuditStats()
	return r
}

func drawLogAuditStats(t *testing.T, r *Root, width, height int) {
	t.Helper()
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(width, height)
	r.logAuditStatsLayout.Draw(screen)
}

// TestLogAuditStatsScrollbarMouseClickThroughRealDispatch mirrors the
// same regression test shape used for every other scrollbar in this
// rollout: a click on the scrollbar's own down arrow must actually
// scroll the Statistics modal's own content, driven through the real
// dispatch chain (logAuditStatsLayout's own MouseHandler), not
// captureMouse directly.
func TestLogAuditStatsScrollbarMouseClickThroughRealDispatch(t *testing.T) {
	r := openLogAuditStatsOverflowing(t, 100, 10)
	drawLogAuditStats(t, r, 100, 10)

	sb := r.logAuditStatsScrollbar
	if sb.maxOffset() <= 0 {
		t.Fatalf("maxOffset = %d, want real overflow on a 10-row screen", sb.maxOffset())
	}
	sbx, sby, _, sbh := sb.GetRect()
	before, _ := r.logAuditStatsView.GetScrollOffset()

	setFocus := func(p tview.Primitive) { r.app.SetFocus(p) }
	handler := r.logAuditStatsLayout.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.Button1, 0), setFocus)
	sb.stopRepeat()
	handler(tview.MouseLeftUp, tcell.NewEventMouse(sbx, sby+sbh-1, tcell.ButtonNone, 0), setFocus)

	after, _ := r.logAuditStatsView.GetScrollOffset()
	if after != before+1 {
		t.Errorf("logAuditStatsView offset after clicking the down arrow = %d, want %d (before=%d)", after, before+1, before)
	}
}

// TestLogAuditStatsHScrollbarMouseClickThroughRealDispatch is the
// horizontal counterpart, using the long Source name to force overflow
// on a screen wide enough to show every Level row but not the long
// line.
func TestLogAuditStatsHScrollbarMouseClickThroughRealDispatch(t *testing.T) {
	r := openLogAuditStatsOverflowing(t, 40, 30)
	drawLogAuditStats(t, r, 40, 30)

	sb := r.logAuditStatsHScrollbar
	if sb.maxOffset() <= 0 {
		t.Fatalf("maxOffset = %d, want real overflow for a 100-character source name on a 40-column screen", sb.maxOffset())
	}
	sbx, sby, _, _ := sb.GetRect()
	_, before := r.logAuditStatsView.GetScrollOffset()

	setFocus := func(p tview.Primitive) { r.app.SetFocus(p) }
	handler := r.logAuditStatsLayout.MouseHandler()
	handler(tview.MouseLeftDown, tcell.NewEventMouse(sbx+1, sby, tcell.Button1, 0), setFocus)
	sb.stopRepeat()
	handler(tview.MouseLeftUp, tcell.NewEventMouse(sbx+1, sby, tcell.ButtonNone, 0), setFocus)

	_, after := r.logAuditStatsView.GetScrollOffset()
	if after != before+1 {
		t.Errorf("logAuditStatsView column offset after clicking the right arrow = %d, want %d (before=%d)", after, before+1, before)
	}
}

// TestLogAuditStatsScrollbarsDoNotShareTheCornerCell pins the user's
// own explicit requirement that the vertical and horizontal bars never
// overlap.
func TestLogAuditStatsScrollbarsDoNotShareTheCornerCell(t *testing.T) {
	r := openLogAuditStatsOverflowing(t, 100, 10)
	drawLogAuditStats(t, r, 100, 10)

	vx, _, _, _ := r.logAuditStatsScrollbar.GetRect()
	hx, _, hw, _ := r.logAuditStatsHScrollbar.GetRect()
	if hx+hw > vx {
		t.Errorf("horizontal bar (x=%d width=%d) reaches into the vertical bar's own column (x=%d)", hx, hw, vx)
	}
}
