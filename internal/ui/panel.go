package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/search"
)

const (
	headerDisplayPage = "display"
	headerEditPage    = "edit"
)

// Table columns — colSize/colModified are also mirrored, cell for cell,
// in columnHeader (see buildColumnHeader): the icon columns get a blank
// header cell (self-explanatory via their own glyphs), Name/Size/
// Modified get a clickable label that sorts by that column.
const (
	colCheckbox = iota
	colType
	colModifier
	colName
	colSizeSep // a bare "│" — see columnSeparator — not a real data column
	colSize
	colModifiedSep // likewise, before colModified
	colModified
)

// Panel is the single directory-listing view: a one-line, browser-
// address-bar-style path header above a table of entries. The path
// header has Start/Home/Back/Forward buttons followed by the path, whose
// components are individually clickable (clicking "b" in /a/b/c/d jumps
// to /a/b); clicking anywhere else in it (e.g. the empty space after the
// path) switches it to a plain editable text field, like a browser URL
// bar. Below that sits a second, one-line column-header row (see
// columnHeader/buildColumnHeader) labelling Name/Size/Modified — clicking
// one sorts by it. The table itself is navigable with the arrow keys
// (built into tview.Table) and Enter, has a clickable checkbox column for
// marking entries, and (via Root) a right-click context menu. No borders
// anywhere — a background color set apart from the plain panel (see
// theme/paintStaticChrome) does the same job without the box-drawing
// look, which reads as dated.
//
// A tview.Table, not tview.List: a checkbox column needs a second column
// at all, which List doesn't have, and Table.CellAt/TableCell.Reference
// give exact hit-testing and per-row data for free — no more hand-rolled
// coordinate math (see RowAt, and contrast with Phase 1's since-removed
// EntryAt, which had to reimplement List's own unexported indexAtPoint).
// historyEntry is one entry in Panel's own browser-style navigation
// history (see history/historyIdx below) — either a real directory, or
// a frozen snapshot of a search-results listing exactly as it stood the
// moment the panel navigated away from it (see snapshotCurrentEntry),
// so Back/Forward into it looks like nothing ever happened rather than
// silently re-running a search that might behave differently by then,
// or take a while.
//
// cursorRow is captured for both alike (see snapshotCurrentEntry), but
// only actually restored by restoreHistoryEntry for a search
// snapshot — the same "nothing ever happened" guarantee doesn't apply
// to a real directory, which per the user's own explicit request
// always lands on row 0 on Back/Forward now, exactly like a fresh
// navigate() into it already would (see load's own newDirectory
// check), never wherever the cursor happened to be when it was last
// left. Still captured unconditionally rather than only when
// isSearch() is true, since which one entry will turn out to be isn't
// decided until whatever navigates away from it next.
type historyEntry struct {
	path      string // "" marks a search-results snapshot — see isSearch
	cursorRow int

	// Populated only when isSearch() is true — see
	// snapshotCurrentEntry/restoreHistoryEntry.
	searchEntries []searchResultEntry
	searchStatus  string
	searchColor   tcell.Color
}

// isSearch reports whether e is a frozen search-results snapshot rather
// than a real directory.
func (e historyEntry) isSearch() bool { return e.path == "" }

type Panel struct {
	*tview.Flex

	app *tview.Application

	// theme is the panel's own copy of the active color scheme (see
	// paintStaticChrome/applyTheme, and Root's own theme field) — needed
	// here too, not just on Root, since entryColor/columnSeparator/addRow
	// color each row's cells directly rather than through a shared parent
	// widget's own single color.
	theme config.ResolvedTheme

	headerPages *tview.Pages
	header      *tview.TextView   // display mode: buttons + path breadcrumbs
	headerEdit  *tview.InputField // edit mode: raw, freely editable path

	// filterField and filterRegexBtn back the glob/regex filter — a
	// live narrow-the-listing box, and the toggle between its two
	// matching modes (see filterByText). Both still exist as real,
	// fully working widgets exactly as before, but per the user's own
	// explicit request no longer sit directly in the header row — they
	// only ever appear embedded in the filter-menu dropdown while it's
	// open (see Root.openFilterMenu), which is what actually adds them
	// to a layout on each open; Panel just owns and keeps them, the
	// same "owned here, shown elsewhere" split filterMenuBtn's own
	// click handler and Root.openFilterMenu together make possible.
	// filterText/filterRegex are the field/button's current values,
	// read by load() on every call; filterField's own SetChangedFunc is
	// what actually drives a live reload as the user types (see
	// NewPanel).
	//
	// filterGlobActive/filterSizeActive/filterMtimeActive are
	// independent on/off toggles for the filter-menu's own three rows
	// (glob/regex, size, modified-time) — per the user's own explicit
	// request to be able to combine them rather than only ever having
	// one filter kind active at a time. filterGlobActive defaults to
	// true (see NewPanel) and auto-flips true again the moment
	// filterField's own text goes from empty to non-empty (see its own
	// SetChangedFunc) — the existing "type to filter, instantly"
	// muscle memory keeps working completely unchanged for anyone who
	// never opens the dropdown at all; the checkbox is only for the
	// "temporarily disable without losing what's already typed" case
	// that behavior alone can't cover, and filterByText's own new
	// active parameter is what actually honors it. filterSizeActive/
	// filterMtimeActive are pure UI toggles for now, read only by
	// renderFilterMenuBtn's own active-count indicator — no filtering
	// logic reads either yet, per the user's own explicit "it's enough
	// to be able to turn them on and off in the dropdown for now".
	// layout is the current column sizing (see columns.go), recomputed
	// whenever the panel's width or the data format changes.
	layout columnLayout

	filterField       *tview.InputField
	filterRegexBtn    *tview.Button
	filterText        string
	filterRegex       bool
	filterGlobActive  bool
	filterSizeActive  bool
	filterMtimeActive bool

	// filterPersistent mirrors config.Settings.FilterPersistent (see its
	// own doc comment there for the full reasoning) — set once from it
	// in NewPanel, updated for every open tab together by
	// Root.setFilterPersistent, exactly the same shape showHidden/
	// sizeBytes/mtimeUnix already follow. Read only by load's own
	// newDirectory branch: true (the default) skips resetting
	// filterText/filterGlobActive/filterSizeActive/filterMtimeActive
	// there at all, so whatever filter was active carries straight into
	// the new directory; false restores the original, pre-this-setting
	// behavior of always starting a freshly navigated directory
	// unfiltered.
	filterPersistent bool

	// filterMatchesNothing is true whenever load's own filterByText call
	// hid every single entry a directory would otherwise have shown —
	// set there, read only by renderFilterMenuBtn (see its own doc
	// comment for what it does with this). A real, user-reported gap
	// filterPersistent's own arrival exposed: a filter carried over from
	// browsing an entirely different, unrelated directory (see
	// config.Settings.FilterPersistent's own doc comment) can silently
	// hide everything in a directory it was never meant to apply to —
	// files freshly pasted in from another tab, say — leaving a listing
	// indistinguishable from a genuinely empty folder unless you already
	// know to check the filter-menu's own indicator. Deliberately not
	// set for a directory that's simply, actually empty to begin with
	// (filterByText is a no-op then regardless — see load's own
	// computation of this field for exactly how that distinction is
	// made) — this is about a filter actively hiding something, not
	// about there being nothing there in the first place.
	filterMatchesNothing bool

	// filterMenuBtn replaces filterField/filterRegexBtn's own old,
	// always-visible slot in the header row — a compact "Nx Y" button
	// (see renderFilterMenuBtn), "Y" chosen for its own passing
	// resemblance to a funnel and "N" naming how many of the three
	// filter-menu rows are currently active, omitted entirely while
	// none are (see the user's own explicit request for exactly this
	// shape). Clicking it (or activating it from the keyboard) opens
	// the dropdown (see onOpenFilterMenu/Root.openFilterMenu) that now
	// holds everything filterField/filterRegexBtn/the two not-yet-built
	// size/modified-time filters need — freeing most of the header row
	// back to the path itself, which is the entire point: the six nav
	// buttons just to its left grew considerably wider becoming real
	// buttons (see buildHeaderSpans), and this is what pays for that
	// space back.
	filterMenuBtn    *tview.TextView
	onOpenFilterMenu func()

	// detailsExpandBtn sits right after filterMenuBtn in the same header
	// row (see NewPanel) — a "<" button that expands the Details
	// sidebar, per the user's own explicit request for a mouse
	// alternative to "I"/the Details button: filterField itself gave up 3 columns
	// (headerFilterWidth) to make room for this button's own 3-column
	// slot ("space, <, space" — tview.Button centers its own label
	// within whatever width it's given, so a plain "<" label already
	// reads exactly that way with no extra padding logic needed here).
	// onExpandDetails is Root's own wiring (showDetailsSidebar) — Panel
	// has no direct reference to Root, the same reason
	// onOpenFile/onRenameGesture/onOpenSearchResult all exist.
	detailsExpandBtn *tview.Button
	onExpandDetails  func()

	// tabStrip is the compact row of tab numbers sitting between
	// filterField and detailsExpandBtn (see tabstrip.go for the whole
	// design, and NewPanel for the header row it's added to) — per the
	// user's own explicit request for that exact position. headerRow is
	// kept as a field purely so refreshTabStrip can ResizeItem the strip's
	// own slot as the tab count changes; nothing else needs it.
	//
	// tabCount/tabActive are what setTabs was last told — Panel renders
	// the tab set but never owns it (see setTabs' own doc comment: Root
	// does). tabStripSpans maps a click's column back to a tab, the same
	// shape headerSpans uses for the path bar.
	//
	// onSelectTab/onNewTab/onOpenTabSwitcher are Root's own wiring, the
	// same reason onExpandDetails/onOpenFile/onRenameGesture all exist:
	// Panel has no reference to Root and no business deciding what
	// switching a tab actually entails.
	headerRow         *tview.Flex
	tabStrip          *tview.TextView
	tabStripSpans     []tabStripSpan
	tabCount          int
	tabActive         int
	onSelectTab       func(int)
	onNewTab          func()
	onOpenTabSwitcher func()

	// columnHeader is a second, single-row table sitting between the path
	// bar and the data table (table) — its own doc comment (see
	// buildColumnHeader) explains why a second table, not a shared row 0
	// of table itself (tview.Table.SetFixed would do that, but at the
	// cost of shifting every existing row index used throughout this
	// package and its tests by one).
	columnHeader *tview.Table
	table        *tview.Table

	// path is the absolute path currently shown.
	path string

	// selected holds the absolute paths currently checked in the checkbox
	// column. Reset on every successful load() — selection is scoped to
	// the directory currently on screen, not carried across navigation,
	// matching how most file managers treat it.
	selected map[string]bool

	// clipboardPaths/clipboardCut mirror the app-wide clipboard's own
	// state (see Root.clipboard/clipboardCut) — pushed down explicitly
	// by setClipboard rather than read from Root directly, since Panel
	// has no reference to Root (see onExpandDetails's own doc comment
	// for why) and the clipboard itself is one Root-level value shared
	// by every open tab, not something each Panel owns. Unlike
	// selected above, survives a load(): the clipboard isn't scoped to
	// whatever directory happens to be on screen, so navigating away
	// and back must still show the same rows highlighted.
	clipboardPaths map[string]bool
	clipboardCut   bool

	// headerSpans locates each clickable region in the header's display
	// text (see buildHeaderSpans), rebuilt on every load().
	headerSpans []headerSpan

	// history is a browser-style navigation history: history[historyIdx]
	// is the current entry — a real directory or a frozen search-results
	// snapshot alike (see historyEntry). navigate() and showSearchResults
	// both append to it (truncating any forward entries first);
	// back()/forward() only move historyIdx, restoring whichever entry
	// they land on (see restoreHistoryEntry).
	history    []historyEntry
	historyIdx int

	// sortKey/sortDescending, sizeBytes/mtimeUnix, and showHidden are all
	// session-scoped display preferences, not per-directory state:
	// load() re-applies whichever is currently set on every call,
	// including when navigating to a new directory, so all of them stick
	// as you browse rather than resetting each time.
	//
	// sortKey/sortDescending pick which of ListDir's fields to sort by
	// and which direction — see applySortPreference, and
	// buildColumnHeader/setSortKey for how a column-header click changes
	// them. Directories always stay grouped first either way; only the
	// order within that group (and within the files that follow) changes.
	// Not persisted across restarts, unlike the three below — sorting by
	// something other than name is more of a one-off, in-the-moment need.
	//
	// sizeBytes/mtimeUnix pick the Size/Modified columns' display format
	// — see formatSizeCell/formatModTimeCell — toggled via Root's
	// "Globals" menu (Root.toggleSizeBytes/toggleMtimeUnix), not from the
	// column header itself: a click there is unambiguously "sort by this
	// column", with no separate click zone competing for the same cell.
	//
	// showHidden: when false, dotfile entries (name starting with ".")
	// are filtered out of the listing entirely in load() — see
	// filterHidden — rather than kept as rows that are merely skipped
	// elsewhere. That's what makes every row-based operation (selectAll,
	// selectByPattern, arrow-key navigation, ...) exclude them for free
	// once hidden, without each one needing its own "is this row actually
	// hidden right now" check.
	//
	// All three are seeded in NewPanel from the on-disk settings (see
	// config.Settings.ShowHidden/SizeBytes/MtimeUnix, and
	// config.DefaultSettings for what a fresh install starts with), and
	// persisted back to it on every toggle (see Root.toggleHidden/
	// toggleSizeBytes/toggleMtimeUnix) — per the user's own request,
	// breakthrough remembers the last session's choice for these three
	// specifically, rather than always resetting to the built-in default.
	sortKey        sortKey
	sortDescending bool
	sizeBytes      bool
	mtimeUnix      bool
	showHidden     bool

	// onError reports failures the user should see (a directory that
	// can't be read, a refused rename) to whoever owns the UI's error
	// display — Root wires this to its error overlay. Panel deliberately
	// doesn't own that overlay itself: it has no business deciding how
	// errors are presented, only which ones are worth reporting.
	onError func(error)

	// onLoad reports every successful load() (navigation, a hidden-files
	// toggle, anything that reloads the directory currently on screen) —
	// Root wires this to refresh the status bar's disk-usage display,
	// which depends on the current directory. Not called for the very
	// first load (inside NewPanel, before Root has anything to wire this
	// to yet) — Root re-syncs once explicitly right after wiring it
	// instead, the same "defensive initial sync" showMenu already does
	// for its own menu labels.
	onLoad func(path string)

	// editing is true while the header's edit field is shown. Root's
	// captureMouse calls captureOutsideEdit before its own logic (only
	// one SetMouseCapture can be installed on Panel, and Root already
	// owns that slot for right-click detection), so it needs this to
	// know whether a click landing outside the edit field should cancel
	// editing.
	editing bool

	// searchMode is true exactly while the panel is showing search
	// results in place of its own real directory — see
	// showSearchResults and its related methods just below rowRef.
	// Every other method here that would otherwise misbehave against a
	// virtual, scattered-directories listing (activateRow's usual "cd
	// into it" meaning, setSortKey's usual "re-read from disk" meaning,
	// the filter box, a header click) checks this first.
	searchMode bool

	// searchEntries holds every result gathered so far while
	// searchMode is true — see appendSearchResult/renderSearchEntries.
	searchEntries []searchResultEntry

	// searchStatusText/searchStatusColor mirror whatever setSearchStatus/
	// setSearchStatusColor most recently painted onto the header — kept
	// here too (not just on the widget) so snapshotCurrentEntry has
	// something to freeze into a historyEntry when the panel navigates
	// away from search results still in progress or just finished.
	searchStatusText  string
	searchStatusColor tcell.Color

	// searchBrowsePath is the real breadcrumb setSearchStatus paints
	// after the status text itself (see its own doc comment) — a normal,
	// fully clickable/editable path, independent of p.path (which search
	// mode leaves frozen throughout — see showSearchResults' own doc
	// comment), so the user isn't limited to Escape or jumping to a
	// specific result to leave search results behind, per their own
	// explicit report. Defaults to p.path when showSearchResults itself
	// first enters search mode; Root.runSearch/showSearchError then set
	// it to the search's own actual scope, which they alone know.
	// searchHeaderOffset is the column setSearchStatus's own breadcrumb
	// starts at within the header text, so captureHeaderMouse can tell a
	// click on the status-text prefix (never a path to edit) apart from
	// one on — or past — the breadcrumb itself.
	searchBrowsePath   string
	searchHeaderOffset int

	// onSearchEscape reports Escape while searchMode is true and the
	// table itself has focus (see captureTableKey) — Root wires this to
	// reopening the search form, the same "Esc: back to search"
	// contract the dialog already promises, just reached from inside
	// the panel itself now rather than a separate results overlay. Left
	// nil (a no-op) in tests that construct a Panel without wiring Root
	// to it at all.
	onSearchEscape func()

	// onOpenSearchResult reports activateRow (Enter/left-click) on a
	// content-search result specifically (see rowRef.searchLine) — Root
	// wires this to opening the file in the configured editor, at that
	// line, per the user's own explicit request that a content match
	// open the file instead of just jumping to it in its own real
	// directory the way a filename match still does (see activateRow's
	// own searchMode branch). Left nil the same as onSearchEscape.
	onOpenSearchResult func(path string, line int)

	// onExitSearchResults reports activateRow leaving search mode by
	// jumping to a filename (or archive-member) match's own real
	// location — every case besides onOpenSearchResult's own "stay in
	// search mode" one above. Root wires this to cancelSearch: a real
	// user report — clicking a result to jump to it, then watching the
	// still-running search silently overwrite the panel with search
	// results again moments later, because nothing had actually told it
	// to stop. Left nil the same as onSearchEscape/onOpenSearchResult.
	onExitSearchResults func()

	// lastNameClickRow/lastNameClickTime back handleNameClick's own
	// click/double-click/rename-gesture disambiguation (see its own doc
	// comment): the previous name-cell click's row and timestamp,
	// tracked independently of whatever row the table's own selection
	// currently sits on (arrow-key navigation moves that too, without
	// this ever having been clicked at all) — only a genuine *second*
	// click landing on the same row within the relevant window counts
	// as a repeat. lastNameClickRow starts at -1 (see NewPanel): 0 is a
	// real, valid row index, and would otherwise make the very first
	// click of a session misread as a repeat of a click that never
	// happened.
	lastNameClickRow  int
	lastNameClickTime time.Time

	// onRenameGesture reports the click-pause-click rename gesture once
	// handleNameClick recognizes it — Root wires this to renameRow, its
	// own row-addressed equivalent of renameCurrentEntry. Left nil the
	// same as onSearchEscape/onOpenSearchResult if nothing's wired it up
	// (e.g. a test constructing a Panel directly).
	onRenameGesture func(row int)

	// onOpenFile reports activateRow landing on a plain file while
	// plainly browsing (not searchMode — see its own branch there) —
	// Root wires this to openLook, per the user's own explicit request
	// that Enter/double-click on a file try Look, the same way they
	// already navigate into a directory. No path parameter: by the time
	// this runs the table's own cursor is already on the row in
	// question (Enter's own SetSelectedFunc row *is* the cursor;
	// captureMouse's MouseLeftDoubleClick case calls focusRow first),
	// so openLook's own CurrentRowPath read already targets the right
	// file, the same self-contained shape every other *Shortcut method
	// already has. Left nil the same as onRenameGesture if nothing's
	// wired it up.
	onOpenFile func()

	// onDescribeRows lets Root override display names and Modified-
	// column times for the directory load() is about to render, plus
	// what the Modified column itself should be called while doing so
	// (see inTrashView below) — wired to Root.describeTrashRows for
	// browsing the trash's own files/ directory, whose real on-disk
	// names (a collision-avoidance hash) and mtimes (the file's own
	// last-edit time, before it was ever trashed) are internal
	// implementation details, not what a user browsing the trash
	// actually wants to see — the item's own original path and deletion
	// time are, per the user's own explicit report that two items
	// trashed from the very same location, more than once, otherwise
	// stay indistinguishable. Called once per load(), not once per row
	// (see load's own doc comment on why). Left nil for a Panel that
	// doesn't need this (returns a nil map, isTrashDir false, same as
	// what's returned when this itself is nil).
	onDescribeRows func(dir string) (descriptions map[string]rowDescription, isTrashDir bool)

	// inTrashView mirrors onDescribeRows' own isTrashDir return from the
	// most recent load() — buildColumnHeader reads this to label the
	// Modified column "Deletion time" instead of its usual "Modify time
	// (mtime)" while it's true.
	inTrashView bool
}

