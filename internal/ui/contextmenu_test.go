package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// menuItemIndex returns the row index of the exact label in r.menu's
// current content (whichever level renderContextMenu last drew), or -1
// if it isn't there right now — the basis for both selectMenuItem below
// and every "is this entry showing" assertion in this file.
func menuItemIndex(r *Root, label string) int {
	for i := 0; i < r.menu.GetItemCount(); i++ {
		if main, _ := r.menu.GetItemText(i); main == label {
			return i
		}
	}
	return -1
}

// selectMenuItem finds label in r.menu's current content and activates
// it exactly the way Enter would (a real click runs the same List
// Selected callback) — t.Fatal if it isn't there at all.
func selectMenuItem(t *testing.T, r *Root, label string) {
	t.Helper()
	idx := menuItemIndex(r, label)
	if idx < 0 {
		t.Fatalf("no %q item in the menu's current content", label)
	}
	r.menu.SetCurrentItem(idx)
	r.menu.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
}

// openMenuOnRow opens the context menu for row (0-based, matching the
// table's own rows including ".."), the same way a right-click on that
// row would: sets target/targetRow first, exactly like captureMouse's
// own MouseRightClick case, since several entries' own visibility rules
// (menuTargetIsFile) and actions read those rather than the panel's
// live cursor.
func openMenuOnRow(t *testing.T, r *Root, row int) {
	t.Helper()
	ref, ok := r.panel.rowRef(row)
	if !ok {
		t.Fatalf("no row %d to open the menu on", row)
	}
	r.target = ref.path
	r.targetRow = row
	r.showMenu(0, 0)
}

// TestContextMenuTopLevelForAFile pins the top-level structure for the
// common case — cursor on a plain file, nothing checked, clipboard
// empty, no split — exactly the entries that always apply plus the
// three submenus, in order. This is deliberately much shorter than the
// roughly forty rows the menu used to always show regardless of
// context — see contextmenu.go's own package doc for why.
func TestContextMenuTopLevelForAFile(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 2) // apple.txt — see fixtureDir

	want := []string{
		"Look", "Edit", "Rename", "Copy", "Cut", "Move to Trash", "Properties",
		menuGroupGlyph + "More actions",
		menuGroupGlyph + "Selection",
		menuGroupGlyph + "Tabs & Split",
	}
	if got := r.menu.GetItemCount(); got != len(want) {
		t.Fatalf("menu has %d items, want %d: %v", got, len(want), want)
	}
	for i, label := range want {
		if main, _ := r.menu.GetItemText(i); main != label {
			t.Errorf("item %d = %q, want %q", i, main, label)
		}
	}
}

// TestContextMenuHidesEditAndTailForADirectory pins menuTargetIsFile:
// "Edit" and (inside "More actions") "tail -f" don't apply to a
// directory and shouldn't be offered for one.
func TestContextMenuHidesEditAndTailForADirectory(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 1) // app-data/ sorts first (directories before files — see fixtureDir/Panel's own sort)

	if got, _ := r.panel.rowRef(r.targetRow); !got.isDir {
		t.Fatalf("setup: row %d = %q, want the app-data directory", r.targetRow, got.name)
	}
	if menuItemIndex(r, "Edit") >= 0 {
		t.Error(`"Edit" should not be offered for a directory`)
	}

	selectMenuItem(t, r, menuGroupGlyph+"More actions")
	if menuItemIndex(r, "tail -f") >= 0 {
		t.Error(`"tail -f" should not be offered for a directory`)
	}
}

// TestContextMenuShowsPasteOnlyWithClipboardContent pins
// menuClipboardHasContent: "Paste" is absent until Copy or Cut has
// actually put something there.
func TestContextMenuShowsPasteOnlyWithClipboardContent(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 2) // apple.txt (row 2 — app-data/ sorts first, see fixtureDir)

	if menuItemIndex(r, "Paste") >= 0 {
		t.Error(`"Paste" should not be offered with an empty clipboard`)
	}

	r.closeMenu()
	r.copyToClipboard()
	openMenuOnRow(t, r, 2) // apple.txt (row 2 — app-data/ sorts first, see fixtureDir)

	if menuItemIndex(r, "Paste") < 0 {
		t.Error(`"Paste" should be offered once Copy has filled the clipboard`)
	}
}

