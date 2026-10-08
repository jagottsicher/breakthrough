// logauditstats.go is the Log Audit viewer's own two Phase 2
// additions that are pure computation over []logview.Entry rather
// than screen plumbing: the mini-timeline above the table (see
// renderLogAuditTimeline) and the Statistics modal ("s").
package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/logview"
)

const logAuditStatsPage = "logaudit-stats"

// logAuditStatsMaxWidth mirrors logAuditDetailMaxWidth's own reasoning
// — a centered, screen-sized modal for one computed report.
const logAuditStatsMaxWidth = 60

// timelineBlocks are the density levels logAuditTimelineText steps
// through, index 0 (blank) for an empty bucket — the same "meter"
// shape chordCountdownBlocks (keymap.go) already uses elsewhere in
// this app, just filling upward (U+2581 through U+2588) instead of
// draining downward, since this meter means "how much happened here",
// not "how much time is left".
var timelineBlocks = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// logAuditTimelineBuckets buckets entries' own Time into bucketCount
// equal-width slices spanning the earliest to the latest timestamp
// among them (zero Times are skipped — they'd only ever come from a
// bug upstream; ParseAll already seeds every timestamp-less line with
// its file's own mtime, see its own doc comment). Returns ok=false
// when there's no meaningful span to bucket at all — fewer than two
// distinct timestamps, the same "not enough range for a timeline"
// case a single-entry or all-identical-timestamp result both are.
// severity[i] is the highest Level seen in that bucket, driving
// logAuditTimelineText's own per-bucket color.
func logAuditTimelineBuckets(entries []logview.Entry, bucketCount int) (counts []int, severity []logview.Level, ok bool) {
	if bucketCount <= 0 {
		return nil, nil, false
	}

	var minT, maxT time.Time
	first := true
	for _, e := range entries {
		if e.Time.IsZero() {
			continue
		}
		if first || e.Time.Before(minT) {
			minT = e.Time
		}
		if first || e.Time.After(maxT) {
			maxT = e.Time
		}
		first = false
	}
	if first || !maxT.After(minT) {
		return nil, nil, false
	}

	span := maxT.Sub(minT)
	counts = make([]int, bucketCount)
	severity = make([]logview.Level, bucketCount)
	for _, e := range entries {
		if e.Time.IsZero() {
			continue
		}
		idx := int(float64(e.Time.Sub(minT)) / float64(span) * float64(bucketCount))
		if idx >= bucketCount {
			idx = bucketCount - 1
		}
		if idx < 0 {
			idx = 0
		}
		counts[idx]++
		if e.Level > severity[idx] {
			severity[idx] = e.Level
		}
	}
	return counts, severity, true
}

// logAuditTimelineText renders entries' own time distribution as one
// line of density blocks, colored per bucket — red if that bucket
// contains an Error/Fatal, the app's own WarningText if a Warn (and
// nothing worse), otherwise plain text color — "" when
// logAuditTimelineBuckets itself has nothing meaningful to show
// (renderLogAuditTimeline then leaves the line blank rather than
// drawing an empty, misleadingly flat bar).
func logAuditTimelineText(entries []logview.Entry, width int, theme config.ResolvedTheme) string {
	if width < 10 {
		width = 10
	}
	if width > 200 {
		width = 200
	}
	counts, severity, ok := logAuditTimelineBuckets(entries, width)
	if !ok {
		return ""
	}

	maxCount := 0
	for _, c := range counts {
		if c > maxCount {
			maxCount = c
		}
	}
	if maxCount == 0 {
		return ""
	}

	var b strings.Builder
	for i, c := range counts {
		level := 0
		if c > 0 {
			level = 1 + int(float64(c)/float64(maxCount)*float64(len(timelineBlocks)-2))
			if level >= len(timelineBlocks) {
				level = len(timelineBlocks) - 1
			}
		}
		color := theme.MutedTextColor
		switch {
		case severity[i] == logview.LevelError || severity[i] == logview.LevelFatal:
			color = theme.CriticalText
		case severity[i] == logview.LevelWarn:
			color = theme.WarningText
		case c > 0:
			color = theme.Text
		}
		fmt.Fprintf(&b, "[%s]%c", colorTag(color), timelineBlocks[level])
	}
	return b.String()
}