// rowDescription overrides one row's own display name and Modified-
// column time — see Panel.onDescribeRows.
type rowDescription struct {
	name    string
	modTime time.Time
}

// headerAction identifies what a headerSpan does when clicked.
type headerAction int

const (
	actionNavigate headerAction = iota // go to target
	actionStart                        // go to the directory breakthrough was launched from
	actionRoot                         // go to the filesystem root ("/")
	actionHome                         // go to the user's home directory
	actionBack                         // step back in history
	actionForward                      // step forward in history
	actionUp                           // go up one level (the parent directory)
	actionReload                       // re-read the current directory from disk
)

// headerSpan is one clickable region in the header's display text:
// [start, end) is its half-open column range (relative to the header's
// own inner rect).
type headerSpan struct {
	start, end int
	action     headerAction
	target     string // only meaningful for actionNavigate
}

// rowRef is attached to each row's name cell via TableCell.SetReference,
// so a row's data can be read straight off the cell instead of being
// re-derived from its displayed text or its index.
type rowRef struct {
	path      string // absolute path; for the ".." row, its parent directory
	name      string // display name, ".." for the parent-directory row
	isDir     bool
	checkable bool // false for "..", which can't be a file operation target

	// entryType/linkTarget/nlink/mountPoint/mode/unreadable mirror the
	// corresponding fsops.Entry fields — see typeGlyph, modifierGlyph,
	// and entryColor, the only things that read them, to render the type
	// and modifier columns and pick the name's own color. The ".." row
	// gets a plain TypeDir with nothing else set (see load): distinguishing
	// whether going up crosses a filesystem boundary, or isn't listable,
	// would need an extra stat purely for that row, not worth it for
	// what's otherwise always just "..". unreadable's own zero value
	// (false) is the deliberately safe default here for exactly that
	// reason — see fsops.Entry.Unreadable's own doc comment.
	entryType  fsops.EntryType
	linkTarget string
	nlink      uint64
	mountPoint bool
	mode       os.FileMode
	unreadable bool

	// size/modTime mirror fsops.Entry's own fields, for the Size/Modified
	// columns (see addRow/formatSizeCell/formatModTimeCell).
	size    int64
	modTime time.Time

	// searchLine is > 0 for a content-search result row specifically
	// (see searchResultEntry/renderSearchEntries) — the matched line
	// number, read by activateRow's own searchMode branch to open the
	// file there instead of just jumping to it. Always 0 for every
	// other row: a real directory entry, or a filename-search result.
	searchLine int

	// archiveHit is true for a filename- or content-search result found
	// *inside* an archive (search.Result.ArchiveMember — see
	// appendSearchResult) — path is still the real, containing archive
	// file. For a filename match, activateRow's usual "Go to file/
	// folder" already does the right thing with it unchanged (see its
	// own doc comment); a content match inside an archive member gets
	// the same "Go to file/folder" treatment too, specifically because
	// this is set (see activateRow's own searchLine check) — entryColor
	// also reads this, to set such a row's own name apart from a real
	// file/directory match, per the user's own explicit request.
	archiveHit bool
}

// NewPanel creates a Panel rooted at path, themed per theme (see
// paintStaticChrome/applyTheme — Root resolves this once at startup from
// the on-disk color scheme, see loadInitialSettings, and again on a live
// scheme switch), with the "Globals" toggles (see Panel.showHidden/
// sizeBytes/mtimeUnix) seeded from settings — the same on-disk source as
// theme, so breakthrough starts up remembering the last session's
// choice (see Root.toggleHidden/toggleSizeBytes/toggleMtimeUnix, which
// persist a change back to it) instead of always resetting to
// config.DefaultSettings' own built-in default. app is needed to move
// keyboard focus into the header's edit field on click and back to the
// list afterwards — see Panel.openEdit.
//
// headerFilterWidth/headerDetailsExpandWidth are filterField's and
// detailsExpandBtn's own fixed widths in the header row (see below) —
// named rather than inline literals since the two are directly related:
// filterField gave up exactly headerDetailsExpandWidth columns (20 to
// 17) to make room for detailsExpandBtn's own 3-column "space, <,
// space" slot right after it, per the user's own explicit request.
const (
	headerFilterWidth        = 17
	headerDetailsExpandWidth = 3

	// filterMenuBtnWidth is filterMenuBtn's own fixed width in the
	// header row — enough for the widest indicator ("3x", since at most
	// three filter-menu rows can ever be active at once) plus the
	// three-column " Y " button itself, with the button always flush
	// against this slot's own right edge (see renderFilterMenuBtn) so
	// it never shifts as the indicator appears/disappears alongside it.
	filterMenuBtnWidth = 6

	// headerTabStripGap is one column of lead-in the tab strip draws for
	// itself before its own first glyph ("+", or the first tab number) —
	// without it that glyph sat flush against the filter box with no
	// visual breathing room at all, per a real user report once the
	// strip started always showing at least the "+" button (see
	// tabstrip.go's own doc comment on why that button is never hidden).
	//
	// Drawn as part of the strip's own text (see refreshTabStrip), not a
	// separate blank Flex item alongside it: a plain nil spacer item has
	// no background color of its own to set, so it painted as a visibly
	// different-colored seam right in front of the strip instead of
	// reading as part of it — a second real report, this time about the
	// fix for the first one.
	headerTabStripGap = 1
)

func NewPanel(app *tview.Application, path string, theme config.ResolvedTheme, settings config.Settings) (*Panel, error) {
	p := &Panel{
		Flex:             tview.NewFlex().SetDirection(tview.FlexRow),
		app:              app,
		theme:            theme,
		table:            tview.NewTable(),
		columnHeader:     tview.NewTable(),
		showHidden:       settings.ShowHidden,
		sizeBytes:        settings.SizeBytes,
		mtimeUnix:        settings.MtimeUnix,
		filterPersistent: settings.FilterPersistent,
		lastNameClickRow: -1, // see its own doc comment: 0 is a real row, -1 isn't
	}
	p.table.SetBorders(false)
	// Re-lay the columns out whenever the panel's width changes — a
	// terminal resize, split view opening or flipping, the Details
	// sidebar appearing. A draw callback rather than a hook on each of
	// those, because it catches every cause including ones added later,
	// and because the width is only actually knowable at draw time.
	//
	// The usual objection to computing in a draw callback — that what it
	// sets lands one frame late (see Panel.setSelectionStyle, which had
	// exactly that bug) — does not apply here: tview's Table.Draw calls
	// DrawForSubclass, and so this callback, *before* it lays out and
	// paints any cell (verified in tview's own table.go, not assumed), so
	// cells changed here are painted this frame. It also can't loop —
	// relayoutColumns returns without touching anything when the width
	// hasn't changed.
	//
	// The width comes in as a parameter rather than from GetRect: an
	// undrawn table still reports a small non-zero rect (15 columns, as
	// it happens), which is indistinguishable from a genuinely narrow
	// pane and would shorten every name against a width that was never
	// real.
	p.table.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		p.relayoutColumns(width)
		return x, y, width, height
	})
	p.table.SetSelectable(true, false) // whole rows, not individual cells
	p.table.SetSelectedFunc(func(row, column int) { p.activateRow(row) })
	p.table.SetInputCapture(p.captureTableKey) // space toggles the checkbox
	p.table.SetFocusFunc(func() { p.setSelectionStyle(true) })
	p.table.SetBlurFunc(func() { p.setSelectionStyle(false) })

	p.columnHeader.SetBorders(false)
	p.columnHeader.SetSelectable(false, false) // labels only, not a second navigable row
	p.columnHeader.SetMouseCapture(p.captureColumnHeaderMouse)

	p.header = tview.NewTextView()
	p.header.SetWrap(false)
	p.header.SetDynamicColors(true) // the six nav buttons' own ButtonBackground padding is a color tag — see buildHeaderSpans
	p.header.SetMouseCapture(p.captureHeaderMouse)

	p.headerEdit = tview.NewInputField()
	p.headerEdit.SetDoneFunc(p.finishEdit)
	// A label, not extra logic in openEdit: InputField reserves exactly
	// this much space before the field itself starts (see tview's own
	// Draw, which measures labelWidth via TaggedStringWidth when none is
	// set explicitly) — the path being edited lines up with the same
	// path already shown in p.header, instead of resetting to column 0
	// the moment editing starts, per the user's own explicit report. See
	// headerButtonPrefix's own doc comment for why this is a named
	// constant rather than a literal repeated here.
	p.headerEdit.SetLabel(headerButtonPrefix)

	p.headerPages = tview.NewPages()
	p.headerPages.AddPage(headerDisplayPage, p.header, true, true)
	p.headerPages.AddPage(headerEditPage, p.headerEdit, true, false)

	p.filterRegexBtn = tview.NewButton(filterModeLabel(false))
	p.filterRegexBtn.SetSelectedFunc(func() {
		if p.searchMode {
			return // the filter box doesn't apply to search results — see Panel's own searchMode doc comment
		}
		p.filterRegex = !p.filterRegex
		p.filterRegexBtn.SetLabel(filterModeLabel(p.filterRegex))
		p.reportError(p.load(p.path))
	})

	p.filterField = tview.NewInputField()
	p.filterField.SetPlaceholder("filter")
	// filterGlobActive starts true: the existing "type to filter,
	// instantly" behavior stays exactly as familiar as before for
	// anyone who never opens the filter-menu dropdown at all (see the
	// struct's own doc comment on filterGlobActive) — it only needs
	// deliberately turning off once, from inside that dropdown, the
	// first time someone actually wants a typed pattern to stop
	// applying without losing what they typed.
	p.filterGlobActive = true
	p.filterField.SetChangedFunc(func(text string) {
		if text == p.filterText {
			return // triggered by load()'s own reset SetText, not real typing — see its doc comment
		}
		if p.searchMode {
			return // see filterRegexBtn's own identical guard just above
		}
		p.filterText = text
		if text != "" {
			// Going from empty to non-empty is as deliberate a signal
			// as pressing the dropdown's own checkbox would be — auto-
			// reactivating here is what keeps typing into the field
			// alone enough, without an extra click, for anyone who
			// never explicitly turned it off. Never auto-*deactivates*
			// on the reverse transition, deliberately: filterByText
			// already treats empty text as a no-op regardless (see its
			// own doc comment), so there's nothing left to preserve by
			// forcing the flag off too — and doing so would fight
			// anyone who explicitly unchecked it while text happened to
			// still be there, the moment they then cleared that text.
			p.filterGlobActive = true
		}
		p.reportError(p.load(p.path))
	})
	// No SetDoneFunc set here, unlike most other fields this app builds
	// in their own NewX constructor: filterField only ever actually
	// receives focus once it's embedded inside the filter-menu dropdown
	// (see Root.renderFilterMenu, which rebuilds and rewires the whole
	// dropdown fresh on every open) — never directly, the way an early
	// version of this binding once did (see openFilterMenu's own doc
	// comment on the "/" plain command's own fix). renderFilterMenu is
	// what sets a real one there instead, closing over that render
	// pass's own keyboard focus-cycling order and close callback —
	// something a fixed, one-time SetDoneFunc set here could never do.

	// filterMenuBtn is what actually sits in the header row now — see
	// its own doc comment on the struct for the full reasoning.
	// renderFilterMenuBtn (called from load(), and from Root's own
	// filter-menu toggle handlers) fills in its real text; this just
	// wires the click.
	p.filterMenuBtn = tview.NewTextView().SetDynamicColors(true)
	p.filterMenuBtn.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		// Checked first, before anything else — a real, user-reported
		// regression otherwise: tview.Flex.MouseHandler (verified
		// directly against its own flex.go, not assumed) never checks a
		// child's own rect itself before calling its MouseHandler; it
		// simply calls every item in headerRow, in order, until one
		// consumes the event. filterMenuBtn sits before tabStrip and
		// detailsExpandBtn there, so without this check it swallowed
		// every click meant for either of them too, regardless of where
		// it actually landed — every other mouse capture in this
		// package already guards this way (see captureHeaderMouse/
		// captureTabStripMouse), this one just missed it originally.
		if !p.filterMenuBtn.InRect(event.Position()) {
			return action, event
		}
		if action == tview.MouseLeftClick && p.onOpenFilterMenu != nil {
			p.onOpenFilterMenu()
		}
		return tview.MouseConsumed, nil
	})
	p.renderFilterMenuBtn()

	// The tab strip — see tabstrip.go. Starts empty and zero-width: a
	// lone tab draws nothing (see refreshTabStrip), so a session that
	// never opens a second one looks exactly as it did before tabs
	// existed.
	p.tabStrip = tview.NewTextView()
	p.tabStrip.SetWrap(false)
	p.tabStrip.SetDynamicColors(true) // the active tab's own highlight is a color tag
	p.tabStrip.SetMouseCapture(p.captureTabStripMouse)

	// "<" expands the Details sidebar — see detailsExpandBtn's own doc
	// comment on the struct.
	p.detailsExpandBtn = tview.NewButton("<")
	p.detailsExpandBtn.SetSelectedFunc(func() {
		if p.onExpandDetails != nil {
			p.onExpandDetails()
		}
	})
	// The Details sidebar is deliberately non-modal (the panel stays
	// focused/usable alongside it — see its own doc comment), so a click
	// on the button that opens it must not itself steal real keyboard
	// focus away from whichever of panel/sidebar actually had it —
	// suppressButtonFocusSteal's own doc comment has the full reasoning
	// (a real, user-reported regression without it).
	suppressButtonFocusSteal(p.detailsExpandBtn)

	// Not SetFocusFunc/SetBlurFunc, unlike every other focus-dependent
	// widget in this file — verified directly against tview's own
	// inputfield.go/textarea.go, not guessed: InputField wraps an inner
	// *TextArea, and TextArea.MouseHandler's own click handling calls
	// setFocus(t) with itself, not with the outer InputField. tview's
	// Application tracks whatever setFocus was last given, so after a
	// mouse click into filterField, Application's own focus pointer holds
	// the inner textArea directly. The InputField constructor forwards
	// the textArea's focus callback up to the outer Box (which is why
	// SetFocusFunc above would still fire), but there's no matching
	// blur-forwarding line — so the next time focus moves elsewhere,
	// Application blurs the textArea directly, bypassing
	// InputField.Blur() (and therefore SetBlurFunc) entirely. Confirmed
	// live: the field correctly turned FocusedBackground on click but
	// never reverted on blur. HasFocus() itself isn't affected by any
	// of this (it ORs the textArea's own correctly-toggling flag), so a
	// SetDrawFunc — run on every screen refresh, not just on a focus
	// transition — sidesteps the missing callback rather than fighting it.
	p.filterField.SetDrawFunc(func(_ tcell.Screen, x, y, width, height int) (int, int, int, int) {
		p.setFilterFieldStyle(p.filterField.HasFocus())
		return x, y, width, height
	})

	p.paintStaticChrome()

	// headerRow puts the path bar and the filter side by side in the
	// same top line, per the user's own request — headerPages keeps
	// whatever's left after the filter's own fixed width, so a long path
	// still has as much room as possible rather than being pushed off
	// entirely.
	headerRow := tview.NewFlex().SetDirection(tview.FlexColumn)
	headerRow.AddItem(p.headerPages, 0, 1, false)
	// filterMenuBtn replaces filterRegexBtn/filterField's own old,
	// always-visible slot here — see its own doc comment on the struct.
	headerRow.AddItem(p.filterMenuBtn, filterMenuBtnWidth, 0, false)
	// The tab strip goes between the filter and the Details button, per
	// the user's own explicit request. refreshTabStrip resizes this slot
	// itself as tabs come and go, which is why headerRow is kept as a
	// field.
	headerRow.AddItem(p.tabStrip, 0, 0, false)
	headerRow.AddItem(p.detailsExpandBtn, headerDetailsExpandWidth, 0, false)
	p.headerRow = headerRow

	p.AddItem(headerRow, 1, 0, false)      // fixed one-line path bar + filter
	p.AddItem(p.columnHeader, 1, 0, false) // fixed one-line column header
	p.AddItem(p.table, 0, 1, true)         // fills the rest, holds focus

	if err := p.navigate(path); err != nil {
		return nil, err
	}

	return p, nil
}

