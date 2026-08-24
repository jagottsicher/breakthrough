package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/search"
)

const searchPage = "search"

// searchFormSize/searchResultsSize are the search dialog's own two
// sizes — one for the form (see newSearchDialog), a noticeably bigger
// one for the results window (see runSearch/backToSearchForm), per the
// user's own request: MC's own results window "darf auch gerne etwas
// größer sein" than the input form that opens it.
const (
	searchFormWidth, searchFormHeight       = 70, 21
	searchResultsWidth, searchResultsHeight = 96, 30
)

// searchModeOptions are the Mode dropdown's own labels, in the same
// order as search.Mode's own constants (ModeGlob, ModeKeyword,
// ModeRegex) — always the full, fixed list, unlike Engine/Content
// below, so search.Mode(index) is a safe, direct cast.
var searchModeOptions = []string{"Glob", "Keyword", "Regex"}

// searchEngineOption/searchContentOption pair one Engine/Search-in
// dropdown label with the search.Engine/search.ContentMode it actually
// means — needed because, unlike Mode, these two dropdowns' own option
// lists are built conditionally (see buildSearchEngineOptions/
// buildSearchContentOptions): once an option in the middle is left out
// (e.g. no "locate" because search.LocateAvailable() is false), the
// dropdown's own selected index no longer lines up with the
// corresponding enum's own numeric value, so it has to be looked up
// through this pairing instead of cast directly.
type searchEngineOption struct {
	label  string
	engine search.Engine
}

type searchContentOption struct {
	label string
	mode  search.ContentMode
}

// buildSearchEngineOptions lists find (always) and locate (only where
// search.LocateAvailable() — see its own doc comment on why that's not
// guaranteed even when the binary itself is present, e.g. an unbuilt
// database on macOS).
func buildSearchEngineOptions() []searchEngineOption {
	opts := []searchEngineOption{{"find", search.EngineFind}}
	if search.LocateAvailable() {
		opts = append(opts, searchEngineOption{"locate", search.EngineLocate})
	}
	return opts
}

// buildSearchContentOptions lists file names (always, and always
// first — see its own callers, which rely on index 0 meaning
// ContentNone), grep (always — every platform this app targets has
// grep), and zgrep/zipgrep only where search.ZgrepAvailable()/
// ZipgrepAvailable() report the binary actually exists.
func buildSearchContentOptions() []searchContentOption {
	opts := []searchContentOption{
		{"File names", search.ContentNone},
		{"File contents (grep)", search.ContentGrep},
	}
	if search.ZgrepAvailable() {
		opts = append(opts, searchContentOption{"gzip contents (zgrep)", search.ContentGzip})
	}
	if search.ZipgrepAvailable() {
		opts = append(opts, searchContentOption{"zip contents (zipgrep)", search.ContentZip})
	}
	return opts
}

func searchOptionLabels[T any](opts []T, label func(T) string) []string {
	labels := make([]string, len(opts))
	for i, o := range opts {
		labels[i] = label(o)
	}
	return labels
}

