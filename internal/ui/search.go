package ui

import (
	"context"
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/search"
)

const searchPage = "search"

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

// buildSearchContentOptions lists file names (always), grep (always —
// every platform this app targets has grep), and zgrep/zipgrep only
// where search.ZgrepAvailable()/ZipgrepAvailable() report the binary
// actually exists.
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

// newSearchDialog builds the search overlay: searchForm (pattern,
// scope, mode, engine, what-to-search) as one page, searchList (the
// results, once a search has run) as another — the same "several sub-
// widgets, one overlay, switched between via Pages" shape
// newPropertiesView already uses, for the same reason: Escape means
// different things on each (see backToSearchForm/closeSearch), which
// needs them individually addressable rather than replaced wholesale.
func (r *Root) newSearchDialog() *tview.Pages {
	r.searchEngineOptions = buildSearchEngineOptions()
	r.searchContentOptions = buildSearchContentOptions()

	r.searchForm = tview.NewForm()
	r.searchForm.SetCancelFunc(r.closeSearch) // Escape while a form field has focus

	r.searchForm.AddInputField("Pattern", "", 40, nil, nil)

	// A plain tview.InputField, added via AddFormItem rather than the
	// Form's own AddInputField convenience method — that one doesn't
	// expose SetInputCapture, which captureSearchScopeKey needs for
	// Tab-completion (see Panel.headerEdit/completePath for the same
	// pattern this reuses directly).
	r.searchScopeField = tview.NewInputField()
	r.searchScopeField.SetLabel("Scope")
	r.searchScopeField.SetInputCapture(r.captureSearchScopeKey)
	r.searchForm.AddFormItem(r.searchScopeField)

	r.searchForm.AddDropDown("Mode", searchModeOptions, 0, nil)
	r.searchForm.AddDropDown("Engine", searchOptionLabels(r.searchEngineOptions, func(o searchEngineOption) string { return o.label }), 0, nil)
	r.searchForm.AddDropDown("Search in", searchOptionLabels(r.searchContentOptions, func(o searchContentOption) string { return o.label }), 0, nil)
	r.searchForm.AddButton("Search", r.runSearch)

	r.searchList = tview.NewList().ShowSecondaryText(false)
	r.searchList.SetHighlightFullLine(true)
	r.searchList.SetBorderPadding(0, 0, 1, 1)
	r.searchList.SetDoneFunc(r.backToSearchForm) // Escape while the results list has focus

	pages := tview.NewPages()
	pages.AddPage("form", r.searchForm, true, true)
	pages.AddPage("results", r.searchList, true, false)
	return pages
}

// openSearch shows the search dialog, centered on screen, on its own
// "form" page — Pattern always starts empty; Scope always resets to
// wherever the panel currently is (the far more common starting point
// than whatever was typed the last time the dialog was open); Mode/
// Engine/Search-in are left exactly as they were, since there's no
// similarly obvious reason to reset those every time.
func (r *Root) openSearch() {
	if item := r.searchForm.GetFormItemByLabel("Pattern"); item != nil {
		item.(*tview.InputField).SetText("")
	}
	r.searchScopeField.SetText(r.panel.path)
	r.searchPages.SwitchToPage("form")

	const width, height = 60, 13
	x, y := r.centeredOnScreen(width, height)
	x, y, w, h := r.clampToPanel(x, y, width, height)
	r.searchPages.SetRect(x, y, w, h)

	r.showOverlay(searchPage, r.searchPages)
}

// closeSearch cancels any in-flight search (see cancelSearch) and
// closes the dialog entirely — Escape from the form page, or picking a
// result (see openSearchResult).
func (r *Root) closeSearch() {
	r.cancelSearch()
	r.hideOverlay()
}

// backToSearchForm cancels any in-flight search and returns to the
// form page without closing the dialog — Escape from the results page,
// so refining a search that came back wrong (or empty) doesn't need
// reopening the whole dialog from scratch.
func (r *Root) backToSearchForm() {
	r.cancelSearch()
	r.searchPages.SwitchToPage("form")
	r.app.SetFocus(r.searchForm)
}

// cancelSearch stops whatever search.Run call is currently in flight,
// if any (see runSearch/streamSearchResults) — so a slow "find /"
// started by an earlier search never keeps working, or racing a newer
// one's own results into the same list, once the user has moved on
// from it.
func (r *Root) cancelSearch() {
	if r.searchCancel != nil {
		r.searchCancel()
		r.searchCancel = nil
	}
}

// captureSearchScopeKey adds bash-style Tab completion to the Scope
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

// runSearch is the form's own "Search" button/Enter action: builds a
// search.Request from the form's current fields, cancels whatever
// search was previously running (see cancelSearch), and starts the new
// one, switching to the results page to stream its matches in as
// they're found (see streamSearchResults).
func (r *Root) runSearch() {
	pattern := r.searchForm.GetFormItemByLabel("Pattern").(*tview.InputField).GetText()
	if pattern == "" {
		return
	}
	scope := r.panel.resolvePath(r.searchScopeField.GetText())

	modeIdx, _ := r.searchForm.GetFormItemByLabel("Mode").(*tview.DropDown).GetCurrentOption()
	engineIdx, _ := r.searchForm.GetFormItemByLabel("Engine").(*tview.DropDown).GetCurrentOption()
	contentIdx, _ := r.searchForm.GetFormItemByLabel("Search in").(*tview.DropDown).GetCurrentOption()

	req := search.Request{
		Pattern: pattern,
		Scope:   scope,
		Mode:    search.Mode(modeIdx),
		Engine:  r.searchEngineOptions[engineIdx].engine,
		Content: r.searchContentOptions[contentIdx].mode,
	}

	r.cancelSearch()
	ctx, cancel := context.WithCancel(context.Background())
	r.searchCancel = cancel

	r.searchList.Clear()
	r.searchPages.SwitchToPage("results")
	r.app.SetFocus(r.searchList)

	results, errs := searchRun(ctx, req)
	go r.streamSearchResults(ctx, results, errs)
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

// streamSearchResults drains results/errs (see search.Run) on a
// background goroutine, appending each match to searchList via
// QueueUpdateDraw — the same "background work, draw updates queued
// onto the UI goroutine" shape StartClock's own ticker already uses.
// Every queued update first checks ctx.Err(): once a newer search has
// cancelled this one (see cancelSearch/runSearch), any of this
// goroutine's own updates still sitting in the queue skip themselves
// instead of appending stale results (or a stale error) on top of the
// new search's own, already-cleared list.
func (r *Root) streamSearchResults(ctx context.Context, results <-chan search.Result, errs <-chan error) {
	for res := range results {
		res := res // captured per-iteration, not the shared loop variable
		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
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
		})
	}
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