// paintStaticChrome applies p.theme's colors to every widget that exists
// for the panel's whole lifetime — as opposed to per-row table cells
// (see addRow/entryColor), which are baked in at construction and only
// get repainted by a full reload (see applyTheme). Called once from
// NewPanel (p.theme already set to the caller's initial value) and again,
// after p.theme changes, from applyTheme (a live color-scheme switch).
func (p *Panel) paintStaticChrome() {
	p.table.SetBackgroundColor(p.theme.PanelBackground)
	p.columnHeader.SetBackgroundColor(p.theme.AccentBackground)

	p.header.SetTextColor(p.theme.Text)
	p.header.SetBackgroundColor(p.theme.AccentBackground)

	// filterMenuBtn's own "Nx" prefix (and the padding filling out
	// whatever's left of filterMenuBtnWidth — see renderFilterMenuBtn)
	// carries no color tag of its own, unlike its own trailing " Y "
	// button — without an explicit background here it fell back to
	// tview's own uninitialized default (plain black), a real,
	// user-reported mismatch against the rest of the header row right
	// beside it.
	p.filterMenuBtn.SetBackgroundColor(p.theme.AccentBackground)

	// FocusedBackground, not AccentBackground: like propertiesEditField/
	// chmodEditField/searchEditField, headerEdit only ever exists on
	// screen while it's the thing actually accepting keystrokes (see
	// openEdit/finishEdit's own headerPages.SwitchToPage calls) — there's
	// no "visible but not focused" state for it to distinguish from, so
	// it's always the "currently active input" color, per the user's own
	// explicit request that every input field in the app follow the same
	// active/inactive/grayed-out convention consistently.
	p.headerEdit.SetFieldBackgroundColor(p.theme.FocusedBackground)
	p.headerEdit.SetBackgroundColor(p.theme.FocusedBackground)
	p.headerEdit.SetFieldTextColor(p.theme.Text)
	p.headerEdit.SetLabelColor(p.theme.Text)

	styleButton(p.filterRegexBtn, p.theme)
	styleButton(p.detailsExpandBtn, p.theme)
	p.styleTabStrip(p.theme)
	// Re-render rather than only recolor: the active tab's own highlight
	// is baked into the text as a color tag (see renderTabStrip), so a
	// live scheme switch has to rebuild the text to pick up the new one.
	p.refreshTabStrip()

	// Unlike headerEdit above, filterField DOES have a real "sometimes
	// focused, sometimes not" cycle — it sits permanently in the header
	// row alongside filterRegexBtn, not shown/hidden the way headerEdit
	// is — so it gets the same FocusedBackground/EditableBackground
	// focus-dependent pair every other multi-state panel in this app
	// already has (see setFilterFieldStyle's own doc comment).
	p.setFilterFieldStyle(p.filterField.HasFocus())

	// The panel's own current-row highlight — see setSelectionStyle's own
	// doc comment for why this isn't just a fixed color the way it used
	// to be. A plain HasFocus() query, not the table's own SetFocusFunc/
	// SetBlurFunc closure it also uses (see NewPanel): this call is a
	// normal, standalone one (not from inside a blur/focus event itself),
	// where HasFocus() is trustworthy.
	p.setSelectionStyle(p.table.HasFocus())
}

// setSelectionStyle sets the panel's own current-row highlight to
// FocusedBackground if focused, EditableBackground otherwise — per the
// user's own explicit request: every other focusable panel in this app
// already shows this same "petrol means this currently has real
// keyboard focus" distinction on its own title bar (see
// toolwindow.go/detailssidebar.go); the main panel, predating all of
// that work, never did.
//
// Takes focused explicitly rather than querying p.table.HasFocus()
// itself, because its two callers need different answers to that exact
// question at the exact moment each runs: paintStaticChrome (see above)
// calls this standalone, where HasFocus() is trustworthy, but
// NewPanel's own SetBlurFunc closure cannot use it at all — verified
// directly against tview's own box.go, not guessed: Box.Blur() runs the
// blur callback *before* clearing its own hasFocus flag, so HasFocus()
// queried from inside a blur callback still (wrongly) reports true.
// SetFocusFunc doesn't have the analogous problem (Box.Focus() sets
// hasFocus true first, then calls the callback), but both pass their
// own already-known answer explicitly here anyway, for the same reason
// rather than leaving one of the two paths relying on it.
//
// Also refreshes the current cursor row's own SelectedStyle (see
// rowSelectedStyle) — the one thing that can change with nothing else
// about that row changing at all: a pure focus/blur transition (Tab to
// a different tab, say) doesn't touch any row's own text or clipboard
// state, only whether tview should show this function's own table-wide
// style here, or a tinted cursor row's own dedicated one, for that row
// specifically (see rowSelectedStyle's own doc comment for why either
// one can be right, depending on focused). Every *other* row is left
// alone: rowSelectedStyle's own verdict for a row that isn't the
// current cursor is never actually consulted by tview at all (see
// Table.Draw's own selected-cell branch), so there's nothing to gain by
// touching more than this one.
func (p *Panel) setSelectionStyle(focused bool) {
	bg := p.theme.EditableBackground
	if focused {
		bg = p.theme.FocusedBackground
	}
	p.table.SetSelectedStyle(tcell.StyleDefault.Background(bg).Foreground(p.theme.Text))

	row, _ := p.table.GetSelection()
	if ref, ok := p.rowRef(row); ok {
		p.setRowCells(row, ref, focused)
		p.paintFixedRowCells(row, ref, focused)
	}
}

// setFilterFieldStyle sets filterField's own background (and its
// placeholder's, which needs setting separately — see paintStaticChrome's
// own comment on SetPlaceholderStyle) to FocusedBackground if focused,
// EditableBackground otherwise — per the user's own explicit request
// that every input field in the app show the same active/inactive
// distinction consistently, not just the newer panels.
//
// Takes focused explicitly, the same shape setSelectionStyle uses, but
// for a different reason here: its only two callers are paintStaticChrome
// and filterField's own SetDrawFunc (see NewPanel), both of which already
// have a trustworthy answer in hand (SetDrawFunc's is freshly queried via
// HasFocus() right before calling this) — see NewPanel's own comment for
// why filterField needs a SetDrawFunc at all rather than the
// SetFocusFunc/SetBlurFunc pair every other widget here uses.
func (p *Panel) setFilterFieldStyle(focused bool) {
	bg := p.theme.EditableBackground
	if focused {
		bg = p.theme.FocusedBackground
	}
	p.filterField.SetPlaceholderStyle(tcell.StyleDefault.Background(bg).Foreground(p.theme.PlaceholderText))
	p.filterField.SetFieldBackgroundColor(bg)
	p.filterField.SetBackgroundColor(bg)
	p.filterField.SetFieldTextColor(p.theme.Text)
}

// applyTheme switches the panel to theme live: every already-built
// widget (see paintStaticChrome), plus a full reload — the simplest way
// to repaint each row's own cell colors (see addRow/entryColor), which
// are baked into their TableCells at construction rather than looked up
// from p.theme on every draw.
func (p *Panel) applyTheme(theme config.ResolvedTheme) {
	p.theme = theme
	p.paintStaticChrome()
	p.reportError(p.load(p.path))
}

// load replaces the panel's contents with the entries of dir. It only
// mutates the panel's state (path, header, table rows) once ListDir has
// succeeded, so a failed load leaves the panel showing whatever it showed
// before. It does not touch history — see navigate, back, forward.
//
// Moving to a genuinely different directory (newDirectory — as opposed
// to a same-directory refresh, e.g. after toggling hidden files, sort,
// or the filter's own regex mode) resets two things:
//
//   - The filter box — unless filterPersistent is true (the default;
//     see config.Settings.FilterPersistent's own doc comment), in which
//     case this step is skipped entirely and whatever filter was active
//     carries straight into the new directory. filterPersistent false
//     restores the original behavior: a filter scoped to "what's on
//     screen, not carried across navigation", the same rule selected
//     already follows, and for the same reason — a filter that stayed
//     applied after moving somewhere unrelated would too easily leave
//     the new directory looking empty for a reason that isn't obvious.
//     filterField.SetText below re-enters this func's own
//     SetChangedFunc, which no-ops there since filterText already
//     matches by the time it fires — see that handler's own comment.
//   - The table's own cursor, back to the top row — per the user's own
//     request, landing on wherever the table's internal selection
//     happened to be left (Table.Clear doesn't touch it, so without this
//     it's whatever row a previous, unrelated directory's listing had
//     it on, not necessarily even a valid row in this one) reads as
//     arbitrary, not "here's the directory you just opened".
//
// Also always exits search mode first, unconditionally: showing a real
// directory (what load does, definitionally) and showing search
// results (searchMode) are mutually exclusive states, and a great many
// existing actions elsewhere (Rename, chown/chmod, Properties' own
// Save, every "Globals" toggle, ...) already refresh via this exact
// p.load(p.path) idiom without knowing anything about search mode at
// all — right-clicking a search-result row for any of them reaches
// r.menu's own real, ordinary items unchanged (see Root.captureMouse's
// MouseRightClick case, which never treated a search-mode row any
// differently to begin with). Without this, one of those would leave
// searchMode still true while the table itself had already been
// silently overwritten with a real directory's rows — activateRow,
// setSortKey, and the filter box would all keep taking their own
// searchMode branch against rows that, by then, are not search results
// at all.
func (p *Panel) load(dir string) error {
	p.searchMode = false
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	entries, err := fsops.ListDir(abs)
	if err != nil {
		return err
	}
	if !p.showHidden {
		entries = filterHidden(entries)
	}

	// onDescribeRows overrides (see its own doc comment) apply to
	// ModTime right here, on entries themselves — before filtering/
	// sorting run — specifically so sorting by Modified (the "Deletion
	// time" column while p.describeRows/isTrashDir is true) reflects
	// what's actually displayed, not the real file's own last-edit time
	// underneath it. Name is deliberately NOT overridden here (see
	// addRow below instead): filterByText/applySortPreference operate
	// on fsops.Entry.Name, which is also what builds each row's own real
	// path just below — overwriting it here would silently break that
	// path construction. Sorting by Name for a trash listing therefore
	// still sorts by the real, hidden on-disk name — a narrow, accepted
	// limitation rather than a second parallel sort implementation (see
	// sortSearchEntries's own doc comment for why that path already
	// costs a dozen-odd duplicated lines elsewhere in this file).
	var describeRows map[string]rowDescription
	p.inTrashView = false
	if p.onDescribeRows != nil {
		describeRows, p.inTrashView = p.onDescribeRows(abs)
	}
	for i, e := range entries {
		if d, ok := describeRows[filepath.Join(abs, e.Name)]; ok {
			entries[i].ModTime = d.modTime
		}
	}

	newDirectory := abs != p.path
	if newDirectory && !p.filterPersistent {
		p.filterText = ""
		p.filterField.SetText("")
		// The filter-menu's own three toggles are exactly as scoped to
		// "what's on screen right now" as the text/regex-mode they sit
		// alongside — carrying size/modified-time filtering into a
		// directory nobody asked to filter would be just as surprising
		// as a stale text pattern silently doing the same (see this
		// func's own doc comment on why that already resets here).
		// filterGlobActive resets to its own default-on state rather
		// than off, matching filterText's own reset to "" immediately
		// above: both together are what let a fresh directory show
		// everything, unfiltered, exactly as filterGlobActive's own doc
		// comment already promises for anyone who's never touched the
		// filter menu at all.
		p.filterGlobActive = true
		p.filterSizeActive = false
		p.filterMtimeActive = false
	}
	beforeFilterCount := len(entries)
	entries = filterByText(entries, p.filterText, p.filterRegex, p.filterGlobActive)
	// See filterMatchesNothing's own doc comment: beforeFilterCount > 0
	// is what tells "the filter hid everything" apart from "this
	// directory is simply empty" — filterByText itself is a no-op on an
	// already-empty entries slice either way, so both would otherwise
	// look identical here.
	p.filterMatchesNothing = len(entries) == 0 && beforeFilterCount > 0
	applySortPreference(entries, p.sortKey, p.sortDescending)

	p.table.Clear()
	p.selected = make(map[string]bool)
	p.lastNameClickRow = -1 // see its own doc comment: a rebuilt table's row indices mean something new
	p.path = abs

	text, spans := buildHeaderSpans(abs, p.theme)
	p.header.SetText(text)
	p.headerSpans = spans
	p.renderFilterMenuBtn()

	// Before the rows, not after: addRow shortens each name against the
	// name column's width, which is whatever the Size and Modified
	// columns leave over — so those two have to be sized from the data
	// first (see columns.go).
	p.sizeColumnsFor(entries)

	row := 0
	if parent := filepath.Dir(abs); parent != abs {
		p.addRow(row, rowRef{path: parent, name: "..", isDir: true, checkable: false, entryType: fsops.TypeDir})
		row++
	}
	for _, e := range entries {
		entryPath := filepath.Join(abs, e.Name)
		name := e.Name
		if d, ok := describeRows[entryPath]; ok {
			name = d.name
		}
		p.addRow(row, rowRef{
			path:       entryPath,
			name:       name,
			isDir:      e.IsDir,
			checkable:  true,
			entryType:  e.Type,
			linkTarget: e.LinkTarget,
			nlink:      e.Nlink,
			mountPoint: e.MountPoint,
			mode:       e.Mode,
			unreadable: e.Unreadable,
			size:       e.Size,
			modTime:    e.ModTime,
		})
		row++
	}

	// After the rows, not before: buildColumnHeader's checkbox reflects
	// allSelected(), which reads the table's own rows — right now that's
	// moot (selected was just reset above, so allSelected() is false
	// either way), but building it from the real, populated table is the
	// robust way to state that instead of relying on that always holding.
	p.buildColumnHeader()

	if newDirectory {
		// SetOffset(0, 0) first, not just focusRow(0): p.table is the
		// same tview.Table instance across every directory this panel
		// ever shows (load only ever Clears its cells, never replaces
		// it), and tview's own scroll state — rowOffset, and especially
		// trackEnd — lives on that instance too, untouched by Clear.
		// trackEnd in particular is a *sticky* "snap to the bottom on
		// the next Draw" flag that tview sets on its own whenever an
		// entire listing already fits on screen (harmless there — there
		// is no "bottom" to snap past) but never clears again just
		// because a different, much longer directory loads next.
		// focusRow(0) alone can leave it stuck true: tview's own
		// Select()-driven clamp only ever resets trackEnd as a side
		// effect of also scrolling rowOffset up to reveal row 0 — which
		// it only bothers doing when the *previous* rowOffset was
		// already greater than 0. Coming from a directory small enough
		// to fit on screen at all (a real, reproduced case: a short
		// subfolder, itself entered from partway down a long one),
		// rowOffset was already sitting at 0 for an unrelated reason,
		// so that clamp branch never fires, trackEnd survives the trip
		// untouched, and the very next Draw of the new, much longer
		// directory snaps the view straight to its own bottom —
		// cursor correctly on row 0, but scrolled to the last screenful
		// instead, a real, reported "doesn't actually land at the top"
		// bug. SetOffset(0, 0) resets both rowOffset and trackEnd
		// unconditionally (verified directly against tview's own
		// table.go), sidestepping this Select()-clamp heuristic
		// entirely rather than depending on it to happen to fire.
		p.table.SetOffset(0, 0)
		p.focusRow(0) // top of the listing — see this func's own doc comment
	}

	if p.onLoad != nil {
		p.onLoad(p.path)
	}
	return nil
}

// showSearchResults switches the panel from its own real directory to
// an initially empty search-results listing — see appendSearchResult
// (called once per streamed result), setSearchStatus (the header's own
// progress/outcome text), and exitSearchResults/activateRow for the
// rest of this mode's lifecycle. Also pushes a new historyEntry for
// this results listing (see snapshotCurrentEntry/pushHistoryEntry),
// same as navigate does for a real directory, so Back/Forward can
// return to it later — per the user's own explicit request that a
// search "excursion" not be invisible to the navigation history the
// way it used to be. Safe to call again while search results are
// already showing (a second search replacing the first): the history
// push is skipped then, so refining and re-running a search doesn't
// stack a new entry per attempt — only the *first* transition into
// search mode is a new "place" to come back to.
//
// p.path itself is deliberately never touched here or anywhere else in
// this mode — navigate/back/forward now know about search mode (see
// historyEntry.isSearch/restoreHistoryEntry) but load() itself still
// doesn't need to, so exitSearchResults' own restore stays nothing more
// than a plain, ordinary reload of whatever p.path still is.
//
// Defaults searchBrowsePath to p.path — a reasonable fallback, and
// exactly right for every test that calls this directly without caring
// about the "continue here" breadcrumb at all. Root.runSearch/
// showSearchError set it to the search's own actual "Start at" scope
// right after calling this, since only they know it.
func (p *Panel) showSearchResults() {
	if !p.searchMode {
		p.snapshotCurrentEntry()
		p.pushHistoryEntry(historyEntry{})
		p.searchBrowsePath = p.path
	}
	p.searchMode = true
	p.searchEntries = nil
	p.selected = make(map[string]bool) // selection scoped to what's on screen, same rule load() already follows for a real directory
	p.lastNameClickRow = -1            // see its own doc comment: a rebuilt table's row indices mean something new
	p.table.Clear()
	p.buildColumnHeader()
}

// searchResultEntry pairs one result's real fsops classification
// (Entry — Name always the result's own real full path, never
// overwritten) with the text actually shown for it (display):
// identical to Entry.Name for a filename match, or "path:line: text"
// for a content match (see appendSearchResult). Kept separate from
// Entry.Name itself, rather than folding the line/text into it,
// specifically because a content search can report the *same* path
// more than once — once per matching line, unless "First hit" is
// checked — and Entry.Name staying the bare path throughout means
// every one of those still sorts/groups exactly where it belongs, next
// to each other and next to whatever else shares its own directory,
// rather than each carrying a different fabricated "name" that
// scatters them apart from one another.
type searchResultEntry struct {
	fsops.Entry
	display    string
	line       int  // > 0 for a content match — see rowRef.searchLine's own doc comment
	archiveHit bool // true for an archive-member match — see rowRef.archiveHit's own doc comment
}

// appendSearchResult adds one streamed result — classified via
// fsops.DescribeEntry, the exact same per-entry classification a real
// directory listing gives each of its own children (symlink
// resolution, broken-symlink detection, mount-point check — see its
// own doc comment). display is res.Path itself for a plain filename
// match (Line == 0, ArchiveMember == ""), "path:line: text" for a
// content match (see search.Result's own Line/Text fields, populated
// only by a content/grep search), "path -> member" for an
// archive-member filename match (search.Result.ArchiveMember,
// populated when Request.IncludeArchives is on), or "path -> member:
// line: text" for a content match found *inside* an archive member
// (both Line and ArchiveMember set — see internal/search's own
// archivecontent.go, populated when Request.IncludeCompressed finds a
// match inside a tar/tar.gz/tar.bz2/tar.xz archive) — the same "->
// target" shape a symlink's own name already gets (see addRow), reused
// here rather than inventing a second arrow convention.
//
// fsops.DescribeEntry(res.Path) always describes the real, containing
// archive file itself for an archive-member match, never something
// synthetic for the virtual member path inside it — there's nothing on
// disk to stat there — which is also exactly right for entry.IsDir/
// entry.Size/etc. below: this is deliberately the archive's own real
// classification, not a guess at the matched member's.
//
// Re-renders immediately, one result at a time: search results arrive
// slowly enough (one find/grep process, one match at a time) that
// re-sorting and redrawing on every single one stays imperceptible,
// and immediate feedback — rows appearing one by one as they're found
// — matches this dialog's own existing "watch it stream in" feel,
// unchanged from before this mode existed.
func (p *Panel) appendSearchResult(res search.Result) {
	entry := fsops.DescribeEntry(res.Path)
	entry.Name = res.Path
	display := res.Path
	switch {
	case res.Line > 0 && res.ArchiveMember != "":
		display = fmt.Sprintf("%s -> %s:%d: %s", res.Path, res.ArchiveMember, res.Line, res.Text)
	case res.Line > 0:
		display = fmt.Sprintf("%s:%d: %s", res.Path, res.Line, res.Text)
	case res.ArchiveMember != "":
		display = res.Path + " -> " + res.ArchiveMember
	}
	p.searchEntries = append(p.searchEntries, searchResultEntry{
		Entry: entry, display: display, line: res.Line, archiveHit: res.ArchiveMember != "",
	})
	p.renderSearchEntries()
}