// newSearchDialog builds the search overlay, laid out after MC's own
// Find File dialog per the user's own request — with one addition MC
// doesn't have (a find/locate Engine choice up top) and one
// simplification tview.Form's own layout model doesn't make practical
// to avoid: MC puts its "Tree" browse button directly beside the
// Start-at field; Form always collects every button into one row at
// the very bottom instead, so Tree lives there alongside Start
// Search/Cancel rather than inline.
//
// Field order: Engine, Start at (Tab-completes — see
// captureSearchScopeKey), Filename + Mode, Ignored dirs, Search in +
// Content. Content starts disabled (see searchContentChanged) — the
// user's own request that a field "not yet available" reads as
// visibly grayed rather than just quietly ignored — enabled once
// Search in picks anything other than "File names".
//
// searchForm (the fields) and searchResultsView (the results list —
// see runSearch) are two pages of one Pages, the same "several sub-
// widgets, one overlay, switched between via Pages" shape
// newPropertiesView already uses, for the same reason: Escape means
// different things on each (see backToSearchForm/closeSearch), which
// needs them individually addressable rather than replaced wholesale.
func (r *Root) newSearchDialog() *tview.Pages {
	r.searchEngineOptions = buildSearchEngineOptions()
	r.searchContentOptions = buildSearchContentOptions()

	r.searchForm = tview.NewForm()
	r.searchForm.SetCancelFunc(r.closeSearch) // Escape while a form field has focus

	r.searchForm.AddDropDown("Engine", searchOptionLabels(r.searchEngineOptions, func(o searchEngineOption) string { return o.label }), 0, nil)

	// A plain tview.InputField, added via AddFormItem rather than the
	// Form's own AddInputField convenience method — that one doesn't
	// expose SetInputCapture, which captureSearchScopeKey needs for
	// Tab-completion (see Panel.headerEdit/completePath for the same
	// pattern this reuses directly).
	r.searchScopeField = tview.NewInputField()
	r.searchScopeField.SetLabel("Start at")
	r.searchScopeField.SetInputCapture(r.captureSearchScopeKey)
	r.searchForm.AddFormItem(r.searchScopeField)

	r.searchForm.AddInputField("Filename", "", 40, nil, nil)
	r.searchForm.AddDropDown("Mode", searchModeOptions, 0, nil)
	r.searchForm.AddInputField("Ignored dirs", "", 40, nil, nil)

	// Created before the "Search in" dropdown below, not after: Form's
	// own AddDropDown applies initialOption immediately, which fires
	// the selected callback (searchContentChanged) right there during
	// construction — searchContentField must already exist by then, or
	// that first call panics on a nil field.
	r.searchContentField = tview.NewInputField()
	r.searchContentField.SetLabel("Content")

	r.searchForm.AddDropDown("Search in", searchOptionLabels(r.searchContentOptions, func(o searchContentOption) string { return o.label }), 0, r.searchContentChanged)
	r.searchForm.AddFormItem(r.searchContentField)

	r.searchForm.AddButton("Tree", r.openSearchTreePicker)
	r.searchForm.AddButton("Start Search", r.runSearch)
	r.searchForm.AddButton("Cancel", r.closeSearch)
	r.searchForm.SetMouseCapture(r.searchFormMouseCapture)

	r.searchList = tview.NewList().ShowSecondaryText(false)
	r.searchList.SetHighlightFullLine(true)
	r.searchList.SetBorderPadding(0, 0, 1, 1)
	r.searchList.SetDoneFunc(r.backToSearchForm) // Escape while the results list has focus

	// searchStatus is the results window's own bottom line: an
	// animated "still working" indicator naming the directory of the
	// most recently found match as a stand-in for "currently scanning"
	// (see streamSearchResults/animateSearchProgress) — the closest
	// approximation available without breakthrough doing its own
	// directory traversal instead of shelling out to find/locate/grep,
	// which don't report that kind of progress themselves. Once the
	// search finishes, this shows a final "Done — N found" instead.
	r.searchStatus = tview.NewTextView().SetDynamicColors(true)
	r.searchStatus.SetBorderPadding(0, 0, 1, 1)

	r.searchResultsView = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.searchList, 0, 1, true).
		AddItem(r.searchStatus, 1, 0, false)

	pages := tview.NewPages()
	pages.AddPage("form", r.searchForm, true, true)
	pages.AddPage("results", r.searchResultsView, true, false)
	return pages
}

// searchContentChanged is the Search in dropdown's own selected
// callback: enables searchContentField once anything other than "File
// names" (ContentNone, always index 0 — see buildSearchContentOptions'
// own doc comment) is picked, disables it again if the choice moves
// back to "File names" — see newSearchDialog's own doc comment on why
// that field starts out grayed.
func (r *Root) searchContentChanged(_ string, index int) {
	r.searchContentField.SetDisabled(r.searchContentOptions[index].mode == search.ContentNone)
}

// openSearchTreePicker is the Tree button's own action: opens the
// directory picker (see openDirPicker) seeded at whatever Start-at
// currently holds, writing the chosen directory back into it on
// Select. Left untouched on Cancel.
func (r *Root) openSearchTreePicker() {
	r.openDirPicker(r.searchScopeField.GetText(), func(path string) {
		r.searchScopeField.SetText(path)
	}, nil)
}

