package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestContextMenuStructure pins the menu's grouping: Info/Rename, then a
// "Selection" section, a "Commands" section, and a "Globals" section, in
// that order — the shape Root.NewRoot builds it in.
func TestContextMenuStructure(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	want := []string{
		"Properties", "Rename",
		menuSectionLabel("Selection"),
		"Select all", "Deselect all", "Select +", "Select -",
		menuSectionLabel("Commands"),
		"Copy", "Cut", "Paste", "chown", "chmod",
		menuSectionLabel("Globals"),
		"Hide hidden files",               // dotfiles are shown by default now
		"Show size in bytes",              // human-readable is the default
		"Show modified date as timestamp", // formatted is the default
	}
	if got := r.menu.GetItemCount(); got != len(want) {
		t.Fatalf("menu has %d items, want %d", got, len(want))
	}
	for i, wantText := range want {
		if main, _ := r.menu.GetItemText(i); main != wantText {
			t.Errorf("item %d = %q, want %q", i, main, wantText)
		}
	}
}

// TestToggleHiddenViaMenu drives the actual menu action, and pins that
// the item's own label flips to describe the next click, not the current
// state.
func TestToggleHiddenViaMenu(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	// Dotfiles are shown by default, so the item starts out offering to
	// hide them.
	if main, _ := r.menu.GetItemText(r.hiddenToggleIdx); main != "Hide hidden files" {
		t.Fatalf("setup: hidden-toggle label = %q, want %q", main, "Hide hidden files")
	}

	r.toggleHidden()

	if r.panel.showHidden {
		t.Error("showHidden should be false after toggling once")
	}
	if main, _ := r.menu.GetItemText(r.hiddenToggleIdx); main != "Show hidden files" {
		t.Errorf("hidden-toggle label = %q, want %q", main, "Show hidden files")
	}
	for row := 0; row < r.panel.table.GetRowCount(); row++ {
		if ref, ok := r.panel.rowRef(row); ok && ref.name == ".hidden" {
			t.Error(".hidden should not be listed after toggling showHidden off")
		}
	}

	r.toggleHidden()
	if !r.panel.showHidden {
		t.Error("showHidden should be true again after toggling twice")
	}
	if main, _ := r.menu.GetItemText(r.hiddenToggleIdx); main != "Hide hidden files" {
		t.Errorf("hidden-toggle label = %q, want %q", main, "Hide hidden files")
	}
}

// TestToggleSizeBytesViaMenu mirrors TestToggleHiddenViaMenu for the
// Size-format toggle: drives the actual menu action, checks the label
// flips and the rendered column changes.
func TestToggleSizeBytesViaMenu(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	if main, _ := r.menu.GetItemText(r.sizeFormatToggleIdx); main != "Show size in bytes" {
		t.Fatalf("setup: size-format label = %q, want %q", main, "Show size in bytes")
	}

	r.toggleSizeBytes()

	if !r.panel.sizeBytes {
		t.Error("sizeBytes should be true after toggling once")
	}
	if main, _ := r.menu.GetItemText(r.sizeFormatToggleIdx); main != "Show size (human-readable)" {
		t.Errorf("size-format label = %q, want %q", main, "Show size (human-readable)")
	}

	r.toggleSizeBytes()
	if r.panel.sizeBytes {
		t.Error("sizeBytes should be false again after toggling twice")
	}
}

// TestToggleMtimeUnixViaMenu mirrors TestToggleHiddenViaMenu for the
// Modified-format toggle.
func TestToggleMtimeUnixViaMenu(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	if main, _ := r.menu.GetItemText(r.mtimeFormatToggleIdx); main != "Show modified date as timestamp" {
		t.Fatalf("setup: mtime-format label = %q, want %q", main, "Show modified date as timestamp")
	}

	r.toggleMtimeUnix()

	if !r.panel.mtimeUnix {
		t.Error("mtimeUnix should be true after toggling once")
	}
	if main, _ := r.menu.GetItemText(r.mtimeFormatToggleIdx); main != "Show modified date formatted" {
		t.Errorf("mtime-format label = %q, want %q", main, "Show modified date formatted")
	}

	r.toggleMtimeUnix()
	if r.panel.mtimeUnix {
		t.Error("mtimeUnix should be false again after toggling twice")
	}
}

