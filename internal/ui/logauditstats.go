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
// its file's own mtime, see its own doc comment). entries is always
// whatever the viewer currently matches (see renderLogAuditTimeline's
// own doc comment) — every file in the open family, narrowed by
// whichever Filter/Time/Level filters are active, same as the table
// below it; never the filters' own nominal range, only the real
// min/max timestamp actually found among what matched. Returns
// ok=false when there's no meaningful span to bucket at all — fewer
// than two distinct timestamps, the same "not enough range for a
// timeline" case a single-entry or all-identical-timestamp result
// both are. severity[i] is the highest Level seen in that bucket,
// driving logAuditTimelineText's own per-bucket color. minT/maxT are
// the span's own real endpoints, for logAuditTimelineText's own start/
// end labels — per the user's own explicit report that the bar alone,
// with no indication of what time range or scale it actually spans,
// was unreadable on a sparse log (long stretches of blank buckets
// between a few real bursts, with nothing to say how long "long"
// actually was).
func logAuditTimelineBuckets(entries []logview.Entry, bucketCount int) (counts []int, severity []logview.Level, minT, maxT time.Time, ok bool) {
	if bucketCount <= 0 {
		return nil, nil, time.Time{}, time.Time{}, false
	}

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
		return nil, nil, time.Time{}, time.Time{}, false
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
	return counts, severity, minT, maxT, true
}

// logAuditBucketTimeRange returns the half-open-except-the-last-bucket
// time span bucket i covers, out of bucketCount equal-width buckets
// spanning [minT, maxT] — the same division logAuditTimelineBuckets
// itself uses to assign entries to buckets, kept here as its own pure,
// screen-free function so the interactive timeline's mouse/keyboard
// selection can turn a bucket index back into a real time range without
// duplicating that arithmetic. The last bucket's own "to" is maxT
// exactly, not the arithmetically rounded upper edge
// (minT + (i+1)*span/bucketCount) — logAuditTimelineBuckets clamps an
// entry at exactly maxT into bucket bucketCount-1 (see its own idx
// clamp), so this must agree or a selection of the last bucket would
// silently exclude the one entry that defines its own upper edge.
func logAuditBucketTimeRange(minT, maxT time.Time, bucketCount, i int) (from, to time.Time) {
	span := maxT.Sub(minT)
	from = minT.Add(time.Duration(float64(i) / float64(bucketCount) * float64(span)))
	if i == bucketCount-1 {
		return from, maxT
	}
	to = minT.Add(time.Duration(float64(i+1) / float64(bucketCount) * float64(span)))
	return from, to
}

// logAuditTimelineLabel renders t for the timeline's own start/end
// labels — just the time ("15:04:05") when both endpoints fall on the
// same calendar day (the common case: following one service's own
// recent activity), the full date too ("2006-01-02 15:04:05", the
// same layout logAuditTimeText uses for the table itself) once the
// span crosses a day boundary, where the bare time alone would be
// ambiguous about which day it belongs to.
func logAuditTimelineLabel(t time.Time, sameDay bool) string {
	if sameDay {
		return t.Format("15:04:05")
	}
	return t.Format("2006-01-02 15:04:05")
}

// logAuditTimelineText renders entries' own time distribution as one
// line of density blocks between its own start/end time labels (see
// logAuditTimelineLabel) — per the user's own explicit report that the
// bar alone said nothing about what time range or scale it covered,
// confusing on a sparse log where most of the width is blank between
// a few real bursts. Colored per bucket — red if that bucket contains
// an Error/Fatal, the app's own WarningText if a Warn (and nothing
// worse), otherwise plain text color — "" when logAuditTimelineBuckets
// itself has nothing meaningful to show (renderLogAuditTimeline then
// leaves the line blank rather than drawing an empty, misleadingly
// flat bar).
func logAuditTimelineText(entries []logview.Entry, width int, theme config.ResolvedTheme) string {
	counts, severity, startLabel, endLabel, _, _, _, _, ok := logAuditTimelineLayout(entries, width)
	if !ok {
		return ""
	}
	return logAuditTimelineBuildText(counts, severity, startLabel, endLabel, theme, -1, -1)
}