// openSearch shows the search dialog, centered on screen, on its own
// "form" page, sized for the form (see searchFormWidth/Height —
// runSearch resizes up to searchResultsWidth/Height once it switches
// to the results page). Filename/Content always start empty; Start at
// always resets to wherever the panel currently is (the far more
// common starting point than whatever was typed the last time the
// dialog was open); Engine/Mode/Ignored dirs/Search in are left
// exactly as they were, since there's no similarly obvious reason to
// reset those every time.
func (r *Root) openSearch() {
	if item := r.searchForm.GetFormItemByLabel("Filename"); item != nil {
		item.(*tview.InputField).SetText("")
	}
	r.searchContentField.SetText("")
	r.searchScopeField.SetText(r.panel.path)
	r.searchPages.SwitchToPage("form")

	r.resizeSearchPages(searchFormWidth, searchFormHeight)
	r.showOverlay(searchPage, r.searchPages)
}

// resizeSearchPages centers a width x height rect on screen (clamped
// to the panel — see clampToPanel) and applies it to r.searchPages —
// shared by openSearch (the form's own smaller size) and runSearch/
// backToSearchForm (the results window's own bigger one), so the
// dialog's footprint always matches whichever of its two pages is
// currently showing rather than staying pinned to the form's size.
func (r *Root) resizeSearchPages(width, height int) {
	x, y := r.centeredOnScreen(width, height)
	x, y, w, h := r.clampToPanel(x, y, width, height)
	r.searchPages.SetRect(x, y, w, h)
}

// closeSearch cancels any in-flight search (see cancelSearch) and
// closes the dialog entirely — Escape from the form page, or picking a
// result (see openSearchResult).
func (r *Root) closeSearch() {
	r.cancelSearch()
	r.hideOverlay()
}

// backToSearchForm cancels any in-flight search and returns to the
// form page (back to the smaller of the dialog's two sizes — see
// resizeSearchPages) without closing the dialog — Escape from the
// results page, so refining a search that came back wrong (or empty)
// doesn't need reopening the whole dialog from scratch.
func (r *Root) backToSearchForm() {
	r.cancelSearch()
	r.searchPages.SwitchToPage("form")
	r.resizeSearchPages(searchFormWidth, searchFormHeight)
	r.app.SetFocus(r.searchForm)
}

// cancelSearch stops whatever search.Run call is currently in flight,
// if any (see runSearch/streamSearchResults), and its paired
// animateSearchProgress ticker (both share the same ctx/searchCancel)
// — so a slow "find /" started by an earlier search never keeps
// working, or racing a newer one's own results into the same list,
// once the user has moved on from it.
func (r *Root) cancelSearch() {
	if r.searchCancel != nil {
		r.searchCancel()
		r.searchCancel = nil
	}
}

// captureSearchScopeKey adds bash-style Tab completion to the Start-at
// field — the same longest-common-prefix completion Panel's own path
// header already has (see Panel.completePath), reusing its own
// completions/resolvePath directly rather than reimplementing it, per
// the user's own request that every dialog with a path field support
// it the same way.
func (r *Root) captureSearchScopeKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() != tcell.KeyTab {
		return event
	}
	matches := r.panel.completions(r.searchScopeField.GetText())
	if len(matches) == 0 {
		return nil
	}
	r.searchScopeField.SetText(longestCommonPrefix(matches))
	return nil
}

// searchFormMouseCapture plugs a gap in tview.Form's own default mouse
// handling: Form's own fallback for "the click landed on blank space,
// not any specific field or button" (see its own MouseHandler) only
// covers MouseLeftDown, not the MouseLeftClick tview derives right
// after for an ordinary click — DropDown/InputField/Button all rely on
// getting that MouseLeftClick themselves, so Form can't unconditionally
// swallow it the way it does for Down. Left as-is, a click on any of
// the Form's own blank space (there's plenty in a vertical form with
// only a handful of fields) falls all the way through Pages' own
// z-order dispatch (see its MouseHandler) to whatever's underneath:
// the panel, which treats it as an ordinary click on whatever row
// happens to be at those coordinates — including navigating into a
// directory. That's the user's own report: a dialog that looks and
// otherwise behaves like a solid overlay, but clicking "past" its
// fields reaches through to the file list behind it.
//
// The fix: swallow MouseLeftClick ourselves whenever it lands inside
// the form's own outer rect but outside every individual item/button's
// rect — i.e. exactly the gap Form's own Down-only fallback leaves
// open. Clicks that DO land on an item or button are returned
// untouched, so DropDown/InputField/Button's own MouseHandler still
// gets them exactly as it always would.
func (r *Root) searchFormMouseCapture(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftClick {
		return action, event
	}
	x, y := event.Position()
	if !r.searchForm.InRect(x, y) {
		return action, event
	}
	for i := 0; i < r.searchForm.GetFormItemCount(); i++ {
		if primitiveContains(r.searchForm.GetFormItem(i), x, y) {
			return action, event
		}
	}
	for i := 0; i < r.searchForm.GetButtonCount(); i++ {
		if primitiveContains(r.searchForm.GetButton(i), x, y) {
			return action, event
		}
	}
	return tview.MouseConsumed, nil
}

