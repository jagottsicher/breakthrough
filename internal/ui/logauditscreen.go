package ui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/filterexpr"
	"github.com/jagottsicher/breakthrough/internal/logview"
)

// The Log Audit screen ("jL"): two full-screen catalog layers stacked
// via pushOverlay (see logaudit.go's own doc comment on
// log.SelectionLayout/log.ViewerLayout), plus a small detail
// modal, the exact same three-piece shape Messages/messagedetail.go
// already establishes for an overlay-with-its-own-detail-view.
//
// Unlike every format/merge concern (internal/logview), everything in
// this file is screen-specific glue: turning Entry values into table
// rows, building the keyword/level highlighting, and the keymap each
// of the three pieces needs — the same split activitylogscreen.go
// already keeps between itself and internal/activitylog.

const (
	logAuditSelectionPage = "logaudit-select"
	logAuditViewerPage    = "logaudit-viewer"
	logAuditDetailPage    = "logaudit-detail"
)

// logAuditState holds the Log Audit screen's own three stacked layers
// (selection, viewer, detail modal) and the Stats overlay — their
// widgets, the timeline's own interaction state, and the last
// read/parsed result (see logaudit.go/logauditscreen.go/
// logauditstats.go).
type logAuditState struct {
	SelectionLayout     *tview.Flex
	TitleBar            *tview.TextView
	Table               *tview.Table
	Scrollbar           *verticalScrollbar
	Hint                *tview.TextView
	HintSpans           []listHintSpan
	Dir                 string
	Groups              []logview.FileGroup
	DiscoverErr         error
	ViewerLayout        *tview.Flex
	ViewerTitle         *tview.TextView
	KeywordField        *tview.InputField
	TimeField           *tview.InputField
	LevelField          *tview.InputField
	FilterRow           *tview.Flex
	TimelineView        *tview.TextView
	TimelineBarCol      int
	TimelineBucketCount int
	TimelineMinT        time.Time
	TimelineMaxT        time.Time
	TimelineCounts      []int
	TimelineSeverity    []logview.Level
	TimelineStartLabel  string
	TimelineEndLabel    string
	TimelineFocused     bool
	TimelineAnchor      int
	TimelineCursor      int
	TimelineDragging    bool
	TimeFieldFocused    bool
	FocusIndicator      *tview.TextView
	ViewerTable         *tview.Table
	ViewerScrollbar     *verticalScrollbar
	ViewerHint          *tview.TextView
	ViewerSpans         []listHintSpan
	GroupOpen           logview.FileGroup // the group the viewer is currently showing — see reopenLogAuditViewer
	AllEntries          []logview.Entry
	// ErrCount/WarnCount are AllEntries' own Error+Fatal/Warn counts —
	// filter-independent (every entry counts, regardless of whether it
	// currently matches Keyword/Time/Level), so computed once by
	// recomputeLogAuditCombined whenever AllEntries itself changes
	// (logaudit.go), not recomputed on every keystroke the way they
	// used to be inside logAuditVisibleEntries' own scan loop — a real
	// cost on a multi-million-entry family, confirmed by benchmark (see
	// that function's own doc comment).
	ErrCount  int
	WarnCount int
	// FilterScanToken increments on every scheduleLogAuditFilterRescan
	// call — its own captured value is what the background scan's
	// result callback compares against before ever applying, the same
	// per-request staleness guard loadToken/diskUsageForPath/
	// viewerLookDownloadPath already establish elsewhere in this app
	// (see scheduleLogAuditFilterRescan's own doc comment).
	FilterScanToken int
	// FilterScanInFlight/FilterScanPending coalesce rapid typing into
	// at most one running scan at a time — see
	// scheduleLogAuditFilterRescan/runLogAuditFilterScan's own doc
	// comments. Without this, every keystroke spawned its own
	// unbounded O(n) scan regardless of whether an earlier one was
	// still running, the real cost behind "stalls before the last file
	// is even loaded, once a keyword search is already running".
	FilterScanInFlight bool
	FilterScanPending  bool
	// ScanFn is how scheduleLogAuditFilterRescan's own background scan
	// actually runs — nil falls back to the real implementation
	// (defaultLogAuditScanFn: safeGo + QueueUpdateDraw), the same
	// "Root owns concurrency" shape Panel.onFetchEntries establishes
	// for remote navigation, just lazily defaulted here instead of
	// wired once in NewRoot, since this one has no construction-order
	// dependency to respect. Overridable in tests so a test can make
	// the scan resolve synchronously (syncLogAuditScanFn) or capture
	// its own completion callback to fire on demand
	// (controlledLogAuditScanFn) — tests never drain QueueUpdateDraw
	// (this package's own established convention), so the real
	// implementation's result would otherwise never be observable at
	// all, not just delayed.
	ScanFn            func(entries []logview.Entry, filters logAuditParsedFilters, done func([]logview.Entry))
	Files             int
	Skipped           int
	ParseErr          error
	NewestFirst       bool
	Following         bool
	FollowCancel      context.CancelFunc
	DetailTitleBar    *tview.TextView
	DetailView        *tview.TextView
	DetailLayout      *tview.Flex
	StatsTitleBar     *tview.TextView
	StatsView         *tview.TextView
	StatsScrollbar    *verticalScrollbar
	StatsHScrollbar   *horizontalScrollbar
	StatsSpacer       *tview.Box
	StatsLayout       *tview.Flex
	DetailDragging    bool
	DetailDragOffsetX int
	DetailDragOffsetY int

	// CurrentEntries/CurrentFiles/CurrentSkipped/CurrentErr hold
	// GroupOpen's own current (first, unrotated) file's last read
	// result — refreshLogAuditCurrentFile's own output, re-run on open
	// and on "r". RotatedEntries/RotatedFiles/RotatedSkipped/RotatedErr
	// are the same for every rotated file (Files[1:]) read so far —
	// filled in once, progressively, by startLogAuditProgressiveLoad,
	// and never touched by a refresh, since a rotated file never
	// changes once logrotate has renamed it away from the family's own
	// bare name. recomputeLogAuditCombined merges both halves into
	// AllEntries/Files/Skipped/ParseErr above, the same fields every
	// other reader (filtering, the title bar, Machiavelli Mode's own
	// context snapshot, ...) already uses without needing to know this
	// split exists at all.
	CurrentEntries []logview.Entry
	CurrentFiles   int
	CurrentSkipped int
	CurrentErr     error
	// CurrentErrCount/WarnCount and RotatedErrCount/WarnCount are
	// countLogAuditErrWarn's own per-batch tallies for CurrentEntries
	// and RotatedEntries respectively — recomputeLogAuditCombined just
	// adds the two pairs together for ErrCount/WarnCount above, instead
	// of walking the full AllEntries on every call (see
	// applyLogAuditRotatedFile's own doc comment for why that mattered).
	CurrentErrCount  int
	CurrentWarnCount int
	RotatedEntries   []logview.Entry
	RotatedFiles     int
	RotatedSkipped   int
	RotatedErr       error
	RotatedErrCount  int
	RotatedWarnCount int
	// LastCombineAt/CombineDirty back applyLogAuditRotatedFile's own
	// throttle (logaudit.go) — LastCombineAt is when AllEntries was
	// last actually recombined/re-rendered during a progressive load,
	// CombineDirty is whether a rotated file has landed since then that
	// still needs to be folded in (flushed by finishLogAuditProgressiveLoad
	// if the throttle ends before one more catch-up tick fires).
	LastCombineAt time.Time
	CombineDirty  bool
	// RotatedCache is session-only (never written to disk, cleared
	// only by the app exiting), keyed by logAuditCacheKey — a rotated
	// file never changes once written, so a cache hit is always
	// correct. Budgeted by settings.LogAuditCacheSize (bytes, not file
	// count — see logview.RotatedCache's own doc comment for why: a
	// single rotated file can be gigabytes, and keeping every family
	// ever opened this session forever used to grow without bound).
	// Read and written only on the UI goroutine (the pre-pass in
	// startLogAuditProgressiveLoad, and applyLogAuditRotatedFile's own
	// QueueUpdateDraw callback) — the background loader goroutine
	// itself never touches it, the same "never touch a Root field
	// concurrently with the UI goroutine" rule startLogAuditFollow's
	// own doc comment already states for the identical reason.
	RotatedCache *logview.RotatedCache
	// TotalFiles is len(group.Files) as Discover found it when the
	// viewer opened — known upfront, never changes for this one open,
	// used only for the "x/y files" loading segment in the title bar
	// (see renderLogAuditViewer).
	TotalFiles int
	// LoadingMore/LoadCancel/LoadAnimFrame back
	// startLogAuditProgressiveLoad's own in-progress state — the same
	// three-field shape Following/FollowCancel plus hashInProgress/
	// hashCancel/hashAnimFrame already establish for an equivalent
	// cancelable background job with its own animation.
	LoadingMore   bool
	LoadCancel    context.CancelFunc
	LoadAnimFrame int
	// ViewerTitleTemplate is the last full title string built by
	// renderLogAuditViewer, with logAuditLoadGlyphPlaceholder still in
	// place of the animated glyph — empty whenever nothing is loading —
	// so animateLogAuditLoad's own ticker can repaint just the glyph
	// every tick without re-scanning every entry the way a full
	// renderLogAuditViewer call would (see its own doc comment).
	ViewerTitleTemplate string
}

const (
	logAuditSelName = iota
	logAuditSelInfo
	// Size/Modified describe the family's own newest file (see
	// logview.FileGroup.Newest) — per the user's own explicit request
	// to see those without opening the group first.
	logAuditSelSize
	logAuditSelModified
)

const (
	logAuditColTime = iota
	logAuditColLevel
	logAuditColSource
	logAuditColMessage
)

// logAuditDetailMaxWidth mirrors messagesDetailMaxWidth's own reasoning
// — a centered, screen-sized modal for one entry's full text.
const logAuditDetailMaxWidth = 90

func logAuditSelectionHintEntries() []listHintEntry {
	return []listHintEntry{
		hintKey("Enter", "open log audit", func(r *Root) { r.openLogAuditViewer() }),
		hintKey("r", "re-scan directory", func(r *Root) { r.reloadLogAuditDiscovery() }),
		hintKey("Esc", "close", func(r *Root) { r.closeLogAuditSelection() }),
	}
}

// logAuditViewerHintEntries: "e"/"w"/"Enter"/"r"/"f"/"s" are all bound
// on the table's own InputCapture (see
// newLogAuditViewerScreen/captureLogAuditViewerTableKey) — every one
// of them fires only while the list itself has focus, typed into a
// filter field otherwise. Rather than repeat "(while the list has
// focus)" on each of those six entries (what this used to do — per
// the user's own explicit report, both too long and inconsistently
// applied, since "e"/"w" never got the suffix either even though the
// same rule already covered them too), renderLogAuditViewer says this
// once, in the title bar, covering the whole group together.
func logAuditViewerHintEntries() []listHintEntry {
	return []listHintEntry{
		hintKey("Tab", "next field", simulateKeyOnFocused(tcell.KeyTab)),
		hintKey("e", "next error", func(r *Root) { r.navigateLogAuditLevel(logview.LevelError) }),
		hintKey("w", "next warning", func(r *Root) { r.navigateLogAuditLevel(logview.LevelWarn) }),
		hintKey("Enter", "details", func(r *Root) {
			row, _ := r.log.ViewerTable.GetSelection()
			r.openLogAuditDetail(row)
		}),
		hintKey("r", "re-read files", func(r *Root) { r.reopenLogAuditViewer() }),
		hintKey("f", "toggle follow", func(r *Root) { r.toggleLogAuditFollow() }),
		hintKey("s", "statistics", func(r *Root) { r.openLogAuditStats() }),
		hintKey("Esc", "back to file selection", func(r *Root) { r.closeLogAuditViewer() }),
	}
}