// TestContextMenuShowsSplitOrientationAndSwapOnlyOnceSplitIsActive pins
// menuSplitIsActive: the "Tabs & Split" submenu's own orientation and
// swap entries only mean something once a split actually exists.
func TestContextMenuShowsSplitOrientationAndSwapOnlyOnceSplitIsActive(t *testing.T) {
	r, _, _ := newSplitRoot(t)
	r.exitSplit() // newSplitRoot may or may not start split — force the "off" state

	openMenuOnRow(t, r, 0) // ".." — newSplitRoot's own first directory has no real files, and none is needed here
	selectMenuItem(t, r, menuGroupGlyph+"Tabs & Split")
	if menuItemIndex(r, "Split above/below") >= 0 || menuItemIndex(r, "Split side by side") >= 0 {
		t.Error("split orientation should not be offered without an active split")
	}
	if menuItemIndex(r, "Swap panes") >= 0 {
		t.Error("Swap panes should not be offered without an active split")
	}

	r.closeMenu()
	r.enterSplit(1)

	openMenuOnRow(t, r, 0)
	selectMenuItem(t, r, menuGroupGlyph+"Tabs & Split")
	if menuItemIndex(r, "Split above/below") < 0 && menuItemIndex(r, "Split side by side") < 0 {
		t.Error("split orientation should be offered once a split is active")
	}
	if menuItemIndex(r, "Swap panes") < 0 {
		t.Error("Swap panes should be offered once a split is active")
	}
}

// TestContextMenuInTrashShowsOnlyTrashActions pins trashMenuTree's own
// replacement of the ordinary menu entirely while browsing the Trash —
// almost nothing about the ordinary list still applies to an
// already-trashed item (see trashMenuTree's own doc comment).
func TestContextMenuInTrashShowsOnlyTrashActions(t *testing.T) {
	r, _, file := newTestRootWithFile(t)
	r.target = file
	r.targetRow = 1
	r.moveSelectionToTrash()
	r.openTrash()

	r.showMenu(0, 0)

	want := []string{"Restore from Trash", "Empty Trash", "Properties"}
	if got := r.menu.GetItemCount(); got != len(want) {
		t.Fatalf("trash menu has %d items, want %d: %v", got, len(want), want)
	}
	for i, label := range want {
		if main, _ := r.menu.GetItemText(i); main != label {
			t.Errorf("item %d = %q, want %q", i, main, label)
		}
	}
}

// TestContextMenuSubmenuShowsItsOwnEntries pins that drilling into a
// group replaces the list with exactly that group's own members, led by
// a "◂ Back" row.
func TestContextMenuSubmenuShowsItsOwnEntries(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 2) // apple.txt (row 2 — app-data/ sorts first, see fixtureDir)

	selectMenuItem(t, r, menuGroupGlyph+"Selection")

	want := []string{menuBackGlyph, "Select all", "Deselect all", "Select +", "Select -"}
	if got := r.menu.GetItemCount(); got != len(want) {
		t.Fatalf("submenu has %d items, want %d: %v", got, len(want), want)
	}
	for i, label := range want {
		if main, _ := r.menu.GetItemText(i); main != label {
			t.Errorf("item %d = %q, want %q", i, main, label)
		}
	}
	if got, want := r.menuTitleBar.GetText(true), " Menu › Selection "; got != want {
		t.Errorf("title bar = %q, want the breadcrumb %q", got, want)
	}
}

// TestContextMenuBackRowReturnsToTopLevel pins that selecting "◂ Back"
// (the same as clicking it) returns to the top-level list, not just
// closes the submenu view some other way.
func TestContextMenuBackRowReturnsToTopLevel(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 2) // apple.txt (row 2 — app-data/ sorts first, see fixtureDir)
	selectMenuItem(t, r, menuGroupGlyph+"Tabs & Split")

	selectMenuItem(t, r, menuBackGlyph)

	if r.activePage != contextMenuPage {
		t.Fatalf("activePage = %q, want the menu still open (%q)", r.activePage, contextMenuPage)
	}
	if menuItemIndex(r, "Look") < 0 {
		t.Error(`back at the top level, "Look" should be showing again`)
	}
	if got, want := r.menuTitleBar.GetText(true), " Menu "; got != want {
		t.Errorf("title bar = %q, want the plain top-level title %q", got, want)
	}
}

