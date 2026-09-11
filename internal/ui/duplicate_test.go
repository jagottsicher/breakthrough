package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// newTestRootWithDuplicateFile is newTestRootWithSedFile's own
// counterpart for this file's tests.
func newTestRootWithDuplicateFile(t *testing.T, content string) (r *Root, dir, file string) {
	t.Helper()
	dir = t.TempDir()
	file = filepath.Join(dir, "report.txt")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40) // openDuplicate sizes/centers against this
	r.panel.focusRow(1)      // off ".." onto the one real entry
	return r, dir, file
}

func TestOpenDuplicatePopulatesTargetsAndOpensForm(t *testing.T) {
	r, _, file := newTestRootWithDuplicateFile(t, "hello\n")

	r.openDuplicate()

	if r.activePage != duplicatePage {
		t.Fatalf("activePage = %q, want %q", r.activePage, duplicatePage)
	}
	if len(r.duplicateTargets) != 1 || r.duplicateTargets[0] != file {
		t.Fatalf("duplicateTargets = %v, want [%s]", r.duplicateTargets, file)
	}
	for _, label := range []string{"Separator", "Suffix text", "Number padding (digits)", "Date/time format", "Number of duplicates", "Preview"} {
		if r.duplicateForm.GetFormItemByLabel(label) == nil {
			t.Errorf("form is missing an item labeled %q", label)
		}
	}
}

func TestOpenDuplicateHasATitleBar(t *testing.T) {
	r, _, _ := newTestRootWithDuplicateFile(t, "hello\n")

	r.openDuplicate()

	if got, want := r.duplicateTitleBar.GetText(true), " Multiply "; got != want {
		t.Errorf("duplicateTitleBar text = %q, want %q", got, want)
	}
	if _, _, w, h := r.duplicateLayout.GetRect(); w <= 0 || h <= 0 {
		t.Errorf("duplicateLayout rect = %dx%d, want a real, positioned size", w, h)
	}
}

// TestResetDuplicateFormPrefillsFromSettings pins the user's own design
// intent: the dialog always starts out showing "what would happen right
// now" (today's sticky defaults), never a blank form.
func TestResetDuplicateFormPrefillsFromSettings(t *testing.T) {
	r, _, _ := newTestRootWithDuplicateFile(t, "hello\n")
	r.settings.DuplicateSeparator = "-"
	r.settings.DuplicateSuffixText = "bak"
	r.settings.DuplicateNumberPadding = 3
	r.settings.DuplicateDateTimeFormat = "15-04-05"
	r.settings.DuplicateCount = 2
	r.settings.DuplicateStrategy = "datetime"
	r.settings.DuplicateDateTimeStrftime = true
	r.settings.DuplicateDateTimeUseUnix = true

	r.openDuplicate()

	if got := r.duplicateSeparatorField.GetText(); got != "-" {
		t.Errorf("Separator = %q, want %q", got, "-")
	}
	if got := r.duplicateSuffixTextField.GetText(); got != "bak" {
		t.Errorf("Suffix text = %q, want %q", got, "bak")
	}
	if got := r.duplicateNumberPaddingField.GetText(); got != "3" {
		t.Errorf("Number padding = %q, want %q", got, "3")
	}
	if got := r.duplicateDateTimeFormatField.GetText(); got != "15-04-05" {
		t.Errorf("Date/time format = %q, want %q", got, "15-04-05")
	}
	if got := r.duplicateCountField.GetText(); got != "2" {
		t.Errorf("Number of duplicates = %q, want %q", got, "2")
	}
	if r.duplicateStrategy != "datetime" {
		t.Errorf("duplicateStrategy = %q, want %q", r.duplicateStrategy, "datetime")
	}
	if !r.duplicateFlags[duplicateLabelStrftime] || !r.duplicateFlags[duplicateLabelUseUnix] {
		t.Errorf("duplicateFlags = %v, want both true", r.duplicateFlags)
	}
}

// TestCycleDuplicateStrategyAdvancesThroughAllChoicesAndWraps pins that
// the Strategy row cycles through exactly the same three choices
// "duplicate_strategy" itself offers on the Options screen (see
// optionSpecByKey), in order, wrapping back to the first.
func TestCycleDuplicateStrategyAdvancesThroughAllChoicesAndWraps(t *testing.T) {
	r, _, _ := newTestRootWithDuplicateFile(t, "hello\n")
	r.openDuplicate()

	want := []string{"suffix_text", "datetime", "numbered"}
	for i, w := range want {
		r.cycleDuplicateStrategy()
		if r.duplicateStrategy != w {
			t.Fatalf("after cycle %d: duplicateStrategy = %q, want %q", i+1, r.duplicateStrategy, w)
		}
	}

	main, _ := r.duplicateOptionsList.GetItemText(0)
	if main != "Strategy: Numbered" {
		t.Errorf("duplicateOptionsList row 0 = %q, want %q", main, "Strategy: Numbered")
	}
}