// newLogAuditScreen builds every piece of the Log Audit screen once, at
// startup — the same build-once/repopulate-on-open shape every other
// full-screen catalog in this package uses.
func (r *Root) newLogAuditScreen() {
	r.newLogAuditSelectionScreen()
	r.newLogAuditViewerScreen()
	r.newLogAuditDetailScreen()
	r.newLogAuditStatsScreen()
}

func (r *Root) newLogAuditSelectionScreen() {
	r.log.TitleBar = newPlainTitleBar("Log Holepunch — select files")
	// A clickable "✕" in the title bar's own top-right corner, per the
	// user's own explicit request — see closeTitleBarButtonCol/
	// renderCloseTitleBar/captureCloseTitleBarMouse (root.go). The
	// glyph itself is drawn by renderLogAuditSelection, every time it
	// renders (the same "render it fresh each time" shape
	// renderReloadTitleBar's own callers already use).
	r.log.TitleBar.SetMouseCapture(captureReloadCloseTitleBarMouse(r.log.TitleBar, r.reloadLogAuditDiscovery, r.closeLogAuditSelection))

	r.log.Table = tview.NewTable()
	r.log.Table.SetBorders(false)
	r.log.Table.SetBorderPadding(1, 0, 2, 1)
	r.log.Table.SetSelectable(true, false)
	r.log.Table.SetFixed(1, 0)
	r.log.Table.SetInputCapture(r.captureLogAuditSelectionTableKey)
	r.log.Table.SetSelectedFunc(func(row, col int) { r.openLogAuditViewer() })

	r.log.Hint = tview.NewTextView()
	r.log.Hint.SetWrap(false)
	r.log.Hint.SetDynamicColors(true)
	hintText, hintSpans := buildListHint(r.theme, logAuditSelectionHintEntries())
	r.log.Hint.SetText(hintText)
	r.log.HintSpans = hintSpans
	r.log.Hint.SetMouseCapture(r.captureListHintMouse(r.log.Hint, &r.log.HintSpans))

	r.log.Scrollbar = newVerticalScrollbarForTable(r, r.log.Table)
	logAuditTableRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.log.Table, 0, 1, true).
		AddItem(r.log.Scrollbar, 1, 0, false)
	logAuditTableRow.SetMouseCapture(chainTableDoubleClick(r.log.Table, r.log.Scrollbar.captureMouse))

	r.log.SelectionLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.log.TitleBar, 1, 0, false).
		AddItem(logAuditTableRow, 0, 1, true).
		AddItem(r.log.Hint, 1, 0, false)
}

func (r *Root) newLogAuditViewerScreen() {
	r.log.ViewerTitle = newPlainTitleBar("Log Holepunch")
	// Same clickable "✕" as the selection screen's own title bar (see
	// its own comment on this, just above in
	// newLogAuditSelectionScreen) — drawn by renderLogAuditViewer
	// itself, since this bar's own text is rebuilt fresh on every
	// render anyway (the running counts).
	r.log.ViewerTitle.SetMouseCapture(captureReloadCloseTitleBarMouse(r.log.ViewerTitle, r.reopenLogAuditViewer, r.closeLogAuditViewer))

	r.log.KeywordField = tview.NewInputField()
	r.log.KeywordField.SetLabel("Filter: ")
	// Placeholder, not a parenthetical in the label — per the user's
	// own explicit request to move every field's own example into the
	// field itself (shown only while it's actually empty), the same
	// row Time/Level's own "(e.g. ...)" labels used to carry instead.
	r.log.KeywordField.SetPlaceholder(`keyword, keyword1 AND (keyword2 OR keyword3)`)
	// logAuditKeywordQuery's own "ok is false for a blank field or an
	// unparseable expression" contract means an unbalanced-parens
	// typo, say, used to be accepted with no visible sign anything was
	// wrong — silently matching every entry instead of narrowing them,
	// the same class of silent failure the Time field's own label
	// color already addresses — per the user's own explicit follow-up
	// request to extend it here too.
	r.log.KeywordField.SetChangedFunc(func(string) {
		r.scheduleLogAuditFilterRescan()
		r.restyleLogAuditKeywordFieldLabel()
	})
	r.log.KeywordField.SetDoneFunc(func(key tcell.Key) {
		// Keyword is the first stop in this screen's own field chain —
		// Backtab from here is the backward edge into the AI
		// Consigliere sidebar, same reasoning
		// captureLogAuditViewerTableKey's own forward-edge case (Tab,
		// below) gives. Checked here, ahead of the ordinary
		// logAuditViewerFieldDone call, so its own ordinary
		// prev (log.ViewerTable) is only ever reached once the
		// sidebar isn't open.
		if key == tcell.KeyBacktab && r.consigliere.Visible {
			if r.consigliere.Scrollbar.maxOffset() > 0 {
				r.app.SetFocus(r.consigliere.History)
			} else {
				r.app.SetFocus(r.consigliere.Input)
			}
			return
		}
		r.logAuditViewerFieldDone(key, r.log.TimeField, r.log.ViewerTable)
	})
	r.log.KeywordField.SetFocusFunc(func() { r.restyleLogAuditFocusTargets(r.log.KeywordField) })

	// Time reuses filterexpr.ParseMtime verbatim (see
	// logAuditEntryVisible) — the exact same "before"/"after"/
	// "between ... and ..."/relative grammar the panel's own
	// Modified-time filter and the Action Log's own Time field
	// already accept, rather than a third date grammar for what is,
	// structurally, the same kind of question.
	r.log.TimeField = tview.NewInputField()
	r.log.TimeField.SetLabel("Time: ")
	// A third example covering "between", per the user's own explicit
	// request — filterexpr.ParseMtime's own grammar (see
	// logAuditEntryVisible's doc comment above) needs exactly two
	// moments joined by "and", unlike "last"/"after" which take just
	// one.
	r.log.TimeField.SetPlaceholder(`last 7 days, after 2026-09-01, between 2026-09-01 and 2026-09-30`)
	// Deliberately NOT a live SetChangedFunc the way Keyword/Level have
	// — per the user's own explicit report, re-bucketing the timeline
	// (and re-filtering the table) on every single keystroke made
	// typing into this field noticeably sluggish, unlike a keyword
	// substring match or a level-threshold compare, both cheap per
	// character. A date/time expression only means anything once it's
	// finished anyway (a half-typed "between 2026-09-0" doesn't parse),
	// so applying it on Enter/Tab/Backtab (SetDoneFunc below) or
	// whenever focus moves on to something else entirely (see
	// restyleLogAuditFocusTargets's own "focused != log.TimeField"
	// render) is both cheaper and more correct than live re-filtering
	// ever was here.
	r.log.TimeField.SetDoneFunc(func(key tcell.Key) {
		r.renderLogAuditViewer()
		r.logAuditViewerFieldDone(key, r.log.LevelField, r.log.KeywordField)
	})
	r.log.TimeField.SetFocusFunc(func() { r.restyleLogAuditFocusTargets(r.log.TimeField) })
	// Unlike the rest of this field's own live behavior (see the
	// "Deliberately NOT a live SetChangedFunc" comment just above),
	// this one check is cheap regardless of how many entries are
	// loaded — a parse attempt against the typed text alone, never a
	// re-filter or re-bucket — so it runs on every keystroke without
	// reintroducing the sluggishness that comment describes. Per the
	// user's own explicit request: an expression that doesn't parse
	// (e.g. "between ... and 2026-10-26 29:59:59" — not a valid hour)
	// used to be accepted with no visible sign anything was wrong at
	// all, silently matching every entry instead of narrowing them;
	// the label's own color is now that sign.
	r.log.TimeField.SetChangedFunc(func(string) { r.restyleLogAuditTimeFieldLabel() })

	// Level is a *minimum* severity ("warn" shows WARN and everything
	// above it — ERROR, FATAL — not only exact WARN matches), the same
	// "t Zeitfilter"/severity-threshold idea the original mockup asked
	// for: a quick "show me the bad stuff" narrowing, not a precise
	// equality filter nobody actually wants while triaging.
	r.log.LevelField = tview.NewInputField()
	r.log.LevelField.SetLabel("Level >= ")
	r.log.LevelField.SetPlaceholder("warn")
	// logAuditMinLevel's own "ok is false for a blank field or an
	// unrecognized level name" contract means a typo ("wrn") used to
	// be accepted with no visible sign anything was wrong — silently
	// matching every entry instead of narrowing them, the same class
	// of silent failure the Time field's own label color already
	// addresses — per the user's own explicit follow-up request to
	// extend it here too.
	r.log.LevelField.SetChangedFunc(func(string) {
		r.scheduleLogAuditFilterRescan()
		r.restyleLogAuditLevelFieldLabel()
	})
	r.log.LevelField.SetDoneFunc(func(key tcell.Key) {
		// next is the timeline, not the table directly — see
		// captureLogAuditTimelineKey's own doc comment on the Tab
		// chain this is now one link of.
		r.logAuditViewerFieldDone(key, r.log.TimelineView, r.log.TimeField)
	})
	r.log.LevelField.SetFocusFunc(func() { r.restyleLogAuditFocusTargets(r.log.LevelField) })

	r.log.FilterRow = tview.NewFlex().
		AddItem(r.log.KeywordField, 0, 2, true).
		AddItem(r.log.TimeField, 0, 2, false).
		AddItem(r.log.LevelField, 0, 1, false)

	r.log.TimelineView = tview.NewTextView()
	r.log.TimelineView.SetWrap(false)
	r.log.TimelineView.SetDynamicColors(true)
	r.log.TimelineView.SetInputCapture(r.captureLogAuditTimelineKey)
	r.log.TimelineView.SetMouseCapture(r.captureLogAuditTimelineMouse)
	// Can't route this one through restyleLogAuditFocusTargets:
	// *tview.TextView.Focus (verified directly against tview's own
	// textview.go) does "t.Lock(); defer t.Unlock()" *before* invoking
	// this very FocusFunc, and repaintLogAuditTimeline calls SetText on
	// this same TextView, which also locks it — a real, confirmed
	// self-deadlock on tview's own non-reentrant mutex, not a
	// hypothetical one (it hung TestLogAuditTimelineArrowKeysMoveAndCollapseSelection
	// outright before this was found). SetBackgroundColor is safe here
	// (Box's own override, never locks), so the focused look still
	// applies immediately; the selection highlight itself first appears
	// on whichever key or mouse event comes next — both already call
	// repaintLogAuditTimeline themselves, safely, from outside this
	// lock. restyleLogAuditFocusTargets itself stays the right function
	// for every other caller (the three fields, the table, and
	// applyLogAuditTheme's own refresh), none of which call back into
	// the same widget they're styling.
	r.log.TimelineView.SetFocusFunc(func() {
		// log.TimelineFocused = true comes FIRST, deliberately,
		// before the possible renderLogAuditViewer call below: that
		// render's own freeze-while-focused guard (renderLogAuditTimeline's
		// own doc comment) is what keeps it from calling SetText on this
		// same TextView from inside this very Focus() callback — the
		// identical self-deadlock the rest of this FocusFunc already
		// works around by skipping repaintLogAuditTimeline entirely.
		// Setting it after would let that one render slip through
		// unfrozen and hit the same lock. The table still updates
		// either way (renderLogAuditViewer's other half, a different
		// widget); only this one widget's own repaint stays deferred to
		// the next key or mouse event, same as always while frozen.
		r.log.TimelineFocused = true
		// Same timeFieldWasFocused check restyleLogAuditFocusTargets
		// itself does (see its own doc comment) — needed here too since
		// a mouse click can jump straight from the Time field to the
		// Timeline, skipping Level (the only stop restyleLogAuditFocusTargets
		// would otherwise have caught this transition through).
		if r.log.TimeFieldFocused {
			r.log.TimeFieldFocused = false
			r.renderLogAuditViewer()
		}
		r.log.TimelineView.SetBackgroundColor(r.theme.InputFocusedBackground)
		for _, f := range r.logAuditFilterFields() {
			styleInput(f, r.theme, false)
		}
		r.renderLogAuditFocusIndicator(false)
	})

	// log.FocusIndicator takes over the row the table's own top
	// border padding used to leave blank (padding dropped to 0 below) —
	// per the user's own explicit request to turn that previously-empty
	// space into a "the list has focus" indicator, rather than leaving
	// it an easy-to-miss cue buried in which row happens to be
	// highlighted.
	r.log.FocusIndicator = tview.NewTextView()
	r.log.FocusIndicator.SetWrap(false)

	r.log.ViewerTable = tview.NewTable()
	r.log.ViewerTable.SetBorders(false)
	r.log.ViewerTable.SetBorderPadding(0, 0, 2, 1)
	r.log.ViewerTable.SetSelectable(true, false)
	r.log.ViewerTable.SetFixed(1, 0)
	r.log.ViewerTable.SetInputCapture(r.captureLogAuditViewerTableKey)
	r.log.ViewerTable.SetSelectedFunc(func(row, col int) { r.openLogAuditDetail(row) })
	r.log.ViewerTable.SetFocusFunc(func() { r.restyleLogAuditFocusTargets(r.log.ViewerTable) })

	r.log.ViewerHint = tview.NewTextView()
	r.log.ViewerHint.SetWrap(false)
	r.log.ViewerHint.SetDynamicColors(true)
	hintText, hintSpans := buildListHint(r.theme, logAuditViewerHintEntries())
	r.log.ViewerHint.SetText(hintText)
	r.log.ViewerSpans = hintSpans
	r.log.ViewerHint.SetMouseCapture(r.captureListHintMouse(r.log.ViewerHint, &r.log.ViewerSpans))

	r.log.ViewerScrollbar = newVerticalScrollbarForTable(r, r.log.ViewerTable)
	logAuditViewerTableRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.log.ViewerTable, 0, 1, true).
		AddItem(r.log.ViewerScrollbar, 1, 0, false)
	logAuditViewerTableRow.SetMouseCapture(chainTableDoubleClick(r.log.ViewerTable, r.log.ViewerScrollbar.captureMouse))

	r.log.ViewerLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.log.ViewerTitle, 1, 0, false).
		AddItem(r.log.FilterRow, 1, 0, false).
		AddItem(r.log.TimelineView, 1, 0, false).
		AddItem(r.log.FocusIndicator, 1, 0, false).
		AddItem(logAuditViewerTableRow, 0, 1, true).
		AddItem(r.log.ViewerHint, 1, 0, false)
}

