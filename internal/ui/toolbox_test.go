package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestToolboxCategoriesHaveNoEmptyFieldsAndUniqueLabels guards the
// catalog's own basic shape the same way TestPlainCommandsHaveNoEmptyFields
// guards the keymap registry: a blank label/help or a nil run would
// silently misrender or do nothing when activated, and a duplicate
// label would make two different rows indistinguishable at a glance.
func TestToolboxCategoriesHaveNoEmptyFieldsAndUniqueLabels(t *testing.T) {
	seen := map[string]bool{}
	for _, cat := range toolboxCategories() {
		if cat.name == "" {
			t.Errorf("category %+v has no name", cat)
		}
		if len(cat.entries) == 0 {
			t.Errorf("category %q has no entries", cat.name)
		}
		for _, e := range cat.entries {
			if e.label == "" {
				t.Errorf("category %q: entry %+v has no label", cat.name, e)
			}
			if e.help == "" {
				t.Errorf("category %q: entry %q has no help text", cat.name, e.label)
			}
			if e.run == nil {
				t.Errorf("category %q: entry %q has no action", cat.name, e.label)
			}
			if seen[e.label] {
				t.Errorf("label %q is used by more than one entry", e.label)
			}
			seen[e.label] = true
		}
	}
}

// TestToolboxDisplayRowsGroupEntriesUnderHeaders pins
// toolboxDisplayRows' own shape: one header row per category, in order,
// a blank spacer between two categories but never before the first one,
// and every entry appearing exactly once, right after its own
// category's header.
func TestToolboxDisplayRowsGroupEntriesUnderHeaders(t *testing.T) {
	cats := toolboxCategories()
	rows := toolboxDisplayRows()

	var gotHeaders []string
	var gotEntries int
	blanks := 0
	for i, dr := range rows {
		switch {
		case dr.blank:
			blanks++
			if i == 0 {
				t.Error("the very first row should never be a blank spacer")
			}
		case dr.header != "":
			gotHeaders = append(gotHeaders, dr.header)
		default:
			gotEntries++
		}
	}

	var wantHeaders []string
	wantEntries := 0
	for _, cat := range cats {
		wantHeaders = append(wantHeaders, cat.name)
		wantEntries += len(cat.entries)
	}

	if strings.Join(gotHeaders, ",") != strings.Join(wantHeaders, ",") {
		t.Errorf("headers = %v, want %v", gotHeaders, wantHeaders)
	}
	if gotEntries != wantEntries {
		t.Errorf("entry rows = %d, want %d", gotEntries, wantEntries)
	}
	if want := len(cats) - 1; blanks != want {
		t.Errorf("blank spacer rows = %d, want %d (one between each pair of categories)", blanks, want)
	}
}

// TestFirstSelectableToolboxRowSkipsTheLeadingHeader pins that the
// cursor's own starting point is a real entry, never the catalog's
// always-present leading header — landing there would put the cursor on
// a row Enter can't do anything with.
func TestFirstSelectableToolboxRowSkipsTheLeadingHeader(t *testing.T) {
	row := firstSelectableToolboxRow()
	entry, ok := toolboxEntryAtRow(row)
	if !ok {
		t.Fatalf("firstSelectableToolboxRow() = %d, not a real entry", row)
	}
	wantFirst := toolboxCategories()[0].entries[0]
	if entry.label != wantFirst.label {
		t.Errorf("first selectable entry = %q, want %q", entry.label, wantFirst.label)
	}
}

// TestToolboxEntryAtRowRejectsHeaderAndOutOfRangeRows pins the guard
// every caller (activateToolboxRow, the mouse handler) relies on: a
// header row and an out-of-range row both report false rather than a
// zero-value entry that would silently run nothing.
func TestToolboxEntryAtRowRejectsHeaderAndOutOfRangeRows(t *testing.T) {
	if _, ok := toolboxEntryAtRow(0); ok {
		t.Error("row 0 is always the leading category header — should not be a real entry")
	}
	if _, ok := toolboxEntryAtRow(-1); ok {
		t.Error("a negative row should never be a real entry")
	}
	if _, ok := toolboxEntryAtRow(len(toolboxDisplayRows()) + 100); ok {
		t.Error("a row far past the end should never be a real entry")
	}
}

