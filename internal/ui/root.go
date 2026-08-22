package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/fsops"
)

const (
	panelPage       = "panel"
	contextMenuPage = "context-menu"
	renamePage      = "rename"
	promptPage      = "prompt"
	pickerPage      = "owner-group-picker"
	quitConfirmPage = "quit-confirm"
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
type overlayFrame struct {
	page    string
	widget  tview.Primitive
	restore func()
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

	// theme is the active color scheme, resolved once at startup (see
	// loadInitialSettings/applyTheme) from settings.ColorScheme against
	// colorSchemes, and again live whenever the Settings overlay (see
	// openSettings/applyColorScheme) picks a different one. settings is
	// every other on-disk setting alongside it (currently just the
	// reserved, not-yet-functional Language placeholder — see
	// config.Settings' own doc comment). colorSchemes is every scheme
	// available to pick from, loaded once at startup — see openSettings'
	// own doc comment on why it isn't re-scanned on every open.
	theme        config.ResolvedTheme
	settings     config.Settings
	colorSchemes []config.NamedTheme

	panel        *Panel
	menu         *tview.List
	rename       *tview.InputField
	prompt       *tview.InputField
	picker       *tview.List // owner/group picker — see openOwnerGroupPicker
	errorView    *tview.TextView
	quitConfirm  *tview.List
	settingsList *tview.List // Settings overlay — see openSettings

	// The search dialog (see search.go/newSearchDialog): searchPages
	// wraps searchForm (the pattern/scope/mode/engine/content inputs) and
	// searchList (the results shown once a search has run) as two pages,
	// the same "several sub-widgets, one overlay" shape r.properties
	// already has. searchScopeField is searchForm's own path field, kept
	// individually addressable for Tab-completion, the same reason
	// r.panel.headerEdit is — see captureSearchScopeKey.
	// searchEngineOptions/searchContentOptions record which
	// search.Engine/search.ContentMode each of their own dropdown's
	// options actually maps to (built once, since availability —
	// LocateAvailable/ZgrepAvailable/ZipgrepAvailable — doesn't change
	// mid-session) — the dropdown's own selected index alone isn't
	// enough once an option is conditionally left out (see their own
	// doc comments in search.go).
	searchPages          *tview.Pages
	searchForm           *tview.Form
	searchScopeField     *tview.InputField
	searchList           *tview.List
	searchEngineOptions  []searchEngineOption
	searchContentOptions []searchContentOption
	// searchCancel stops whatever search.Run call is currently in
	// flight, if any — called before starting a new one, and when the
	// dialog closes, so a slow "find /" left running never keeps working
	// after the user has moved on (see runSearch/closeSearch).
	searchCancel context.CancelFunc

	// mainLayout wraps panel, bashLine, and statusBar into the vertical
	// stack registered as panelPage (see newBottomBar/NewRoot) — panel
	// still owns its own rect the same way it always has (clampToPanel
	// and everything else reading panel.GetInnerRect() is unaffected),
	// just resized to leave the bottom two rows free.
	mainLayout *tview.Flex

	// bashLine is the second-to-last row: a plain shell command line
	// (see runShellCommand) — pasting into it works because
	// cmd/breakthrough enables tview's bracketed-paste support
	// (Application.EnablePaste), not anything Root itself does.
	//
	// statusBar is the last row: user/disk-usage/quick-action buttons/
	// clock (see buildStatusBar), with statusBarSpans locating each
	// button the same way propertySpans do for Properties.
	bashLine       *tview.InputField
	statusBar      *tview.TextView
	statusBarSpans []statusBarSpan

	// bashHistory is every command available for the bash line's Up/Down
	// navigation (see bashHistoryUp/Down) — seeded at construction from
	// bashHistoryFile's existing content (see historyFilePath/
	// loadBashHistory: real, cross-session history, the same as a real
	// shell's own), then appended to (both here and back to
	// bashHistoryFile — see appendBashHistory) as runShellCommand runs
	// each one. bashHistoryIdx is which entry is currently showing:
	// len(bashHistory) means "not currently browsing history" — a fresh
	// or in-progress line, not one recalled from it — in which case
	// bashHistoryDraft is what that in-progress line was, restored if
	// Down is pressed back past the newest entry.
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
	propertiesDirty bool
	stagedName      string
	stagedMode      os.FileMode
	stagedMtime     time.Time
	stagedOwner     string
	stagedGroup     string

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
		panel:        panel,
		settings:     settings,
		colorSchemes: colorSchemes,
		theme:        theme,
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
	r.menu.SetBorderPadding(0, 0, 1, 1)                   // 1-char left/right padding; no border needed for this
	r.menu.AddItem("Properties", "", 0, r.openProperties) // first and default-selected
	r.menu.AddItem("Edit", "", 0, r.editCurrentEntry)
	r.menu.AddItem("Rename", "", 0, r.openRename)
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

	// The bottom bar (see newBottomBar): bashLine/statusBar must exist
	// before onLoad can be wired below, and before mainLayout is built.
	r.newBottomBar()

	// Panel's disk-usage display depends on whichever directory it's
	// currently showing — refreshed on every navigation from here on
	// (onLoad isn't called for the very first load, which already
	// happened inside NewPanel above, before there was anything to wire
	// it to — refreshStatusBar is called once explicitly, right after
	// AddPage below, to cover that one case).
	panel.onLoad = func(string) { r.refreshStatusBar() }

	r.quitConfirm = tview.NewList().ShowSecondaryText(false)
	r.quitConfirm.SetHighlightFullLine(true)
	r.quitConfirm.SetBorderPadding(0, 0, 1, 1)
	r.quitConfirm.AddItem("Quit breakthrough", "", 0, r.confirmQuit)
	r.quitConfirm.AddItem("Cancel", "", 0, r.cancelQuit)
	r.quitConfirm.SetDoneFunc(r.cancelQuit) // Escape

	// The owner/group picker (see openOwnerGroupPicker) — one shared List,
	// repopulated and repositioned per open, the same pattern rename/
	// prompt/propertiesEditField already use.
	r.picker = tview.NewList().ShowSecondaryText(false)
	r.picker.SetHighlightFullLine(true)
	r.picker.SetBorderPadding(0, 0, 1, 1)

	// The Settings overlay (see openSettings) — same "one shared,
	// repopulated List" pattern as r.picker above.
	r.settingsList = r.newSettingsList()

	// The search dialog (see openSearch).
	r.searchPages = r.newSearchDialog()

	// mainLayout stacks the panel above the two new bottom rows — panel
	// gets the lion's share (0, 1: no fixed size, proportion 1, i.e. all
	// remaining space) and real focus by default (see NewFlex/AddItem's
	// own "focus" parameter); bashLine/statusBar are each pinned to
	// exactly one row (1, 0) and never auto-focused — reaching bashLine
	// is a deliberate click, not something Tab should stumble into.
	r.mainLayout = tview.NewFlex().SetDirection(tview.FlexRow)
	r.mainLayout.AddItem(panel, 0, 1, true)
	r.mainLayout.AddItem(r.bashLine, 1, 0, false)
	r.mainLayout.AddItem(r.statusBar, 1, 0, false)

	r.AddPage(panelPage, r.mainLayout, true, true)
	r.AddPage(contextMenuPage, r.menu, false, false)
	r.AddPage(renamePage, r.rename, false, false)
	r.AddPage(promptPage, r.prompt, false, false)
	r.AddPage(propertiesPage, r.properties, false, false)
	r.AddPage(pickerPage, r.picker, false, false)
	r.AddPage(errorPage, r.errorView, false, false)
	r.AddPage(quitConfirmPage, r.quitConfirm, false, false)
	r.AddPage(settingsPage, r.settingsList, false, false)
	r.AddPage(searchPage, r.searchPages, false, false)

	panel.SetMouseCapture(r.captureMouse)
	r.SetMouseCapture(r.captureOutsideClick)

	r.applyTheme(theme)  // paints every widget constructed above in one place — see applyTheme's own doc comment
	r.refreshStatusBar() // initial sync — see the onLoad comment above

	if len(configWarnings) > 0 {
		r.showError(fmt.Errorf("config: %s", strings.Join(configWarnings, "; ")))
	}

	return r, nil
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
// already open, without closing it — see openOwnerGroupPicker, the only
// current use: the owner/group picker floats on top of Properties rather
// than replacing it.
func (r *Root) pushOverlay(page string, widget tview.Primitive, restore func()) {
	r.overlayStack = append(r.overlayStack, overlayFrame{page: page, widget: widget, restore: restore})
	r.activePage = page
	r.activeWidget = widget
	r.ShowPage(page)
	if restore != nil {
		restore()
	} else {
		r.app.SetFocus(widget)
	}
}

// hideOverlay closes just the topmost overlay layer, if any, revealing
// whatever was underneath it — restoring that layer's own focus (see
// overlayFrame.restore) — or, if that was the only one open, returning
// focus to the panel.
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
		r.app.SetFocus(r.panel)
		return
	}

	below := r.overlayStack[len(r.overlayStack)-1]
	r.activePage = below.page
	r.activeWidget = below.widget
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
// a second click to do anything else. Scrolling is swallowed outright
// while an overlay is open — letting it through would scroll the list out
// from under a menu that stays put, which both looks wrong and would
// leave targetRow (see openRename) pointing at a different file than the
// one the menu was opened for.
//
// The Properties overlay is the one exception to "click outside closes
// it": once propertiesDirty is true (see markPropertiesDirty), an
// outside click is consumed and otherwise ignored instead — Cancel or
// Save is the only way out from there, so an in-progress edit (a
// permission bit already toggled, a name half-typed) can't be silently
// discarded, or just as silently lost track of, by a stray click.
func (r *Root) captureOutsideClick(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if r.activePage == "" {
		return action, event // nothing open, nothing to do
	}

	x, y := event.Position()
	if primitiveContains(r.activeWidget, x, y) {
		return action, event // event landed on the open overlay itself
	}

	if r.activePage == propertiesPage && r.propertiesDirty {
		return tview.MouseConsumed, nil
	}

	switch action {
	case tview.MouseLeftClick, tview.MouseRightClick:
		r.hideOverlay()
		return tview.MouseConsumed, nil
	case tview.MouseScrollUp, tview.MouseScrollDown, tview.MouseScrollLeft, tview.MouseScrollRight:
		return tview.MouseConsumed, nil
	default:
		return action, event
	}
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
	x, y, width, height = r.clampToPanel(x, y, width, height)

	r.menu.SetRect(x, y, width, height)
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
func mtimeFormatToggleLabel(mtimeUnix bool) string {
	if mtimeUnix {
		return "Show mtime formatted"
	}
	return "Show mtime as timestamp"
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

	if _, err := fsops.Rename(r.target, newName); err != nil {
		r.showError(err)
		return
	}
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

// pasteClipboard is "Paste": copies or moves (per clipboardCut) whatever
// Copy/Cut last captured into the directory currently on screen. A no-op
// if nothing was ever copied/cut.
//
// Each target that would collide with an existing entry in the current
// directory is skipped with an error — asking "overwrite?" once per
// colliding file in a multi-file paste isn't built yet (a known
// simplification; fsops.Copy/Move's force parameter is where that would
// hook in). Only the first error is reported, to avoid stacking one error
// overlay per failed file; the rest of the paste still runs to
// completion rather than stopping at the first collision.
func (r *Root) pasteClipboard() {
	if len(r.clipboard) == 0 {
		return
	}

	var firstErr error
	for _, src := range r.clipboard {
		dst := filepath.Join(r.panel.path, filepath.Base(src))
		var err error
		if r.clipboardCut {
			err = fsops.Move(src, dst, false)
		} else {
			err = fsops.Copy(src, dst, false)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if err := r.panel.load(r.panel.path); err != nil {
		firstErr = err // the reload failing is more urgent to report than a copy conflict
	} else if r.clipboardCut && firstErr == nil {
		r.clipboard = nil // moved away cleanly; nothing left to paste again
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

// openChmod is the context menu's "chmod": prompts for an octal
// permission string (e.g. "755") and applies it to the target.
func (r *Root) openChmod() {
	target := r.target
	r.openPrompt("chmod (octal, e.g. 755):", "", func(text string) {
		mode, err := fsops.ParseMode(text)
		if err != nil {
			r.showError(err)
			return
		}
		if err := fsops.Chmod(target, mode); err != nil {
			r.showError(err)
			return
		}
		r.showError(r.panel.load(r.panel.path))
	})
}
