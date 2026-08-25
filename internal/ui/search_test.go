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
		if !strings.Contains(got, "Done — 0 found") {
			t.Errorf("%s: noSearchResultsText(%+v) = %q, missing the base message", tt.name, tt.req, got)
		}
		gotStale := strings.Contains(got, "stale")
		if gotStale != tt.wantStale {
			t.Errorf("%s: noSearchResultsText(%+v) = %q, mentions staleness = %v, want %v", tt.name, tt.req, got, gotStale, tt.wantStale)
		}
	}
}

// TestParseIgnoreDirs also pins a real user report: "/development"
// (a leading slash) used to silently exclude nothing at all — find's
// own -name test matches a bare basename, which never contains a "/",
// so that pattern could never match anything — rather than being
// treated the same as the "development" the user almost certainly
// meant. Both a leading and a trailing slash are stripped, and a
// bare "/" on its own (nothing left once stripped) drops out
// entirely, the same as an empty entry already does.
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
		{"/development", []string{"development"}},
		{"development/", []string{"development"}},
		{"/development/", []string{"development"}},
		{"/", nil},
		{"/.git, /node_modules/", []string{".git", "node_modules"}},
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
// editing Start-at, reusing Panel.dirCompletions/longestCommonPrefix
// directly — see dirCompletions' own doc comment on why Start-at gets
// its own directory-only, case-sensitive variant rather than Panel's
// plain completions. Wired in only while editing Start-at specifically
// (see activateSearchTextField) — this activates that field's own span
// to pin the wiring, not just the completion function in isolation.
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

	// apple.txt/apricot.txt are files, excluded outright by
	// dirCompletions — app-data/ is the only directory starting with
	// "ap", so completion goes straight there instead of stopping at
	// "ap" itself the way Panel's own plain (file-inclusive) completion
	// would (see TestCompletions at the Panel level for that).
	want := dir + "/app-data/"
	if got := r.searchEditField.GetText(); got != want {
		t.Errorf("Start-at editor text after Tab = %q, want %q", got, want)
	}
}

// TestCaptureSearchScopeKeyAlwaysConsumesTab pins the user's own
// explicit request: Start-at is deliberately exempted from the
// Tab-cycles-through-every-option behavior every other field in this
// dialog has — Tab here always means "complete", even once there's
// nothing left to add, never "leave the field" (Backtab or a click
// elsewhere are how you actually leave it — see commitPendingSearchEdit).
func TestCaptureSearchScopeKeyAlwaysConsumesTab(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"appdata1", "appdata2"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
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
	r.searchEditField.SetText(dir + "/app")

	// First Tab: "app" is shared by appdata1/ and appdata2/, extending
	// to "appdata" — consumed.
	if result := r.captureSearchScopeKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)); result != nil {
		t.Fatal("Tab should always be consumed")
	}
	want := dir + "/appdata"
	if got := r.searchEditField.GetText(); got != want {
		t.Fatalf("after first Tab, text = %q, want %q", got, want)
	}

	// Second Tab: still ambiguous between appdata1/ and appdata2/ —
	// nothing left to complete, but it must still be consumed (a no-op
	// on the text, not a fall-through to field navigation).
	if result := r.captureSearchScopeKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)); result != nil {
		t.Error("Tab should still be consumed once nothing is left to complete")
	}
	if got := r.searchEditField.GetText(); got != want {
		t.Errorf("after second Tab, text = %q, want unchanged %q", got, want)
	}
}

// TestCaptureSearchScopeKeyExcludesFiles pins the user's own explicit
// request, and the real bug report behind it: completing Start-at used
// to stop at "Download" (not the full "Downloads/") because a
// same-directory file, "download-thing.sh", was still in the running
// as a candidate purely on account of its name, despite Start-at never
// being able to hold a file in the first place. dirCompletions'
// directory-only filtering (see its own doc comment) resolves this on
// its own — the file is gone from the candidate list before the two
// names' shared prefix ever matters, so completion goes straight to
// the one real directory that's actually a valid Start-at value.
func TestCaptureSearchScopeKeyExcludesFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Downloads"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "download-thing.sh"), nil, 0644); err != nil {
		t.Fatal(err)
	}

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
	r.searchEditField.SetText(dir + "/Down")

	r.captureSearchScopeKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))

	want := dir + "/Downloads/"
	if got := r.searchEditField.GetText(); got != want {
		t.Errorf("Start-at editor text after Tab = %q, want %q (download-thing.sh, a file, should never have been a candidate)", got, want)
	}
}

// TestEnterInFilenameFieldRunsSearch pins the user's own request:
// Enter while editing Filename specifically runs the search
// immediately, the same as clicking Search.
func TestEnterInFilenameFieldRunsSearch(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch() // first focus is already Filename — see its own doc comment
	idx, ok := r.searchSpanIndex("filename")
	if !ok {
		t.Fatal("setup: no span tagged \"filename\"")
	}
	r.searchSpans[idx].activate() // opens the shared inline editor
	r.searchEditField.SetText("*.go")
	r.searchEditField.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if captured.Pattern != "*.go" {
		t.Errorf("Pattern = %q, want %q — Enter in Filename should have run the search", captured.Pattern, "*.go")
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q, want the form closed, revealing the panel's own results", r.activePage)
	}
	if !r.panel.searchMode {
		t.Error("panel.searchMode = false, want true — Enter in Filename should have run the search")
	}
}