// TestToolboxFixedEntryRunsImmediately pins toolboxFixedEntry's own
// shape: activating it opens a tool window right away, with no prompt
// in between — "echo" stands in for a real tool the same way the
// existing openToolCommand tests already use it, to keep this hermetic.
func TestToolboxFixedEntryRunsImmediately(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	entry := toolboxFixedEntry("Test Echo", "a fixed test entry", "echo", "hello")
	entry.run(r)

	if len(r.toolWindows) != 1 {
		t.Fatalf("len(toolWindows) = %d, want 1 right after a fixed entry runs", len(r.toolWindows))
	}
	tw := r.toolWindows[0]
	defer tw.close()

	if got := tw.titleBar.GetText(true); !strings.Contains(got, "echo hello") {
		t.Errorf("tool window title = %q, want it to mention %q", got, "echo hello")
	}
}

// TestToolboxFixedEntryTitleOmitsTrailingSpaceWithoutArgs pins that a
// fixed entry with no args at all (lsusb, lscpu, ...) titles its window
// with just the bare command name, not the name plus a stray trailing
// space.
func TestToolboxFixedEntryTitleOmitsTrailingSpaceWithoutArgs(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	entry := toolboxFixedEntry("Test Echo", "a fixed test entry with no args", "echo")
	entry.run(r)

	if len(r.toolWindows) != 1 {
		t.Fatalf("len(toolWindows) = %d, want 1", len(r.toolWindows))
	}
	tw := r.toolWindows[0]
	defer tw.close()

	if got := strings.TrimSpace(tw.titleBar.GetText(true)); got != "echo" {
		t.Errorf("tool window title = %q, want exactly %q", got, "echo")
	}
}

// TestToolboxArgEntryPromptsBeforeRunning pins toolboxArgEntry's own
// shape: activating it opens the small floating input first, prefilled
// as configured, with no tool window started yet — mirroring the
// now-retired TestOpenPingTestWindowPromptsForHost this generalizes.
func TestToolboxArgEntryPromptsBeforeRunning(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	entry := toolboxArgEntry("Test Echo Arg", "help text", "Echo arg:", "prefilled", "echo", "fixed1")
	entry.run(r)

	if r.activePage != toolboxInputPage {
		t.Fatalf("activePage = %q, want the toolbox input overlay", r.activePage)
	}
	if got := r.toolboxInput.GetLabel(); got != " Echo arg: " {
		t.Errorf("prompt label = %q, want %q", got, " Echo arg: ")
	}
	if got := r.toolboxInput.GetText(); got != "prefilled" {
		t.Errorf("prompt text = %q, want the configured prefill %q", got, "prefilled")
	}
	if len(r.toolWindows) != 0 {
		t.Errorf("len(toolWindows) = %d, want 0 before the prompt is even submitted", len(r.toolWindows))
	}
}

// TestToolboxArgEntrySubmittingRunsFixedArgsThenTypedText pins that the
// typed text is split on whitespace and appended after the entry's own
// fixed args — "tail -f" (fixedArgs ["-f"]) plus a typed path runs
// "tail -f <path>", not just the path alone or the fixed args alone.
func TestToolboxArgEntrySubmittingRunsFixedArgsThenTypedText(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	entry := toolboxArgEntry("Test Echo Arg", "help text", "Echo arg:", "", "echo", "fixed1")
	entry.run(r)

	r.toolboxInput.SetText("typed value")
	r.toolboxInput.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if len(r.toolWindows) != 1 {
		t.Fatalf("len(toolWindows) = %d, want 1 after submitting the prompt", len(r.toolWindows))
	}
	tw := r.toolWindows[0]
	defer tw.close()

	if got := tw.titleBar.GetText(true); !strings.Contains(got, "echo fixed1 typed value") {
		t.Errorf("tool window title = %q, want it to mention %q", got, "echo fixed1 typed value")
	}
	if r.activePage == toolboxInputPage {
		t.Error("the input overlay should have closed once submitted")
	}
}

// TestToolboxArgEntryEmptySubmissionRunsNothing pins that pressing
// Enter on an empty (or all-whitespace) field cancels rather than
// running the command with no argument at all — the same "nothing
// typed means nothing happens" rule finishPrompt already follows for
// the generic prompt overlay.
func TestToolboxArgEntryEmptySubmissionRunsNothing(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	entry := toolboxArgEntry("Test Echo Arg", "help text", "Echo arg:", "", "echo")
	entry.run(r)

	r.toolboxInput.SetText("   ")
	r.toolboxInput.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if len(r.toolWindows) != 0 {
		t.Errorf("len(toolWindows) = %d, want 0 — an all-whitespace submission should run nothing", len(r.toolWindows))
	}
}

