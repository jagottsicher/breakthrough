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

// filterMenuFieldRowCheckbox/filterMenuFieldRowField reach into the
// size (index 2) or modified-time (index 3) row — each itself a Flex
// (checkbox + InputField, see newFilterMenuFieldRow) — the same one-
// level-deeper access filterMenuGlobCheckbox already uses for the glob
// row.
func filterMenuFieldRowCheckbox(t *testing.T, r *Root, rowIndex int) *tview.TextView {
	t.Helper()
	row, ok := filterMenuRow(t, r, rowIndex).(*tview.Flex)
	if !ok {
		t.Fatalf("filterMenuLayout item %d is not a *tview.Flex", rowIndex)
	}
	checkbox, ok := row.GetItem(0).(*tview.TextView)
	if !ok {
		t.Fatalf("row %d item 0 is not a *tview.TextView (the checkbox)", rowIndex)
	}
	return checkbox
}

func filterMenuFieldRowField(t *testing.T, r *Root, rowIndex int) *tview.InputField {
	t.Helper()
	row, ok := filterMenuRow(t, r, rowIndex).(*tview.Flex)
	if !ok {
		t.Fatalf("filterMenuLayout item %d is not a *tview.Flex", rowIndex)
	}
	field, ok := row.GetItem(1).(*tview.InputField)
	if !ok {
		t.Fatalf("row %d item 1 is not a *tview.InputField (the expression field)", rowIndex)
	}
	return field
}

// click simulates a real left-click on p by going through its own
// MouseHandler — the same "call the actual dispatch path, not the
// closure directly" approach TestCheckboxClickTogglesDisplay already
// establishes for the panel's own checkbox column. The position
// doesn't matter here: every mouse capture in this file only checks
// the action, never where exactly it landed (see
// Root.newFilterMenuFieldRow's own doc comment on why a checkbox is
// its own click target, separate from its own expression field).
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
	// what actually positions mtimeRow/sizeRow themselves, four levels
	// down inside filterMenuLayout now that each is its own Flex
	// (checkbox + expression field).
	root.Draw(screen)

	mtimeCheckbox := filterMenuFieldRowCheckbox(t, root, 3)
	x, y, w, h := mtimeCheckbox.GetRect()
	if w == 0 || h == 0 {
		t.Fatal("setup: the modified-time checkbox has no rect — was the menu actually drawn?")
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

	sizeCheckbox := filterMenuFieldRowCheckbox(t, r, 2)
	if got, want := sizeCheckbox.GetText(true), checkboxText(false)+" Size filter"; got != want {
		t.Errorf("size checkbox = %q, want %q", got, want)
	}
	if got := filterMenuFieldRowField(t, r, 2).GetText(); got != "" {
		t.Errorf("size field = %q, want empty (nothing typed yet)", got)
	}

	mtimeCheckbox := filterMenuFieldRowCheckbox(t, r, 3)
	if got, want := mtimeCheckbox.GetText(true), checkboxText(false)+" Modified time filter"; got != want {
		t.Errorf("modified-time checkbox = %q, want %q", got, want)
	}
	if got := filterMenuFieldRowField(t, r, 3).GetText(); got != "" {
		t.Errorf("modified-time field = %q, want empty (nothing typed yet)", got)
	}
}

// TestFilterMenuGlobCheckboxTogglesActiveAndReloads pins the
// "disable without clearing" contract filterGlobActive exists for:
// clicking the checkbox flips it, re-renders in place (the dropdown
// stays open — see newFilterMenuFieldRow's own doc comment), and
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

// TestFilterMenuSizeToggleAloneDoesNotCountWithoutAnExpression pins the
// same "checked, but nothing to actually filter by yet" no-op
// renderFilterMenuBtn's own doc comment describes: toggling the size
// checkbox on its own, with its expression field still empty, doesn't
// bump the "Nx" indicator — exactly like the glob row's own checkbox
// with nothing typed into it. It does still flip the toggle itself
// (and only that one, not modified-time or the glob row) and re-render
// its own label.
func TestFilterMenuSizeToggleAloneDoesNotCountWithoutAnExpression(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.filterField.SetText("apple.txt") // one real filter already active — see renderFilterMenuBtn's own doc comment on what counts

	r.openFilterMenu()
	sizeCheckbox := filterMenuFieldRowCheckbox(t, r, 2)
	click(sizeCheckbox)

	if !r.panel.filterSizeActive {
		t.Error("filterSizeActive should be true after clicking its own checkbox")
	}
	if r.panel.filterMtimeActive {
		t.Error("clicking the size checkbox should not affect filterMtimeActive")
	}
	if got, want := sizeCheckbox.GetText(true), checkboxText(true)+" Size filter"; got != want {
		t.Errorf("size checkbox after toggling = %q, want %q", got, want)
	}
	if got, want := r.panel.filterMenuBtn.GetText(true), "1x"; !containsSubstring(got, want) {
		t.Errorf("filterMenuBtn text = %q, want it to still contain %q (glob only — size has no expression yet)", got, want)
	}
	if containsSubstring(r.panel.filterMenuBtn.GetText(true), "2x") {
		t.Error("an empty size expression should not count toward the indicator even with its checkbox on")
	}
}

