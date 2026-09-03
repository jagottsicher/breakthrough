package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// selectRow moves the panel's own table cursor to row — the same
// cursor selectedOrCurrentPaths (what openChmod actually reads) falls
// back to once nothing is checkbox-selected, and the same one a real
// right-click already moves before the context menu (and so openChmod)
// ever runs (see editCurrentEntry's own doc comment on that same
// convention). Column doesn't matter for Select itself, but colName
// matches what a real row-activation already selects.
func selectRow(r *Root, row int) {
	r.panel.table.Select(row, colName)
}

// chmodDialogFixture builds a small tree for testing the dialog's own
// recursion options, mirroring fsops' own chmodRecursiveFixture (see
// internal/fsops/chmod_test.go) one level up, through the dialog
// itself rather than the underlying fsops functions directly (already
// covered there):
//
//	<root>/target/
//	<root>/target/f.txt
//	<root>/target/sub/
//	<root>/target/sub/g.txt
//
// root is what NewRoot itself opens on — target is nested one level
// inside it, rather than being root itself, so it shows up as an
// ordinary, selectable row (row 1 — the only entry — see selectRow) in
// its own listing; a directory never appears as a row of its own
// listing.
func chmodDialogFixture(t *testing.T) (root, target, topFile, subDir, subFile string) {
	t.Helper()
	root = t.TempDir()
	target = filepath.Join(root, "target")
	topFile = filepath.Join(target, "f.txt")
	subDir = filepath.Join(target, "sub")
	subFile = filepath.Join(subDir, "g.txt")

	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(topFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, target, topFile, subDir, subFile
}

func chmodPerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

// TestOpenChmodPrefillsFromFirstTarget pins that Permissions starts at
// the target's own real current mode — unlike the old prompt, which
// always started blank (see openChmod's own doc comment).
func TestOpenChmodPrefillsFromFirstTarget(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 2) // apple.txt
	r.openChmod()

	if r.stagedChmodMode != 0o640 {
		t.Errorf("stagedChmodMode = %o, want 0640 (the file's own current mode)", r.stagedChmodMode)
	}
}

// TestOpenChmodDefaultsFilesModeFromMainMode pins the traditional
// dir/file relationship default (755/644, ...): Files' own starting
// value is Permissions' own starting value with every execute bit
// cleared — exactly the relationship the user's own example (755
// dirs / 644 files) assumes.
func TestOpenChmodDefaultsFilesModeFromMainMode(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "app-data")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 1) // app-data
	r.openChmod()

	if r.stagedChmodFilesMode != 0o644 {
		t.Errorf("stagedChmodFilesMode = %o, want 0644 (0755 with every execute bit cleared)", r.stagedChmodFilesMode)
	}
}

// TestChmodDialogHidesRecursiveSectionForPlainFiles pins that a
// selection made up entirely of plain files shows neither the
// recursive-folders toggle nor the Files section at all — there's
// nothing for either to apply to (see chmodAnyDir/chmodFieldOrder).
func TestChmodDialogHidesRecursiveSectionForPlainFiles(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 2) // apple.txt
	r.openChmod()

	if r.chmodAnyDir {
		t.Error("chmodAnyDir should be false for a plain-file-only selection")
	}
	for _, f := range []chmodField{chmodFieldRecursiveDirs, chmodFieldFilesMode, chmodFieldFilesEnable} {
		if _, ok := r.chmodSpanForField(f); ok {
			t.Errorf("field %v should not be rendered for a plain-file-only selection", f)
		}
	}
}

// TestChmodDialogShowsRecursiveSectionForDirectory is the mirror image:
// once any target is a directory, Permissions'/Directory's own field
// plus the folders toggle and the Files-enable checkbox all show right
// away. Files' own individual bits/octal value are a further step:
// they only become field stops once stagedChmodFilesEnabled is actually
// checked (see chmodFieldOrder's own doc comment) — this pins both
// halves, before and after checking it.
func TestChmodDialogShowsRecursiveSectionForDirectory(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 1) // app-data
	r.openChmod()

	if !r.chmodAnyDir {
		t.Fatal("chmodAnyDir should be true for a directory target")
	}
	for _, f := range []chmodField{chmodFieldMode, chmodFieldRecursiveDirs, chmodFieldFilesEnable} {
		if _, ok := r.chmodSpanForField(f); !ok {
			t.Errorf("field %v should be rendered for a directory target", f)
		}
	}
	if _, ok := r.chmodSpanForField(chmodFieldFilesMode); ok {
		t.Error("chmodFieldFilesMode should not have its own field yet — Files isn't enabled")
	}

	r.stagedChmodFilesEnabled = true
	r.rerenderChmodDialog()
	if _, ok := r.chmodSpanForField(chmodFieldFilesMode); !ok {
		t.Error("chmodFieldFilesMode should have its own field once Files is enabled")
	}
}