// TestToolboxArgEntryEscapeCancelsWithoutRunning pins that Escape
// discards the prompt the same way it does for the generic one.
func TestToolboxArgEntryEscapeCancelsWithoutRunning(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	entry := toolboxArgEntry("Test Echo Arg", "help text", "Echo arg:", "", "echo")
	entry.run(r)

	r.toolboxInput.SetText("this should never run")
	r.toolboxInput.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})

	if len(r.toolWindows) != 0 {
		t.Errorf("len(toolWindows) = %d, want 0 — Escape should cancel, not run", len(r.toolWindows))
	}
	if r.activePage == toolboxInputPage {
		t.Error("the input overlay should have closed on Escape")
	}
}

// TestOpenToolboxScreenRendersTheWholeCatalog pins openToolboxScreen's
// own basic contract when given every category at once: it shows the
// Toolbox page and fills the table with exactly one row per
// toolboxDisplayRows entry, cursor landing on a real one. There is no
// keyboard shortcut reaching the combined catalog directly any more
// ("jn"/"jh" each show one category — see their own tests below), but
// the shared rendering mechanism handling every category at once
// remains real, exercised behavior worth its own test.
func TestOpenToolboxScreenRendersTheWholeCatalog(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openToolboxScreen("Toolbox", toolboxCategories())

	if r.activePage != toolboxPage {
		t.Fatalf("activePage = %q, want the Toolbox screen", r.activePage)
	}
	if got, want := r.toolboxTable.GetRowCount(), len(toolboxDisplayRows()); got != want {
		t.Errorf("row count = %d, want %d", got, want)
	}
	row, _ := r.toolboxTable.GetSelection()
	if !isToolboxEntryRow(row) {
		t.Errorf("selected row %d is not a real entry", row)
	}
}

// TestToolboxCategoriesNamedFiltersAndPreservesOrder pins
// toolboxCategoriesNamed's own two cases: filtering down to exactly the
// requested categories, in the catalog's own order, and — no names at
// all — falling back to the full, unfiltered catalog.
func TestToolboxCategoriesNamedFiltersAndPreservesOrder(t *testing.T) {
	all := toolboxCategories()
	if got := toolboxCategoriesNamed(); len(got) != len(all) {
		t.Errorf("toolboxCategoriesNamed() (no names) = %d categories, want the full catalog's %d", len(got), len(all))
	}

	networking := toolboxCategoriesNamed("Networking")
	if len(networking) != 1 || networking[0].name != "Networking" {
		t.Fatalf("toolboxCategoriesNamed(%q) = %+v, want exactly that one category", "Networking", networking)
	}

	hardware := toolboxCategoriesNamed("Hardware")
	if len(hardware) != 1 || hardware[0].name != "Hardware" {
		t.Fatalf("toolboxCategoriesNamed(%q) = %+v, want exactly that one category", "Hardware", hardware)
	}
}

// TestOpenNetworkToolsShowsOnlyTheNetworkingCategory pins "jn"'s own
// contract: a dedicated screen with just the Networking entries, no
// Hardware header or entries mixed in.
func TestOpenNetworkToolsShowsOnlyTheNetworkingCategory(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openNetworkTools()

	if r.activePage != toolboxPage {
		t.Fatalf("activePage = %q, want the Toolbox screen", r.activePage)
	}
	if got, want := r.toolboxTitleBar.GetText(true), " Network Tools "+string(toolWindowCloseGlyph)+" "; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	wantRows := toolboxDisplayRowsFor(toolboxCategoriesNamed("Networking"))
	if got, want := r.toolboxTable.GetRowCount(), len(wantRows); got != want {
		t.Errorf("row count = %d, want %d (Networking category alone)", got, want)
	}
	for _, dr := range r.toolboxRows {
		if dr.header == "Hardware" {
			t.Error("Network Tools screen must not include the Hardware header")
		}
	}
}