// logAuditTimelineLayout is the entries-scanning half of what used to
// be one function: finding the real time span, picking a label format,
// and bucketing — everything up to (but not including) building the
// final colored string. Split out so the interactive timeline can
// re-render a selection change against the SAME already-computed
// counts/severity (via logAuditTimelineBuildText below) without
// re-scanning entries — load-bearing for renderLogAuditTimeline's own
// freeze-while-focused behavior (root.go's own doc comment on
// logAuditTimelineFocused): the entries scan is what gets frozen, not
// the text-building step, so a moved cursor still repaints instantly.
// barCol is the bar's own first column relative to this view's inner
// rect (len(startLabel) plus the one separating space) — persisted by
// the caller for mouse hit-testing, never independently recomputed
// from the final string (see that caller's own doc comment on why).
func logAuditTimelineLayout(entries []logview.Entry, width int) (counts []int, severity []logview.Level, startLabel, endLabel string, barCol, bucketCount int, minT, maxT time.Time, ok bool) {
	if width < 10 {
		width = 10
	}
	if width > 200 {
		width = 200
	}

	// A first pass just to find the real span, before committing to a
	// label format or a final bucket count — logAuditTimelineBuckets
	// itself is cheap (one pass over entries), and the label width has
	// to be known before the second, real call below can know how many
	// buckets actually fit in what's left of width.
	_, _, minT, maxT, ok = logAuditTimelineBuckets(entries, 1)
	if !ok {
		return nil, nil, "", "", 0, 0, time.Time{}, time.Time{}, false
	}
	sameDay := minT.Year() == maxT.Year() && minT.YearDay() == maxT.YearDay()
	startLabel = logAuditTimelineLabel(minT, sameDay)
	endLabel = logAuditTimelineLabel(maxT, sameDay)

	barWidth := width - len(startLabel) - len(endLabel) - 2 // 2 separating spaces
	if barWidth < 10 {
		// Too narrow for both labels and a readable bar — the labels
		// are what actually answers the user's own "what scale is
		// this" question, so they win; the bar still renders, just
		// below the floor logAuditTimelineBuckets itself also enforces
		// (the same 10 newLogAuditViewerScreen's own first call uses).
		barWidth = 10
	}

	counts, severity, minT, maxT, ok = logAuditTimelineBuckets(entries, barWidth)
	if !ok {
		return nil, nil, "", "", 0, 0, time.Time{}, time.Time{}, false
	}

	maxCount := 0
	for _, c := range counts {
		if c > maxCount {
			maxCount = c
		}
	}
	if maxCount == 0 {
		return nil, nil, "", "", 0, 0, time.Time{}, time.Time{}, false
	}

	return counts, severity, startLabel, endLabel, len(startLabel) + 1, len(counts), minT, maxT, true
}

// logAuditTimelineBuildText is the pure text-building half (see
// logAuditTimelineLayout's own doc comment on the split): turns
// already-computed buckets into the final colored string, optionally
// highlighting an inclusive selStart..selEnd bucket range (either
// negative means "no selection"). The highlight composes with each
// bucket's own existing severity color via the "::r" reverse-video
// attribute — already an established tag idiom in this codebase (see
// gitbreachdiffsplit.go's own "[%s::b]") — rather than replacing the
// foreground color outright, so a selected Error/Fatal bucket doesn't
// lose the very severity signal the timeline exists to show.
func logAuditTimelineBuildText(counts []int, severity []logview.Level, startLabel, endLabel string, theme config.ResolvedTheme, selStart, selEnd int) string {
	if len(counts) == 0 {
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
	fmt.Fprintf(&b, "[%s]%s [-:-:-]", colorTag(theme.MutedTextColor), startLabel)
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
		attr := ""
		if selStart >= 0 && i >= selStart && i <= selEnd {
			attr = "::r"
		}
		fmt.Fprintf(&b, "[%s%s]%c", colorTag(color), attr, timelineBlocks[level])
	}
	fmt.Fprintf(&b, "[%s] %s[-:-:-]", colorTag(theme.MutedTextColor), endLabel)
	return b.String()
}

