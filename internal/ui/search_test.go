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
	if opts[1].mode != search.ContentGrep {
		t.Errorf("opts[1] = %+v, want mode ContentGrep", opts[1])
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

func TestOpenSearchShowsFieldsPrefilledWithPanelScopeAndFocusesFilename(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openSearch()

	if r.activePage != searchPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, searchPage)
	}
	if r.searchFilenameValue != "" {
		t.Errorf("Filename = %q, want empty", r.searchFilenameValue)
	}
	if r.searchScopeValue != dir {
		t.Errorf("Start at = %q, want the panel's own current directory %q", r.searchScopeValue, dir)
	}

	// The user's own request: Filename, not Engine, has first focus.
	wantIdx, ok := r.searchSpanIndex("filename")
	if !ok {
		t.Fatal("setup: no span tagged \"filename\"")
	}
	if r.searchFocusedIdx != wantIdx {
		t.Errorf("searchFocusedIdx = %d, want %d (the Filename span)", r.searchFocusedIdx, wantIdx)
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
// two-stage behavior: from the results page, it returns to the fields
// (see backToSearchForm) rather than closing the whole dialog the way
// Escape from the fields page itself does (see closeSearch).
func TestBackToSearchFormReturnsToFormWithoutClosing(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchFilenameValue = "anything"
	r.runSearch() // switches to the results page

	r.backToSearchForm()

	if r.activePage != searchPage {
		t.Errorf("activePage = %q, want still open (%q)", r.activePage, searchPage)
	}
}

// TestActivateSearchTextFieldAndCommit pins the Properties-style
// editing paradigm end to end: activating a text span opens the shared
// inline editor positioned over it, and committing via Enter stages
// the typed text into that field's own value.
func TestActivateSearchTextFieldAndCommit(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()

	idx, ok := r.searchSpanIndex("filename")
	if !ok {
		t.Fatal("setup: no span tagged \"filename\"")
	}
	r.searchSpans[idx].activate()

	if !r.searchEditField.HasFocus() {
		t.Fatal("activating a text span should have focused the shared inline editor")
	}
	r.searchEditField.SetText("*.go")
	r.finishSearchEdit(tcell.KeyEnter)

	if r.searchFilenameValue != "*.go" {
		t.Errorf("searchFilenameValue = %q, want %q", r.searchFilenameValue, "*.go")
	}
}

// TestCaptureSearchScopeKeyCompletesPath pins Tab-completion while
// editing Start-at, reusing Panel.completions/longestCommonPrefix
// directly — the same behavior Panel's own path header already has,
// per the user's own request that every dialog with a path field
// support it. Wired in only while editing Start-at specifically (see
// activateSearchTextField) — this activates that field's own span to
// pin the wiring, not just the completion function in isolation.
func TestCaptureSearchScopeKeyCompletesPath(t *testing.T) {
	dir := fixtureDir(t) // apple.txt, apricot.txt, banana.txt, app-data/
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()

	idx, ok := r.searchSpanIndex("start-at")
	if !ok {
		t.Fatal("setup: no span tagged \"start-at\"")
	}
	r.searchSpans[idx].activate()
	r.searchEditField.SetText(dir + "/ap")
	r.searchEditField.InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), func(tview.Primitive) {})

	// "ap" is common to apple.txt, apricot.txt, and app-data/ — their
	// longest shared prefix is "ap" itself, same as
	// TestCompletions/TestLongestCommonPrefix already pin at the Panel
	// level; this just pins that the search dialog's own editor reaches
	// the same completions call while editing Start-at.
	want := dir + "/ap"
	if got := r.searchEditField.GetText(); got != want {
		t.Errorf("Start-at editor text after Tab = %q, want %q", got, want)
	}
}

func TestRunSearchBuildsRequestFromDialogState(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchFilenameValue = "*.go"
	r.searchScopeValue = "/tmp"
	r.searchModeIdx = 2 // Regex
	r.searchIgnoreEnabled = true
	r.searchIgnoreValue = ".git, node_modules"
	r.searchCaseSensitive = true

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
	if !captured.CaseSensitive {
		t.Error("CaseSensitive = false, want true")
	}
	if r.activePage != searchPage {
		t.Errorf("activePage = %q, want still open (%q) on the results page", r.activePage, searchPage)
	}
}