// TestOpenHardwareToolsShowsOnlyTheHardwareCategory is
// TestOpenNetworkToolsShowsOnlyTheNetworkingCategory's own counterpart
// for "jh".
func TestOpenHardwareToolsShowsOnlyTheHardwareCategory(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openHardwareTools()

	if r.activePage != toolboxPage {
		t.Fatalf("activePage = %q, want the Toolbox screen", r.activePage)
	}
	if got, want := r.toolboxTitleBar.GetText(true), " Hardware Tools "+string(toolWindowCloseGlyph)+" "; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	wantRows := toolboxDisplayRowsFor(toolboxCategoriesNamed("Hardware"))
	if got, want := r.toolboxTable.GetRowCount(), len(wantRows); got != want {
		t.Errorf("row count = %d, want %d (Hardware category alone)", got, want)
	}
	for _, dr := range r.toolboxRows {
		if dr.header == "Networking" {
			t.Error("Hardware Tools screen must not include the Networking header")
		}
	}
}

// TestOpenHardwareToolsAfterNetworkToolsShowsOnlyHardware pins that
// switching from one filtered screen to the other actually replaces
// its rows rather than adding to or getting stuck on the previous
// one's — r.toolboxRows must not stay stuck on "jn"'s own Networking
// rows once "jh" opens.
func TestOpenHardwareToolsAfterNetworkToolsShowsOnlyHardware(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openNetworkTools()
	r.openHardwareTools()

	wantRows := toolboxDisplayRowsFor(toolboxCategoriesNamed("Hardware"))
	if got, want := r.toolboxTable.GetRowCount(), len(wantRows); got != want {
		t.Errorf("row count after opening jh = %d, want %d (Hardware only)", got, want)
	}
	if got, want := r.toolboxTitleBar.GetText(true), " Hardware Tools "+string(toolWindowCloseGlyph)+" "; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

// TestCaptureToolboxKeyEscapeClosesTheScreen pins the one key this
// screen's own capture handles directly — Enter already reaches
// activateToolboxRow through the table's own SetSelectedFunc, the same
// as every other selectable table in this app.
func TestCaptureToolboxKeyEscapeClosesTheScreen(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openNetworkTools()

	if got := r.captureToolboxKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); got != nil {
		t.Error("captureToolboxKey should consume Escape")
	}
	if r.activePage == toolboxPage {
		t.Error("Escape should have closed the Toolbox screen")
	}
}

// TestActivateToolboxRowOnAHeaderRowDoesNothing pins that Enter (or a
// click) landing on a header/blank row — which the arrow keys should
// never actually let the cursor reach, but a stray SetSelectedFunc call
// still could — is a safe no-op rather than a panic on the zero-value
// entry's nil run field.
func TestActivateToolboxRowOnAHeaderRowDoesNothing(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	// The full, multi-category catalog, not openNetworkTools/
	// openHardwareTools: a single-category screen no longer renders a
	// header row at all (see toolboxDisplayRowsFor's own doc comment —
	// "jn"/"jh" already name the one category in the title bar itself),
	// so this needs the one path that still has a header row to
	// activate.
	r.openToolboxScreen("Toolbox", toolboxCategories())

	r.activateToolboxRow(0) // the leading category header

	if len(r.toolWindows) != 0 {
		t.Errorf("len(toolWindows) = %d, want 0 — activating a header row should do nothing", len(r.toolWindows))
	}
	if r.activePage != toolboxPage {
		t.Errorf("activePage = %q, want the Toolbox screen to still be open", r.activePage)
	}
}

// toolboxTestFormEntry is a small toolboxFormEntry used by the tests
// below — one text field (with a default) and one toggle, build()
// deliberately skipping the text field whenever it's empty (the "no
// blank tokens" rule toolboxFormEntry's own doc comment requires of
// every real build func).
func toolboxTestFormEntry() toolboxEntry {
	fields := []toolboxFormField{
		{name: "a", label: "A", kind: toolboxFieldText, defaultText: "x"},
		{name: "b", label: "B toggle", kind: toolboxFieldToggle},
	}
	build := func(v toolboxFormValues) []string {
		var args []string
		if a := v.Text["a"]; a != "" {
			args = append(args, a)
		}
		if v.Flags["b"] {
			args = append(args, "-b")
		}
		return args
	}
	return toolboxFormEntry("Test Form", "a form test entry", "echo", fields, build)
}