// TestEnterInOtherFieldsDoesNotRunSearch pins that the Enter-runs-
// search behavior is scoped to Filename alone — Start-at's own Enter
// still just commits and stays on the form.
func TestEnterInOtherFieldsDoesNotRunSearch(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	searchRunCalled := false
	isolateSearchRun(t, func(_ context.Context, req search.Request) (<-chan search.Result, <-chan error) {
		searchRunCalled = true
		return make(chan search.Result), make(chan error)
	})

	r.openSearch()
	idx, ok := r.searchSpanIndex("start-at")
	if !ok {
		t.Fatal("setup: no span tagged \"start-at\"")
	}
	r.searchSpans[idx].activate()
	r.searchEditField.SetText(dir)
	r.searchEditField.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if searchRunCalled {
		t.Error("Enter in Start-at should not have run the search")
	}
	if r.searchScopeValue != dir {
		t.Errorf("searchScopeValue = %q, want %q — Enter should still commit", r.searchScopeValue, dir)
	}
}

// TestCommitPendingSearchEditPreservesInProgressText pins the fix for
// a real bug: refining Start-at by hand after a Tree pick, then
// leaving the field via a click elsewhere rather than Enter/Tab, used
// to throw the in-progress text away outright — the next render
// silently fell back to whatever Start-at held before that edit began.
// commitPendingSearchEdit is captureSearchMouse's own fix for this
// (see its own doc comment) — tested directly here since routing a
// synthetic click through captureSearchMouse itself needs a real
// on-screen layout this test doesn't have.
func TestCommitPendingSearchEditPreservesInProgressText(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()
	r.searchScopeValue = dir // simulates a Tree pick

	idx, ok := r.searchSpanIndex("start-at")
	if !ok {
		t.Fatal("setup: no span tagged \"start-at\"")
	}
	r.searchSpans[idx].activate() // opens the shared inline editor, prefilled with dir
	refined := dir + "/app-data"
	r.searchEditField.SetText(refined) // the user refining it by hand, never pressing Enter/Tab

	r.commitPendingSearchEdit()

	if r.searchScopeValue != refined {
		t.Errorf("searchScopeValue = %q, want %q (the refined value, not the original Tree pick)", r.searchScopeValue, refined)
	}
	if r.searchEditCommit != nil {
		t.Error("searchEditCommit should be nil again once committed — see finishSearchEdit's own doc comment on why")
	}
}

// TestCommitPendingSearchEditNoopWhenNotEditing pins that calling it
// with nothing in progress (searchEditCommit nil) is a safe no-op —
// captureSearchMouse calls it unconditionally on every click,
// regardless of whether an edit is actually in progress.
func TestCommitPendingSearchEditNoopWhenNotEditing(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()

	r.commitPendingSearchEdit() // must not panic or change anything
}

// TestRunSearchRejectsNonexistentStartAt pins the user's own request:
// searching from a Start-at directory that doesn't actually exist
// reports a clear error instead of silently running find(1) anyway and
// coming back with an empty, indistinguishable-from-a-real-empty-
// result "No matches found" (find itself exits non-zero on a missing
// path, but this app's own runner.go deliberately never treats a
// non-zero exit as an error — see its own doc comment) — and, per a
// second, follow-up request, does so on the panel itself (its own
// search-status line, in red — see Panel.setSearchStatusColor) rather
// than Root's own global error overlay, which used to close the whole
// search dialog outright and discard whatever was already typed in
// ("das ist doof... das muss ja nicht so ein Game-Killer sein").
// Escape from there (see Panel.onSearchEscape) goes straight back to
// the still-intact form.
func TestRunSearchRejectsNonexistentStartAt(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	searchRunCalled := false
	isolateSearchRun(t, func(_ context.Context, req search.Request) (<-chan search.Result, <-chan error) {
		searchRunCalled = true
		return make(chan search.Result), make(chan error)
	})

	r.openSearch()
	r.searchFilenameValue = "*.go"
	r.searchScopeValue = dir + "/does-not-exist"

	r.runSearch()

	if searchRunCalled {
		t.Error("runSearch should not have shelled out to find at all for a non-existent Start-at")
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q, want the form overlay closed (revealing the panel), not the global error overlay", r.activePage)
	}
	if !r.panel.searchMode {
		t.Error("panel.searchMode = false, want true — the error shows on the panel itself, in place of results")
	}
	if got := r.panel.table.GetRowCount(); got != 0 {
		t.Errorf("panel has %d rows after a rejected search, want 0 (no results — just the error status line)", got)
	}
	if got := r.panel.header.GetText(true); !strings.Contains(got, "does not exist") {
		t.Errorf("panel status = %q, want it to mention the directory doesn't exist", got)
	}
	if got := r.panel.header.GetText(true); !strings.Contains(got, "Esc") {
		t.Errorf("panel status = %q, want the Esc-back-to-search hint even for this error case (see setSearchStatus)", got)
	}

	// Escape must return to the still-intact form, with Filename
	// untouched.
	r.panel.captureTableKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if r.activePage != searchPage {
		t.Errorf("activePage = %q after Escape, want the form open again (%q)", r.activePage, searchPage)
	}
	if r.searchFilenameValue != "*.go" {
		t.Errorf("searchFilenameValue = %q after Escape, want it preserved (%q)", r.searchFilenameValue, "*.go")
	}
}