func (r *Root) newLogAuditDetailScreen() {
	r.log.DetailTitleBar = newPlainTitleBar("Log entry")

	r.log.DetailView = tview.NewTextView()
	r.log.DetailView.SetWrap(true)
	r.log.DetailView.SetDynamicColors(true)
	r.log.DetailView.SetBorderPadding(0, 0, 1, 1)
	r.log.DetailView.SetInputCapture(r.captureLogAuditDetailKey)

	r.log.DetailLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.log.DetailTitleBar, 1, 0, false).
		AddItem(r.log.DetailView, 0, 1, true)
	r.log.DetailLayout.SetMouseCapture(r.dragLogAuditDetailMouseCapture)
}

// logAuditGroupInfo describes one FileGroup for the selection table's
// own Info column — file count, how many are compressed (gzip, xz,
// zstd, and bzip2 are all decompressed transparently — see
// logview.Open), and a call-out for any file whose compression Open
// doesn't recognize at all, which stays possible even though every
// extension familyBase/compressedExt currently parses out is
// supported today.
func logAuditGroupInfo(g logview.FileGroup) string {
	compressed, unsupported := 0, 0
	for _, f := range g.Files {
		if f.Compressed {
			compressed++
		}
		if !f.Supported {
			unsupported++
		}
	}
	info := strconv.Itoa(len(g.Files)) + " file"
	if len(g.Files) != 1 {
		info += "s"
	}
	if compressed > 0 {
		info += fmt.Sprintf(" (%d compressed)", compressed)
	}
	if unsupported > 0 {
		info += fmt.Sprintf(" — %d unsupported compression", unsupported)
	}
	return info
}

// renderLogAuditSelection fills the selection table: the family's own
// base name and logAuditGroupInfo's own summary — no checkbox column;
// Enter simply opens whichever row the cursor is already on (see
// openLogAuditViewer's own doc comment for why).
func (r *Root) renderLogAuditSelection() {
	title := " Log Holepunch — select files"
	if r.panel.isRemote() {
		// Named explicitly, not just "(remote)" — per this project's own
		// "a remote view must never describe local paths or metadata"
		// rule, read the other way: this screen now genuinely reads
		// through a remote connection (see currentLogAuditSource,
		// logauditsource.go), so saying so, and which one, is the
		// honest thing to show right where the directory itself is
		// already named.
		title += " — " + r.panel.remoteConn.Label()
	}
	renderReloadCloseTitleBar(r.log.TitleBar, title+" ", r.lastScreenWidth)
	r.log.Table.Clear()

	header := func(col int, text string) {
		r.log.Table.SetCell(0, col,
			tview.NewTableCell(text).
				SetTextColor(r.theme.Text).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false))
	}
	header(logAuditSelName, "Name")
	header(logAuditSelInfo, "Info")
	header(logAuditSelSize, "Size")
	header(logAuditSelModified, "Modified")

	if r.log.DiscoverErr != nil {
		showTablePlaceholder(r.log.Table, r.log.DiscoverErr.Error(), r.theme.EntryError)
		return
	}
	if len(r.log.Groups) == 0 {
		showTablePlaceholder(r.log.Table, "No log files found in "+r.log.Dir, r.theme.PlaceholderText)
		return
	}

	enableTableSelection(r.log.Table)
	for i, g := range r.log.Groups {
		row := i + 1
		click := r.clickLogAuditSelectionCell(row)
		r.log.Table.SetCell(row, logAuditSelName,
			tview.NewTableCell(g.Base).SetTextColor(r.theme.Text).SetSelectable(true).SetClickedFunc(click))
		// Text, not MutedTextColor — per the user's own explicit report:
		// Name already uses Text (white), and Info/Size/Modified looked
		// inconsistently dimmer next to it for no real reason (none of
		// these three carry any "this is secondary" meaning Name
		// itself doesn't also have).
		r.log.Table.SetCell(row, logAuditSelInfo,
			tview.NewTableCell(logAuditGroupInfo(g)).SetTextColor(r.theme.Text).SetSelectable(true).SetClickedFunc(click))
		sizeText, modText := "", ""
		if newest, ok := g.Newest(); ok {
			sizeText = humanSize(newest.Size)
			modText = logAuditTimeText(newest.ModTime)
		}
		r.log.Table.SetCell(row, logAuditSelSize,
			tview.NewTableCell(sizeText).SetTextColor(r.theme.Text).SetSelectable(true).SetClickedFunc(click))
		r.log.Table.SetCell(row, logAuditSelModified,
			tview.NewTableCell(modText).SetTextColor(r.theme.Text).SetSelectable(true).SetClickedFunc(click))
	}

	if cur, _ := r.log.Table.GetSelection(); cur < 1 || cur > len(r.log.Groups) {
		r.log.Table.Select(1, 0)
	}
}

// chainTableDoubleClick wraps fallback (each caller's own row-wide
// mouse capture — a scrollbar's own captureMouse, or a plain pass-
// through where there isn't one) with a check for
// tview.MouseLeftDoubleClick landing on one of table's own data
// cells — a real, reported bug, first found on Log Holepunch's own
// two tables: a fast second click (within tview's own 500ms
// DoubleClickInterval — see Application.fireMouseActions, verified
// directly against tview's own source, not guessed) fires
// MouseLeftDoubleClick instead of a second MouseLeftClick, and
// Table.MouseHandler has no case for that action at all, so it's
// never consumed there — it fell through to whatever was drawn
// underneath instead, exactly the shape Root.captureMouse's own
// MouseLeftDoubleClick case already handles for the main panel's own
// table. Reruns whichever cell's own Clicked func a plain click
// already wired — idempotent, since every caller's own Clicked func
// just reopens/reactivates the same row — rather than duplicating
// that logic here.
//
// Only actually reaches table at all if table is still part of
// tview.Pages' own visible stack by the time the second click
// arrives (confirmed directly against a real session, not assumed):
// Log Holepunch's own viewer opens *on top* of the selection table
// (pushOverlay never hides what's underneath), so Pages' own
// cascading dispatch still finds this capture when the viewer's own
// fresher widget doesn't claim the click first. A screen whose own
// Clicked action instead calls hideOverlay() outright (closing
// itself, not opening something new on top) has already removed
// itself from that same stack by the time the second click's own
// MouseLeftDoubleClick fires — wiring this exact helper onto such a
// table's own mouse capture would build and pass tests but never
// actually run in that scenario, a real trap this doc comment exists
// to name explicitly rather than leave the next reader to rediscover.
func chainTableDoubleClick(table *tview.Table, fallback func(tview.MouseAction, *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse)) func(tview.MouseAction, *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	return func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseLeftDoubleClick {
			x, y := event.Position()
			row, column := table.CellAt(x, y)
			if cell := table.GetCell(row, column); cell != nil && cell.Clicked != nil {
				cell.Clicked()
				return tview.MouseConsumed, nil
			}
		}
		return fallback(action, event)
	}
}

// clickLogAuditSelectionCell is one row's own mouse action — see
// clickSessionsCell's own doc comment (sessions.go) for why this has
// to be per-cell rather than a table-wide mouse capture: tview's own
// Table never runs a row's SetSelectedFunc on a plain click, only
// moves the selection there. A real, reported bug: clicking a log
// family selected the row but a second click did nothing to it at
// all — the event fell through to whatever was drawn underneath
// instead (the file panel), since nothing here ever actually opened
// it on a click, only on Enter. Returns true so tview does not also
// move the selection afterwards (this already does, via Select).
func (r *Root) clickLogAuditSelectionCell(row int) func() bool {
	return func() bool {
		r.log.Table.Select(row, 0)
		r.openLogAuditViewer()
		return true
	}
}

// logAuditTimeText renders an Entry's own Time — same layout
// activityLogTimeText already uses elsewhere in this package.
func logAuditTimeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}

// logAuditLevelColor mirrors activityLogLevelColor's own "stop sign"
// convention: Error/Fatal red, Warn the app's own WarningText, Info/
// Debug/Trace muted, Unknown plain text (nothing to call out).
func logAuditLevelColor(theme config.ResolvedTheme, level logview.Level) tcell.Color {
	switch level {
	case logview.LevelError, logview.LevelFatal:
		return theme.CriticalText
	case logview.LevelWarn:
		return theme.WarningText
	case logview.LevelUnknown:
		return theme.Text
	default:
		return theme.MutedTextColor
	}
}

// logAuditKeywordQuery parses the Keyword field's own boolean query
// expression (see internal/filterexpr's ParseQuery: terms, AND/OR/NAND/
// NOR/NOT, parentheses) — ok is false for a blank field or an
// unparseable expression, the same graceful-degradation contract
// logAuditMinLevel/the Time field (logAuditEntryVisible) already
// follow: that part of the filter simply isn't applied, no error state
// shown for it. Called once per render (see renderLogAuditViewer), not
// once per entry — an expression's own parse cost is the same whether
// the family holds ten lines or several million, so compiling it
// 5.7 million times over would be pure waste on exactly the large
// remote families 19a (feature_ideas.txt) is about.
func logAuditKeywordQuery(keyword string) (filterexpr.QueryFilter, bool) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return filterexpr.QueryFilter{}, false
	}
	q, err := filterexpr.ParseQuery(keyword)
	if err != nil {
		return filterexpr.QueryFilter{}, false
	}
	return q, true
}

// logAuditMatchesQuery reports whether e's own Source or Message
// satisfies query — hasQuery false (a blank or unparseable Keyword
// field, see logAuditKeywordQuery) always matches, the same "blank
// filter shows everything" contract filterActivityLogEntries already
// has.
func logAuditMatchesQuery(e logview.Entry, query filterexpr.QueryFilter, hasQuery bool) bool {
	if !hasQuery {
		return true
	}
	return query.Match(e.Source, e.Message)
}

