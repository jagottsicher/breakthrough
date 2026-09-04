package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/replace"
	"github.com/jagottsicher/breakthrough/internal/viewer"
)

const (
	panelPage        = "panel"
	contextMenuPage  = "context-menu"
	renamePage       = "rename"
	promptPage       = "prompt"
	pickerPage       = "owner-group-picker"
	quitConfirmPage  = "quit-confirm"
	purgeConfirmPage = "purge-confirm"
	sedReplacePage   = "sed-replace"
	sedPreviewPage   = "sed-preview"
)

// overlayFrame is one entry in Root.overlayStack (see showOverlay/
// pushOverlay/hideOverlay): the page name and widget shown, plus an
// optional restore callback run instead of the default
// Application.SetFocus(widget) when this frame becomes the topmost one
// again after whatever was layered on top of it closes. Properties is
// the only current user (see showOverlayWithRestore/restoreProperties)
// — it keeps several sub-widgets simultaneously visible (see
// newPropertiesView), so simply refocusing r.properties itself isn't
// precise enough to land keyboard focus back on the right one.
//
// emptyStackFocus is a second, separate override — see
// pushOverlayReturningFocusTo — for the *other* moment hideOverlay hands
// out focus: this frame's own close, when it was the last one open. That
// case has always defaulted to the panel, correct for every overlay that
// opens *from* the panel (the context menu and everything under it), but
// not for one that opens from somewhere else — see openCompletionPicker,
// currently the only user, which opens from bashLine and should hand
// focus back there, not to the panel, once it's gone.
type overlayFrame struct {
	page            string
	widget          tview.Primitive
	restore         func()
	emptyStackFocus tview.Primitive
}

