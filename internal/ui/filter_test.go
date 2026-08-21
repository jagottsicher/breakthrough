package ui

import (
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/rivo/tview"
)

func TestFilterModeLabel(t *testing.T) {
	if got := filterModeLabel(false); got != "Glob" {
		t.Errorf("filterModeLabel(false) = %q, want %q", got, "Glob")
	}
	if got := filterModeLabel(true); got != "Regex" {
		t.Errorf("filterModeLabel(true) = %q, want %q", got, "Regex")
	}
}

func entryNames(entries []fsops.Entry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names
}

func TestFilterByTextEmptyIsNoop(t *testing.T) {
	entries := []fsops.Entry{{Name: "apple.txt"}, {Name: "banana.txt"}}
	got := filterByText(entries, "", false)
	if len(got) != 2 {
		t.Errorf("filterByText with empty filterText = %v, want all entries kept", entryNames(got))
	}
}

func TestFilterByTextGlobMode(t *testing.T) {
	entries := []fsops.Entry{{Name: "apple.txt"}, {Name: "apricot.txt"}, {Name: "banana.txt"}}

	got := filterByText(entries, "*.txt", false)
	if len(got) != 3 {
		t.Errorf("filterByText(*.txt) = %v, want all 3 kept", entryNames(got))
	}

	got = filterByText(entries, "ap*", false)
	want := []string{"apple.txt", "apricot.txt"}
	if len(got) != len(want) || got[0].Name != want[0] || got[1].Name != want[1] {
		t.Errorf("filterByText(ap*) = %v, want %v", entryNames(got), want)
	}

	got = filterByText(entries, "banana.txt", false)
	if len(got) != 1 || got[0].Name != "banana.txt" {
		t.Errorf("filterByText(banana.txt) (exact, no wildcard) = %v, want just banana.txt", entryNames(got))
	}

	// No wildcard, not an exact name either: filepath.Match anchors the
	// whole name, the same as Select+/- already relies on — "an"
	// (contained in "banana.txt") should not match it.
	got = filterByText(entries, "an", false)
	if len(got) != 0 {
		t.Errorf("filterByText(an) = %v, want none — glob mode is anchored, not substring", entryNames(got))
	}
}

func TestFilterByTextGlobInvalidPatternKeepsEverything(t *testing.T) {
	entries := []fsops.Entry{{Name: "apple.txt"}, {Name: "banana.txt"}}
	got := filterByText(entries, "[", false) // unterminated character class
	if len(got) != len(entries) {
		t.Errorf("filterByText([) = %v, want every entry kept (malformed pattern treated as no filter yet)", entryNames(got))
	}
}

func TestFilterByTextRegexMode(t *testing.T) {
	entries := []fsops.Entry{{Name: "apple.txt"}, {Name: "apricot.txt"}, {Name: "banana.txt"}}

	got := filterByText(entries, "^ap", true)
	want := []string{"apple.txt", "apricot.txt"}
	if len(got) != len(want) || got[0].Name != want[0] || got[1].Name != want[1] {
		t.Errorf("filterByText(^ap, regex) = %v, want %v", entryNames(got), want)
	}

	// Unlike glob mode, regexp.MatchString is unanchored by default —
	// substring matching is exactly what a bare regex like "an" does.
	got = filterByText(entries, "an", true)
	if len(got) != 1 || got[0].Name != "banana.txt" {
		t.Errorf("filterByText(an, regex) = %v, want just banana.txt", entryNames(got))
	}
}

func TestFilterByTextRegexInvalidPatternKeepsEverything(t *testing.T) {
	entries := []fsops.Entry{{Name: "apple.txt"}, {Name: "banana.txt"}}
	got := filterByText(entries, "(unclosed", true)
	if len(got) != len(entries) {
		t.Errorf("filterByText((unclosed, regex) = %v, want every entry kept (invalid regex treated as no filter yet)", entryNames(got))
	}
}