// parseIgnoreDirs splits the Ignored dirs field's comma-separated text
// into individual directory names (see search.Request.IgnoreDirs' own
// doc comment on how each is matched) — surrounding whitespace trimmed,
// empty entries (a trailing comma, or the field left blank) dropped.
func parseIgnoreDirs(text string) []string {
	var dirs []string
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			dirs = append(dirs, part)
		}
	}
	return dirs
}

// runSearch is the form's own "Start Search" button/Enter action:
// builds a search.Request from the form's current fields — Filename's
// own text if Search in is still "File names" (ContentNone), or
// Content's otherwise (see searchContentChanged) — cancels whatever
// search was previously running (see cancelSearch), and starts the new
// one on the bigger results page (see resizeSearchPages), streaming
// its matches in as they're found (see streamSearchResults) alongside
// an animated "still working" status line (see animateSearchProgress).
func (r *Root) runSearch() {
	contentIdx, _ := r.searchForm.GetFormItemByLabel("Search in").(*tview.DropDown).GetCurrentOption()
	contentMode := r.searchContentOptions[contentIdx].mode

	var pattern string
	if contentMode == search.ContentNone {
		pattern = r.searchForm.GetFormItemByLabel("Filename").(*tview.InputField).GetText()
	} else {
		pattern = r.searchContentField.GetText()
	}
	if pattern == "" {
		return
	}
	scope := r.panel.resolvePath(r.searchScopeField.GetText())
	ignoreDirs := parseIgnoreDirs(r.searchForm.GetFormItemByLabel("Ignored dirs").(*tview.InputField).GetText())

	modeIdx, _ := r.searchForm.GetFormItemByLabel("Mode").(*tview.DropDown).GetCurrentOption()
	engineIdx, _ := r.searchForm.GetFormItemByLabel("Engine").(*tview.DropDown).GetCurrentOption()

	req := search.Request{
		Pattern:    pattern,
		Scope:      scope,
		Mode:       search.Mode(modeIdx),
		Engine:     r.searchEngineOptions[engineIdx].engine,
		Content:    contentMode,
		IgnoreDirs: ignoreDirs,
	}

	r.cancelSearch()
	ctx, cancel := context.WithCancel(context.Background())
	r.searchCancel = cancel

	r.searchList.Clear()
	r.searchAnimFrame = 0
	r.searchLastDir = ""
	r.searchStartDir = scope
	r.renderSearchStatus()
	r.searchPages.SwitchToPage("results")
	r.resizeSearchPages(searchResultsWidth, searchResultsHeight)
	r.app.SetFocus(r.searchList)

	go r.animateSearchProgress(ctx)
	results, errs := searchRun(ctx, req)
	go r.streamSearchResults(ctx, req, results, errs)
}

// searchRun is search.Run — a package-level var, not called directly,
// so a test can override it (see search_test.go) and verify runSearch's
// own request-building without depending on real find/grep/locate
// being on $PATH, and without a background goroutine ever reaching
// r.app.QueueUpdateDraw with nothing running the event loop to drain
// it (see StartClock's own doc comment for the same concern this
// avoids the same way loadInitialSettings/userConfigFilePath already
// do for config I/O).
var searchRun = search.Run

// animateSearchProgress advances searchAnimFrame every
// hashAnimationInterval until ctx is done (see runSearch/cancelSearch)
// — the same ticker-driven "in progress" animation computeHashes' own
// animateHashProgress already uses, reusing hashAnimationFrames
// directly rather than a second, separately-defined set of frames.
func (r *Root) animateSearchProgress(ctx context.Context) {
	ticker := time.NewTicker(hashAnimationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			r.app.QueueUpdateDraw(func() {
				if ctx.Err() != nil {
					return
				}
				r.searchAnimFrame++
				r.renderSearchStatus()
			})
		case <-ctx.Done():
			return
		}
	}
}

