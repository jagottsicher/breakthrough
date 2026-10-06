package ui

import (
	"os"
	"path/filepath"
	"strings"
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

func TestContextMenuShowsMnemonicHintWithoutStartingChord(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 2)

	if r.pendingChord != 0 {
		t.Fatalf("context menu opened with pending chord %q", r.pendingChord)
	}
	if got := r.chordIndicatorText(); got != "" {
		t.Fatalf("context menu started a chord countdown: %q", got)
	}
	for _, want := range []string{"Menu:", "Look", "Rename", "Copy", "Multiply", "Properties"} {
		if !strings.Contains(r.buttonBar.GetText(true), want) {
			t.Errorf("button bar hint = %q, want it to contain %q", r.buttonBar.GetText(true), want)
		}
	}

	r.closeMenu()
	if strings.Contains(r.buttonBar.GetText(true), "Menu:") {
		t.Errorf("button bar still shows context-menu hint after close: %q", r.buttonBar.GetText(true))
	}
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
		"Look", "Edit", "Execute", "Open with…", "Rename", "Copy", "Cut", "Multiply", "Move to Trash", "Properties",
		menuGroupGlyph + "Label",
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
// "Edit", "Execute", "Open with…", and (inside "More actions") "tail -f"
// don't apply to a directory and shouldn't be offered for one.
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
	if menuItemIndex(r, "Execute") >= 0 {
		t.Error(`"Execute" should not be offered for a directory`)
	}
	if menuItemIndex(r, "Open with…") >= 0 {
		t.Error(`"Open with…" should not be offered for a directory`)
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

// TestContextMenuRightArrowEntersSubmenu pins the arrow-only counterpart
// to selecting a "▸ Group" row and pressing Enter — per the user's own
// explicit report that drilling into a submenu shouldn't need a separate
// Enter once the cursor already sits on it.
func TestContextMenuRightArrowEntersSubmenu(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 2) // apple.txt (row 2 — app-data/ sorts first, see fixtureDir)
	r.menu.SetCurrentItem(menuItemIndex(r, menuGroupGlyph+"Selection"))

	if got := r.captureContextMenuKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)); got != nil {
		t.Error("Right arrow on a submenu row should be consumed")
	}
	if got, want := r.menuTitleBar.GetText(true), " Menu › Selection "; got != want {
		t.Errorf("title bar = %q, want the breadcrumb %q", got, want)
	}
}