// Root is breakthrough's top-level UI: the directory panel, plus a
// right-click context menu and the overlays it opens (Properties, Rename,
// the single-line prompt behind Select +/-/chown/chmod), and a Ctrl+Q
// quit confirmation. Pages layers all of these as floating overlays on
// top of the still-visible panel; Root owns the logic for what appears
// where, giving each overlay real keyboard focus while it's shown (see
// showOverlay/hideOverlay), and closing whichever one is open when the
// user clicks outside it (captureOutsideClick) — except Properties while
// it has unsaved edits in progress, which blocks that instead (see
// propertiesDirty).
//
// The context menu is grouped into three parts: Properties/Rename, a
// "Selection" section (Select all/Deselect all/Select +/Select -,
// operating on the checkbox column), and a "Commands" section
// (Copy/Cut/Paste/chown/chmod). See menuSectionLabel for how the section
// dividers are drawn, and docs/whitepaper.md for the dialog-based
// Copy-to/Move-to planned as a possible later addition alongside the
// clipboard-style Copy/Cut/Paste built here.
//
// Properties (see properties.go) is also where Name, Permissions, and
// Modified can be edited in place — Owner/Group are still read-only,
// pending a cross-platform way to list system users/groups (macOS
// doesn't expose them via /etc/passwd the way Linux does).
type Root struct {
	*tview.Pages

	app *tview.Application

	// mouseEnabled mirrors whatever cmd/breakthrough's own initial
	// app.EnableMouse(true) call left the Application in — tview itself
	// has no getter for this (see Application.EnableMouse's own private
	// enableMouse field), so this is Root's own copy of the same state,
	// flipped by ToggleMouseShortcut (F3). Enabling mouse reporting is
	// what lets this app see clicks/drags at all, but it also means the
	// terminal emulator hands every mouse event to breakthrough instead
	// of handling it itself — per a real user report, that breaks a
	// terminal's own native text selection/copy (e.g. to grab a
	// filename) for anyone who doesn't already know their terminal's own
	// override gesture (Shift-drag, on most xterm-derived emulators).
	// F3 is a plain, always-available escape hatch for exactly that,
	// independent of whichever gesture (if any) the current terminal
	// happens to support.
	mouseEnabled bool

	// appVersion/appCommit/appBuildDate/appBuiltBy are the Help
	// overlay's own About section's source (see help.go's aboutText) —
	// cmd/breakthrough's own version/commit/date/builtBy vars, set via
	// ldflags by the release pipeline (see .goreleaser.yaml), passed in
	// through SetVersionInfo once NewRoot returns rather than as
	// parameters here, so every one of this package's own tests
	// constructing a Root directly doesn't need to pass them. Defaulted
	// to exactly the same literals cmd/breakthrough's own vars start
	// with, so a Root nothing ever calls SetVersionInfo on (every test)
	// shows the same "dev build" text a real, plain "go build" binary
	// would too.
	appVersion, appCommit, appBuildDate, appBuiltBy string

	// toolWindows holds every currently open toolWindow (see
	// openToolCommand), in the order they were opened — unlike every
	// other overlay in this codebase, there can be several of these open
	// simultaneously (e.g. a ping and a tail -f side by side), each its
	// own dynamically added/removed Pages entry rather than one fixed
	// page reused across opens. The order itself matters, not just
	// membership: it's the fixed sequence CycleFocusShortcut (see
	// detailssidebar.go) steps through with Tab. toolWindowSeq is the
	// source of each new one's own unique Pages name (see
	// openToolCommand).
	toolWindows   []*toolWindow
	toolWindowSeq int

	// lastScreenWidth/Height are the terminal's own size as of the most
	// recent handleBeforeDraw call — how it tells a genuine resize apart
	// from any other reason a draw happens to run (a keypress, a click,
	// StartClock's own once-a-second tick, ...). Zero-valued until the
	// very first draw, which handleBeforeDraw treats as a "resize" too —
	// harmless, since nothing it repositions is open that early anyway.
	lastScreenWidth, lastScreenHeight int

	// theme is the active color scheme, resolved once at startup (see
	// loadInitialSettings/applyTheme) from settings.ColorScheme against
	// colorSchemes, and again live whenever the Options overlay (see
	// openOptions/applyColorScheme) picks a different one. settings is
	// every other on-disk setting alongside it (currently just the
	// reserved, not-yet-functional Language placeholder — see
	// config.Settings' own doc comment). colorSchemes is every scheme
	// available to pick from, loaded once at startup — see openOptions'
	// own doc comment on why it isn't re-scanned on every open.
	theme        config.ResolvedTheme
	settings     config.Settings
	colorSchemes []config.NamedTheme

	panel *Panel
	// menu is the context menu's own List — the real focus target
	// throughout (see showMenu); menuTitleBar/menuLayout are its "Menu"
	// title bar and the Flex stacking the two, which is what's actually
	// registered on Pages/positioned instead (the same
	// widget/layout split detailsSidebar/detailsSidebarLayout already
	// established — see detailssidebar.go).
	menu         *tview.List
	menuTitleBar *tview.TextView
	menuLayout   *tview.Flex
	rename       *tview.InputField
	prompt       *tview.InputField
	picker       *tview.List // owner/group picker — see openOwnerGroupPicker
	errorView    *tview.TextView
	quitConfirm  *tview.List

	// purgeConfirm backs both "Remove" and "Empty Trash" (see
	// newPurgeConfirm/openPurgeConfirm in trash.go) — one shared,
	// repopulated widget, the same pattern r.picker/r.prompt already use.
	// pendingPurge is the action confirmPurge runs once the user actually
	// confirms, set by whichever of openRemoveConfirm/openEmptyTrashConfirm
	// opened the dialog.
	purgeConfirm *tview.List
	pendingPurge func()

	// sedForm/sedFlagsList/sedActions/sedLayout together make up the
	// "Sed Replace" dialog (see sedreplace.go, especially newSedForm's
	// own doc comment on why the five flag toggles are a separate List
	// rather than Form checkboxes: tview.Form re-applies one uniform
	// field background to every item it owns on every Draw call, which
	// would force a checkbox into the same "editable text field"
	// highlight real fields get). sedLayout stacks the other three and
	// is what sedReplacePage actually shows. All three (plus sedFlags)
	// are rebuilt fresh on every open (see resetSedForm), unlike
	// purgeConfirm's shared, repopulated single widget, since Form has
	// no equivalent of List's SetItemText to reset one in place.
	// sedFindField/sedReplaceField/sedAdvancedField are kept directly
	// (their typed text values are read back in runSedPreview);
	// sedFlags holds the five toggles' current state, keyed by their
	// label constants. sedTargets is the file(s) this open is for (see
	// selectedOrCurrentPaths).
	sedForm          *tview.Form
	sedFindField     *tview.InputField
	sedReplaceField  *tview.InputField
	sedAdvancedField *tview.InputField
	sedFlags         map[string]bool
	sedFlagsList     *tview.List
	sedActions       *tview.List
	sedLayout        *tview.Flex
	sedTargets       []string

	// sedPreviewStatus/sedPreviewTable/sedPreviewActions/sedPreviewLayout
	// show Preview's own dry-run result (runSedPreview): a one-line
	// status (progress while running, a summary once done), the actual
	// Name/Line/Excerpt table (see sedPreviewRows), and Apply/Back/
	// Cancel — the same three-choice shape purgeConfirm already has.
	// sedPendingChanges is what confirmApplySed actually writes if the
	// user goes on to confirm.
	//
	// sedPreviewCancel/sedPreviewAnimFrame/sedPreviewProcessed/
	// sedPreviewTotal/sedPreviewCurrentPos back the live progress
	// animation (see animateSedPreviewProgress/renderSedPreviewStatus) —
	// the same shape searchCancel/searchAnimFrame and friends already
	// have for a live search.
	sedPreviewStatus     *tview.TextView
	sedPreviewTable      *tview.Table
	sedPreviewActions    *tview.List
	sedPreviewLayout     *tview.Flex
	sedPendingChanges    []replace.FileChange
	sedPreviewCancel     context.CancelFunc
	sedPreviewAnimFrame  int
	sedPreviewProcessed  int
	sedPreviewTotal      int
	sedPreviewCurrentPos string

	optionsList *tview.List     // Options overlay — see openOptions
	helpView    *tview.TextView // Help overlay's own scrollable content — see help.go/openHelp
	viewerView  *tview.TextView // Look overlay's built-in pager — see viewer.go/openLook

	// helpTitleBar/helpLayout are Help's own one-row title bar (see
	// newHelpTitleBar) and the Flex stacking it over helpView (the same
	// shape detailsSidebarLayout has over detailsSidebar — see
	// detailssidebar.go) — helpLayout, not helpView directly, is what
	// openHelp actually sizes/positions and pushes as the overlay now.
	helpTitleBar *tview.TextView
	helpLayout   *tview.Flex

	// detailsSidebar is the right-hand Details layer toggled by Ctrl+D
	// — see detailssidebar.go. detailsSidebarVisible tracks whether it's
	// currently shown; unlike every overlay above it, it's deliberately
	// not modal (see newDetailsSidebarView's own doc comment), so it
	// can't reuse activePage/overlayStack the way those do.
	//
	// detailsTitleBar is its own one-row "Details" label above
	// detailsSidebar's own content, the same shape toolWindow's own
	// title bar has (see toolwindow.go) — detailsSidebarLayout is the
	// Flex stacking the two, which is what's actually registered on
	// Pages/positioned (see repositionDetailsSidebar); detailsSidebar
	// itself remains the one real focus target throughout (SetFocus,
	// CycleFocusShortcut, ...), unaffected by wrapping it in a layout
	// purely for drawing purposes.
	detailsSidebar        *tview.TextView
	detailsTitleBar       *tview.TextView
	detailsSidebarLayout  *tview.Flex
	detailsSidebarVisible bool

	// detailsTarget/Stat/StatErr/Image cache what the sidebar is
	// currently showing — mirrors propertiesTarget/propertiesStat above,
	// but reloaded on every selection change (see refreshDetailsSidebar),
	// not just once when opened. detailsTarget is "" whenever nothing
	// meaningfully selected (the ".." row, or an empty listing) — see
	// loadDetailsTarget. detailsImage is non-nil once detailsTarget names
	// either a file viewer.Load actually decoded as an image, or a PDF
	// whose first page could be rasterized (see viewer.LoadPDFPage) —
	// detailsPDFPageCount (>0 only for the latter) is what tells the two
	// apart wherever that matters (see renderDetailsSidebar). It's set
	// independently of detailsImage: PDFPageCount can succeed even when
	// rendering the page image itself fails (no pdftoppm installed).
	detailsTarget          string
	detailsStat            fsops.Info
	detailsStatErr         error
	detailsImage           *viewer.Result
	detailsPDFPageCount    int
	detailsPreviewRowStart int
	detailsPreviewRowEnd   int

	// detailsMetadataState is "" until fetchDetailsMetadata has run for
	// the current detailsTarget (see its own doc comment on why that's
	// still a stub) — reset back to "" by loadDetailsTarget every time
	// the target changes, the same way detailsHashes is.
	// detailsMetaRowStart/End are the metadata section's own click-zone
	// bounds within the rendered text (-1 when there's no image, so
	// nothing to show it for — see renderDetailsSidebar).
	detailsMetadataState string
	detailsMetaRowStart  int
	detailsMetaRowEnd    int

	// detailsHashes/InProgress/AnimFrame/Cancel/BytesRead/RowStart mirror
	// propertiesHashes/hashInProgress/hashAnimFrame/hashCancel/
	// hashBytesRead/hashSectionRow above exactly (see computeDetailsHashes'
	// own doc comment on why this is a second, independent copy rather
	// than shared state) — detailsHashRowStart is -1 whenever the current
	// target is a directory (or resolves to one), which never gets a hash
	// section at all, same as Properties' own isDirish check.
	detailsHashes         *fsops.Hashes
	detailsHashInProgress bool
	detailsHashAnimFrame  int
	detailsHashCancel     context.CancelFunc
	detailsHashBytesRead  atomic.Int64
	detailsHashRowStart   int

	// viewerPDFPath/Page/PageCount/Mode track Look's own PDF page
	// navigation (see viewer.go's showPDFPage/renderPDFPageContent/
	// turnPDFPage/setPDFViewMode) — viewerPDFPath is "" whenever Look
	// isn't currently showing a PDF at all (reset at the top of every
	// showBuiltinLook call, regardless of what Kind it turns out to be
	// — see its own doc comment), which is also what captureViewerKey
	// checks before ever treating PageUp/PageDown/'g'/'t' as PDF
	// actions instead of TextView's own default handling. viewerPDFMode
	// resets to viewer.PDFViewAuto alongside viewerPDFPath — 'g'/'t'
	// only ever override it for the PDF currently open, never persist
	// to the next one.
	viewerPDFPath      string
	viewerPDFPage      int
	viewerPDFPageCount int
	viewerPDFMode      viewer.PDFViewMode

	// The directory picker (see dirpicker.go/openDirPicker) — the
	// "Tree" browse action shared by the search dialog's Start-at field
	// and, later, the planned Copy-to/Move-to target navigation.
	// dirPickerPath is whatever directory is currently being browsed;
	// dirPickerOnSelect/dirPickerOnCancel are set fresh by each
	// openDirPicker call, run by confirmDirPicker/cancelDirPicker.
	dirPicker          *tview.Flex
	dirPickerHeader    *tview.TextView
	dirPickerList      *tview.List
	dirPickerSelectBtn *tview.Button
	dirPickerCancelBtn *tview.Button
	dirPickerPath      string
	dirPickerOnSelect  func(string)
	dirPickerOnCancel  func()

	// The search dialog (see search.go/newSearchDialog) — after MC's
	// own Find File dialog, reusing Properties' own "plain text plus a
	// shared inline editor" editing paradigm (see newPropertiesView).
	// searchPages wraps searchFieldsPages (the fields — see below) as
	// its own single page — results themselves show directly in the
	// panel's own normal file overview area instead of a second page
	// here (see Panel.showSearchResults), per the user's own request.
	//
	// searchFieldsPages itself wraps the fields Flex (searchTop/
	// searchLeft/searchRight — MC's own Start-at/Ignore-dirs block
	// above a two-column Filename/Content section, plus searchButtons)
	// and searchEditField, the one shared inline editor repositioned
	// over whichever field is currently being edited (see
	// activateSearchTextField) — the same "one shared field,
	// repositioned per use" approach propertiesEditField/Root.rename/
	// Root.prompt all already use. searchEditCommit is that field's own
	// pending commit callback, set fresh each time (see
	// activateSearchTextField/finishSearchEdit).
	//
	// searchSpans is every clickable/keyboard-focusable region across
	// all three of searchTop/searchLeft/searchRight, rebuilt on every
	// render (see rerenderSearchDialog) — the same running list
	// propertySpans is for Properties, just spanning three TextViews
	// instead of one (see searchSpan's own doc comment). searchFocusedIdx
	// is which one currently has keyboard focus, or
	// len(searchSpans)/len(searchSpans)+1 for Cancel/Search.
	//
	// searchEngineOptions records which search.Engine each of Engine's
	// own choice options actually maps to (built once, since
	// LocateAvailable doesn't change mid-session) — the group's own
	// selected index alone isn't enough once "locate" is conditionally
	// left out (see its own doc comment in search.go). searchEngineIdx
	// is that selected index; searchScopeValue/searchFilenameValue/
	// searchIgnoreValue/searchContentValue are the dialog's own four
	// text fields. There's no equivalent choice group for Content's own
	// search tool any more (previously "Search in": File names/Content
	// (grep)/gzip/zip) — removed for now per the user's own request;
	// runSearch decides content vs. filename search, always plain grep,
	// purely from whether searchContentValue is filled in.
	//
	// The rest are MC's own Find File checkboxes (verified against its
	// real find.c source, not guessed — see rerenderSearchDialog's own
	// doc comment), replacing this dialog's earlier, shared Glob/
	// Keyword/Regex choice group: searchShellPatterns (Filename's own
	// "Using shell patterns") and searchContentRegex (Content's own
	// "Regular expression") are independent of each other, per MC's own
	// design — Filename's pattern syntax and Content's pattern syntax
	// are never the same choice. searchRecursive/searchFollowSymlinks
	// only matter for EngineFind (locate's own index has no live
	// traversal to shape this way); searchCaseSensitive is shown in
	// both columns but is one shared value (this app never runs a
	// filename and a content search at once, so nothing is lost keeping
	// it single); searchSkipHidden/searchWholeWords/searchFirstHit are
	// each their own — see runSearch for how every one of these feeds
	// into the search.Request that's actually built.
	//
	// searchIncludeArchives is Filename's own "Include zip, tar (gz,
	// bz2, xz)" checkbox — search.Request.IncludeArchives, a plain
	// filename search only (meaningless once Content is filled in, the
	// same as searchRecursive/searchFollowSymlinks are meaningless for
	// EngineLocate — see its own doc comment).
	//
	// searchIncludeCompressed is Content's own mirror of that —
	// "Include compressed files" — search.Request.IncludeCompressed, a
	// plain content search only (Content filled in, meaningless
	// otherwise — the exact opposite scoping of searchIncludeArchives).
	searchPages             *tview.Pages
	searchFieldsPages       *tview.Pages
	searchTop               *tview.TextView
	searchLeft              *tview.TextView
	searchRight             *tview.TextView
	searchEditField         *tview.InputField
	searchEditCommit        func(string)
	searchButtons           *tview.Flex
	searchCancelBtn         *tview.Button
	searchSearchBtn         *tview.Button
	searchSpans             []searchSpan
	searchFocusedIdx        int
	searchEngineOptions     []searchEngineOption
	searchEngineIdx         int
	searchScopeValue        string
	searchFilenameValue     string
	searchIgnoreValue       string
	searchContentValue      string
	searchIgnoreEnabled     bool
	searchCaseSensitive     bool
	searchSkipHidden        bool
	searchRecursive         bool
	searchFollowSymlinks    bool
	searchShellPatterns     bool
	searchContentRegex      bool
	searchWholeWords        bool
	searchFirstHit          bool
	searchIncludeArchives   bool
	searchIncludeCompressed bool
	// searchCancel stops whatever search.Run call is currently in
	// flight, if any, and its paired animateSearchProgress ticker (both
	// share this same ctx) — called before starting a new one, and when
	// the dialog closes, so a slow "find /" left running never keeps
	// working after the user has moved on (see runSearch/closeSearch).
	searchCancel context.CancelFunc
	// searchAnimFrame/searchCurrentPos/searchLastDir/searchStartDir
	// back the results window's own status line (see
	// renderSearchStatus): searchAnimFrame is the current "still
	// working" animation frame (see animateSearchProgress).
	// searchCurrentPos is the real, live "where are we right now" —
	// set from search.Request.OnProgress (see runSearch), which for
	// EngineFind actually is breakthrough's own code watching the
	// traversal happen (a second, lightweight find -type d — see
	// internal/search's own startDirectoryProgress), not a guess.
	// searchLastDir, the directory of the most recently streamed
	// match, is the fallback once searchCurrentPos has nothing to show
	// — EngineLocate (OnProgress is never called for it — see its own
	// doc comment: locate has no live traversal to report on at all)
	// or once the search has already finished and progress has
	// stopped arriving. searchStartDir (Start at, as of when the
	// search began) is the last resort, shown until either one has
	// anything at all.
	searchAnimFrame  int
	searchCurrentPos string
	searchLastDir    string
	searchStartDir   string

	// The Chmod dialog (see chmoddialog.go/newChmodDialog) — the same
	// "plain text plus a shared inline editor" paradigm Properties/Search
	// already use, scaled down to this dialog's own much smaller field
	// set: a Permissions value always shown, plus (only once chmodAnyDir
	// — see below) a "recursive" toggle for it and a whole second Files
	// section (its own value and its own "recursive" toggle), per the
	// user's own explicit request that folders and files be
	// independently, optionally recursive rather than one shared setting
	// for both.
	//
	// chmodTargets is every path the dialog will apply to on Apply (see
	// openChmod/selectedOrCurrentPaths), captured once when it opens.
	// chmodAnyDir reports whether any of them is a directory (or
	// resolves to one — see isDirish), computed alongside chmodTargets:
	// a selection made up entirely of plain files has nothing for either
	// recursive toggle to apply to, so neither the toggle nor the Files
	// section appears at all in that case (see chmodFieldOrder/
	// renderChmodDialog).
	//
	// stagedChmodMode/stagedChmodRecursiveDirs/stagedChmodFilesEnabled/
	// stagedChmodFilesMode are the dialog's own in-progress state — see
	// applyChmodDialog for how each actually gets used, and openChmod for
	// how they're seeded when the dialog opens.
	chmodTargets             []string
	chmodAnyDir              bool
	stagedChmodMode          os.FileMode
	stagedChmodRecursiveDirs bool
	stagedChmodFilesEnabled  bool
	stagedChmodFilesMode     os.FileMode

	chmodPages      *tview.Pages
	chmodText       *tview.TextView
	chmodEditField  *tview.InputField
	chmodEditTarget chmodField
	chmodButtons    *tview.Flex
	chmodCancelBtn  *tview.Button
	chmodApplyBtn   *tview.Button
	chmodSpans      []chmodSpan
	chmodFocusedIdx int

	// mainLayout wraps panel, bashConsole, buttonBar, and statusBar into
	// the vertical stack registered as panelPage (see newBottomBar/
	// NewRoot) — panel still owns its own rect the same way it always
	// has (clampToPanel and everything else reading
	// panel.GetInnerRect() is unaffected), just resized to leave the
	// bottom rows free.
	mainLayout *tview.Flex

	// bashConsole is the topmost of the three bottom rows: bashLine, a
	// multi-line shell command/script editor, plus bashHint, a
	// single-row, non-editable legend spelling out its own keybindings
	// (see bashconsole.go) — both wrapped in their own nested Flex so
	// the pair can grow together (see expandBashConsole) without
	// disturbing mainLayout's own four-row split (panel plus these
	// three). Collapsed to a single row (just bashLine, bashHint
	// hidden) until bashLine gains focus. Every command runs
	// full-screen, via a real terminal (see runShellCommandFullScreen)
	// — see newBashConsole's own doc comment on why this doesn't try to
	// distinguish which commands "need" that. Pasting into bashLine
	// works because cmd/breakthrough enables tview's bracketed-paste
	// support (Application.EnablePaste), not anything Root itself does.
	//
	// buttonBar, the middle row, is the quick-action buttons (Help,
	// Rename, Edit, Look, Properties, Find, Sed, toggle hidden files,
	// Options, Trash, Trashbin/Restore, Remove — see buildButtonBar),
	// with buttonBarSpans locating each one the same way propertySpans
	// do for Properties. Unlike when this was first written, it's no
	// longer fixed for the run of the program: refreshButtonBar rebuilds
	// it on the same onLoad wiring statusBar uses below, since which
	// buttons even appear (Trashbin vs. Restore, Trash's own
	// disappearance) and one label's own text (Hide vs. Unhide) both
	// depend on live state now — see buildButtonBar's own doc comment.
	//
	// statusBar, the last row, is purely informational, deliberately
	// with nothing clickable in it any more (see buildStatusBar): the
	// current user, disk/inode usage, the running kernel, uptime/load
	// average where available, and the clock — refreshed on navigation
	// and once a second by the clock's own ticker (see
	// refreshStatusBar), unlike buttonBar above.
	bashConsole    *tview.Flex
	bashLine       *tview.TextArea
	bashHint       *tview.TextView
	buttonBar      *tview.TextView
	buttonBarSpans []buttonBarSpan
	statusBar      *tview.TextView

	// bashLineCompletingPick is true only for the moment openCompletionPicker
	// moves focus away from bashLine to the completion picker it opens —
	// a deliberate, momentary transition, not the user leaving the
	// console, so collapseBashConsole (bashLine's own BlurFunc, which
	// that focus change would otherwise trigger) checks this and skips
	// collapsing while it's set. Never true otherwise: blurring away for
	// any real reason (Escape, clicking the panel, running a command)
	// still collapses normally.
	bashLineCompletingPick bool

	// bashHistory is every command available for the bash line's
	// Ctrl+P/Ctrl+N navigation (see bashHistoryUp/Down — not Up/Down
	// themselves, which move the cursor within bashLine's own
	// multi-line text instead) — seeded at construction from
	// bashHistoryFile's existing content (see historyFilePath/
	// loadBashHistory: real, cross-session history, the same as a real
	// shell's own), then appended to (both here and back to
	// bashHistoryFile — see appendBashHistory) as runBashCommand runs
	// each one. bashHistoryIdx is which entry is currently showing:
	// len(bashHistory) means "not currently browsing history" — a fresh
	// or in-progress line, not one recalled from it — in which case
	// bashHistoryDraft is what that in-progress line was, restored if
	// Ctrl+N is pressed back past the newest entry.
	bashHistory      []string
	bashHistoryIdx   int
	bashHistoryDraft string
	bashHistoryFile  string

	// currentUser is resolved once (see currentUsername) — it can't
	// meaningfully change over a session, unlike the current directory
	// (df) or the clock, which is why only those two need refreshing
	// (see refreshStatusBar).
	currentUser string

	// properties is the Properties overlay's own nested Pages (see
	// newPropertiesView) — propertiesText is the always-visible read-only
	// display; propertiesEditField is shown/positioned on top of it only
	// while a field is being text-edited; propertiesButtons (and its two
	// buttons, propertiesCancelBtn/propertiesSaveBtn — kept individually
	// addressable so setPropertiesFocus can give either one real keyboard
	// focus) are visible the whole time Properties is open.
	properties           *tview.Pages
	propertiesTitleBar   *tview.TextView
	propertiesText       *tview.TextView
	propertiesEditField  *tview.InputField
	propertiesEditTarget propertyField
	propertiesButtons    *tview.Flex
	propertiesCancelBtn  *tview.Button
	propertiesSaveBtn    *tview.Button

	// propertiesFocusIndex is Properties' own keyboard-navigation cursor
	// (see setPropertiesFocus/movePropertiesFocus/capturePropertiesKey):
	// -1 (nothing focused, Properties' state right after opening) or an
	// index into propertyFieldOrder for a field span, or
	// len(propertyFieldOrder)/len(propertyFieldOrder)+1 for the Cancel/
	// Save buttons. Clicking a field (see activatePropertyField) sets it
	// too, so keyboard navigation continues naturally from wherever the
	// mouse last landed.
	propertiesFocusIndex int

	// promptSubmit is what the currently-open prompt overlay (see
	// openPrompt/finishPrompt) runs with the typed text if the user
	// confirms with Enter. Only meaningful while promptPage is active.
	promptSubmit func(text string)

	// clipboard holds the absolute paths Copy or Cut last captured (see
	// clipboardTargets) — the checkbox selection at the time, or just the
	// right-clicked target if nothing was checked. clipboardCut records
	// which of the two: Paste copies when false, moves (via fsops.Move)
	// when true.
	clipboard    []string
	clipboardCut bool

	// hiddenToggleIdx/sizeFormatToggleIdx/mtimeFormatToggleIdx are the
	// "Globals" section's three toggle items' indices in r.menu, set once
	// in NewRoot — needed so toggleHidden/toggleSizeBytes/toggleMtimeUnix
	// and showMenu can relabel their own item in place (see
	// hiddenToggleLabel/sizeFormatToggleLabel/mtimeFormatToggleLabel) to
	// describe what clicking it will do next, rather than a static label
	// that stops matching reality after the first click.
	hiddenToggleIdx      int
	sizeFormatToggleIdx  int
	mtimeFormatToggleIdx int

	// target is the absolute path the context menu / rename prompt is
	// currently acting on. targetRow is its table row *index* (0 = "..",
	// 1 = the first entry, ...; see Panel.rowIndexAt) — not a screen
	// coordinate, which is what a since-fixed bug here used to store
	// (see captureMouse's MouseRightClick case): openRename passes it
	// straight to Panel.nameCellRect, which indexes Table.GetCell with
	// it, so a screen y only happened to line up by coincidence, and
	// silently drifted out of sync with the table's own scroll offset as
	// soon as one existed. Only meaningful while one of the overlays
	// below is visible.
	target    string
	targetRow int

	// propertiesTarget/propertiesStat cache what the Properties overlay is
	// currently showing, so computeHashes (triggered separately, after
	// Properties is already open — see capturePropertiesKey/
	// capturePropertiesMouse) knows what to hash and can re-render the
	// same text with the results appended, without re-running fsops.Stat.
	// propertiesHashes holds the computed digests once that's happened,
	// nil until then — renderProperties reads it directly rather than
	// taking it as a parameter, since it's re-run after every kind of
	// edit, not just this one. hashSectionRow is the 0-based row, within
	// that text, where the hash hint/result line starts — set by
	// renderProperties, read by capturePropertiesMouse to tell whether a
	// click landed on it.
	propertiesTarget string
	propertiesStat   fsops.Info
	propertiesHashes *fsops.Hashes
	hashSectionRow   int

	// hashInProgress/hashAnimFrame/hashCancel back computeHashes' own
	// "in progress" animation (see hashAnimationFrames): hashInProgress
	// is what renderProperties checks to show the current animation
	// frame instead of the plain hint or the finished results;
	// hashAnimFrame is which frame that is, advanced by a ticker on its
	// own background goroutine; hashCancel stops that ticker and — via
	// the same ctx — an in-flight fsops.Hash call itself, promptly, not
	// just its eventual reporting (see fsops.Hash's own doc comment on
	// why that distinction is what actually stops it from still reading
	// and reporting progress into hashBytesRead well after being
	// "cancelled" — a real bug this fixed) — once a newer hash
	// computation, or reopening Properties for a different target (see
	// openProperties), has superseded it.
	hashInProgress bool
	hashAnimFrame  int
	hashCancel     context.CancelFunc

	// hashBytesRead is how many bytes hashFile's in-flight call has
	// streamed so far (see computeHashes' own onProgress callback),
	// read by renderProperties to show a percentage alongside the
	// animation whenever propertiesStat.Size is known. Unlike
	// hashInProgress/hashAnimFrame/hashCancel above — which are only
	// ever touched from within the tview event loop, either directly or
	// via QueueUpdateDraw — this one is also written from hashFile's
	// own background goroutine on every Read, so it has to stay an
	// atomic rather than a plain int64.
	hashBytesRead atomic.Int64

	// propertySpans locates each editable region in the Properties
	// overlay's current text (see propertiesBuilder), rebuilt on every
	// renderProperties call.
	propertySpans []propertySpan

	// propertiesDirty/stagedName/stagedMode/stagedMtime/stagedOwner/
	// stagedGroup hold the Properties overlay's in-progress edit, if any
	// — see markPropertiesDirty and savePropertiesEdit. The staged values
	// start out equal to propertiesStat's own (set in openProperties) and
	// only diverge as fields are edited; nothing here is written to the
	// real file until Save. stagedOwner/stagedGroup are plain name
	// strings either way — chosen via the owner/group picker or typed
	// into its text fallback — resolved to a uid/gid via
	// fsops.ResolveUID/ResolveGID only at Save time, the same as
	// propertiesStat.Owner/Group are themselves already just names.
	//
	// stagedRecursiveOwner/stagedRecursiveGroup are the directory-only
	// "apply recursively" toggles (see recursiveApplyField/
	// toggleRecursiveApply) that sit right after Owner and right after
	// Group respectively — two independent toggles rather than one
	// combined switch for both, per the user's own explicit request.
	// Always reset to false in openProperties, never carried over from a
	// previous Properties session, since each describes what this
	// particular Save should do, not a standing preference.
	propertiesDirty      bool
	stagedName           string
	stagedMode           os.FileMode
	stagedMtime          time.Time
	stagedOwner          string
	stagedGroup          string
	stagedRecursiveOwner bool
	stagedRecursiveGroup bool

	// activePage/activeWidget mirror overlayStack's top frame — see
	// showOverlay/pushOverlay/hideOverlay. This drives both explicit focus
	// handling and captureOutsideClick's "click outside the topmost
	// overlay closes it" behavior.
	activePage   string
	activeWidget tview.Primitive

	// overlayStack holds every currently-open overlay, most-recently-opened
	// last. showOverlay closes everything already open first, then opens
	// exactly one — the original one-overlay-at-a-time behavior every
	// caller except the owner/group picker wants. pushOverlay instead adds
	// a new layer on top of whatever's already open, leaving it visible
	// underneath — see openOwnerGroupPicker, whose picker floats on top of
	// Properties rather than replacing it, per the user's own request.
	// hideOverlay always closes just the topmost layer, revealing whatever
	// was underneath, if anything.
	overlayStack []overlayFrame

	// dragStartRow/dragCurrentRow/dragMoved/dragging track a right-button
	// drag in progress, live — see captureMouse's MouseRightDown/MouseMove/
	// MouseRightUp cases and advanceDrag.
	//
	// dragStartRow never changes once a drag begins. dragCurrentRow is
	// where the toggled range currently ends, updated on every MouseMove
	// so advanceDrag only has to toggle the rows that changed membership
	// since the last update (see Panel.applyDragDelta), not re-toggle the
	// whole range each time. dragMoved stays false until the mouse first
	// leaves the press row; nothing gets toggled before that, so a plain
	// right-click (no movement at all) still opens the context menu as
	// usual, matching what tview itself does: it only synthesizes
	// MouseRightClick when the release position matches the press
	// position, so a real drag never produces one.
	dragStartRow   int
	dragCurrentRow int
	dragMoved      bool
	dragging       bool
}

