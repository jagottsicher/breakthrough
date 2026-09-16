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

// TestOpenToolboxRendersTheWholeCatalog pins openToolbox's own basic
// contract: it shows the Toolbox page and fills the table with exactly
// one row per toolboxDisplayRows entry, cursor landing on a real one.
func TestOpenToolboxRendersTheWholeCatalog(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openToolbox()

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

// TestCaptureToolboxKeyEscapeClosesTheScreen pins the one key this
// screen's own capture handles directly — Enter already reaches
// activateToolboxRow through the table's own SetSelectedFunc, the same
// as every other selectable table in this app.
func TestCaptureToolboxKeyEscapeClosesTheScreen(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openToolbox()

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
	r.openToolbox()

	r.activateToolboxRow(0) // the leading "Networking" header

	if len(r.toolWindows) != 0 {
		t.Errorf("len(toolWindows) = %d, want 0 — activating a header row should do nothing", len(r.toolWindows))
	}
	if r.activePage != toolboxPage {
		t.Errorf("activePage = %q, want the Toolbox screen to still be open", r.activePage)
	}
}
