package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// TestToggleMouseReportingFlipsState pins the "om" chord's own action
// (see toggleMouseReporting) — a real user report that a mouse-aware
// terminal app with no way to turn that off breaks the terminal's own
// native text selection/copy, and no easy-to-remember way back.
func TestToggleMouseReportingFlipsState(t *testing.T) {
	isolateUserConfigFile(t)
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	if !r.mouseEnabled {
		t.Fatal("setup: mouse should start enabled, matching config.DefaultSettings")
	}

	r.toggleMouseReporting()
	if r.mouseEnabled {
		t.Error("the first press should disable mouse reporting")
	}
	if got := r.buildStatusBar(); !strings.Contains(got, "Mouse off") {
		t.Errorf("status bar = %q, want it to contain %q", got, "Mouse off")
	}
	if r.settings.MouseEnabled {
		t.Error("the flip should have updated the stored setting too")
	}

	r.toggleMouseReporting()
	if !r.mouseEnabled {
		t.Error("a second press should re-enable mouse reporting")
	}
	if got := r.buildStatusBar(); !strings.Contains(got, "Mouse on") {
		t.Errorf("status bar = %q, want it to contain %q", got, "Mouse on")
	}
}

// TestMouseEnabledSurvivesIntoTheConfigFile mirrors
// TestSplitOrientationSurvivesIntoTheConfigFile (see split_test.go) for
// mouse_enabled — per the user's own explicit request that this survive
// a restart rather than always starting back at "on".
func TestMouseEnabledSurvivesIntoTheConfigFile(t *testing.T) {
	dir := t.TempDir()
	configPath := isolateUserConfigFile(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.toggleMouseReporting()

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading the config back: %v", err)
	}
	if got := string(data); !strings.Contains(got, "mouse_enabled = false") {
		t.Errorf("config = %q, want it to record mouse_enabled", got)
	}
}

// TestNewRootAppliesMouseEnabledFromSettings pins the other half: a
// loaded settings.MouseEnabled = false (see loadInitialSettings) is
// honored from the moment NewRoot returns, both in Root's own
// bookkeeping (mouseEnabled mirrors it — see its own doc comment) and in
// the real Application state cmd/breakthrough's own initial
// EnableMouse(true) would otherwise leave in force. isolateInitialSettings,
// not isolateUserConfigFile: loadInitialSettings reads via
// config.UserConfigFile directly, a separate override point from
// userConfigFilePath (see loadInitialSettings' own doc comment) — a
// config file written at the latter would never actually be read here.
func TestNewRootAppliesMouseEnabledFromSettings(t *testing.T) {
	settings := config.DefaultSettings()
	settings.MouseEnabled = false
	isolateInitialSettings(t, settings, config.LoadColorSchemes("", ""))

	r, err := NewRoot(tview.NewApplication(), t.TempDir())
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	if r.mouseEnabled {
		t.Error("mouseEnabled should be false, matching the loaded settings")
	}
	if got := r.buildStatusBar(); !strings.Contains(got, "Mouse off") {
		t.Errorf("status bar = %q, want it to contain %q", got, "Mouse off")
	}
}

// TestRequestQuitPreselectsCancel pins the user's own explicit request:
// "q"/Ctrl+Q is easy to reach in the middle of ordinary browsing, so
// the confirmation must not let a single Enter quit by itself — the
// selection has to start on "Cancel", the same "safety default" every
// other confirmation in this app already uses (see newConfirmDialog's
// own comment on the Remove/Empty Trash dialog).
func TestRequestQuitPreselectsCancel(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.RequestQuit()

	if r.activePage != quitConfirmPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, quitConfirmPage)
	}
	row := r.quitConfirm.GetCurrentItem()
	label, _ := r.quitConfirm.GetItemText(row)
	if label != "Cancel" {
		t.Errorf("preselected item = %q, want %q", label, "Cancel")
	}
}

// TestRequestQuitHasATitleBar pins the same fix
// TestOpenRemoveConfirmHasATitleBar does for its own dialog: quitConfirm
// used to be a bare List with no heading either.
func TestRequestQuitHasATitleBar(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.RequestQuit()

	if got, want := r.quitConfirmTitleBar.GetText(true), " Quit "; got != want {
		t.Errorf("quitConfirmTitleBar text = %q, want %q", got, want)
	}
	if _, _, w, h := r.quitConfirmLayout.GetRect(); w <= 0 || h <= 0 {
		t.Errorf("quitConfirmLayout rect = %dx%d, want a real, positioned size", w, h)
	}
}