// TestToggleChmodTogglesAreIndependent pins that clicking the
// recursive-folders toggle and the Files-enable toggle each flip only
// their own state.
func TestToggleChmodTogglesAreIndependent(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 1) // app-data
	r.openChmod()

	if r.stagedChmodRecursiveDirs || r.stagedChmodFilesEnabled {
		t.Fatal("setup: both should start off")
	}

	dirsSpan, ok := r.chmodSpanForField(chmodFieldRecursiveDirs)
	if !ok {
		t.Fatal("no chmodFieldRecursiveDirs span found")
	}
	r.activateChmodField(dirsSpan)
	if !r.stagedChmodRecursiveDirs {
		t.Error("clicking the folders toggle should turn it on")
	}
	if r.stagedChmodFilesEnabled {
		t.Error("clicking the folders toggle should not affect the Files toggle")
	}

	filesSpan, ok := r.chmodSpanForField(chmodFieldFilesEnable)
	if !ok {
		t.Fatal("no chmodFieldFilesEnable span found")
	}
	r.activateChmodField(filesSpan)
	if !r.stagedChmodFilesEnabled {
		t.Error("clicking the Files toggle should turn it on")
	}
	if !r.stagedChmodRecursiveDirs {
		t.Error("clicking the Files toggle should not turn the folders toggle back off")
	}
}

// TestChmodBitClickTogglesMode pins the user's own explicit request:
// Permissions gets the same individual rwx bits Properties' own
// permissionsField already has, not just the octal field alone —
// clicking one flips exactly that bit in stagedChmodMode.
func TestChmodBitClickTogglesMode(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 2) // apple.txt
	r.openChmod()

	span, ok := r.chmodSpanForField(chmodFieldModeOwnerRead)
	if !ok {
		t.Fatal("no chmodFieldModeOwnerRead span found")
	}
	r.activateChmodField(span)
	if r.stagedChmodMode != 0o400 {
		t.Errorf("stagedChmodMode = %o, want 0400 after clicking owner-read", r.stagedChmodMode)
	}

	r.activateChmodField(span) // click again: toggles back off
	if r.stagedChmodMode != 0 {
		t.Errorf("stagedChmodMode = %o, want 0 after clicking owner-read again", r.stagedChmodMode)
	}
}

// TestChmodBitKeyboardShortcuts mirrors properties_test.go's own
// TestPermBitKeyboardShortcuts exactly, for Permissions' bits here
// instead of Properties' — the matching letter sets a bit on, a
// non-matching letter does nothing, and Delete clears it, per the
// user's own explicit request that permission entry work identically
// in both dialogs.
func TestChmodBitKeyboardShortcuts(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 2) // apple.txt
	r.openChmod()

	idx, _ := r.chmodFieldIndex(chmodFieldModeOwnerRead)
	r.setChmodFocus(idx)

	// The matching letter ('r' for a read bit) sets it on.
	if got := r.captureChmodKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone)); got != nil {
		t.Error("the matching letter should be consumed")
	}
	if r.stagedChmodMode != 0o400 {
		t.Errorf("stagedChmodMode = %o, want 0400 after 'r'", r.stagedChmodMode)
	}

	// A non-matching letter ('w' on a read bit) does nothing.
	if got := r.captureChmodKey(tcell.NewEventKey(tcell.KeyRune, 'w', tcell.ModNone)); got == nil {
		t.Error("a non-matching letter should not be consumed")
	}
	if r.stagedChmodMode != 0o400 {
		t.Errorf("stagedChmodMode = %o, want unchanged 0400 after a non-matching letter", r.stagedChmodMode)
	}

	// Delete clears it.
	r.captureChmodKey(tcell.NewEventKey(tcell.KeyDelete, 0, tcell.ModNone))
	if r.stagedChmodMode != 0 {
		t.Errorf("stagedChmodMode = %o, want 0 after Delete", r.stagedChmodMode)
	}

	// '-' also clears it (a no-op here, it's already off, but pins the key works).
	r.setChmodBit(chmodFieldModeOwnerRead, true) // back on, so '-' has something to clear
	r.captureChmodKey(tcell.NewEventKey(tcell.KeyRune, '-', tcell.ModNone))
	if r.stagedChmodMode != 0 {
		t.Errorf("stagedChmodMode = %o, want 0 after '-'", r.stagedChmodMode)
	}

	// Space toggles, same as Enter.
	r.captureChmodKey(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone))
	if r.stagedChmodMode != 0o400 {
		t.Errorf("stagedChmodMode = %o, want 0400 after Space", r.stagedChmodMode)
	}
}

