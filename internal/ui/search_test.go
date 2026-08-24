package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/search"
)

// fakeSearchRun returns a searchRun replacement that records the
// Request it was given in *captured — the channels it returns are
// deliberately never closed. streamSearchResults' own background
// goroutine (spawned by runSearch) would otherwise run to completion
// and, seeing both channels close with nothing having arrived, reach
// r.app.QueueUpdateDraw to report "no matches" (see its own doc
// comment) — with nothing here running the event loop to drain that
// call, it would block forever (see isolateHashFile's own doc comment
// for the same concern elsewhere). Tests using this only ever care
// about the Request runSearch built and/or its own synchronous side
// effects (activePage, ctx cancellation) — never anything that depends
// on the search actually finishing — so leaving it perpetually
// in-flight is safe.
func fakeSearchRun(captured *search.Request) func(context.Context, search.Request) (<-chan search.Result, <-chan error) {
	return func(_ context.Context, req search.Request) (<-chan search.Result, <-chan error) {
		*captured = req
		return make(chan search.Result), make(chan error)
	}
}

// isolateSearchRun overrides searchRun for the duration of t, restoring
// the real search.Run afterward.
func isolateSearchRun(t *testing.T, fake func(context.Context, search.Request) (<-chan search.Result, <-chan error)) {
	t.Helper()
	original := searchRun
	searchRun = fake
	t.Cleanup(func() { searchRun = original })
}

func TestBuildSearchEngineOptionsAlwaysHasFindFirst(t *testing.T) {
	opts := buildSearchEngineOptions()
	if len(opts) == 0 || opts[0].label != "find" || opts[0].engine != search.EngineFind {
		t.Fatalf("buildSearchEngineOptions()[0] = %+v, want {label: \"find\", engine: EngineFind}", opts[0])
	}
	if len(opts) == 2 && opts[1].label != "locate" {
		t.Errorf("a second option should be \"locate\" when present, got %+v", opts[1])
	}
}

func TestBuildSearchContentOptionsAlwaysHasFileNamesAndGrep(t *testing.T) {
	opts := buildSearchContentOptions()
	if len(opts) < 2 {
		t.Fatalf("buildSearchContentOptions() = %+v, want at least 2 entries", opts)
	}
	if opts[0].label != "File names" || opts[0].mode != search.ContentNone {
		t.Errorf("opts[0] = %+v, want {label: \"File names\", mode: ContentNone}", opts[0])
	}
	if opts[1].label != "File contents (grep)" || opts[1].mode != search.ContentGrep {
		t.Errorf("opts[1] = %+v, want {label: \"File contents (grep)\", mode: ContentGrep}", opts[1])
	}
}

func TestFormatSearchResult(t *testing.T) {
	tests := []struct {
		name string
		res  search.Result
		want string
	}{
		{"filename match", search.Result{Path: "/home/jens/apple.txt"}, "/home/jens/apple.txt"},
		{"content match", search.Result{Path: "/home/jens/a.go", Line: 12, Text: "func main() {"}, "/home/jens/a.go:12: func main() {"},
	}
	for _, tt := range tests {
		if got := formatSearchResult(tt.res); got != tt.want {
			t.Errorf("%s: formatSearchResult(%+v) = %q, want %q", tt.name, tt.res, got, tt.want)
		}
	}
}

// TestNoSearchResultsTextMentionsStaleIndexOnlyForLocateFilenameSearch
// pins that the extra "locate's own index may be stale" hint (a real
// user report — see its own doc comment) only ever appears for the one
// combination it's actually relevant to: Engine == EngineLocate,
// Content == ContentNone. A locate-backed content search still goes
// through grep directly (see runContentSearch), so the hint would be
// misleading there.
func TestNoSearchResultsTextMentionsStaleIndexOnlyForLocateFilenameSearch(t *testing.T) {
	tests := []struct {
		name      string
		req       search.Request
		wantStale bool
	}{
		{"locate + filenames", search.Request{Engine: search.EngineLocate, Content: search.ContentNone}, true},
		{"locate + grep content", search.Request{Engine: search.EngineLocate, Content: search.ContentGrep}, false},
		{"find + filenames", search.Request{Engine: search.EngineFind, Content: search.ContentNone}, false},
	}
	for _, tt := range tests {
		got := noSearchResultsText(tt.req)
		if !strings.Contains(got, "No matches found") {
			t.Errorf("%s: noSearchResultsText(%+v) = %q, missing the base message", tt.name, tt.req, got)
		}
		gotStale := strings.Contains(got, "stale")
		if gotStale != tt.wantStale {
			t.Errorf("%s: noSearchResultsText(%+v) = %q, mentions staleness = %v, want %v", tt.name, tt.req, got, gotStale, tt.wantStale)
		}
	}
}