// NewRoot creates the top-level UI rooted at path. app is passed down to
// the Panel, which needs it to move keyboard focus into its header's edit
// field — see Panel.openEdit.
func NewRoot(app *tview.Application, path string) (*Root, error) {
	settings, colorSchemes, configWarnings := loadInitialSettings()
	theme := config.FindColorScheme(colorSchemes, settings.ColorScheme).Resolve()

	panel, err := NewPanel(app, path, theme, settings)
	if err != nil {
		return nil, err
	}

	r := &Root{
		Pages:        tview.NewPages(),
		app:          app,
		mouseEnabled: true, // matches cmd/breakthrough's own initial app.EnableMouse(true)
		panel:        panel,
		settings:     settings,
		colorSchemes: colorSchemes,
		theme:        theme,
		// Matches cmd/breakthrough's own version/commit/date/builtBy
		// vars' own default literals exactly — see SetVersionInfo's own
		// doc comment and the struct field comment above.
		appVersion:   "dev",
		appCommit:    "none",
		appBuildDate: "unknown",
		appBuiltBy:   "source",
		// -1: "nothing focused yet" — see focusedPropertyField/
		// setPropertiesFocus. Set here rather than only in openProperties
		// so it's already correct for anything that renders Properties
		// without going through openProperties (e.g. seedProperties in
		// the test suite).
		propertiesFocusIndex: -1,
	}

	// No borders on the floating elements below — a background color set
	// apart from the plain panel does the same job without the
	// box-drawing look (colors themselves are applied once, uniformly,
	// by applyTheme near the end of this function, not per widget here).
	r.menu = tview.NewList().ShowSecondaryText(false)
	r.menu.SetHighlightFullLine(true)
	r.menu.SetBorderPadding(0, 0, 1, 1) // 1-char left/right padding; no border needed for this
	// Look first and default-selected (see showMenu's own
	// SetCurrentItem(0)), per the user's own explicit request — the
	// same read-only, no-side-effects action Enter on a plain file now
	// tries too (see Panel.activateRow), so it's also this menu's own
	// most-likely-wanted default.
	r.menu.AddItem("Look", "", 0, r.lookCurrentEntry)
	r.menu.AddItem("Rename", "", 0, r.openRename)
	r.menu.AddItem("Edit", "", 0, r.editCurrentEntry)
	r.menu.AddItem("tail -f", "", 0, r.tailCurrentEntry) // lowercase, per the user's own explicit request — it's a command name, not a title
	r.menu.AddItem("Properties", "", 0, r.openProperties)
	r.menu.AddItem(menuSectionLabel("Selection"), "", 0, nil)
	r.menu.AddItem("Select all", "", 0, r.panel.selectAll)
	r.menu.AddItem("Deselect all", "", 0, r.panel.deselectAll)
	r.menu.AddItem("Select +", "", 0, r.openSelectPlus)
	r.menu.AddItem("Select -", "", 0, r.openSelectMinus)
	r.menu.AddItem(menuSectionLabel("Commands"), "", 0, nil)
	r.menu.AddItem("Copy", "", 0, r.copyToClipboard)
	r.menu.AddItem("Cut", "", 0, r.cutToClipboard)
	r.menu.AddItem("Paste", "", 0, r.pasteClipboard)
	r.menu.AddItem("chown", "", 0, r.openChown)
	r.menu.AddItem("chmod", "", 0, r.openChmod)
	r.menu.AddItem("sed", "", 0, r.openSedReplace)
	r.menu.AddItem(menuSectionLabel("Delete"), "", 0, nil)
	r.menu.AddItem("Move to Trash", "", 0, r.moveSelectionToTrash)
	r.menu.AddItem("Remove", "", 0, r.openRemoveConfirm)
	r.menu.AddItem("Go to Trash", "", 0, r.openTrash)
	r.menu.AddItem("Restore from Trash", "", 0, r.restoreSelectionFromTrash)
	r.menu.AddItem("Empty Trash", "", 0, r.openEmptyTrashConfirm)
	r.menu.AddItem(menuSectionLabel("Tools"), "", 0, nil)
	// Ping is this first toolWindow slice's own proof of concept (see
	// toolwindow.go) — a placeholder entry point, not itself the planned
	// feature: the real Toolbox (networking/hardware tool departments,
	// see feature_ideas.txt) replaces this one entry with a whole
	// submenu once it exists.
	r.menu.AddItem("Ping (test)", "", 0, r.openPingTestWindow)
	r.menu.AddItem(menuSectionLabel("Globals"), "", 0, nil)
	// hiddenToggleIdx/sizeFormatToggleIdx/mtimeFormatToggleIdx are
	// computed rather than hardcoded literals, so they keep pointing at
	// the right row if another item is ever added above them — see
	// toggleHidden/toggleSizeBytes/toggleMtimeUnix and showMenu, which
	// all need them to relabel their own item in place.
	r.hiddenToggleIdx = r.menu.GetItemCount()
	r.menu.AddItem(hiddenToggleLabel(r.panel.showHidden), "", 0, r.toggleHidden)
	r.sizeFormatToggleIdx = r.menu.GetItemCount()
	r.menu.AddItem(sizeFormatToggleLabel(r.panel.sizeBytes), "", 0, r.toggleSizeBytes)
	r.mtimeFormatToggleIdx = r.menu.GetItemCount()
	r.menu.AddItem(mtimeFormatToggleLabel(r.panel.mtimeUnix), "", 0, r.toggleMtimeUnix)
	r.menu.SetDoneFunc(r.closeMenu) // Escape

	// A one-row "Menu" title bar above it, the same shape every other
	// panel's now has (toolWindow, Details, Properties — see
	// toolwindow.go/detailssidebar.go/newPropertiesView), per the user's
	// own explicit request. menuLayout, not r.menu itself, is what's
	// actually registered on Pages/positioned (see showMenu) — r.menu
	// remains the real focus target throughout (showMenu still passes
	// it, not menuLayout, to showOverlay), unaffected by wrapping it in
	// a layout purely for drawing, the same split Details' own
	// detailsSidebar/detailsSidebarLayout already established.
	r.menuTitleBar = tview.NewTextView()
	r.menuTitleBar.SetWrap(false)
	r.menuTitleBar.SetText(" Menu ")
	r.menuLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.menuTitleBar, 1, 0, false).
		AddItem(r.menu, 0, 1, true)

	// No label: this is positioned exactly over the target's row in
	// openRename, so it reads as "the row itself became editable" rather
	// than a separate prompt.
	r.rename = tview.NewInputField()
	r.rename.SetDoneFunc(r.finishRename) // Enter or Escape

	// Backs Select +/-/chown/chmod: a single labelled field, centered on
	// screen (unlike rename, it's not tied to any one row) — see
	// openPrompt.
	r.prompt = tview.NewInputField()
	r.prompt.SetDoneFunc(r.finishPrompt) // Enter or Escape

	r.properties = r.newPropertiesView()
	r.errorView = r.newErrorView()

	// Panel reports its own failures (unreadable directory, bad path
	// typed into the header) through Root's error overlay.
	panel.onError = r.showError

	// The bottom bars (see newBottomBar): bashLine/buttonBar/statusBar
	// must exist before onLoad can be wired below, and before
	// mainLayout is built.
	r.newBottomBar()

	// Panel's disk-usage display depends on whichever directory it's
	// currently showing — refreshed on every navigation from here on
	// (onLoad isn't called for the very first load, which already
	// happened inside NewPanel above, before there was anything to wire
	// it to — refreshStatusBar is called once explicitly, right after
	// AddPage below, to cover that one case). refreshButtonBar rides
	// along on the same wiring: buildButtonBar's own Trashbin/Restore
	// swap and Trash's disappearance both depend on the panel's current
	// directory too (see inTrash), so every navigation that can move in
	// or out of the trash — including toggleHidden's own reload, which
	// this also keeps in sync for the Hide/Unhide label — needs to
	// re-render it exactly when it re-renders statusBar.
	panel.onLoad = func(string) {
		r.refreshStatusBar()
		r.refreshButtonBar()
	}

	r.quitConfirm = tview.NewList().ShowSecondaryText(false)
	r.quitConfirm.SetHighlightFullLine(true)
	r.quitConfirm.SetBorderPadding(0, 0, 1, 1)
	r.quitConfirm.AddItem("Quit breakthrough", "", 0, r.confirmQuit)
	r.quitConfirm.AddItem("Cancel", "", 0, r.cancelQuit)
	r.quitConfirm.SetDoneFunc(r.cancelQuit) // Escape

	// The Remove/Empty-Trash confirmation (see trash.go) — same "one
	// shared List" shape as quitConfirm above, deliberately different
	// default focus (see newPurgeConfirm's own comment).
	r.purgeConfirm = r.newPurgeConfirm()

	// The "Sed Replace" dialog and its own Preview screen (see
	// sedreplace.go) — sedForm/sedFlagsList/sedActions are rebuilt fresh
	// on every open (see resetSedForm), but sedLayout (which stacks all
	// three) and sedPreviewLayout (and the view/actions it wraps) are
	// built once here, the same as everything else on this page.
	r.sedForm = r.newSedForm()
	r.sedFlagsList = r.newSedFlagsList()
	r.sedActions = r.newSedActions()
	r.sedLayout = r.newSedLayout()
	r.sedPreviewLayout = r.newSedPreviewLayout()

	// The owner/group picker (see openOwnerGroupPicker) — one shared List,
	// repopulated and repositioned per open, the same pattern rename/
	// prompt/propertiesEditField already use.
	r.picker = tview.NewList().ShowSecondaryText(false)
	r.picker.SetHighlightFullLine(true)
	r.picker.SetBorderPadding(0, 0, 1, 1)

	// The Options overlay (see openOptions) — same "one shared,
	// repopulated List" pattern as r.picker above.
	r.optionsList = r.newOptionsList()

	// The search dialog (see openSearch).
	r.searchPages = r.newSearchDialog()

	// The Chmod dialog (see openChmod/chmoddialog.go).
	r.chmodPages = r.newChmodDialog()

	// The directory picker (see openDirPicker) — built once and reset
	// on every open, the same as everything else above.
	r.dirPicker = r.newDirPicker()

	// The Help overlay (see help.go/openHelp) — a single, static,
	// read-only TextView, the simplest of all of these (nothing to
	// reset or repopulate on open) — now topped with its own one-row
	// title bar (helpTitleBar/helpLayout), per the user's own explicit
	// request that Help get the same title bar (and close button) every
	// other overlay in this app already has.
	r.helpView = r.newHelpView()
	r.helpTitleBar = r.newHelpTitleBar()
	r.helpLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.helpTitleBar, 1, 0, false).
		AddItem(r.helpView, 0, 1, true)

	// The Look overlay's built-in pager (see viewer.go/openLook) — same
	// single-static-TextView shape as Help, just with its own text set
	// fresh on every open instead of once here.
	r.viewerView = r.newViewerView()

	// The Details sidebar (see detailssidebar.go): its own content is a
	// single static TextView, same shape as Help/the Look pager above,
	// topped with its own "Details" title bar (see newDetailsTitleBar) —
	// detailsSidebarLayout, the Flex stacking the two, is what's actually
	// positioned and shown/hidden its own way (see showDetailsSidebar).
	r.detailsSidebar = r.newDetailsSidebarView()
	r.detailsTitleBar = r.newDetailsTitleBar()
	r.detailsSidebarLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.detailsTitleBar, 1, 0, false).
		AddItem(r.detailsSidebar, 0, 1, true)

	// "Esc: back to search" while search results are showing (see
	// Panel.onSearchEscape's own doc comment) — a right-click on a
	// search-result row already reaches r.menu the exact same way a
	// real row's does (see captureMouse's MouseRightClick case, never
	// mode-specific to begin with), so there's no separate context menu
	// to build here any more.
	panel.onSearchEscape = r.backToSearchForm

	// Live-updates the Details sidebar as the cursor moves — tview's own
	// native hook for "the table's current row changed, for any reason"
	// (arrow keys, a click, a directory reload repositioning the cursor,
	// ...), not something this codebase already had a use for before
	// this sidebar needed one. refreshDetailsSidebar itself is a cheap
	// no-op whenever the sidebar isn't actually visible, so this costs
	// nothing extra during plain browsing the rest of the time.
	panel.table.SetSelectionChangedFunc(func(int, int) { r.refreshDetailsSidebar() })

	// A content-search match opens in the configured editor, at its
	// own matched line, instead of just jumping to it (see
	// Panel.onOpenSearchResult's own doc comment).
	panel.onOpenSearchResult = r.runEditor

	// Jumping to a filename/archive-member match's own real location
	// stops the search itself, not just the panel's own display of it
	// (see Panel.onExitSearchResults' own doc comment) — cancelSearch is
	// exactly what Escape/closing the dialog already does for the same
	// reason (see closeSearch), just reached from a result click now.
	panel.onExitSearchResults = r.cancelSearch

	// The click-pause-click gesture on an already-selected row's own
	// name renames it (see Panel.onRenameGesture/handleNameClick's own
	// doc comment) — per the user's own explicit request, the same for
	// files and directories alike.
	panel.onRenameGesture = r.renameRow

	// Enter/double-click on a plain file tries Look now, the same way
	// they already navigate into a directory (see Panel.onOpenFile's
	// own doc comment) — per the user's own explicit request.
	panel.onOpenFile = r.openLook

	// The header row's own "<" button expands the Details sidebar (see
	// Panel.onExpandDetails/detailsExpandBtn's own doc comments) —
	// showDetailsSidebar directly, not the toggle: this button only
	// ever means "expand", the same one-directional meaning the
	// sidebar's own ">" collapse button (see newDetailsTitleBar) has in
	// the other direction, per the user's own explicit request for two
	// separate, fixed-direction mouse controls rather than one shared
	// toggle.
	panel.onExpandDetails = r.showDetailsSidebar

	// Browsing the trash itself shows each item's own original path and
	// deletion time instead of its real on-disk name/mtime (see
	// Panel.onDescribeRows/Root.describeTrashRows' own doc comments).
	panel.onDescribeRows = r.describeTrashRows

	// mainLayout stacks the panel above the three bottom rows — panel
	// gets the lion's share (0, 1: no fixed size, proportion 1, i.e. all
	// remaining space) and real focus by default (see NewFlex/AddItem's
	// own "focus" parameter); bashConsole/buttonBar/statusBar are each
	// pinned to exactly one row (1, 0) and never auto-focused — reaching
	// bashLine is a deliberate click, not something Tab should stumble
	// into. bashConsole's own row grows past that single row once
	// bashLine is actually focused (see expandBashConsole/
	// collapseBashConsole) — ResizeItem there targets it by widget
	// identity, not position, so it's unaffected by buttonBar sitting
	// between it and statusBar.
	r.mainLayout = tview.NewFlex().SetDirection(tview.FlexRow)
	r.mainLayout.AddItem(panel, 0, 1, true)
	r.mainLayout.AddItem(r.bashConsole, 1, 0, false)
	r.mainLayout.AddItem(r.buttonBar, 1, 0, false)
	r.mainLayout.AddItem(r.statusBar, 1, 0, false)

	r.AddPage(panelPage, r.mainLayout, true, true)
	r.AddPage(contextMenuPage, r.menuLayout, false, false)
	r.AddPage(renamePage, r.rename, false, false)
	r.AddPage(promptPage, r.prompt, false, false)
	r.AddPage(propertiesPage, r.properties, false, false)
	r.AddPage(pickerPage, r.picker, false, false)
	r.AddPage(errorPage, r.errorView, false, false)
	r.AddPage(quitConfirmPage, r.quitConfirm, false, false)
	r.AddPage(purgeConfirmPage, r.purgeConfirm, false, false)
	r.AddPage(sedReplacePage, r.sedLayout, false, false)
	r.AddPage(sedPreviewPage, r.sedPreviewLayout, false, false)
	r.AddPage(optionsPage, r.optionsList, false, false)
	r.AddPage(searchPage, r.searchPages, false, false)
	r.AddPage(chmodPage, r.chmodPages, false, false)
	r.AddPage(dirPickerPage, r.dirPicker, false, false)
	r.AddPage(helpPage, r.helpLayout, false, false)
	r.AddPage(viewerPage, r.viewerView, false, false)
	r.AddPage(detailsSidebarPage, r.detailsSidebarLayout, false, false)

	panel.SetMouseCapture(r.captureMouse)
	r.SetMouseCapture(r.captureOutsideClick)
	app.SetBeforeDrawFunc(r.handleBeforeDraw)

	r.applyTheme(theme)  // paints every widget constructed above in one place — see applyTheme's own doc comment
	r.refreshStatusBar() // initial sync — see the onLoad comment above

	// Both of these can have something to say, and both go through the
	// same showError overlay — collected into one notice rather than
	// risking the second call silently overwriting the first before
	// anything's even been drawn (see pruneTrashAtStartup's own doc
	// comment).
	var startupNotices []string
	if len(configWarnings) > 0 {
		startupNotices = append(startupNotices, fmt.Sprintf("config: %s", strings.Join(configWarnings, "; ")))
	}
	if notice := r.pruneTrashAtStartup(); notice != "" {
		startupNotices = append(startupNotices, notice)
	}
	if len(startupNotices) > 0 {
		r.showError(fmt.Errorf("%s", strings.Join(startupNotices, "\n\n")))
	}

	return r, nil
}