// TestChmodFilesBitKeyboardShortcuts is the same pin for Files' own 9
// bits — independent of Permissions', toggling stagedChmodFilesMode
// instead. Files must actually be enabled first: its own bits aren't
// field stops at all until then (see chmodFieldOrder's own doc
// comment).
func TestChmodFilesBitKeyboardShortcuts(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 1) // app-data — a directory, so the Files row exists
	r.openChmod()
	r.stagedChmodFilesEnabled = true
	r.stagedChmodFilesMode = 0

	idx, _ := r.chmodFieldIndex(chmodFieldFilesGroupWrite)
	r.setChmodFocus(idx)

	if got := r.captureChmodKey(tcell.NewEventKey(tcell.KeyRune, 'w', tcell.ModNone)); got != nil {
		t.Error("the matching letter should be consumed")
	}
	if r.stagedChmodFilesMode != 0o020 {
		t.Errorf("stagedChmodFilesMode = %o, want 020 after 'w'", r.stagedChmodFilesMode)
	}
	if r.stagedChmodMode == 0o020 {
		t.Error("setting a Files bit should not have touched stagedChmodMode")
	}

	r.captureChmodKey(tcell.NewEventKey(tcell.KeyDelete, 0, tcell.ModNone))
	if r.stagedChmodFilesMode != 0 {
		t.Errorf("stagedChmodFilesMode = %o, want 0 after Delete", r.stagedChmodFilesMode)
	}
}

// TestChmodBitsAndOctalStayInSync pins that the two input methods (the
// 9 individual rwx bits and the octal value) are always just two views
// of the exact same stagedChmodMode — clicking bits updates what the
// octal field itself renders as, with no separate copy to drift out of
// sync.
func TestChmodBitsAndOctalStayInSync(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 2) // apple.txt
	r.openChmod()

	for _, f := range []chmodField{chmodFieldModeOwnerRead, chmodFieldModeOwnerWrite, chmodFieldModeOwnerExec} {
		span, ok := r.chmodSpanForField(f)
		if !ok {
			t.Fatalf("no span found for %v", f)
		}
		r.activateChmodField(span)
	}

	if r.stagedChmodMode != 0o700 {
		t.Fatalf("stagedChmodMode = %o, want 0700 after setting owner rwx", r.stagedChmodMode)
	}
	if got := r.chmodText.GetText(true); !strings.Contains(got, "rwx------ (0700)") {
		t.Errorf("chmodText = %q, want it to contain %q", got, "rwx------ (0700)")
	}
}

// TestApplyChmodDialogPlainFile is the base case: no recursion options
// touched, one plain file target — behaves exactly like the old
// single-target prompt did.
func TestApplyChmodDialogPlainFile(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 2) // apple.txt
	r.openChmod()
	r.stagedChmodMode = 0o600

	r.applyChmodDialog()

	if got := chmodPerm(t, path); got != 0o600 {
		t.Errorf("mode = %o, want 0600", got)
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q after Apply, want closed", r.activePage)
	}
}

// TestApplyChmodDialogRefreshesDetailsShowingSameFile pins the user's
// own explicit request extended to the Chmod dialog: applying it
// immediately updates Details too, if it's showing that same target
// (see refreshDetailsIfShowing's own doc comment).
func TestApplyChmodDialogRefreshesDetailsShowingSameFile(t *testing.T) {
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
	r.detailsMetadataState = "stale-marker" // see TestApplyChownRefreshesDetailsShowingSameFile's own doc comment

	selectRow(r, 2) // apple.txt
	r.openChmod()
	r.stagedChmodMode = 0o600
	r.applyChmodDialog()

	if r.detailsTarget != path {
		t.Fatalf("detailsTarget changed unexpectedly to %q", r.detailsTarget)
	}
	if r.detailsMetadataState != "" {
		t.Error("detailsMetadataState should have been reset by loadDetailsTarget — Details wasn't actually reloaded")
	}
}