// logAuditMinLevel parses levelText (e.g. "warn", "ERROR") into a
// minimum-severity threshold — ok is false for blank or unparseable
// text, the same graceful-degradation contract filterActivityLogEntries
// already has for an unparseable time expression: that half of the
// filter simply isn't applied, no error state shown for it.
func logAuditMinLevel(levelText string) (logview.Level, bool) {
	levelText = strings.TrimSpace(levelText)
	if levelText == "" {
		return logview.LevelUnknown, false
	}
	level := logview.ParseLevel(levelText)
	if level == logview.LevelUnknown {
		return logview.LevelUnknown, false
	}
	return level, true
}

// logAuditParsedFilters is every viewer filter already parsed once —
// see parseLogAuditFilters' own doc comment for why this must never
// happen per entry inside logAuditVisibleEntries/
// logAuditViewerVisibleEntries' own loops.
type logAuditParsedFilters struct {
	query       filterexpr.QueryFilter
	hasQuery    bool
	mtime       filterexpr.MtimeFilter
	hasMtime    bool
	minLevel    logview.Level
	hasMinLevel bool
}

// parseLogAuditFilters parses the Keyword/Time/Level fields' own
// current text exactly once per render — a real, user-reported
// performance bug without this: logAuditEntryVisible used to call
// filterexpr.ParseMtime itself, per entry, inside
// logAuditVisibleEntries' own loop — up to eleven different
// date/time layouts tried in sequence (see ParseMtime's own doc
// comment) on every single one of a 74k-entry family's own entries,
// on every keystroke typed into Keyword, whenever Time also happened
// to be populated. logAuditKeywordQuery was already correctly hoisted
// outside the loop; ParseMtime and logAuditMinLevel were not.
//
// now is captured once here too, not re-read per entry: a relative
// expression like "last 7 days" means the same instant for every
// entry in a single scan, not a slightly different one for each as
// evaluation proceeds.
//
// An unparseable Time expression still means "that half of the filter
// simply isn't applied" (hasMtime false), never "match nothing" —
// the same graceful-degradation contract logAuditMinLevel's own doc
// comment already establishes for Level, preserved exactly by
// parsing once instead of inline per call.
func parseLogAuditFilters(keyword, timeExpr, levelText string, now time.Time) logAuditParsedFilters {
	query, hasQuery := logAuditKeywordQuery(keyword)

	var mtime filterexpr.MtimeFilter
	hasMtime := false
	if strings.TrimSpace(timeExpr) != "" {
		if f, err := filterexpr.ParseMtime(timeExpr, now); err == nil {
			mtime, hasMtime = f, true
		}
	}

	minLevel, hasMinLevel := logAuditMinLevel(levelText)

	return logAuditParsedFilters{
		query: query, hasQuery: hasQuery,
		mtime: mtime, hasMtime: hasMtime,
		minLevel: minLevel, hasMinLevel: hasMinLevel,
	}
}

// logAuditEntryVisible combines all three viewer filters (AND, the
// same combination rule filterActivityLogEntries' own keyword+time
// pair already uses) against f — already parsed once per render by
// parseLogAuditFilters, never re-parsed here (see its own doc comment
// for why). An entry with LevelUnknown never passes a Level filter,
// even ">= trace" — "no level recognized" and "this severity or
// worse" are different claims, and the filter is explicitly about the
// latter.
func logAuditEntryVisible(e logview.Entry, f logAuditParsedFilters) bool {
	if !logAuditMatchesQuery(e, f.query, f.hasQuery) {
		return false
	}
	if f.hasMtime && !f.mtime.Match(e.Time) {
		return false
	}
	if f.hasMinLevel {
		if e.Level == logview.LevelUnknown || e.Level < f.minLevel {
			return false
		}
	}
	return true
}

// logAuditMaxRenderedRows caps how many matching entries
// renderLogAuditViewer actually builds TableCells for — a real
// /var/log/nginx/access.log family can run into the hundreds of
// thousands of lines, and rebuilding that many tview.TableCell values
// on every keystroke in the filter field would make typing feel
// sluggish long before anything else about this screen does. The
// title bar's own count still reports the true total (see
// renderLogAuditViewer) so narrowing the filter further is always the
// visible next step, not a silent truncation.
const logAuditMaxRenderedRows = 2000

// logAuditHighlight escapes text for safe use inside a tview cell (see
// panel.go's own addRow doc comment on why escaping has to happen
// before any tag is added, not after) and, if lowerTerms is non-empty,
// wraps every case-insensitive occurrence of any of them in a
// background tag (reusing nameHighlightTags — panel.go — exactly
// as-is: it already wraps an escaped string in "set background, then
// reset" tags) — matching is done against the original, unescaped text
// so the indices found are never thrown off by an escape sequence
// tview.Escape may have inserted.
//
// lowerTerms is already lower-cased and already filtered of empty
// strings — see logAuditLowerTerms, called once per render by
// renderLogAuditViewer/renderLogAuditDetail, not recomputed here on
// every call: this runs twice per table row (Source and Message), up
// to logAuditMaxRenderedRows times each, so lower-casing terms inside
// it used to mean thousands of redundant strings.ToLower calls and
// allocations per keystroke for no reason — the same class of
// per-entry-instead-of-per-render cost parseLogAuditFilters' own doc
// comment already describes fixing elsewhere in this file.
//
// Several terms, not a single keyword, since the Keyword field's own
// boolean query (internal/filterexpr's ParseQuery) can carry several
// terms at once — see QueryFilter.PositiveTerms's own doc comment for
// why only the positively-matching ones (never one under NOT/NAND/NOR)
// ever reach here. At each position, the longest term that matches
// right there wins — so a shorter term that happens to be a prefix of
// a longer one at the same spot (both given as terms) doesn't fire
// twice or split the longer match apart.
// logAuditLowerTerms lower-cases terms (QueryFilter.PositiveTerms' own
// output) once, filtering out any empty string — see
// logAuditHighlight's own doc comment for why this must happen once
// per render, not once per call.
func logAuditLowerTerms(terms []string) []string {
	lowerTerms := make([]string, 0, len(terms))
	for _, t := range terms {
		if t != "" {
			lowerTerms = append(lowerTerms, strings.ToLower(t))
		}
	}
	return lowerTerms
}

func logAuditHighlight(text string, lowerTerms []string, bg tcell.Color) string {
	if len(lowerTerms) == 0 {
		return tview.Escape(text)
	}

	lower := strings.ToLower(text)
	var b strings.Builder
	rest := text
	restLower := lower
	for {
		bestIdx, bestLen := -1, 0
		for _, lt := range lowerTerms {
			idx := strings.Index(restLower, lt)
			if idx < 0 {
				continue
			}
			if bestIdx < 0 || idx < bestIdx || (idx == bestIdx && len(lt) > bestLen) {
				bestIdx, bestLen = idx, len(lt)
			}
		}
		if bestIdx < 0 {
			b.WriteString(tview.Escape(rest))
			break
		}
		b.WriteString(tview.Escape(rest[:bestIdx]))
		match := rest[bestIdx : bestIdx+bestLen]
		b.WriteString(nameHighlightTags(tview.Escape(match), bg))
		rest = rest[bestIdx+bestLen:]
		restLower = restLower[bestIdx+bestLen:]
	}
	return b.String()
}

// logAuditFilterFields lists the Keyword/Time/Level fields together,
// for restyleLogAuditFilterFields.
func (r *Root) logAuditFilterFields() []*tview.InputField {
	return []*tview.InputField{r.log.KeywordField, r.log.TimeField, r.log.LevelField}
}

// restyleLogAuditFocusTargets re-styles every focusable stop on this
// screen's filter row (the three fields, the timeline, and the table's
// own focus indicator) from one shared place, given whichever one
// (identified by interface identity, not type — they don't share one
// beyond tview.Primitive) actually just gained focus: highlighting
// that one and graying out every other. One function instead of each
// of the five FocusFunc callbacks separately remembering to also
// unstyle the other four — the Timeline's own addition is exactly the
// kind of thing a scattered version would have been easy to forget at
// one of the other four call sites.
//
// Works around a real, confirmed tview gap rather than a bug in our
// own BlurFunc wiring, for the three *tview.InputField targets
// specifically: NewInputField's own constructor (verified directly
// against inputfield.go) forwards its embedded TextArea's Focus event
// to the InputField's own registered FocusFunc — "// Forward focus
// event to the input field." — but never adds the matching Blur
// forwarder. A Tab-driven focus change still blurs the *InputField
// itself (Root's own code calls r.app.SetFocus on the InputField value
// directly — see logAuditViewerFieldDone), so that path's own blur
// would still fire correctly if anything were still listening for it.
// A mouse click, though, is handled by TextArea.MouseHandler (see its
// own MouseLeftDown case), which hands focus to the TextArea *value*,
// not the InputField wrapping it — so from that point on,
// Application's own a.focus.Blur() blurs the TextArea, which has no
// corresponding forwarder back out to the InputField's own BlurFunc,
// and a Blur-driven restyle would stay visually "focused" forever
// after even once real focus has long since moved elsewhere —
// confirmed directly against a tcell.SimulationScreen with
// instrumented Focus/Blur callbacks and app.GetFocus() printed
// before/after each click, not assumed from reading tview's source
// alone. This is exactly why every restyle here happens from a
// FocusFunc (the known-forwarded event) rather than any BlurFunc (the
// known-broken one for InputField) — not one of the five callers below
// registers a BlurFunc of its own at all.
func (r *Root) restyleLogAuditFocusTargets(focused tview.Primitive) {
	// timeFieldWasFocused, not r.log.TimeField.HasFocus(): the
	// latter is exactly the unreliable signal this function's own doc
	// comment already explains tview never updates on a mouse-driven
	// focus change away from an InputField (Blur fires on its embedded
	// TextArea, never forwarded back to the InputField's own Box) — own
	// bookkeeping, updated on every call here (which, unlike Blur, this
	// function always reliably is, driven by the next target's own
	// Focus event), is what correctly detects "the Time field just lost
	// focus" so its own debounced render (see its own SetDoneFunc
	// comment on why Time isn't live like Keyword/Level) still applies
	// when the user leaves it with a mouse click rather than Tab.
	timeFieldWasFocused := r.log.TimeFieldFocused
	for _, f := range r.logAuditFilterFields() {
		styleInput(f, r.theme, tview.Primitive(f) == focused)
	}
	r.log.TimeFieldFocused = focused == r.log.TimeField
	if timeFieldWasFocused && !r.log.TimeFieldFocused {
		r.renderLogAuditViewer()
	}
	r.log.TimelineFocused = focused == r.log.TimelineView
	background := r.theme.PopupBackground
	if r.log.TimelineFocused {
		background = r.theme.InputFocusedBackground
	}
	r.log.TimelineView.SetBackgroundColor(background)
	r.repaintLogAuditTimeline()
	r.renderLogAuditFocusIndicator(focused == r.log.ViewerTable)
	// styleInput's own call just above (inside the loop) resets every
	// filter field's label to the ordinary, uniform TextColor on every
	// focus change — reapplied here, after it, so a focus change never
	// silently erases any of the three fields' own validity color.
	r.restyleLogAuditKeywordFieldLabel()
	r.restyleLogAuditTimeFieldLabel()
	r.restyleLogAuditLevelFieldLabel()
}

// logAuditValidityLabelColor is tcell.ColorLime/ColorRed directly,
// not theme.EntryExecutable/CriticalText: this is a live, momentary
// ping meant to catch the eye while typing, not part of this app's
// own themed palette — EntryExecutable in particular is deliberately
// muted (see its own doc comment on why), the opposite of what this
// needs. Per the user's own explicit request for something "deutlich
// heller, greller" than the themed colors gave here. Shared by all
// three filter fields' own restyle functions below.
func logAuditValidityLabelColor(valid bool) tcell.Color {
	if valid {
		return tcell.ColorLime
	}
	return tcell.ColorRed
}