// TestFilterMenuTypingIntoSizeFieldActivatesAndCounts pins the
// counterpart: typing an actual expression into the size field is what
// makes it count, auto-activating the checkbox exactly the way typing
// into the glob field already auto-activates that row (see
// newFilterMenuFieldRow's own doc comment).
func TestFilterMenuTypingIntoSizeFieldActivatesAndCounts(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.filterField.SetText("apple.txt")

	r.openFilterMenu()
	sizeField := filterMenuFieldRowField(t, r, 2)
	sizeField.SetText("> 1m")

	if !r.panel.filterSizeActive {
		t.Error("typing into the size field should auto-activate its own checkbox")
	}
	if r.panel.filterSizeText != "> 1m" {
		t.Errorf("filterSizeText = %q, want %q", r.panel.filterSizeText, "> 1m")
	}
	if got, want := r.panel.filterMenuBtn.GetText(true), "2x"; !containsSubstring(got, want) {
		t.Errorf("filterMenuBtn text = %q, want it to contain %q (glob + size both active with an expression)", got, want)
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
	click(filterMenuFieldRowCheckbox(t, r, 3))
	filterMenuFieldRowField(t, r, 3).SetText("last 7 days")
	r.hideOverlay()

	r.openFilterMenu()
	mtimeCheckbox := filterMenuFieldRowCheckbox(t, r, 3)
	if got, want := mtimeCheckbox.GetText(true), checkboxText(true)+" Modified time filter"; got != want {
		t.Errorf("modified-time checkbox after reopening = %q, want %q (state should have survived)", got, want)
	}
	if got, want := filterMenuFieldRowField(t, r, 3).GetText(), "last 7 days"; got != want {
		t.Errorf("modified-time field after reopening = %q, want %q (its own typed text should have survived too)", got, want)
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

	sizeCheckbox := filterMenuFieldRowCheckbox(t, r, 2)
	sizeCheckbox.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activePage == filterMenuPage {
		t.Error("Escape from the size checkbox should close the dropdown")
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

	sizeCheckbox := filterMenuFieldRowCheckbox(t, r, 2)
	sizeCheckbox.InputHandler()(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone), func(tview.Primitive) {})

	if !r.panel.filterSizeActive {
		t.Error("Space on the size checkbox should toggle filterSizeActive, same as clicking it")
	}
	if r.activePage != filterMenuPage {
		t.Error("toggling a checkbox by keyboard should not close the dropdown")
	}
}

// TestFilterMenuTabCyclesFocusThroughAllSevenStops pins the user's own
// explicit request: keyboard navigation within the dropdown, the same
// as the tab switcher already offers — Tab must move focus through
// every one of the dropdown's own seven stops in order (glob checkbox,
// glob/regex mode button, glob pattern field, size checkbox, size
// expression field, modified-time checkbox, modified-time expression
// field) and wrap back to the first, not just exit the dropdown
// outright the way every one of them used to. Grew from five stops to
// seven once the size/modified-time rows gained their own real
// expression fields alongside their checkboxes.
func TestFilterMenuTabCyclesFocusThroughAllSevenStops(t *testing.T) {
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
	sizeCheckbox := filterMenuFieldRowCheckbox(t, r, 2)
	sizeField := filterMenuFieldRowField(t, r, 2)
	mtimeCheckbox := filterMenuFieldRowCheckbox(t, r, 3)
	mtimeField := filterMenuFieldRowField(t, r, 3)

	if !r.panel.filterField.HasFocus() {
		t.Fatal("setup: filterField should have initial focus when the dropdown opens")
	}

	tab := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	noop := func(tview.Primitive) {}

	r.panel.filterField.InputHandler()(tab, noop)
	if !sizeCheckbox.HasFocus() {
		t.Error("Tab from filterField should move focus to the size checkbox")
	}

	sizeCheckbox.InputHandler()(tab, noop)
	if !sizeField.HasFocus() {
		t.Error("Tab from the size checkbox should move focus to the size expression field")
	}

	sizeField.InputHandler()(tab, noop)
	if !mtimeCheckbox.HasFocus() {
		t.Error("Tab from the size expression field should move focus to the modified-time checkbox")
	}

	mtimeCheckbox.InputHandler()(tab, noop)
	if !mtimeField.HasFocus() {
		t.Error("Tab from the modified-time checkbox should move focus to the modified-time expression field")
	}

	mtimeField.InputHandler()(tab, noop)
	if !globCheckbox.HasFocus() {
		t.Error("Tab from the modified-time expression field should wrap back to the glob checkbox")
	}

	globCheckbox.InputHandler()(tab, noop)
	if !regexBtn.HasFocus() {
		t.Error("Tab from the glob checkbox should move focus to the glob/regex mode button")
	}

	if r.activePage != filterMenuPage {
		t.Error("cycling focus with Tab should never close the dropdown")
	}
}