func TestToggleDuplicateFlagFlipsStateAndLabel(t *testing.T) {
	r, _, _ := newTestRootWithDuplicateFile(t, "hello\n")
	r.openDuplicate()

	if r.duplicateFlags[duplicateLabelStrftime] {
		t.Fatal("setup: Strftime should start off (settings default)")
	}

	r.toggleDuplicateFlag(duplicateLabelStrftime)

	if !r.duplicateFlags[duplicateLabelStrftime] {
		t.Error("toggleDuplicateFlag should have flipped the flag to true")
	}
	main, _ := r.duplicateOptionsList.GetItemText(1)
	if want := duplicateFlagItemText(duplicateLabelStrftime, true); main != want {
		t.Errorf("duplicateOptionsList row 1 = %q, want %q", main, want)
	}
}

// TestRenderDuplicatePreviewShowsComputedName pins the live preview
// against a real, on-disk-checked computation — report.txt exists,
// report_1.txt doesn't, so the default Numbered strategy's own preview
// must read exactly "report_1.txt".
func TestRenderDuplicatePreviewShowsComputedName(t *testing.T) {
	r, _, _ := newTestRootWithDuplicateFile(t, "hello\n")
	r.openDuplicate()

	if got, want := r.duplicatePreviewView.GetText(true), "report_1.txt"; got != want {
		t.Errorf("preview = %q, want %q", got, want)
	}
}

// TestRenderDuplicatePreviewReactsToSeparatorEdits pins that the
// preview is genuinely live — driven by SetChangedFunc, not just
// computed once at open.
func TestRenderDuplicatePreviewReactsToSeparatorEdits(t *testing.T) {
	r, _, _ := newTestRootWithDuplicateFile(t, "hello\n")
	r.openDuplicate()

	r.duplicateSeparatorField.SetText("-")

	if got, want := r.duplicatePreviewView.GetText(true), "report-1.txt"; got != want {
		t.Errorf("preview after editing Separator = %q, want %q", got, want)
	}
}