// restyleLogAuditKeywordFieldLabel colors the "Filter: " label by
// whether logAuditKeywordQuery can currently parse it — see its own
// doc comment on the silent-degradation contract this makes visible.
// Cheap enough to run on every keystroke: a parse attempt against the
// typed text alone, the same already-live cost renderLogAuditViewer
// itself pays right alongside it in this field's own SetChangedFunc.
func (r *Root) restyleLogAuditKeywordFieldLabel() {
	text := strings.TrimSpace(r.log.KeywordField.GetText())
	valid := true
	if text != "" {
		_, ok := logAuditKeywordQuery(text)
		valid = ok
	}
	r.log.KeywordField.SetLabelColor(logAuditValidityLabelColor(valid))
}

// restyleLogAuditTimeFieldLabel colors the "Time: " label by whether
// filterexpr.ParseMtime can currently parse it — per the user's own
// explicit request for a visible sign the expression typed so far
// either means something or doesn't, cheap enough to run on every
// keystroke (see SetChangedFunc's own doc comment above on why the
// field's live re-filtering specifically isn't).
func (r *Root) restyleLogAuditTimeFieldLabel() {
	text := strings.TrimSpace(r.log.TimeField.GetText())
	valid := true
	if text != "" {
		_, err := filterexpr.ParseMtime(text, time.Now())
		valid = err == nil
	}
	r.log.TimeField.SetLabelColor(logAuditValidityLabelColor(valid))
}

// restyleLogAuditLevelFieldLabel mirrors
// restyleLogAuditKeywordFieldLabel for the "Level >= " field — see
// logAuditMinLevel's own doc comment on the identical silent-
// degradation contract this makes visible.
func (r *Root) restyleLogAuditLevelFieldLabel() {
	text := strings.TrimSpace(r.log.LevelField.GetText())
	valid := true
	if text != "" {
		_, ok := logAuditMinLevel(text)
		valid = ok
	}
	r.log.LevelField.SetLabelColor(logAuditValidityLabelColor(valid))
}

// logAuditCurrentlyFocused reports whichever of the five
// restyleLogAuditFocusTargets stops (three fields, the timeline, or
// the table) currently has real keyboard focus, or nil if none do
// (e.g. a different overlay entirely). Only applyLogAuditTheme needs
// this — every focus *transition* already knows which target it is
// without asking (see each SetFocusFunc above), the same "pass the
// known state in, don't re-derive it" shape renderLogAuditFocusIndicator's
// own doc comment already establishes; a theme refresh is the one case
// that isn't itself a transition and has nothing else to go on.
func (r *Root) logAuditCurrentlyFocused() tview.Primitive {
	switch {
	case r.log.KeywordField.HasFocus():
		return r.log.KeywordField
	case r.log.TimeField.HasFocus():
		return r.log.TimeField
	case r.log.LevelField.HasFocus():
		return r.log.LevelField
	case r.log.TimelineView.HasFocus():
		return r.log.TimelineView
	case r.log.ViewerTable.HasFocus():
		return r.log.ViewerTable
	}
	return nil
}

// renderLogAuditFocusIndicator paints the row itself in
// theme.InputFocusedBackground while focused is true (logAuditViewer-
// Table itself has keyboard focus), theme.SurfaceBackground otherwise
// — the user's own explicit request for a visible cue that Tab-cycling
// has landed back on the list (as opposed to one of the three filter
// fields above it), rather than only being inferable from which row
// happens to be highlighted. No text of its own (an earlier "● List"
// label was dropped per the user's own explicit, later report that it
// added nothing the background color alone doesn't already say).
// Takes focused explicitly rather than querying log.ViewerTable.
// HasFocus() itself: tview.Box.Blur() (verified directly against its
// own box.go) runs the blur callback *before* flipping its own
// hasFocus flag, so a blur-triggered call here would otherwise still
// see focus as true — the exact same "pass the known state in, don't
// re-derive it" shape styleInput's own FocusFunc/BlurFunc pairs
// already use elsewhere in this file. Called from the table's own
// SetFocusFunc/SetBlurFunc (see newLogAuditViewerScreen) and once from
// applyLogAuditTheme (passing the table's own current, settled
// HasFocus — safe there, since that call never happens from inside a
// Blur callback) so a live theme switch repaints it in the right
// color either way.
func (r *Root) renderLogAuditFocusIndicator(focused bool) {
	// PanelBackground, not SurfaceBackground — matches the table's own
	// new background (see log.ViewerTable's own comment) so the
	// indicator still blends into the table it's a focus cue for while
	// unfocused.
	background := r.theme.PanelBackground
	if focused {
		background = r.theme.InputFocusedBackground
	}
	r.log.FocusIndicator.SetBackgroundColor(background)
}

// logAuditLoadGlyphPlaceholder stands in for the animated glyph inside
// log.ViewerTitleTemplate — buildLogAuditViewerTitle writes it,
// resolveLogAuditLoadGlyph/tickLogAuditLoadAnimation substitute the
// real glyph in later, so neither needs to rebuild the rest of the
// title string around it.
const logAuditLoadGlyphPlaceholder = "\x00"

// buildLogAuditViewerTitle builds the viewer's own title-bar text from
// shown/errCount/warnCount (see logAuditVisibleEntries) and whatever
// Root fields renderLogAuditViewer already keeps current — factored
// out so it can be built once per real data change and reused as-is
// by the animation-only tick (see tickLogAuditLoadAnimation), instead
// of every one of several-times-a-second ticks re-scanning every
// entry just to redraw a single moving glyph.
func (r *Root) buildLogAuditViewerTitle(shown []logview.Entry, errCount, warnCount int) string {
	filesText := fmt.Sprintf("%d files", r.log.Files)
	if r.log.LoadingMore {
		// The glyph itself is filled in by resolveLogAuditLoadGlyph (a
		// fresh render) or tickLogAuditLoadAnimation (the ticker, no
		// rescan) — never baked in here, so both can share this exact
		// same template string.
		filesText = fmt.Sprintf("%s %d/%d files", logAuditLoadGlyphPlaceholder, r.log.Files, r.log.TotalFiles)
	}

	title := fmt.Sprintf(" Log Holepunch — %d events │ %s │ %d skipped │ %d errors │ %d warnings",
		len(r.log.AllEntries), filesText, r.log.Skipped, errCount, warnCount)
	if len(shown) != len(r.log.AllEntries) {
		title += fmt.Sprintf(" │ %d matching", len(shown))
	}
	if len(shown) > logAuditMaxRenderedRows {
		// Without this, a file past the cap silently showed only a
		// fraction of what the counts above claim — a real, user-
		// reported point of confusion ("es werden doch auch alle logs
		// ... richtig angezeigt", i.e. the file counts looked complete,
		// so the missing rows read as a bug rather than a documented
		// limit). Shortened to "showing x/y" (was "showing first x (of
		// y — narrow the filter to see the rest)") per the user's own
		// explicit request, now that the files segment above can
		// itself grow a second, similarly-shaped count while loading.
		title += fmt.Sprintf(" │ showing %d/%d", logAuditMaxRenderedRows, len(shown))
	}
	if r.log.Following {
		title += " │ ● following"
	}
	// A parse error only blocks the whole view when there is nothing
	// else to show — readLogAuditFile/logaudit.go keeps every entry
	// that DID parse from a group's other files even when one of them
	// failed (a corrupted .gz, a permission error on an older
	// rotation, ...), so showing just the error page here used to
	// silently hide all of those too, a real, independently-discovered
	// bug directly on point for "nicht alle Logeinträge sichtbar": an
	// unrelated problem with one rotation of a file could erase every
	// entry from the whole family, successfully-read ones included.
	if r.log.ParseErr != nil && len(r.log.AllEntries) > 0 {
		title += fmt.Sprintf(" │ 1 file failed: %s", r.log.ParseErr.Error())
	}
	// One general note instead of repeating it on every affected hint
	// — see logAuditViewerHintEntries' own doc comment for why.
	title += " │ e/w/Enter/r/f/s need the list focused"
	return title
}

// resolveLogAuditLoadGlyph substitutes the current animation frame's
// own glyph into template (see logAuditLoadGlyphPlaceholder) — a plain
// pass-through when nothing is loading, since buildLogAuditViewerTitle
// never writes the placeholder in that case.
func (r *Root) resolveLogAuditLoadGlyph(template string) string {
	if !r.log.LoadingMore {
		return template
	}
	glyph := hashAnimationFrames[r.log.LoadAnimFrame%len(hashAnimationFrames)]
	return strings.Replace(template, logAuditLoadGlyphPlaceholder, glyph, 1)
}

// tickLogAuditLoadAnimation repaints only the viewer's own title bar
// with the next animation frame — see animateLogAuditLoad's own doc
// comment (logaudit.go) for why this never calls renderLogAuditViewer
// itself. A no-op once log.ViewerTitleTemplate has been cleared
// (stopLogAuditProgressiveLoad already ran) — a tick that was already
// queued when that happened lands here, finds nothing to do, and
// simply returns.
func (r *Root) tickLogAuditLoadAnimation() {
	r.log.LoadAnimFrame++
	if r.log.ViewerTitleTemplate == "" {
		return
	}
	title := r.resolveLogAuditLoadGlyph(r.log.ViewerTitleTemplate)
	renderReloadCloseTitleBar(r.log.ViewerTitle, title+" ", r.lastScreenWidth)
}

// logAuditVisibleEntries filters r.log.AllEntries down to whatever
// currently matches the Keyword/Time/Level fields — factored out of
// renderLogAuditViewer so consigliereLogAuditMatchedLines (Machiavelli
// Mode's own context snapshot addition) and Statistics
// (logAuditViewerVisibleEntries, logauditstats.go) can reuse the exact
// same filtering the table itself shows, rather than risking the two
// drifting apart. Error/warning counts for the title bar come from
// r.log.ErrCount/WarnCount instead (recomputeLogAuditCombined,
// logaudit.go) — filter-independent, so computed once whenever
// AllEntries itself changes, not here on every call.
//
// A real, reported performance bug on a multi-million-entry family
// (4.4M events confirmed): this is a single O(n) pass with no per-
// entry string parsing any more (see parseLogAuditFilters' own doc
// comment for the two things that used to be re-parsed per entry
// instead of once), but n itself is still enough to take a real,
// measurable fraction of a second — confirmed by benchmark
// (BenchmarkLogAuditVisibleEntries). Callers driven by live typing
// (Keyword/Level's own SetChangedFunc) must not call this directly on
// the UI goroutine at that scale — see scheduleLogAuditFilterRescan's
// own doc comment for why and how.
func (r *Root) logAuditVisibleEntries() []logview.Entry {
	filters := parseLogAuditFilters(r.log.KeywordField.GetText(), r.log.TimeField.GetText(), r.log.LevelField.GetText(), time.Now())
	return scanLogAuditEntries(r.log.AllEntries, filters)
}

// scanLogAuditEntries is logAuditVisibleEntries' own pure, Root-free
// half — entries and filters already known, no tview/Root field
// touched — so scheduleLogAuditFilterRescan can run it from a
// background goroutine against a captured snapshot instead of
// r.log.AllEntries directly (see its own doc comment on why that
// matters).
func scanLogAuditEntries(entries []logview.Entry, filters logAuditParsedFilters) []logview.Entry {
	var shown []logview.Entry
	for _, e := range entries {
		if logAuditEntryVisible(e, filters) {
			shown = append(shown, e)
		}
	}
	return shown
}

// renderLogAuditViewer is the ordinary, synchronous entry point —
// see renderLogAuditViewerFor's own doc comment for what it actually
// builds, and scheduleLogAuditFilterRescan's for the live-typing
// alternative this is deliberately not.
func (r *Root) renderLogAuditViewer() {
	r.renderLogAuditViewerFor(r.logAuditVisibleEntries())
}