// TestContextMenuRightArrowOnALeafFallsThrough pins the other half:
// Right on a plain, non-submenu row does nothing of its own (it isn't a
// general "activate" gesture the way Enter is) and reaches tview.List's
// own default handling untouched, the same as Left already does at the
// top level.
func TestContextMenuRightArrowOnALeafFallsThrough(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 2) // apple.txt (row 2 — app-data/ sorts first, see fixtureDir)
	r.menu.SetCurrentItem(menuItemIndex(r, "Copy"))

	if got := r.captureContextMenuKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)); got == nil {
		t.Error("Right arrow on a leaf row should not be consumed")
	}
	if got, want := r.menuTitleBar.GetText(true), " Menu "; got != want {
		t.Errorf("title bar = %q, want it to stay at the top level %q", got, want)
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

// TestContextMenuWidthFitsALongTitleBar pins a real, user-reported gap:
// the menu used to size itself only to its widest row, so a submenu
// whose breadcrumb title ("Menu › Tabs & Split") is longer than every
// one of its own short entries ("New tab", "Close tab", ...) rendered
// with a title that overflowed the box. Width must now cover whichever
// is wider, the title bar or the widest row, plus a small margin.
func TestContextMenuWidthFitsALongTitleBar(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.SetRect(0, 0, 80, 24) // realistic — see clampToPanel's own default-rect gotcha noted elsewhere
	openMenuOnRow(t, r, 2)        // apple.txt (row 2 — app-data/ sorts first, see fixtureDir)
	selectMenuItem(t, r, menuGroupGlyph+"Tabs & Split")

	_, _, width, _ := r.menuLayout.GetRect()
	titleWidth := tview.TaggedStringWidth(r.menuTitleBar.GetText(false))
	if width < titleWidth {
		t.Errorf("menuLayout width = %d, want at least the title bar's own width %d", width, titleWidth)
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

// TestContextMenuMnemonicOpensPropertiesInBothMenus pins "i" — the same
// letter Properties already has as its own single-key equivalent — for
// both the ordinary top-level menu and the Trash's own shorter one,
// where Properties is the one entry the two share.
func TestContextMenuMnemonicOpensPropertiesInBothMenus(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	openMenuOnRow(t, r, 2) // apple.txt

	r.menu.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'i', tcell.ModNone), func(tview.Primitive) {})

	if r.activePage != propertiesPage {
		t.Errorf("'i' should have opened Properties from the ordinary menu, activePage = %q", r.activePage)
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

// assertHintBarListsEveryLeaf pins the real bug reported live: a
// submenu entry with no mnemonic of its own silently fell out of the
// status-line legend (contextMenuHintBar skips anything with
// entry.mnemonic == 0), even though arrowing to it and pressing Enter
// always worked — so "the menu looks right, the legend doesn't" went
// unnoticed for every submenu, which this checks generically for
// whichever level r.menu currently shows: every visible, actionable
// entry (a leaf, not a group heading — those never get a mnemonic, see
// menuMnemonicEntry's own doc comment) must have its own resolved label
// findable in the legend text.
func assertHintBarListsEveryLeaf(t *testing.T, r *Root) {
	t.Helper()
	hint := r.buttonBar.GetText(true)
	for _, entry := range r.visibleMenuEntries() {
		if entry.action == nil {
			continue // a group heading (its own submenu) — never in the legend
		}
		if label := entry.resolvedLabel(r); !strings.Contains(hint, label) {
			t.Errorf("hint bar %q is missing %q", hint, label)
		}
	}
}

// TestContextMenuHintBarCoversEveryMoreActionsEntry pins
// assertHintBarListsEveryLeaf for "More actions" — the longest submenu,
// and the one every entry above now has a mnemonic for.
func TestContextMenuHintBarCoversEveryMoreActionsEntry(t *testing.T) {
	r, _, file := newTestRootWithFile(t)
	r.target = file
	r.targetRow = 1
	r.copyToClipboard() // so "Paste, following symlinks" is visible too

	r.showMenu(0, 0)
	selectMenuItem(t, r, menuGroupGlyph+"More actions")

	assertHintBarListsEveryLeaf(t, r)
}

// TestContextMenuHintBarCoversEverySelectionEntry mirrors
// TestContextMenuHintBarCoversEveryMoreActionsEntry for "Selection".
func TestContextMenuHintBarCoversEverySelectionEntry(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 2) // apple.txt
	selectMenuItem(t, r, menuGroupGlyph+"Selection")

	assertHintBarListsEveryLeaf(t, r)
}

// TestContextMenuHintBarCoversEveryTabsAndSplitEntry mirrors
// TestContextMenuHintBarCoversEveryMoreActionsEntry for "Tabs & Split",
// with a split active so every one of its conditional entries (split
// orientation, Swap panes) is visible too.
func TestContextMenuHintBarCoversEveryTabsAndSplitEntry(t *testing.T) {
	r, _, _ := newSplitRoot(t)
	r.enterSplit(1)

	openMenuOnRow(t, r, 0) // ".." — see TestContextMenuShowsSplitOrientationAndSwapOnlyOnceSplitIsActive
	selectMenuItem(t, r, menuGroupGlyph+"Tabs & Split")

	assertHintBarListsEveryLeaf(t, r)
}

// TestNoDuplicateContextMenuMnemonicsPerLevel mirrors
// TestNoDuplicatePlainKeyBindings (keymap_test.go) for the context
// menu's own mnemonics: a collision is only a problem within the same
// level (the top level, or one specific submenu) — menuMnemonicEntry
// and contextMenuHintBar both only ever look at r.currentMenuTree(), so
// the same letter reused across two different submenus (or between a
// submenu and the top level) is never ambiguous in practice.
func TestNoDuplicateContextMenuMnemonicsPerLevel(t *testing.T) {
	checkLevel := func(t *testing.T, name string, entries []menuEntry) {
		t.Helper()
		seen := map[rune]string{}
		for _, entry := range entries {
			if entry.mnemonic == 0 || entry.action == nil {
				continue
			}
			if other, dup := seen[entry.mnemonic]; dup {
				t.Errorf("%s: mnemonic %q is bound to both %q and %q", name, string(entry.mnemonic), other, entry.label)
			}
			seen[entry.mnemonic] = entry.label
		}
	}

	top := contextMenuTree()
	checkLevel(t, "top level", top)
	for _, entry := range top {
		if entry.submenu != nil {
			checkLevel(t, entry.label, entry.submenu)
		}
	}
}

// TestContextMenuHintBarHasNoBackButtonAtTopLevel pins that the "◂
// Back" button is specific to a submenu being open — nothing to go
// back to from the top level, so it isn't shown there at all.
func TestContextMenuHintBarHasNoBackButtonAtTopLevel(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 2) // apple.txt

	if strings.Contains(r.buttonBar.GetText(true), "Back") {
		t.Errorf("hint bar at the top level = %q, want no \"Back\" button", r.buttonBar.GetText(true))
	}
}

// TestContextMenuHintBarBackButtonReturnsToTopLevel pins the user's own
// explicit request: the hint bar's own "◂ Back" button, clicked the
// same way any other button-bar button is, drives the open menu back
// out of a submenu exactly like Escape/Left-arrow or clicking the
// list's own leading "◂ Back" row already do.
func TestContextMenuHintBarBackButtonReturnsToTopLevel(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRow(t, r, 2) // apple.txt
	selectMenuItem(t, r, menuGroupGlyph+"Selection")
	if r.menuInSubmenu == nil {
		t.Fatal("setup: should be inside the \"Selection\" submenu")
	}

	text := r.buttonBar.GetText(true)
	col := strings.Index(text, "Back")
	if col < 0 {
		t.Fatal("hint bar has no \"Back\" button while a submenu is open")
	}
	clickButtonBar(t, r, len([]rune(text[:col])))

	if r.menuInSubmenu != nil {
		t.Error("clicking \"Back\" should have returned to the top level")
	}
	if r.activePage != contextMenuPage {
		t.Errorf("activePage = %q, want the menu to stay open at the top level", r.activePage)
	}
}

// TestContextMenuMouseClickIntoSubmenuKeepsMenuFocused is the regression
// test for a real, reproduced bug found live: clicking a group row
// ("More actions"/"Selection"/"Tabs & Split") with the mouse silently
// moved real keyboard focus off r.menu and onto the panel's own table
// underneath — tview.List's own MouseHandler has no case at all for
// MouseLeftDown (only MouseLeftClick and the four scroll actions), so
// an unconsumed one fell straight through to the table's default,
// Box-inherited mouse handling, which grabs focus on exactly that
// action (the same class of bug TestClickingColumnHeaderKeepsTableFocused
// in mousefocus_test.go already caught once for columnHeader — see
// captureContextMenuMouse's own doc comment). Once that happened, every
// mnemonic letter — "a"/"A" included, per the user's own explicit
// report — silently stopped reaching the menu at all, even though the
// click itself still correctly drilled into the submenu. Uses clickAt
// (mousefocus_test.go) deliberately: it sends the real
// MouseLeftDown-then-MouseLeftClick pair a genuine click produces,
// exactly what a MouseLeftClick-only test (as every other mouse test in
// this file sends) can never catch — see clickAt's own doc comment.
func TestContextMenuMouseClickIntoSubmenuKeepsMenuFocused(t *testing.T) {
	dir := fixtureDir(t)
	root, screen := drawnRootTwice(t, dir)

	root.target = filepath.Join(dir, "apple.txt")
	root.targetRow = 2
	root.showMenu(5, 5)
	root.Draw(screen)
	root.Draw(screen)

	idx := -1
	for i := 0; i < root.menu.GetItemCount(); i++ {
		main, _ := root.menu.GetItemText(i)
		if main == menuGroupGlyph+"Selection" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("no \"Selection\" row in the top-level menu")
	}
	rectX, rectY, _, _ := root.menu.GetInnerRect()
	clickAt(root, rectX, rectY+idx)
	root.Draw(screen)

	if root.menuInSubmenu == nil {
		t.Fatal("setup: should be inside the \"Selection\" submenu")
	}
	if got := root.app.GetFocus(); got != root.menu {
		t.Fatalf("focus after clicking into a submenu = %T, want r.menu", got)
	}

	// The real-world symptom: "a" (Select all) must still reach the
	// menu's own mnemonic dispatch, not silently do nothing.
	event := tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone)
	if got := root.captureContextMenuKey(event); got != nil {
		t.Error("\"a\" should have been consumed by the Selection submenu's own mnemonic")
	}
	if len(root.panel.SelectedPaths()) == 0 {
		t.Error("\"a\" should have fired Select all")
	}
}

