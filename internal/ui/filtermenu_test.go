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

// TestClickingDetailsExpandBtnDoesNotOpenFilterMenu pins a real,
// user-reported regression: tview.Flex.MouseHandler (verified directly
// against its own flex.go, not assumed) never checks a child's own
// rect before calling its MouseHandler — it calls every item in
// headerRow, in registration order, until one consumes the event.
// filterMenuBtn sits before tabStrip and detailsExpandBtn there, so
// without its own explicit InRect guard it swallowed every click meant
// for either of them, anywhere in the header row, regardless of where
// it actually landed. Drives this through the real dispatch chain
// (root.MouseHandler, not calling a closure directly), the only way to
// actually exercise the bug this pins.
func TestClickingDetailsExpandBtnDoesNotOpenFilterMenu(t *testing.T) {
	dir := fixtureDir(t)
	root, cleanup := drawnRoot(t, dir)
	defer cleanup()

	expandCalled := false
	root.panel.onExpandDetails = func() { expandCalled = true }

	x, y, w, h := root.panel.detailsExpandBtn.GetRect()
	if w == 0 || h == 0 {
		t.Fatal("setup: detailsExpandBtn has no rect — was the panel actually drawn?")
	}
	cx, cy := x+w/2, y+h/2

	handler := root.MouseHandler()
	handler(tview.MouseLeftClick, tcell.NewEventMouse(cx, cy, tcell.Button1, 0), func(tview.Primitive) {})

	if root.activePage == filterMenuPage {
		t.Error("clicking detailsExpandBtn should not open the filter menu")
	}
	if !expandCalled {
		t.Error("clicking detailsExpandBtn should still call onExpandDetails")
	}
}

// TestClickingMtimeRowDoesNotToggleSizeRow pins the same class of bug
// one level deeper: filterMenuLayout calls every top-level row (title
// bar, glob row, size row, modified-time row) in order too, so
// sizeRow's own capture must also decline a click that isn't actually
// its own, or it swallows clicks meant for mtimeRow, which comes right
// after it.
func TestClickingMtimeRowDoesNotToggleSizeRow(t *testing.T) {
	dir := fixtureDir(t)
	root, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)
	root.SetRect(0, 0, 80, 24)
	root.Draw(screen)

	root.openFilterMenu()
	// Flex.SetRect alone (all openFilterMenu itself does) never cascades
	// to a child's own rect — only Draw does (verified directly against
	// tview's own flex.go, not assumed) — a second real draw pass is
	// what actually positions mtimeRow/sizeRow themselves, three levels
	// down inside filterMenuLayout.
	root.Draw(screen)

	mtimeRow, ok := filterMenuRow(t, root, 3).(*tview.TextView)
	if !ok {
		t.Fatal("filterMenuLayout item 3 is not a *tview.TextView")
	}
	x, y, w, h := mtimeRow.GetRect()
	if w == 0 || h == 0 {
		t.Fatal("setup: mtimeRow has no rect — was the menu actually drawn?")
	}
	cx, cy := x+w/2, y+h/2

	handler := root.MouseHandler()
	handler(tview.MouseLeftClick, tcell.NewEventMouse(cx, cy, tcell.Button1, 0), func(tview.Primitive) {})

	if root.panel.filterSizeActive {
		t.Error("clicking the modified-time row should not toggle filterSizeActive")
	}
	if !root.panel.filterMtimeActive {
		t.Error("clicking the modified-time row should toggle filterMtimeActive")
	}
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

// TestFilterMatchesNothingColorsIndicatorRed pins a real, user-reported
// gap filterPersistent's own arrival exposed: a filter carried over
// from an entirely unrelated directory (files pasted in from another
// tab, say) can silently hide everything a directory would otherwise
// show, leaving a listing indistinguishable from a genuinely empty
// folder unless you already know to check the "Nx" indicator. Once a
// filter hides everything, that indicator's own count must render in
// EntryError's own red instead of the plain default color, impossible
// to miss even at a glance.
func TestFilterMatchesNothingColorsIndicatorRed(t *testing.T) {
	dir := fixtureDir(t) // rows: "..", app-data, apple.txt, apricot.txt, banana.txt
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.filterField.SetText("*.zzz") // matches nothing in fixtureDir

	if !r.panel.filterMatchesNothing {
		t.Fatal("filterMatchesNothing should be true once the filter hides every entry")
	}
	if got := r.panel.table.GetRowCount(); got != 1 { // ".." only
		t.Errorf("row count with a filter matching nothing = %d, want 1 (\"..\" only)", got)
	}

	wantTag := "[" + colorTag(r.panel.theme.EntryError) + "::]"
	if got := r.panel.filterMenuBtn.GetText(false); !containsSubstring(got, wantTag) {
		t.Errorf("filterMenuBtn raw text = %q, want it to contain the EntryError color tag %q", got, wantTag)
	}
	if got := r.panel.filterMenuBtn.GetText(true); !containsSubstring(got, "1x") {
		t.Errorf("filterMenuBtn text = %q, want it to still contain \"1x\"", got)
	}
}

// TestFilterMatchesNothingFalseForGenuinelyEmptyDirectory pins the
// other half: an actually empty directory, with no filter narrowing
// anything, must not be mistaken for "a filter is hiding everything" —
// there's nothing here to warn about, and the "Nx" indicator itself is
// already correctly absent (see
// TestRenderFilterMenuBtnOmitsIndicatorWhenNoFiltersActive) since no
// filter is even active.
func TestFilterMatchesNothingFalseForGenuinelyEmptyDirectory(t *testing.T) {
	dir := t.TempDir() // genuinely empty — not even fixtureDir's own entries
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	if r.panel.filterMatchesNothing {
		t.Error("filterMatchesNothing should be false for a directory that's simply empty, filter or no filter")
	}
}