// SetVersionInfo lets cmd/breakthrough hand its own build-time
// version/commit/date/builtBy vars (set via ldflags by the release
// pipeline — see .goreleaser.yaml) to the Help overlay's own About
// section (help.go's aboutText), once NewRoot has already returned —
// there's no other point before then this package could read them at,
// since they're main's own package-level vars, not this one's. Never
// called at all by this package's own tests, which is exactly why
// NewRoot seeds every one of these fields with cmd/breakthrough's own
// default literals already (see the struct field's own doc comment):
// a Root nothing ever calls this on still shows sensible text.
func (r *Root) SetVersionInfo(version, commit, date, builtBy string) {
	r.appVersion = version
	r.appCommit = commit
	r.appBuildDate = date
	r.appBuiltBy = builtBy
}

// showOverlay closes whatever overlay (or stack of layered overlays) is
// currently open and opens page/widget as the new, sole one — the
// original one-overlay-at-a-time behavior every caller except the
// owner/group picker wants (see pushOverlay for that one).
//
// Focus is set explicitly via Application.SetFocus (inside pushOverlay)
// rather than relying on Pages' own "re-focus the last visible page if
// already focused" behavior — the implicit version turned out to be
// fragile in practice (Escape and outside clicks not reliably reaching
// the shown overlay), the same reason Panel.openEdit does this explicitly
// too.
func (r *Root) showOverlay(page string, widget tview.Primitive) {
	r.showOverlayWithRestore(page, widget, nil)
}

