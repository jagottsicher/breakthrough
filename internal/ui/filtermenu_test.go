package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// filterMenuRow retrieves one of filterMenuLayout's own rows by index —
// 0 is the title bar, 1 the glob/regex row (itself a Flex — see
// filterMenuGlobCheckbox), 2 size, 3 modified-time — the same
// GetItem-based access every test below uses instead of needing extra
// Root fields for widgets renderFilterMenu otherwise only ever keeps as
// local variables.
func filterMenuRow(t *testing.T, r *Root, index int) tview.Primitive {
	t.Helper()
	if index >= r.filterMenuLayout.GetItemCount() {
		t.Fatalf("filterMenuLayout has %d items, want at least %d", r.filterMenuLayout.GetItemCount(), index+1)
	}
	return r.filterMenuLayout.GetItem(index)
}

// filterMenuGlobCheckbox retrieves the glob row's own checkbox TextView
// — the row itself is a Flex (checkbox + filterRegexBtn + filterField),
// so this reaches one level deeper than filterMenuRow alone.
func filterMenuGlobCheckbox(t *testing.T, r *Root) *tview.TextView {
	t.Helper()
	globRow, ok := filterMenuRow(t, r, 1).(*tview.Flex)
	if !ok {
		t.Fatal("filterMenuLayout item 1 is not a *tview.Flex (the glob row)")
	}
	checkbox, ok := globRow.GetItem(0).(*tview.TextView)
	if !ok {
		t.Fatal("glob row item 0 is not a *tview.TextView (the checkbox)")
	}
	return checkbox
}

// click simulates a real left-click on p by going through its own
// MouseHandler — the same "call the actual dispatch path, not the
// closure directly" approach TestCheckboxClickTogglesDisplay already
// establishes for the panel's own checkbox column. The position
// doesn't matter here: every mouse capture in this file only checks
// the action, never where exactly it landed (see
// Root.newFilterMenuToggleRow's own doc comment on why a whole row is
// one click target).
func click(p tview.Primitive) {
	handler := p.MouseHandler()
	handler(tview.MouseLeftClick, tcell.NewEventMouse(0, 0, tcell.Button1, 0), func(tview.Primitive) {})
}

// TestOpenFilterMenuShowsAllThreeRows pins openFilterMenu's own basic
// contract: activePage switches to filterMenuPage, and the layout ends
// up with a title bar plus all three rows (glob/regex, size,
// modified-time), each already reflecting the active panel's own
// current toggle state.
func TestOpenFilterMenuShowsAllThreeRows(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openFilterMenu()

	if r.activePage != filterMenuPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, filterMenuPage)
	}
	if got := r.filterMenuLayout.GetItemCount(); got != 4 {
		t.Fatalf("filterMenuLayout has %d items, want 4 (title bar + 3 rows)", got)
	}
	if got, want := r.filterMenuTitleBar.GetText(true), " Filters "; got != want {
		t.Errorf("filterMenuTitleBar text = %q, want %q", got, want)
	}

	checkbox := filterMenuGlobCheckbox(t, r)
	if got, want := checkbox.GetText(true), checkboxText(true); got != want {
		t.Errorf("glob checkbox = %q, want %q (filterGlobActive defaults to true)", got, want)
	}

	sizeRow, ok := filterMenuRow(t, r, 2).(*tview.TextView)
	if !ok {
		t.Fatal("filterMenuLayout item 2 is not a *tview.TextView (the size row)")
	}
	if got, want := sizeRow.GetText(true), checkboxText(false)+" Size filter"; got != want {
		t.Errorf("size row = %q, want %q", got, want)
	}

	mtimeRow, ok := filterMenuRow(t, r, 3).(*tview.TextView)
	if !ok {
		t.Fatal("filterMenuLayout item 3 is not a *tview.TextView (the modified-time row)")
	}
	if got, want := mtimeRow.GetText(true), checkboxText(false)+" Modified time filter"; got != want {
		t.Errorf("modified-time row = %q, want %q", got, want)
	}
}