// TestFilterMatchesNothingFalseWhenSomeEntriesStillMatch pins that a
// filter narrowing the listing down to a strict subset — the ordinary,
// self-evident case, not the one this field exists to flag — never
// sets filterMatchesNothing, even though it did hide *some* entries.
func TestFilterMatchesNothingFalseWhenSomeEntriesStillMatch(t *testing.T) {
	dir := fixtureDir(t) // rows: "..", app-data, apple.txt, apricot.txt, banana.txt
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.filterField.SetText("ap*") // matches apple.txt/apricot.txt, not banana.txt

	if r.panel.filterMatchesNothing {
		t.Error("filterMatchesNothing should be false when the filter still matches some entries")
	}
	if got := r.panel.filterMenuBtn.GetText(false); containsSubstring(got, colorTag(r.panel.theme.EntryError)) {
		t.Errorf("filterMenuBtn raw text = %q, should not carry the EntryError color when the filter still matches something", got)
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

// TestFilterMenuEscapeClosesDropdownFromAnyRow pins a real, user-
// reported gap: before this, Escape (or Enter/Tab/Backtab) in
// filterField only ever moved keyboard focus to the table, never
// actually closed the dropdown itself (see renderFilterMenu's own doc
// comment on filterField's own fix), and the two toggle rows had no
// keyboard handling of any kind. Checked from a toggle row
// specifically, not the glob field, since that's the row type that
// previously had no keyboard path out at all.
func TestFilterMenuEscapeClosesDropdownFromAnyRow(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openFilterMenu()
	if r.activePage != filterMenuPage {
		t.Fatal("setup: the dropdown should be open")
	}

	sizeRow, ok := filterMenuRow(t, r, 2).(*tview.TextView)
	if !ok {
		t.Fatal("filterMenuLayout item 2 is not a *tview.TextView")
	}
	sizeRow.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activePage == filterMenuPage {
		t.Error("Escape from the size row should close the dropdown")
	}
}

// TestFilterMenuSpaceTogglesRowFromKeyboard pins the other half of the
// same gap: Space (or Enter) on a toggle row must flip it, the
// keyboard equivalent of clicking it, without closing the dropdown —
// ticking one filter and then reaching the next is the whole point.
func TestFilterMenuSpaceTogglesRowFromKeyboard(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openFilterMenu()

	sizeRow, ok := filterMenuRow(t, r, 2).(*tview.TextView)
	if !ok {
		t.Fatal("filterMenuLayout item 2 is not a *tview.TextView")
	}
	sizeRow.InputHandler()(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone), func(tview.Primitive) {})

	if !r.panel.filterSizeActive {
		t.Error("Space on the size row should toggle filterSizeActive, same as clicking it")
	}
	if r.activePage != filterMenuPage {
		t.Error("toggling a row by keyboard should not close the dropdown")
	}
}

// TestFilterMenuTabCyclesFocusThroughAllFiveStops pins the user's own
// explicit request: keyboard navigation within the dropdown, the same
// as the tab switcher already offers — Tab must move focus through
// every one of the dropdown's own five stops in order (glob checkbox,
// glob/regex mode button, glob pattern field, size toggle,
// modified-time toggle) and wrap back to the first, not just exit the
// dropdown outright the way every one of them used to.
func TestFilterMenuTabCyclesFocusThroughAllFiveStops(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openFilterMenu()

	globCheckbox := filterMenuGlobCheckbox(t, r)
	globRow, ok := filterMenuRow(t, r, 1).(*tview.Flex)
	if !ok {
		t.Fatal("filterMenuLayout item 1 is not a *tview.Flex")
	}
	regexBtn, ok := globRow.GetItem(1).(*tview.Button)
	if !ok {
		t.Fatal("glob row item 1 is not a *tview.Button")
	}
	sizeRow, ok := filterMenuRow(t, r, 2).(*tview.TextView)
	if !ok {
		t.Fatal("filterMenuLayout item 2 is not a *tview.TextView")
	}
	mtimeRow, ok := filterMenuRow(t, r, 3).(*tview.TextView)
	if !ok {
		t.Fatal("filterMenuLayout item 3 is not a *tview.TextView")
	}

	if !r.panel.filterField.HasFocus() {
		t.Fatal("setup: filterField should have initial focus when the dropdown opens")
	}

	tab := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	noop := func(tview.Primitive) {}

	r.panel.filterField.InputHandler()(tab, noop)
	if !sizeRow.HasFocus() {
		t.Error("Tab from filterField should move focus to the size row")
	}

	sizeRow.InputHandler()(tab, noop)
	if !mtimeRow.HasFocus() {
		t.Error("Tab from the size row should move focus to the modified-time row")
	}

	mtimeRow.InputHandler()(tab, noop)
	if !globCheckbox.HasFocus() {
		t.Error("Tab from the modified-time row should wrap back to the glob checkbox")
	}

	globCheckbox.InputHandler()(tab, noop)
	if !regexBtn.HasFocus() {
		t.Error("Tab from the glob checkbox should move focus to the glob/regex mode button")
	}

	if r.activePage != filterMenuPage {
		t.Error("cycling focus with Tab should never close the dropdown")
	}
}