// TestRunSearchAllowsNonexistentStartAtForLocate pins that the
// existence check only applies to EngineFind — EngineLocate never uses
// Scope at all (see Request.Scope's own doc comment), so there's
// nothing to validate against it.
func TestRunSearchAllowsNonexistentStartAtForLocate(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchFilenameValue = "*.go"
	r.searchScopeValue = dir + "/does-not-exist"
	r.searchEngineIdx = searchEngineIndex(t, r, search.EngineLocate)

	r.runSearch()

	if r.activePage == errorPage {
		t.Errorf("locate shouldn't be blocked by a non-existent Start-at, got error overlay: %q", r.errorView.GetText(true))
	}
	if captured.Engine != search.EngineLocate {
		t.Errorf("Engine = %v, want EngineLocate", captured.Engine)
	}
}

// TestRunSearchRejectsNonexistentStartAtForLocatePlainContentSearch
// pins the same non-existent-Start-at check TestRunSearchAllowsNonexistentStartAtForLocate
// just pinned as *skipped* for a locate filename search, now correctly
// *applying* for a plain content search under locate instead — the one
// EngineLocate case that genuinely uses Start-at as a real grep scope
// (see runSearch's own plainContentSearch), so a typo'd or
// since-deleted directory needs exactly the same catch EngineFind
// already gets, not a silent "0 found" indistinguishable from a real
// empty result.
func TestRunSearchRejectsNonexistentStartAtForLocatePlainContentSearch(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	searchRunCalled := false
	isolateSearchRun(t, func(context.Context, search.Request) (<-chan search.Result, <-chan error) {
		searchRunCalled = true
		return make(chan search.Result), make(chan error)
	})

	r.openSearch()
	r.searchContentValue = "needle" // Content only — Filename left blank
	r.searchScopeValue = dir + "/does-not-exist"
	r.searchEngineIdx = searchEngineIndex(t, r, search.EngineLocate)

	r.runSearch()

	if searchRunCalled {
		t.Error("runSearch should not have shelled out to grep at all for a non-existent Start-at")
	}
	if !r.panel.searchMode {
		t.Error("panel.searchMode = false, want true — the error shows on the panel itself, same as EngineFind's own identical case")
	}
	if got := r.panel.header.GetText(true); !strings.Contains(got, "does not exist") {
		t.Errorf("panel status = %q, want it to mention the directory doesn't exist", got)
	}
}

// TestRerenderSearchDialogUndimsStartAtForLocatePlainContentSearch
// pins that Start-at's own visible dimming (see rerenderSearchDialog's
// own scopeDimmed) stays in sync with whether it's actually used —
// the same "Engine=locate dims it" behavior
// TestSearchEngineChangeDimsScopeField already pins, refined for the
// one case that's now an exception to it: dimmed for locate's usual
// "Start-at means nothing" cases (a filename search, pinned here),
// but NOT for a plain content search, where it's now a real, active
// grep scope — a field showing as inert while actually driving the
// search would be exactly the kind of visual-versus-actual mismatch
// that caused this whole area's confusion in the first place.
func TestRerenderSearchDialogUndimsStartAtForLocatePlainContentSearch(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()
	r.searchEngineIdx = searchEngineIndex(t, r, search.EngineLocate)

	r.searchFilenameValue = "*.go"
	r.searchContentValue = ""
	r.rerenderSearchDialog()
	if !rowIsDimmed(t, r, "start-at") {
		t.Error("Start at should be dimmed for a locate filename search")
	}

	r.searchFilenameValue = ""
	r.searchContentValue = "needle"
	r.rerenderSearchDialog()
	if rowIsDimmed(t, r, "start-at") {
		t.Error("Start at should NOT be dimmed for locate's own plain content search — it's a real, active scope here")
	}
}

// TestRunSearchUsesStartAtForLocatePlainContentSearchToo pins the
// user's own explicit, twice-repeated request — the second time after
// an earlier attempt at this got it backwards (see this test's own
// git history): a *plain* content search (Content filled in, Filename
// left blank — nothing for locate itself to narrow with, so there's no
// name-matching step for Engine to drive at all) greps from Start-at's
// own real, typed value, exactly the same as it already does under
// Engine=find — Engine is meant to be entirely irrelevant here, not
// "irrelevant except Scope." Start-at is deliberately set to something
// other than the panel's own current directory below, to prove the
// panel's own path is never silently substituted in its place.
func TestRunSearchUsesStartAtForLocatePlainContentSearchToo(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	altScope := filepath.Join(dir, "app-data") // deliberately NOT r.panel.path — must still be used, not silently swapped out
	r.openSearch()
	r.searchContentValue = "needle"
	r.searchScopeValue = altScope
	r.searchEngineIdx = searchEngineIndex(t, r, search.EngineLocate)

	r.runSearch()

	if captured.Scope != altScope {
		t.Errorf("Scope = %q, want Start-at's own real value %q, not the panel's own current directory %q", captured.Scope, altScope, dir)
	}
}

