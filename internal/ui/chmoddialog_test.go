package ui

import (
	"os"
	"path/filepath"
	"testing"

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
// once any target is a directory, every field shows.
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
	for _, f := range []chmodField{chmodFieldMode, chmodFieldRecursiveDirs, chmodFieldFilesMode, chmodFieldFilesEnable} {
		if _, ok := r.chmodSpanForField(f); !ok {
			t.Errorf("field %v should be rendered for a directory target", f)
		}
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
