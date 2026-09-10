package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
	got := filterByText(entries, "", false, true)
	if len(got) != 2 {
		t.Errorf("filterByText with empty filterText = %v, want all entries kept", entryNames(got))
	}
}

func TestFilterByTextGlobMode(t *testing.T) {
	entries := []fsops.Entry{{Name: "apple.txt"}, {Name: "apricot.txt"}, {Name: "banana.txt"}}

	got := filterByText(entries, "*.txt", false, true)
	if len(got) != 3 {
		t.Errorf("filterByText(*.txt) = %v, want all 3 kept", entryNames(got))
	}

	got = filterByText(entries, "ap*", false, true)
	want := []string{"apple.txt", "apricot.txt"}
	if len(got) != len(want) || got[0].Name != want[0] || got[1].Name != want[1] {
		t.Errorf("filterByText(ap*) = %v, want %v", entryNames(got), want)
	}

	got = filterByText(entries, "banana.txt", false, true)
	if len(got) != 1 || got[0].Name != "banana.txt" {
		t.Errorf("filterByText(banana.txt) (exact, no wildcard) = %v, want just banana.txt", entryNames(got))
	}

	// No wildcard, not an exact name either: filepath.Match anchors the
	// whole name, the same as Select+/- already relies on — "an"
	// (contained in "banana.txt") should not match it.
	got = filterByText(entries, "an", false, true)
	if len(got) != 0 {
		t.Errorf("filterByText(an) = %v, want none — glob mode is anchored, not substring", entryNames(got))
	}
}

// TestFilterByTextInactiveIsNoopEvenWithText pins the filter-menu's own
// "disable without clearing" checkbox (see Panel.filterGlobActive's own
// doc comment): active false is a no-op regardless of how real a
// pattern filterText holds, the same as if it were empty.
func TestFilterByTextInactiveIsNoopEvenWithText(t *testing.T) {
	entries := []fsops.Entry{{Name: "apple.txt"}, {Name: "apricot.txt"}, {Name: "banana.txt"}}
	got := filterByText(entries, "ap*", false, false)
	if len(got) != len(entries) {
		t.Errorf("filterByText with active=false = %v, want every entry kept despite a real pattern", entryNames(got))
	}
}

func TestFilterByTextGlobInvalidPatternKeepsEverything(t *testing.T) {
	entries := []fsops.Entry{{Name: "apple.txt"}, {Name: "banana.txt"}}
	got := filterByText(entries, "[", false, true) // unterminated character class
	if len(got) != len(entries) {
		t.Errorf("filterByText([) = %v, want every entry kept (malformed pattern treated as no filter yet)", entryNames(got))
	}
}

func TestFilterByTextRegexMode(t *testing.T) {
	entries := []fsops.Entry{{Name: "apple.txt"}, {Name: "apricot.txt"}, {Name: "banana.txt"}}

	got := filterByText(entries, "^ap", true, true)
	want := []string{"apple.txt", "apricot.txt"}
	if len(got) != len(want) || got[0].Name != want[0] || got[1].Name != want[1] {
		t.Errorf("filterByText(^ap, regex) = %v, want %v", entryNames(got), want)
	}

	// Unlike glob mode, regexp.MatchString is unanchored by default —
	// substring matching is exactly what a bare regex like "an" does.
	got = filterByText(entries, "an", true, true)
	if len(got) != 1 || got[0].Name != "banana.txt" {
		t.Errorf("filterByText(an, regex) = %v, want just banana.txt", entryNames(got))
	}
}

func TestFilterByTextRegexInvalidPatternKeepsEverything(t *testing.T) {
	entries := []fsops.Entry{{Name: "apple.txt"}, {Name: "banana.txt"}}
	got := filterByText(entries, "(unclosed", true, true)
	if len(got) != len(entries) {
		t.Errorf("filterByText((unclosed, regex) = %v, want every entry kept (invalid regex treated as no filter yet)", entryNames(got))
	}
}

func TestFilterBySizeEmptyIsNoop(t *testing.T) {
	entries := []fsops.Entry{{Name: "small.txt", Size: 10}, {Name: "big.txt", Size: 10_000_000}}
	got := filterBySize(entries, "", true)
	if len(got) != 2 {
		t.Errorf("filterBySize with an empty expression = %v, want all entries kept", entryNames(got))
	}
}

func TestFilterBySizeInactiveIsNoopEvenWithExpression(t *testing.T) {
	entries := []fsops.Entry{{Name: "small.txt", Size: 10}, {Name: "big.txt", Size: 10_000_000}}
	got := filterBySize(entries, "> 1m", false)
	if len(got) != 2 {
		t.Errorf("filterBySize with active=false = %v, want every entry kept despite a real expression", entryNames(got))
	}
}

func TestFilterBySizeNarrowsByComparison(t *testing.T) {
	entries := []fsops.Entry{
		{Name: "small.txt", Size: 10},
		{Name: "medium.txt", Size: 500 * 1024},
		{Name: "big.txt", Size: 5 * 1024 * 1024},
	}
	got := filterBySize(entries, "> 1m", true)
	if len(got) != 1 || got[0].Name != "big.txt" {
		t.Errorf("filterBySize(> 1m) = %v, want just big.txt", entryNames(got))
	}
}