// TestRunSearchKeepsStartAtScopeForLocateWithFilenameNarrowing pins
// that a locate-narrowed search (Filename also filled in — locate
// itself now has something to narrow with, see search.listThenGrep)
// still passes Start-at's own resolved value through as Scope even
// though nothing downstream actually reads it for that case — Scope
// genuinely doesn't matter for locate's own narrowing step either way
// (see Request.Scope's own doc comment), but there's no reason to
// special-case leaving it out just because it happens to go unused.
func TestRunSearchKeepsStartAtScopeForLocateWithFilenameNarrowing(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	altScope := filepath.Join(dir, "app-data")
	r.openSearch()
	r.searchFilenameValue = "*.go"
	r.searchContentValue = "needle"
	r.searchScopeValue = altScope
	r.searchEngineIdx = searchEngineIndex(t, r, search.EngineLocate)

	r.runSearch()

	if captured.Scope != altScope {
		t.Errorf("Scope = %q, want Start-at's own %q unchanged (Filename narrows locate's own list, so the override doesn't apply)", captured.Scope, altScope)
	}
}

// TestRunSearchKeepsStartAtScopeForFindPlainContentSearch pins that
// the panel-path override above is scoped to Engine=locate
// specifically — a plain content search under Engine=find keeps using
// Start-at exactly as it always has (grep's own real, user-chosen
// scope, not dimmed or inert the way it is for locate).
func TestRunSearchKeepsStartAtScopeForFindPlainContentSearch(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	altScope := filepath.Join(dir, "app-data")
	r.openSearch()
	r.searchContentValue = "needle"
	r.searchScopeValue = altScope
	r.searchEngineIdx = searchEngineIndex(t, r, search.EngineFind)

	r.runSearch()

	if captured.Scope != altScope {
		t.Errorf("Scope = %q, want Start-at's own %q unchanged (Engine is find, not locate)", captured.Scope, altScope)
	}
}

// searchEngineIndex finds the index into r.searchEngineOptions whose
// own engine matches want, skipping the test if it isn't available on
// this host (e.g. locate — see search.LocateAvailable's own doc
// comment).
func searchEngineIndex(t *testing.T, r *Root, want search.Engine) int {
	t.Helper()
	for i, opt := range r.searchEngineOptions {
		if opt.engine == want {
			return i
		}
	}
	t.Skipf("%v not available in this environment", want)
	return -1
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
	r.searchShellPatterns = false // "Using shell patterns" unchecked -> filename Mode is Regex
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
	if r.activePage != "" {
		t.Errorf("activePage = %q, want the form closed, revealing the panel's own results", r.activePage)
	}
	if !r.panel.searchMode {
		t.Error("panel.searchMode = false, want true")
	}
}

// TestRunSearchIncludeArchivesReachesRequest pins that the "Include
// zip, tar (gz, bz2, xz)" checkbox actually reaches
// search.Request.IncludeArchives for a plain filename search.
func TestRunSearchIncludeArchivesReachesRequest(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchFilenameValue = "abc*"
	r.searchIncludeArchives = true
	r.runSearch()

	if !captured.IncludeArchives {
		t.Error("IncludeArchives = false, want true")
	}
}

// TestRunSearchIncludeArchivesIgnoredForContentSearch pins the same
// scope decision NamePattern/NameMode already document on Request
// itself: Include Archives only ever means something for a plain
// filename search — checking it and also typing something into Content
// must not reach IncludeArchives as true, the same as it has no
// checkbox of its own in Content's own column to begin with (see
// rerenderSearchDialog).
func TestRunSearchIncludeArchivesIgnoredForContentSearch(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchContentValue = "TODO"
	r.searchIncludeArchives = true
	r.runSearch()

	if captured.IncludeArchives {
		t.Error("IncludeArchives = true, want false once Content is filled in")
	}
}

// TestRunSearchIncludeCompressedReachesRequest pins that the "Include
// compressed files" checkbox actually reaches
// search.Request.IncludeCompressed for a content search.
func TestRunSearchIncludeCompressedReachesRequest(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchContentValue = "TODO"
	r.searchIncludeCompressed = true
	r.runSearch()

	if !captured.IncludeCompressed {
		t.Error("IncludeCompressed = false, want true")
	}
}

// TestRunSearchIncludeCompressedIgnoredForFilenameSearch is
// TestRunSearchIncludeArchivesIgnoredForContentSearch's own mirror:
// Include Compressed only ever means something for a content search —
// checking it while Content is left blank (a plain filename search)
// must not reach IncludeCompressed as true.
func TestRunSearchIncludeCompressedIgnoredForFilenameSearch(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchFilenameValue = "*.go"
	r.searchIncludeCompressed = true
	r.runSearch()

	if captured.IncludeCompressed {
		t.Error("IncludeCompressed = true, want false for a plain filename search")
	}
}