// renderLogAuditTimeline fills the timeline bar from entries (the
// viewer's own currently-matching set — see renderLogAuditViewer) —
// the bar's own real width if it's been drawn at least once, else the
// whole screen's width as a reasonable first guess (the same
// first-draw fallback nameColumnWidth already uses for exactly the
// same reason).
func (r *Root) renderLogAuditTimeline(entries []logview.Entry) {
	_, _, width, _ := r.logAuditTimelineView.GetRect()
	if width <= 0 {
		if _, _, screenWidth, _ := r.GetRect(); screenWidth > 4 {
			width = screenWidth - 4
		} else {
			width = 60
		}
	}
	r.logAuditTimelineView.SetText(logAuditTimelineText(entries, width, r.theme))
}

// logAuditLevelCount/logAuditSourceCount are logAuditCountsByLevel/
// logAuditCountsBySource's own result rows.
type logAuditLevelCount struct {
	Level logview.Level
	Count int
}

type logAuditSourceCount struct {
	Source string
	Count  int
}

// logAuditLevelOrder is every Level the Statistics modal lists, worst
// severity first — matches the "stop sign first" convention
// logAuditLevelColor already establishes, so the modal reads the same
// "most urgent at the top" way the color coding already implies.
var logAuditLevelOrder = []logview.Level{
	logview.LevelFatal, logview.LevelError, logview.LevelWarn,
	logview.LevelInfo, logview.LevelDebug, logview.LevelTrace, logview.LevelUnknown,
}

// logAuditCountsByLevel counts entries per Level, in logAuditLevelOrder
// — a Level with zero entries is left out rather than shown as "0",
// the same "don't advertise something that isn't there" reasoning
// chordFamilies' own "y" family doc comment gives for a different
// kind of absence.
func logAuditCountsByLevel(entries []logview.Entry) []logAuditLevelCount {
	counts := map[logview.Level]int{}
	for _, e := range entries {
		counts[e.Level]++
	}
	var out []logAuditLevelCount
	for _, l := range logAuditLevelOrder {
		if c := counts[l]; c > 0 {
			out = append(out, logAuditLevelCount{Level: l, Count: c})
		}
	}
	return out
}

// logAuditStatsMaxSources caps the Statistics modal's own "by source"
// section — a format with hundreds of distinct Source values (one per
// container, say) would otherwise turn a quick overview into a second
// scrollable table, which is exactly what the main view already is.
const logAuditStatsMaxSources = 10

// logAuditCountsBySource counts entries per Source, busiest first (a
// tie breaks on name, for a deterministic order rather than Go's own
// unspecified map-iteration order) — an entry with no recognized
// Source (e.g. a FormatPlain line) is left out: there is nothing
// meaningful to group it under.
func logAuditCountsBySource(entries []logview.Entry) []logAuditSourceCount {
	counts := map[string]int{}
	for _, e := range entries {
		if e.Source == "" {
			continue
		}
		counts[e.Source]++
	}
	out := make([]logAuditSourceCount, 0, len(counts))
	for s, c := range counts {
		out = append(out, logAuditSourceCount{Source: s, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Source < out[j].Source
	})
	return out
}

// logAuditStatsText renders the Statistics modal's own full report —
// escaped once at the end (tview.Escape), since every piece going
// into it (Level.String(), a Source value) is already known plain
// text, never itself carrying style tags.
func logAuditStatsText(entries []logview.Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d events\n\nBy level:\n", len(entries))
	for _, lc := range logAuditCountsByLevel(entries) {
		fmt.Fprintf(&b, "  %-6s %d\n", lc.Level.String(), lc.Count)
	}

	sources := logAuditCountsBySource(entries)
	fmt.Fprintf(&b, "\nBy source (top %d):\n", logAuditStatsMaxSources)
	if len(sources) == 0 {
		b.WriteString("  (no Source recognized for this format)\n")
	}
	for i, sc := range sources {
		if i >= logAuditStatsMaxSources {
			break
		}
		fmt.Fprintf(&b, "  %-20s %d\n", sc.Source, sc.Count)
	}
	return tview.Escape(b.String())
}

// newLogAuditStatsScreen builds the Statistics modal once, at
// startup — the same build-once/repopulate-on-open shape every other
// overlay in this package uses.
func (r *Root) newLogAuditStatsScreen() {
	r.logAuditStatsTitleBar = newPlainTitleBar("Statistics")

	r.logAuditStatsView = tview.NewTextView()
	r.logAuditStatsView.SetWrap(false)
	r.logAuditStatsView.SetBorderPadding(0, 0, 1, 1)
	r.logAuditStatsView.SetInputCapture(r.captureLogAuditStatsKey)

	r.logAuditStatsLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.logAuditStatsTitleBar, 1, 0, false).
		AddItem(r.logAuditStatsView, 0, 1, true)
}