// scheduleLogAuditFilterRescan is the Keyword/Level fields' own live
// SetChangedFunc hook — unlike renderLogAuditViewer's own ordinary,
// synchronous callers (opening the viewer, "r", follow, toggling
// sort, a Time field edit...), live typing needs the scan itself off
// the UI goroutine: see logAuditVisibleEntries' own doc comment for
// the real-world cost on a multi-million-entry family — a real,
// reported case where typing into Keyword felt "unglaublich träge"
// even after the per-entry parsing fixes, because the scan is a
// genuine, unavoidable O(n) pass over all of AllEntries regardless.
//
// entries is captured here, on the UI goroutine, before dispatch —
// logview.Merge (logaudit.go) always allocates a fresh slice rather
// than mutating an existing one in place, so this snapshot stays
// valid for the goroutine's own read even if the progressive loader
// replaces r.log.AllEntries with a newer slice while the scan is
// still running; the two can never alias the same backing array.
//
// FilterScanToken guards against a result landing after a newer
// keystroke superseded it — the same per-request staleness guard
// loadToken/diskUsageForPath/viewerLookDownloadPath already establish
// elsewhere in this app (see the async-ui skill). The table and
// timeline simply keep showing whatever they already had until the
// newest request's own result lands — no placeholder, no "loading"
// state of its own: a 330ms-on-4.4M scan landing shortly after the
// last keystroke is the same "results settle a moment after you stop
// typing" shape search-as-you-type already has everywhere else, not
// something that needs its own visible affordance.
func (r *Root) scheduleLogAuditFilterRescan() {
	r.log.FilterScanToken++
	if r.log.FilterScanInFlight {
		// Coalesce rather than spawn a second concurrent scan: fast
		// typing on a multi-million-entry family used to mean every
		// keystroke's own O(n) scan ran regardless of whether the
		// previous one had even finished — see runLogAuditFilterScan's
		// own doc comment for what happens once this one completes.
		r.log.FilterScanPending = true
		return
	}
	r.runLogAuditFilterScan()
}

// runLogAuditFilterScan is scheduleLogAuditFilterRescan's own dispatch
// half — only ever one of these in flight at a time (FilterScanInFlight
// guards it). Re-reads Keyword/Time/Level fresh here, not at schedule
// time, so a coalesced dispatch (FilterScanPending, below) always scans
// against whatever the fields actually hold once it finally runs, not
// a stale snapshot from whichever earlier keystroke first asked.
func (r *Root) runLogAuditFilterScan() {
	entries := r.log.AllEntries
	filters := parseLogAuditFilters(r.log.KeywordField.GetText(), r.log.TimeField.GetText(), r.log.LevelField.GetText(), time.Now())
	token := r.log.FilterScanToken
	r.log.FilterScanInFlight = true
	scan := r.log.ScanFn
	if scan == nil {
		scan = r.defaultLogAuditScanFn
	}
	scan(entries, filters, func(shown []logview.Entry) {
		r.log.FilterScanInFlight = false
		if r.log.FilterScanToken == token {
			r.renderLogAuditViewerFor(shown)
		}
		// else: superseded by a newer keystroke while this scan was
		// running — same per-request staleness guard loadToken/
		// diskUsageForPath/viewerLookDownloadPath already establish
		// elsewhere in this app (see async-ui skill).
		if r.log.FilterScanPending {
			r.log.FilterScanPending = false
			r.runLogAuditFilterScan() // one more, coalesced, pass — see scheduleLogAuditFilterRescan
		}
	})
}

// defaultLogAuditScanFn is log.ScanFn's own real implementation —
// wrapped in safeGo (see its own doc comment) like every other
// background goroutine in this app, result delivered back through
// QueueUpdateDraw.
func (r *Root) defaultLogAuditScanFn(entries []logview.Entry, filters logAuditParsedFilters, done func([]logview.Entry)) {
	r.safeGo("log audit filter scan", nil, func() {
		shown := scanLogAuditEntries(entries, filters)
		r.app.QueueUpdateDraw(func() {
			done(shown)
		})
	})
}

// renderLogAuditViewerFor is renderLogAuditViewer's own apply half —
// shown already computed, by either of its two callers above (inline
// for the ordinary synchronous case, or scheduleLogAuditFilterRescan's
// own QueueUpdateDraw callback for a live keystroke at scale) — fills
// the viewer table with every entry matching the keyword field,
// Time/Level/Source/Message columns, the matched keyword highlighted
// in Source/Message via logAuditHighlight — and updates the title bar
// with the running counts the original conversation's own mockup
// called for ("4,812 events │ 3 files │ 2 compressed │ 1 error"). The
// Time column header is clickable (see above) and toggles
// log.NewestFirst, the same "click to sort, click again to reverse"
// convention the panel's own column headers already use (see
// sortArrow, panel.go) — applied here by reversing shown before
// logAuditMaxRenderedRows truncates it, so a truncated view always
// keeps the chronological end that direction actually means to show
// (the newest entries, not whichever happened to come first in file
// order) rather than silently biasing toward one end regardless of
// what the arrow says.
func (r *Root) renderLogAuditViewerFor(shown []logview.Entry) {
	r.log.ViewerTable.Clear()

	header := func(col int, text string) {
		r.log.ViewerTable.SetCell(0, col,
			tview.NewTableCell(text).
				SetTextColor(r.theme.Text).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false))
	}
	timeLabel := padRight("Time"+sortArrow(r.log.NewestFirst), 19)
	r.log.ViewerTable.SetCell(0, logAuditColTime,
		tview.NewTableCell(timeLabel).
			SetTextColor(r.theme.Text).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false).
			SetClickedFunc(func() bool {
				r.log.NewestFirst = !r.log.NewestFirst
				r.renderLogAuditViewer()
				return false
			}))
	header(logAuditColLevel, padRight("Level", 5))
	header(logAuditColSource, "Source")
	header(logAuditColMessage, "Message")

	keyword := r.log.KeywordField.GetText()
	query, hasQuery := logAuditKeywordQuery(keyword)

	var highlightTerms []string
	if hasQuery {
		highlightTerms = logAuditLowerTerms(query.PositiveTerms())
	}

	title := r.buildLogAuditViewerTitle(shown, r.log.ErrCount, r.log.WarnCount)
	if r.log.LoadingMore {
		r.log.ViewerTitleTemplate = title
	} else {
		r.log.ViewerTitleTemplate = ""
	}
	renderReloadCloseTitleBar(r.log.ViewerTitle, r.resolveLogAuditLoadGlyph(title)+" ", r.lastScreenWidth)

	r.renderLogAuditTimeline(shown)

	if r.log.ParseErr != nil && len(r.log.AllEntries) == 0 {
		showTablePlaceholder(r.log.ViewerTable, r.log.ParseErr.Error(), r.theme.EntryError)
		return
	}
	if len(shown) == 0 {
		placeholder := "No log entries."
		if len(r.log.AllEntries) > 0 {
			placeholder = "No entries match the current filter."
		}
		showTablePlaceholder(r.log.ViewerTable, placeholder, r.theme.PlaceholderText)
		return
	}

	// log.NewestFirst reverses the table's own order, not shown
	// itself — renderLogAuditTimeline above already ran against the
	// real chronological order, which its own density strip always
	// reads left-to-right regardless of which way the table currently
	// sorts.
	rendered := shown
	if r.log.NewestFirst {
		rendered = slices.Clone(rendered)
		slices.Reverse(rendered)
	}
	// logAuditMaxRenderedRows caps the table itself, not the counts
	// above or the timeline (both still reflect every matching entry)
	// — see its own doc comment for why. Truncating after the reverse
	// above, not before: the first logAuditMaxRenderedRows of whichever
	// order is currently showing is always the relevant end to keep —
	// the newest entries when sorted newest-first, not whichever
	// happened to come first in file order regardless of the arrow.
	if len(rendered) > logAuditMaxRenderedRows {
		rendered = rendered[:logAuditMaxRenderedRows]
	}

	enableTableSelection(r.log.ViewerTable)
	bg := r.theme.ButtonBackground
	for i, e := range rendered {
		row := i + 1
		levelColor := logAuditLevelColor(r.theme, e.Level)
		cell := func(col int, text string, color tcell.Color) {
			r.log.ViewerTable.SetCell(row, col,
				tview.NewTableCell(text).SetTextColor(color).SetSelectable(true).
					SetClickedFunc(r.clickLogAuditViewerCell(row)))
		}
		cell(logAuditColTime, padRight(logAuditTimeText(e.Time), 19), r.theme.Text)
		cell(logAuditColLevel, padRight(e.Level.String(), 5), levelColor)
		cell(logAuditColSource, logAuditHighlight(e.Source, highlightTerms, bg), r.theme.Text)
		cell(logAuditColMessage, logAuditHighlight(e.Message, highlightTerms, bg), r.theme.Text)
		r.log.ViewerTable.GetCell(row, logAuditColTime).SetReference(e)
	}

	if cur, _ := r.log.ViewerTable.GetSelection(); cur < 1 || cur > len(rendered) {
		r.log.ViewerTable.Select(1, 0)
	}
}

// logAuditEntryAt returns the Entry backing row, if any — stashed on
// the Time cell via SetReference in renderLogAuditViewer, the same
// "reference the real data, don't re-derive it from displayed text"
// convention Panel's own addRow/CurrentRowPath already use.
func (r *Root) logAuditEntryAt(row int) (logview.Entry, bool) {
	cell := r.log.ViewerTable.GetCell(row, logAuditColTime)
	if cell == nil {
		return logview.Entry{}, false
	}
	e, ok := cell.GetReference().(logview.Entry)
	return e, ok
}

// navigateLogAuditLevel moves the cursor to the next row at or below
// level's own severity (Error also matches Fatal — the same "e" key
// covers both, since both are equally worth stopping at) — wraps
// around to the top once it reaches the bottom, so "e"/"w" read as "the
// next one, cycling" rather than stalling once the last match has been
// seen.
func (r *Root) navigateLogAuditLevel(level logview.Level) {
	rowCount := r.log.ViewerTable.GetRowCount()
	if rowCount <= 1 {
		return
	}
	cur, _ := r.log.ViewerTable.GetSelection()
	matches := func(l logview.Level) bool {
		if level == logview.LevelError {
			return l == logview.LevelError || l == logview.LevelFatal
		}
		return l == level
	}
	for i := 1; i < rowCount; i++ {
		row := cur + i
		if row >= rowCount {
			row -= rowCount - 1
		}
		if e, ok := r.logAuditEntryAt(row); ok && matches(e.Level) {
			r.log.ViewerTable.Select(row, 0)
			return
		}
	}
}

// clickLogAuditViewerCell mirrors clickLogAuditSelectionCell's own
// doc comment above, for the viewer table's own rows: a click used to
// select a log line but never open it the way Enter does, falling
// through to whatever was underneath instead (the file panel) on a
// second click.
func (r *Root) clickLogAuditViewerCell(row int) func() bool {
	return func() bool {
		r.log.ViewerTable.Select(row, 0)
		r.openLogAuditDetail(row)
		return true
	}
}