// TestRequestQuitWhilePastingAsksToCancelTheCopyInstead pins the user's
// own explicit report: quitting must not be possible at all while a
// Paste is still running, out from under a copy already mid-write to
// disk — RequestQuit must not even open the ordinary quitConfirm in
// this state, since accepting it would tear the app (and the running
// copy) down immediately with no way back.
func TestRequestQuitWhilePastingAsksToCancelTheCopyInstead(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	newPasteTestJob(r, false, dir, 1)

	r.RequestQuit()

	if r.activePage == quitConfirmPage {
		t.Fatal("RequestQuit opened the ordinary quit prompt while a paste is still running")
	}
	if r.activePage != confirmPage {
		t.Fatalf("activePage = %q, want %q (the shared confirm dialog)", r.activePage, confirmPage)
	}
	if got := r.confirmDialog.GetCurrentItem(); got != 0 {
		t.Errorf("preselected item = %d, want 0 (Cancel) — a stray Enter must never cancel the copy and quit", got)
	}
	if r.pasteJob == nil {
		t.Error("merely asking should not have cancelled the running paste")
	}
}

// TestConfirmingQuitWhilePastingCancelsTheJobThenQuits pins the other
// half: actually accepting the question TestRequestQuitWhilePastingAsksToCancelTheCopyInstead
// opens must cancel the running paste (see cancelPasteJob's own doc
// comment: whatever file is already mid-write finishes exactly where it
// was headed, nothing is left half-written) before quitting — not quit
// first and leave the job dangling, and not quit without ever touching
// it either.
func TestConfirmingQuitWhilePastingCancelsTheJobThenQuits(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	newPasteTestJob(r, false, dir, 1)

	r.RequestQuit()
	r.confirmDialog.SetCurrentItem(1) // "Yes, cancel and quit"
	r.acceptConfirm()

	if r.pasteJob != nil {
		t.Error("confirming should have cancelled the running paste job")
	}
	// confirmQuit itself calls Application.Stop, which is a safe no-op
	// here (no screen was ever set — see tview's own application.go) —
	// nothing further to observe about the quit half beyond it not
	// panicking, which a failing t.Fatalf above would already have
	// caught if reached in a broken state.
}

// TestMouseStatusText pins the exact wording buildStatusBar's own
// "Mouse on/off" segment uses.
func TestMouseStatusText(t *testing.T) {
	if got := mouseStatusText(true); got != "Mouse on" {
		t.Errorf("mouseStatusText(true) = %q, want %q", got, "Mouse on")
	}
	if got := mouseStatusText(false); got != "Mouse off" {
		t.Errorf("mouseStatusText(false) = %q, want %q", got, "Mouse off")
	}
}

// TestContextMenuEditRunsEditCurrentEntry pins the "Edit" menu item (see
// contextMenuTree): it's wired to editCurrentEntry, the same action the
// bottom bar's own Edit button/"e" key already runs — see
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
	r.panel.focusRow(2) // off ".." onto apple.txt (row 1 is app-data/ — directories sort first, see fixtureDir)
	r.target = filepath.Join(dir, "apple.txt")
	r.targetRow = 2
	r.showMenu(0, 0) // open the context menu the way a real right-click would

	selectMenuItem(t, r, "Edit")

	if r.activePage == errorPage {
		t.Errorf("selecting Edit should not report an error here, got: %q", r.errorView.GetText(true))
	}
}

// TestUpdateOverlayTitleBarColorsTracksActiveOverlay pins the user's own
// explicit request that this active/inactive title-bar distinction apply
// to every panel with one, not just tool windows'/Details' own:
// whichever of Properties/the context menu is currently the topmost
// overlay gets FocusedBackground on its own title bar, the other (or
// both, if neither is open) gets EditableBackground — see
// updateOverlayTitleBarColors' own doc comment.
func TestUpdateOverlayTitleBarColorsTracksActiveOverlay(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	if got, want := r.propertiesTitleBar.GetBackgroundColor(), r.theme.EditableBackground; got != want {
		t.Errorf("propertiesTitleBar with nothing open = %v, want EditableBackground %v", got, want)
	}
	if got, want := r.menuTitleBar.GetBackgroundColor(), r.theme.EditableBackground; got != want {
		t.Errorf("menuTitleBar with nothing open = %v, want EditableBackground %v", got, want)
	}

	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()
	if got, want := r.propertiesTitleBar.GetBackgroundColor(), r.theme.FocusedBackground; got != want {
		t.Errorf("propertiesTitleBar while Properties is open = %v, want FocusedBackground %v", got, want)
	}
	if got, want := r.menuTitleBar.GetBackgroundColor(), r.theme.EditableBackground; got != want {
		t.Errorf("menuTitleBar while Properties (not the menu) is open = %v, want EditableBackground %v", got, want)
	}

	// showMenu (via showOverlay/closeAllOverlays) closes Properties first
	// — the same "only one overlay at a time" policy every other opener
	// already follows.
	r.showMenu(0, 0)
	if got, want := r.menuTitleBar.GetBackgroundColor(), r.theme.FocusedBackground; got != want {
		t.Errorf("menuTitleBar while the menu is open = %v, want FocusedBackground %v", got, want)
	}
	if got, want := r.propertiesTitleBar.GetBackgroundColor(), r.theme.EditableBackground; got != want {
		t.Errorf("propertiesTitleBar after Properties was closed = %v, want EditableBackground %v", got, want)
	}

	r.hideOverlay()
	if got, want := r.menuTitleBar.GetBackgroundColor(), r.theme.EditableBackground; got != want {
		t.Errorf("menuTitleBar after closing it = %v, want EditableBackground %v", got, want)
	}
}

