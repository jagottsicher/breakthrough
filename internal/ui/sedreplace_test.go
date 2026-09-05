package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/replace"
)

// newTestRootWithSedFile is newTestRootWithFile's own counterpart for
// this file's tests — the trash tests' helper (trash_test.go) also
// points $XDG_RUNTIME_DIR somewhere throwaway, which Sed Replace has no
// need for, so this one skips that setup.
func newTestRootWithSedFile(t *testing.T, content string) (r *Root, dir, file string) {
	t.Helper()
	dir = t.TempDir()
	file = filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1) // off ".." onto the one real entry
	return r, dir, file
}

// sedSetFlag sets one of sedFlagsList's toggles directly by label — the
// map itself, not simulating a click, since these tests are about what
// buildSedScript/Apply do with a given flag state, not the click/toggle
// mechanism (see TestToggleSedFlagUpdatesStateAndLabel for that).
func sedSetFlag(t *testing.T, r *Root, label string, checked bool) {
	t.Helper()
	if _, ok := r.sedFlags[label]; !ok {
		t.Fatalf("no such sed flag %q", label)
	}
	r.sedFlags[label] = checked
}

// blockingSedPreviewFunc is a sedPreviewFunc replacement that never
// returns — the same reasoning fakeSearchRun's own doc comment gives for
// leaving its channels open forever: runSedPreview's background
// goroutine would otherwise reach r.app.QueueUpdateDraw, which blocks
// forever with nothing here running the real Application event loop to
// drain it. Tests using this only ever care about runSedPreview's own
// synchronous setup (activePage, sedPreviewTotal, sedPreviewCancel) —
// never anything that depends on a preview actually finishing (see
// TestShowSedPreviewResult* below for that, called directly instead).
func blockingSedPreviewFunc(_ []string, _ string, _ bool, _ func(string)) ([]replace.FileChange, map[string]string, error) {
	select {}
}

// isolateSedPreviewFunc overrides sedPreviewFunc for the duration of t,
// restoring the real replace.Preview afterward.
func isolateSedPreviewFunc(t *testing.T, fake func([]string, string, bool, func(string)) ([]replace.FileChange, map[string]string, error)) {
	t.Helper()
	original := sedPreviewFunc
	sedPreviewFunc = fake
	t.Cleanup(func() { sedPreviewFunc = original })
}

func TestOpenSedReplacePopulatesTargetAndOpensForm(t *testing.T) {
	r, _, file := newTestRootWithSedFile(t, "hello world\n")

	r.openSedReplace()

	if r.activePage != sedReplacePage {
		t.Fatalf("activePage = %q, want %q", r.activePage, sedReplacePage)
	}
	if len(r.sedTargets) != 1 || r.sedTargets[0] != file {
		t.Fatalf("sedTargets = %v, want [%s]", r.sedTargets, file)
	}
	for _, label := range []string{"Find", "Replace with", "Advanced sed script (overrides Find/Replace above)"} {
		if r.sedForm.GetFormItemByLabel(label) == nil {
			t.Errorf("form is missing an item labeled %q", label)
		}
	}
}

func TestToggleSedFlagUpdatesStateAndLabel(t *testing.T) {
	r, _, _ := newTestRootWithSedFile(t, "hello world\n")
	r.openSedReplace()

	if r.sedFlags[sedLabelRegex] {
		t.Fatal("setup: Regex should default to false")
	}

	r.toggleSedFlag(sedLabelRegex)
	if !r.sedFlags[sedLabelRegex] {
		t.Error("toggleSedFlag should have flipped Regex to true")
	}
	idx := -1
	for i, label := range sedFlagOrder {
		if label == sedLabelRegex {
			idx = i
		}
	}
	main, _ := r.sedFlagsList.GetItemText(idx)
	if main != sedFlagItemText(sedLabelRegex, true) {
		t.Errorf("list item text = %q, want %q", main, sedFlagItemText(sedLabelRegex, true))
	}

	r.toggleSedFlag(sedLabelRegex)
	if r.sedFlags[sedLabelRegex] {
		t.Error("toggleSedFlag should have flipped Regex back to false")
	}
}