// openLogAuditDetail shows row's own full entry — File/Line, Time,
// Level, Source, and the complete Message with the current keyword
// highlighted — centered over the whole screen, the same shape
// openMessageDetail (messagedetail.go) already establishes.
func (r *Root) openLogAuditDetail(row int) {
	e, ok := r.logAuditEntryAt(row)
	if !ok {
		return
	}

	width := logAuditDetailMaxWidth
	if _, _, screenWidth, _ := r.GetRect(); screenWidth > 2 && width > screenWidth-2 {
		width = screenWidth - 2
	}

	query, hasQuery := logAuditKeywordQuery(r.log.KeywordField.GetText())
	var highlightTerms []string
	if hasQuery {
		highlightTerms = logAuditLowerTerms(query.PositiveTerms())
	}
	bg := r.theme.ButtonBackground
	header := fmt.Sprintf("%s  %s  %s\n", logAuditTimeText(e.Time), e.Level.String(), e.File)
	body := strings.Join(wrapText(e.Message, width), "\n")
	full := header + body

	// Raw only earns its own section when it actually differs from
	// Message — identical for FormatPlain (see Entry.Raw's own doc
	// comment), where showing it twice would be noise, not help.
	if e.Raw != e.Message {
		rawBody := strings.Join(wrapText(e.Raw, width), "\n")
		full += "\n\nRaw:\n" + rawBody
	}

	text := tview.Escape(header) + logAuditHighlight(full[len(header):], highlightTerms, bg)
	r.log.DetailView.SetText(text)
	r.log.DetailTitleBar.SetText(" Log entry ")

	textWidth, textHeight := textSize(full)
	height := textHeight + 1 // +1 for the title bar row
	_, _, screenWidth, screenHeight := r.GetRect()
	x, y, boxWidth, boxHeight := r.clampToScreen((screenWidth-textWidth)/2, (screenHeight-height)/2, textWidth, height)

	r.log.DetailLayout.SetRect(x, y, boxWidth, boxHeight)
	r.pushOverlay(logAuditDetailPage, r.log.DetailLayout, nil)
}

func (r *Root) closeLogAuditDetail() {
	r.hideOverlay()
}

// moveLogAuditDetail repositions the Log entry detail dialog to
// (x, y) without touching its own current width/height — mirrors
// moveSearch (search.go), simpler for the same reason: the dialog is
// a single tview.Flex (title bar + text view, see
// newLogAuditDetailScreen), so one SetRect is all that's needed —
// Flex recalculates both children's own sub-rects on the next Draw,
// unlike Search/Properties' own separately-positioned title bars.
func (r *Root) moveLogAuditDetail(x, y int) {
	_, _, width, height := r.log.DetailLayout.GetRect()
	x, y, width, height = r.clampToScreen(x, y, width, height)
	r.log.DetailLayout.SetRect(x, y, width, height)
	// Flex only cascades a real rect down to its own children on the
	// next Draw() — a title-bar click right after a drag-move (the
	// very next mouse event, well before any Draw happens) would
	// otherwise still test against the pre-move rect, the same "SetRect
	// alone never cascades" caveat moveSearch's own doc comment already
	// covers for an identical shape.
	r.log.DetailTitleBar.SetRect(x, y, width, 1)
}

// dragLogAuditDetailMouseCapture makes the Log entry detail dialog's
// own title bar draggable — mirrors dragSearchMouseCapture (see its
// own, considerably longer doc comment for the full reasoning,
// including why captureOutsideClick needs its own
// log.DetailDragging check the same way it already has one for
// search.Dragging/properties.Dragging). Deliberately no close glyph,
// unlike Search/Properties — see log.DetailDragging's own doc
// comment in root.go.
func (r *Root) dragLogAuditDetailMouseCapture(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	x, y := event.Position()

	if r.log.DetailDragging {
		switch action {
		case tview.MouseMove:
			if event.Buttons()&tcell.ButtonPrimary == 0 {
				r.log.DetailDragging = false
				return tview.MouseConsumed, nil
			}
			r.moveLogAuditDetail(x-r.log.DetailDragOffsetX, y-r.log.DetailDragOffsetY)
			return tview.MouseConsumed, nil
		case tview.MouseLeftUp:
			r.log.DetailDragging = false
			return tview.MouseConsumed, nil
		}
		return tview.MouseConsumed, nil // swallow everything else for the duration of the drag
	}

	if action == tview.MouseLeftDown && r.log.DetailTitleBar.InRect(x, y) {
		wx, wy, _, _ := r.log.DetailLayout.GetRect()
		r.log.DetailDragging = true
		r.log.DetailDragOffsetX, r.log.DetailDragOffsetY = x-wx, y-wy
		return tview.MouseConsumed, nil
	}

	return action, event
}

// captureLogAuditSelectionTableKey: "r" re-scans the directory, Escape
// closes — the same shape captureActivityLogTableKey already
// establishes. Enter is handled by SetSelectedFunc, not here (see
// newLogAuditSelectionScreen).
func (r *Root) captureLogAuditSelectionTableKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeLogAuditSelection()
		return nil
	}
	// Tab/Backtab reach the AI Consigliere sidebar — see
	// startConsigliereChord's own doc comment in consigliere.go. The
	// global focus cycle excludes consigliere.Input/consigliere.History
	// while this screen is open (see focusCycleStops' own doc comment
	// in detailssidebar.go), so this table — the only real stop here —
	// is responsible for its own entry point into it, same reasoning
	// gitBreachFocusNext/gitBreachFocusPrev already give. A no-op
	// (falls through unchanged) whenever the sidebar isn't even open:
	// nothing else to cycle to on this screen.
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
	if event.Key() == tcell.KeyRune {
		switch event.Rune() {
		case 'r':
			r.reloadLogAuditDiscovery()
			return nil
		case 'g':
			// "gC" reaches the AI Consigliere from here too — see
			// startConsigliereChord's own doc comment in
			// consigliere.go. log.Table is a *tview.Table, which
			// (unlike a *tview.TextView) has no built-in meaning for a
			// bare 'g' to begin with, so nothing existing is lost by
			// claiming it here.
			r.startConsigliereChord(nil)
			return nil
		}
	}
	return event
}

// logAuditViewerFieldDone is each filter field's own SetDoneFunc — the
// same shape activityLogFieldDone already establishes for Activity
// Log's own keyword/time pair, just cycling through three fields
// (Keyword → Time → Level → table) instead of two: Tab/Enter moves to
// next, Backtab to prev, Escape closes the viewer regardless of which
// field it was pressed in.
func (r *Root) logAuditViewerFieldDone(key tcell.Key, next, prev tview.Primitive) {
	switch key {
	case tcell.KeyEscape:
		r.closeLogAuditViewer()
	case tcell.KeyTab, tcell.KeyEnter:
		r.app.SetFocus(next)
	case tcell.KeyBacktab:
		r.app.SetFocus(prev)
	}
}

// captureLogAuditViewerTableKey: Tab returns focus to the Keyword
// field (restarting the Keyword→Time→Level→Timeline→Table cycle),
// Backtab to the Timeline (the stop right before the table in that
// cycle — see captureLogAuditTimelineKey) — "e"/"w" jump to the next
// error/warning, "r" re-reads the same files, "f" toggles follow, "s"
// opens the Statistics modal, Escape closes the viewer (back to the
// selection screen, see closeLogAuditViewer/pushOverlay).
func (r *Root) captureLogAuditViewerTableKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeLogAuditViewer()
		return nil
	}
	if event.Key() == tcell.KeyTab {
		// The forward edge into the AI Consigliere sidebar — the
		// table is this screen's own last stop (Keyword -> Time ->
		// Level -> Timeline -> Table), so wrapping back to Keyword
		// (the ordinary case) is what gives way to it instead,
		// exactly the same reasoning the Keyword field's own backward
		// edge (SetDoneFunc above) gives for Backtab.
		if r.consigliere.Visible {
			r.app.SetFocus(r.consigliere.Input)
			return nil
		}
		r.app.SetFocus(r.log.KeywordField)
		return nil
	}
	if event.Key() == tcell.KeyBacktab {
		r.app.SetFocus(r.log.TimelineView)
		return nil
	}
	if event.Key() == tcell.KeyRune {
		switch event.Rune() {
		case 'e':
			r.navigateLogAuditLevel(logview.LevelError)
			return nil
		case 'w':
			r.navigateLogAuditLevel(logview.LevelWarn)
			return nil
		case 'r':
			r.reopenLogAuditViewer()
			return nil
		case 'f':
			r.toggleLogAuditFollow()
			return nil
		case 's':
			r.openLogAuditStats()
			return nil
		case 'g':
			// "gC" reaches the AI Consigliere — see
			// startConsigliereChord's own doc comment in
			// consigliere.go. log.ViewerTable is a *tview.Table,
			// unaffected by the TextView-only vi-style "g" (Home)
			// binding captureLogAuditDetailKey's own case below has to
			// account for.
			r.startConsigliereChord(r.log.KeywordField)
			return nil
		}
	}
	return event
}

// captureLogAuditTimelineKey handles keyboard interaction once the
// Timeline itself holds focus: Left/Right move the cursor one bucket
// and collapse the selection (anchor = cursor); Shift+Left/Right
// extend it — leaving the anchor exactly where it already was, never
// re-seeding it on "the first Shift press", since every *plain* move
// already keeps anchor and cursor equal, so whatever anchor was
// holding from the last plain move is already the correct start point
// the moment a Shift sequence begins (see moveLogAuditTimelineCursor).
// Enter/Space commit the current selection (single bucket or range)
// into the Time field, the same as a mouse release after a drag (see
// commitLogAuditTimelineSelection). Tab/Backtab move on to the table/
// Level field respectively — log.TimelineView is a
// *tview.TextView, not an *tview.InputField, so neither key routes
// through SetDoneFunc the way the three fields above it do; this is
// the only place either is handled at all for this widget. Escape
// closes the whole viewer, matching every other stop on this screen.
func (r *Root) captureLogAuditTimelineKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape:
		r.closeLogAuditViewer()
		return nil
	case tcell.KeyTab:
		r.app.SetFocus(r.log.ViewerTable)
		return nil
	case tcell.KeyBacktab:
		r.app.SetFocus(r.log.LevelField)
		return nil
	case tcell.KeyEnter:
		r.commitLogAuditTimelineSelection()
		return nil
	case tcell.KeyLeft:
		r.moveLogAuditTimelineCursor(-1, event.Modifiers()&tcell.ModShift != 0)
		return nil
	case tcell.KeyRight:
		r.moveLogAuditTimelineCursor(1, event.Modifiers()&tcell.ModShift != 0)
		return nil
	case tcell.KeyRune:
		switch event.Rune() {
		case ' ':
			r.commitLogAuditTimelineSelection()
			return nil
		case 'g':
			// "gC" reaches the AI Consigliere — see
			// startConsigliereChord's own doc comment in
			// consigliere.go. log.TimelineView is a *tview.TextView,
			// but every key this bar cares about (bucket selection via
			// Left/Right/Space/Enter above) is already fully custom —
			// tview's own built-in vi-style "g" (Home) would only ever
			// have scrolled a one-line visual bar with nothing
			// meaningful to scroll to, unlike captureLogAuditDetailKey's
			// own case below.
			r.startConsigliereChord(r.log.KeywordField)
			return nil
		}
	}
	return event
}

// moveLogAuditTimelineCursor moves the cursor by delta (±1 bucket),
// clamped to [0, bucketCount-1]. extend=false (a plain arrow)
// collapses the selection onto the new position (anchor = cursor);
// extend=true (Shift held) leaves the anchor untouched, so repeated
// Shift+arrow presses keep extending the same range from the same
// starting point.
func (r *Root) moveLogAuditTimelineCursor(delta int, extend bool) {
	n := r.log.TimelineBucketCount
	if n == 0 {
		return
	}
	c := r.log.TimelineCursor + delta
	if c < 0 {
		c = 0
	}
	if c >= n {
		c = n - 1
	}
	r.log.TimelineCursor = c
	if !extend {
		r.log.TimelineAnchor = c
	}
	r.repaintLogAuditTimeline()
}

// logAuditTimelineBucketAt maps an absolute screen column (as a mouse
// event reports it) to a bucket index, using the bar's own column
// logAuditTimelineLayout actually computed at the last unfrozen render
// (see log.TimelineBarCol's own doc comment on root.go) — never
// re-derived from the rendered string itself. ok is false for a column
// before the bar's own start, past its end, or while there's nothing
// to select at all (bucketCount == 0, e.g. the "no meaningful span"
// case).
func (r *Root) logAuditTimelineBucketAt(x int) (bucket int, ok bool) {
	if r.log.TimelineBucketCount == 0 {
		return 0, false
	}
	viewX, _, _, _ := r.log.TimelineView.GetInnerRect()
	col := x - viewX - r.log.TimelineBarCol
	if col < 0 || col >= r.log.TimelineBucketCount {
		return 0, false
	}
	return col, true
}