// logAuditTimelineUnlaidOutWidth is tview.Box's own default width
// (verified directly against NewBox in box.go, not assumed) — what
// logAuditTimelineView.GetRect() still reports the very first time
// renderLogAuditTimeline runs for a freshly-opened group, since
// openLogAuditViewer calls renderLogAuditViewer (and so this) before
// pushOverlay, and pushOverlay alone never cascades a real rect down
// to a child either — only an actual Draw() does (the same "SetRect
// alone never cascades, only Draw() does" caveat this package's own
// chmoddialog.go doc comment already established for tview.Pages).
// A plain "width <= 0" check (what this used to be) never catches
// this: 15 is a real, positive, but entirely wrong number for an
// 80+-column terminal, a real, user-reported bug — the family's own
// first-ever timeline render came out barely 15 columns wide,
// contradicting the width of a real room or the screen it's on, then
// correcting itself (needlessly, confusingly differently from the
// first look) the moment literally anything else re-rendered it after
// the view's first real Draw() had already happened. See
// nameColumnWidth's own doc comment (panel.go) for the same class of
// "falls back to a generous estimate before the first real draw"
// problem, solved there with an explicitly-tracked layout field
// instead — not practical to copy verbatim here since this bar has no
// sibling columns of its own to size against, only the screen itself.
const logAuditTimelineUnlaidOutWidth = 15

// renderLogAuditTimeline fills the timeline bar from entries (the
// viewer's own currently-matching set — see renderLogAuditViewer) —
// the bar's own real width if it's been drawn at least once (anything
// past logAuditTimelineUnlaidOutWidth — see its own doc comment), else
// the whole screen's width as a reasonable first guess.
func (r *Root) renderLogAuditTimeline(entries []logview.Entry) {
	if r.logAuditTimelineFocused || r.logAuditTimelineDragging {
		// Frozen. The only way this runs while the timeline itself
		// holds focus or a mouse drag is in progress is a follow-mode
		// tick (renderLogAuditViewer's own caller) — never a filter
		// field's ChangedFunc, since typing into a field requires that
		// field, not the timeline, to have focus. Re-bucketing against
		// freshly tailed entries here would shift the buckets out from
		// under an in-progress selection; the table (renderLogAuditViewer's
		// other half) keeps updating live regardless, only this one
		// line visually freezes until focus leaves or the drag ends —
		// see commitLogAuditTimelineSelection's own doc comment on why
		// a commit itself is never blocked by this (it moves focus off
		// the timeline first).
		return
	}

	_, _, width, _ := r.logAuditTimelineView.GetRect()
	if width <= logAuditTimelineUnlaidOutWidth {
		if _, _, screenWidth, _ := r.GetRect(); screenWidth > 4 {
			width = screenWidth - 4
		} else {
			width = 60
		}
	}

	counts, severity, startLabel, endLabel, barCol, bucketCount, minT, maxT, ok := logAuditTimelineLayout(entries, width)
	if !ok {
		r.logAuditTimelineCounts = nil
		r.logAuditTimelineSeverity = nil
		r.logAuditTimelineBucketCount = 0
		r.logAuditTimelineView.SetText("")
		return
	}

	if bucketCount != r.logAuditTimelineBucketCount || !minT.Equal(r.logAuditTimelineMinT) || !maxT.Equal(r.logAuditTimelineMaxT) {
		// Geometry changed since the last render (new filter, resize,
		// or simply the first render ever) — any previously selected
		// bucket index may no longer even exist at this size. Reset
		// rather than clamp: a clamped-but-silently-different selection
		// after a commit would be harder to make sense of than no
		// selection at all.
		r.logAuditTimelineAnchor = 0
		r.logAuditTimelineCursor = 0
	}
	r.logAuditTimelineCounts = counts
	r.logAuditTimelineSeverity = severity
	r.logAuditTimelineStartLabel = startLabel
	r.logAuditTimelineEndLabel = endLabel
	r.logAuditTimelineBarCol = barCol
	r.logAuditTimelineBucketCount = bucketCount
	r.logAuditTimelineMinT = minT
	r.logAuditTimelineMaxT = maxT

	r.repaintLogAuditTimeline()
}