// TestFilterFieldNarrowsListingLive pins the actual end-to-end wiring:
// typing into filterField (via SetChangedFunc, not just on Enter/Tab)
// reloads the panel with the filter applied immediately.
func TestFilterFieldNarrowsListingLive(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.panel.filterField.SetText("apr*")

	rowCount := r.panel.table.GetRowCount()
	// ".." (not filtered, always kept) + apricot.txt — "apr*" is
	// deliberately specific enough not to also catch app-data or
	// apple.txt, unlike a broader "ap*" would.
	if rowCount != 2 {
		t.Fatalf("row count after filtering to \"apr*\" = %d, want 2 (.., apricot.txt)", rowCount)
	}
	names := make(map[string]bool)
	for row := 0; row < rowCount; row++ {
		ref, ok := r.panel.rowRef(row)
		if !ok {
			continue
		}
		names[ref.name] = true
	}
	for _, want := range []string{"..", "apricot.txt"} {
		if !names[want] {
			t.Errorf("filtered listing missing %q, got rows %v", want, names)
		}
	}
	if names["banana.txt"] || names["app-data"] || names["apple.txt"] {
		t.Errorf("filtered listing should not include non-matching entries, got rows %v", names)
	}
}

// TestFilterRegexToggleFlipsModeAndRelabels pins that clicking the
// regex-toggle button switches filterByText's interpretation and
// updates its own label to reflect the new mode.
func TestFilterRegexToggleFlipsModeAndRelabels(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	if r.panel.filterRegex {
		t.Fatal("setup: should start in glob mode")
	}
	if got := r.panel.filterRegexBtn.GetLabel(); got != "Glob" {
		t.Fatalf("setup: button label = %q, want %q", got, "Glob")
	}

	r.panel.filterField.SetText("^ap") // a pattern that's valid regex but not a glob match for anything here
	r.panel.filterRegexBtn.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if !r.panel.filterRegex {
		t.Error("filterRegex should be true after clicking the toggle")
	}
	if got := r.panel.filterRegexBtn.GetLabel(); got != "Regex" {
		t.Errorf("button label = %q, want %q", got, "Regex")
	}

	names := make(map[string]bool)
	for row := 0; row < r.panel.table.GetRowCount(); row++ {
		if ref, ok := r.panel.rowRef(row); ok {
			names[ref.name] = true
		}
	}
	if !names["apple.txt"] || !names["apricot.txt"] || names["banana.txt"] {
		t.Errorf("after switching to regex mode, \"^ap\" should match apple.txt/apricot.txt only, got %v", names)
	}
}

// TestFilterResetsOnNavigationButNotOnSameDirectoryRefresh pins the two
// halves of load()'s own reset rule: moving to a different directory
// clears the filter (both the field's text and Panel.filterText),
// while reloading the same directory (e.g. toggling hidden files)
// leaves it exactly as typed.
func TestFilterResetsOnNavigationButNotOnSameDirectoryRefresh(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.panel.filterField.SetText("ap*")
	if r.panel.filterText != "ap*" {
		t.Fatalf("setup: filterText = %q, want %q", r.panel.filterText, "ap*")
	}

	// Same-directory refresh (what toggling hidden files does under the
	// hood) must not touch the filter.
	if err := r.panel.load(r.panel.path); err != nil {
		t.Fatalf("load (refresh): %v", err)
	}
	if r.panel.filterText != "ap*" {
		t.Errorf("filterText after a same-directory refresh = %q, want unchanged %q", r.panel.filterText, "ap*")
	}
	if got := r.panel.filterField.GetText(); got != "ap*" {
		t.Errorf("filterField text after a same-directory refresh = %q, want unchanged %q", got, "ap*")
	}

	// Navigating to a different directory must clear it.
	sub := filepath.Join(dir, "app-data")
	if err := r.panel.navigate(sub); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if r.panel.filterText != "" {
		t.Errorf("filterText after navigating to a new directory = %q, want cleared", r.panel.filterText)
	}
	if got := r.panel.filterField.GetText(); got != "" {
		t.Errorf("filterField text after navigating to a new directory = %q, want cleared", got)
	}
}

// TestFilterFieldDoneReturnsFocusToTableWithoutClearing pins that
// Enter/Escape in the filter field just returns focus to the table —
// the filter itself stays applied, since it's meant to be a persistent
// narrowing of the view, not a one-shot search box that clears itself.
func TestFilterFieldDoneReturnsFocusToTableWithoutClearing(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.panel.filterField.SetText("ap*")
	r.app.SetFocus(r.panel.filterField)

	r.panel.filterField.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if got := r.panel.filterField.GetText(); got != "ap*" {
		t.Errorf("filterField text after Enter = %q, want unchanged %q", got, "ap*")
	}
	if !r.panel.table.HasFocus() {
		t.Error("focus should return to the table after Enter in the filter field")
	}
}