// TestContextMenuEscapeGoesBackThenCloses pins the two-step Escape
// behavior: one press backs out of a submenu, a second (now at the top
// level) closes the menu entirely — the same "back before close" shape
// resolveChord's own Escape handling already established for the
// keyboard layer's chords.
func TestContextMenuEscapeGoesBackThenCloses(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 2) // apple.txt (row 2 — app-data/ sorts first, see fixtureDir)
	selectMenuItem(t, r, menuGroupGlyph+"More actions")

	r.menu.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})
	if r.activePage != contextMenuPage {
		t.Fatalf("activePage = %q after one Escape, want still open at the top level (%q)", r.activePage, contextMenuPage)
	}
	if menuItemIndex(r, "Look") < 0 {
		t.Error("one Escape from a submenu should return to the top level, not close the menu")
	}

	r.menu.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})
	if r.activePage != "" {
		t.Errorf("activePage = %q after a second Escape, want closed (\"\")", r.activePage)
	}
}

// TestContextMenuLeftArrowGoesBackFromSubmenu pins the second way back
// out of a submenu (see captureContextMenuKey) — Left arrow, alongside
// Escape and clicking/selecting "◂ Back".
func TestContextMenuLeftArrowGoesBackFromSubmenu(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 2) // apple.txt (row 2 — app-data/ sorts first, see fixtureDir)
	selectMenuItem(t, r, menuGroupGlyph+"Selection")

	r.menu.InputHandler()(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.activePage != contextMenuPage {
		t.Fatalf("activePage = %q, want the menu still open", r.activePage)
	}
	if menuItemIndex(r, "Look") < 0 {
		t.Error("Left arrow from a submenu should return to the top level")
	}
}

// TestContextMenuLeftArrowAtTopLevelFallsThrough pins
// captureContextMenuKey's own guard: Left arrow at the top level (where
// there's nothing to back out of) reaches tview.List's own default
// handling untouched, rather than being silently swallowed.
func TestContextMenuLeftArrowAtTopLevelFallsThrough(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 2) // apple.txt (row 2 — app-data/ sorts first, see fixtureDir)

	if got := r.captureContextMenuKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)); got == nil {
		t.Error("Left arrow at the top level should not be consumed")
	}
}

// TestContextMenuReopeningStartsAtTopLevel pins that closing the menu
// from inside a submenu and opening it again starts fresh at the top —
// a real risk with content rebuilt in place rather than recreated (see
// showMenu's own explicit menuInSubmenu reset).
func TestContextMenuReopeningStartsAtTopLevel(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 2) // apple.txt (row 2 — app-data/ sorts first, see fixtureDir)
	selectMenuItem(t, r, menuGroupGlyph+"Selection")
	r.closeMenu()

	openMenuOnRow(t, r, 2) // apple.txt (row 2 — app-data/ sorts first, see fixtureDir)

	if menuItemIndex(r, "Look") < 0 {
		t.Error("reopening the menu should start at the top level, not the last submenu")
	}
	if got, want := r.menuTitleBar.GetText(true), " Menu "; got != want {
		t.Errorf("title bar = %q, want the plain top-level title %q", got, want)
	}
}

// TestContextMenuResizesForItsCurrentContent pins that menuLayout's own
// rect actually changes height between the top level and a shorter
// submenu — resizeContextMenu is called on every render, not only the
// initial open, so drilling in/out never leaves stale dead space or a
// clipped list behind.
func TestContextMenuResizesForItsCurrentContent(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	openMenuOnRow(t, r, 2) // apple.txt (row 2 — app-data/ sorts first, see fixtureDir)

	_, _, _, topHeight := r.menuLayout.GetRect()

	selectMenuItem(t, r, menuGroupGlyph+"Selection")
	_, _, _, submenuHeight := r.menuLayout.GetRect()

	// The "Selection" submenu (4 members + "◂ Back" = 5 rows) is shorter
	// than the top level (7 leaves + 3 groups = 10 rows) in this fixture,
	// so the two heights must actually differ, not just both exist.
	if topHeight == submenuHeight {
		t.Errorf("menuLayout height stayed %d after drilling into a shorter submenu, want it to shrink", topHeight)
	}
}