// TestSearchContentChangedTogglesContentFieldDisabled pins the user's
// own request: the Content field starts out unavailable (grayed) since
// Search in defaults to "File names" (ContentNone), and only accepts
// typed input once something else is picked. tview's own InputField
// exposes no public getter for its disabled state, so this proves it
// behaviorally instead: a keystroke dispatched through InputHandler is
// silently ignored while disabled, accepted once enabled.
func TestSearchContentChangedTogglesContentFieldDisabled(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()

	r.searchContentField.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone), func(tview.Primitive) {})
	if got := r.searchContentField.GetText(); got != "" {
		t.Errorf("Content field text = %q after a keystroke while disabled, want unchanged (empty)", got)
	}

	// Index 1 is "File contents (grep)" — see buildSearchContentOptions'
	// own doc comment on that fixed ordering.
	r.searchForm.GetFormItemByLabel("Search in").(*tview.DropDown).SetCurrentOption(1)

	r.searchContentField.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone), func(tview.Primitive) {})
	if got := r.searchContentField.GetText(); got != "x" {
		t.Errorf("Content field text = %q after a keystroke once enabled, want %q", got, "x")
	}

	// Switching back to "File names" should re-disable it.
	r.searchForm.GetFormItemByLabel("Search in").(*tview.DropDown).SetCurrentOption(0)
	r.searchContentField.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModNone), func(tview.Primitive) {})
	if got := r.searchContentField.GetText(); got != "x" {
		t.Errorf("Content field text = %q after a keystroke once re-disabled, want unchanged (%q)", got, "x")
	}
}

func TestParseIgnoreDirs(t *testing.T) {
	tests := []struct {
		text string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{".git", []string{".git"}},
		{".git, node_modules", []string{".git", "node_modules"}},
		{" .git ,, node_modules ,", []string{".git", "node_modules"}},
	}
	for _, tt := range tests {
		got := parseIgnoreDirs(tt.text)
		if len(got) != len(tt.want) {
			t.Errorf("parseIgnoreDirs(%q) = %v, want %v", tt.text, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("parseIgnoreDirs(%q) = %v, want %v", tt.text, got, tt.want)
				break
			}
		}
	}
}

func TestOpenSearchShowsFormPrefilledWithPanelScope(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openSearch()

	if r.activePage != searchPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, searchPage)
	}
	if got := r.searchForm.GetFormItemByLabel("Filename").(*tview.InputField).GetText(); got != "" {
		t.Errorf("Pattern = %q, want empty", got)
	}
	if got := r.searchScopeField.GetText(); got != dir {
		t.Errorf("Scope = %q, want the panel's own current directory %q", got, dir)
	}
}

func TestCloseSearchHidesOverlay(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()

	r.closeSearch()

	if r.activePage != "" {
		t.Errorf("activePage = %q after closeSearch, want closed", r.activePage)
	}
}

// TestBackToSearchFormReturnsToFormWithoutClosing pins Escape's own
// two-stage behavior: from the results page, it returns to the form
// (see backToSearchForm) rather than closing the whole dialog the way
// Escape from the form page itself does (see closeSearch).
func TestBackToSearchFormReturnsToFormWithoutClosing(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchForm.GetFormItemByLabel("Filename").(*tview.InputField).SetText("anything")
	r.runSearch() // switches to the results page

	r.backToSearchForm()

	if r.activePage != searchPage {
		t.Errorf("activePage = %q, want still open (%q)", r.activePage, searchPage)
	}
	if !r.searchForm.HasFocus() {
		t.Error("backToSearchForm should have moved focus back to the form")
	}
}

// TestCaptureSearchScopeKeyCompletesPath pins Tab-completion in the
// Scope field, reusing Panel.completions/longestCommonPrefix directly
// — the same behavior Panel's own path header already has, per the
// user's own request that every dialog with a path field support it.
func TestCaptureSearchScopeKeyCompletesPath(t *testing.T) {
	dir := fixtureDir(t) // apple.txt, apricot.txt, banana.txt, app-data/
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()

	r.searchScopeField.SetText(dir + "/ap")
	r.captureSearchScopeKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))

	// "ap" is common to apple.txt, apricot.txt, and app-data/ — their
	// longest shared prefix is "ap" itself, same as
	// TestCompletions/TestLongestCommonPrefix already pin at the Panel
	// level; this just pins that the search dialog's own Scope field
	// reaches the same completions call.
	want := dir + "/ap"
	if got := r.searchScopeField.GetText(); got != want {
		t.Errorf("Scope after Tab = %q, want %q", got, want)
	}
}

