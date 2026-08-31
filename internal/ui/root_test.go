package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestContextMenuStructure pins the menu's grouping: Properties/Edit/
// Look/Tail -f/Rename, then a "Selection" section, a "Commands" section,
// a "Delete" section, and a "Globals" section, in that order — the shape
// Root.NewRoot builds it in.
func TestContextMenuStructure(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	want := []string{
		"Properties", "Edit", "Look", "Tail -f", "Rename",
		menuSectionLabel("Selection"),
		"Select all", "Deselect all", "Select +", "Select -",
		menuSectionLabel("Commands"),
		"Copy", "Cut", "Paste", "chown", "chmod", "Sed Replace",
		menuSectionLabel("Delete"),
		"Move to Trash", "Remove", "Go to Trash", "Restore from Trash", "Empty Trash",
		menuSectionLabel("Globals"),
		"Hide hidden files",      // dotfiles are shown by default now
		"Show size in bytes",     // human-readable is the default
		"Show time as timestamp", // formatted is the default
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

// TestContextMenuEditRunsEditCurrentEntry pins the new "Edit" menu item
// (see NewRoot): it's wired to editCurrentEntry, the same action the
// bottom bar's own Edit button/Ctrl+E already runs — see
// editCurrentEntry's own doc comment for why reading
// Panel.CurrentRowPath there already targets whichever row the context
// menu was opened for, without this item needing r.target itself.
// app.Suspend is a no-op here (no real screen behind r.app — see
// runEditor's own doc comment), so this only pins that selecting the
// item reaches editCurrentEntry/runEditor and the panel reloads
// cleanly afterwards, not that an editor actually ran.
func TestContextMenuEditRunsEditCurrentEntry(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1) // off ".." (the table's default initial selection) onto a real entry
	r.target = filepath.Join(dir, "apple.txt")
	r.showMenu(0, 0) // open the context menu the way a real right-click would

	editIdx := -1
	for i := 0; i < r.menu.GetItemCount(); i++ {
		if main, _ := r.menu.GetItemText(i); main == "Edit" {
			editIdx = i
			break
		}
	}
	if editIdx < 0 {
		t.Fatal(`no "Edit" item found in the context menu`)
	}

	r.menu.SetCurrentItem(editIdx)
	r.menu.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activePage == errorPage {
		t.Errorf("selecting Edit should not report an error here, got: %q", r.errorView.GetText(true))
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

	if main, _ := r.menu.GetItemText(r.mtimeFormatToggleIdx); main != "Show time as timestamp" {
		t.Fatalf("setup: mtime-format label = %q, want %q", main, "Show time as timestamp")
	}

	r.toggleMtimeUnix()

	if !r.panel.mtimeUnix {
		t.Error("mtimeUnix should be true after toggling once")
	}
	if main, _ := r.menu.GetItemText(r.mtimeFormatToggleIdx); main != "Show time formatted" {
		t.Errorf("mtime-format label = %q, want %q", main, "Show time formatted")
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
// TestChownTextFallbackNoopToOwnUser exercises openChown's own pre-picker
// behavior directly — still reachable as the picker's fallback when
// fsops.ListUsers/ListGroups isn't available (e.g. macOS). See
// TestOpenChownPickerNoopToOwnUser for the picker-based primary path,
// which openChown itself now tries first wherever it's available (this
// test's own environment included).
func TestChownTextFallbackNoopToOwnUser(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path

	r.openChownTextFallback(path)
	r.prompt.SetText(fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()))
	r.finishPrompt(tcell.KeyEnter)

	if r.activePage == errorPage {
		t.Errorf("chown to own uid/gid should succeed, got error overlay: %q", r.errorView.GetText(true))
	}
}

// TestOpenChownPickerNoopToOwnUser exercises openChown's actual current
// behavior: the owner picker, then the group picker, both confirmed via
// Enter on their pre-centered (current-user/current-group) selection —
// a no-op chown, so this doesn't need root to pass.
func TestOpenChownPickerNoopToOwnUser(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path

	r.openChown()
	if r.activePage != pickerPage {
		t.Skip("fsops.ListUsers unavailable in this environment (e.g. macOS): falls back to the text prompt instead")
	}

	// Owner step: confirm the pre-selected (current) user.
	r.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
	if r.activePage != pickerPage {
		t.Fatalf("activePage = %q after the owner step, want the group picker still open", r.activePage)
	}

	// Group step: confirm the pre-selected (current) group.
	r.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activePage == errorPage {
		t.Errorf("chown to own uid/gid should succeed, got error overlay: %q", r.errorView.GetText(true))
	}
}

// TestOpenChownPickerGroupCancelAppliesOwnerOnly pins that backing out of
// just the group step (Escape) still applies the already-picked owner,
// leaving the group untouched — the same flexibility chown(1)'s own
// "owner[:group]" syntax has always had.
func TestOpenChownPickerGroupCancelAppliesOwnerOnly(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.target = path

	r.openChown()
	if r.activePage != pickerPage {
		t.Skip("fsops.ListUsers unavailable in this environment (e.g. macOS): falls back to the text prompt instead")
	}

	// Owner step: confirm the pre-selected (current) user.
	r.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
	// Group step: back out instead of confirming.
	r.picker.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activePage == errorPage {
		t.Errorf("owner-only chown to the current user should succeed, got error overlay: %q", r.errorView.GetText(true))
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

// simulationScreen returns a real tcell.SimulationScreen sized width x
// height, initialized and ready for handleBeforeDraw — the same
// approach clickButtonBar (see bottombar_test.go) already uses to give
// mouse-position tests a genuinely drawn screen to work against.
func simulationScreen(t *testing.T, width, height int) tcell.SimulationScreen {
	t.Helper()
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(width, height)
	return screen
}

// TestHandleBeforeDrawRepositionsDetailsSidebarOnResize pins the user's
// own explicit report: the Details sidebar previously stayed at its old
// size/position across a live terminal resize, clashing with the panel
// underneath (which does resize correctly, being the one page in Root
// added with AddPage's own resize=true).
func TestHandleBeforeDrawRepositionsDetailsSidebarOnResize(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 30)
	r.showDetailsSidebar()

	x, _, width, _ := r.detailsSidebar.GetRect()
	if x+width != 100 {
		t.Fatalf("setup: sidebar not flush against the right edge of a 100-wide screen: x=%d width=%d", x, width)
	}

	// Simulates what Application.draw's own "fullscreen" handling
	// already does to Root's own rect before handleBeforeDraw ever runs
	// for real (see its own doc comment) — done by hand here since this
	// test calls handleBeforeDraw directly, without a running
	// Application behind it.
	r.SetRect(0, 0, 160, 40)
	screen := simulationScreen(t, 160, 40)

	r.handleBeforeDraw(screen)

	x, _, width, _ = r.detailsSidebar.GetRect()
	if x+width != 160 {
		t.Errorf("sidebar after resize: x=%d width=%d, want flush against the new 160-wide screen", x, width)
	}
}

// TestHandleBeforeDrawRerendersPropertiesOnResize pins the same fix for
// Properties: once the panel's own inner rect has actually shrunk
// (simulated directly here — see handleBeforeDraw's own doc comment on
// why that specifically, unlike the screen size itself, still catches
// up one draw behind a live resize, via tview's own Pages resize=true
// handling rather than anything this function does), a draw must
// re-clamp Properties to fit it, not leave it sitting at whatever size
// fit its content when it was last rendered.
func TestHandleBeforeDrawRerendersPropertiesOnResize(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 200, 50)
	r.panel.SetRect(0, 0, 200, 46) // realistic — see clampToPanel's own default-rect gotcha noted elsewhere
	r.target = path
	r.openProperties()

	_, _, wideWidth, _ := r.properties.GetRect()

	r.SetRect(0, 0, 40, 50)
	r.panel.SetRect(0, 0, 40, 46) // as if a real draw had already cascaded the resize down to it
	screen := simulationScreen(t, 40, 50)

	r.handleBeforeDraw(screen)

	_, _, narrowWidth, _ := r.properties.GetRect()
	if narrowWidth >= wideWidth {
		t.Errorf("properties width after shrinking the panel = %d, want less than the original %d (clamped to the now-narrower panel)", narrowWidth, wideWidth)
	}
	if narrowWidth > 40 {
		t.Errorf("properties width = %d, want at most the new screen width 40", narrowWidth)
	}
}

// TestHandleBeforeDrawNoopWhenScreenSizeUnchanged pins the guard that
// makes this cheap to call on every single draw: a call that doesn't
// actually follow a resize must not touch anything, verified here by
// deliberately leaving the sidebar's own rect wrong and confirming a
// same-size call doesn't so much as notice.
func TestHandleBeforeDrawNoopWhenScreenSizeUnchanged(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 30)
	screen := simulationScreen(t, 100, 30)
	r.handleBeforeDraw(screen) // establishes lastScreenWidth/Height at 100x30

	r.showDetailsSidebar()
	r.detailsSidebar.SetRect(0, 0, 5, 5) // deliberately wrong, so a real reposition would be obvious

	r.handleBeforeDraw(screen) // same 100x30 screen again

	x, y, width, height := r.detailsSidebar.GetRect()
	if x != 0 || y != 0 || width != 5 || height != 5 {
		t.Errorf("rect = (%d,%d,%d,%d), want left untouched (0,0,5,5) — the screen size never actually changed", x, y, width, height)
	}
}