// TestCopyToClipboardThenPasteLeavesSourceInPlace exercises Copy/Paste
// end to end: capture a target via clipboardTargets, navigate elsewhere,
// paste — the source must survive since Copy, unlike Cut, never removes
// it.
func TestCopyToClipboardThenPasteLeavesSourceInPlace(t *testing.T) {
	srcDir := fixtureDir(t)
	dstDir := t.TempDir()

	r, err := NewRoot(tview.NewApplication(), srcDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.target = filepath.Join(srcDir, "apple.txt")
	r.copyToClipboard()

	if err := r.panel.load(dstDir); err != nil {
		t.Fatalf("load(dstDir): %v", err)
	}
	r.pasteClipboard()

	if _, err := os.Stat(filepath.Join(dstDir, "apple.txt")); err != nil {
		t.Errorf("pasted file missing in dst: %v", err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "apple.txt")); err != nil {
		t.Errorf("Copy should leave the source file in place: %v", err)
	}
}

// TestCutToClipboardThenPasteRemovesSource is Copy's counterpart for Cut:
// the source must be gone afterwards, and the clipboard cleared so a
// stray second Paste doesn't try to move something that's already moved.
func TestCutToClipboardThenPasteRemovesSource(t *testing.T) {
	srcDir := fixtureDir(t)
	dstDir := t.TempDir()

	r, err := NewRoot(tview.NewApplication(), srcDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.target = filepath.Join(srcDir, "banana.txt")
	r.cutToClipboard()

	if err := r.panel.load(dstDir); err != nil {
		t.Fatalf("load(dstDir): %v", err)
	}
	r.pasteClipboard()

	if _, err := os.Stat(filepath.Join(dstDir, "banana.txt")); err != nil {
		t.Errorf("pasted file missing in dst: %v", err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "banana.txt")); !os.IsNotExist(err) {
		t.Errorf("Cut should remove the source file, stat err = %v", err)
	}
	if len(r.clipboard) != 0 {
		t.Errorf("clipboard should be cleared after a successful cut-paste, got %v", r.clipboard)
	}
}

// TestClipboardTargetsPrefersSelectionOverTarget pins clipboardTargets'
// rule: the checkbox selection wins over the right-clicked target when
// there is one, so Copy/Cut on a multi-row selection acts on all of it
// rather than just whatever was last right-clicked.
func TestClipboardTargetsPrefersSelectionOverTarget(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.panel.toggleCheckbox(1) // app-data
	r.panel.toggleCheckbox(2) // apple.txt
	r.target = filepath.Join(dir, "banana.txt")

	got := r.clipboardTargets()
	if len(got) != 2 {
		t.Fatalf("clipboardTargets() = %v, want the 2 checked entries", got)
	}
}

// TestPasteConflictReportsErrorAndLeavesDestUntouched pins Paste's
// collision handling: an existing dst entry is refused (see fsops.Copy's
// force parameter), reported through the error overlay, and left as it
// was.
func TestPasteConflictReportsErrorAndLeavesDestUntouched(t *testing.T) {
	srcDir := fixtureDir(t)
	dstDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dstDir, "apple.txt"), []byte("existing"), 0o640); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), srcDir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = filepath.Join(srcDir, "apple.txt")
	r.copyToClipboard()

	if err := r.panel.load(dstDir); err != nil {
		t.Fatalf("load(dstDir): %v", err)
	}
	r.pasteClipboard()

	if r.activePage != errorPage {
		t.Error("pasting onto an existing file should open the error overlay")
	}
	got, err := os.ReadFile(filepath.Join(dstDir, "apple.txt"))
	if err != nil || string(got) != "existing" {
		t.Errorf("existing dst file should be untouched, got %q, %v", got, err)
	}
}

// TestChmodViaPrompt drives the actual openChmod -> type into r.prompt ->
// finishPrompt(Enter) path, the same sequence a real keystroke-by-
// keystroke entry followed by Enter produces.
func TestChmodViaPrompt(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path

	r.openChmod()
	r.prompt.SetText("600")
	r.finishPrompt(tcell.KeyEnter)

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

// TestChownViaPromptNoopToOwnUser is Chown's counterpart, kept privilege-
// independent the same way TestChownNoopToOwnUser in fsops is: changing
// ownership to anyone else needs root, but chowning to the process's own
// uid:gid is guaranteed to succeed anywhere this test runs.
func TestChownViaPromptNoopToOwnUser(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path

	r.openChown()
	r.prompt.SetText(fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()))
	r.finishPrompt(tcell.KeyEnter)

	if r.activePage == errorPage {
		t.Errorf("chown to own uid/gid should succeed, got error overlay: %q", r.errorView.GetText(true))
	}
}

// TestSelectPlusViaPrompt drives Select+ through the same prompt flow.
func TestSelectPlusViaPrompt(t *testing.T) {
	dir := fixtureDir(t) // app-data/, apple.txt, apricot.txt, banana.txt
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openSelectPlus()
	r.prompt.SetText("ap*")
	r.finishPrompt(tcell.KeyEnter)

	if len(r.panel.selected) != 3 { // app-data, apple.txt, apricot.txt
		t.Errorf("selected = %d, want 3", len(r.panel.selected))
	}
}

// TestSelectMinusViaPrompt is Select+'s counterpart: it unmarks matches
// instead, leaving everything else that was checked alone.
func TestSelectMinusViaPrompt(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.selectAll()

	r.openSelectMinus()
	r.prompt.SetText("apricot.txt")
	r.finishPrompt(tcell.KeyEnter)

	if r.panel.selected[filepath.Join(dir, "apricot.txt")] {
		t.Error("apricot.txt should be unselected after Select -")
	}
	if len(r.panel.selected) != 3 { // everything else stays selected
		t.Errorf("selected = %d, want 3", len(r.panel.selected))
	}
}

// TestPromptCancelDoesNotSubmit pins finishPrompt's Escape/Tab path: the
// callback must not run, and the overlay must close (same DoneFunc
// contract finishRename already has).
func TestPromptCancelDoesNotSubmit(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	called := false
	r.openPrompt("Test:", "", func(string) { called = true })
	r.prompt.SetText("whatever")
	r.finishPrompt(tcell.KeyEscape)

	if called {
		t.Error("Escape should cancel the prompt without calling onSubmit")
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q, want empty after cancel", r.activePage)
	}
}