// renderSearchStatus paints searchStatus' own text: the current
// animation frame plus whatever directory streamSearchResults last saw
// a match in (searchLastDir), falling back to Start at's own value
// (searchStartDir) until the first result arrives.
func (r *Root) renderSearchStatus() {
	frame := hashAnimationFrames[r.searchAnimFrame%len(hashAnimationFrames)]
	dir := r.searchLastDir
	if dir == "" {
		dir = r.searchStartDir
	}
	r.searchStatus.SetText(frame + " " + dir)
}

// noSearchResultsText is what the results list shows when a search
// finished without a single match — see streamSearchResults' own doc
// comment on why that needs to be an explicit, visible state rather
// than just leaving the list empty. For a locate-engine filename
// search specifically, it also names the single most likely reason:
// locate answers entirely from its own prebuilt index (updatedb),
// which — unlike a live find/grep — can be hours or days stale and
// simply doesn't know about a file created (or renamed, or deleted)
// since the last run. Combined with Scope being applied client-side
// (see LocateArgs' own doc comment on why locate has no directory-scope
// argument of its own to give it directly), a locate search from deep
// in a project directory routinely comes back with everything filtered
// out or missing outright — a silently-empty list gave no hint that
// the search had genuinely run, let alone why it came back empty.
func noSearchResultsText(req search.Request) string {
	if req.Engine == search.EngineLocate && req.Content == search.ContentNone {
		return "No matches found (locate's own index may be stale — see Engine: find for a live search instead)"
	}
	return "No matches found"
}

// streamSearchResults drains results/errs (see search.Run) on a
// background goroutine, appending each match to searchList via
// QueueUpdateDraw — the same "background work, draw updates queued
// onto the UI goroutine" shape StartClock's own ticker already uses.
// Every queued update first checks ctx.Err(): once a newer search has
// cancelled this one (see cancelSearch/runSearch), any of this
// goroutine's own updates still sitting in the queue skip themselves
// instead of appending stale results (or a stale error) on top of the
// new search's own, already-cleared list. Each arriving result also
// updates searchLastDir — see renderSearchStatus.
//
// If the search finishes with zero matches and no error, one
// noSearchResultsText(req) item is added instead of leaving the list
// looking exactly like it did before the search ran — easily read as
// "the search didn't do anything" (a real user report). Either way,
// the status line's own animation stops and settles on a final
// "Done — N found" once the search is over.
func (r *Root) streamSearchResults(ctx context.Context, req search.Request, results <-chan search.Result, errs <-chan error) {
	count := 0
	for res := range results {
		res := res // captured per-iteration, not the shared loop variable
		count++
		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			r.searchLastDir = filepath.Dir(res.Path)
			r.searchList.AddItem(formatSearchResult(res), "", 0, func() {
				r.openSearchResult(res)
			})
		})
	}
	if err := <-errs; err != nil {
		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			r.showError(err)
			// See below: animateSearchProgress's own ticker otherwise
			// keeps running (and overwriting whatever's set here) until
			// something else eventually cancels it.
			r.cancelSearch()
		})
		return
	}
	if count == 0 {
		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			r.searchList.AddItem(noSearchResultsText(req), "", 0, nil)
		})
	}
	r.app.QueueUpdateDraw(func() {
		if ctx.Err() != nil {
			return
		}
		r.searchStatus.SetText(fmt.Sprintf("Done — %d found", count))
		// The search itself is over, but nothing else would ever stop
		// animateSearchProgress's own ticker on its own — cancelSearch
		// is otherwise only called by a *newer* search starting, or the
		// dialog closing, both of which are still in the future here.
		// Left running, it would silently overwrite the "Done" text set
		// just above with the next animation frame within
		// hashAnimationInterval.
		r.cancelSearch()
	})
}

// formatSearchResult renders one search.Result as a searchList item:
// just the path for a filename match (Line == 0), or
// "path:line: text" for a content match.
func formatSearchResult(res search.Result) string {
	if res.Line == 0 {
		return res.Path
	}
	return fmt.Sprintf("%s:%d: %s", res.Path, res.Line, res.Text)
}

// openSearchResult is what picking a result does: closes the dialog
// and jumps the panel straight to it (see Panel.navigateAndSelect) —
// for both a filename and a content match, since neither this app nor
// the panel itself can yet open a file to a specific line (a job for
// the "View" action instead).
func (r *Root) openSearchResult(res search.Result) {
	r.closeSearch()
	r.showError(r.panel.navigateAndSelect(res.Path))
}
