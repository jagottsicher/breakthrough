package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rivo/tview"
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

func sedCheck(t *testing.T, r *Root, label string, checked bool) {
	t.Helper()
	item := r.sedForm.GetFormItemByLabel(label)
	cb, ok := item.(*tview.Checkbox)
	if !ok {
		t.Fatalf("form item %q is not a checkbox (or missing)", label)
	}
	cb.SetChecked(checked)
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

func TestRunSedPreviewGuidedModeFindsChange(t *testing.T) {
	r, _, file := newTestRootWithSedFile(t, "hello world\n")
	r.openSedReplace()

	r.sedFindField.SetText("hello")
	r.sedReplaceField.SetText("goodbye")

	r.runSedPreview()

	if r.activePage != sedPreviewPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, sedPreviewPage)
	}
	if len(r.sedPendingChanges) != 1 || r.sedPendingChanges[0].Path != file {
		t.Fatalf("sedPendingChanges = %+v, want exactly one change for %s", r.sedPendingChanges, file)
	}
	if got := string(r.sedPendingChanges[0].After); got != "goodbye world\n" {
		t.Errorf("computed After = %q, want %q", got, "goodbye world\n")
	}
	// File itself must still be untouched - Preview never writes.
	data, _ := os.ReadFile(file)
	if string(data) != "hello world\n" {
		t.Errorf("Preview modified the file on disk: %q", data)
	}

	preview := r.sedPreviewView.GetText(true)
	if !strings.Contains(preview, "hello world") || !strings.Contains(preview, "goodbye world") {
		t.Errorf("preview text = %q, want it to show both the before and after line", preview)
	}
}

func TestRunSedPreviewAdvancedFieldOverridesGuidedFields(t *testing.T) {
	r, _, _ := newTestRootWithSedFile(t, "hello world\n")
	r.openSedReplace()

	// Guided fields would produce "goodbye world" - the advanced script
	// must win instead and produce something else entirely.
	r.sedFindField.SetText("hello")
	r.sedReplaceField.SetText("goodbye")
	r.sedAdvancedField.SetText("s/world/there/")

	r.runSedPreview()

	if len(r.sedPendingChanges) != 1 {
		t.Fatalf("sedPendingChanges = %+v, want exactly one change", r.sedPendingChanges)
	}
	if got := string(r.sedPendingChanges[0].After); got != "hello there\n" {
		t.Errorf("After = %q, want %q (advanced script should override Find/Replace)", got, "hello there\n")
	}
}

func TestRunSedPreviewNoChangeShowsNoFilesWouldChange(t *testing.T) {
	r, _, _ := newTestRootWithSedFile(t, "hello world\n")
	r.openSedReplace()

	r.sedFindField.SetText("not-present-anywhere")
	r.sedReplaceField.SetText("x")

	r.runSedPreview()

	if len(r.sedPendingChanges) != 0 {
		t.Errorf("sedPendingChanges = %+v, want none", r.sedPendingChanges)
	}
	if !strings.Contains(r.sedPreviewView.GetText(true), "No files would change") {
		t.Errorf("preview text = %q, want the no-op message", r.sedPreviewView.GetText(true))
	}
}

func TestConfirmApplySedCancelPreselectedDoesNotWrite(t *testing.T) {
	r, _, file := newTestRootWithSedFile(t, "hello world\n")
	r.openSedReplace()
	r.sedFindField.SetText("hello")
	r.sedReplaceField.SetText("goodbye")
	r.runSedPreview()

	r.confirmApplySed()
	if r.activePage != purgeConfirmPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, purgeConfirmPage)
	}
	if got := r.purgeConfirm.GetCurrentItem(); got != 1 {
		t.Fatalf("preselected item = %d, want 1 (Cancel)", got)
	}
	r.cancelPurge()

	data, _ := os.ReadFile(file)
	if string(data) != "hello world\n" {
		t.Errorf("file was modified despite Cancel being preselected: %q", data)
	}
}

func TestConfirmApplySedConfirmedWritesChanges(t *testing.T) {
	r, _, file := newTestRootWithSedFile(t, "hello world\n")
	r.openSedReplace()
	r.sedFindField.SetText("hello")
	r.sedReplaceField.SetText("goodbye")
	r.runSedPreview()

	r.confirmApplySed()
	r.purgeConfirm.SetCurrentItem(2) // "Yes, delete permanently" - see newPurgeConfirm
	r.confirmPurge()

	data, err := os.ReadFile(file)
	if err != nil || string(data) != "goodbye world\n" {
		t.Fatalf("file content = %q, %v, want %q", data, err, "goodbye world\n")
	}
	if len(r.sedPendingChanges) != 0 {
		t.Error("sedPendingChanges should be cleared after applying")
	}
	if r.activePage == sedPreviewPage || r.activePage == purgeConfirmPage {
		t.Errorf("activePage = %q, want the dialog closed", r.activePage)
	}
}

func TestConfirmApplySedWithBackupKeepsOriginal(t *testing.T) {
	r, _, file := newTestRootWithSedFile(t, "hello world\n")
	r.openSedReplace()
	r.sedFindField.SetText("hello")
	r.sedReplaceField.SetText("goodbye")
	sedCheck(t, r, sedLabelBackup, true)
	r.runSedPreview()

	r.confirmApplySed()
	r.purgeConfirm.SetCurrentItem(2)
	r.confirmPurge()

	backup, err := os.ReadFile(file + ".bak")
	if err != nil || string(backup) != "hello world\n" {
		t.Fatalf(".bak content = %q, %v, want the original content", backup, err)
	}
}

func TestBackToSedFormPreservesFieldValues(t *testing.T) {
	r, _, _ := newTestRootWithSedFile(t, "hello world\n")
	r.openSedReplace()
	r.sedFindField.SetText("hello")
	r.sedReplaceField.SetText("goodbye")
	r.runSedPreview()

	r.backToSedForm()

	if r.activePage != sedReplacePage {
		t.Fatalf("activePage = %q, want %q", r.activePage, sedReplacePage)
	}
	if got := r.sedFindField.GetText(); got != "hello" {
		t.Errorf("Find field = %q after Back, want it preserved as %q", got, "hello")
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