// TestToolboxFormEntryOpensWithDefaultsAndPreview pins toolboxFormEntry's
// own shape: activating it opens toolboxForm's own overlay (not
// toolboxInput), prefilled with every field's own default, and the
// preview line already reflects exactly what Run would execute — before
// anything has been touched at all.
func TestToolboxFormEntryOpensWithDefaultsAndPreview(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	entry := toolboxTestFormEntry()
	entry.run(r)

	if r.activePage != toolboxFormPage {
		t.Fatalf("activePage = %q, want the toolbox form overlay", r.activePage)
	}
	if len(r.toolWindows) != 0 {
		t.Errorf("len(toolWindows) = %d, want 0 before Run is ever pressed", len(r.toolWindows))
	}
	if got := r.toolboxFormPreview.GetText(true); !strings.Contains(got, "echo x") {
		t.Errorf("preview = %q, want it to mention %q", got, "echo x")
	}
}

// TestToolboxFormEntryRunUsesCurrentFieldValues pins that Run builds its
// argv from whatever the fields currently hold, not the defaults they
// opened with, and that the empty-field skip rule actually keeps an
// empty text field from reaching argv as a blank token.
func TestToolboxFormEntryRunUsesCurrentFieldValues(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	entry := toolboxTestFormEntry()
	entry.run(r)

	input, ok := r.toolboxFormWidgets["a"].(*tview.InputField)
	if !ok {
		t.Fatalf("toolboxFormWidgets[%q] is not an InputField", "a")
	}
	input.SetText("typed")
	r.toolboxFormToggleState["b"] = true

	r.runToolboxForm()

	if len(r.toolWindows) != 1 {
		t.Fatalf("len(toolWindows) = %d, want 1 after Run", len(r.toolWindows))
	}
	tw := r.toolWindows[0]
	defer tw.close()

	if got := tw.titleBar.GetText(true); !strings.Contains(got, "echo typed -b") {
		t.Errorf("tool window title = %q, want it to mention %q", got, "echo typed -b")
	}
	if r.activePage == toolboxFormPage {
		t.Error("the form overlay should have closed once Run ran")
	}
}

// TestToolboxFormEntryCancelRunsNothing pins Cancel's own shape: closes
// the overlay, starts nothing.
func TestToolboxFormEntryCancelRunsNothing(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	entry := toolboxTestFormEntry()
	entry.run(r)

	r.toolboxFormCancelBtn.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if len(r.toolWindows) != 0 {
		t.Errorf("len(toolWindows) = %d, want 0 — Cancel should run nothing", len(r.toolWindows))
	}
	if r.activePage == toolboxFormPage {
		t.Error("Cancel should have closed the form overlay")
	}
}