// TestRunSearchWiresOnProgress pins that runSearch actually sets
// search.Request.OnProgress (see renderSearchStatus/searchCurrentPos) —
// not exercised end to end here (that would need the real
// tview.Application event loop actually running to drain
// QueueUpdateDraw, which this package's own tests deliberately avoid
// needing — see StartClock's own doc comment for the identical
// concern), just that the wiring itself is actually in place rather
// than silently left nil.
func TestRunSearchWiresOnProgress(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchFilenameValue = "*.go"
	r.runSearch()

	if captured.OnProgress == nil {
		t.Error("OnProgress = nil, want it wired for the status line's live progress")
	}
}

// TestRunSearchStripsSlashesFromIgnoreDirs pins the real user report
// end to end, through the actual dialog field runSearch reads (not
// just parseIgnoreDirs in isolation — see its own test): typing
// "/development" into Ignore dirs used to build a Request whose
// IgnoreDirs held that leading slash intact, silently excluding
// nothing at all (find's own -name test can't match a basename against
// a pattern containing "/") — captured.IgnoreDirs must hold the
// stripped "development" instead, the same directory name Find
// recursively would actually prune.
func TestRunSearchStripsSlashesFromIgnoreDirs(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchFilenameValue = "hangman.*"
	r.searchIgnoreEnabled = true
	r.searchIgnoreValue = "/development"

	r.runSearch()

	want := []string{"development"}
	if len(captured.IgnoreDirs) != len(want) || captured.IgnoreDirs[0] != want[0] {
		t.Errorf("IgnoreDirs = %v, want %v (the leading slash stripped)", captured.IgnoreDirs, want)
	}
}

// TestRunSearchFilenameChecksMapToRequestFields pins MC's own Filename
// checkboxes (Find recursively / Follow symlinks / Using shell
// patterns) each landing on their own Request field — NonRecursive
// inverted from Find recursively (see Request.NonRecursive's own doc
// comment on why), FollowSymlinks passed straight through, and Using
// shell patterns feeding filenameMode rather than a Request field of
// its own.
func TestRunSearchFilenameChecksMapToRequestFields(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchFilenameValue = "*.go"
	r.searchRecursive = false
	r.searchFollowSymlinks = true
	r.searchShellPatterns = true

	r.runSearch()

	if !captured.NonRecursive {
		t.Error("NonRecursive = false, want true (Find recursively was unchecked)")
	}
	if !captured.FollowSymlinks {
		t.Error("FollowSymlinks = false, want true")
	}
	if captured.Mode != search.ModeGlob {
		t.Errorf("Mode = %v, want ModeGlob (Using shell patterns was checked)", captured.Mode)
	}
}