// captureLogAuditTimelineMouse drives the Timeline's own click/drag
// selection. MouseLeftDown on a bucket gives the Timeline real
// keyboard focus (a click alone doesn't otherwise move it there — the
// same gap dragSearchMouseCapture's own click-to-focus handling closes
// for the Find dialog) and starts a new single-bucket selection.
// MouseMove while a drag is in progress only moves the cursor and
// repaints the highlight — no commit, no re-bucketing, so the
// selection can be dragged leftward just as freely as rightward and
// the timeline never shifts under the cursor mid-drag (see
// log.TimelineDragging's own doc comment on root.go). MouseLeftUp,
// or a stray MouseMove that arrives with the button already released
// (the drag ended somewhere this capture never saw the button go up,
// e.g. outside the terminal's own reported bounds), both commit —
// mirroring Enter/Space — via commitLogAuditTimelineSelection, which
// is also what turns log.TimelineDragging back off.
func (r *Root) captureLogAuditTimelineMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	x, y := event.Position()

	if r.log.TimelineDragging {
		switch action {
		case tview.MouseMove:
			if event.Buttons()&tcell.ButtonPrimary == 0 {
				r.commitLogAuditTimelineSelection()
				return tview.MouseConsumed, nil
			}
			if b, ok := r.logAuditTimelineBucketAt(x); ok {
				r.log.TimelineCursor = b
				r.repaintLogAuditTimeline()
			}
			return tview.MouseConsumed, nil
		case tview.MouseLeftUp:
			r.commitLogAuditTimelineSelection()
			return tview.MouseConsumed, nil
		}
		return tview.MouseConsumed, nil // swallow everything else for the duration of the drag
	}

	if action == tview.MouseLeftDown {
		_, viewY, _, _ := r.log.TimelineView.GetInnerRect()
		if y != viewY {
			return action, event
		}
		b, ok := r.logAuditTimelineBucketAt(x)
		if !ok {
			return action, event
		}
		r.app.SetFocus(r.log.TimelineView)
		r.log.TimelineDragging = true
		r.log.TimelineAnchor = b
		r.log.TimelineCursor = b
		r.repaintLogAuditTimeline()
		return tview.MouseConsumed, nil
	}

	return action, event
}

// commitLogAuditTimelineSelection writes the current selection
// ([min(anchor,cursor), max(anchor,cursor)], always direction-
// independent regardless of which direction it was built in) into the
// Time field
// as "between <von> and <bis>" — the exact grammar
// filterexpr.ParseMtime already accepts elsewhere on this screen (see
// log.TimeField's own placeholder), with both ends floored/ceiled
// to the whole second: ParseMtime's own "between" is inclusive on both
// ends already (!t.Before(start) && !t.After(end)), but a bucket
// boundary can fall mid-second while every real timestamp here is
// second-granular, so rounding outward is what actually guarantees no
// entry at the edge is excluded regardless.
//
// Moves focus to the Time field *before* setting its text, not after:
// that fires restyleLogAuditFocusTargets (clearing log.TimelineFocused)
// before the explicit renderLogAuditViewer call below runs, so the
// timeline's own freeze-while-focused check (renderLogAuditTimeline)
// is already lifted by the time the newly-narrowed range needs to
// re-bucket and zoom in — a commit must never be blocked by the same
// freeze that protects an in-progress selection from follow mode.
//
// Renders immediately, unlike ordinary typing into this same field
// (see its own SetDoneFunc comment on why Time isn't live) — per the
// user's own explicit report that having to press Enter a second time
// after an already-deliberate mouse/keyboard selection was pure
// friction: a commit is already the equivalent of "done editing", the
// same one-shot action Enter itself is, not a partial keystroke.
func (r *Root) commitLogAuditTimelineSelection() {
	r.log.TimelineDragging = false
	if r.log.TimelineBucketCount == 0 {
		return
	}
	start := min(r.log.TimelineAnchor, r.log.TimelineCursor)
	end := max(r.log.TimelineAnchor, r.log.TimelineCursor)
	from, _ := logAuditBucketTimeRange(r.log.TimelineMinT, r.log.TimelineMaxT, r.log.TimelineBucketCount, start)
	_, to := logAuditBucketTimeRange(r.log.TimelineMinT, r.log.TimelineMaxT, r.log.TimelineBucketCount, end)
	from = from.Truncate(time.Second)
	if toFloor := to.Truncate(time.Second); !toFloor.Equal(to) {
		to = toFloor.Add(time.Second)
	} else {
		to = toFloor
	}

	r.app.SetFocus(r.log.TimeField)
	r.log.TimeField.SetText(fmt.Sprintf("between %s and %s",
		from.Format("2006-01-02 15:04:05"), to.Format("2006-01-02 15:04:05")))
	r.renderLogAuditViewer()
}

// captureLogAuditDetailKey: Escape is the only key this modal itself
// handles — same shape captureMessageDetailKey already establishes.
func (r *Root) captureLogAuditDetailKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeLogAuditDetail()
		return nil
	}
	// Alt+Arrow moves the dialog one cell at a time — the same
	// convention toolWindow's own InputHandler already establishes
	// (toolwindow.go), reused here rather than invented fresh, per the
	// user's own explicit request that this dialog become movable by
	// keyboard, not just by dragging its title bar.
	if event.Modifiers()&tcell.ModAlt != 0 {
		x, y, _, _ := r.log.DetailLayout.GetRect()
		switch event.Key() {
		case tcell.KeyLeft:
			r.moveLogAuditDetail(x-1, y)
			return nil
		case tcell.KeyRight:
			r.moveLogAuditDetail(x+1, y)
			return nil
		case tcell.KeyUp:
			r.moveLogAuditDetail(x, y-1)
			return nil
		case tcell.KeyDown:
			r.moveLogAuditDetail(x, y+1)
			return nil
		}
	}
	if event.Key() == tcell.KeyRune && event.Rune() == 'g' {
		// "gC" reaches the AI Consigliere — see startConsigliereChord's
		// own doc comment in consigliere.go. Deliberate trade-off, not
		// an oversight: log.DetailView is a genuinely scrollable
		// *tview.TextView (a full entry can run well past one screen),
		// and tview's own built-in vi-style bindings (textview.go) map
		// bare 'g' to "scroll to the very top" — claiming it here for
		// the chord loses that one specific jump. 'G' (scroll to the
		// very bottom), 'j'/'k'/'h'/'l', the arrow keys, and PageUp/
		// PageDown/Home/End are all a different rune or tcell.Key, so
		// none of them are affected.
		r.startConsigliereChord(nil)
		return nil
	}
	// Tab/Backtab into the AI Consigliere sidebar — the only other
	// stop on this screen, same reasoning
	// captureLogAuditSelectionTableKey's own identical case gives.
	// Neither key was ever a built-in TextView binding to begin with
	// (tview's own vi-style keys are 'g'/'G'/'j'/'k'/'h'/'l', not
	// Tab/Backtab — see textview.go), so nothing existing is lost.
	if r.consigliere.Visible && (event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab) {
		if event.Key() == tcell.KeyBacktab && r.consigliere.Scrollbar.maxOffset() > 0 {
			r.app.SetFocus(r.consigliere.History)
		} else {
			r.app.SetFocus(r.consigliere.Input)
		}
		return nil
	}
	return event
}

// applyLogAuditTheme themes all three Log Audit pieces — split out of
// applyTheme the same way applyActivityLogTheme already is.
func (r *Root) applyLogAuditTheme(theme config.ResolvedTheme) {
	if r.log.Table == nil {
		return
	}
	// PanelBackground, not SurfaceBackground — see mounts.Layout's own
	// identical comment (mounts.go) for the full reasoning.
	r.log.SelectionLayout.SetBackgroundColor(theme.PanelBackground)
	r.log.Table.SetBackgroundColor(theme.PanelBackground)
	// Without this, tview's own default selected-cell style just
	// reverses each cell's own foreground into a background instead of
	// using one consistent selection bar — the same fix already applied
	// to ssh.KeysTable/connectionMenuTable/sed.PreviewTable/tabSwitcher/
	// gitBreach's tables, for the identical reason.
	r.log.Table.SetSelectedStyle(tcell.StyleDefault.
		Background(theme.SelectionBackground).
		Foreground(theme.TextColor))
	r.log.TitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.log.TitleBar.SetTextColor(theme.TextColor)
	// SurfaceBackground/TextColor, matching the main screen's own
	// buttonBar/statusBar — see mounts.Hint's own identical comment.
	r.log.Hint.SetBackgroundColor(theme.SurfaceBackground)
	r.log.Hint.SetTextColor(theme.TextColor)
	hintText, hintSpans := buildListHint(theme, logAuditSelectionHintEntries())
	r.log.Hint.SetText(hintText)
	r.log.HintSpans = hintSpans
	r.renderLogAuditSelection() // cell colors are baked in per cell, not looked up live at draw time

	// PanelBackground, not SurfaceBackground — see mounts.Layout's own
	// identical comment (mounts.go) for the full reasoning.
	r.log.ViewerLayout.SetBackgroundColor(theme.PanelBackground)
	r.log.ViewerTable.SetBackgroundColor(theme.PanelBackground)
	// Same fix as log.Table just above, for the same reason.
	r.log.ViewerTable.SetSelectedStyle(tcell.StyleDefault.
		Background(theme.SelectionBackground).
		Foreground(theme.TextColor))
	r.log.ViewerTitle.SetBackgroundColor(theme.InputFocusedBackground)
	r.log.ViewerTitle.SetTextColor(theme.TextColor)
	// log.FilterRow themed explicitly to the same PanelBackground
	// the table below it now uses, rather than left at tview's own
	// default — see log.FilterRow's own doc comment (root.go) for
	// the visual mismatch this fixes.
	r.log.FilterRow.SetBackgroundColor(theme.PanelBackground)
	// log.TimelineView deliberately does NOT match SurfaceBackground
	// while unfocused — per the user's own explicit report, a density
	// strip that reads as part of the same surface as the list below it
	// is confusing; it needs its own, visibly different tone.
	// PopupBackground is reused here rather than a plain hardcoded
	// color: it is already this app's own "distinct secondary surface"
	// role (the Detail/Statistics modals both use it — see
	// applyLogAuditTheme below), and darker than SurfaceBackground in
	// the default scheme, fixing the user's second report ("zu hell")
	// in the same change. Focused/unfocused styling for all five stops
	// (three fields, the timeline, the table's own focus indicator) now
	// goes through one shared place — restyleLogAuditFocusTargets — fed
	// here with whichever one actually has focus right now, the one
	// case that isn't itself a focus transition and so has nothing else
	// to pass in (see logAuditCurrentlyFocused's own doc comment).
	r.restyleLogAuditFocusTargets(r.logAuditCurrentlyFocused())
	// SurfaceBackground/TextColor, matching the main screen's own
	// buttonBar/statusBar — see mounts.Hint's own identical comment.
	r.log.ViewerHint.SetBackgroundColor(theme.SurfaceBackground)
	r.log.ViewerHint.SetTextColor(theme.TextColor)
	viewerHintText, viewerHintSpans := buildListHint(theme, logAuditViewerHintEntries())
	r.log.ViewerHint.SetText(viewerHintText)
	r.log.ViewerSpans = viewerHintSpans
	r.renderLogAuditViewer()

	r.log.DetailLayout.SetBackgroundColor(theme.PopupBackground)
	r.log.DetailView.SetBackgroundColor(theme.PopupBackground)
	r.log.DetailView.SetTextColor(theme.TextColor)
	r.log.DetailTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.log.DetailTitleBar.SetTextColor(theme.TextColor)

	r.applyLogAuditStatsTheme(theme)
}