func TestRunSearchBuildsRequestFromFormFields(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchForm.GetFormItemByLabel("Filename").(*tview.InputField).SetText("*.go")
	r.searchScopeField.SetText("/tmp")
	r.searchForm.GetFormItemByLabel("Mode").(*tview.DropDown).SetCurrentOption(2) // Regex
	r.searchForm.GetFormItemByLabel("Ignored dirs").(*tview.InputField).SetText(".git, node_modules")

	r.runSearch()

	if captured.Pattern != "*.go" {
		t.Errorf("Pattern = %q, want %q", captured.Pattern, "*.go")
	}
	if captured.Scope != "/tmp" {
		t.Errorf("Scope = %q, want %q", captured.Scope, "/tmp")
	}
	if captured.Mode != search.ModeRegex {
		t.Errorf("Mode = %v, want ModeRegex", captured.Mode)
	}
	if captured.Engine != search.EngineFind {
		t.Errorf("Engine = %v, want EngineFind (the default selection)", captured.Engine)
	}
	if captured.Content != search.ContentNone {
		t.Errorf("Content = %v, want ContentNone (the default selection)", captured.Content)
	}
	wantIgnore := []string{".git", "node_modules"}
	if len(captured.IgnoreDirs) != len(wantIgnore) || captured.IgnoreDirs[0] != wantIgnore[0] || captured.IgnoreDirs[1] != wantIgnore[1] {
		t.Errorf("IgnoreDirs = %v, want %v", captured.IgnoreDirs, wantIgnore)
	}
	if r.activePage != searchPage {
		t.Errorf("activePage = %q, want still open (%q) on the results page", r.activePage, searchPage)
	}
}

// TestRunSearchUsesContentFieldWhenContentModeSelected pins the split
// between Filename and Content (see newSearchDialog's own doc comment
// on why there are now two separate pattern fields, not one reused for
// both): once Search in picks anything other than "File names",
// runSearch reads the pattern from Content, ignoring whatever Filename
// still holds.
func TestRunSearchUsesContentFieldWhenContentModeSelected(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchForm.GetFormItemByLabel("Filename").(*tview.InputField).SetText("should-be-ignored")
	r.searchForm.GetFormItemByLabel("Search in").(*tview.DropDown).SetCurrentOption(1) // grep
	r.searchContentField.SetText("TODO")

	r.runSearch()

	if captured.Pattern != "TODO" {
		t.Errorf("Pattern = %q, want %q (from Content, not Filename)", captured.Pattern, "TODO")
	}
	if captured.Content != search.ContentGrep {
		t.Errorf("Content = %v, want ContentGrep", captured.Content)
	}
}

// TestRunSearchResizesToBiggerResultsWindow and
// TestBackToSearchFormResizesBackToFormSize pin the user's own request
// that the results window "darf auch gerne etwas größer sein" than the
// form that opens it (see resizeSearchPages/searchResultsWidth/Height).
func TestRunSearchResizesToBiggerResultsWindow(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	isolateSearchRun(t, fakeSearchRun(new(search.Request)))

	r.panel.SetRect(0, 0, 150, 50) // large enough that clampToPanel doesn't shrink either size
	r.openSearch()
	_, _, formWidth, formHeight := r.searchPages.GetRect()

	r.searchForm.GetFormItemByLabel("Filename").(*tview.InputField).SetText("anything")
	r.runSearch()

	_, _, resultsWidth, resultsHeight := r.searchPages.GetRect()
	if resultsWidth <= formWidth || resultsHeight <= formHeight {
		t.Errorf("results rect = %dx%d, want bigger than the form's own %dx%d", resultsWidth, resultsHeight, formWidth, formHeight)
	}
}

func TestBackToSearchFormResizesBackToFormSize(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	isolateSearchRun(t, fakeSearchRun(new(search.Request)))

	r.panel.SetRect(0, 0, 150, 50)
	r.openSearch()
	_, _, formWidth, formHeight := r.searchPages.GetRect()

	r.searchForm.GetFormItemByLabel("Filename").(*tview.InputField).SetText("anything")
	r.runSearch()
	r.backToSearchForm()

	_, _, gotWidth, gotHeight := r.searchPages.GetRect()
	if gotWidth != formWidth || gotHeight != formHeight {
		t.Errorf("rect after backToSearchForm = %dx%d, want back to the form's own %dx%d", gotWidth, gotHeight, formWidth, formHeight)
	}
}