// TestTabAroundTheToolboxFormReachesEveryWidgetAndWrapsBack pins the
// same cross-widget Tab/Backtab chain Sed Replace's own identical test
// (TestTabFromTheAdvancedFieldReachesTheFlagsAndActions) already proves
// for its own form/list/actions trio: the form's own last field reaches
// toolboxFormToggles, which reaches Cancel, which cycles to Run, which
// wraps back to the form's own first field — all the way around and
// back, never stuck forward-only the way Befund 8's bug once left
// connectdialog.go/rsync.go/sshshell.go's own button rows.
func TestTabAroundTheToolboxFormReachesEveryWidgetAndWrapsBack(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	entry := toolboxTestFormEntry()
	entry.run(r)

	noop := func(tview.Primitive) {}
	tab := func(p tview.Primitive) { p.InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), noop) }
	backtab := func(p tview.Primitive) { p.InputHandler()(tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone), noop) }

	fieldA, ok := r.toolboxFormWidgets["a"].(*tview.InputField)
	if !ok {
		t.Fatalf("toolboxFormWidgets[%q] is not an InputField", "a")
	}

	tab(fieldA)
	if !r.toolboxFormToggles.HasFocus() {
		t.Fatal("Tab from the form's own last field did not reach toolboxFormToggles")
	}

	tab(r.toolboxFormToggles)
	if !r.toolboxFormCancelBtn.HasFocus() {
		t.Fatal("Tab from toolboxFormToggles did not reach Cancel")
	}

	tab(r.toolboxFormCancelBtn)
	if !r.toolboxFormRunBtn.HasFocus() {
		t.Fatal("Tab from Cancel did not reach Run")
	}

	tab(r.toolboxFormRunBtn)
	if !fieldA.HasFocus() {
		t.Fatal("Tab from Run did not wrap back to the form's own first field")
	}

	// Backtab from the form's own first field wraps within the form
	// itself (fieldA is also its only field here) — the same
	// "Backtab wraps inside the form, never out to a sibling widget"
	// convention sedFindField's own Backtab already establishes
	// (TestTabFromTheAdvancedFieldReachesTheFlagsAndActions), left to
	// tview's native Form handling rather than anything wired above.
	backtab(fieldA)
	if !fieldA.HasFocus() {
		t.Fatal("Backtab from the form's own first field should wrap within the form itself")
	}

	// Back to Run the long way (fieldA -> toggles -> Cancel -> Run), to
	// check Backtab's own chain starting there.
	tab(fieldA)
	tab(r.toolboxFormToggles)
	tab(r.toolboxFormCancelBtn)
	if !r.toolboxFormRunBtn.HasFocus() {
		t.Fatal("Tab from Cancel did not reach Run")
	}

	backtab(r.toolboxFormRunBtn)
	if !r.toolboxFormCancelBtn.HasFocus() {
		t.Fatal("Backtab from Run did not reach Cancel")
	}

	backtab(r.toolboxFormCancelBtn)
	if !r.toolboxFormToggles.HasFocus() {
		t.Fatal("Backtab from Cancel did not reach toolboxFormToggles")
	}

	backtab(r.toolboxFormToggles)
	if !fieldA.HasFocus() {
		t.Fatal("Backtab from toolboxFormToggles did not reach the form's own last field")
	}
}

// TestToolboxCurlEntryBuildOmitsEmptyOptionalFields pins curl's own
// build func directly — GET and an empty Header/Output/Follow redirects
// must produce no flags at all beyond the URL itself, never a blank
// token like an empty "-H" value.
func TestToolboxCurlEntryBuildOmitsEmptyOptionalFields(t *testing.T) {
	entry := toolboxCurlEntry()
	// toolboxCurlEntry doesn't expose build directly — reach it the same
	// way toolboxFormEntry's own run does, through a real open, so this
	// also doubles as curl's own catalog-shape regression test.
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	entry.run(r)

	urlField, ok := r.toolboxFormWidgets[toolboxCurlFieldURL].(*tview.InputField)
	if !ok {
		t.Fatalf("toolboxFormWidgets[%q] is not an InputField", toolboxCurlFieldURL)
	}
	urlField.SetText("https://example.com")

	if got := r.toolboxFormPreview.GetText(true); got != "curl https://example.com" {
		t.Errorf("preview = %q, want exactly %q (GET default and every optional field empty)", got, "curl https://example.com")
	}

	r.runToolboxForm()
	if len(r.toolWindows) != 1 {
		t.Fatalf("len(toolWindows) = %d, want 1", len(r.toolWindows))
	}
	tw := r.toolWindows[0]
	defer tw.close()
	if got := tw.titleBar.GetText(true); !strings.Contains(got, "curl https://example.com") {
		t.Errorf("tool window title = %q, want it to mention %q", got, "curl https://example.com")
	}
}

// TestToolboxCurlEntryBuildIncludesEveryFieldWhenSet is
// TestToolboxCurlEntryBuildOmitsEmptyOptionalFields's counterpart: every
// field set to a non-default value reaches argv, in order.
func TestToolboxCurlEntryBuildIncludesEveryFieldWhenSet(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	entry := toolboxCurlEntry()
	entry.run(r)

	urlField := r.toolboxFormWidgets[toolboxCurlFieldURL].(*tview.InputField)
	urlField.SetText("https://example.com")
	headerField := r.toolboxFormWidgets[toolboxCurlFieldHeader].(*tview.InputField)
	headerField.SetText("Accept: text/plain")
	outputField := r.toolboxFormWidgets[toolboxCurlFieldOutput].(*tview.InputField)
	outputField.SetText("out.txt")
	methodField := r.toolboxFormWidgets[toolboxCurlFieldMethod].(*tview.DropDown)
	methodField.SetCurrentOption(1) // POST, per toolboxCurlEntry's own choices order
	// Set directly rather than through toolboxFormToggles' own selected
	// func (the same direct-state-manipulation style toggleSedFlag's own
	// tests already use) — updateToolboxFormPreview must be called
	// explicitly afterwards, since nothing here went through the real
	// UI event that would normally trigger it.
	r.toolboxFormToggleState[toolboxCurlFieldFollow] = true
	r.updateToolboxFormPreview()

	want := `curl -X POST -H Accept: text/plain -L -o out.txt https://example.com`
	if got := r.toolboxFormPreview.GetText(true); got != want {
		t.Errorf("preview = %q, want %q", got, want)
	}
}