// repaintLogAuditTimeline redraws the timeline from the buckets
// renderLogAuditTimeline last actually computed, plus whatever the
// current selection is — no entries re-scan, safe (and meant) to call
// on every keyboard/mouse selection change, including while frozen
// (see renderLogAuditTimeline's own doc comment): moving the cursor
// must never itself be what silently un-freezes a frozen timeline.
func (r *Root) repaintLogAuditTimeline() {
	selStart, selEnd := -1, -1
	if r.logAuditTimelineFocused || r.logAuditTimelineDragging {
		selStart = min(r.logAuditTimelineAnchor, r.logAuditTimelineCursor)
		selEnd = max(r.logAuditTimelineAnchor, r.logAuditTimelineCursor)
	}
	text := logAuditTimelineBuildText(r.logAuditTimelineCounts, r.logAuditTimelineSeverity, r.logAuditTimelineStartLabel, r.logAuditTimelineEndLabel, r.theme, selStart, selEnd)
	r.logAuditTimelineView.SetText(text)
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

	// PopupBackground, matching logAuditStatsView's own static background
	// (see applyLogAuditStatsTheme) — never focus-dependent the way Git
	// Breach's own boxes are, so a plain closure rather than the
	// scrollbar's own PanelBackground default is all this needs.
	popupTrackColor := func() tcell.Color { return r.theme.PopupBackground }

	r.logAuditStatsScrollbar = newVerticalScrollbarForTextView(r, r.logAuditStatsView)
	r.logAuditStatsScrollbar.EnableTextDensityMinimap() // logAuditStatsView is SetWrap(false)
	r.logAuditStatsScrollbar.SetTrackColorFunc(popupTrackColor)
	topRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.logAuditStatsView, 0, 1, true).
		AddItem(r.logAuditStatsScrollbar, 1, 0, false)
	topRow.SetMouseCapture(r.logAuditStatsScrollbar.captureMouse)

	// The horizontal counterpart, same corner handling as Compare's own
	// diff view (comparediff.go): a reserved spacer column under the
	// vertical bar, so the two bars' rects never share a cell.
	r.logAuditStatsHScrollbar = newHorizontalScrollbarForTextView(r, r.logAuditStatsView)
	r.logAuditStatsHScrollbar.SetTrackColorFunc(popupTrackColor)
	r.logAuditStatsSpacer = tview.NewBox().SetBackgroundColor(r.theme.PopupBackground)
	hBarRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.logAuditStatsHScrollbar, 0, 1, false).
		AddItem(r.logAuditStatsSpacer, 1, 0, false)
	hBarRow.SetMouseCapture(r.logAuditStatsHScrollbar.captureMouse)

	contentRow := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(topRow, 0, 1, true).
		AddItem(hBarRow, 1, 0, false)

	r.logAuditStatsLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.logAuditStatsTitleBar, 1, 0, false).
		AddItem(contentRow, 0, 1, true)
}

// logAuditViewerVisibleEntries recomputes the viewer's own current
// Keyword/Time/Level filter over logAuditAllEntries — the same
// logAuditEntryVisible call renderLogAuditViewer makes, shared here so
// Statistics reports on exactly what the table is currently showing,
// not the whole unfiltered group.
func (r *Root) logAuditViewerVisibleEntries() []logview.Entry {
	query, hasQuery := logAuditKeywordQuery(r.logAuditKeywordField.GetText())
	timeExpr := r.logAuditTimeField.GetText()
	levelText := r.logAuditLevelField.GetText()
	now := time.Now()

	var shown []logview.Entry
	for _, e := range r.logAuditAllEntries {
		if logAuditEntryVisible(e, query, hasQuery, timeExpr, levelText, now) {
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