// showOverlayWithRestore is showOverlay, plus a restore callback for
// this specific frame — see overlayFrame and restoreProperties for the
// one case that currently needs it.
func (r *Root) showOverlayWithRestore(page string, widget tview.Primitive, restore func()) {
	r.closeAllOverlays()
	r.pushOverlay(page, widget, restore)
}

// pushOverlay adds page/widget as a new layer on top of whatever's
// already open, without closing it — see openOwnerGroupPicker
// (owner/group picker over Properties) and openHelp (help screen over
// anything).
//
// tview.Pages.ShowPage only flips a page's Visible flag — it does NOT
// reorder Pages' own internal page list, and Pages.Draw always walks that
// list in original AddPage registration order (verified by reading
// tview's pages.go directly). So without SendToFront here, a page that
// happened to be registered earlier in NewRoot (e.g. propertiesPage) would
// draw underneath — and get fully covered by — a page registered later
// (e.g. searchPage), even though pushOverlay just made it the topmost,
// most-recently-shown layer. SendToFront moves it to the end of Pages' own
// list so draw order actually matches stacking order, regardless of which
// page happened to be AddPage'd first.
func (r *Root) pushOverlay(page string, widget tview.Primitive, restore func()) {
	r.overlayStack = append(r.overlayStack, overlayFrame{page: page, widget: widget, restore: restore})
	r.activePage = page
	r.activeWidget = widget
	r.ShowPage(page)
	r.SendToFront(page)
	r.updateOverlayTitleBarColors()
	if restore != nil {
		restore()
	} else {
		r.app.SetFocus(widget)
	}
}

// updateOverlayTitleBarColors recolors every modal overlay's own title
// bar that doesn't already track its own focus state the way
// toolWindow's/Details' do (see toolwindow.go/detailssidebar.go) —
// currently propertiesTitleBar/menuTitleBar/helpTitleBar — based on
// which one, if any, is currently the topmost overlay (r.activePage):
// FocusedBackground for that one, EditableBackground for every other,
// per the user's own explicit request that this same active/inactive
// distinction apply to every panel with a title bar, not just tool
// windows'/Details' own. Called from pushOverlay/hideOverlay, the only
// two places activePage itself changes, and from applyTheme, so a live
// color-scheme switch re-derives the right one instead of resetting to
// a fixed color regardless of which overlay (if any) is actually active
// right now — the same live-switch hazard detailsTitleBar's own
// applyTheme case documents.
func (r *Root) updateOverlayTitleBarColors() {
	set := func(bar *tview.TextView, page string) {
		if r.activePage == page {
			bar.SetBackgroundColor(r.theme.FocusedBackground)
		} else {
			bar.SetBackgroundColor(r.theme.EditableBackground)
		}
	}
	set(r.propertiesTitleBar, propertiesPage)
	set(r.menuTitleBar, contextMenuPage)
	set(r.helpTitleBar, helpPage)
}

// pushOverlayReturningFocusTo is pushOverlay, plus emptyStackFocus (see
// its own doc comment on overlayFrame) for this one frame — currently
// only openCompletionPicker's own case.
func (r *Root) pushOverlayReturningFocusTo(page string, widget, emptyStackFocus tview.Primitive) {
	r.pushOverlay(page, widget, nil)
	r.overlayStack[len(r.overlayStack)-1].emptyStackFocus = emptyStackFocus
}