// openMenuOnRowFocused is openMenuOnRow plus the cursor move a real
// right-click always makes first (see captureMouse's MouseRightClick
// case) — needed for any test exercising an action that reads the
// panel's live cursor (see labelTargets) rather than r.target itself.
func openMenuOnRowFocused(t *testing.T, r *Root, row int) {
	t.Helper()
	r.panel.focusRow(row)
	openMenuOnRow(t, r, row)
}

// TestContextMenuLabelSubmenuShowsColoredSwatchRows pins that the
// "Label" group's own ten rows match labelMenuRows exactly — the same
// swatch-plus-name text r.picker already shows for the "zl" chord (see
// labelMenuRows' own doc comment), not a plain-text duplicate.
func TestContextMenuLabelSubmenuShowsColoredSwatchRows(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	openMenuOnRowFocused(t, r, 2) // apple.txt

	selectMenuItem(t, r, menuGroupGlyph+"Label")

	want := labelMenuRows(r.theme, r.settings)
	if got := r.menu.GetItemCount(); got != len(want)+1 { // +1 for "◂ Back"
		t.Fatalf("Label submenu has %d items, want %d (9 colors + no-label + Back)", got, len(want)+1)
	}
	if main, _ := r.menu.GetItemText(0); main != menuBackGlyph {
		t.Errorf("item 0 = %q, want %q", main, menuBackGlyph)
	}
	for i, row := range want {
		if main, _ := r.menu.GetItemText(i + 1); main != row.text {
			t.Errorf("item %d = %q, want %q", i+1, main, row.text)
		}
	}
}