// TestContextMenuCopyPasteRoundTrip is an end-to-end smoke test through
// the new drill-down/context-sensitive menu: Copy on one file, Paste
// (only visible now that the clipboard has content) into a different
// directory, confirming the file actually landed there — pinning that
// the restructuring didn't just move labels around but left the real
// actions correctly wired.
func TestContextMenuCopyPasteRoundTrip(t *testing.T) {
	src := fixtureDir(t)
	dst := t.TempDir()
	isolateUserConfigFile(t)
	r, err := NewRoot(tview.NewApplication(), src)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	openMenuOnRow(t, r, 2) // apple.txt
	selectMenuItem(t, r, "Copy")

	if err := r.panel.navigate(dst); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	openMenuOnRow(t, r, 0) // ".." — Paste doesn't need a real file target

	// Paste runs through startPaste's own async engine now (see
	// pasteconflict.go) — isolatePasteIO/waitPasteIO give this a
	// deterministic way to wait for its background goroutine to have
	// actually done the real copy before checking for it, rather than
	// racing a background goroutine that may not even have started yet.
	done := isolatePasteIO(t)
	selectMenuItem(t, r, "Paste")
	waitPasteIO(t, done, 1)

	if _, err := os.Stat(filepath.Join(dst, "apple.txt")); err != nil {
		t.Errorf("apple.txt should have been pasted into %s: %v", dst, err)
	}
}

// TestContextMenuMnemonicFiresMatchingEntry pins the user's own
// explicit request: once the menu is open, the same letter each of
// Look/Edit/Rename/Copy/Cut/Move to Trash already has as its own
// single-key equivalent in the primary keyboard layer (see plainCommands
// in keymap.go) fires that same action directly, without arrowing down
// to it first.
func TestContextMenuMnemonicFiresMatchingEntry(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 2) // apple.txt

	r.menu.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModNone), func(tview.Primitive) {})

	if len(r.clipboard) == 0 {
		t.Error("'c' should have fired Copy directly, the same as selecting it with Enter")
	}
}

// TestContextMenuMnemonicMoveToTrashActuallyMoves is the same pin as
// above, but for "d" specifically, checked against a real, end-to-end
// effect rather than just the clipboard: not just that some method got
// called, but that the file is actually gone from its original
// location.
func TestContextMenuMnemonicMoveToTrashActuallyMoves(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	target := filepath.Join(dir, "apple.txt")
	r.panel.focusRow(2) // moveSelectionToTrash reads the panel's own cursor, not r.target
	openMenuOnRow(t, r, 2)

	r.menu.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone), func(tview.Primitive) {})

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("'d' should have moved apple.txt to Trash, stat err = %v", err)
	}
}

// TestContextMenuMnemonicIgnoresHiddenEntry pins the other half: a
// mnemonic whose own entry isn't currently visible (Edit, for a
// directory) must not fire at all — captureContextMenuKey passes the
// key through untouched, exactly as if no mnemonic existed here for it.
func TestContextMenuMnemonicIgnoresHiddenEntry(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 1) // app-data/ — a directory (see fixtureDir), Edit hidden here

	event := tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone)
	if got := r.captureContextMenuKey(event); got != event {
		t.Error("captureContextMenuKey should pass an unmatched mnemonic through untouched")
	}
}

// TestContextMenuMnemonicIgnoresCtrlAndAltModified pins that a mnemonic
// only fires for a bare, unmodified letter — the same guard
// acceptsPlainKeyCommand's own dispatch already applies for the primary
// keyboard layer, so Ctrl+C (a terminal-wide interrupt everywhere else
// in this app) is never silently reinterpreted as "Copy" just because
// the menu happens to be open.
func TestContextMenuMnemonicIgnoresCtrlAndAltModified(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 2) // apple.txt

	event := tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModCtrl)
	if got := r.captureContextMenuKey(event); got != event {
		t.Error("Ctrl+c should pass through untouched, not fire the Copy mnemonic")
	}
	if len(r.clipboard) != 0 {
		t.Error("Ctrl+c must not have fired Copy")
	}
}