// hideOverlay closes just the topmost overlay layer, if any, revealing
// whatever was underneath it — restoring that layer's own focus (see
// overlayFrame.restore) — or, if that was the only one open, returning
// focus to the panel, unless that closing frame itself requested
// somewhere else instead (see overlayFrame.emptyStackFocus).
func (r *Root) hideOverlay() {
	if len(r.overlayStack) == 0 {
		return
	}
	top := r.overlayStack[len(r.overlayStack)-1]
	r.overlayStack = r.overlayStack[:len(r.overlayStack)-1]
	r.HidePage(top.page)

	if len(r.overlayStack) == 0 {
		r.activePage = ""
		r.activeWidget = nil
		r.updateOverlayTitleBarColors()
		if top.emptyStackFocus != nil {
			r.app.SetFocus(top.emptyStackFocus)
		} else {
			r.app.SetFocus(r.panel)
		}
		return
	}

	below := r.overlayStack[len(r.overlayStack)-1]
	r.activePage = below.page
	r.activeWidget = below.widget
	r.updateOverlayTitleBarColors()
	if below.restore != nil {
		below.restore()
	} else {
		r.app.SetFocus(below.widget)
	}
}

// closeAllOverlays hides every currently-open overlay layer without
// bothering to restore focus to any of the intermediate ones along the
// way — showOverlay's own tail end (pushOverlay) sets focus correctly
// once, for whatever it opens next.
func (r *Root) closeAllOverlays() {
	for _, f := range r.overlayStack {
		r.HidePage(f.page)
	}
	r.overlayStack = nil
	r.activePage = ""
	r.activeWidget = nil
}

// captureOutsideClick keeps the panel underneath an open overlay inert:
// a click outside the overlay closes it (instead of leaving it stuck
// open) and is consumed rather than also acting on the panel, so it takes
// a second click to do anything else. Every *other* mouse action outside
// the overlay — scrolling, but also plain movement, button-down/up, and
// double/middle clicks — is swallowed outright without closing anything,
// the same "consumed, no other effect" a plain outside click gets once
// it's done its one job of closing the overlay.
//
// That "every other action" part used to be narrower — only scrolling
// was actually swallowed; everything else fell through to whatever was
// underneath unconsumed — which left a real gap: a right-click *drag*
// (see Root.captureMouse's own doc comment) is MouseRightDown, then
// MouseMove/MouseRightUp, not a single MouseRightClick, so none of those
// were being caught here at all. With the quit-confirmation overlay
// open, that meant a right-click on a panel row still reached
// Panel.captureMouse underneath it and opened the context menu right on
// top of the still-open quit dialog — exactly the user's own direct
// report ("wenn der quit confirm dialog offen ist, kann man immer noch
// rumklicken oder den overlay öffnen. dann muss wirklich alle gesperrt
// sein"). Consuming unconditionally, rather than opting in one action at
// a time, closes that gap for good — including for any other overlay
// this already guards, not just the quit dialog — instead of requiring
// every individual mouse-action variant tview has to be enumerated and
// kept in sync here by hand.
//
// The Properties overlay is the one exception to "click outside closes
// it": once propertiesDirty is true (see markPropertiesDirty), an
// outside click is consumed and otherwise ignored instead — Cancel or
// Save is the only way out from there, so an in-progress edit (a
// permission bit already toggled, a name half-typed) can't be silently
// discarded, or just as silently lost track of, by a stray click.
//
// The Details button specifically is a second, narrower exception,
// checked before either of the above: a click on it reaches the button
// bar's own handling (see buttonBarActionAt/runButtonBarAction)
// completely untouched, toggling the Details sidebar alongside
// Properties rather than being swallowed as an "outside click" or
// (while dirty) ignored outright — per the user's own explicit request
// to open or close Details *while Properties stays open*, the same
// "also works while Properties is open" carve-out
// ToggleDetailsSidebarShortcut's own doc comment already makes for
// Ctrl+D. The two already coexist independently of this (see
// ComputeHashesShortcut's own doc comment); this is only what let the
// click reach that existing mechanism in the first place. Scoped to
// Properties and to Details alone — every other overlay, and every
// other button-bar click, still gets the ordinary handling below.
func (r *Root) captureOutsideClick(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if r.activePage == "" {
		return action, event // nothing open, nothing to do
	}

	x, y := event.Position()
	if primitiveContains(r.activeWidget, x, y) {
		return action, event // event landed on the open overlay itself
	}

	if r.activePage == propertiesPage && action == tview.MouseLeftClick {
		if bAction, ok := r.buttonBarActionAt(x, y); ok && bAction == buttonActionDetails {
			return action, event
		}
	}

	if action == tview.MouseLeftClick || action == tview.MouseRightClick {
		if r.activePage == propertiesPage && r.propertiesDirty {
			return tview.MouseConsumed, nil // Cancel/Save only, see propertiesDirty's own doc comment above
		}
		r.hideOverlay()
	}
	return tview.MouseConsumed, nil
}

// primitiveContains reports whether (x, y) falls within p's rectangle.
func primitiveContains(p tview.Primitive, x, y int) bool {
	rx, ry, w, h := p.GetRect()
	return x >= rx && x < rx+w && y >= ry && y < ry+h
}

// clampToPanel keeps an overlay of the given size fully inside the
// panel's inner rect, shrinking it first if it can't fit at all — a long
// path in the Info view is easily wider than an 80-column terminal, and
// without this the labelled left edge gets pushed off-screen.
func (r *Root) clampToPanel(x, y, width, height int) (int, int, int, int) {
	px, py, pw, ph := r.panel.GetInnerRect()
	if pw <= 0 || ph <= 0 {
		return x, y, width, height
	}

	if width > pw {
		width = pw
	}
	if height > ph {
		height = ph
	}
	if x+width > px+pw {
		x = px + pw - width
	}
	if y+height > py+ph {
		y = py + ph - height
	}
	if x < px {
		x = px
	}
	if y < py {
		y = py
	}

	return x, y, width, height
}