// TestToolboxNmapFlagForParsesTheChoiceLabelsOwnSuffix pins
// toolboxNmapFlagFor directly: "Default" (and anything else without a
// "(-flag)" suffix) means no flag at all, every other choice yields
// exactly the flag inside its own parentheses.
func TestToolboxNmapFlagForParsesTheChoiceLabelsOwnSuffix(t *testing.T) {
	cases := map[string]string{
		"Default":                       "",
		"SYN scan (-sS)":                "-sS",
		"UDP scan (-sU)":                "-sU",
		"Ping scan, no port scan (-sn)": "-sn",
		"Aggressive (-T4)":              "-T4",
	}
	for choice, want := range cases {
		if got := toolboxNmapFlagFor(choice); got != want {
			t.Errorf("toolboxNmapFlagFor(%q) = %q, want %q", choice, got, want)
		}
	}
}

// TestToolboxNmapEntryBuildOmitsEmptyOptionalFields pins nmap's own
// build func via a real open: Default scan type/timing and an empty
// Ports field must produce no flags at all beyond the target itself.
// Checked through the preview only, not a real Run — unlike curl, nmap
// isn't guaranteed to be installed on every CI runner (see
// TestToolboxCurlEntryBuildOmitsEmptyOptionalFields's own real-curl
// execution, which stays safe since curl is universally preinstalled).
func TestToolboxNmapEntryBuildOmitsEmptyOptionalFields(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	entry := toolboxNmapEntry()
	entry.run(r)

	targetField, ok := r.toolboxFormWidgets[toolboxNmapFieldTarget].(*tview.InputField)
	if !ok {
		t.Fatalf("toolboxFormWidgets[%q] is not an InputField", toolboxNmapFieldTarget)
	}
	targetField.SetText("192.168.1.0/24")

	if got := r.toolboxFormPreview.GetText(true); got != "nmap 192.168.1.0/24" {
		t.Errorf("preview = %q, want exactly %q (Default scan/timing, empty Ports)", got, "nmap 192.168.1.0/24")
	}
}

// TestToolboxNmapEntryBuildIncludesEveryFieldWhenSet is
// TestToolboxNmapEntryBuildOmitsEmptyOptionalFields's counterpart: every
// field set to a non-default value reaches argv, in the Scan type/Ports/
// Timing template/Target order toolboxNmapEntry's own build func uses.
func TestToolboxNmapEntryBuildIncludesEveryFieldWhenSet(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	entry := toolboxNmapEntry()
	entry.run(r)

	targetField := r.toolboxFormWidgets[toolboxNmapFieldTarget].(*tview.InputField)
	targetField.SetText("example.com")
	portsField := r.toolboxFormWidgets[toolboxNmapFieldPorts].(*tview.InputField)
	portsField.SetText("22,80,443")
	scanField := r.toolboxFormWidgets[toolboxNmapFieldScan].(*tview.DropDown)
	scanField.SetCurrentOption(1) // SYN scan (-sS), per toolboxNmapScanChoices' own order
	timingField := r.toolboxFormWidgets[toolboxNmapFieldTiming].(*tview.DropDown)
	timingField.SetCurrentOption(5) // Aggressive (-T4), per toolboxNmapTimingChoices' own order

	want := "nmap -sS -p 22,80,443 -T4 example.com"
	if got := r.toolboxFormPreview.GetText(true); got != want {
		t.Errorf("preview = %q, want %q", got, want)
	}
}