// setSearchStatus paints the header's own two-part display while search
// results are showing: text itself (the animated "still searching"
// indicator, then a final "Done — N found") followed by a real,
// ordinary breadcrumb for searchBrowsePath — the same clickable
// buttons/path segments a real directory's header shows (see
// buildHeaderSpans), editable by clicking past it the same way too (see
// openEdit) — so search mode no longer traps the user between only
// Escape and jumping to a specific result, per their own explicit
// report that it did. Any click there is real navigation (see
// captureHeaderMouse/runHeaderAction) and leaves search mode the moment
// it actually goes anywhere, the same as activating a result already
// does.
//
// searchHeaderOffset records where the breadcrumb starts within the
// combined text, so captureHeaderMouse can tell a click on the status
// prefix (never a path to edit) apart from one on, or past, the
// breadcrumb itself. Also mirrors text into searchStatusText, for
// snapshotCurrentEntry to freeze if the panel navigates away before
// this search is ever revisited.
func (p *Panel) setSearchStatus(text string) {
	p.searchStatusText = text

	const separator = ", or continue here: "
	prefix := text + separator
	p.searchHeaderOffset = tview.TaggedStringWidth(prefix)

	breadcrumbText, breadcrumbSpans := buildHeaderSpans(p.searchBrowsePath, p.theme)
	p.header.SetText(prefix + breadcrumbText)

	spans := make([]headerSpan, len(breadcrumbSpans))
	for i, s := range breadcrumbSpans {
		s.start += p.searchHeaderOffset
		s.end += p.searchHeaderOffset
		spans[i] = s
	}
	p.headerSpans = spans
}

// setSearchStatusColor overrides the header's text color while showing
// search status — normally left at its usual paintStaticChrome color
// (theme.Text), but set to theme.EntryError for a search that was
// refused before it ever ran (see Root.showSearchError) — the same red
// a broken symlink already gets in a real listing (see entryColor).
// Root.runSearch resets it back to theme.Text before a real search
// begins, in case a previous one left it red. Also mirrored into
// searchStatusColor — see setSearchStatus's own doc comment.
func (p *Panel) setSearchStatusColor(c tcell.Color) {
	p.searchStatusColor = c
	p.header.SetTextColor(c)
}

// renderSearchEntries re-sorts (per whatever sortKey/sortDescending are
// currently set to — the exact same two fields a real directory
// listing's own column-header click already drives, see setSortKey)
// and redraws every row from searchEntries. There's no ListDir call to
// repeat the way a real directory's own sort change re-triggers one
// (see load's own doc comment) — searchEntries already holds
// everything there is to show; appendSearchResult and a sort-key
// change while search results are showing (see setSortKey) both just
// call this again directly.
//
// sortSearchEntries/sortSearchGroup, not applySortPreference/sortGroup:
// those operate on []fsops.Entry directly, but a content search can
// carry the same path more than once (see searchResultEntry's own doc
// comment) — this mirrors their exact two-pass shape (establish a
// directories-first, name-sorted base the same way ListDir does before
// a real directory's own load() ever calls applySortPreference, then
// refine within each group for a non-name sort key) against
// searchResultEntry instead, since Go generics aren't in play here and
// duplicating roughly a dozen lines is simpler than making those two
// take an interface.
//
// Explicitly re-applies each row's own checked state from p.selected
// afterward: unlike load() (which always resets p.selected right
// before its own addRow loop, so addRow's own hardcoded "unchecked"
// starting glyph is already correct there), this re-render must
// preserve whatever was already checked across a sort-key change or a
// newly-streamed-in result — addRow itself has no way to know that on
// its own; it always draws a fresh checkbox unchecked.
func (p *Panel) renderSearchEntries() {
	sorted := append([]searchResultEntry(nil), p.searchEntries...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].IsDir != sorted[j].IsDir {
			return sorted[i].IsDir // directories before files
		}
		return strings.ToLower(sorted[i].Name) < strings.ToLower(sorted[j].Name)
	})
	sortSearchEntries(sorted, p.sortKey, p.sortDescending)

	p.table.Clear()
	for row, e := range sorted {
		p.addRow(row, rowRef{
			path:       e.Name, // always the result's own real path (Entry, embedded — see searchResultEntry's own doc comment)
			name:       e.display,
			isDir:      e.IsDir,
			checkable:  true,
			entryType:  e.Type,
			linkTarget: e.LinkTarget,
			nlink:      e.Nlink,
			mountPoint: e.MountPoint,
			mode:       e.Mode,
			unreadable: e.Unreadable,
			size:       e.Size,
			modTime:    e.ModTime,
			searchLine: e.line,
			archiveHit: e.archiveHit,
		})
		if p.selected[e.Name] {
			p.setChecked(row, true)
		}
	}
	p.buildColumnHeader()
}

// sortSearchEntries is applySortPreference's own []searchResultEntry
// counterpart — see renderSearchEntries' own doc comment on why it's a
// duplicate rather than a shared, parameterized implementation.
func sortSearchEntries(entries []searchResultEntry, key sortKey, descending bool) {
	split := len(entries)
	for i, e := range entries {
		if !e.IsDir {
			split = i
			break
		}
	}
	sortSearchGroup(entries[:split], key, descending)
	sortSearchGroup(entries[split:], key, descending)
}