// TestRunSearchContentChecksMapToRequestFields pins MC's own Content
// checkboxes (Whole words / Regular expression / First hit) — Whole
// words and First hit pass straight through, Regular expression feeds
// the content search's own Mode (independent from the Filename
// column's own Mode, see runSearch's own doc comment).
func TestRunSearchContentChecksMapToRequestFields(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchContentValue = "TODO" // a filled Content field is what selects content search now — see runSearch's own doc comment
	r.searchWholeWords = true
	r.searchContentRegex = true
	r.searchFirstHit = true

	r.runSearch()

	if !captured.WholeWords {
		t.Error("WholeWords = false, want true")
	}
	if !captured.FirstHit {
		t.Error("FirstHit = false, want true")
	}
	if captured.Mode != search.ModeRegex {
		t.Errorf("Mode = %v, want ModeRegex (Regular expression was checked)", captured.Mode)
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

// TestRunSearchUsesContentValueWhenContentFilled pins the split between
// Filename and Content (see newSearchDialog's own doc comment on why
// there are two separate pattern fields, not one reused for both):
// once Content itself is filled in, runSearch reads the pattern from
// there, ignoring whatever Filename still holds (see runSearch's own
// doc comment — there's no separate "Search in" choice any more, a
// filled Content field is the only signal).
func TestRunSearchUsesContentValueWhenContentFilled(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchFilenameValue = "should-be-ignored"
	r.searchContentValue = "TODO"

	r.runSearch()

	if captured.Pattern != "TODO" {
		t.Errorf("Pattern = %q, want %q (from Content, not Filename)", captured.Pattern, "TODO")
	}
	if captured.Content != search.ContentGrep {
		t.Errorf("Content = %v, want ContentGrep", captured.Content)
	}
}

// TestRunSearchClosesFormAndShowsResultsInPanel pins the user's own
// request that results show directly in the panel's own normal file
// overview area — runSearch closes the form overlay outright (not just
// switching to some second, bigger page of it — that page no longer
// exists) and switches the panel into search-results mode (see
// Panel.showSearchResults).
func TestRunSearchClosesFormAndShowsResultsInPanel(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	isolateSearchRun(t, fakeSearchRun(new(search.Request)))

	r.openSearch()
	if r.activePage != searchPage {
		t.Fatalf("activePage = %q before Search, want the form open (%q)", r.activePage, searchPage)
	}

	r.searchFilenameValue = "anything"
	r.runSearch()

	if r.activePage != "" {
		t.Errorf("activePage = %q after Search, want the form closed, revealing the panel", r.activePage)
	}
	if !r.panel.searchMode {
		t.Error("panel.searchMode = false after Search, want true")
	}
}

// TestBackToSearchFormReopensWithoutTouchingPanel pins Escape/Ctrl+F
// while search results are showing: the form reopens (see openSearch's
// own searchMode-aware reset) without discarding or otherwise
// disturbing whatever the panel currently has on screen (see
// Panel.showSearchResults never being called again here) — refining a
// search that came back wrong (or empty) doesn't lose the results
// already gathered.
func TestBackToSearchFormReopensWithoutTouchingPanel(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	isolateSearchRun(t, fakeSearchRun(new(search.Request)))

	r.openSearch()
	r.searchFilenameValue = "anything"
	r.runSearch()
	r.panel.appendSearchResult(search.Result{Path: filepath.Join(dir, "apple.txt")})

	r.backToSearchForm()

	if r.activePage != searchPage {
		t.Errorf("activePage = %q, want the form open again (%q)", r.activePage, searchPage)
	}
	if !r.panel.searchMode {
		t.Error("panel.searchMode = false after backToSearchForm, want still true — results shouldn't be discarded")
	}
	if got := r.panel.table.GetRowCount(); got != 1 {
		t.Errorf("panel has %d rows after backToSearchForm, want the one already-gathered result still there", got)
	}
	if r.searchFilenameValue != "anything" {
		t.Errorf("searchFilenameValue = %q after backToSearchForm, want it preserved", r.searchFilenameValue)
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
// renderSearchStatus' own three-source text, most-specific first: the
// current animation frame (see hashAnimationFrames, reused directly)
// plus searchCurrentPos (live OnProgress) once set, falling back to
// searchLastDir once a result has arrived, and finally to
// searchStartDir before either one has anything at all.
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
	r.searchCurrentPos = ""
	r.renderSearchStatus()

	got := r.panel.header.GetText(true)
	if !strings.Contains(got, hashAnimationFrames[2]) {
		t.Errorf("status = %q, want it to contain frame %q", got, hashAnimationFrames[2])
	}
	if !strings.Contains(got, "/start") {
		t.Errorf("status = %q, want the fallback searchStartDir (%q) before any result arrives", got, "/start")
	}

	r.searchLastDir = "/found/here"
	r.renderSearchStatus()
	got = r.panel.header.GetText(true)
	if !strings.Contains(got, "/found/here") {
		t.Errorf("status = %q, want searchLastDir (%q) once a result has arrived", got, "/found/here")
	}

	r.searchCurrentPos = "/currently/scanning/this"
	r.renderSearchStatus()
	got = r.panel.header.GetText(true)
	if !strings.Contains(got, "/currently/scanning/this") {
		t.Errorf("status = %q, want live searchCurrentPos (%q) to win over searchLastDir", got, "/currently/scanning/this")
	}
	if strings.Contains(got, "/found/here") {
		t.Errorf("status = %q, want searchLastDir not shown once searchCurrentPos is set", got)
	}
	// The real user report this pins: with searchCurrentPos changing
	// length on every live update, an Esc hint placed after it visibly
	// jumped left/right along with it — see setSearchStatusLive's own
	// doc comment. The hint's own position must stay right after the
	// animation frame instead, never after the live-changing detail.
	if hintIdx, detailIdx := strings.Index(got, "Esc"), strings.Index(got, "/currently/scanning/this"); hintIdx < 0 || detailIdx < 0 || hintIdx > detailIdx {
		t.Errorf("status = %q, want the Esc hint before searchCurrentPos, not after (hintIdx=%d, detailIdx=%d)", got, hintIdx, detailIdx)
	}
}

// TestSetSearchStatusAlwaysAppendsEscHint pins the user's own explicit
// request: the panel's own search-status line always reminds that
// Escape goes back to the search, regardless of the search's own
// outcome — checked directly against setSearchStatus, used by
// streamSearchResults' own final status and showSearchError (see
// setSearchStatusLive's own doc comment for renderSearchStatus's own,
// differently-ordered variant, covered by its own test below).
func TestSetSearchStatusAlwaysAppendsEscHint(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()

	r.setSearchStatus("Done — 3 found")
	if got := r.panel.header.GetText(true); !strings.Contains(got, "Esc") {
		t.Errorf("status = %q, want it to mention Esc", got)
	}

	r.setSearchStatus("") // showSearchError's own case — no other status text at all
	if got := r.panel.header.GetText(true); !strings.Contains(got, "Esc") {
		t.Errorf("status with no other text = %q, want it to still mention Esc", got)
	}
}

// TestSetSearchStatusLiveKeepsHintBeforeDetail pins the real user
// report renderSearchStatus's own doc comment describes: with the Esc
// hint appended at the very end (setSearchStatus's own shape), a
// continuously changing, variable-length detail (searchCurrentPos)
// pushed the hint's own on-screen position left and right on every
// single live update. setSearchStatusLive instead keeps the hint
// sandwiched right after prefix, before detail, so the hint's own
// position stays fixed regardless of how long detail currently is.
func TestSetSearchStatusLiveKeepsHintBeforeDetail(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()

	r.setSearchStatusLive("|", "/very/long/currently/scanning/path")
	got := r.panel.header.GetText(true)

	hintIdx := strings.Index(got, "Esc")
	detailIdx := strings.Index(got, "/very/long/currently/scanning/path")
	if hintIdx < 0 || detailIdx < 0 {
		t.Fatalf("status = %q, want both the Esc hint and the detail present", got)
	}
	if hintIdx > detailIdx {
		t.Errorf("status = %q, want the Esc hint before the detail, not after", got)
	}
	if !strings.HasPrefix(got, "|") {
		t.Errorf("status = %q, want it to start with prefix %q", got, "|")
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

// TestChoiceSpanSelectsOption pins the choice group mechanism shared by
// Engine and the various checkboxes: activating one of a group's own
// spans selects it (updates the relevant *Idx/bool field) and
// re-renders. Uses Find recursively rather than Engine's own
// find/locate choice — always present regardless of host, unlike
// locate (see search.LocateAvailable's own doc comment).
func TestChoiceSpanSelectsOption(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()

	if !r.searchRecursive {
		t.Fatal("setup: searchRecursive = false, want true (MC's own default — see newSearchDialog)")
	}

	idx := -1
	for i, s := range r.searchSpans {
		if s.widget != r.searchLeft {
			continue
		}
		text, _ := textAtSpan(r, s)
		if text == "● Find recursively" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("setup: no span for Find recursively found")
	}
	r.searchSpans[idx].activate()

	if r.searchRecursive {
		t.Error("searchRecursive = true after activating Find recursively, want false")
	}
}

// TestRerenderSearchDialogHintsAtEcryptfsNextToLocate pins the user's
// own explicit request: a plain-text (non-interactive) reminder next
// to the locate option itself, not just in a result screen's own
// wording, since locate silently finds nothing at all under an
// eCryptfs-encrypted home directory (see updatedb's own PRUNEFS) — a
// real user report.
func TestRerenderSearchDialogHintsAtEcryptfsNextToLocate(t *testing.T) {
	if !search.LocateAvailable() {
		t.Skip("locate not available in this environment")
	}
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openSearch()

	got := r.searchTop.GetText(true)
	if !strings.Contains(got, "eCryptfs") {
		t.Errorf("searchTop text = %q, want it to mention eCryptfs next to locate", got)
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
		{"Include zip, tar (gz, bz2, xz)", func() bool { return r.searchIncludeArchives }},
		{"Include compressed files", func() bool { return r.searchIncludeCompressed }},
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

// TestFocusedSearchButtonActivatesViaEnterAndSpace is a regression test
// for a real bug: Cancel/Search, once given real keyboard focus via
// Tab, silently did nothing on Enter or Space. Calling
// captureSearchKey directly (as most of this file's other tests do)
// could never have caught it — the bug was one level up, in *where*
// that capture was installed, not in what it does. It used to sit on
// fields, the shared ancestor of both the span area AND searchButtons;
// tview.Application dispatches every key event to a.root and lets each
// ancestor's own SetInputCapture run before delegating further down
// to whichever descendant actually has focus (see application.go's own
// "Pass other key events to the root primitive" and Box.
// WrapInputHandler) — so captureSearchKey's unconditional KeyEnter/' '
// cases swallowed the event before it ever reached the focused
// button's own SetSelectedFunc/spaceAlsoActivates. Fixed by moving the
// capture down onto spanArea, a sibling of searchButtons rather than
// an ancestor of it (see newSearchDialog's own doc comment).
//
// So this dispatches through r.searchFieldsPages.InputHandler() —
// the real ancestor chain tview.Application itself would use, not a
// direct call to the button's own InputHandler — to actually exercise
// that chain and catch a regression if the capture ever migrates back
// up to an ancestor of the buttons.
func TestFocusedSearchButtonActivatesViaEnterAndSpace(t *testing.T) {
	noop := func(tview.Primitive) {}

	t.Run("Enter on Cancel closes the dialog", func(t *testing.T) {
		dir := fixtureDir(t)
		r, err := NewRoot(tview.NewApplication(), dir)
		if err != nil {
			t.Fatalf("NewRoot: %v", err)
		}
		r.openSearch()
		r.setSearchFocus(len(r.searchSpans)) // Cancel

		r.searchFieldsPages.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), noop)

		if r.activePage != "" {
			t.Errorf("activePage = %q after Enter on a focused Cancel, want closed", r.activePage)
		}
	})

	t.Run("Space on Cancel closes the dialog", func(t *testing.T) {
		dir := fixtureDir(t)
		r, err := NewRoot(tview.NewApplication(), dir)
		if err != nil {
			t.Fatalf("NewRoot: %v", err)
		}
		r.openSearch()
		r.setSearchFocus(len(r.searchSpans)) // Cancel

		r.searchFieldsPages.InputHandler()(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone), noop)

		if r.activePage != "" {
			t.Errorf("activePage = %q after Space on a focused Cancel, want closed", r.activePage)
		}
	})

	t.Run("Enter on Search runs the search", func(t *testing.T) {
		dir := fixtureDir(t)
		r, err := NewRoot(tview.NewApplication(), dir)
		if err != nil {
			t.Fatalf("NewRoot: %v", err)
		}
		var captured search.Request
		isolateSearchRun(t, fakeSearchRun(&captured))
		r.openSearch()
		r.searchFilenameValue = "anything"
		r.setSearchFocus(len(r.searchSpans) + 1) // Search

		r.searchFieldsPages.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), noop)

		if !r.panel.searchMode {
			t.Error("panel.searchMode = false after Enter on a focused Search, want true")
		}
	})

	t.Run("Space on Search runs the search", func(t *testing.T) {
		dir := fixtureDir(t)
		r, err := NewRoot(tview.NewApplication(), dir)
		if err != nil {
			t.Fatalf("NewRoot: %v", err)
		}
		var captured search.Request
		isolateSearchRun(t, fakeSearchRun(&captured))
		r.openSearch()
		r.searchFilenameValue = "anything"
		r.setSearchFocus(len(r.searchSpans) + 1) // Search

		r.searchFieldsPages.InputHandler()(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone), noop)

		if !r.panel.searchMode {
			t.Error("panel.searchMode = false after Space on a focused Search, want true")
		}
	})
}

// TestEscapeClosesSearchRegardlessOfFocus pins the user's own explicit
// request: Escape means Cancel in this dialog, the same as it already
// does in Properties (see capturePropertiesKey/newPropertiesButtons'
// own identical SetExitFunc cases) — no matter which of the three
// different places real keyboard focus might currently be (a span/
// checkbox/field, reached via captureSearchKey; Cancel or Search
// itself, reached via each button's own SetExitFunc instead, the exact
// same "ancestor capture never sees a focused button's own keys" split
// TestFocusedSearchButtonActivatesViaEnterAndSpace already pins for
// Enter/Space).
func TestEscapeClosesSearchRegardlessOfFocus(t *testing.T) {
	noop := func(tview.Primitive) {}

	t.Run("Escape on a span closes the dialog", func(t *testing.T) {
		dir := fixtureDir(t)
		r, err := NewRoot(tview.NewApplication(), dir)
		if err != nil {
			t.Fatalf("NewRoot: %v", err)
		}
		r.openSearch() // first focus is the Filename span — see its own doc comment

		r.searchFieldsPages.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), noop)

		if r.activePage != "" {
			t.Errorf("activePage = %q after Escape on a focused span, want closed", r.activePage)
		}
	})

	t.Run("Escape on Cancel closes the dialog", func(t *testing.T) {
		dir := fixtureDir(t)
		r, err := NewRoot(tview.NewApplication(), dir)
		if err != nil {
			t.Fatalf("NewRoot: %v", err)
		}
		r.openSearch()
		r.setSearchFocus(len(r.searchSpans)) // Cancel

		r.searchFieldsPages.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), noop)

		if r.activePage != "" {
			t.Errorf("activePage = %q after Escape on a focused Cancel, want closed", r.activePage)
		}
	})

	t.Run("Escape on Search closes the dialog without running it", func(t *testing.T) {
		dir := fixtureDir(t)
		r, err := NewRoot(tview.NewApplication(), dir)
		if err != nil {
			t.Fatalf("NewRoot: %v", err)
		}
		searchRunCalled := false
		isolateSearchRun(t, func(context.Context, search.Request) (<-chan search.Result, <-chan error) {
			searchRunCalled = true
			return make(chan search.Result), make(chan error)
		})
		r.openSearch()
		r.searchFilenameValue = "anything"
		r.setSearchFocus(len(r.searchSpans) + 1) // Search

		r.searchFieldsPages.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), noop)

		if r.activePage != "" {
			t.Errorf("activePage = %q after Escape on a focused Search, want closed", r.activePage)
		}
		if searchRunCalled {
			t.Error("Escape on a focused Search should close the dialog, not run the search")
		}
	})
}

// TestSearchEngineChangeDimsScopeField pins the user's own request:
// Start at reads as visibly unavailable while Engine=locate (it no
// longer affects locate's own results — see search.Request.Scope's own
// doc comment) — via dimTag in the rendered text, not (like the
// previous, tview.Form-based version of this dialog) a SetDisabled
// call whose color change tview's own Form.Draw silently discarded
// every frame. Content has no such dimming any more — see
// rerenderSearchDialog's own doc comment on its column.
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

// Content's own field is no longer ever dimmed (there's no more
// "Search in" choice gating it — see rerenderSearchDialog's own doc
// comment on the Content column), so the test that used to pin its
// dimming behavior across a Search in change is gone; nothing replaces
// it, since "always editable" needs no test of its own beyond
// TestRunSearchUsesContentValueWhenContentFilled already covering the
// behavior that matters (a filled Content field selects content
// search).