// clampToScreen is clampToPanel's own logic, bounded against the whole
// screen (Root's own rect) instead of just the current panel's inner
// rect — used only by the Help overlay (see helpSize), a read-only
// reference deliberately allowed to span wider than one panel. Every
// other overlay in this app stays within one panel — see clampToPanel's
// own doc comment.
func (r *Root) clampToScreen(x, y, width, height int) (int, int, int, int) {
	_, _, sw, sh := r.GetRect()
	if sw <= 0 || sh <= 0 {
		return x, y, width, height
	}

	if width > sw {
		width = sw
	}
	if height > sh {
		height = sh
	}
	if x+width > sw {
		x = sw - width
	}
	if y+height > sh {
		y = sh - height
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	return x, y, width, height
}

// handleBeforeDraw is registered via Application.SetBeforeDrawFunc in
// NewRoot, so it runs before every single Draw — deliberately not
// SetAfterDrawFunc: Application.draw itself already updates Root's own
// rect to the new screen size (SetRoot's own "fullscreen" argument)
// before calling either handler, but only *before* actually runs before
// the primitive tree is drawn (verified directly against tview's own
// application.go, not guessed — the two are asymmetric despite the
// similar names). Repositioning from an after-handler would still look
// right eventually, but only from the *next* draw on, one full resize
// behind, rather than in the very same frame the resize itself caused.
//
// A real, user-reported gap this fixes: neither Properties nor the
// Details sidebar previously reacted to a live terminal resize at all —
// both compute their own size/position once, at whatever moment they're
// opened or last re-rendered for an unrelated reason (an edit, a hash
// finishing, ...), and nothing before this ever revisited that purely
// because the terminal itself changed size. The panel underneath is
// unaffected either way: panelPage is the one page in Root added with
// AddPage's own resize=true, so tview already re-fits it to the new
// screen on every Draw, with no help needed here.
//
// Every other overlay (Search, Help, Options, Sed, the pickers, ...)
// still has the same gap this fixes for Properties and Details
// specifically — deliberately narrow in scope to the two the user
// actually asked about, not a general "reposition whatever's currently
// open" mechanism; extending it further is a real, and fairly
// mechanical, follow-up once there's a reason to.
//
// One real remaining wrinkle for Properties specifically: it sizes
// itself via clampToPanel, against the panel's own current inner rect —
// but the panel (mainLayout, panelPage's own Item) only gets its rect
// updated to match a resize via Pages' own resize=true handling inside
// root.Draw, which runs *after* this returns (see above). So on the
// very first draw following a resize, Properties still clamps against
// the panel's previous-frame bounds, catching up one draw later —
// normally imperceptible (an actual resize drag fires many of these in
// quick succession, well before a user could react to any one of them),
// and correct behavior arrives well before the *next* deliberate
// interaction either way. Recomputing the panel's own rect by hand here
// too, ahead of Pages' own turn, was tried and rejected: Flex layout
// (mainLayout's own bashConsole/buttonBar/statusBar split, further
// complicated by bashConsole's own variable expanded height) isn't
// something worth re-deriving by hand just to shave off one frame,
// on a component tview itself already recomputes correctly moments
// later anyway.
//
// Always returns false: returning true here would tell Application.draw
// to skip drawing the primitive tree entirely for this frame (see
// SetBeforeDrawFunc's own doc comment) — never what a mere size check
// should do.
func (r *Root) handleBeforeDraw(screen tcell.Screen) bool {
	width, height := screen.Size()
	if width == r.lastScreenWidth && height == r.lastScreenHeight {
		return false
	}
	r.lastScreenWidth, r.lastScreenHeight = width, height

	if r.detailsSidebarVisible {
		r.repositionDetailsSidebar()
	}
	if r.activePage == propertiesPage {
		r.rerenderProperties()
	}

	return false
}

// RequestQuit shows a confirmation overlay instead of quitting right
// away — Ctrl+Q (see cmd/breakthrough) is easy to hit by accident, so the
// application only actually stops once the user picks "Quit breakthrough"
// from this overlay (or presses Enter, since it's the default selection).
func (r *Root) RequestQuit() {
	// Ctrl+Q is a global key capture, so it can arrive while the header
	// is mid-edit. Without this the edit field would stay on screen after
	// cancelling the quit, focused-looking but unreachable, since
	// hideOverlay hands focus to the panel's table rather than back to it.
	r.panel.cancelEdit()

	width, height := listSize(r.quitConfirm)

	_, _, screenWidth, screenHeight := r.GetRect() // Root fills the whole screen
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2

	r.quitConfirm.SetRect(x, y, width, height)
	r.quitConfirm.SetCurrentItem(0)
	r.showOverlay(quitConfirmPage, r.quitConfirm)
}

// RequestCancel is the Ctrl+Q sibling for Ctrl+C (see cmd/breakthrough):
// a global "back out of whatever is open" that behaves like Escape.
// Having it as a real key matters because Escape is deliberately inert
// while the path header is being edited — Ctrl+C is the keyboard way out
// of that, where otherwise only a mouse click would do.
//
// It never quits: stopping breakthrough is Ctrl+Q plus a confirmation.
func (r *Root) RequestCancel() {
	if r.activePage != "" {
		r.hideOverlay()
		return
	}
	r.panel.cancelEdit()
}

// ToggleMouseShortcut is F3's own action — see cmd/breakthrough. Always
// fires regardless of what's currently open or focused, the same
// "reachable from literally anywhere" category F1/Ctrl+Q/Ctrl+C are in
// (see HelpShortcut's own doc comment): the whole point is grabbing
// text via the terminal's own native selection, which can be anywhere
// on screen — a dialog, the bash line, a plain directory listing — so
// gating this behind acceptsGlobalShortcut the way most other shortcuts
// are would defeat it in exactly the cases it's most likely needed.
//
// See mouseEnabled's own doc comment on why this exists at all: mouse
// reporting being on is what makes this app's own clicks/drags work,
// but it also hands the terminal emulator's own native text
// selection/copy over to breakthrough instead — not every terminal's
// own override gesture (Shift-drag, on most xterm-derived emulators) is
// something everyone already knows, and this is a plain, memorable way
// out that doesn't depend on it. refreshStatusBar repaints the "Mouse
// on/off" segment (see buildStatusBar) immediately, rather than waiting
// for StartClock's own once-a-second tick to eventually catch up.
func (r *Root) ToggleMouseShortcut() {
	r.mouseEnabled = !r.mouseEnabled
	r.app.EnableMouse(r.mouseEnabled)
	r.refreshStatusBar()
}

// confirmQuit is the quit overlay's "Quit breakthrough" action.
func (r *Root) confirmQuit() {
	r.app.Stop()
}

// cancelQuit hides the quit overlay without taking any action (Escape or
// "Cancel").
func (r *Root) cancelQuit() {
	r.hideOverlay()
}

// advanceDrag brings the live-toggled range up to date with row, the
// mouse's current (from MouseMove) or final (from MouseRightUp) position.
// The very first time row differs from dragStartRow, it also brings the
// press row itself into the toggle — it isn't toggled immediately on
// MouseRightDown, so that a plain click (down and up with no movement at
// all) never toggles anything, matching the same click-vs-drag test
// tview itself uses (see captureMouse's doc comment). Safe to call
// repeatedly (every MouseMove) or exactly once (MouseRightUp with no
// MouseMove in between): each call only toggles the delta since the last
// one, via Panel.applyDragDelta, so calling it more or fewer times along
// the same path never changes the end result.
func (r *Root) advanceDrag(row int) {
	if !r.dragMoved && row != r.dragStartRow {
		r.dragMoved = true
		r.panel.toggleCheckbox(r.dragStartRow)
	}
	if r.dragMoved && row != r.dragCurrentRow {
		r.panel.applyDragDelta(r.dragStartRow, r.dragCurrentRow, row)
	}
	r.dragCurrentRow = row
}

// captureMouse intercepts right-button activity on the panel: a plain
// right-click opens the context menu (unchanged from before); a
// right-button drag across rows instead toggles each of them live, as the
// drag reaches them, and does not open the menu. Everything else
// (left-click, scrolling) passes through unchanged to the panel's own
// handling — see Panel.activateRow for that.
//
// The click-vs-drag distinction is tview's own, not something tracked
// here: Application.fireMouseActions only synthesizes MouseRightClick
// when the release position matches the press position — a genuine drag
// simply never produces one, only MouseRightDown, MouseMove (repeatedly,
// while the button stays held — button state itself hasn't changed, so
// neither Down nor Up fires again for these), and MouseRightUp fire.
// MouseRightUp always runs first, for both a click and a drag; advanceDrag
// only actually toggles anything once the release position has moved off
// the press row (possibly having done so already via MouseMove, possibly
// only now if no MouseMove ever arrived — e.g. a fast drag some terminals
// report as just down+up with no intermediate positions). If nothing ever
// moved, the event is left unconsumed so the MouseRightClick that (per
// tview) is about to follow can open the menu as usual.
//
// This is also where Panel.captureOutsideEdit gets a chance to run first:
// only one SetMouseCapture can be installed on Panel at a time, and Root
// already needs that slot for right-click detection, so Root's capture
// delegates to Panel's own "was the header being edited and did this
// click land outside it" check before doing anything else.
func (r *Root) captureMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if r.panel.captureOutsideEdit(action, event) {
		return tview.MouseConsumed, nil
	}

	switch action {
	case tview.MouseRightDown:
		x, y := event.Position()
		if row, ok := r.panel.rowIndexAt(x, y); ok {
			r.dragStartRow = row
			r.dragCurrentRow = row
			r.dragMoved = false
			r.dragging = true
			r.panel.focusRow(row) // move the highlight to the press row right away, not just on release
		} else {
			r.dragging = false
		}
		return action, event // Table has no case for this action; harmless to pass through

	case tview.MouseMove:
		if !r.dragging {
			return action, event
		}
		x, y := event.Position()
		if row, ok := r.panel.rowIndexAt(x, y); ok {
			r.advanceDrag(row)
			r.panel.focusRow(row)
		}
		return tview.MouseConsumed, nil

	case tview.MouseRightUp:
		wasDragging := r.dragging
		r.dragging = false
		if !wasDragging {
			return action, event
		}
		x, y := event.Position()
		if endRow, ok := r.panel.rowIndexAt(x, y); ok {
			r.advanceDrag(endRow)
		}
		if !r.dragMoved {
			// Never actually moved off the press row: nothing was
			// toggled, let a same-position MouseRightClick, if tview
			// fires one right after this, open the menu as usual.
			return action, event
		}
		r.panel.focusRow(r.dragCurrentRow) // leave the highlight on the row the drag ended on
		return tview.MouseConsumed, nil

	case tview.MouseRightClick:
		x, y := event.Position()
		path, ok := r.panel.RowAt(x, y)
		if !ok {
			return action, event // nothing sensible to act on
		}
		row, ok := r.panel.rowIndexAt(x, y)
		if !ok {
			// RowAt just succeeded via this same lookup, so this
			// shouldn't happen — stay defensive rather than fall back to
			// a stale or wrong targetRow.
			return action, event
		}
		r.panel.focusRow(row) // the menu is about this row; the highlight should agree
		r.target = path
		r.targetRow = row
		r.showMenu(x, y)
		return tview.MouseConsumed, nil

	case tview.MouseLeftDoubleClick:
		// This, not Panel.handleNameClick, is where an actual fast
		// double-click on a row's name activates it (see
		// handleNameClick's own doc comment for why): tview's own
		// Application.fireMouseActions fires this action for a second
		// click's release landing within DoubleClickInterval (500ms) of
		// the first, INSTEAD of a second MouseLeftClick — verified
		// directly against tview's own source, not guessed — and
		// Table.MouseHandler has no case for it at all, so it would
		// otherwise just be silently dropped. Scoped to the name column
		// specifically (colName), matching exactly what a plain click
		// there already activates via TableCell.SetClickedFunc (see
		// addRow) — a double-click landing on the checkbox column
		// instead stays the checkbox's own business alone; without this
		// check, double-clicking it fast would activate the row (wrong
		// column entirely) instead of just toggling it twice.
		x, y := event.Position()
		row, column := r.panel.table.CellAt(x, y)
		if column != colName {
			return action, event
		}
		if _, ok := r.panel.rowRef(row); !ok {
			return action, event
		}
		r.panel.focusRow(row)
		r.panel.activateRow(row)
		return tview.MouseConsumed, nil

	default:
		return action, event
	}
}

// menuSectionLabel renders a non-actionable divider row's text for the
// context menu — dim style tags (tview's own "[fg:bg:flags]" syntax,
// enabled by default for List item text) rather than a real, clickable
// item, so it reads as a section label. Paired with a nil selected func
// in the AddItem call that uses it (see NewRoot), which makes Enter/click
// on the row a no-op. Arrow-key navigation still stops on it for a
// moment, since tview.List has no "disabled item" concept to skip it with
// — a small, accepted quirk rather than hand-rolling navigation logic for
// what's cosmetic.
func menuSectionLabel(name string) string {
	return fmt.Sprintf("[::d]── %s ──[::-]", name)
}

// showMenu positions the context menu near (x, y), clamped to the panel's
// inner rect so it doesn't get drawn partly off-screen, and reveals it as
// an overlay on top of the still-visible panel.
func (r *Root) showMenu(x, y int) {
	// Defensive re-sync rather than trusting each toggle method's own
	// relabel to always have run last: cheap, and keeps this correct even
	// if something else ever changes the underlying Panel field directly.
	r.menu.SetItemText(r.hiddenToggleIdx, hiddenToggleLabel(r.panel.showHidden), "")
	r.menu.SetItemText(r.sizeFormatToggleIdx, sizeFormatToggleLabel(r.panel.sizeBytes), "")
	r.menu.SetItemText(r.mtimeFormatToggleIdx, mtimeFormatToggleLabel(r.panel.mtimeUnix), "")

	width, height := listSize(r.menu)
	height++ // reserved title bar row (see menuLayout)
	x, y, width, height = r.clampToPanel(x, y, width, height)

	r.menuLayout.SetRect(x, y, width, height)
	r.menu.SetCurrentItem(0)
	r.showOverlay(contextMenuPage, r.menu)
}

// toggleHidden is the context menu's "Globals" hidden-files toggle: flips
// whether dotfile entries are shown (see Panel.showHidden), reloads the
// current directory so the change takes effect immediately, and relabels
// the menu item itself to describe what clicking it will do next time.
func (r *Root) toggleHidden() {
	r.panel.showHidden = !r.panel.showHidden
	r.showError(r.panel.load(r.panel.path))
	r.menu.SetItemText(r.hiddenToggleIdx, hiddenToggleLabel(r.panel.showHidden), "")
	r.settings.ShowHidden = r.panel.showHidden
	r.persistSetting("show_hidden", strconv.FormatBool(r.panel.showHidden))
}

// hiddenToggleLabel renders the hidden-files toggle's label as the
// action clicking it performs next, not its current state — e.g. it
// reads "Show hidden files" while they're hidden, the same convention
// most file managers use for a toggle like this.
func hiddenToggleLabel(showHidden bool) string {
	if showHidden {
		return "Hide hidden files"
	}
	return "Show hidden files"
}

// toggleSizeBytes is the "Globals" section's Size-format toggle: flips
// whether the list's Size column shows exact bytes or humanSize's
// shorthand (see Panel.sizeBytes/formatSizeCell), reloads the current
// directory so the change takes effect immediately, and relabels the
// menu item itself — the same pattern toggleHidden already uses, see its
// own doc comment for why (dirty labels, dirty defensive re-sync).
func (r *Root) toggleSizeBytes() {
	r.panel.sizeBytes = !r.panel.sizeBytes
	r.showError(r.panel.load(r.panel.path))
	r.menu.SetItemText(r.sizeFormatToggleIdx, sizeFormatToggleLabel(r.panel.sizeBytes), "")
	r.settings.SizeBytes = r.panel.sizeBytes
	r.persistSetting("size_bytes", strconv.FormatBool(r.panel.sizeBytes))
}

// sizeFormatToggleLabel is sizeBytes's own toggleHidden-style label.
func sizeFormatToggleLabel(sizeBytes bool) string {
	if sizeBytes {
		return "Show size (human-readable)"
	}
	return "Show size in bytes"
}

// toggleMtimeUnix is the "Globals" section's Modified-format toggle:
// flips whether the list's Modified column shows a Unix timestamp or the
// formatted "YYYY-MM-DD HH:MM:SS" (see Panel.mtimeUnix/
// formatModTimeCell) — otherwise a copy of toggleSizeBytes.
func (r *Root) toggleMtimeUnix() {
	r.panel.mtimeUnix = !r.panel.mtimeUnix
	r.showError(r.panel.load(r.panel.path))
	r.menu.SetItemText(r.mtimeFormatToggleIdx, mtimeFormatToggleLabel(r.panel.mtimeUnix), "")
	r.settings.MtimeUnix = r.panel.mtimeUnix
	r.persistSetting("mtime_unix", strconv.FormatBool(r.panel.mtimeUnix))
}

// mtimeFormatToggleLabel is mtimeUnix's own toggleHidden-style label.
// Worded as "time", not "mtime": this same column, and this same
// toggle, now applies to a trashed item's own deletion time while
// browsing the trash (see Panel.onDescribeRows/buildColumnHeader), not
// only a real directory's modification time — "mtime" specifically
// would be wrong there.
func mtimeFormatToggleLabel(mtimeUnix bool) string {
	if mtimeUnix {
		return "Show time formatted"
	}
	return "Show time as timestamp"
}

