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

// duplicateStrategyDropDown returns the dialog's own Strategy field —
// always item index 1 (Target is 0) regardless of which strategy is
// currently selected, since Target and Strategy are the only two items
// renderDuplicateForm ever adds unconditionally at fixed positions.
func duplicateStrategyDropDown(t *testing.T, r *Root) *tview.DropDown {
	t.Helper()
	dd, ok := r.duplicateForm.GetFormItem(1).(*tview.DropDown)
	if !ok {
		t.Fatalf("form item 1 is not the Strategy dropdown")
	}
	return dd
}

// selectDuplicateStrategy switches the dialog's own Strategy dropdown
// to value ("numbered"/"suffix_text"/"datetime") — DropDown.
// SetCurrentOption fires the same "selected" callback a real arrow-
// key-and-Enter or mouse pick would (verified directly against
// tview's own dropdown.go, not assumed), so this exercises the real
// renderDuplicateForm rebuild, not a shortcut around it.
func selectDuplicateStrategy(t *testing.T, r *Root, value string) {
	t.Helper()
	opt, ok := optionSpecByKey("duplicate_strategy")
	if !ok {
		t.Fatal("no duplicate_strategy optionSpec")
	}
	for i, c := range opt.choices(r) {
		if c.value == value {
			duplicateStrategyDropDown(t, r).SetCurrentOption(i)
			return
		}
	}
	t.Fatalf("no such strategy choice %q", value)
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
	for _, label := range []string{"Target", "Strategy", "Separator", "Number padding (digits)", "Number of duplicates"} {
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

// TestDuplicateStrategyFieldUsesThemeColorsWhenFocused is the
// regression pin for a real, reported bug: DropDown keeps its own
// separate focusedStyle that Form's generic per-item theming
// (SetFieldBackgroundColor/SetFieldTextColor, applied via
// SetFormAttributes) never reaches — only DropDown.SetFocusedStyle
// itself does (see renderDuplicateForm's own doc comment) — so without
// an explicit SetFocusedStyle call, Strategy rendered in tview's own
// stock blue-on-white instead of this app's own palette while it had
// real focus, which it does by default the moment the dialog opens
// (Target, a TextView, never takes it). Verified against a real
// rendered screen (tcell.SimulationScreen), not just that some setter
// was called — a plausible fix that silently didn't reach the actually
// visible style was exactly the first attempt at this.
func TestDuplicateStrategyFieldUsesThemeColorsWhenFocused(t *testing.T) {
	r, _, _ := newTestRootWithDuplicateFile(t, "hello\n")
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	r.app.SetScreen(screen) // must run before SetSize: SetScreen re-Init()s an unstarted screen, undoing an earlier SetSize
	screen.SetSize(100, 40)
	r.SetRect(0, 0, 100, 40)

	r.openDuplicate()
	screen.Clear()
	r.Draw(screen)

	found := false
	w, h := screen.Size()
	for y := 0; y < h && !found; y++ {
		for x := 0; x < w-8; x++ {
			text := ""
			for i := 0; i < 8; i++ {
				c, _, _ := screen.Get(x+i, y)
				text += c
			}
			if text != "Numbered" {
				continue
			}
			found = true
			_, style, _ := screen.Get(x, y)
			fg, bg, _ := style.Decompose()
			if want := r.theme.Text; fg != want {
				t.Errorf("Strategy field foreground = %v, want theme.Text %v", fg, want)
			}
			if want := r.theme.FocusedBackground; bg != want {
				t.Errorf("Strategy field background = %v, want theme.FocusedBackground %v", bg, want)
			}
		}
	}
	if !found {
		t.Fatal("could not find the rendered 'Numbered' option text at all")
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

	if r.duplicateSeparatorValue != "-" {
		t.Errorf("duplicateSeparatorValue = %q, want %q", r.duplicateSeparatorValue, "-")
	}
	if r.duplicateSuffixTextValue != "bak" {
		t.Errorf("duplicateSuffixTextValue = %q, want %q", r.duplicateSuffixTextValue, "bak")
	}
	if r.duplicateNumberPaddingValue != "3" {
		t.Errorf("duplicateNumberPaddingValue = %q, want %q", r.duplicateNumberPaddingValue, "3")
	}
	if got := r.duplicateDateTimeFormatField.GetText(); got != "15-04-05" {
		t.Errorf("Date/time format field = %q, want %q", got, "15-04-05")
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

// TestRenderDuplicateFormShowsOnlyRelevantFieldsPerStrategy pins the
// core complaint the strategy-driven redesign exists to fix: the form
// must never show every strategy's own fields combined at once —
// "Suffix text" has no business being editable while "Numbered" is
// selected, and vice versa.
func TestRenderDuplicateFormShowsOnlyRelevantFieldsPerStrategy(t *testing.T) {
	r, _, _ := newTestRootWithDuplicateFile(t, "hello\n")
	r.openDuplicate() // default strategy: numbered

	cases := []struct {
		strategy string
		want     []string
		wantNot  []string
	}{
		{"numbered", []string{"Number padding (digits)"}, []string{"Suffix text", "Date/time format", duplicateLabelStrftime, duplicateLabelUseUnix}},
		{"suffix_text", []string{"Suffix text"}, []string{"Number padding (digits)", "Date/time format", duplicateLabelStrftime, duplicateLabelUseUnix}},
		{"datetime", []string{"Date/time format", duplicateLabelStrftime, duplicateLabelUseUnix}, []string{"Suffix text", "Number padding (digits)"}},
	}
	for _, c := range cases {
		selectDuplicateStrategy(t, r, c.strategy)
		for _, label := range c.want {
			if r.duplicateForm.GetFormItemByLabel(label) == nil {
				t.Errorf("strategy %q: form is missing %q", c.strategy, label)
			}
		}
		for _, label := range c.wantNot {
			if r.duplicateForm.GetFormItemByLabel(label) != nil {
				t.Errorf("strategy %q: form should not show %q", c.strategy, label)
			}
		}
	}
}

// TestRenderDuplicateFormPreservesValuesAcrossStrategySwitch pins that
// switching strategies rebuilds the Form's own *widgets* without
// losing any value already typed — Separator and Number of duplicates
// are shown by every strategy, so their own widgets are destroyed and
// recreated on every switch too, not just the strategy-specific ones.
func TestRenderDuplicateFormPreservesValuesAcrossStrategySwitch(t *testing.T) {
	r, _, _ := newTestRootWithDuplicateFile(t, "hello\n")
	r.openDuplicate()

	r.duplicateSeparatorField.SetText("~")
	r.duplicateCountField.SetText("4")

	selectDuplicateStrategy(t, r, "datetime")

	if got := r.duplicateSeparatorField.GetText(); got != "~" {
		t.Errorf("Separator after switching strategy = %q, want %q (preserved)", got, "~")
	}
	if got := r.duplicateCountField.GetText(); got != "4" {
		t.Errorf("Number of duplicates after switching strategy = %q, want %q (preserved)", got, "4")
	}
}

// TestRenderDuplicatePreviewShowsComputedName pins the live preview
// against a real, on-disk-checked computation — report.txt exists,
// report.txt_1 doesn't, so the default Numbered strategy's own preview
// must read exactly "Preview: report.txt_1" — always after the
// *entire* original name, extension included, per the user's own
// explicit correction (see ComputeDuplicateName's own doc comment).
func TestRenderDuplicatePreviewShowsComputedName(t *testing.T) {
	r, _, _ := newTestRootWithDuplicateFile(t, "hello\n")
	r.openDuplicate()

	if got, want := r.duplicatePreviewView.GetText(true), "Preview: report.txt_1"; got != want {
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

	if got, want := r.duplicatePreviewView.GetText(true), "Preview: report.txt-1"; got != want {
		t.Errorf("preview after editing Separator = %q, want %q", got, want)
	}
}

// TestRenderDuplicatePreviewReactsToStrategyChange pins that switching
// strategy alone (no other field touched) is enough to update the
// preview — the exact case a real, reported confusion ("aktueller Name
// wird falsch interpretiert") traced back to when the old design let
// every strategy's fields sit combined on screen.
func TestRenderDuplicatePreviewReactsToStrategyChange(t *testing.T) {
	r, _, _ := newTestRootWithDuplicateFile(t, "hello\n")
	r.openDuplicate()

	selectDuplicateStrategy(t, r, "suffix_text")

	if got, want := r.duplicatePreviewView.GetText(true), "Preview: report.txt_copy"; got != want {
		t.Errorf("preview after switching to suffix_text = %q, want %q", got, want)
	}
}

// TestRenderDuplicatePreviewNeverSplitsOffAnyExtension is the UI-level
// pin for the same regression ComputeDuplicateName's own tests already
// cover: the preview must never insert anything before an extension,
// or before just the first dot of a compound one.
func TestRenderDuplicatePreviewNeverSplitsOffAnyExtension(t *testing.T) {
	r, dir, _ := newTestRootWithDuplicateFile(t, "hello\n")
	archive := filepath.Join(dir, "archive.tar.gz")
	if err := os.WriteFile(archive, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r.openDuplicate()
	r.duplicateTargets = []string{archive}
	r.renderDuplicatePreview()

	if got, want := r.duplicatePreviewView.GetText(true), "Preview: archive.tar.gz_1"; got != want {
		t.Errorf("preview = %q, want %q", got, want)
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
	if !strings.Contains(got, "report.txt_1") {
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
	selectDuplicateStrategy(t, r, "suffix_text")
	r.duplicateSuffixTextField.SetText("bak")

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
		path := filepath.Join(dir, "report.txt_"+strconv.Itoa(n))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("report.txt_%d: %v", n, err)
			continue
		}
		if string(data) != "hello\n" {
			t.Errorf("report.txt_%d content = %q, want %q", n, data, "hello\n")
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
		t.Fatal("second run should have failed: report.txt_copy already exists")
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

	for _, name := range []string{"report.txt-1", "report.txt-2"} {
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