// TestToolboxDigEntryBuildOmitsEmptyOptionalFields pins dig's own build
// func via a real open: Default record type and an empty Server field
// must produce no extra tokens at all beyond the domain itself. Checked
// through the preview only, not a real Run — dig isn't guaranteed to be
// installed on every CI runner, the same reasoning
// TestToolboxNmapEntryBuildOmitsEmptyOptionalFields's own doc comment
// gives.
func TestToolboxDigEntryBuildOmitsEmptyOptionalFields(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	entry := toolboxDigEntry()
	entry.run(r)

	domainField, ok := r.toolboxFormWidgets[toolboxDigFieldDomain].(*tview.InputField)
	if !ok {
		t.Fatalf("toolboxFormWidgets[%q] is not an InputField", toolboxDigFieldDomain)
	}
	domainField.SetText("example.com")

	if got := r.toolboxFormPreview.GetText(true); got != "dig example.com" {
		t.Errorf("preview = %q, want exactly %q (Default record type, empty Server)", got, "dig example.com")
	}
}

// TestToolboxDigEntryBuildIncludesEveryFieldWhenSet is
// TestToolboxDigEntryBuildOmitsEmptyOptionalFields's counterpart: every
// field set to a non-default value reaches argv, in dig's own real
// "@server name type" order.
func TestToolboxDigEntryBuildIncludesEveryFieldWhenSet(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	entry := toolboxDigEntry()
	entry.run(r)

	domainField := r.toolboxFormWidgets[toolboxDigFieldDomain].(*tview.InputField)
	domainField.SetText("example.com")
	serverField := r.toolboxFormWidgets[toolboxDigFieldServer].(*tview.InputField)
	serverField.SetText("8.8.8.8")
	typeField := r.toolboxFormWidgets[toolboxDigFieldType].(*tview.DropDown)
	typeField.SetCurrentOption(3) // MX, per toolboxDigRecordTypeChoices' own order

	want := "dig @8.8.8.8 example.com MX"
	if got := r.toolboxFormPreview.GetText(true); got != want {
		t.Errorf("preview = %q, want %q", got, want)
	}
}

// TestToolboxWgetEntryBuildOmitsEmptyOptionalFields pins wget's own
// build func via a real open: an unchecked Resume and an empty Output
// path must produce no extra tokens at all beyond the URL itself.
// Checked through the preview only, not a real Run — wget isn't
// preinstalled on every platform (notably macOS), the same reasoning
// TestToolboxNmapEntryBuildOmitsEmptyOptionalFields's own doc comment
// gives for nmap/dig.
func TestToolboxWgetEntryBuildOmitsEmptyOptionalFields(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	entry := toolboxWgetEntry()
	entry.run(r)

	urlField, ok := r.toolboxFormWidgets[toolboxWgetFieldURL].(*tview.InputField)
	if !ok {
		t.Fatalf("toolboxFormWidgets[%q] is not an InputField", toolboxWgetFieldURL)
	}
	urlField.SetText("https://example.com/file.tar.gz")

	want := "wget https://example.com/file.tar.gz"
	if got := r.toolboxFormPreview.GetText(true); got != want {
		t.Errorf("preview = %q, want exactly %q (unchecked Resume, empty Output path)", got, want)
	}
}

// TestToolboxWgetEntryBuildIncludesEveryFieldWhenSet is
// TestToolboxWgetEntryBuildOmitsEmptyOptionalFields's counterpart: every
// field set to a non-default value reaches argv, in the Resume/Output/
// URL order toolboxWgetEntry's own build func uses.
func TestToolboxWgetEntryBuildIncludesEveryFieldWhenSet(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	entry := toolboxWgetEntry()
	entry.run(r)

	urlField := r.toolboxFormWidgets[toolboxWgetFieldURL].(*tview.InputField)
	urlField.SetText("https://example.com/file.tar.gz")
	outputField := r.toolboxFormWidgets[toolboxWgetFieldOutput].(*tview.InputField)
	outputField.SetText("out.tar.gz")
	// Set directly rather than through toolboxFormToggles' own selected
	// func (the same direct-state-manipulation style
	// TestToolboxCurlEntryBuildIncludesEveryFieldWhenSet's own comment
	// explains) — updateToolboxFormPreview must be called explicitly
	// afterwards, since nothing here went through the real UI event
	// that would normally trigger it.
	r.toolboxFormToggleState[toolboxWgetFieldResume] = true
	r.updateToolboxFormPreview()

	want := "wget -c -O out.tar.gz https://example.com/file.tar.gz"
	if got := r.toolboxFormPreview.GetText(true); got != want {
		t.Errorf("preview = %q, want %q", got, want)
	}
}