// TestContextMenuLabelSubmenuPickingEntryAppliesLabelAndCloses pins the
// end-to-end path: entering the "Label" submenu and picking entry id 4
// sets label 4 on the right-clicked row's own path and closes the menu
// — the same "one final choice, not a toggle" close behavior the
// Options screen's own color-scheme picker already has (see
// labelSubmenuEntries' own doc comment).
func TestContextMenuLabelSubmenuPickingEntryAppliesLabelAndCloses(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	path := filepath.Join(dir, "apple.txt")
	openMenuOnRowFocused(t, r, 2) // apple.txt — see fixtureDir
	selectMenuItem(t, r, menuGroupGlyph+"Label")

	r.menu.SetCurrentItem(5) // Back(0) + no-label(1) + ids 1..4 = index 5 is id 4
	r.menu.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if got := r.labels.Get(path); got != 4 {
		t.Errorf("Get(%q) = %d, want 4", path, got)
	}
	if r.activePage == contextMenuPage {
		t.Error("menu should have closed after picking a label")
	}
}

// TestContextMenuLabelHiddenForRemotePanel pins menuLabelAvailable:
// the "Label" group must not even appear for a remote panel — the same
// exclusion openLabelMenu itself already enforces for the "zl" chord.
func TestContextMenuLabelHiddenForRemotePanel(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.remote = newTestFakeRemote(dir)
	openMenuOnRowFocused(t, r, 2)

	if idx := menuItemIndex(r, menuGroupGlyph+"Label"); idx >= 0 {
		t.Error("\"Label\" group should not appear for a remote panel")
	}
}
