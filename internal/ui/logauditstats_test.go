package ui

import (
	"strings"
	"testing"
	"time"

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