func TestFilterBySizeInvalidExpressionKeepsEverything(t *testing.T) {
	entries := []fsops.Entry{{Name: "small.txt", Size: 10}, {Name: "big.txt", Size: 10_000_000}}
	// ">" alone (no number yet) is exactly the mid-keystroke shape this
	// no-op contract exists for — someone about to type "> 1m" passes
	// through this incomplete state on the way there.
	got := filterBySize(entries, ">", true)
	if len(got) != len(entries) {
		t.Errorf("filterBySize(>) = %v, want every entry kept (incomplete expression treated as no filter yet)", entryNames(got))
	}
}

func TestFilterByMtimeEmptyIsNoop(t *testing.T) {
	now := time.Now()
	entries := []fsops.Entry{{Name: "old.txt", ModTime: now.Add(-365 * 24 * time.Hour)}, {Name: "new.txt", ModTime: now}}
	got := filterByMtime(entries, "", true, now)
	if len(got) != 2 {
		t.Errorf("filterByMtime with an empty expression = %v, want all entries kept", entryNames(got))
	}
}

func TestFilterByMtimeInactiveIsNoopEvenWithExpression(t *testing.T) {
	now := time.Now()
	entries := []fsops.Entry{{Name: "old.txt", ModTime: now.Add(-365 * 24 * time.Hour)}, {Name: "new.txt", ModTime: now}}
	got := filterByMtime(entries, "last 7 days", false, now)
	if len(got) != 2 {
		t.Errorf("filterByMtime with active=false = %v, want every entry kept despite a real expression", entryNames(got))
	}
}

func TestFilterByMtimeNarrowsByComparison(t *testing.T) {
	now := time.Now()
	entries := []fsops.Entry{
		{Name: "old.txt", ModTime: now.Add(-365 * 24 * time.Hour)},
		{Name: "recent.txt", ModTime: now.Add(-2 * 24 * time.Hour)},
	}
	got := filterByMtime(entries, "last 7 days", true, now)
	if len(got) != 1 || got[0].Name != "recent.txt" {
		t.Errorf("filterByMtime(last 7 days) = %v, want just recent.txt", entryNames(got))
	}
}

func TestFilterByMtimeInvalidExpressionKeepsEverything(t *testing.T) {
	now := time.Now()
	entries := []fsops.Entry{{Name: "old.txt", ModTime: now.Add(-365 * 24 * time.Hour)}, {Name: "new.txt", ModTime: now}}
	got := filterByMtime(entries, "sometime soon-ish", true, now)
	if len(got) != len(entries) {
		t.Errorf("filterByMtime(garbage) = %v, want every entry kept (unparseable expression treated as no filter yet)", entryNames(got))
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

// TestSizeFieldNarrowsListingLive is the size row's own counterpart to
// TestFilterFieldNarrowsListingLive: typing into the dropdown's real
// InputField (via SetChangedFunc — see newFilterMenuFieldRow) reloads
// the panel with the size filter applied immediately, auto-activating
// its own checkbox exactly like typing into the glob field does.
func TestSizeFieldNarrowsListingLive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "small.txt"), make([]byte, 10), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), make([]byte, 5*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openFilterMenu()
	filterMenuFieldRowField(t, r, 2).SetText("> 1m")

	if !r.panel.filterSizeActive {
		t.Fatal("typing into the size field should auto-activate it")
	}
	rowCount := r.panel.table.GetRowCount()
	if rowCount != 2 { // ".." + big.txt
		t.Fatalf("row count after filtering to \"> 1m\" = %d, want 2 (.., big.txt)", rowCount)
	}
	ref, ok := r.panel.rowRef(1)
	if !ok || ref.name != "big.txt" {
		t.Errorf("row 1 = %+v, ok=%v, want big.txt", ref, ok)
	}
}