func TestBuildSedScriptGuidedMode(t *testing.T) {
	r, _, _ := newTestRootWithSedFile(t, "hello world\n")
	r.openSedReplace()
	r.sedFindField.SetText("hello")
	r.sedReplaceField.SetText("goodbye")

	script, extendedRegex, err := r.buildSedScript()
	if err != nil {
		t.Fatalf("buildSedScript: %v", err)
	}
	if extendedRegex {
		t.Error("extendedRegex should default to false")
	}
	want, err := replace.BuildScript("hello", "goodbye", false, false, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if script != want {
		t.Errorf("buildSedScript() script = %q, want %q", script, want)
	}
}

func TestBuildSedScriptAdvancedFieldOverridesGuidedFields(t *testing.T) {
	r, _, _ := newTestRootWithSedFile(t, "hello world\n")
	r.openSedReplace()
	r.sedFindField.SetText("hello")
	r.sedReplaceField.SetText("goodbye")
	r.sedAdvancedField.SetText("s/world/there/")
	sedSetFlag(t, r, sedLabelExtendedRegex, true)

	script, extendedRegex, err := r.buildSedScript()
	if err != nil {
		t.Fatalf("buildSedScript: %v", err)
	}
	if script != "s/world/there/" {
		t.Errorf("script = %q, want the advanced field verbatim", script)
	}
	if !extendedRegex {
		t.Error("extendedRegex should still apply to an advanced script (it's a real sed invocation flag, not just guided-mode escaping)")
	}
}

func TestRunSedPreviewOpensPreviewPageWithProgressState(t *testing.T) {
	r, _, file := newTestRootWithSedFile(t, "hello world\n")
	r.openSedReplace()
	r.sedFindField.SetText("hello")
	r.sedReplaceField.SetText("goodbye")

	isolateSedPreviewFunc(t, blockingSedPreviewFunc)
	r.runSedPreview()

	// Everything below is set synchronously in runSedPreview, before its
	// background goroutine ever reaches sedPreviewFunc - safe to check
	// immediately, unlike anything that would depend on a preview
	// actually completing (see blockingSedPreviewFunc's own doc comment).
	if r.activePage != sedPreviewPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, sedPreviewPage)
	}
	if r.sedPreviewTotal != 1 {
		t.Errorf("sedPreviewTotal = %d, want 1 (len(sedTargets))", r.sedPreviewTotal)
	}
	if r.sedPreviewCancel == nil {
		t.Error("sedPreviewCancel should be set while a preview is in flight")
	}
	_ = file
}

func TestShowSedPreviewResultPopulatesChangesAndTable(t *testing.T) {
	r, _, file := newTestRootWithSedFile(t, "hello world\n")
	r.openSedReplace()

	changes := []replace.FileChange{{Path: file, Before: []byte("hello world\n"), After: []byte("goodbye world\n")}}
	r.showSedPreviewResult(changes, nil, nil)

	if len(r.sedPendingChanges) != 1 || r.sedPendingChanges[0].Path != file {
		t.Fatalf("sedPendingChanges = %+v, want exactly one change for %s", r.sedPendingChanges, file)
	}
	if r.sedPreviewTable.GetRowCount() < 2 { // header + at least one data row
		t.Errorf("sedPreviewTable has %d rows, want a header plus at least one change", r.sedPreviewTable.GetRowCount())
	}
	name := r.sedPreviewTable.GetCell(1, 0).Text
	if name != filepath.Base(file) {
		t.Errorf("row 1 name cell = %q, want %q", name, filepath.Base(file))
	}
	excerpt := r.sedPreviewTable.GetCell(1, 2).Text
	if excerpt != "goodbye world" {
		t.Errorf("row 1 excerpt cell = %q, want %q", excerpt, "goodbye world")
	}
}

func TestShowSedPreviewResultErrorClosesDialogAndShowsError(t *testing.T) {
	r, _, _ := newTestRootWithSedFile(t, "hello world\n")
	r.openSedReplace()

	r.showSedPreviewResult(nil, nil, os.ErrInvalid)

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, errorPage)
	}
}

func TestSedPreviewRowsChangedLineFile(t *testing.T) {
	changes := []replace.FileChange{{Path: "/tmp/a.txt", Before: []byte("hello\nworld\n"), After: []byte("hello\nthere\n")}}
	rows := sedPreviewRows(changes, nil)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want exactly 1", rows)
	}
	if rows[0] != (sedPreviewRow{name: "a.txt", line: "2", excerpt: "there"}) {
		t.Errorf("rows[0] = %+v, want {a.txt 2 there false}", rows[0])
	}
}

func TestSedPreviewRowsLineCountChangeGetsSummaryRow(t *testing.T) {
	changes := []replace.FileChange{{Path: "/tmp/a.txt", Before: []byte("one\ntwo\n"), After: []byte("one\n")}}
	rows := sedPreviewRows(changes, nil)
	if len(rows) != 1 || rows[0].line != "-" {
		t.Fatalf("rows = %+v, want one summary row with line \"-\"", rows)
	}
}