// sortSearchGroup is sortGroup's own []searchResultEntry counterpart —
// same three keys (size, modified, name as the tiebreaker and the
// whole comparison for sortByName itself), same stable sort so ties
// keep whatever relative order they already arrived in.
func sortSearchGroup(entries []searchResultEntry, key sortKey, descending bool) {
	less := func(i, j int) bool {
		switch key {
		case sortBySize:
			if entries[i].Size != entries[j].Size {
				return entries[i].Size < entries[j].Size
			}
		case sortByModified:
			if !entries[i].ModTime.Equal(entries[j].ModTime) {
				return entries[i].ModTime.Before(entries[j].ModTime)
			}
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	}
	if descending {
		asc := less
		less = func(i, j int) bool { return asc(j, i) }
	}
	sort.SliceStable(entries, less)
}

// exitSearchResults leaves search mode behind, restoring whatever the
// panel was showing right before showSearchResults switched it over —
// a no-op if search results aren't showing at all (closeSearch calls
// this unconditionally, whether or not a search was ever actually run).
//
// Simply steps back exactly one history entry (see back): the
// search-results entry showSearchResults pushed is always the current
// position by the time this runs, since nothing else can push another
// entry while searchMode is still true (every other navigation method
// checks it first — see Panel's own searchMode doc comment) — so this
// used to need its own separate searchRestorePath/Row bookkeeping, but
// now that a search listing is itself a real historyEntry (see
// showSearchResults), leaving it is just an ordinary Back.
func (p *Panel) exitSearchResults() error {
	if !p.searchMode {
		return nil
	}
	p.back()
	return nil
}

// filterHidden removes dotfile entries (name starting with ".") from
// entries — load()'s effect when showHidden has been toggled off (the
// default is true — dotfiles shown — see Panel.showHidden). This
// happens before any row is ever added to the table, rather than adding
// a row that's then somehow marked hidden: every row-based operation
// (selectAll, selectByPattern, arrow-key navigation, ...) excludes a
// hidden entry for free that way, with no operation needing its own "is
// this one actually hidden right now" check.
func filterHidden(entries []fsops.Entry) []fsops.Entry {
	visible := entries[:0] // reuses entries' backing array; entries itself isn't read again after this call
	for _, e := range entries {
		if !strings.HasPrefix(e.Name, ".") {
			visible = append(visible, e)
		}
	}
	return visible
}

// filterModeLabel is the filter's regex-toggle button's own label — the
// mode clicking it is currently in, not (unlike e.g. Root's Globals-menu
// toggles) what clicking it will switch to: there's no room in an
// 8-column button for a longer "click for X" phrasing, and the mode
// itself is what needs to be legible from across the panel while typing
// a pattern, not a description of the click.
func filterModeLabel(regex bool) string {
	if regex {
		return "Regex"
	}
	return "Glob"
}

// renderFilterMenuBtn fills in filterMenuBtn's own text: an "Nx"
// indicator (N = how many of the filter-menu's three rows are actually
// in effect right now) immediately before a three-column " Y " button —
// "Y" per the user's own explicit choice, for its own passing
// resemblance to a funnel. Omitted entirely once N is 0, per the same
// explicit request, rather than ever showing "0x" — left-padded up to
// filterMenuBtnWidth instead, so the button's own three columns always
// sit flush against this slot's own right edge (where the tab strip
// picks up right after it) regardless of how wide the indicator is.
//
// The glob/regex row only actually counts once it's both switched on
// *and* has something to filter by — matching filterByText's own real
// no-op condition (see its own doc comment) — since the indicator's
// whole point is "filtering is genuinely narrowing the list right now",
// not just "a checkbox happens to be checked". Size/modified-time have
// no such distinction yet (see filterSizeActive/filterMtimeActive's own
// doc comment on the struct: pure toggles, no filtering logic behind
// them) — their raw toggle state is the only signal there is.
//
// Also colors the "Nx" itself EntryError's own red whenever
// filterMatchesNothing is true (see its own doc comment) — a filter
// hiding every single entry reads exactly like a genuinely empty
// directory otherwise, and this indicator is the one place a plain,
// unfiltered look at the listing itself never will be: a stray glance
// at "Nx" in a color already meaningful elsewhere as "something's
// wrong" is far more likely to register than noticing a small, neutral
// count is present at all.
func (p *Panel) renderFilterMenuBtn() {
	count := 0
	if p.filterGlobActive && p.filterText != "" {
		count++
	}
	if p.filterSizeActive {
		count++
	}
	if p.filterMtimeActive {
		count++
	}

	prefix := ""
	if count > 0 {
		prefix = fmt.Sprintf("%dx", count)
		if p.filterMatchesNothing {
			prefix = fmt.Sprintf("[%s::]%s[-:-:-]", colorTag(p.theme.EntryError), prefix)
		}
	}

	keyBG := colorTag(p.theme.ButtonBackground)
	button := fmt.Sprintf("[:%s:] Y [-:-:-]", keyBG)
	visible := tview.TaggedStringWidth(prefix) + 3 // the button's own 3 visible columns
	if pad := filterMenuBtnWidth - visible; pad > 0 {
		prefix = strings.Repeat(" ", pad) + prefix
	}
	p.filterMenuBtn.SetText(prefix + button)
}

// filterByText narrows entries to those whose name matches filterText —
// via filepath.Match (shell-pattern globbing, "*"/"?"/"[...]", the same
// syntax Select+/- already uses) by default, or via regexp.MatchString
// once filterRegex is on — matching how Midnight Commander's own filter
// dialog offers exactly the same two modes ("Shell Patterns" on or off).
// An empty filterText, or active being false, is a no-op (every entry
// kept, unfiltered) — active is the filter-menu's own glob/regex
// checkbox (see Panel.filterGlobActive's own doc comment): a pattern
// left typed in but deliberately switched off stops applying without
// losing it, the same "disable without clearing" every other toggle in
// this app already offers.
//
// An invalid pattern is treated the same as "no filter yet" (every
// entry kept) rather than surfaced as an error: this runs on every
// keystroke, so an incomplete regex (or a malformed glob like an
// unterminated "[") is an expected, transient state while typing, not
// something worth interrupting for.
func filterByText(entries []fsops.Entry, filterText string, filterRegex, active bool) []fsops.Entry {
	if !active || filterText == "" {
		return entries
	}

	var match func(name string) bool
	if filterRegex {
		re, err := regexp.Compile(filterText)
		if err != nil {
			return entries
		}
		match = re.MatchString
	} else {
		// Checked once, against an arbitrary probe string, rather than
		// per entry inside match itself: filterText's validity as a
		// pattern doesn't depend on what it's being matched against, and
		// checking it per entry would make every entry fail to match
		// (not just leave the pattern effectively unfiltered) the moment
		// it's malformed — the opposite of what this function documents.
		if _, err := filepath.Match(filterText, ""); err != nil {
			return entries
		}
		match = func(name string) bool {
			hit, _ := filepath.Match(filterText, name) // err is nil: already checked above
			return hit
		}
	}

	visible := entries[:0] // reuses entries' backing array, same as filterHidden
	for _, e := range entries {
		if match(e.Name) {
			visible = append(visible, e)
		}
	}
	return visible
}

// sortKey picks which of an Entry's fields Panel sorts by — see
// applySortPreference and buildColumnHeader/setSortKey (a column-header
// click).
type sortKey int

const (
	sortByName sortKey = iota
	sortBySize
	sortByModified
)

// applySortPreference reorders entries in place: directories first
// (ListDir's own grouping, kept regardless of key/direction — merging
// files and directories into one order isn't what any of these keys are
// for), then by key within each group, in the given direction.
func applySortPreference(entries []fsops.Entry, key sortKey, descending bool) {
	split := len(entries)
	for i, e := range entries {
		if !e.IsDir {
			split = i
			break
		}
	}
	sortGroup(entries[:split], key, descending)
	sortGroup(entries[split:], key, descending)
}

// sortGroup sorts one directories-or-files group by key, breaking ties
// (and doing the entire comparison, for sortByName itself) by name — the
// same case-insensitive comparison ListDir already sorted the group by,
// so a sortByName, ascending call here is a no-op on already-sorted
// input rather than doing needless work.
func sortGroup(entries []fsops.Entry, key sortKey, descending bool) {
	less := func(i, j int) bool {
		switch key {
		case sortBySize:
			if entries[i].Size != entries[j].Size {
				return entries[i].Size < entries[j].Size
			}
		case sortByModified:
			if !entries[i].ModTime.Equal(entries[j].ModTime) {
				return entries[i].ModTime.Before(entries[j].ModTime)
			}
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	}
	if descending {
		asc := less
		less = func(i, j int) bool { return asc(j, i) }
	}
	sort.SliceStable(entries, less)
}

// addRow renders one table row for ref at the given row index.
func (p *Panel) addRow(row int, ref rowRef) {
	checkbox := tview.NewTableCell(checkboxText(false)).SetTextColor(p.theme.Text)
	if !ref.checkable {
		checkbox.SetText(" ")
	} else {
		checkbox.SetClickedFunc(func() bool {
			p.toggleCheckbox(row)
			return false // also let the row become selected/highlighted,
			// the same visible feedback a click anywhere else in the row
			// gives — a checked/unchecked glyph flipping is easy to miss
			// on its own, especially if the click landed a column off.
		})
	}
	p.table.SetCell(row, colCheckbox, checkbox)

	// The type-indicator glyph itself stays the plain Text color — only
	// the name (below) picks up entryColor's own distinction, per the
	// user's own explicit request. MC's own skin colors both; this app
	// deliberately doesn't, so the narrow type column reads as pure
	// punctuation rather than a second, redundant color cue.
	typeCell := tview.NewTableCell(string(typeGlyph(ref))).SetTextColor(p.theme.Text)
	p.table.SetCell(row, colType, typeCell)

	modCell := tview.NewTableCell(string(modifierGlyph(ref))).SetTextColor(p.theme.Text)
	p.table.SetCell(row, colModifier, modCell)

	focused := p.table.HasFocus()
	p.setRowCells(row, ref, focused)
	p.paintFixedRowCells(row, ref, focused)
}

// rowBackground is what addRow/setRowCells/paintFixedRowCells tint
// ref's own row with: a shade from ClipboardCopyBackground/
// ClipboardCutBackground if ref.path is currently held on the
// clipboard (see setClipboard), ok false otherwise — meaning "leave
// this cell exactly as its own zero-value construction already has
// it", not "paint it PanelBackground": a cell nothing has ever called
// SetBackgroundColor on stays Transparent (tview's own term), showing
// through to whatever the table's own background already is, and
// explicitly repainting that same color on every single cell of every
// ordinary row would risk it drifting from the table's own actual
// background under some future tview change instead of a single
// source of truth. Checked by absolute path, not selection state:
// unrelated to the checkbox column (see checkboxText) — a file can be
// checked without being on the clipboard, and vice versa (Copy/Cut
// capture the checked selection as a snapshot, which the checkboxes
// are then free to change independently of it). ".." (checkable
// false) never tints even if its own path happens to match — it isn't
// a real clipboard target, and rowRef.path for it is the parent
// directory, not something Copy/Cut could ever have captured.
func (p *Panel) rowBackground(ref rowRef) (bg tcell.Color, ok bool) {
	if !ref.checkable || !p.clipboardPaths[ref.path] {
		return 0, false
	}
	if p.clipboardCut {
		return p.theme.ClipboardCutBackground, true
	}
	return p.theme.ClipboardCopyBackground, true
}

// rowBackgroundInactive is rowBackground's own Inactive-variant sibling
// — same shape, same conditions, just ClipboardCopyBackgroundInactive/
// ClipboardCutBackgroundInactive instead of the full-brightness colors
// (see their own doc comment in internal/config/theme.go) — used only
// by rowSelectedStyle, for a tinted row that's also the cursor row
// while this panel doesn't currently have real keyboard focus.
func (p *Panel) rowBackgroundInactive(ref rowRef) (bg tcell.Color, ok bool) {
	if !ref.checkable || !p.clipboardPaths[ref.path] {
		return 0, false
	}
	if p.clipboardCut {
		return p.theme.ClipboardCutBackgroundInactive, true
	}
	return p.theme.ClipboardCopyBackgroundInactive, true
}

// rowSelectedStyle computes what ref's own cells should carry as their
// SelectedStyle — the separate style tview only ever consults for
// whichever row currently is the table's own cursor row (see
// Table.Draw's own selected-cell branch: a cell's SelectedStyle wins
// outright over the table-wide SetSelectedStyle whenever it's actually
// set, checked in that order) — given focused, this panel's own current
// real-keyboard-focus state.
//
// tcell.StyleDefault (tview's own "nothing cell-specific set" zero
// value, letting the table-wide SetSelectedStyle apply instead — see
// setSelectionStyle) for an untinted row always, and for a tinted one
// specifically while focused is true: the user's own explicit request
// that the focus/cursor indicator (FocusedBackground) win outright over
// the clipboard tint while this panel is actively focused, so the
// cursor's own position stays unambiguous even among several tinted
// rows in the same selection — the same "current row happens to also
// be selected" ambiguity a plain, unconditional tint would otherwise
// reintroduce for exactly the case FocusedBackground already existed to
// solve.
//
// Once focused is false, a tinted row's own color takes over instead —
// its dedicated, deliberately darker Inactive variant (see
// rowBackgroundInactive), not its full-brightness one: a second,
// separate real gap the user reported once the first fix landed —
// without its own distinct color, an unfocused clipboard-held cursor
// row became indistinguishable from every other tinted row in the same
// selection, losing "where would the cursor land if I switched back"
// exactly as thoroughly as the original bug lost "is this file still
// on the clipboard at all".
func (p *Panel) rowSelectedStyle(ref rowRef, focused bool) tcell.Style {
	if _, tinted := p.rowBackground(ref); !tinted || focused {
		return tcell.StyleDefault
	}
	bg, _ := p.rowBackgroundInactive(ref)
	return tcell.StyleDefault.Background(bg).Foreground(p.theme.Text)
}

// paintFixedRowCells applies rowBackground's own verdict to the three
// cells addRow builds directly (checkbox, type, modifier) — colName/
// colSize/colModified are setRowCells' own responsibility, since it
// rebuilds those from scratch every time it runs anyway (see its own
// doc comment) and a freshly constructed cell is already untinted by
// default. These three, unlike those, are only ever built once by
// addRow itself — setClipboard's own repaint (a clipboard change,
// nothing on disk) mutates them in place here rather than recreating
// them, which would also throw away the checkbox's own click handler.
// SetTransparency(true) is the untint path, not SetBackgroundColor(
// PanelBackground): it puts a previously tinted cell back into the
// exact same "never touched" state a fresh one starts in, rather than
// hardcoding a value that could drift from the table's own actual
// background (see rowBackground's own doc comment).
//
// Also sets each cell's own SelectedStyle to rowSelectedStyle's own
// verdict, given focused — this panel's current real-keyboard-focus
// state, passed in explicitly rather than queried here (see this
// function's own callers: addRow/relayoutColumns/setClipboard can
// safely call p.table.HasFocus() themselves since none of them run
// from inside a blur callback, but setSelectionStyle's own SetBlurFunc
// caller cannot — see its own doc comment on why HasFocus() lies from
// there specifically). See rowSelectedStyle's own doc comment for the
// full reasoning: without this at all, a clipboard-held row that's also
// the cursor row loses its own tint entirely, hidden by the table-wide
// SetSelectedStyle regardless of focus state — the original,
// user-reported bug; with it always applying regardless of focus, the
// cursor's own position became ambiguous among several tinted rows
// instead — a real, separate follow-up report. Threading focused
// through, rather than always giving a tinted row the same override, is
// what lets the cursor/focus indicator win while focused and the tint's
// own darker Inactive variant win once it isn't, rather than picking
// one of those two real, contradictory requirements to satisfy.
func (p *Panel) paintFixedRowCells(row int, ref rowRef, focused bool) {
	bg, tinted := p.rowBackground(ref)
	selStyle := p.rowSelectedStyle(ref, focused)
	for _, col := range [...]int{colCheckbox, colType, colModifier} {
		cell := p.table.GetCell(row, col)
		if cell == nil {
			continue
		}
		if tinted {
			cell.SetBackgroundColor(bg)
		} else {
			cell.SetTransparency(true)
		}
		cell.SetSelectedStyle(selStyle)
	}
}

// setClipboard applies the app-wide clipboard's current contents (see
// Root.clipboard/clipboardCut and Root.syncClipboardHighlight, which
// calls this for every open tab, not just whichever one triggered the
// change) to this panel's own row highlighting. Repaints whatever rows
// are already on screen in place — nothing on disk changed, so there's
// nothing to reload — and stores paths/cut so any row addRow builds
// afterward (a fresh load(), not just a repaint) picks up the current
// state too.
func (p *Panel) setClipboard(paths []string, cut bool) {
	m := make(map[string]bool, len(paths))
	for _, path := range paths {
		m[path] = true
	}
	p.clipboardPaths = m
	p.clipboardCut = cut

	focused := p.table.HasFocus()
	for row := 0; row < p.table.GetRowCount(); row++ {
		if ref, ok := p.rowRef(row); ok {
			p.setRowCells(row, ref, focused)
			p.paintFixedRowCells(row, ref, focused)
		}
	}
}

// setRowCells fills the three width-dependent cells of one row — Name,
// Size and Modified — from the panel's current column layout.
//
// Split out of addRow so relayoutColumns can re-render an existing row
// after the panel's width changed, without rebuilding the whole listing
// or touching the filesystem.
//
// focused is this panel's own current real-keyboard-focus state,
// passed in explicitly rather than queried here — see
// paintFixedRowCells' own doc comment for the full reasoning (shared
// verbatim, since both functions get it from the exact same set of
// callers) and rowSelectedStyle's for what it actually does with it.
func (p *Panel) setRowCells(row int, ref rowRef, focused bool) {
	color := p.entryColor(ref)
	bg, tinted := p.rowBackground(ref)
	selStyle := p.rowSelectedStyle(ref, focused)

	// The suffix travels separately from the name so shortenNameLabel
	// can protect it: a trailing "/" or " -> target" says what kind of
	// entry this is, which a shortened name alone no longer does.
	suffix := ""
	if ref.entryType == fsops.TypeDir {
		suffix = "/"
	}
	if ref.linkTarget != "" {
		suffix = " -> " + ref.linkTarget
	}
	name, suffix := shortenNameLabel(ref.name, suffix, p.nameColumnWidth())

	// Escaping happens *after* shortening, and shortening works on plain
	// text: tview.TableCell.Text is parsed for style tags (see
	// tview.Print), so any literal "[" in a filename — unusual but
	// entirely legal — has to be escaped before it reaches a cell, and
	// cutting a string that already contained tags would slice through
	// one and corrupt every style after it.
	label := tview.Escape(name)
	if ref.isDir {
		// Wrap just the name itself in a style tag that sets its
		// background to DirectoryBackground, leaving the foreground
		// untouched (see nameHighlightTags's own doc comment) — not the
		// trailing "/" or the symlink arrow appended below, and not the
		// column's own blank padding out to the row's right edge, the
		// way SetBackgroundColor on the whole cell would.
		label = nameHighlightTags(label, p.theme.DirectoryBackground)
	}
	label += tview.Escape(suffix)
	if width := p.layout.name; width > 0 {
		// Pads out to the column's own full width — see padRight's own
		// doc comment for why this, rather than SetExpansion, is what
		// keeps this table's Name column in step with columnHeader's.
		// Guarded on a real (post-first-draw) width, the same guard
		// nameColumnWidth's own huge fallback exists for: padding to
		// that placeholder would try to build a gigabyte-long string.
		label = padRight(label, width)
	}

	nameCell := tview.NewTableCell(label).SetTextColor(color)
	nameCell.SetReference(ref)
	nameCell.SetClickedFunc(func() bool {
		return p.handleNameClick(row)
	})
	if tinted {
		// Supersedes the inline DirectoryBackground tag above rather than
		// layering with it: SetBackgroundColor repaints this cell's whole
		// rectangle, tag-highlighted name text included, not just the
		// blank padding around it — deliberate, so a clipboard-held
		// directory's row still reads as one consistent color instead of
		// two different backgrounds fighting for the same few characters.
		nameCell.SetBackgroundColor(bg)
	}
	// See paintFixedRowCells' own doc comment for the full reasoning —
	// unconditional on tinted, unlike SetBackgroundColor just above:
	// rowSelectedStyle already returns tcell.StyleDefault for the
	// untinted case, exactly matching what this freshly constructed
	// cell already starts with anyway.
	nameCell.SetSelectedStyle(selStyle)
	p.table.SetCell(row, colName, nameCell)

	// ".." (checkable false) has no real Entry behind it, so ref.size/
	// modTime are just zero values — blank cells instead of formatting
	// those into a nonsense "0B"/"0001-01-01 00:00:00".
	sizeText, mtimeText := "", ""
	if ref.checkable {
		sizeText = formatSizeCell(ref.size, p.sizeBytes)
		mtimeText = formatModTimeCell(ref.modTime, p.mtimeUnix)
	}
	sizeSepCell := p.columnSeparator()
	sizeCell := tview.NewTableCell(padLeft(sizeText, p.layout.size)).SetTextColor(p.theme.Text)
	modSepCell := p.columnSeparator()
	modCell := tview.NewTableCell(padLeft(mtimeText, p.layout.mod)).SetTextColor(p.theme.Text)
	if tinted {
		sizeSepCell.SetBackgroundColor(bg)
		sizeCell.SetBackgroundColor(bg)
		modSepCell.SetBackgroundColor(bg)
		modCell.SetBackgroundColor(bg)
	}
	// See nameCell's own SetSelectedStyle call above for why this is
	// unconditional on tinted.
	sizeSepCell.SetSelectedStyle(selStyle)
	sizeCell.SetSelectedStyle(selStyle)
	modSepCell.SetSelectedStyle(selStyle)
	modCell.SetSelectedStyle(selStyle)
	p.table.SetCell(row, colSizeSep, sizeSepCell)
	p.table.SetCell(row, colSize, sizeCell)
	p.table.SetCell(row, colModifiedSep, modSepCell)
	p.table.SetCell(row, colModified, modCell)
}

// nameColumnWidth is how much room a row's name has. Falls back to a
// generous width before the panel has ever been drawn (layout.name is
// zero then), so a listing built before the first draw isn't shortened
// against a width of nothing — relayoutColumns re-renders it with the
// real figure as soon as there is one.
func (p *Panel) nameColumnWidth() int {
	if p.layout.name <= 0 {
		return 1 << 30
	}
	return p.layout.name
}

// nameHighlightTags wraps escapedName (see addRow's own escaping, right
// before this is called) in a tview style tag that sets only its
// background to bg — "[:#rrggbb:]" leaves the foreground field empty,
// which tview's tag parser reads as "no change" rather than "reset" (see
// parseTag in tview/strings.go), so the entry's own EntryNormal/
// EntryExecutable/EntryError text color (set via SetTextColor on the
// whole cell, see addRow) still shows through unchanged. "[-:-:-]" then
// resets background (and everything else) back to the cell's base style
// for whatever text follows — the trailing "/" or " -> target" arrow, and
// the column's own blank padding, all stay in the table's ordinary
// background rather than picking up bg too.
func nameHighlightTags(escapedName string, bg tcell.Color) string {
	return fmt.Sprintf("[:#%06x:]%s[-:-:-]", uint32(bg.Hex())&0xffffff, escapedName)
}

// columnSeparator is a new cell for the bare "│" columns dividing
// Name/Size/Modified (colSizeSep/colModifiedSep) — a thin rule between
// columns rather than a full box-drawing border around them (see the
// Panel doc comment on why this codebase avoids those generally; this is
// the one deliberate exception, at the user's own request, scoped to
// just these column boundaries). Always present, even on the ".." row:
// it's structural, not data, so there's nothing to blank out the way
// Size/Modified's own values are for that row.
func (p *Panel) columnSeparator() *tview.TableCell {
	return tview.NewTableCell("│").SetTextColor(p.theme.Text)
}

// formatSizeCell renders size as either the exact byte count
// (bytesMode) or humanSize's shorthand.
//
// Unpadded: how wide the column ends up is decided per render from what
// the whole listing actually contains (see computeColumnLayout in
// columns.go), not by a constant here. Both this and formatModTimeCell
// used to pad to a fixed width — 14 and 21 columns — which reserved
// room for a worst case that most directories never contain and, in a
// split pane, pushed the Size and Modified columns off the right edge
// entirely.
func formatSizeCell(size int64, bytesMode bool) string {
	if bytesMode {
		return groupThousands(size)
	}
	return humanSize(size)
}

// formatModTimeCell renders t as either a Unix timestamp (unixMode) or
// the same "2006-01-02 15:04:05" layout the Properties overlay's
// Modified field uses. Unpadded — see formatSizeCell.
func formatModTimeCell(t time.Time, unixMode bool) string {
	if unixMode {
		return strconv.FormatInt(t.Unix(), 10)
	}
	return t.Format("2006-01-02 15:04:05")
}

// measureDataColumns is the widest Size and Modified value the given
// rows would render to, in the formats currently in force — what
// computeColumnLayout sizes those two columns against.
func measureDataColumns(refs []rowRef, bytesMode, unixMode bool) (widestSize, widestMod int) {
	for _, ref := range refs {
		if !ref.checkable {
			continue // ".." has no size or time of its own — see addRow
		}
		if w := utf8.RuneCountInString(formatSizeCell(ref.size, bytesMode)); w > widestSize {
			widestSize = w
		}
		if w := utf8.RuneCountInString(formatModTimeCell(ref.modTime, unixMode)); w > widestMod {
			widestMod = w
		}
	}
	return widestSize, widestMod
}

// relayoutColumns recomputes the column widths for the panel's current
// width and re-renders every row's Name/Size/Modified cell into them.
//
// Re-renders from each row's own stored rowRef rather than re-reading
// the directory: nothing about the filesystem has changed, only how much
// room there is to show it in, and a reload here would turn every
// terminal resize into a burst of disk access.
//
// Returns whether anything actually changed, so the draw hook that calls
// this can avoid asking for a redraw it doesn't need.
func (p *Panel) relayoutColumns(width int) bool {
	if width <= 0 {
		return false
	}

	refs := make([]rowRef, 0, p.table.GetRowCount())
	for row := 0; row < p.table.GetRowCount(); row++ {
		if ref, ok := p.rowRef(row); ok {
			refs = append(refs, ref)
		}
	}

	widestSize, widestMod := measureDataColumns(refs, p.sizeBytes, p.mtimeUnix)
	layout := computeColumnLayout(width, widestSize, widestMod, p.inTrashView,
		p.sortKey == sortBySize, p.sortKey == sortByModified)
	if layout == p.layout {
		return false
	}
	p.layout = layout

	focused := p.table.HasFocus()
	for row := 0; row < p.table.GetRowCount(); row++ {
		ref, ok := p.rowRef(row)
		if !ok {
			continue
		}
		p.setRowCells(row, ref, focused)
	}
	p.buildColumnHeader()
	return true
}

// sortArrow is the small suffix buildColumnHeader appends to whichever
// column label is the current sort key, showing its direction. Plain
// arrows (U+2191/U+2193), not a geometric triangle: those are Unicode
// "ambiguous width", a rendering risk not worth taking here when a
// narrow, universally single-width alternative exists (the checkbox's
// circles are this codebase's one deliberate exception, at the user's
// explicit request — not a precedent to keep spending here).
func sortArrow(descending bool) string {
	if descending {
		return " ↓"
	}
	return " ↑"
}

// captureColumnHeaderMouse suppresses tview.Table's own default
// MouseLeftDown handling for columnHeader — a real, user-reported
// regression otherwise: tview.Table.MouseHandler (verified directly
// against its own table.go, not assumed) unconditionally calls
// setFocus(t) on MouseLeftDown before anything else runs, regardless of
// which cell (if any) is actually clicked. columnHeader is a bare
// non-navigable label row (SetSelectable(false, false) — see its own
// construction), never meant to hold real keyboard focus, so every
// click on it — Name/Size/Modified to sort, or the header's own
// select-all checkbox — silently stole focus away from the table
// underneath, leaving c/x/v/d and every other plain-key shortcut dead
// until something else happened to refocus the table again.
//
// MouseLeftClick itself is deliberately passed through unchanged rather
// than replaced: tview.Table.MouseHandler's own MouseLeftClick case
// only runs each cell's own Clicked callback (setSortKey/
// toggleSelectAllViaHeader, wired in buildColumnHeader below) — it
// never calls setFocus itself (again verified directly against table.go)
// — so letting it fall through still fires the intended action without
// ever touching focus, the exact same InRect-then-"only click passes"
// shape captureTabStripMouse already uses for the same reason.
func (p *Panel) captureColumnHeaderMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if !p.columnHeader.InRect(event.Position()) {
		return action, event
	}
	if action != tview.MouseLeftClick {
		return tview.MouseConsumed, nil
	}
	return action, event
}

// suppressButtonFocusSteal wraps btn's own SetMouseCapture so a click
// on it never grants real keyboard focus to btn itself — tview's own
// Button.MouseHandler (verified directly against its own button.go)
// unconditionally calls setFocus(b) on MouseLeftDown, before Selected
// ever runs. The same InRect-then-suppress-non-click shape
// captureColumnHeaderMouse/captureButtonBarMouse already use to close
// the same class of bug for those: MouseLeftClick is passed through
// unchanged (Button.MouseHandler's own MouseLeftClick case only calls
// Selected, never setFocus — verified the same way), so the button's
// own action still fires normally.
func suppressButtonFocusSteal(btn *tview.Button) {
	btn.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if !btn.InRect(event.Position()) {
			return action, event
		}
		if action != tview.MouseLeftClick {
			return tview.MouseConsumed, nil
		}
		return action, event
	})
}