// TestToggleHidden pins the "." key's own action — no menu label to
// check any more (see contextmenu.go's own package doc on why the
// "Globals" toggles were dropped from the menu entirely), just the
// state flip and its effect on the listing.
func TestToggleHidden(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	if !r.panel.showHidden {
		t.Fatal("setup: dotfiles should be shown by default")
	}

	r.toggleHidden()

	if r.panel.showHidden {
		t.Error("showHidden should be false after toggling once")
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
}

// TestToggleSizeBytes mirrors TestToggleHidden for the Size-format
// toggle (the "z" chord's own "s" member).
func TestToggleSizeBytes(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	if r.panel.sizeBytes {
		t.Fatal("setup: human-readable should be the default")
	}

	r.toggleSizeBytes()

	if !r.panel.sizeBytes {
		t.Error("sizeBytes should be true after toggling once")
	}

	r.toggleSizeBytes()
	if r.panel.sizeBytes {
		t.Error("sizeBytes should be false again after toggling twice")
	}
}

// TestToggleMtimeUnix mirrors TestToggleHidden for the Modified-format
// toggle (the "z" chord's own "t" member).
func TestToggleMtimeUnix(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	if r.panel.mtimeUnix {
		t.Fatal("setup: formatted should be the default")
	}

	r.toggleMtimeUnix()

	if !r.panel.mtimeUnix {
		t.Error("mtimeUnix should be true after toggling once")
	}

	r.toggleMtimeUnix()
	if r.panel.mtimeUnix {
		t.Error("mtimeUnix should be false again after toggling twice")
	}
}

// Copy/Cut/Paste's own round-trip tests (on-disk effect, clipboard
// clearing, Details refresh, and the whole paste-conflict-resolution
// feature) live in pasteconflict_test.go now, alongside the async
// engine (pasteconflict.go) they exercise.

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

// TestCopyToClipboardCountsFilesAndDirs pins clipboardCounts' own
// classification, exercised through the real Copy path — a directory
// symlink among the targets would count as a dir here too (see
// isDirish), the same as everywhere else in this app already treats
// one, though fixtureDir itself has no symlink to cover that with.
func TestCopyToClipboardCountsFilesAndDirs(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.panel.toggleCheckbox(1) // app-data (dir)
	r.panel.toggleCheckbox(2) // apple.txt (file)
	r.panel.toggleCheckbox(3) // apricot.txt (file)
	r.copyToClipboard()

	if r.clipboardDirs != 1 || r.clipboardFiles != 2 {
		t.Errorf("clipboardDirs/Files = %d/%d, want 1/2", r.clipboardDirs, r.clipboardFiles)
	}
}

// TestCopyToClipboardSyncsHighlightAcrossOpenTabs pins
// syncClipboardHighlight's own point: the clipboard is one Root-level
// value shared by every tab, so Copy from tab 1 must also tint the
// same path's row in tab 2, without switching to it first.
func TestCopyToClipboardSyncsHighlightAcrossOpenTabs(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.newTab(dir) // a second tab on the very same directory
	if len(r.tabs) != 2 {
		t.Fatalf("setup: want 2 tabs, got %d", len(r.tabs))
	}
	firstTab, secondTab := r.tabs[0], r.tabs[1]

	r.switchToTab(0)
	r.panel.toggleCheckbox(2) // apple.txt
	r.copyToClipboard()

	held := filepath.Join(dir, "apple.txt")
	for name, p := range map[string]*Panel{"first (triggering) tab": firstTab, "second tab": secondTab} {
		row, ok := rowForPath(p, held)
		if !ok {
			t.Fatalf("%s: apple.txt row not found", name)
		}
		if _, tinted := cellBackground(p.table.GetCell(row, colName)); !tinted {
			t.Errorf("%s: apple.txt not tinted after Copy on the other tab", name)
		}
	}
}

// TestReloadCurrentTabReReadsFromDisk pins the "z" chord's own "r"
// member ("Reload"): a file that shows up after the active tab already
// loaded its directory is visible once reloadCurrentTab runs — the
// header row's own "⭯" button does the same thing via
// Panel.runHeaderAction directly (see
// TestRunHeaderActionReloadReReadsFromDisk); this is Root's own
// keyboard-reachable path to it.
func TestReloadCurrentTabReReadsFromDisk(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	newFile := filepath.Join(dir, "just-landed.txt")
	if err := os.WriteFile(newFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := rowForPath(r.panel, newFile); ok {
		t.Fatal("setup: just-landed.txt shouldn't be visible before reload runs")
	}

	r.reloadCurrentTab()

	if _, ok := rowForPath(r.panel, newFile); !ok {
		t.Error("just-landed.txt still not visible after reloadCurrentTab")
	}
}

// Chmod's own dialog (openChmod) is tested in chmoddialog_test.go now —
// it no longer goes through r.prompt/finishPrompt at all (see
// chmoddialog.go's own doc comment on why it was rebuilt into a full
// dialog).

// TestRenameRowOpensRenameForGivenRow pins renameRow's own row-addressed
// shape (see Panel.onRenameGesture/handleNameClick): it targets
// whichever row it's given directly, the same as renameCurrentEntry
// already does for the panel's own cursor.
func TestRenameRowOpensRenameForGivenRow(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.renameRow(2) // apple.txt

	want := filepath.Join(dir, "apple.txt")
	if r.target != want {
		t.Errorf("target = %q, want %q", r.target, want)
	}
	if r.targetRow != 2 {
		t.Errorf("targetRow = %d, want 2", r.targetRow)
	}
	if r.activePage != renamePage {
		t.Errorf("activePage = %q, want %q", r.activePage, renamePage)
	}
}

// TestRenameRowNoopsForDotDot pins that the rename gesture can't be
// used to rename ".." — the same exclusion CurrentRowPath already
// applies for the keyboard path ("r"/renameCurrentEntry).
func TestRenameRowNoopsForDotDot(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.renameRow(0) // ".."

	if r.activePage != "" {
		t.Errorf("activePage = %q, want still closed", r.activePage)
	}
}

// TestFinishRenameRefreshesDetailsShowingSameFile pins the user's own
// explicit request: committing a rename ("r", or the click-pause-click
// gesture) immediately updates Details too, if it's showing that same
// file — following it to its own new path, the same fix
// savePropertiesEdit already needed for a rename via Properties (see
// refreshDetailsIfShowing's own doc comment).
func TestFinishRenameRefreshesDetailsShowingSameFile(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	path := filepath.Join(dir, "apple.txt")
	r.panel.focusRow(2) // apple.txt
	r.showDetailsSidebar()
	if r.detailsTarget != path {
		t.Fatalf("setup: detailsTarget = %q, want %q", r.detailsTarget, path)
	}

	r.renameRow(2)
	r.rename.SetText("renamed.txt")
	r.finishRename(tcell.KeyEnter)

	want := filepath.Join(dir, "renamed.txt")
	if r.detailsTarget != want {
		t.Errorf("detailsTarget after rename = %q, want %q", r.detailsTarget, want)
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

// TestApplyChownRefreshesDetailsShowingSameFile pins the user's own
// explicit request extended to the standalone chown action: applying it
// immediately updates Details too, if it's showing that same file (see
// refreshDetailsIfShowing's own doc comment).
func TestApplyChownRefreshesDetailsShowingSameFile(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.panel.focusRow(2) // apple.txt
	r.showDetailsSidebar()
	if r.detailsTarget != path {
		t.Fatalf("setup: detailsTarget = %q, want %q", r.detailsTarget, path)
	}
	// A same-uid/gid chown (see TestChownNoopToOwnUser in fsops for why
	// that's the only privilege-free option here) leaves detailsStat's
	// own content unchanged either way, so a value comparison can't tell
	// "reloaded" apart from "never touched" — detailsMetadataState,
	// unconditionally reset by loadDetailsTarget (see its own doc
	// comment) regardless of what the fresh stat comes back as, can.
	r.detailsMetadataState = "stale-marker"

	r.applyChown(path, os.Getuid(), os.Getgid())

	if r.detailsTarget != path {
		t.Fatalf("detailsTarget changed unexpectedly to %q", r.detailsTarget)
	}
	if r.detailsMetadataState != "" {
		t.Error("detailsMetadataState should have been reset by loadDetailsTarget — Details wasn't actually reloaded")
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

	// detailsSidebarLayout, not detailsSidebar itself — see
	// TestInfoSidebarSizeIsAtLeastOneThirdWidthAndFullHeight's own doc
	// comment on why (detailssidebar_test.go).
	x, _, width, _ := r.detailsSidebarLayout.GetRect()
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

	x, _, width, _ = r.detailsSidebarLayout.GetRect()
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
