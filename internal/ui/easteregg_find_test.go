package ui

import (
	"context"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/search"
)

func TestRunSearchAppendsEasterEggResultForExactTerm(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchFilenameValue = "easteregg"
	r.runSearch()

	found := false
	for _, e := range r.panel.searchEntries {
		if e.Name == easterEggSentinelPath {
			found = true
			if e.display != easterEggSentinelLabel {
				t.Errorf("display = %q, want %q", e.display, easterEggSentinelLabel)
			}
		}
	}
	if !found {
		t.Error("searching for \"easteregg\" should append the sentinel row")
	}
	// Filename search still ran for real too — the real file search
	// isn't replaced, just supplemented (see runSearch's own comment).
	if captured.Pattern != "easteregg" {
		t.Errorf("Pattern passed to the real search = %q, want unchanged %q", captured.Pattern, "easteregg")
	}
}

func TestRunSearchEasterEggTermIsCaseInsensitiveAndTrimmed(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	var captured search.Request
	isolateSearchRun(t, fakeSearchRun(&captured))

	r.openSearch()
	r.searchFilenameValue = "  EasterEgg  "
	r.runSearch()

	found := false
	for _, e := range r.panel.searchEntries {
		if e.Name == easterEggSentinelPath {
			found = true
		}
	}
	if !found {
		t.Error("\"  EasterEgg  \" should still trigger the sentinel row (case/whitespace-insensitive)")
	}
}

func TestRunSearchDoesNotAppendEasterEggResultForOtherTerms(t *testing.T) {
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

	for _, e := range r.panel.searchEntries {
		if e.Name == easterEggSentinelPath {
			t.Error("an unrelated search term should never append the sentinel row")
		}
	}
}

func TestRunSearchDoesNotTriggerOnAContentOnlySearchForTheTerm(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	isolateSearchRun(t, func(context.Context, search.Request) (<-chan search.Result, <-chan error) {
		return make(chan search.Result), make(chan error)
	})

	r.openSearch()
	r.searchContentValue = "easteregg" // Content, not Filename
	r.runSearch()

	for _, e := range r.panel.searchEntries {
		if e.Name == easterEggSentinelPath {
			t.Error("\"easteregg\" in the Content field (not Filename) should not trigger the sentinel row")
		}
	}
}

func TestActivateRowOnEasterEggSentinelOpensTheEggViaEnter(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.showSearchResults()
	r.panel.appendEasterEggSearchResult()

	r.panel.activateRow(0)

	if r.activePage != easterEggPage {
		t.Errorf("activePage = %q, want %q after activating the sentinel row", r.activePage, easterEggPage)
	}
	r.closeEasterEgg()
}

func TestOpenLookOnEasterEggSentinelOpensTheEgg(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.showSearchResults()
	r.panel.appendEasterEggSearchResult()
	r.panel.table.Select(0, 0)

	r.openLook()

	if r.activePage != easterEggPage {
		t.Errorf("activePage = %q, want %q after \"l\" on the sentinel row", r.activePage, easterEggPage)
	}
	r.closeEasterEgg()
}

func TestEasterEggSentinelIconFlipsWithFileIcons(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.showSearchResults()
	r.panel.appendEasterEggSearchResult()

	r.panel.fileIcons = true
	r.panel.renderSearchEntries()
	iconCell := r.panel.table.GetCell(0, colType)
	if iconCell.Text != "🐇" {
		t.Errorf("type cell with fileIcons on = %q, want the rabbit glyph", iconCell.Text)
	}

	r.panel.fileIcons = false
	r.panel.renderSearchEntries()
	iconCell = r.panel.table.GetCell(0, colType)
	if iconCell.Text != "╨" {
		t.Errorf("type cell with fileIcons off = %q, want the ╨ fallback", iconCell.Text)
	}
}

func TestLoadDetailsTargetShowsFakeBlockForEasterEggSentinel(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.detailsSidebarVisible = true

	r.loadDetailsTarget(easterEggSentinelPath)

	if got := r.detailsSidebar.GetText(true); got != easterEggDetailsText {
		t.Errorf("Details text = %q, want the fixed fake block %q", got, easterEggDetailsText)
	}
	if r.detailsStatErr != nil {
		t.Errorf("detailsStatErr = %v, want nil — the sentinel should never be stat'd for real", r.detailsStatErr)
	}
}

// TestPropertiesOnEasterEggSentinelDoesNotPanic pins the one stray-
// action case the sentinel path's own non-existence could plausibly
// crash on — Properties always goes through a real stat, which must
// degrade (a plain error dialog) rather than panic, the same "degrade,
// don't crash" contract the rest of this app already follows.
func TestPropertiesOnEasterEggSentinelDoesNotPanic(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.showSearchResults()
	r.panel.appendEasterEggSearchResult()
	r.panel.table.Select(0, 0)

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("openProperties on the sentinel row panicked: %v", rec)
		}
	}()
	r.openProperties()

	if r.activePage == propertiesPage {
		t.Error("Properties should not actually open for a path that was never real")
	}
}