// buildColumnHeader (re)builds columnHeader's one row: the checkbox
// column gets a clickable ○/● of its own (see toggleSelectAllViaHeader)
// — an additional way to trigger the context menu's Select all/Deselect
// all, right where the data rows' own checkboxes are; type/modifier get
// a blank cell (self-explanatory via their own glyphs, no label needed);
// Name/Size/Modified get their own clickable cell — click sorts by that
// column (see setSortKey), starting ascending if it wasn't already the
// active key, or reversing direction if it was. The active column's
// label gets sortArrow's suffix.
//
// This table's columns only end up matching table's own widths because
// every cell in both tables is explicitly padded to the exact same
// externally-computed width, not because the two tables' widths happen
// to agree on their own: colCheckbox/colType/colModifier are always
// exactly 1 character wide in both tables (their content is always
// exactly that long), colSize/colModified are always padded to
// p.layout.size/mod (see formatSizeCell/formatModTimeCell and padLeft),
// and colName is always padded to p.layout.name (see padRight, and
// setRowCells' identical treatment for the data table's own Name
// cells) — deliberately not left to tview.Table's own per-table
// Expansion/leftover-distribution math, which turned out not to
// reliably agree between two *separate* Table widgets even when their
// own inputs (p.layout) were identical (a real, user-reported bug —
// see padRight's own doc comment for the full reasoning and how it was
// confirmed live, not assumed).
func (p *Panel) buildColumnHeader() {
	p.columnHeader.Clear()

	checkboxHeader := tview.NewTableCell(checkboxText(p.allSelected())).SetTextColor(p.theme.Text)
	checkboxHeader.SetClickedFunc(func() bool {
		p.toggleSelectAllViaHeader()
		return false
	})
	p.columnHeader.SetCell(0, colCheckbox, checkboxHeader)
	p.columnHeader.SetCell(0, colType, tview.NewTableCell(" ").SetTextColor(p.theme.Text))
	p.columnHeader.SetCell(0, colModifier, tview.NewTableCell(" ").SetTextColor(p.theme.Text))

	nameLabel := "Name"
	if p.sortKey == sortByName {
		nameLabel += sortArrow(p.sortDescending)
	}
	if width := p.layout.name; width > 0 {
		// See padRight's own doc comment: padding to the exact same
		// externally-computed width setRowCells pads every data row's
		// own Name cell to is what keeps this header's Name column from
		// drifting a column off from the data table's, rather than
		// leaving it to SetExpansion's own per-table leftover math.
		nameLabel = padRight(nameLabel, width)
	}
	nameCell := tview.NewTableCell(nameLabel).SetTextColor(p.theme.Text)
	nameCell.SetClickedFunc(func() bool {
		p.setSortKey(sortByName)
		return false
	})
	p.columnHeader.SetCell(0, colName, nameCell)

	// Widths and labels both come from the layout, which sized them
	// against the data actually on screen and picked the longest header
	// variant that fits (see computeColumnLayout) — including the
	// trash's own "Deletion time" wording, see onDescribeRows.
	p.columnHeader.SetCell(0, colSizeSep, p.columnSeparator())
	p.setColumnHeaderCell(colSize, p.layout.size, p.layout.sizeHeader, sortBySize)
	p.columnHeader.SetCell(0, colModifiedSep, p.columnSeparator())
	p.setColumnHeaderCell(colModified, p.layout.mod, p.layout.modHeader, sortByModified)
}

// setColumnHeaderCell builds one of columnHeader's fixed-width, right-
// aligned Size/Modified cells — see buildColumnHeader.
func (p *Panel) setColumnHeaderCell(col, width int, label string, key sortKey) {
	text := label
	if p.sortKey == key {
		text += sortArrow(p.sortDescending)
	}
	cell := tview.NewTableCell(padLeft(text, width)).SetTextColor(p.theme.Text)
	cell.SetClickedFunc(func() bool {
		p.setSortKey(key)
		return false
	})
	p.columnHeader.SetCell(0, col, cell)
}

// setSortKey is what clicking a column header does: switch to sorting by
// key, starting ascending — or, if key is already the active one, flip
// direction instead. The same "click a new column: ascending; click the
// active one again: reverse" convention most file managers use.
func (p *Panel) setSortKey(key sortKey) {
	if p.sortKey == key {
		p.sortDescending = !p.sortDescending
	} else {
		p.sortKey = key
		p.sortDescending = false
	}
	if p.searchMode {
		// Nothing to re-read from disk (see renderSearchEntries' own
		// doc comment) — searchEntries already holds everything there
		// is; a sort-key change here just re-renders it.
		p.renderSearchEntries()
		return
	}
	p.reportError(p.load(p.path))
}

// checkboxText renders the checkbox column's two states as an outline vs.
// filled circle (○/●) — breakthrough already commits to UTF-8 support
// (see docs/whitepaper.md). A blank space for "unchecked" was considered
// and rejected: it would make the checkbox column invisible whenever a
// row isn't checked, losing the "there's something clickable here" cue
// entirely rather than just being subtle about it (see the ballot-box
// glyphs this replaced, and addRow's now-highlight-on-click for the
// other half of that fix).
func checkboxText(checked bool) string {
	if checked {
		return "●"
	}
	return "○"
}

// typeGlyph renders ref's type character, matching Midnight Commander's
// own single-character "type" panel field exactly (confirmed against
// source.midnight-commander.org/man/mc.html — not guessed): '*' for an
// executable file, '/' directory, '~' symlink to a directory, '@'
// symlink to a file, '!' a broken symlink, '=' socket, '|' FIFO, '-'
// character device, '+' block device. A plain, non-executable file gets
// no character at all (blank), same as MC.
//
// This is deliberately just the one character MC itself uses — no
// modifier tacked on, unlike an earlier version of this column that
// packed a mount-point/hard-link marker in as a second character next to
// it: the two glyphs then had no gap between them (e.g. "~>"), reading as
// cramped compared to every other row's single character plus real
// column spacing. See modifierGlyph, colModifier for where that
// information moved instead: its own column, so the table's own
// inter-column gap does the separating rather than a hand-picked
// character.
func typeGlyph(ref rowRef) byte {
	switch ref.entryType {
	case fsops.TypeDir:
		return '/'
	case fsops.TypeSymlinkDir:
		return '~'
	case fsops.TypeSymlinkFile:
		return '@'
	case fsops.TypeSymlinkBroken:
		return '!'
	case fsops.TypeSocket:
		return '='
	case fsops.TypeFIFO:
		return '|'
	case fsops.TypeCharDevice:
		return '-'
	case fsops.TypeBlockDevice:
		return '+'
	case fsops.TypeFile:
		if ref.mode&0o111 != 0 { // any executable bit (owner, group, or other)
			return '*'
		}
	}
	return ' '
}

// entryColor sets a row's name apart by color for every case worth
// flagging beyond the type glyph alone, per p.theme's own Entry* fields
// (the default scheme's red/green for EntryError/EntryExecutable match
// Midnight Commander's own default skin). Unlike MC, this is applied
// only to the name cell, not the narrow type-indicator column (see
// addRow) — the glyph itself stays plain Text, so the type column reads
// as punctuation rather than a second, redundant color cue. Checked in
// this order, most specific/urgent first:
//
//  1. A broken symlink — nothing to open at all.
//  2. archiveHit — a search-mode display concern (see its own doc
//     comment on rowRef), unrelated to the entry's own file type, so it
//     stays ahead of every type-based case below.
//  3. The four special types (socket/FIFO/char/block device) — not
//     really "readable content" in the EntryNormal/EntryExecutable
//     sense at all, regardless of what ref.unreadable says.
//  4. ref.unreadable — the invoking user can't actually read this
//     entry's content, which matters more than whether it also happens
//     to look like an archive or be executable.
//  5. A recognized archive extension (never for a directory, even one
//     literally named like an archive — see isArchiveName).
//  6. A working symlink to a file (TypeSymlinkBroken, the one other
//     symlink case, was already handled in step 1).
//  7. An executable TypeFile.
//  8. A dotfile/dotdir name (anything starting with "." except ".."
//     itself — see EntryHidden's own doc comment) — checked dead last:
//     every case above says something more specific and more worth
//     noticing than "this is hidden", so none of them are ever dimmed
//     by this instead.
//  9. Everything else: EntryNormal.
func (p *Panel) entryColor(ref rowRef) tcell.Color {
	switch {
	case ref.entryType == fsops.TypeSymlinkBroken:
		return p.theme.EntryError
	case ref.archiveHit:
		// The same lighter, "auxiliary information" gray the search
		// dialog's own hint texts use (see internal/ui/search.go's
		// hintText) rather than a dedicated new theme color — an
		// archive-member row isn't a real file/directory match the way
		// every other row here is, so it reads a shade less prominent,
		// per the user's own explicit request.
		return p.theme.PlaceholderText
	case ref.entryType == fsops.TypeSocket, ref.entryType == fsops.TypeFIFO,
		ref.entryType == fsops.TypeCharDevice, ref.entryType == fsops.TypeBlockDevice:
		return p.theme.EntrySpecial
	case ref.unreadable:
		return p.theme.EntryUnreadable
	case !ref.isDir && isArchiveName(ref.name):
		return p.theme.EntryArchive
	case ref.entryType == fsops.TypeSymlinkFile:
		return p.theme.EntrySymlink
	case ref.entryType == fsops.TypeFile && ref.mode&0o111 != 0:
		return p.theme.EntryExecutable
	case ref.name != ".." && strings.HasPrefix(ref.name, "."):
		return p.theme.EntryHidden
	default:
		return p.theme.EntryNormal
	}
}

// archiveHighlightExtensions is the set of filename suffixes
// isArchiveName recognizes — deliberately broader than
// internal/search's own archiveExtensions (see that var's own doc
// comment on its "Stufe A" scope): this is a purely visual "this looks
// like an archive" cue, not a claim that Search's own Include Archives
// option can look inside it, so it also covers formats Search doesn't
// support yet (7z, rar, zstd, a lone gzip/bzip2/xz/lzma) alongside the
// zip/tar family Search already handles.
var archiveHighlightExtensions = []string{
	".zip", ".tar", ".tgz", ".tbz", ".tbz2", ".txz",
	".tar.gz", ".tar.bz2", ".tar.xz",
	".gz", ".bz2", ".xz", ".lzma", ".lz",
	".7z", ".rar", ".zst",
}

// isArchiveName reports whether name's own extension (checked
// case-insensitively, matching every other extension check in this
// codebase — see internal/search's own classifyArchive) matches one of
// archiveHighlightExtensions. Only ever consulted for a non-directory
// entry (see entryColor) — a directory literally named e.g.
// "backup.tar.gz" isn't actually an archive, just confusingly named one.
func isArchiveName(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range archiveHighlightExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// modifierGlyph renders ref's modifier column — information MC's own
// type field doesn't carry at all, so unlike typeGlyph this isn't
// borrowed from anywhere, just breakthrough's own addition: '>' for a
// mount point (a directory, or directory symlink, sitting on a different
// filesystem than the one being listed — a separate partition, an NFS
// share, an fstab bind mount, ...), or '&' for a plain file with more
// than one hard link (content that also exists under another name — see
// rowRef's own doc comment on why that's not tracked for ".."). The two
// never apply to the same entry, so one column comfortably covers both.
func modifierGlyph(ref rowRef) byte {
	switch {
	case ref.mountPoint && (ref.entryType == fsops.TypeDir || ref.entryType == fsops.TypeSymlinkDir):
		return '>'
	case ref.entryType == fsops.TypeFile && ref.nlink > 1:
		return '&'
	}
	return ' '
}

// rowRef returns the rowRef attached to row's name cell, if any.
func (p *Panel) rowRef(row int) (rowRef, bool) {
	cell := p.table.GetCell(row, colName)
	if cell == nil {
		return rowRef{}, false
	}
	ref, ok := cell.GetReference().(rowRef)
	return ref, ok
}

// toggleCheckbox flips whether row's entry is selected and updates the
// checkbox cell's text to match. A no-op for the ".." row (checkable
// false) or a row with no rowRef at all — the latter shouldn't be
// reachable, but this stays defensive rather than assume it. The ".."
// guard matters here specifically because, unlike a plain mouse click,
// captureTableKey's Space shortcut reaches this directly regardless of
// which row is currently selected.
func (p *Panel) toggleCheckbox(row int) {
	ref, ok := p.rowRef(row)
	if !ok || !ref.checkable {
		return
	}
	p.setChecked(row, !p.selected[ref.path])
}

// setChecked sets row's checkbox state directly to checked (rather than
// flipping it, as toggleCheckbox does) and updates the cell's text to
// match. Shares toggleCheckbox's guards, and is what it's built on top
// of; also used directly by selectAll/deselectAll/selectByPattern, which
// all need to set a specific target state rather than toggle. Every
// selection path funnels through here — including live drag-toggling —
// which is why refreshHeaderCheckbox lives here too, rather than needing
// each of those callers to remember to keep the column header's own
// checkbox in sync themselves.
func (p *Panel) setChecked(row int, checked bool) {
	ref, ok := p.rowRef(row)
	if !ok || !ref.checkable {
		return
	}

	if checked {
		p.selected[ref.path] = true
	} else {
		delete(p.selected, ref.path)
	}

	if cell := p.table.GetCell(row, colCheckbox); cell != nil {
		cell.SetText(checkboxText(checked))
	}
	p.refreshHeaderCheckbox()
}

// refreshHeaderCheckbox keeps the column header's own checkbox glyph
// (see buildColumnHeader/toggleSelectAllViaHeader) in sync with whether
// every checkable row is currently selected.
func (p *Panel) refreshHeaderCheckbox() {
	if cell := p.columnHeader.GetCell(0, colCheckbox); cell != nil {
		cell.SetText(checkboxText(p.allSelected()))
	}
}

// allSelected reports whether every checkable row in the current listing
// is currently selected. False for a listing with nothing checkable in
// it at all (just "..", or nothing loaded yet) — an all-unchecked master
// checkbox is what "nothing to select" should look like, not a
// misleadingly-filled one.
func (p *Panel) allSelected() bool {
	anyCheckable := false
	for row := 0; row < p.table.GetRowCount(); row++ {
		ref, ok := p.rowRef(row)
		if !ok || !ref.checkable {
			continue
		}
		anyCheckable = true
		if !p.selected[ref.path] {
			return false
		}
	}
	return anyCheckable
}

// toggleSelectAllViaHeader is the column header's checkbox's own action
// (see buildColumnHeader): selects everything if it isn't all selected
// yet, deselects everything if it already is — the same two actions the
// context menu's Select all/Deselect all already offer, just reachable
// without opening the menu.
func (p *Panel) toggleSelectAllViaHeader() {
	if p.allSelected() {
		p.deselectAll()
	} else {
		p.selectAll()
	}
}

// selectAll checks every checkable row in the current listing — the
// context menu's "Select all".
func (p *Panel) selectAll() {
	for row := 0; row < p.table.GetRowCount(); row++ {
		p.setChecked(row, true)
	}
}

// deselectAll unchecks every row in the current listing — the context
// menu's "Deselect all".
func (p *Panel) deselectAll() {
	for row := 0; row < p.table.GetRowCount(); row++ {
		p.setChecked(row, false)
	}
}

// invertSelection flips every row's own checked state — checked becomes
// unchecked and vice versa. The plain-letter keyboard layer's own "*",
// Midnight Commander's own convention for exactly this (its own "+"/"-"
// select/deselect a whole pattern; "*" is the one that inverts what's
// already there, and this app already borrows the first two for the
// context menu's "Select +"/"Select -").
//
// Goes through toggleCheckbox rather than reading/writing p.selected
// directly: that already skips ".." on its own (see its own doc
// comment), so this doesn't need to re-derive checkability itself.
func (p *Panel) invertSelection() {
	for row := 0; row < p.table.GetRowCount(); row++ {
		p.toggleCheckbox(row)
	}
}

// selectByPattern sets checked on every row whose name matches pattern —
// shell glob syntax, as filepath.Match understands it (the same
// convention the header's path completion already uses). Backs the
// context menu's "Select +" (checked=true) and "Select -" (checked=
// false), the classic file-manager pair for marking/unmarking a whole
// group by wildcard instead of clicking each entry. Returns the number of
// rows that matched; an error is only ever a malformed pattern
// (filepath.ErrBadPattern) — matching nothing is not an error, just zero.
func (p *Panel) selectByPattern(pattern string, checked bool) (matched int, err error) {
	for row := 0; row < p.table.GetRowCount(); row++ {
		ref, ok := p.rowRef(row)
		if !ok || !ref.checkable {
			continue
		}
		hit, err := filepath.Match(pattern, ref.name)
		if err != nil {
			return matched, err
		}
		if hit {
			p.setChecked(row, checked)
			matched++
		}
	}
	return matched, nil
}

// SelectedPaths returns the absolute paths currently checked, in no
// particular order. Used by Root's Copy/Cut to capture what the clipboard
// should act on.
func (p *Panel) SelectedPaths() []string {
	paths := make([]string, 0, len(p.selected))
	for path := range p.selected {
		paths = append(paths, path)
	}
	return paths
}

// captureTableKey handles the one key the table needs beyond its built-in
// navigation: Space toggles the checkbox on the currently selected row,
// the same action a click on that row's checkbox performs.
func (p *Panel) captureTableKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyRune && event.Rune() == ' ' {
		row, _ := p.table.GetSelection()
		p.toggleCheckbox(row)
		return nil
	}
	if event.Key() == tcell.KeyEscape && p.searchMode && p.onSearchEscape != nil {
		p.onSearchEscape()
		return nil
	}
	return event
}

// nameRenameClickWithin is handleNameClick's own timing window for a
// second click landing on the same row as the previous one (see its
// own doc comment): at or under this counts as the slower, deliberate
// click-pause-click rename gesture, per the user's own explicit
// request ("Klick, 1 Sekunde warten, und noch ein Klick... zum
// Umbennen"); slower than this is treated as a fresh first click
// instead. There's no separate, shorter "this counts as a double-click"
// window to check here at all — a second click fast enough for that
// never reaches this function in the first place (see handleNameClick's
// own doc comment on why: tview's Application itself intercepts it as
// MouseLeftDoubleClick before Table.MouseHandler, which has no case for
// that action, ever gets to call this). The upper bound here is
// deliberately generous around the user's own "one second" —
// comfortably reachable by an unhurried second click without
// stretching so far that an unrelated, much later click could still
// mistakenly trigger it.
const nameRenameClickWithin = 1500 * time.Millisecond

// handleNameClick is what a plain left-click (MouseLeftClick) on the
// name cell does — Enter still always activates immediately (see
// NewPanel's own SetSelectedFunc), unaffected by any of this. A single
// click on a row that isn't already the one handleNameClick itself last
// saw clicked only selects it (returns false, the same "let tview's own
// default Table.MouseHandler apply the selection" this always did for a
// click activateRow itself no-ops on) — per the user's own explicit
// report that a single click immediately navigating into a directory
// (the previous, direct name.SetClickedFunc -> activateRow wiring) was
// too eager, especially with the Details sidebar open for browsing
// around: a folder should highlight/select first, exactly like a file
// already effectively did, not jump into it on the very first click.
//
// Double-clicking to activate it instead (per the user's own explicit
// request: "Doppelklick statt Klickverhalten") is NOT handled here at
// all, even though it might look like the natural place for it — a
// real double-click's second release fires tview's own
// MouseLeftDoubleClick action instead of a second MouseLeftClick
// (verified directly against tview's own Application.fireMouseActions
// source, not guessed: DoubleClickInterval, 500ms by default, decides
// which one), and Table.MouseHandler has no case for that action at
// all, so it would never reach a TableCell's own ClickedFunc — this
// function — a second time in the first place. See Root.captureMouse's
// own MouseLeftDoubleClick case for where activation actually happens.
//
// What this function DOES still own is the slower gesture: a second
// click on the same row — lastNameClickRow, tracked regardless of
// whatever the table's own current selection is, so an
// arrow-key-selected row doesn't retroactively count as "clicked",
// and reset to -1 on every Panel.load so a stale row index from
// whatever the *previous* directory's listing had can never coincide
// with a freshly loaded one — within nameRenameClickWithin runs the
// click-pause-click rename gesture (see Root.renameRow, wired as
// onRenameGesture) — for a file exactly as much as a directory, per the
// user's own explicit request that the two behave the same way here.
// Slower than that, or a click on a different row, is simply a fresh
// first click again.
func (p *Panel) handleNameClick(row int) bool {
	now := time.Now()
	isRepeat := row == p.lastNameClickRow
	elapsed := now.Sub(p.lastNameClickTime)
	p.lastNameClickRow = row
	p.lastNameClickTime = now

	if isRepeat && elapsed <= nameRenameClickWithin {
		if p.onRenameGesture != nil {
			p.onRenameGesture(row)
		}
		return true
	}
	return false
}