// TestOpenSearchTreePickerSeedsFromScopeFieldAndWritesBack pins the
// Tree button's own action: opens the directory picker (see
// dirpicker_test.go for the picker's own behavior) seeded at whatever
// Start-at currently holds, writing the chosen directory back into it.
func TestOpenSearchTreePickerSeedsFromScopeFieldAndWritesBack(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()
	r.searchScopeField.SetText(dir)

	r.openSearchTreePicker()

	if r.dirPickerPath != dir {
		t.Errorf("dirPickerPath = %q, want seeded from Start at (%q)", r.dirPickerPath, dir)
	}

	appData := filepath.Join(dir, "app-data")
	r.confirmDirPicker() // picks whatever's currently browsed — still dir, since nothing navigated
	if got := r.searchScopeField.GetText(); got != dir {
		t.Errorf("Start at after confirming = %q, want %q", got, dir)
	}

	// Navigating into a subdirectory first and confirming that instead
	// should write the deeper path back, not the original seed.
	r.openSearchTreePicker()
	r.loadDirPicker(appData)
	r.confirmDirPicker()
	if got := r.searchScopeField.GetText(); got != appData {
		t.Errorf("Start at after navigating and confirming = %q, want %q", got, appData)
	}
}

// TestRenderSearchStatusShowsAnimationFrameAndFallbackDir pins
// renderSearchStatus' own two-source text: the current animation frame
// (see hashAnimationFrames, reused directly) plus searchLastDir once
// set, falling back to searchStartDir before any result has arrived.
func TestRenderSearchStatusShowsAnimationFrameAndFallbackDir(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()

	r.searchAnimFrame = 2
	r.searchStartDir = "/start"
	r.searchLastDir = ""
	r.renderSearchStatus()

	got := r.searchStatus.GetText(true)
	if !strings.Contains(got, hashAnimationFrames[2]) {
		t.Errorf("status = %q, want it to contain frame %q", got, hashAnimationFrames[2])
	}
	if !strings.Contains(got, "/start") {
		t.Errorf("status = %q, want the fallback searchStartDir (%q) before any result arrives", got, "/start")
	}

	r.searchLastDir = "/found/here"
	r.renderSearchStatus()
	got = r.searchStatus.GetText(true)
	if !strings.Contains(got, "/found/here") {
		t.Errorf("status = %q, want searchLastDir (%q) once a result has arrived", got, "/found/here")
	}
}

// TestRunSearchCancelsPreviousSearch pins that starting a new search
// cancels whatever was previously running (see cancelSearch) — so a
// slow earlier search can't keep racing new results into the list
// alongside a newer one's own.
func TestRunSearchCancelsPreviousSearch(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	var firstCtx context.Context
	calls := 0
	isolateSearchRun(t, func(ctx context.Context, req search.Request) (<-chan search.Result, <-chan error) {
		calls++
		if calls == 1 {
			firstCtx = ctx
		}
		// Deliberately never closed — see fakeSearchRun's own doc comment.
		return make(chan search.Result), make(chan error)
	})

	r.openSearch()
	pattern := r.searchForm.GetFormItemByLabel("Filename").(*tview.InputField)

	pattern.SetText("first")
	r.runSearch()
	if firstCtx.Err() != nil {
		t.Fatal("setup: the first search's context should not be cancelled yet")
	}

	pattern.SetText("second")
	r.runSearch()
	if firstCtx.Err() == nil {
		t.Error("starting a second search should have cancelled the first one's own context")
	}
	if calls != 2 {
		t.Errorf("searchRun was called %d times, want 2", calls)
	}
}

// TestSearchShortcutRespectsGuard mirrors
// TestOptionsShortcutRespectsGuard for Ctrl+F.
func TestSearchShortcutRespectsGuard(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.app.SetFocus(r.bashLine)
	r.SearchShortcut()
	if r.activePage == searchPage {
		t.Error("SearchShortcut should no-op while the bash line has focus")
	}

	r.app.SetFocus(r.panel)
	r.SearchShortcut()
	if r.activePage != searchPage {
		t.Errorf("activePage = %q after SearchShortcut with the guard passing, want %q", r.activePage, searchPage)
	}
}

// TestOpenSearchResultNavigatesAndCloses pins picking a result: the
// panel jumps straight to it (see Panel.navigateAndSelect) and the
// dialog closes.
func TestOpenSearchResultNavigatesAndCloses(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	sub := filepath.Join(dir, "app-data")
	target := filepath.Join(sub, "nested.txt")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r.openSearch()

	r.openSearchResult(search.Result{Path: target})

	if r.activePage != "" {
		t.Errorf("activePage = %q after picking a result, want closed", r.activePage)
	}
	if r.panel.path != sub {
		t.Errorf("panel.path = %q, want the result's own parent directory %q", r.panel.path, sub)
	}
	_, path, ok := r.panel.CurrentRowPath()
	if !ok || path != target {
		t.Errorf("CurrentRowPath() = (%q, %v), want (%q, true) — the cursor should have landed on the result itself", path, ok, target)
	}
}