// TestRenderDuplicatePreviewSummarizesMultipleTargetsAndCount pins the
// "+N more targets"/"M in total per target" summary for everything
// past the one real, on-disk-checked first preview (see
// renderDuplicatePreview's own doc comment for why the rest can't be
// simulated the same way).
func TestRenderDuplicatePreviewSummarizesMultipleTargetsAndCount(t *testing.T) {
	r, dir, file := newTestRootWithDuplicateFile(t, "hello\n")
	other := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(other, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r.openDuplicate()
	r.duplicateTargets = []string{file, other}
	r.duplicateCountField.SetText("3")

	got := r.duplicatePreviewView.GetText(true)
	if !strings.Contains(got, "report_1.txt") {
		t.Errorf("preview = %q, want it to mention the first target's own computed name", got)
	}
	if !strings.Contains(got, "+1 more targets") {
		t.Errorf("preview = %q, want it to mention the one remaining target", got)
	}
	if !strings.Contains(got, "3 in total per target") {
		t.Errorf("preview = %q, want it to mention the requested count", got)
	}
}

// TestApplyDuplicateSelectionWritesBackSettingsButNotCountMax pins the
// user's own explicit "self-adapting defaults" request: whatever is
// chosen in the dialog becomes the new sticky default for all eight
// dialog-facing settings, persisted to disk through the exact same
// optionSpec.apply functions the Options screen itself uses —
// "duplicate_count_max" is deliberately untouched, since the dialog
// never offers a field for it at all.
func TestApplyDuplicateSelectionWritesBackSettingsButNotCountMax(t *testing.T) {
	configPath := isolateUserConfigFile(t)
	r, _, _ := newTestRootWithDuplicateFile(t, "hello\n")
	r.openDuplicate()

	r.duplicateSeparatorField.SetText(".")
	r.cycleDuplicateStrategy() // numbered -> suffix_text
	r.duplicateSuffixTextField.SetText("bak")
	r.duplicateDateTimeFormatField.SetText("15-04-05")
	r.toggleDuplicateFlag(duplicateLabelStrftime)
	r.toggleDuplicateFlag(duplicateLabelUseUnix)

	r.applyDuplicateSelection(3, 2)

	switch {
	case r.settings.DuplicateSeparator != ".":
		t.Errorf("DuplicateSeparator = %q, want %q", r.settings.DuplicateSeparator, ".")
	case r.settings.DuplicateStrategy != "suffix_text":
		t.Errorf("DuplicateStrategy = %q, want %q", r.settings.DuplicateStrategy, "suffix_text")
	case r.settings.DuplicateSuffixText != "bak":
		t.Errorf("DuplicateSuffixText = %q, want %q", r.settings.DuplicateSuffixText, "bak")
	case r.settings.DuplicateNumberPadding != 3:
		t.Errorf("DuplicateNumberPadding = %d, want 3", r.settings.DuplicateNumberPadding)
	case r.settings.DuplicateDateTimeFormat != "15-04-05":
		t.Errorf("DuplicateDateTimeFormat = %q, want %q", r.settings.DuplicateDateTimeFormat, "15-04-05")
	case !r.settings.DuplicateDateTimeStrftime:
		t.Error("DuplicateDateTimeStrftime should be true")
	case !r.settings.DuplicateDateTimeUseUnix:
		t.Error("DuplicateDateTimeUseUnix should be true")
	case r.settings.DuplicateCount != 2:
		t.Errorf("DuplicateCount = %d, want 2", r.settings.DuplicateCount)
	case r.settings.DuplicateCountMax != 100:
		t.Errorf("DuplicateCountMax = %d, want the untouched default 100", r.settings.DuplicateCountMax)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading persisted config: %v", err)
	}
	for _, want := range []string{"duplicate_separator = .", "duplicate_strategy = suffix_text", "duplicate_suffix_text = bak"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("persisted config missing %q; got:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), "duplicate_count_max") {
		t.Errorf("persisted config should not mention duplicate_count_max at all; got:\n%s", data)
	}
}

// TestApplyDuplicateSelectionNeverRunsOnCancel pins the other half:
// experimenting with fields and then hitting Cancel must never touch
// r.settings at all — only runDuplicate's own confirm path calls
// applyDuplicateSelection.
func TestApplyDuplicateSelectionNeverRunsOnCancel(t *testing.T) {
	r, _, _ := newTestRootWithDuplicateFile(t, "hello\n")
	r.openDuplicate()

	r.duplicateSeparatorField.SetText("!!!")
	r.hideOverlay() // Cancel's own action

	if r.settings.DuplicateSeparator == "!!!" {
		t.Error("Cancel must not have written the edited Separator back to settings")
	}
}

// TestPerformDuplicateNumberedCreatesSequentialCopies is the core
// sabotage-worthy test: each of the three duplicates must be computed
// *after* the previous one actually landed on disk, or every one of
// them collides on the same first candidate. Content is checked too,
// confirming this is a real Copy, not just a name computation.
func TestPerformDuplicateNumberedCreatesSequentialCopies(t *testing.T) {
	_, dir, file := newTestRootWithDuplicateFile(t, "hello\n")
	opts := fsops.DuplicateOptions{Separator: "_", Strategy: fsops.DuplicateNumbered}

	created, err := performDuplicate([]string{file}, opts, fsops.CopyOptions{}, 3)
	if err != nil {
		t.Fatalf("performDuplicate: %v", err)
	}
	if created != 3 {
		t.Fatalf("created = %d, want 3", created)
	}
	for _, n := range []int{1, 2, 3} {
		path := filepath.Join(dir, "report_"+strconv.Itoa(n)+".txt")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("report_%d.txt: %v", n, err)
			continue
		}
		if string(data) != "hello\n" {
			t.Errorf("report_%d.txt content = %q, want %q", n, data, "hello\n")
		}
	}
}

// TestPerformDuplicateSuffixTextDoesNotRetry pins the documented,
// deliberate behavior: a fixed-suffix duplicate run twice produces an
// ordinary "already exists" failure the second time, not a silent
// second attempt at some other name.
func TestPerformDuplicateSuffixTextDoesNotRetry(t *testing.T) {
	_, _, file := newTestRootWithDuplicateFile(t, "hello\n")
	opts := fsops.DuplicateOptions{Separator: "_", Strategy: fsops.DuplicateSuffixText, SuffixText: "copy"}

	created, err := performDuplicate([]string{file}, opts, fsops.CopyOptions{}, 1)
	if err != nil || created != 1 {
		t.Fatalf("first run: created=%d, err=%v, want 1, nil", created, err)
	}

	created, err = performDuplicate([]string{file}, opts, fsops.CopyOptions{}, 1)
	if err == nil {
		t.Fatal("second run should have failed: report_copy.txt already exists")
	}
	if created != 0 {
		t.Errorf("second run created = %d, want 0", created)
	}
}

// isolateFailingCopy overrides fsCopy to succeed exactly succeedCount
// times, then fail — the one way to force performDuplicate's own
// stop-at-first-error path deterministically, since fsops.Copy itself
// never fails on this test's own plain, freshly-computed names.
func isolateFailingCopy(t *testing.T, succeedCount int) {
	t.Helper()
	original := fsCopy
	calls := 0
	fsCopy = func(src, dst string, opts fsops.CopyOptions) error {
		calls++
		if calls > succeedCount {
			return errors.New("simulated copy failure")
		}
		return original(src, dst, opts)
	}
	t.Cleanup(func() { fsCopy = original })
}

// TestPerformDuplicateStopsAtFirstErrorAndReportsCountSoFar pins that a
// mid-run failure doesn't silently discard how many duplicates already
// succeeded — the count runDuplicate's own error message reports.
func TestPerformDuplicateStopsAtFirstErrorAndReportsCountSoFar(t *testing.T) {
	_, _, file := newTestRootWithDuplicateFile(t, "hello\n")
	isolateFailingCopy(t, 2)
	opts := fsops.DuplicateOptions{Separator: "_", Strategy: fsops.DuplicateNumbered}

	created, err := performDuplicate([]string{file}, opts, fsops.CopyOptions{}, 5)

	if err == nil {
		t.Fatal("want an error once the third copy fails")
	}
	if created != 2 {
		t.Errorf("created = %d, want 2 (the two that succeeded before the failure)", created)
	}
}

func TestRunDuplicateRejectsNonPositiveCount(t *testing.T) {
	r, _, _ := newTestRootWithDuplicateFile(t, "hello\n")
	r.openDuplicate()
	r.duplicateCountField.SetText("0")
	before := r.settings.DuplicateSeparator

	r.runDuplicate()

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want %q (a non-positive count is rejected outright)", r.activePage, errorPage)
	}
	if r.settings.DuplicateSeparator != before {
		t.Error("a rejected count must not have applied/persisted anything")
	}
}