// logAuditViewerVisibleEntries recomputes the viewer's own current
// Keyword/Time/Level filter over logAuditAllEntries — the same
// logAuditEntryVisible call renderLogAuditViewer makes, shared here so
// Statistics reports on exactly what the table is currently showing,
// not the whole unfiltered group.
func (r *Root) logAuditViewerVisibleEntries() []logview.Entry {
	keyword := r.logAuditKeywordField.GetText()
	timeExpr := r.logAuditTimeField.GetText()
	levelText := r.logAuditLevelField.GetText()
	now := time.Now()

	var shown []logview.Entry
	for _, e := range r.logAuditAllEntries {
		if logAuditEntryVisible(e, keyword, timeExpr, levelText, now) {
			shown = append(shown, e)
		}
	}
	return shown
}

// openLogAuditStats shows the Statistics modal for whatever the
// viewer currently matches — centered over the whole screen, the same
// shape openMessageDetail/openLogAuditDetail already establish.
func (r *Root) openLogAuditStats() {
	text := logAuditStatsText(r.logAuditViewerVisibleEntries())
	r.logAuditStatsView.SetText(text)

	width := logAuditStatsMaxWidth
	if _, _, screenWidth, _ := r.GetRect(); screenWidth > 2 && width > screenWidth-2 {
		width = screenWidth - 2
	}
	textWidth, textHeight := textSize(text)
	if textWidth > width {
		textWidth = width
	}
	height := textHeight + 1 // +1 for the title bar row
	_, _, screenWidth, screenHeight := r.GetRect()
	x, y, boxWidth, boxHeight := r.clampToScreen((screenWidth-textWidth)/2, (screenHeight-height)/2, textWidth, height)

	r.logAuditStatsLayout.SetRect(x, y, boxWidth, boxHeight)
	r.pushOverlay(logAuditStatsPage, r.logAuditStatsLayout, nil)
}

func (r *Root) closeLogAuditStats() {
	r.hideOverlay()
}

// captureLogAuditStatsKey: Escape is the only key this modal itself
// handles — same shape captureMessageDetailKey/captureLogAuditDetailKey
// already establish.
func (r *Root) captureLogAuditStatsKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeLogAuditStats()
		return nil
	}
	return event
}

// applyLogAuditStatsTheme themes the Statistics modal — split out the
// same way applyMessageDetailTheme/applyActivityLogTheme already are,
// called from applyLogAuditTheme.
func (r *Root) applyLogAuditStatsTheme(theme config.ResolvedTheme) {
	if r.logAuditStatsView == nil {
		return
	}
	r.logAuditStatsLayout.SetBackgroundColor(theme.PopupBackground)
	r.logAuditStatsView.SetBackgroundColor(theme.PopupBackground)
	r.logAuditStatsView.SetTextColor(theme.TextColor)
	r.logAuditStatsTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.logAuditStatsTitleBar.SetTextColor(theme.TextColor)
}