// TestApplyChmodDialogRecursiveDirsReachesSubfoldersNotFiles pins the
// user's own explicit request's first half: turning the folders toggle
// on applies Permissions' own value to every subfolder too, but never
// to a file, matching fsops.ChmodDirsRecursive exactly.
func TestApplyChmodDialogRecursiveDirsReachesSubfoldersNotFiles(t *testing.T) {
	root, target, topFile, subDir, subFile := chmodDialogFixture(t)

	r, err := NewRoot(tview.NewApplication(), root)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 1) // target/ — the only entry
	r.openChmod()
	r.stagedChmodMode = 0o700
	r.stagedChmodRecursiveDirs = true

	r.applyChmodDialog()

	if got := chmodPerm(t, target); got != 0o700 {
		t.Errorf("target mode = %o, want 0700", got)
	}
	if got := chmodPerm(t, subDir); got != 0o700 {
		t.Errorf("subDir mode = %o, want 0700", got)
	}
	if got := chmodPerm(t, topFile); got != 0o644 {
		t.Errorf("topFile mode = %o, want unchanged 0644", got)
	}
	if got := chmodPerm(t, subFile); got != 0o644 {
		t.Errorf("subFile mode = %o, want unchanged 0644", got)
	}
}

// TestApplyChmodDialogFilesEnabledReachesNestedFilesOnly is the user's
// own explicit request's second half: turning the Files toggle on
// applies Files' own separate value to every file inside, recursively,
// without touching the directories at all — matching
// fsops.ChmodFilesRecursive exactly, and independent of the folders
// toggle (left off here).
func TestApplyChmodDialogFilesEnabledReachesNestedFilesOnly(t *testing.T) {
	root, target, topFile, subDir, subFile := chmodDialogFixture(t)

	r, err := NewRoot(tview.NewApplication(), root)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 1) // target/
	r.openChmod()
	r.stagedChmodFilesEnabled = true
	r.stagedChmodFilesMode = 0o600

	subDirModeBefore := chmodPerm(t, subDir)

	r.applyChmodDialog()

	if got := chmodPerm(t, topFile); got != 0o600 {
		t.Errorf("topFile mode = %o, want 0600", got)
	}
	if got := chmodPerm(t, subFile); got != 0o600 {
		t.Errorf("subFile mode = %o, want 0600", got)
	}
	// target itself still gets Permissions' own value (a plain,
	// non-recursive Chmod — stagedChmodRecursiveDirs is off here), but
	// subDir, never walked by a non-recursive Chmod on target alone,
	// must be untouched.
	if got := chmodPerm(t, target); got != r.stagedChmodMode {
		t.Errorf("target mode = %o, want %o (Permissions' own value)", got, r.stagedChmodMode)
	}
	if got := chmodPerm(t, subDir); got != subDirModeBefore {
		t.Errorf("subDir mode = %o, want unchanged %o", got, subDirModeBefore)
	}
}

// TestApplyChmodDialogBothRecursiveOptionsCombine pins the user's own
// explicit example in full: folders (including subfolders) get 755,
// files (including nested ones) get 644, in a single Apply.
func TestApplyChmodDialogBothRecursiveOptionsCombine(t *testing.T) {
	root, target, topFile, subDir, subFile := chmodDialogFixture(t)

	r, err := NewRoot(tview.NewApplication(), root)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 1) // target/
	r.openChmod()
	r.stagedChmodMode = 0o755
	r.stagedChmodRecursiveDirs = true
	r.stagedChmodFilesEnabled = true
	r.stagedChmodFilesMode = 0o644

	r.applyChmodDialog()

	if got := chmodPerm(t, target); got != 0o755 {
		t.Errorf("target mode = %o, want 0755", got)
	}
	if got := chmodPerm(t, subDir); got != 0o755 {
		t.Errorf("subDir mode = %o, want 0755", got)
	}
	if got := chmodPerm(t, topFile); got != 0o644 {
		t.Errorf("topFile mode = %o, want 0644", got)
	}
	if got := chmodPerm(t, subFile); got != 0o644 {
		t.Errorf("subFile mode = %o, want 0644", got)
	}
}