// listSize returns a no-border, no-secondary-text List's width — the
// widest item's rendered text plus 1-char left/right padding (see the
// SetBorderPadding calls in NewRoot) — and its height, one row per item.
// tview.TaggedStringWidth, not a plain rune count, since section-header
// items (see menuSectionLabel) carry style tags that aren't part of what
// actually gets drawn.
func listSize(l *tview.List) (width, height int) {
	for i := 0; i < l.GetItemCount(); i++ {
		main, _ := l.GetItemText(i)
		if w := tview.TaggedStringWidth(main); w > width {
			width = w
		}
	}
	return width + 2, l.GetItemCount() // +2: 1-char padding on each side
}

// closeMenu hides the context menu without taking any action (Escape).
func (r *Root) closeMenu() {
	r.hideOverlay()
}

// openRename is the context menu's "Rename" action. Rather than a prompt
// floating near the menu, it positions the rename field exactly over the
// target's own name cell — not the whole row: the checkbox column is
// deliberately left uncovered, so the row's current checked state stays
// visible (without becoming editable itself) while renaming. It reads as
// just the name becoming editable in place, pre-filled with the current
// one.
func (r *Root) openRename() {
	x, y, width, ok := r.panel.nameCellRect(r.targetRow)
	if !ok {
		return // targetRow came from a right-click just validated by RowAt
	}
	x, y, width, height := r.clampToPanel(x, y, width, 1)

	r.rename.SetText(filepath.Base(r.target))
	r.rename.SetRect(x, y, width, height)

	r.showOverlay(renamePage, r.rename)
}

// finishRename handles Enter (submit) and Escape/Tab (cancel) in the
// rename prompt.
//
// The rename field is closed up front rather than in a defer: a failed
// rename opens the error overlay, and hiding "the active overlay"
// afterwards would close that error again before the user ever saw it.
func (r *Root) finishRename(key tcell.Key) {
	newName := r.rename.GetText()
	r.hideOverlay()

	if key != tcell.KeyEnter {
		return // cancelled
	}
	if newName == "" || newName == filepath.Base(r.target) {
		return
	}

	newPath, err := fsops.Rename(r.target, newName)
	if err != nil {
		r.showError(err)
		return
	}
	r.refreshDetailsIfShowing(r.target, newPath)
	r.showError(r.panel.load(r.panel.path))
}

// openPrompt shows a single-line input overlay labelled label, pre-filled
// with prefill, centered on screen (unlike rename, it isn't tied to any
// one row — Select +/-/chown/chmod all act on either the checkbox
// selection or a single right-clicked target, not a visible row range).
// onSubmit runs with whatever was typed if the user confirms with Enter;
// see finishPrompt for what happens on Escape/Tab instead.
func (r *Root) openPrompt(label, prefill string, onSubmit func(text string)) {
	r.prompt.SetLabel(label + " ")
	r.prompt.SetText(prefill)
	r.promptSubmit = onSubmit

	width := tview.TaggedStringWidth(label) + 26 // label plus room to type
	const height = 1
	_, _, screenWidth, screenHeight := r.GetRect() // Root fills the whole screen
	if width > screenWidth {
		width = screenWidth
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	x, y, width, clampedHeight := r.clampToPanel(x, y, width, height)

	r.prompt.SetRect(x, y, width, clampedHeight)
	r.showOverlay(promptPage, r.prompt)
}

// finishPrompt handles Enter (submit) and Escape/Tab (cancel) in the
// generic prompt overlay (see openPrompt) — the same DoneFunc pattern
// finishRename uses, generalized since this overlay backs several
// different actions rather than just one.
func (r *Root) finishPrompt(key tcell.Key) {
	text := r.prompt.GetText()
	submit := r.promptSubmit
	r.hideOverlay()
	r.promptSubmit = nil

	if key != tcell.KeyEnter || text == "" || submit == nil {
		return // cancelled, or nothing typed
	}
	submit(text)
}

// openSelectPlus is the context menu's "Select +": prompts for a glob
// pattern and checks every currently-listed entry that matches it, in
// addition to whatever was already checked.
func (r *Root) openSelectPlus() {
	r.openPrompt("Select + (glob pattern):", "", func(text string) {
		if _, err := r.panel.selectByPattern(text, true); err != nil {
			r.showError(err)
		}
	})
}

// openSelectMinus is "Select -": the same pattern prompt as Select+, but
// unchecks matches instead of checking them.
func (r *Root) openSelectMinus() {
	r.openPrompt("Select - (glob pattern):", "", func(text string) {
		if _, err := r.panel.selectByPattern(text, false); err != nil {
			r.showError(err)
		}
	})
}

// clipboardTargets is what Copy/Cut capture: the current checkbox
// selection if there is one, otherwise just the entry the context menu
// was opened on — so Copy/Cut/Paste on a single, unmarked file needs no
// separate "select it first" step.
func (r *Root) clipboardTargets() []string {
	if paths := r.panel.SelectedPaths(); len(paths) > 0 {
		return paths
	}
	if r.target != "" {
		return []string{r.target}
	}
	return nil
}

// selectedOrCurrentPaths is what Move to Trash/Remove (see trash.go) and
// Sed Replace (see sedreplace.go) all act on: the current checkbox
// selection if there is one, otherwise whichever entry the table's
// cursor is currently on — the same fallback shape clipboardTargets
// uses for Copy/Cut, but read directly from the panel's cursor instead
// of r.target, so it also works for the keyboard-shortcut path (Ctrl+T/
// Entf, Ctrl+R/Ctrl+Delete, Ctrl+S — see cmd/breakthrough), which never
// goes through a right-click that would have set r.target at all.
func (r *Root) selectedOrCurrentPaths() []string {
	if paths := r.panel.SelectedPaths(); len(paths) > 0 {
		return paths
	}
	if _, path, ok := r.panel.CurrentRowPath(); ok {
		return []string{path}
	}
	return nil
}

// copyToClipboard is the context menu's "Copy": remembers the current
// clipboard targets (see clipboardTargets) for a later Paste, which will
// copy them, leaving these where they are.
func (r *Root) copyToClipboard() {
	r.clipboard = r.clipboardTargets()
	r.clipboardCut = false
}

// cutToClipboard is "Cut": same as Copy, except the later Paste will move
// the targets (removing them from here) instead of copying them.
func (r *Root) cutToClipboard() {
	r.clipboard = r.clipboardTargets()
	r.clipboardCut = true
}

// pasteClipboard is "Paste": copies or moves (per clipboardCut)
// whatever Copy/Cut last captured into the directory currently on
// screen — pasteInto's own thin wrapper for that common case.
//
// While search results are showing (see Panel.searchMode), "the
// directory currently on screen" has no single meaning any more — the
// rows visible are scattered across however many real directories a
// search touched, and r.panel.path itself stays whatever real
// directory was current *before* the search ran (see
// Panel.showSearchResults' own doc comment), not any of them. Pasting
// there instead of alongside the row that was actually right-clicked
// (r.target, set by Root.captureMouse's MouseRightClick case the exact
// same way for a search result as for a real row) would silently land
// in an unrelated directory the user never asked about.
func (r *Root) pasteClipboard() {
	dir := r.panel.path
	if r.panel.searchMode {
		dir = filepath.Dir(r.target)
	}
	r.pasteInto(dir)
}

// pasteInto is pasteClipboard's own shared implementation, generalized
// to an explicit destination directory — pasteClipboard itself is the
// only caller, picking a search result's own directory instead of
// r.panel.path while search results are showing (see its own doc
// comment). A no-op if nothing was ever copied/cut.
//
// Each target that would collide with an existing entry in dir is
// skipped with an error — asking "overwrite?" once per colliding file
// in a multi-file paste isn't built yet (a known simplification;
// fsops.Copy/Move's force parameter is where that would hook in).
// Only the first error is reported, to avoid stacking one error
// overlay per failed file; the rest of the paste still runs to
// completion rather than stopping at the first collision.
func (r *Root) pasteInto(dir string) {
	if len(r.clipboard) == 0 {
		return
	}

	var firstErr error
	for _, src := range r.clipboard {
		dst := filepath.Join(dir, filepath.Base(src))
		var err error
		if r.clipboardCut {
			err = fsops.Move(src, dst, false)
		} else {
			err = fsops.Copy(src, dst, false)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
		// Only for a successful Move, not Copy: src is untouched by a
		// copy (still exactly what Details would already be showing, if
		// anything), and dst is a brand new path a copy could never
		// already have been the target of. A move, like a rename,
		// genuinely relocates the same real entry — Details needs to
		// keep following it under its new path.
		if err == nil && r.clipboardCut {
			r.refreshDetailsIfShowing(src, dst)
		}
	}

	if r.clipboardCut && firstErr == nil {
		r.clipboard = nil // moved away cleanly; nothing left to paste again
	}

	// Only reload if the panel actually happens to be showing dir right
	// now — pasting into a search result's own directory, elsewhere,
	// shouldn't force-navigate or otherwise disturb whatever the panel
	// currently has on screen.
	if r.panel.path == dir {
		if err := r.panel.load(r.panel.path); err != nil {
			firstErr = err // the reload failing is more urgent to report than a copy conflict
		}
	}

	if firstErr != nil {
		r.showError(firstErr)
	}
}

// openChown is the context menu's "chown": opens a scrollable picker
// (openOwnerGroupPicker) of every local user, then — once one's picked —
// every local group, applying both once the group's confirmed too.
// Backing out of just the group step (Escape) still applies the
// already-picked owner, leaving the group unchanged — the same
// flexibility chown(1)'s own "owner[:group]" syntax has always had.
//
// Falls back to that same text syntax (openChownTextFallback, this
// action's entire behavior before the picker existed) if the picker's
// data source (fsops.ListUsers/ListGroups) isn't available — e.g. on
// macOS. target is captured up front rather than read from r.target
// inside the callbacks — nothing else changes it while any of this is
// open in this single-threaded UI, but reading it early avoids relying
// on that staying true.
//
// The context menu is closed explicitly first: openOwnerGroupPicker
// always layers on top of whatever's currently open (see
// pushOverlay) rather than replacing it — the right behavior when it's
// opened from Properties' Owner/Group fields (see
// activatePropertyField), but not here, where nothing should be left
// showing underneath it.
func (r *Root) openChown() {
	r.hideOverlay()
	target := r.target

	info, err := fsops.Stat(target)
	if err != nil {
		r.showError(err)
		return
	}

	r.openOwnerGroupPicker(pickUser, info.UID, r.centeredOnScreen, func(_ string, uid int) {
		r.openOwnerGroupPicker(pickGroup, info.GID, r.centeredOnScreen, func(_ string, gid int) {
			r.applyChown(target, uid, gid)
		}, func() {
			r.applyChown(target, uid, -1) // group step cancelled: owner-only change
		}, func() {
			r.openChownTextFallback(target)
		})
	}, nil, func() {
		r.openChownTextFallback(target)
	})
}

// applyChown runs fsops.Chown and reloads the panel, reporting either's
// failure — the common tail of every path through openChown.
func (r *Root) applyChown(target string, uid, gid int) {
	if err := fsops.Chown(target, uid, gid); err != nil {
		r.showError(err)
		return
	}
	r.refreshDetailsIfShowing(target, target)
	r.showError(r.panel.load(r.panel.path))
}

// openChownTextFallback prompts for chown(1)'s own "owner[:group]"
// syntax — openChown's fallback when the picker's data source isn't
// available.
func (r *Root) openChownTextFallback(target string) {
	r.openPrompt("chown (owner[:group]):", "", func(text string) {
		uid, gid, err := fsops.ParseOwnerGroup(text)
		if err != nil {
			r.showError(err)
			return
		}
		r.applyChown(target, uid, gid)
	})
}