// TestFilterMenuGlobCheckboxTogglesActiveAndReloads pins the
// "disable without clearing" contract filterGlobActive exists for:
// clicking the checkbox flips it, re-renders in place (the dropdown
// stays open — see newFilterMenuToggleRow's own doc comment), and
// actually reloads the panel so the effect is immediate.
func TestFilterMenuGlobCheckboxTogglesActiveAndReloads(t *testing.T) {
	dir := fixtureDir(t) // apple.txt, apricot.txt, banana.txt, app-data/
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.filterField.SetText("apple.txt")
	if got := r.panel.table.GetRowCount(); got != 2 { // ".." + apple.txt
		t.Fatalf("setup: row count = %d, want 2 (filtered to just apple.txt)", got)
	}

	r.openFilterMenu()
	click(filterMenuGlobCheckbox(t, r))

	if r.panel.filterGlobActive {
		t.Error("filterGlobActive should be false after clicking the checkbox once")
	}
	if r.activePage != filterMenuPage {
		t.Error("the dropdown should stay open after toggling one row")
	}
	if got := r.panel.table.GetRowCount(); got != 5 { // ".." + all 4 real entries, filter now inactive
		t.Errorf("row count after disabling the filter = %d, want 5 (every entry back)", got)
	}
}

// TestFilterMenuSizeToggleUpdatesRowAndButtonIndicator pins the two
// stub rows' own contract (see Panel.filterSizeActive's own doc
// comment: no filtering logic yet, just a toggle that shows up in
// filterMenuBtn's own "Nx" count) — and that toggling one doesn't
// disturb the other, or the glob row.
func TestFilterMenuSizeToggleUpdatesRowAndButtonIndicator(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.filterField.SetText("apple.txt") // one real filter already active — see renderFilterMenuBtn's own doc comment on what counts

	r.openFilterMenu()
	sizeRow, ok := filterMenuRow(t, r, 2).(*tview.TextView)
	if !ok {
		t.Fatal("filterMenuLayout item 2 is not a *tview.TextView")
	}
	click(sizeRow)

	if !r.panel.filterSizeActive {
		t.Error("filterSizeActive should be true after clicking its own row")
	}
	if r.panel.filterMtimeActive {
		t.Error("clicking the size row should not affect filterMtimeActive")
	}
	if got, want := sizeRow.GetText(true), checkboxText(true)+" Size filter"; got != want {
		t.Errorf("size row after toggling = %q, want %q", got, want)
	}
	if got, want := r.panel.filterMenuBtn.GetText(true), "2x"; !containsSubstring(got, want) {
		t.Errorf("filterMenuBtn text = %q, want it to contain %q (glob + size both active)", got, want)
	}
}

// containsSubstring avoids importing strings just for one Contains
// call in a test file that otherwise has no use for it.
func containsSubstring(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestRenderFilterMenuBtnOmitsIndicatorWhenNoFiltersActive pins the
// user's own explicit request: no "0x" ever shown, only "1x"/"2x"/"3x"
// once at least one filter-menu row is actually in effect.
func TestRenderFilterMenuBtnOmitsIndicatorWhenNoFiltersActive(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	// filterGlobActive defaults to true, but filterText is empty — per
	// renderFilterMenuBtn's own doc comment, the glob row only counts
	// once it's both active *and* has something to filter by.
	if r.panel.filterText != "" {
		t.Fatal("setup: filterText should start empty")
	}

	got := r.panel.filterMenuBtn.GetText(true)
	if containsSubstring(got, "0x") {
		t.Errorf("filterMenuBtn text = %q, want no \"0x\" indicator at all", got)
	}
	if containsSubstring(got, "x") {
		t.Errorf("filterMenuBtn text = %q, want no count indicator while nothing is active", got)
	}
}

// TestFilterMenuStateSurvivesReopen pins that the filter-menu's own
// three toggles genuinely live on Panel, not on the transient overlay
// widgets renderFilterMenu rebuilds from scratch on every open — a
// toggle set, the dropdown closed, then reopened must still show it.
func TestFilterMenuStateSurvivesReopen(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openFilterMenu()
	mtimeRow, ok := filterMenuRow(t, r, 3).(*tview.TextView)
	if !ok {
		t.Fatal("filterMenuLayout item 3 is not a *tview.TextView")
	}
	click(mtimeRow)
	r.hideOverlay()

	r.openFilterMenu()
	mtimeRow, ok = filterMenuRow(t, r, 3).(*tview.TextView)
	if !ok {
		t.Fatal("filterMenuLayout item 3 is not a *tview.TextView after reopening")
	}
	if got, want := mtimeRow.GetText(true), checkboxText(true)+" Modified time filter"; got != want {
		t.Errorf("modified-time row after reopening = %q, want %q (state should have survived)", got, want)
	}
}
