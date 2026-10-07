package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/logview"
)

func TestLogAuditTimelineBucketsNotEnoughRange(t *testing.T) {
	if _, _, ok := logAuditTimelineBuckets(nil, 10); ok {
		t.Error("no entries should not produce buckets")
	}
	same := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
	entries := []logview.Entry{{Time: same}, {Time: same}}
	if _, _, ok := logAuditTimelineBuckets(entries, 10); ok {
		t.Error("every entry sharing the exact same Time should not produce buckets")
	}
}

func TestLogAuditTimelineBucketsSpreadsAcrossRange(t *testing.T) {
	base := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
	entries := []logview.Entry{
		{Time: base, Level: logview.LevelInfo},
		{Time: base.Add(9 * time.Minute), Level: logview.LevelError},
	}
	counts, severity, ok := logAuditTimelineBuckets(entries, 10)
	if !ok {
		t.Fatal("two distinct timestamps should produce buckets")
	}
	if counts[0] != 1 || counts[len(counts)-1] != 1 {
		t.Errorf("counts = %v, want the first and last bucket each holding one entry", counts)
	}
	if severity[len(severity)-1] != logview.LevelError {
		t.Errorf("severity[last] = %v, want LevelError (the later entry)", severity[len(severity)-1])
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