// TestRunSearchIgnoresIgnoreValueWhenDisabled pins that the Ignored
// dirs value only feeds into the Request while its own enable
// checkbox is on — typing something there first and only then
// disabling it shouldn't leave it silently still applied.
func TestRunSearchIgnoresIgnoreValueWhenDisabled(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchFilenameValue = "*.go"
	r.searchIgnoreValue = ".git"
	r.searchIgnoreEnabled = false

	r.runSearch()

	if len(captured.IgnoreDirs) != 0 {
		t.Errorf("IgnoreDirs = %v, want none — the enable checkbox is off", captured.IgnoreDirs)
	}
}

// TestRunSearchSkipHiddenAddsGlobToIgnoreDirs pins Skip hidden's own
// mechanism (see search.Request.IgnoreDirs' own doc comment): it
// appends ".*" rather than needing a separate Request field, combining
// with an explicit Ignored dirs list rather than replacing it.
func TestRunSearchSkipHiddenAddsGlobToIgnoreDirs(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchFilenameValue = "*.go"
	r.searchIgnoreEnabled = true
	r.searchIgnoreValue = "node_modules"
	r.searchSkipHidden = true

	r.runSearch()

	want := []string{"node_modules", ".*"}
	if len(captured.IgnoreDirs) != len(want) || captured.IgnoreDirs[0] != want[0] || captured.IgnoreDirs[1] != want[1] {
		t.Errorf("IgnoreDirs = %v, want %v", captured.IgnoreDirs, want)
	}
}