// TestApplyChmodDialogMultipleTargets pins multi-select support (see
// selectedOrCurrentPaths) — the user's own explicit request that the
// dialog work "auf einen oder mehrere Ordner angewendet".
func TestApplyChmodDialogMultipleTargets(t *testing.T) {
	dir := fixtureDir(t)
	apple := filepath.Join(dir, "apple.txt")
	apricot := filepath.Join(dir, "apricot.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.toggleCheckbox(2) // apple.txt
	r.panel.toggleCheckbox(3) // apricot.txt
	r.openChmod()

	if len(r.chmodTargets) != 2 {
		t.Fatalf("chmodTargets = %v, want 2 entries", r.chmodTargets)
	}
	r.stagedChmodMode = 0o600

	r.applyChmodDialog()

	if got := chmodPerm(t, apple); got != 0o600 {
		t.Errorf("apple.txt mode = %o, want 0600", got)
	}
	if got := chmodPerm(t, apricot); got != 0o600 {
		t.Errorf("apricot.txt mode = %o, want 0600", got)
	}
}

// TestApplyChmodDialogContinuesPastFailureForOtherTargets pins
// applyChmodDialog's own "collect only the first error, keep going"
// convention: a target removed out from under the dialog fails its own
// fsops.Stat, but the other target in the same selection still gets
// chmod'd, and the failure still surfaces afterward.
func TestApplyChmodDialogContinuesPastFailureForOtherTargets(t *testing.T) {
	dir := fixtureDir(t)
	apple := filepath.Join(dir, "apple.txt")
	apricot := filepath.Join(dir, "apricot.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.toggleCheckbox(2) // apple.txt
	r.panel.toggleCheckbox(3) // apricot.txt
	r.openChmod()
	r.stagedChmodMode = 0o600

	// Removed after the dialog already captured chmodTargets, simulating
	// a target that stopped existing while the dialog was open.
	if err := os.Remove(apple); err != nil {
		t.Fatal(err)
	}

	r.applyChmodDialog()

	if r.activePage != errorPage {
		t.Error("the missing target's failure should surface as an error overlay")
	}
	if got := chmodPerm(t, apricot); got != 0o600 {
		t.Errorf("apricot.txt mode = %o, want 0600 (should still be applied despite apple.txt failing)", got)
	}
}

// TestCancelChmodDialogDiscardsChanges pins that Cancel never touches
// the real file, even after a toggle was flipped.
func TestCancelChmodDialogDiscardsChanges(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "app-data")
	before := chmodPerm(t, path)

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 1) // app-data
	r.openChmod()
	r.stagedChmodMode = 0o700
	r.stagedChmodRecursiveDirs = true

	r.cancelChmodDialog()

	if r.activePage != "" {
		t.Errorf("activePage = %q after Cancel, want closed", r.activePage)
	}
	if got := chmodPerm(t, path); got != before {
		t.Errorf("mode = %o, want unchanged %o", got, before)
	}
}

// chmodEditFieldOpen reports whether the shared inline text editor (see
// activateChmodTextField) is currently the one showing — the same role
// propertiesEditFieldOpen has for Properties.
func chmodEditFieldOpen(r *Root) bool {
	for _, p := range r.chmodPages.GetPageNames(true) {
		if p == "editfield" {
			return true
		}
	}
	return false
}

// TestOpenChmodResetsStaleEditField pins the real, reported bug: a real
// mouse click on Cancel/Apply while a value's shared inline editor is
// still open reaches chmodCancelBtn/chmodApplyBtn's own MouseHandler
// directly (see cancelChmodDialog/applyChmodDialog), never through
// finishChmodEdit — the only other place "editfield" gets hidden — so
// it used to stay showing, at whatever position/content it last had,
// right into the *next* open. That surfaced as a second, stray octal
// field in the wrong place even for a target with nothing to have
// generated it — a single plain file, chmodAnyDir false, no Files row
// rendered at all. openChmod's own HidePage("editfield") call (see its
// own doc comment) is what this pins.
func TestOpenChmodResetsStaleEditField(t *testing.T) {
	dir := fixtureDir(t)

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 1) // app-data — a directory, so it has a Files row to edit
	r.openChmod()
	r.stagedChmodFilesEnabled = true // Files' own bits/octal aren't field stops otherwise
	r.rerenderChmodDialog()

	filesSpan, ok := r.chmodSpanForField(chmodFieldFilesMode)
	if !ok {
		t.Fatal("no chmodFieldFilesMode span found")
	}
	r.activateChmodField(filesSpan) // opens the shared inline editor over it
	if !chmodEditFieldOpen(r) {
		t.Fatal("setup: editfield should be open after activating a text field")
	}

	r.cancelChmodDialog() // the real bug: bypasses finishChmodEdit entirely

	selectRow(r, 2) // apple.txt — a plain file, no Files row at all this time
	r.openChmod()

	if chmodEditFieldOpen(r) {
		t.Error("openChmod should reset a stale inline editor left open by a previous session")
	}
}