func TestSedPreviewRowsSkippedSortedByPath(t *testing.T) {
	skipped := map[string]string{"/tmp/z.txt": "is a directory", "/tmp/a.txt": "looks like a binary file"}
	rows := sedPreviewRows(nil, skipped)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want exactly 2", rows)
	}
	if rows[0].name != "a.txt" || rows[1].name != "z.txt" {
		t.Errorf("rows = %+v, want a.txt before z.txt", rows)
	}
	if !rows[0].skipped || !rows[1].skipped {
		t.Errorf("rows = %+v, want both marked skipped", rows)
	}
}

func TestConfirmApplySedCancelPreselectedDoesNotWrite(t *testing.T) {
	r, _, file := newTestRootWithSedFile(t, "hello world\n")
	r.openSedReplace()
	r.showSedPreviewResult([]replace.FileChange{{Path: file, Before: []byte("hello world\n"), After: []byte("goodbye world\n")}}, nil, nil)

	r.confirmApplySed()
	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, confirmPage)
	}
	if got := r.confirmDialog.GetCurrentItem(); got != 1 {
		t.Fatalf("preselected item = %d, want 1 (Cancel)", got)
	}
	r.cancelConfirm()

	data, _ := os.ReadFile(file)
	if string(data) != "hello world\n" {
		t.Errorf("file was modified despite Cancel being preselected: %q", data)
	}
}

func TestConfirmApplySedConfirmedWritesChanges(t *testing.T) {
	r, _, file := newTestRootWithSedFile(t, "hello world\n")
	r.openSedReplace()
	r.showSedPreviewResult([]replace.FileChange{{Path: file, Before: []byte("hello world\n"), After: []byte("goodbye world\n")}}, nil, nil)

	r.confirmApplySed()
	r.confirmDialog.SetCurrentItem(2) // "Yes, delete permanently" - see newPurgeConfirm
	r.acceptConfirm()

	data, err := os.ReadFile(file)
	if err != nil || string(data) != "goodbye world\n" {
		t.Fatalf("file content = %q, %v, want %q", data, err, "goodbye world\n")
	}
	if len(r.sedPendingChanges) != 0 {
		t.Error("sedPendingChanges should be cleared after applying")
	}
	if r.activePage == sedPreviewPage || r.activePage == confirmPage {
		t.Errorf("activePage = %q, want the dialog closed", r.activePage)
	}
}

func TestConfirmApplySedWithBackupKeepsOriginal(t *testing.T) {
	r, _, file := newTestRootWithSedFile(t, "hello world\n")
	r.openSedReplace()
	sedSetFlag(t, r, sedLabelBackup, true)
	r.showSedPreviewResult([]replace.FileChange{{Path: file, Before: []byte("hello world\n"), After: []byte("goodbye world\n")}}, nil, nil)

	r.confirmApplySed()
	r.confirmDialog.SetCurrentItem(2)
	r.acceptConfirm()

	backup, err := os.ReadFile(file + ".bak")
	if err != nil || string(backup) != "hello world\n" {
		t.Fatalf(".bak content = %q, %v, want the original content", backup, err)
	}
}

func TestBackToSedFormPreservesFieldValuesAndCancelsPreview(t *testing.T) {
	r, _, _ := newTestRootWithSedFile(t, "hello world\n")
	r.openSedReplace()
	r.sedFindField.SetText("hello")

	isolateSedPreviewFunc(t, blockingSedPreviewFunc)
	r.runSedPreview()
	if r.sedPreviewCancel == nil {
		t.Fatal("setup: expected a preview in flight")
	}

	r.backToSedForm()

	if r.activePage != sedReplacePage {
		t.Fatalf("activePage = %q, want %q", r.activePage, sedReplacePage)
	}
	if got := r.sedFindField.GetText(); got != "hello" {
		t.Errorf("Find field = %q after Back, want it preserved as %q", got, "hello")
	}
	if r.sedPreviewCancel != nil {
		t.Error("backToSedForm should have cancelled the in-flight preview")
	}
}

func TestSedReplaceShortcutNoOpWhileOverlayIsOpen(t *testing.T) {
	r, _, file := newTestRootWithSedFile(t, "hello world\n")

	r.openOptions() // any overlay; makes acceptsGlobalShortcut false
	r.SedReplaceShortcut()

	if r.activePage != optionsPage {
		t.Errorf("SedReplaceShortcut should not have acted while Options was open, activePage = %q", r.activePage)
	}
	data, _ := os.ReadFile(file)
	if string(data) != "hello world\n" {
		t.Error("file should be untouched")
	}
}