func TestRunDuplicateRejectsCountAboveMax(t *testing.T) {
	r, _, _ := newTestRootWithDuplicateFile(t, "hello\n")
	r.settings.DuplicateCountMax = 2
	r.openDuplicate()
	r.duplicateCountField.SetText("5")
	before := r.settings.DuplicateSeparator

	r.runDuplicate()

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want %q (a count above duplicate_count_max is rejected outright)", r.activePage, errorPage)
	}
	if r.settings.DuplicateSeparator != before {
		t.Error("a rejected count must not have applied/persisted anything")
	}
}

// isolateDuplicateIO overrides fsCopy for the duration of t, still
// calling through to the real fsops.Copy, but signaling once each call
// actually returns — the same technique isolatePasteIO already uses
// (see pasteconflict_test.go's own doc comment) to deterministically
// wait for a background goroutine's real, on-disk work without needing
// a running Application to drain QueueUpdateDraw.
func isolateDuplicateIO(t *testing.T) <-chan struct{} {
	t.Helper()
	done := make(chan struct{}, 64)
	original := fsCopy
	fsCopy = func(src, dst string, opts fsops.CopyOptions) error {
		err := original(src, dst, opts)
		done <- struct{}{}
		return err
	}
	t.Cleanup(func() { fsCopy = original })
	return done
}

// TestRunDuplicateAppliesSelectionClosesDialogAndCopiesInBackground is
// the end-to-end pin: a valid "Duplicate" press writes the sticky
// defaults back, closes the dialog immediately (not once the copy
// finishes), and actually creates the file(s) on disk in the
// background — the real fsCopy/QueueUpdateDraw hop this test's own
// isolateDuplicateIO exists to wait out.
func TestRunDuplicateAppliesSelectionClosesDialogAndCopiesInBackground(t *testing.T) {
	isolateUserConfigFile(t)
	r, dir, _ := newTestRootWithDuplicateFile(t, "hello\n")
	done := isolateDuplicateIO(t)
	r.openDuplicate()
	r.duplicateSeparatorField.SetText("-")
	r.duplicateCountField.SetText("2")

	r.runDuplicate()

	if r.activePage == duplicatePage {
		t.Fatal("runDuplicate should have closed the dialog immediately")
	}
	if r.settings.DuplicateSeparator != "-" {
		t.Errorf("DuplicateSeparator = %q, want the edited %q applied as the new default", r.settings.DuplicateSeparator, "-")
	}

	<-done
	<-done

	for _, name := range []string{"report-1.txt", "report-2.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestContextMenuMnemonicMOpensDuplicate pins "mm" — "m" opens the
// context menu (see keymap.go), and once it's open, "m" again fires
// "Multiply" directly, per the user's own explicit design.
func TestContextMenuMnemonicMOpensDuplicate(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.panel.focusRow(2)    // openDuplicate reads the panel's own cursor, not r.target — see moveSelectionToTrash's identical note
	openMenuOnRow(t, r, 2) // apple.txt

	r.menu.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'm', tcell.ModNone), func(tview.Primitive) {})

	if r.activePage != duplicatePage {
		t.Errorf("'m' should have opened Multiply from the context menu, activePage = %q", r.activePage)
	}
}