// TestMtimeFieldNarrowsListingLive is the modified-time row's own
// counterpart to TestFilterFieldNarrowsListingLive.
func TestMtimeFieldNarrowsListingLive(t *testing.T) {
	dir := t.TempDir()
	oldFile := filepath.Join(dir, "old.txt")
	newFile := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(oldFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	longAgo := time.Now().Add(-365 * 24 * time.Hour)
	if err := os.Chtimes(oldFile, longAgo, longAgo); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openFilterMenu()
	filterMenuFieldRowField(t, r, 3).SetText("last 7 days")

	if !r.panel.filterMtimeActive {
		t.Fatal("typing into the modified-time field should auto-activate it")
	}
	rowCount := r.panel.table.GetRowCount()
	if rowCount != 2 { // ".." + new.txt
		t.Fatalf("row count after filtering to \"last 7 days\" = %d, want 2 (.., new.txt)", rowCount)
	}
	ref, ok := r.panel.rowRef(1)
	if !ok || ref.name != "new.txt" {
		t.Errorf("row 1 = %+v, ok=%v, want new.txt", ref, ok)
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

// TestFilterPersistsAcrossNavigationByDefault pins the user's own
// explicit request (config.Settings.FilterPersistent's own doc
// comment): browsing several directories in a row with the same
// filter switched on is the default — navigating to a genuinely
// different directory must NOT clear the filter (neither the field's
// text nor Panel.filterText) the way it always used to.
func TestFilterPersistsAcrossNavigationByDefault(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	if !r.panel.filterPersistent {
		t.Fatal("setup: filterPersistent should default to true")
	}

	r.panel.filterField.SetText("ap*")
	if r.panel.filterText != "ap*" {
		t.Fatalf("setup: filterText = %q, want %q", r.panel.filterText, "ap*")
	}
	r.panel.filterSizeActive = true
	r.panel.filterSizeText = "> 1m"
	r.panel.filterMtimeActive = true
	r.panel.filterMtimeText = "last 7 days"

	sub := filepath.Join(dir, "app-data")
	if err := r.panel.navigate(sub); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if r.panel.filterText != "ap*" {
		t.Errorf("filterText after navigating to a new directory = %q, want unchanged %q (persistent by default)", r.panel.filterText, "ap*")
	}
	if got := r.panel.filterField.GetText(); got != "ap*" {
		t.Errorf("filterField text after navigating to a new directory = %q, want unchanged %q", got, "ap*")
	}
	if !r.panel.filterSizeActive || r.panel.filterSizeText != "> 1m" {
		t.Errorf("size filter after navigating = active %v, text %q, want unchanged (active, %q)", r.panel.filterSizeActive, r.panel.filterSizeText, "> 1m")
	}
	if !r.panel.filterMtimeActive || r.panel.filterMtimeText != "last 7 days" {
		t.Errorf("modified-time filter after navigating = active %v, text %q, want unchanged (active, %q)", r.panel.filterMtimeActive, r.panel.filterMtimeText, "last 7 days")
	}
}

// TestFilterResetsOnNavigationWhenNotPersistent pins the opt-out (see
// config.Settings.FilterPersistent's own doc comment for the "off"
// half): a same-directory refresh (what toggling hidden files does
// under the hood) must still not touch the filter regardless of
// filterPersistent, but navigating to a genuinely different directory
// now clears it — restoring the original, pre-this-setting behavior
// for anyone who prefers it.
func TestFilterResetsOnNavigationWhenNotPersistent(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.filterPersistent = false

	r.panel.filterField.SetText("ap*")
	if r.panel.filterText != "ap*" {
		t.Fatalf("setup: filterText = %q, want %q", r.panel.filterText, "ap*")
	}
	r.panel.filterSizeActive = true
	r.panel.filterSizeText = "> 1m"
	r.panel.filterMtimeActive = true
	r.panel.filterMtimeText = "last 7 days"

	// Same-directory refresh (what toggling hidden files does under the
	// hood) must not touch the filter either way.
	if err := r.panel.load(r.panel.path); err != nil {
		t.Fatalf("load (refresh): %v", err)
	}
	if r.panel.filterText != "ap*" {
		t.Errorf("filterText after a same-directory refresh = %q, want unchanged %q", r.panel.filterText, "ap*")
	}
	if got := r.panel.filterField.GetText(); got != "ap*" {
		t.Errorf("filterField text after a same-directory refresh = %q, want unchanged %q", got, "ap*")
	}
	if r.panel.filterSizeText != "> 1m" || r.panel.filterMtimeText != "last 7 days" {
		t.Errorf("size/modified-time text after a same-directory refresh = %q/%q, want unchanged", r.panel.filterSizeText, r.panel.filterMtimeText)
	}

	// Navigating to a different directory must clear it, since
	// filterPersistent is false here.
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
	if r.panel.filterSizeActive || r.panel.filterSizeText != "" {
		t.Errorf("size filter after navigating = active %v, text %q, want cleared", r.panel.filterSizeActive, r.panel.filterSizeText)
	}
	if r.panel.filterMtimeActive || r.panel.filterMtimeText != "" {
		t.Errorf("modified-time filter after navigating = active %v, text %q, want cleared", r.panel.filterMtimeActive, r.panel.filterMtimeText)
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

	// filterField's own SetDoneFunc is only ever installed by
	// renderFilterMenu (see its own doc comment for why) — going
	// through the real openFilterMenu, not setting text/focus directly,
	// is what actually wires it for this test the same way a real "/"
	// press or "Y" click would.
	r.openFilterMenu()
	r.panel.filterField.SetText("ap*")

	r.panel.filterField.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if got := r.panel.filterField.GetText(); got != "ap*" {
		t.Errorf("filterField text after Enter = %q, want unchanged %q", got, "ap*")
	}
	if r.activePage == filterMenuPage {
		t.Error("Enter in the filter field should close the dropdown, not just move focus")
	}
	if !r.panel.table.HasFocus() {
		t.Error("focus should return to the table after Enter closes the filter menu")
	}
}