// TestOpenChmodNoopWithoutTarget pins that opening Chmod with nothing
// selected and nothing under the cursor (see selectedOrCurrentPaths) is
// a harmless no-op, not a crash.
func TestOpenChmodNoopWithoutTarget(t *testing.T) {
	dir := t.TempDir() // empty: only the ".." row exists
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openChmod()

	if r.activePage != "" {
		t.Errorf("activePage = %q, want still closed", r.activePage)
	}
}

// TestChmodFirstRowLabelSwitchesOnDirectory pins the user's own explicit
// request: the first row reads "Permissions:" for a plain-file-only
// selection (there's no separate Files row to disambiguate it from) and
// "Directory:" once a directory is involved (Files' own row exists
// alongside it then, so the more specific word — plus the shared
// "Permissions" column heading above both rows — says which one this is).
func TestChmodFirstRowLabelSwitchesOnDirectory(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	selectRow(r, 2) // apple.txt — plain file
	r.openChmod()
	got := r.chmodText.GetText(true)
	if !strings.Contains(got, "Permissions:") {
		t.Errorf("chmodText = %q, want it to contain %q for a plain-file selection", got, "Permissions:")
	}
	if strings.Contains(got, "Directory:") {
		t.Errorf("chmodText = %q, should not contain %q for a plain-file selection", got, "Directory:")
	}

	selectRow(r, 1) // app-data — a directory
	r.openChmod()
	got = r.chmodText.GetText(true)
	if !strings.Contains(got, "Directory:") {
		t.Errorf("chmodText = %q, want it to contain %q once a directory is selected", got, "Directory:")
	}
	if !strings.Contains(got, "Permissions") {
		t.Errorf("chmodText = %q, want it to still contain the %q column heading", got, "Permissions")
	}
}

// TestChmodFilesRowClickAnywhereEnablesWhenDisabled pins the user's own
// explicit request: while Files isn't enabled yet, a click doesn't have
// to land precisely on the ○ glyph — anywhere on that same row (e.g.
// over the dimmed permission display itself) turns it on too.
func TestChmodFilesRowClickAnywhereEnablesWhenDisabled(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 1) // app-data
	r.openChmod()

	if r.stagedChmodFilesEnabled {
		t.Fatal("setup: Files should start disabled")
	}
	span, ok := r.chmodSpanForField(chmodFieldFilesEnable)
	if !ok {
		t.Fatal("no chmodFieldFilesEnable span found")
	}
	// The row's own single span, while disabled, spans well past the
	// checkbox+label alone (see renderChmodDialog) — pin that it really
	// does cover the dimmed permission display too, not just "○ Files:".
	if span.endCol-span.startCol <= len("○ Files:") {
		t.Errorf("disabled Files row span is only %d cols wide, want it to also cover the dimmed permission display", span.endCol-span.startCol)
	}

	r.activateChmodField(span)
	if !r.stagedChmodFilesEnabled {
		t.Error("clicking anywhere on the disabled Files row should have enabled it")
	}
}

// TestChmodFilesRowShowsRecursiveHint pins the user's own explicit
// follow-up request: a plain "recursive" word follows Files' own value —
// explaining that checking the box reaches every file inside, not just
// the folder's own immediate children — shown in both states, disabled
// and enabled alike, and never its own clickable field either way
// (unlike Directory's own "recursive" toggle right above it, which is).
func TestChmodFilesRowShowsRecursiveHint(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	selectRow(r, 1) // app-data
	r.openChmod()

	if got := r.chmodText.GetText(true); !strings.Contains(got, "recursive") {
		t.Errorf("chmodText = %q, want it to contain %q while Files is disabled", got, "recursive")
	}

	r.stagedChmodFilesEnabled = true
	r.rerenderChmodDialog()
	if got := r.chmodText.GetText(true); !strings.Contains(got, "recursive") {
		t.Errorf("chmodText = %q, want it to still contain %q once Files is enabled", got, "recursive")
	}
}