// activateRow is handleNameClick's own double-click case, and what
// Enter always does directly and immediately (see NewPanel's own
// SetSelectedFunc) regardless of any click timing at all. While
// showing a real directory: enter the row's directory; for a regular
// file, try Look instead (onOpenFile — per the user's own explicit
// request that Enter/double-click on a file behave as usefully as they
// already do on a directory, rather than doing nothing). Ignores the
// checkbox column: a click there is handled by its own
// TableCell.ClickedFunc instead (see addRow), which returns true
// specifically so this never also runs for it.
//
// While showing search results (searchMode): a content-search match
// (ref.searchLine > 0 — see rowRef's own doc comment) opens the file
// in the configured editor, at that line, via onOpenSearchResult —
// per the user's own explicit request, and deliberately without
// leaving search mode: unlike a plain jump, this reads as "peek at
// this match," and a content search often has several, each worth
// checking in turn without losing the list between them. Every other
// result — a filename match, a content match with onOpenSearchResult
// left nil, or a content match *inside* an archive member
// (ref.archiveHit — see archivecontent.go's own Result.ArchiveMember,
// which can now be set alongside a real Line/Text too, not just for a
// filename-only archive-member hit) — is never "this panel's own
// directory" the way a real row's target always is, so there's nothing
// to navigate *into*, or (for the archive case specifically) nothing
// real at ref.path/ref.searchLine to open at all — ref.path is the
// containing archive file itself, not the matched member's own
// extracted content, and opening an archive in a text editor at an
// arbitrary line makes no sense; instead this leaves search mode
// entirely and jumps to the result's real location (see
// navigateAndSelect), the same "Go to file/folder" meaning left-click
// on a result has always had otherwise.
//
// Returns whether IT already settled the table's own selection itself
// (a real user report: it always did, but the original caller —
// addRow's own name.SetClickedFunc, before handleNameClick sat between
// the two — used to unconditionally tell tview.Table to *also* select
// whatever cell the click's own screen position landed on.
// Table.MouseHandler computes that position before ever calling
// this func at all, so once navigate/navigateAndSelect has gone on to
// clear and rebuild the whole table underneath it — a genuinely
// different directory, with completely different rows now sitting at
// that same numeric index — tview's own follow-up selection silently
// overwrote a correct, deliberate one (e.g. landing exactly on a
// search result's own real archive file) with whatever row happened to
// occupy the *old* table's row/column index instead, typically the
// wrong entry entirely (verified against tview's own Table.MouseHandler
// source, not guessed). Only the branches that never touch the table
// itself (a content match, a regular file's own onOpenFile — Look never
// rebuilds this panel's own listing — and any click activateRow
// otherwise no-ops on) return false, letting tview's own default
// selection still provide the "row highlights" feedback a real file
// click would otherwise leave invisible.
func (p *Panel) activateRow(row int) (handledSelection bool) {
	ref, ok := p.rowRef(row)
	if !ok {
		return false
	}
	if p.searchMode {
		if ref.searchLine > 0 && !ref.archiveHit && p.onOpenSearchResult != nil {
			p.onOpenSearchResult(ref.path, ref.searchLine)
			return false
		}
		p.searchMode = false
		if p.onExitSearchResults != nil {
			p.onExitSearchResults()
		}
		p.reportError(p.navigateAndSelect(ref.path))
		return true
	}
	if !ref.isDir {
		if p.onOpenFile != nil { // ".." is always isDir true, so this is always a real file
			p.onOpenFile()
		}
		return false
	}
	p.reportError(p.navigate(ref.path))
	return true
}

// nameCellRect returns the on-screen position and width row's name cell
// occupied the last time the table was drawn (see TableCell.GetLastPosition),
// or ok=false if row doesn't exist. Used to position the rename field
// exactly over the name column without also covering the checkbox
// column, so the checkbox stays visible — showing what's otherwise
// selected — while renaming, without becoming part of what's editable.
func (p *Panel) nameCellRect(row int) (x, y, width int, ok bool) {
	if _, ok := p.rowRef(row); !ok {
		return 0, 0, 0, false
	}
	x, y, width = p.table.GetCell(row, colName).GetLastPosition()
	return x, y, width, true
}

// RowAt returns the absolute path of the entry at screen position (x, y),
// or ok=false if that position isn't a selectable entry — outside the
// table, past the last row, or the ".." row, which isn't a file operation
// target. Used by Root to find which entry was right-clicked.
func (p *Panel) RowAt(x, y int) (path string, ok bool) {
	row, ok := p.rowIndexAt(x, y)
	if !ok {
		return "", false
	}
	ref, _ := p.rowRef(row) // rowIndexAt already confirmed this succeeds
	if ref.name == ".." {
		return "", false
	}
	return ref.path, true
}

// CurrentRowPath is RowAt's keyboard equivalent: the row and absolute
// path of whichever entry the table's own cursor (arrow-key navigation)
// currently sits on, rather than one under a screen position. Used by
// Root's keyboard-triggered actions ("e" Edit, "r" Rename, "i"
// Properties) that have no right-clicked position to work from. ok is
// false for the ".." row (not a file operation target, matching RowAt)
// or an empty table.
func (p *Panel) CurrentRowPath() (row int, path string, ok bool) {
	row, _ = p.table.GetSelection()
	ref, ok := p.rowRef(row)
	if !ok || ref.name == ".." {
		return 0, "", false
	}
	return row, ref.path, true
}

// rowIndexAt returns the row index at screen position (x, y), or
// ok=false if that position doesn't correspond to a populated row.
// Unlike RowAt, this doesn't exclude "..": a caller working with a
// contiguous range of rows (see selectRange) needs the real index either
// way, and toggling ".." is already a no-op (not checkable).
//
// Checks p.table.InRect first — not just tview's own Table.CellAt,
// which validates a computed row against the table's own *data* size
// (how many entries there are), not against how many of them actually
// fit on screen right now. With bashLine able to occupy a large part of
// the screen while expanded (see expandBashConsole), a position well
// below the table's own current, shrunk bounds could otherwise still
// arithmetically land on a real row further down the (longer) listing
// — exactly the bug the user reported: a right-click over the expanded
// console still opened the panel's own context menu for whatever row
// the position numerically mapped to. SetMouseCapture's own contract
// (see tview's own Box.WrapMouseHandler) doesn't filter by position on
// its own; every caller into this needs to.
func (p *Panel) rowIndexAt(x, y int) (row int, ok bool) {
	if !p.table.InRect(x, y) {
		return 0, false
	}
	row, _ = p.table.CellAt(x, y)
	if _, ok := p.rowRef(row); !ok {
		return 0, false
	}
	return row, true
}

// focusRow moves the table's own current-row highlight — and, via arrow
// keys, where keyboard navigation continues from — to row. This is
// distinct from the checkbox column's per-entry marking (see selected,
// toggleCheckbox): a right-click or right-drag on a row that wasn't
// already the highlighted one would otherwise leave the highlight
// pointing somewhere else while the context menu or the just-toggled
// checkboxes are clearly about a different row.
func (p *Panel) focusRow(row int) {
	p.table.Select(row, colName)
}

// applyDragDelta toggles exactly the rows whose membership in the range
// [start, to] differs from their membership in [start, from] — the rows
// a right-button drag has newly entered or newly left since the last
// update, each toggled exactly once. This is what lets Root.captureMouse
// update the toggled state live, once per MouseMove, without re-toggling
// (and so silently cancelling) rows the drag already passed over: calling
// this repeatedly as the range grows, shrinks, or reverses direction
// always leaves each row's state consistent with whether it's currently
// inside [start, to], not with how many times a naive re-toggle-the-whole-
// range would have flipped it. toggleCheckbox already no-ops for a row
// that isn't checkable (the ".." row, if it falls within either range),
// so that's handled for free here too.
func (p *Panel) applyDragDelta(start, from, to int) {
	oldLo, oldHi := min(start, from), max(start, from)
	newLo, newHi := min(start, to), max(start, to)

	for row := min(oldLo, newLo); row <= max(oldHi, newHi); row++ {
		inOld := row >= oldLo && row <= oldHi
		inNew := row >= newLo && row <= newHi
		if inOld != inNew {
			p.toggleCheckbox(row)
		}
	}
}

// currentRow returns the table's own current cursor row (see
// tview.Table.GetSelection) — an opaque number here, never re-resolved
// against a row's own identity, used purely to freeze it away (see
// snapshotCurrentEntry) for restoreHistoryEntry's own search-snapshot
// case to later restore — a real directory's own cursorRow is captured
// the same way but deliberately never restored any more (see
// restoreHistoryEntry's own doc comment).
func (p *Panel) currentRow() int {
	row, _ := p.table.GetSelection()
	return row
}

// snapshotCurrentEntry freezes whatever the panel is showing right now
// into history[historyIdx] — the cursor row always, plus (see
// historyEntry.isSearch) a full copy of the current search results and
// status if that's what this entry actually is, so navigating back to a
// search snapshot later (see restoreHistoryEntry) restores it exactly
// rather than a plain reload silently losing the cursor, or the
// results entirely — a real directory's own frozen cursorRow is simply
// never read back out again. A no-op before any history entry exists
// yet.
//
// Checked against history[historyIdx].isSearch(), not the live
// searchMode flag: activateRow's own searchMode branch already flips
// searchMode to false *before* calling navigate (which is what
// eventually calls this), so relying on the live flag here would miss
// capturing the very results being left — the history entry's own
// recorded type is what actually matters.
func (p *Panel) snapshotCurrentEntry() {
	if len(p.history) == 0 {
		return
	}
	e := &p.history[p.historyIdx]
	e.cursorRow = p.currentRow()
	if e.isSearch() {
		e.searchEntries = append([]searchResultEntry(nil), p.searchEntries...)
		e.searchStatus = p.searchStatusText
		e.searchColor = p.searchStatusColor
	}
}

// pushHistoryEntry appends entry as the new current position, truncating
// any "forward" entries beyond where we were — the same behavior a
// browser's address bar has. Bootstraps history from scratch if this is
// the very first entry ever (navigate's own original contract: nothing
// is lost, since there was no other "place" before the first one).
// Skips the push entirely if entry duplicates whatever's already
// current — the same real directory again (navigate's own long-standing
// rule), or search mode already showing (showSearchResults' own "safe
// to call again" rule, extended to also mean no duplicate history entry
// per repeat/refined search).
func (p *Panel) pushHistoryEntry(entry historyEntry) {
	if len(p.history) == 0 {
		p.history = []historyEntry{entry}
		p.historyIdx = 0
		return
	}

	current := p.history[p.historyIdx]
	if current.isSearch() == entry.isSearch() && current.path == entry.path {
		return
	}

	p.history = append(p.history[:p.historyIdx+1], entry)
	p.historyIdx = len(p.history) - 1
}

// restoreHistoryEntry redisplays entry exactly as historyEntry
// describes it: a real directory (just load — see below for why not
// also its own cursorRow), or a frozen search-results snapshot (see
// historyEntry.isSearch) redisplayed as-is — never re-run, since a
// search might behave differently or take a while by the time anyone
// comes back to it. Used by back/forward, which — unlike navigate —
// must not push a new history entry for where they land.
func (p *Panel) restoreHistoryEntry(entry historyEntry) error {
	if entry.isSearch() {
		p.searchMode = true
		p.searchEntries = append([]searchResultEntry(nil), entry.searchEntries...)
		p.selected = make(map[string]bool)
		p.lastNameClickRow = -1 // see its own doc comment: a rebuilt table's row indices mean something new
		p.table.Clear()
		p.buildColumnHeader()
		p.renderSearchEntries()
		p.setSearchStatus(entry.searchStatus)
		p.setSearchStatusColor(entry.searchColor)
		p.focusRowClamped(entry.cursorRow)
		return nil
	}

	// No focusRowClamped(entry.cursorRow) here, unlike the search branch
	// above — per the user's own explicit request, Back/Forward into a
	// real directory always lands on row 0, the same as entering it any
	// other way already does (see load's own newDirectory check, which
	// fires here too: entry.path is never the same as whatever p.path
	// was a moment ago, or back()/forward() wouldn't have had anywhere
	// to go). Landing on the old cursor row used to be the whole point
	// of tracking it at all here — now it's just load's own ordinary
	// "opening a directory" behavior, nothing extra to add on top.
	return p.load(entry.path)
}

// focusRowClamped is focusRow, clamped to the table's own current row
// range first — a stored cursorRow can point past the end (or, for an
// empty table, below 0) if whatever it pointed at was deleted, or a
// re-run search now finds fewer results, between when it was frozen and
// when it's restored.
func (p *Panel) focusRowClamped(row int) {
	if max := p.table.GetRowCount() - 1; row > max {
		row = max
	}
	if row < 0 {
		row = 0
	}
	p.focusRow(row)
}

// navigate is load plus history bookkeeping: on success it records the
// new directory as a new historyEntry (see pushHistoryEntry),
// snapshotting wherever the panel is leaving first (see
// snapshotCurrentEntry) so returning to it later restores its own
// cursor row — or, for a search-results entry, its own frozen results —
// rather than a plain fresh load(). Discards any "forward" entries
// beyond where we were, the same behavior a browser's address bar has.
// Every user-initiated jump (opening a directory, a breadcrumb click,
// the Home button, submitting the edit field, activateRow's own
// searchMode branch via navigateAndSelect) goes through this; back()
// and forward() call restoreHistoryEntry directly instead, since they
// must not themselves push new history entries.
func (p *Panel) navigate(dir string) error {
	p.snapshotCurrentEntry()
	if err := p.load(dir); err != nil {
		return err
	}
	p.pushHistoryEntry(historyEntry{path: p.path})
	return nil
}

// navigateAndSelect is navigate's own "and land on this specific entry"
// variant — target's parent directory becomes the panel's new current
// directory (see navigate), and target's own row, if load() actually
// produced one (see the same filter/showHidden rules any other row is
// subject to — a target the current filter or a hidden-files toggle
// would exclude simply isn't found here, not an error), gets the
// cursor. Used by activateRow's own searchMode branch to jump straight
// to a search result instead of just opening the directory it's in and
// leaving the cursor wherever load()'s own top-of-listing reset put it.
func (p *Panel) navigateAndSelect(target string) error {
	if err := p.navigate(filepath.Dir(target)); err != nil {
		return err
	}
	name := filepath.Base(target)
	for row := 0; row < p.table.GetRowCount(); row++ {
		if ref, ok := p.rowRef(row); ok && ref.name == name {
			p.focusRow(row)
			break
		}
	}
	return nil
}

// reportError hands err to whoever is displaying errors, if anyone is.
// A nil error is ignored, so callers can pass a result through directly.
func (p *Panel) reportError(err error) {
	if err != nil && p.onError != nil {
		p.onError(err)
	}
}

// back steps one entry back in history, if possible — a real directory
// or a frozen search-results snapshot alike (see restoreHistoryEntry).
//
// The index only moves once the restore has actually succeeded: if a
// directory has since been deleted, moving it anyway would leave
// historyIdx pointing at a path the panel isn't showing, and the next
// navigate() would then truncate the forward entries from the wrong
// position — silently dropping a page the user never left. Snapshots
// wherever the panel is leaving first (see snapshotCurrentEntry), the
// same as navigate — so, unlike before, a subsequent forward() back to
// here still finds it exactly as it was left, not a fresh reload.
func (p *Panel) back() {
	if p.historyIdx <= 0 {
		return
	}
	p.snapshotCurrentEntry()
	if err := p.restoreHistoryEntry(p.history[p.historyIdx-1]); err != nil {
		p.reportError(err) // stay put rather than lie about where we are
		return
	}
	p.historyIdx--
}

// forward steps one entry forward in history, if possible. As in back(),
// the index only moves once the restore has succeeded, and wherever the
// panel is leaving is snapshotted first.
func (p *Panel) forward() {
	if p.historyIdx >= len(p.history)-1 {
		return
	}
	p.snapshotCurrentEntry()
	if err := p.restoreHistoryEntry(p.history[p.historyIdx+1]); err != nil {
		p.reportError(err)
		return
	}
	p.historyIdx++
}

// previousPath returns the directory navigate() most recently moved
// away from — one step back in the browser-style history (see
// back/forward) — for the bash line's own "cd -" (see
// Root.changeDirectory). false if that entry is a frozen search-results
// snapshot (see historyEntry.isSearch) rather than a real directory:
// there's nothing for "cd -" to sensibly land on there. Not a true
// shell OLDPWD toggle (repeated "cd -" walks further back through
// history rather than swapping between exactly two directories the way
// a real shell's does), but close enough for what this is actually used
// for: a quick way back to wherever the panel just was.
func (p *Panel) previousPath() (string, bool) {
	if p.historyIdx <= 0 {
		return "", false
	}
	prev := p.history[p.historyIdx-1]
	if prev.isSearch() {
		return "", false
	}
	return prev.path, true
}

