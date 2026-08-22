package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/search"
)

// fakeSearchRun returns a searchRun replacement that records the
// Request it was given in *captured and immediately closes both
// channels with nothing sent — safe to use from a test regardless of
// whether the real find/grep/locate binaries are installed, and never
// reaches r.app.QueueUpdateDraw (see searchRun's own doc comment on
// why that matters).
func fakeSearchRun(captured *search.Request) func(context.Context, search.Request) (<-chan search.Result, <-chan error) {
	return func(_ context.Context, req search.Request) (<-chan search.Result, <-chan error) {
		*captured = req
		results := make(chan search.Result)
		errs := make(chan error)
		close(results)
		close(errs)
		return results, errs
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
	if got := r.searchForm.GetFormItemByLabel("Pattern").(*tview.InputField).GetText(); got != "" {
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
	r.searchForm.GetFormItemByLabel("Pattern").(*tview.InputField).SetText("anything")
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
	r.searchForm.GetFormItemByLabel("Pattern").(*tview.InputField).SetText("*.go")
	r.searchScopeField.SetText("/tmp")
	r.searchForm.GetFormItemByLabel("Mode").(*tview.DropDown).SetCurrentOption(2) // Regex

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
	if r.activePage != searchPage {
		t.Errorf("activePage = %q, want still open (%q) on the results page", r.activePage, searchPage)
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
		results := make(chan search.Result)
		errs := make(chan error)
		close(results)
		close(errs)
		return results, errs
	})

	r.openSearch()
	pattern := r.searchForm.GetFormItemByLabel("Pattern").(*tview.InputField)

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
// TestSettingsShortcutRespectsGuard for Ctrl+F.
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