// TestRunSearchUsesContentValueWhenContentTypeSelected pins the split
// between Filename and Content (see newSearchDialog's own doc comment
// on why there are two separate pattern fields, not one reused for
// both): once Search in picks anything other than "File names",
// runSearch reads the pattern from Content, ignoring whatever Filename
// still holds.
func TestRunSearchUsesContentValueWhenContentTypeSelected(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchFilenameValue = "should-be-ignored"
	r.searchContentTypeIdx = 1 // grep — see buildSearchContentOptions' own fixed ordering
	r.searchContentValue = "TODO"

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
// fields that open it (see resizeSearchPages/searchResultsWidth/Height).
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

	r.searchFilenameValue = "anything"
	r.runSearch()

	_, _, resultsWidth, resultsHeight := r.searchPages.GetRect()
	if resultsWidth <= formWidth || resultsHeight <= formHeight {
		t.Errorf("results rect = %dx%d, want bigger than the fields' own %dx%d", resultsWidth, resultsHeight, formWidth, formHeight)
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

	r.searchFilenameValue = "anything"
	r.runSearch()
	r.backToSearchForm()

	_, _, gotWidth, gotHeight := r.searchPages.GetRect()
	if gotWidth != formWidth || gotHeight != formHeight {
		t.Errorf("rect after backToSearchForm = %dx%d, want back to the fields' own %dx%d", gotWidth, gotHeight, formWidth, formHeight)
	}
}

// TestOpenSearchTreePickerSeedsFromScopeValueAndWritesBack pins the
// Tree button's own action: opens the directory picker (see
// dirpicker_test.go for the picker's own behavior) seeded at whatever
// Start-at currently holds, writing the chosen directory back into it.
func TestOpenSearchTreePickerSeedsFromScopeValueAndWritesBack(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()
	r.searchScopeValue = dir

	r.openSearchTreePicker()

	if r.dirPickerPath != dir {
		t.Errorf("dirPickerPath = %q, want seeded from Start at (%q)", r.dirPickerPath, dir)
	}

	appData := filepath.Join(dir, "app-data")
	r.confirmDirPicker() // picks whatever's currently browsed — still dir, since nothing navigated
	if r.searchScopeValue != dir {
		t.Errorf("Start at after confirming = %q, want %q", r.searchScopeValue, dir)
	}

	// Navigating into a subdirectory first and confirming that instead
	// should write the deeper path back, not the original seed.
	r.openSearchTreePicker()
	r.loadDirPicker(appData)
	r.confirmDirPicker()
	if r.searchScopeValue != appData {
		t.Errorf("Start at after navigating and confirming = %q, want %q", r.searchScopeValue, appData)
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

	r.searchFilenameValue = "first"
	r.runSearch()
	if firstCtx.Err() != nil {
		t.Fatal("setup: the first search's context should not be cancelled yet")
	}

	r.searchFilenameValue = "second"
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

// TestChoiceSpanSelectsOption pins the choice group mechanism shared
// by Engine/Mode/Search in: activating one of a group's own spans
// selects it (updates the relevant *Idx field) and re-renders.
func TestChoiceSpanSelectsOption(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()

	if r.searchModeIdx != 0 {
		t.Fatalf("setup: searchModeIdx = %d, want 0 (Glob)", r.searchModeIdx)
	}

	// Mode's own three choice spans follow Engine's and Start-at's and
	// Tree's own spans in searchTop — rather than hardcoding an index,
	// find "Keyword" by rendered text (see searchModeLabels) among
	// searchTop's own spans.
	idx := -1
	for i, s := range r.searchSpans {
		if s.widget != r.searchTop {
			continue
		}
		text, _ := textAtSpan(r, s)
		if text == "○ Keyword" || text == "● Keyword" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("setup: no span for the Keyword mode option found")
	}
	r.searchSpans[idx].activate()

	if r.searchModeIdx != 1 {
		t.Errorf("searchModeIdx = %d after activating Keyword, want 1", r.searchModeIdx)
	}
}

// textAtSpan reads the literal text a searchSpan's own row/column range
// covers, from its widget's current (tag-stripped) rendered text — used
// by TestChoiceSpanSelectsOption to find a specific option by its own
// rendered label without hardcoding its span index, which shifts if
// the dialog's own layout changes.
func textAtSpan(r *Root, s searchSpan) (string, bool) {
	lines := strings.Split(s.widget.GetText(true), "\n")
	if s.row < 0 || s.row >= len(lines) {
		return "", false
	}
	runes := []rune(lines[s.row])
	if s.startCol < 0 || s.endCol > len(runes) || s.startCol > s.endCol {
		return "", false
	}
	return string(runes[s.startCol:s.endCol]), true
}

// TestCheckboxSpansToggle pins Ignore-dirs-enable/Case sensitive/Skip
// hidden: each is a single-option choice group (see searchBuilder.choice)
// that flips its own bool on activation.
func TestCheckboxSpansToggle(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()

	tests := []struct {
		name string
		get  func() bool
	}{
		{"Ignore dirs:", func() bool { return r.searchIgnoreEnabled }},
		{"Case sensitive", func() bool { return r.searchCaseSensitive }},
		{"Skip hidden", func() bool { return r.searchSkipHidden }},
	}
	for _, tt := range tests {
		if tt.get() {
			t.Fatalf("setup: %q should start unchecked", tt.name)
		}
		idx := -1
		for i, s := range r.searchSpans {
			text, ok := textAtSpan(r, s)
			if ok && strings.HasSuffix(text, tt.name) {
				idx = i
				break
			}
		}
		if idx < 0 {
			t.Fatalf("no span found for %q", tt.name)
		}
		r.searchSpans[idx].activate()
		if !tt.get() {
			t.Errorf("%q should be checked after activating its span", tt.name)
		}
	}
}

// TestMoveSearchFocusWrapsThroughButtons pins Tab/Backtab's own
// wraparound: past the last span reaches Cancel then Search, past
// Search wraps back to the first span; Backtab from the first span
// wraps to Search — the same shape movePropertiesFocus already has.
func TestMoveSearchFocusWrapsThroughButtons(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()
	n := len(r.searchSpans)

	r.setSearchFocus(n - 1) // the last real span
	r.moveSearchFocus(1)
	if r.searchFocusedIdx != n {
		t.Errorf("focus after Tab past the last span = %d, want %d (Cancel)", r.searchFocusedIdx, n)
	}
	if !r.searchCancelBtn.HasFocus() {
		t.Error("real focus should be on Cancel")
	}

	r.moveSearchFocus(1)
	if r.searchFocusedIdx != n+1 {
		t.Errorf("focus after Tab from Cancel = %d, want %d (Search)", r.searchFocusedIdx, n+1)
	}
	if !r.searchSearchBtn.HasFocus() {
		t.Error("real focus should be on Search")
	}

	r.moveSearchFocus(1)
	if r.searchFocusedIdx != 0 {
		t.Errorf("focus after Tab from Search = %d, want 0 (wraps to the first span)", r.searchFocusedIdx)
	}

	r.moveSearchFocus(-1)
	if r.searchFocusedIdx != n+1 {
		t.Errorf("focus after Backtab from the first span = %d, want %d (wraps to Search)", r.searchFocusedIdx, n+1)
	}
}

// TestSearchEngineChangeDimsScopeField and
// TestSearchContentTypeChangeDimsContentField pin the user's own
// request: Start at reads as visibly unavailable while Engine=locate
// (it no longer affects locate's own results — see
// search.Request.Scope's own doc comment), and Content while Search in
// is still "File names" — both via dimTag in the rendered text, not
// (like the previous, tview.Form-based version of this dialog) a
// SetDisabled call whose color change tview's own Form.Draw silently
// discarded every frame.
func TestSearchEngineChangeDimsScopeField(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()

	// Checked on Start-at's own row specifically, not searchTop's whole
	// text: Ignore dirs' own value lives in the same TextView and is
	// dimmed by default too (its own enable checkbox starts off), so a
	// whole-text check can't tell which field the dimming belongs to.
	if rowIsDimmed(t, r, "start-at") {
		t.Fatal("setup: Start at should not be dimmed while Engine=find")
	}
	if len(r.searchEngineOptions) < 2 {
		t.Skip("locate not available in this environment")
	}

	r.searchEngineIdx = 1 // locate
	r.rerenderSearchDialog()

	if !rowIsDimmed(t, r, "start-at") {
		t.Error("Start at should be dimmed while Engine=locate")
	}

	r.searchEngineIdx = 0 // back to find
	r.rerenderSearchDialog()
	if rowIsDimmed(t, r, "start-at") {
		t.Error("Start at should no longer be dimmed once back on find")
	}
}

// rowIsDimmed reports whether the raw (tag-included) text of the row
// tagName's own span sits on contains dimTag — see
// TestSearchEngineChangeDimsScopeField's own doc comment on why a
// whole-TextView check isn't precise enough.
func rowIsDimmed(t *testing.T, r *Root, tagName string) bool {
	t.Helper()
	idx, ok := r.searchSpanIndex(tagName)
	if !ok {
		t.Fatalf("no span tagged %q", tagName)
	}
	span := r.searchSpans[idx]
	lines := strings.Split(span.widget.GetText(false), "\n")
	if span.row < 0 || span.row >= len(lines) {
		t.Fatalf("span row %d out of range for %q (%d lines)", span.row, tagName, len(lines))
	}
	return strings.Contains(lines[span.row], dimTag)
}

func TestSearchContentTypeChangeDimsContentField(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()

	if !strings.Contains(r.searchRight.GetText(false), dimTag) {
		t.Fatal("setup: Content should start dimmed — Search in defaults to \"File names\"")
	}

	r.searchContentTypeIdx = 1 // grep
	r.rerenderSearchDialog()
	if strings.Contains(r.searchRight.GetText(false), dimTag) {
		t.Error("Content should no longer be dimmed once Search in picks grep")
	}

	r.searchContentTypeIdx = 0 // back to "File names"
	r.rerenderSearchDialog()
	if !strings.Contains(r.searchRight.GetText(false), dimTag) {
		t.Error("Content should be dimmed again once back on \"File names\"")
	}
}