// headerButtons is the fixed definition of the seven nav buttons shown
// at the start of the header row — Start/Root/Home/Back/Forward/Up/
// Reload — as one shared slice so buildHeaderSpans (the colored,
// clickable rendering) and headerButtonPrefix (headerEdit's own
// plain-text label — see its own doc comment) can never drift out of
// column-alignment with each other, verified by
// TestHeaderButtonPrefixMatchesBuildHeaderSpans.
// Start's own glyph is "∎" (U+220E), not "^": at the time this glyph
// was chosen, this app's own button bar wrote Ctrl-shortcuts as "^E",
// "^L" and so on, so a bare "^" here risked reading as one of those
// instead of a button in its own right — "∎" carries no such collision.
// The button bar has since moved to highlighting a plain letter within
// each label instead (see buildButtonBar's own highlightKey), but "∎"
// remains the right call regardless: a bare "^" would still misread as
// up/caret shorthand rather than a button of its own. "^" itself isn't
// reused for Up either, despite visually suggesting "upward": ↑ says
// that unambiguously and isn't asked to also serve as a stand-in for
// whatever Start used to mean. "⭯" (U+2B6F) is Reload — the user's own
// explicit choice of glyph, added at the end rather than interrupting
// the original five: this app has no other way to notice a file
// changing underneath it (another process, a network/mounted
// filesystem, ...), so re-reading the current directory from disk on
// demand needs a click target of its own, the same reasoning the other
// six buttons here already follow.
//
// "/" (Root) is the newest addition, per the user's own explicit
// request, placed right after Start rather than at the end the way
// Reload was: Reload is unrelated to the other five (it re-reads the
// current directory, it doesn't go anywhere), so tacking it on last
// avoided disturbing an established, muscle-memorized order; Root, by
// contrast, belongs with Start/Home as a third "jump to a fixed place"
// destination, and reads most naturally sitting right beside Start —
// the button its own "go to the directory breakthrough started in"
// meaning is closest to, conceptually. A second way to reach the exact
// same destination the breadcrumb's own leading "/" (actionNavigate,
// target "/") already provided — see actionRoot's own doc comment on
// runHeaderAction for why a second, dedicated button is worth having
// anyway: that first "/" is plain, unstyled breadcrumb text, no more
// visually a "button" than any other path segment, easy to never
// notice as a click target at all.
var headerButtons = []struct {
	glyph  string
	action headerAction
}{
	{"∎", actionStart},
	{"/", actionRoot},
	{"~", actionHome},
	{"<", actionBack},
	{">", actionForward},
	{"↑", actionUp},
	{"⭯", actionReload},
}

// headerButtonSeparator is the plain, normal-background column between
// each padded button (and once more before the path itself starts) —
// per the user's own explicit request, deliberately left uncolored,
// unlike the buttons themselves either side of it, so it reads as a
// gap between two distinct buttons rather than part of either one.
const headerButtonSeparator = " "

// headerButtonPrefix is the plain-text form of the six nav buttons
// plus their separators — see buildHeaderSpans for the colored,
// clickable version actually drawn in the header. Reused by
// headerEdit's own SetLabel (see NewPanel) so the path being edited
// starts at the exact same column the displayed one already does,
// rather than resetting to column 0 the moment editing starts, per the
// user's own explicit report. Deliberately plain, with no color tags:
// column *width* has to match buildHeaderSpans exactly, but an
// InputField's own label has no use for embedded color tags the way
// the header TextView's dynamic-colored text does. Computed once from
// headerButtons, not hand-typed, so the two can never silently drift
// out of sync with each other on a future edit to either.
var headerButtonPrefix = func() string {
	var b strings.Builder
	for _, btn := range headerButtons {
		b.WriteString(" " + btn.glyph + " " + headerButtonSeparator)
	}
	return b.String()
}()

// buildHeaderSpans renders the header's display text — the six
// headerButtons, each padded into its own three-column "button" (one
// character of theme.ButtonBackground either side of the glyph, the
// same highlightKey convention the bottom button bar already uses for
// its own keys — see buildButtonBar — just without a trailing label,
// since each of these six buttons *is* its own label already), then
// the path, with one clickable span per path component (the leading
// "/" plus each name in between), e.g. clicking "b" in "/a/b/c/d" jumps
// to "/a/b". Column offsets are measured via tview.TaggedStringWidth,
// not a plain rune count — a directory name containing double-width
// (e.g. CJK) characters occupies two terminal columns per character,
// and a rune count would silently drift the spans after it out of
// alignment with what's actually drawn on screen.
//
// Per the user's own explicit request, each button's own click region
// (the headerSpan recorded below) covers its full three-column padded
// area, not just the glyph's own single column — clicking the
// highlighted background either side of a glyph activates it exactly
// the same as clicking the glyph itself, matching what "this whole
// colored area is a button" actually implies.
//
// A click that lands in the header but doesn't hit any of these spans
// (e.g. on a "/" separator, on one of the plain separator columns
// between two buttons, or in empty space after the path) is handled by
// captureHeaderMouse as "switch to edit mode" — deliberately not
// represented as a span here, since it's everything else.
func buildHeaderSpans(abs string, theme config.ResolvedTheme) (text string, spans []headerSpan) {
	var b strings.Builder
	col := 0

	keyBG := colorTag(theme.ButtonBackground)
	for _, btn := range headerButtons {
		start := col
		fmt.Fprintf(&b, "[:%s:] %s [-:-:-]", keyBG, btn.glyph)
		col += 1 + tview.TaggedStringWidth(btn.glyph) + 1
		spans = append(spans, headerSpan{start: start, end: col, action: btn.action})
		b.WriteString(headerButtonSeparator)
		col++
	}

	rootStart := col
	b.WriteString("/")
	col++
	spans = append(spans, headerSpan{start: rootStart, end: col, action: actionNavigate, target: "/"})

	rest := strings.TrimPrefix(abs, "/")
	if rest != "" {
		current := ""
		parts := strings.Split(rest, "/")
		for i, part := range parts {
			current += "/" + part
			width := tview.TaggedStringWidth(part)
			spans = append(spans, headerSpan{start: col, end: col + width, action: actionNavigate, target: current})
			b.WriteString(part)
			col += width
			if i < len(parts)-1 {
				b.WriteByte('/')
				col++
			}
		}
	}

	return b.String(), spans
}

// captureHeaderMouse drives the header's buttons and breadcrumbs, and
// falls back to opening the edit field for any other click within the
// header. It deliberately never lets tview's default TextView mouse
// handling run: that handler grabs focus on MouseLeftDown, which would
// silently break arrow-key navigation in the list below (it'd start
// scrolling the header's text instead). So every mouse event landing
// within the header's bounds is consumed here.
func (p *Panel) captureHeaderMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if !p.header.InRect(event.Position()) {
		return action, event
	}

	if action == tview.MouseLeftClick {
		x, _ := event.Position()
		rectX, _, _, _ := p.header.GetInnerRect()
		col := x - rectX

		if span, ok := p.spanAt(col); ok {
			p.runHeaderAction(span)
		} else if !p.searchMode || col >= p.searchHeaderOffset {
			p.openEdit()
		}
		// The remaining case — searchMode and col < searchHeaderOffset —
		// is a click on setSearchStatus's own status-text prefix, before
		// its "continue here" breadcrumb even starts: never a real path,
		// so there's nothing here to open an editor over.
	}

	return tview.MouseConsumed, nil
}

// spanAt returns the headerSpan covering column col, if any.
func (p *Panel) spanAt(col int) (headerSpan, bool) {
	for _, span := range p.headerSpans {
		if col >= span.start && col < span.end {
			return span, true
		}
	}
	return headerSpan{}, false
}

// runHeaderAction executes a clicked button or breadcrumb.
func (p *Panel) runHeaderAction(span headerSpan) {
	switch span.action {
	case actionStart:
		// history[0] is the first directory this Panel ever successfully
		// loaded — NewPanel's initial navigate() call sets it and nothing
		// afterwards ever removes it, so it's a stable record of where
		// breakthrough started, independent of the OS home directory.
		// Always a real directory, never a search snapshot (see
		// historyEntry.isSearch): nothing can search before ever having
		// navigated anywhere at all.
		p.reportError(p.navigate(p.history[0].path))
	case actionRoot:
		// Same target the breadcrumb's own leading "/" (actionNavigate,
		// target "/") already jumps to — this is a second, dedicated,
		// consistently-styled button for it, at the user's own explicit
		// request, rather than relying on that first "/" alone being
		// discoverable as a click target in the first place.
		p.reportError(p.navigate("/"))
	case actionHome:
		home, err := os.UserHomeDir()
		if err != nil {
			p.reportError(err)
			return
		}
		p.reportError(p.navigate(home))
	case actionBack:
		p.back()
	case actionForward:
		p.forward()
	case actionUp:
		// filepath.Dir("/") == "/" — a no-op navigate back to the same
		// directory at the filesystem root, the same "nothing to do,
		// nothing goes wrong either" territory Start/Home already have
		// at their own edges (already at the start directory, already
		// home). Mirrors the ".." row's own identical parent-or-self
		// check in load, just without needing a visible row for it.
		p.reportError(p.navigate(filepath.Dir(p.path)))
	case actionReload:
		// Straight to load, not navigate: navigate exists to move
		// somewhere and record that move in history (see its own doc
		// comment) — reloading the same directory in place is neither,
		// so it skips navigate's own snapshotCurrentEntry/
		// pushHistoryEntry bookkeeping entirely rather than going
		// through it just to have pushHistoryEntry's own same-path check
		// (see its own doc comment) turn it into a no-op regardless.
		// Clicking this while search results are showing exits back to
		// the plain directory listing rather than re-running the search
		// — load always does that (see its own doc comment), the same
		// behavior the "z" chord's own Reload member and
		// setShowHidden/toggleHidden already have too.
		p.reportError(p.load(p.path))
	case actionNavigate:
		p.reportError(p.navigate(span.target))
	}
}

// openEdit switches the header to its editable text field, pre-filled
// with the current path — searchBrowsePath while search results are
// showing (see effectiveBrowsePath), since p.path itself stays frozen
// at wherever the panel was before the search throughout that mode —
// and moves keyboard focus there. headerEdit's own label (see NewPanel)
// already reserves the "∎~<>↑⭯ " prefix's own width, so the path text
// itself lines up with wherever p.header was just showing it — nothing
// further to do here for that.
func (p *Panel) openEdit() {
	p.editing = true
	p.headerEdit.SetText(p.effectiveBrowsePath())
	p.headerPages.SwitchToPage(headerEditPage)
	p.app.SetFocus(p.headerEdit)
}

// effectiveBrowsePath is whichever path the header's own editable field
// and relative-path resolution (see resolvePath) should act on right
// now: searchBrowsePath while search results are showing, p.path
// otherwise. Two different things while searchMode is true — see
// showSearchResults' own doc comment on why p.path itself never moves
// during that mode — one and the same the rest of the time.
func (p *Panel) effectiveBrowsePath() string {
	if p.searchMode {
		return p.searchBrowsePath
	}
	return p.path
}

// closeEdit switches back to the display header and returns focus to the
// table. Shared by finishEdit's Enter case and cancelEdit.
func (p *Panel) closeEdit() {
	p.editing = false
	p.headerPages.SwitchToPage(headerDisplayPage)
	p.app.SetFocus(p.table)
}

// cancelEdit closes the edit field without navigating anywhere — used
// when a click lands outside it, the same way a browser's address bar
// reverts to the current URL instead of acting on whatever was typed.
// Safe to call unconditionally: it does nothing if no edit is in
// progress.
func (p *Panel) cancelEdit() {
	if !p.editing {
		return
	}
	p.closeEdit()
}

// captureOutsideEdit is called by Root.captureMouse (see the editing
// field's doc comment for why it can't just be Panel's own
// SetMouseCapture) before Root does anything else. If the header is being
// edited and the click landed outside the edit field, it cancels editing
// and reports that it handled the click, so Root doesn't also act on it
// (e.g. by opening the context menu on a stray right-click).
func (p *Panel) captureOutsideEdit(action tview.MouseAction, event *tcell.EventMouse) (consumed bool) {
	if !p.editing {
		return false
	}
	if action != tview.MouseLeftClick && action != tview.MouseRightClick {
		return false
	}
	x, y := event.Position()
	if primitiveContains(p.headerEdit, x, y) {
		return false // click landed on the edit field itself
	}

	p.cancelEdit()
	return true
}

// finishEdit handles the header edit field's special keys. Only Enter
// ends editing by submitting; a click outside the field (see
// captureOutsideEdit) is the only other way out — Escape and Backtab are
// deliberately no-ops here so they can't be hit by accident while typing
// a path. Tab completes the path in place, bash-style.
func (p *Panel) finishEdit(key tcell.Key) {
	switch key {
	case tcell.KeyEnter:
		if typed := p.headerEdit.GetText(); typed != "" {
			p.reportError(p.navigate(p.resolvePath(typed)))
		}
		p.closeEdit()
	case tcell.KeyTab:
		p.completePath()
	}
}

// completePath is the Tab handler: it extends whatever is typed to the
// longest unambiguous completion, exactly like bash does — a single match
// completes fully, several matches complete as far as they agree, and no
// match leaves the text alone.
//
// This deliberately does not use tview's InputField autocomplete
// (SetAutocompleteFunc), even though it exists: that renders a drop-down
// *outside* the field's own rect, which the header's mouse capture would
// have to know about to avoid swallowing clicks meant for it, and its
// callback fires on every keystroke — a full directory read per typed
// character. Completing in place has neither problem and is closer to
// what bash actually does.
func (p *Panel) completePath() {
	matches := p.completions(p.headerEdit.GetText())
	if len(matches) == 0 {
		return
	}
	p.headerEdit.SetText(longestCommonPrefix(matches))
}

// completions returns the possible expansions of currentText: the path
// component after the last "/" matched, case-insensitively, against the
// real contents of the directory before it. Directories come back with a
// trailing "/" so completion chains into them. Results keep the text the
// user typed (including a leading "~" or a relative path) rather than the
// resolved absolute form, so completing doesn't rewrite the field into
// something the user didn't type.
func (p *Panel) completions(currentText string) []string {
	dir, prefix := "", currentText
	if idx := strings.LastIndex(currentText, "/"); idx >= 0 {
		dir, prefix = currentText[:idx+1], currentText[idx+1:]
	}

	entries, err := fsops.ListDir(p.resolvePath(dir))
	if err != nil {
		return nil
	}

	var matches []string
	lowerPrefix := strings.ToLower(prefix)
	for _, e := range entries {
		if !strings.HasPrefix(strings.ToLower(e.Name), lowerPrefix) {
			continue
		}
		name := e.Name
		if e.IsDir {
			name += "/"
		}
		matches = append(matches, dir+name)
	}
	return matches
}

// dirCompletions is completions' own directory-only, case-sensitive
// counterpart — used only by the search dialog's own Start-at field
// (see internal/ui/search.go's captureSearchScopeKey), which can only
// ever be a directory, never a file. Two differences from completions,
// both per the user's own explicit request:
//
//   - Files are excluded outright, not just deprioritized — Entry.IsDir
//     already accounts for a directory symlink too (see its own doc
//     comment), so no separate symlink handling is needed here. A real
//     user report: typing "Down" with a "Downloads/" directory *and* an
//     unrelated "download-thing.sh" file both present used to keep
//     completing only as far as their shared prefix ("Download"),
//     because the file was still in the running as a candidate — for a
//     field that can only ever hold a directory, that file was always a
//     false ambiguity.
//   - Matched case-sensitively, unlike completions' own deliberately
//     case-insensitive matching. Directory-only filtering alone already
//     resolves the specific "Downloads/" vs. "download-thing.sh" case
//     above (the file is simply gone from the candidate list before
//     case ever matters) — this is a second, independent request: two
//     directories differing only in case should no longer both match a
//     lowercase (or differently-cased) prefix here the way completions'
//     own case-insensitive matching still allows for the header.
func (p *Panel) dirCompletions(currentText string) []string {
	dir, prefix := "", currentText
	if idx := strings.LastIndex(currentText, "/"); idx >= 0 {
		dir, prefix = currentText[:idx+1], currentText[idx+1:]
	}

	entries, err := fsops.ListDir(p.resolvePath(dir))
	if err != nil {
		return nil
	}

	var matches []string
	for _, e := range entries {
		if !e.IsDir || !strings.HasPrefix(e.Name, prefix) {
			continue
		}
		matches = append(matches, dir+e.Name+"/")
	}
	return matches
}

// resolvePath turns text typed into the header into an absolute path: a
// leading "~" expands to the user's home directory (the header has a "~"
// button doing the same thing, so users reasonably expect it), and a
// relative path resolves against effectiveBrowsePath — the directory
// the panel is currently showing, or searchBrowsePath while search
// results are showing instead — not the process's working directory,
// which stops matching what the user sees the moment they navigate
// anywhere.
func (p *Panel) resolvePath(input string) string {
	switch {
	case input == "~":
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	case strings.HasPrefix(input, "~/"):
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, input[len("~/"):])
		}
	}

	if filepath.IsAbs(input) {
		return input
	}
	return filepath.Join(p.effectiveBrowsePath(), input)
}

// longestCommonPrefix returns the longest prefix shared by all values,
// compared by rune (so it can never cut a multi-byte character in
// half) and case-insensitively — matching completions' own
// case-insensitive matching (see its own doc comment). Comparing
// case-*sensitively* here was a real bug: completions() can legitimately
// return two entries that only differ in case (e.g. "Downloads/" and
// "download-thing.sh", both matching a typed "Down"), and a
// case-sensitive compare then collapses the shared prefix all the way
// back to before their very first differing-case letter — "Down" itself
// vanishes, completing back to the parent directory instead of forward.
// The output text still uses values[0]'s own actual casing throughout
// (never a mix of the different candidates' casing), so what's typed
// back is always something that's genuinely one of the real matches'
// own spelling, up to the point they diverge.
func longestCommonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}

	prefix := []rune(values[0])
	for _, value := range values[1:] {
		runes := []rune(value)
		if len(runes) < len(prefix) {
			prefix = prefix[:len(runes)]
		}
		for i := range prefix {
			if unicode.ToLower(prefix[i]) != unicode.ToLower(runes[i]) {
				prefix = prefix[:i]
				break
			}
		}
	}
	return string(prefix)
}

// sizeColumnsFor sizes the two data columns for a listing about to be
// built — load's own counterpart to relayoutColumns, which re-measures
// rows already on screen.
//
// Deliberately leaves the name column alone. How wide that can be
// depends on the panel's width, which is only knowable at draw time (an
// undrawn table reports a plausible-looking but meaningless rect), so
// the draw callback owns it: until the first draw the name column reads
// as zero, which nameColumnWidth treats as "don't shorten anything yet",
// and after one it keeps the last real value until the next draw
// corrects it.
//
// Takes fsops.Entry rather than rowRef because load has the entries in
// hand before any row exists to read a ref back off.
func (p *Panel) sizeColumnsFor(entries []fsops.Entry) {
	refs := make([]rowRef, 0, len(entries))
	for _, e := range entries {
		refs = append(refs, rowRef{checkable: true, size: e.Size, modTime: e.ModTime})
	}
	widestSize, widestMod := measureDataColumns(refs, p.sizeBytes, p.mtimeUnix)

	name := p.layout.name
	p.layout = computeColumnLayout(0, widestSize, widestMod, p.inTrashView,
		p.sortKey == sortBySize, p.sortKey == sortByModified)
	p.layout.name = name
}
