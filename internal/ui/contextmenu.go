package ui

import (
	"github.com/gdamore/tcell/v2"
)

// The context menu: a single registry (contextMenuTree/trashMenuTree),
// the same "one place a menu entry is declared" discipline the keyboard
// layer's own registry already established (see keymap.go's own package
// doc) — rendered fresh every time it's opened or navigated, rather than
// ~40 unconditional AddItem calls at construction time with a handful of
// fragile fixed-index fields (hiddenToggleIdx and friends, in an earlier
// version of this file) mutated in place afterward to keep two or three
// labels honest.
//
// Built to replace a menu that showed the same roughly forty rows no
// matter what was actually selected or where — per the user's own
// explicit request for something genuinely context-sensitive and
// visibly shorter, not just reorganized: every entry below carries its
// own visibility rule instead of always being there, and the once-flat
// "Selection"/"Commands"/"Tabs"/"Tools"/"Globals" sections have become
// either an entry's own visibility check or a one-level submenu you
// drill into on demand (see menuEntry.submenu) — Windows Explorer's own
// cascading-menu convention, minus the actual cascade: a submenu
// replaces the current list in place rather than opening beside it,
// which needs no horizontal room a narrow terminal might not have.
//
// Three placeholder entries that only ever showed a "not implemented
// yet" notice (grep, zgrep, du, df — see the now-deleted
// placeholderMenuAction) are gone rather than moved into a submenu: a
// slimmer menu was the explicit goal, and a permanent placeholder is the
// opposite of that. The three "Globals" display toggles (hidden files,
// size format, time format) are gone too — they're panel-wide view
// settings, not an action on the selected file, and they already live in
// the Options screen and on their own keys ("." and the "z" chord).

// menuEntry is one row the context menu can show.
//
// Exactly one of action/submenu is set: a leaf runs action and closes
// nothing on its own (most actions open their own overlay, which
// replaces the menu the same way it always has); a group has submenu
// instead and, when chosen, drills into it (see enterMenuSubmenu)
// rather than running anything itself.
type menuEntry struct {
	label string

	// dynamicLabel overrides label when set — for the one or two entries
	// (Split on/off, its own orientation) that need to describe what
	// choosing them will do *next*, which depends on live state at
	// render time, the same convention buildButtonBar's own
	// hideUnhideLabel/splitButtonLabel already use for the button bar.
	dynamicLabel func(r *Root) string

	// visible reports whether this entry appears at all right now — nil
	// means always. Checked fresh on every render (renderContextMenu),
	// so it can never show a row that no longer applies, or hide one
	// state changed to make relevant, the way a menu built once at
	// startup and only ever relabelled in place could.
	visible func(r *Root) bool

	// mnemonic, if set, is a plain-letter shortcut for this entry while
	// the menu holding it is open (see captureContextMenuKey) — per the
	// user's own explicit request: "m" (Context menu) opens the menu
	// exactly as it always has, and once it's open, the same letter each
	// of these actions already has as its own single-key equivalent in
	// the primary keyboard layer (see plainCommands in keymap.go) fires
	// it directly, without needing to arrow down to it first. Deliberately
	// the plain-key layer's own letter for each action, not the entry's
	// own first letter — "Copy" and "Cut" would otherwise collide on "C",
	// and "Move to Trash" would misleadingly suggest "M" (already "m"
	// itself, the menu's own key) rather than its real letter, "d". 0
	// (the zero rune) for every entry with no such shortcut, which is
	// most of them — this is for the handful the user singled out
	// explicitly, not a rule every entry has to have an opinion on.
	//
	// "Multiply" is the one deliberate exception to "not the entry's own
	// first letter": it has no plain-key equivalent to mirror at all —
	// Duplicate only ever opens from inside this menu (see duplicate.go),
	// never from a bare letter of its own — so there is nothing else for
	// its mnemonic to agree with. Its own first letter happening to be
	// the same "m" that opens the menu is the whole point, per the user's
	// own explicit design: "m" opens the menu, "mm" duplicates.
	mnemonic rune

	action  func(r *Root)
	submenu []menuEntry
}

// resolvedLabel is what this entry actually shows, static or computed.
func (e menuEntry) resolvedLabel(r *Root) string {
	if e.dynamicLabel != nil {
		return e.dynamicLabel(r)
	}
	return e.label
}

// menuGroupGlyph/menuBackGlyph mark a drill-in and a step back the same
// geometric-Unicode way this app's own close/split/resize buttons
// already do (✕, ◫, ◢) — a visual vocabulary already established rather
// than a new one invented just for this.
const (
	menuGroupGlyph = "▸ "
	menuBackGlyph  = "◂ Back"
)

// contextMenuTree is the top-level menu — see this file's own package
// doc for why it looks the way it does. Everything actually shown is a
// registry lookup away from what runs it, the same "can't drift apart"
// property the keyboard layer's own plainCommands/chordFamilies already
// have.
func contextMenuTree() []menuEntry {
	return []menuEntry{
		// Look first and default-selected (see showMenu's own
		// SetCurrentItem(0)), per the user's own explicit request — the
		// same read-only, no-side-effects action Enter on a plain file
		// already tries too (see Panel.activateRow), so it's also this
		// menu's own most-likely-wanted default.
		{label: "Look", mnemonic: 'l', action: func(r *Root) { r.lookCurrentEntry() }},
		{label: "Edit", visible: menuTargetIsFile, mnemonic: 'e', action: func(r *Root) { r.editCurrentEntry() }},
		{label: "Rename", mnemonic: 'r', action: func(r *Root) { r.openRename() }},
		{label: "Copy", mnemonic: 'c', action: func(r *Root) { r.copyToClipboard() }},
		{label: "Cut", mnemonic: 'x', action: func(r *Root) { r.cutToClipboard() }},
		{label: "Multiply", mnemonic: 'm', action: func(r *Root) { r.openDuplicate() }},
		// Only once there's actually something to paste — per the user's
		// own explicit request that the menu stop always showing every
		// action regardless of whether it currently means anything.
		{label: "Paste", visible: menuClipboardHasContent, action: func(r *Root) { r.pasteClipboard() }},
		{label: "Move to Trash", mnemonic: 'd', action: func(r *Root) { r.moveSelectionToTrash() }},
		{label: "Properties", mnemonic: 'i', action: func(r *Root) { r.openProperties() }},
		{label: "More actions", submenu: []menuEntry{
			{label: "tail -f", visible: menuTargetIsFile, action: func(r *Root) { r.tailCurrentEntry() }},
			{label: "chown", action: func(r *Root) { r.openChown() }},
			{label: "chmod", action: func(r *Root) { r.openChmod() }},
			{label: "sed", action: func(r *Root) { r.openSedReplace() }},
			{label: "Batch rename", action: func(r *Root) { r.openBatchRename() }},
			{label: "Undo last rename", action: func(r *Root) { r.undoLastBatchRename() }},
			// The dangerous sibling of "Move to Trash" above — kept out
			// of the top level on purpose, the same "punctual action up
			// top, consequential one a step further away" shape the
			// plain-letter keyboard layer's own d/D pair already uses.
			{label: "Remove", action: func(r *Root) { r.openRemoveConfirm() }},
			// The dereferencing sibling of "Paste" above, kept out of the
			// top level for the same reason — the keyboard layer's own
			// v/V pair uses this exact placement too (see keymap.go).
			// Same visibility gate as plain Paste: nothing to offer once
			// the clipboard is empty either way.
			{label: "Paste, following symlinks", visible: menuClipboardHasContent, action: func(r *Root) { r.pasteClipboardFollowingSymlinks() }},
		}},
		{label: "Selection", submenu: []menuEntry{
			{label: "Select all", action: func(r *Root) { r.panel.selectAll() }},
			{label: "Deselect all", action: func(r *Root) { r.panel.deselectAll() }},
			{label: "Select +", action: func(r *Root) { r.openSelectPlus() }},
			{label: "Select -", action: func(r *Root) { r.openSelectMinus() }},
		}},
		{label: "Tabs & Split", submenu: []menuEntry{
			{label: "New tab", action: func(r *Root) { r.newTabHere() }},
			{label: "Close tab", action: func(r *Root) { r.closeCurrentTab() }},
			{label: "Switch tab...", action: func(r *Root) { r.openTabSwitcher(r.activeTab) }},
			{dynamicLabel: func(r *Root) string { return splitToggleLabel(r.splitActive) }, action: func(r *Root) { r.toggleSplit() }},
			// Only once there's an actual split to orient/swap — before
			// that, neither means anything (see splitIsActive).
			{dynamicLabel: func(r *Root) string { return splitOrientationLabel(r.settings.SplitStacked) }, visible: menuSplitIsActive, action: func(r *Root) { r.toggleSplitStacked() }},
			{label: "Swap panes", visible: menuSplitIsActive, action: func(r *Root) { r.swapPanesOrExplain() }},
		}},
	}
}

// trashMenuTree replaces contextMenuTree entirely while browsing the
// Trash (see Root.inTrash) — a deliberately different, much shorter
// list rather than the same one with items conditionally hidden: almost
// nothing above still applies to an already-trashed item (there's
// nowhere to Cut/Paste/chmod/sed it to, Move to Trash makes no sense on
// something already there), so starting over with only what does apply
// reads more clearly than a heavily-pruned copy of the ordinary menu
// would. Flat, no submenu — three items needs no further grouping.
func trashMenuTree() []menuEntry {
	return []menuEntry{
		{label: "Restore from Trash", action: func(r *Root) { r.restoreSelectionFromTrash() }},
		{label: "Empty Trash", action: func(r *Root) { r.openEmptyTrashConfirm() }},
		{label: "Properties", mnemonic: 'i', action: func(r *Root) { r.openProperties() }},
	}
}

// menuTargetIsFile reports whether the row this menu was opened for
// (r.target/r.targetRow — set by the right-click that opened it, or by
// MenuShortcut for the keyboard path) is a plain file rather than a
// directory. False (hiding Edit/tail -f) for a directory, and
// defensively for a target that can no longer be found at all — editing
// or tailing something that isn't there either way.
func menuTargetIsFile(r *Root) bool {
	ref, ok := r.panel.rowRef(r.targetRow)
	return ok && !ref.isDir
}

// menuClipboardHasContent reports whether Paste currently has anything
// to do — see Root.clipboard, filled by Copy/Cut.
func menuClipboardHasContent(r *Root) bool {
	return len(r.clipboard) > 0
}

// menuSplitIsActive reports whether split view is currently showing —
// the precondition for "Split orientation"/"Swap panes" meaning
// anything at all.
func menuSplitIsActive(r *Root) bool {
	return r.splitActive
}

// currentMenuTree is whichever list renderContextMenu should actually
// draw right now: the Trash's own short list takes over unconditionally
// (see trashMenuTree's own doc comment), then whichever submenu is
// drilled into, then the ordinary top level.
func (r *Root) currentMenuTree() []menuEntry {
	if r.inTrash() {
		return trashMenuTree()
	}
	if r.menuInSubmenu != nil {
		return r.menuInSubmenu.submenu
	}
	return contextMenuTree()
}

// renderContextMenu rebuilds r.menu's rows from currentMenuTree — called
// on every open and every drill in/out, never mutated in place
// afterward. A "◂ Back" row leads the list whenever a submenu is showing
// (never for the Trash's own flat list, which has nothing to go back to
// beyond closing the menu outright).
func (r *Root) renderContextMenu() {
	r.menu.Clear()

	if r.menuInSubmenu != nil {
		r.menu.AddItem(menuBackGlyph, "", 0, r.closeMenuOrGoBack)
	}

	for _, entry := range r.currentMenuTree() {
		if entry.visible != nil && !entry.visible(r) {
			continue
		}
		if entry.submenu != nil {
			r.menu.AddItem(menuGroupGlyph+entry.resolvedLabel(r), "", 0, func() { r.enterMenuSubmenu(entry) })
			continue
		}
		r.menu.AddItem(entry.resolvedLabel(r), "", 0, func() { entry.action(r) })
	}

	r.renderContextMenuTitle()
}

// renderContextMenuTitle keeps the menu's own title bar naming where you
// are — "Menu" at the top level, "Menu › Tabs & Split" one level in —
// the same breadcrumb idea a drill-down file picker already gives for
// free, applied here since a submenu otherwise looks identical to a
// second, unrelated menu once you're inside it.
func (r *Root) renderContextMenuTitle() {
	if r.menuInSubmenu != nil {
		r.menuTitleBar.SetText(" Menu › " + r.menuInSubmenu.label + " ")
		return
	}
	r.menuTitleBar.SetText(" Menu ")
}

// enterMenuSubmenu drills into entry's own submenu, keeping the menu's
// current top-left corner rather than jumping to a new position — only
// its size changes to fit whatever the submenu actually holds.
func (r *Root) enterMenuSubmenu(entry menuEntry) {
	r.menuInSubmenu = &entry
	r.renderContextMenu()
	x, y, _, _ := r.menuLayout.GetRect()
	r.resizeContextMenu(x, y)
	r.menu.SetCurrentItem(0)
}

// closeMenuOrGoBack is the menu's own Escape/Left-arrow/Backspace
// action (see captureContextMenuKey and the SetDoneFunc wiring in
// NewRoot): one level back out of a submenu, or closes the menu
// entirely once already at the top — the same "Escape backs out one
// step at a time" shape a chord's own resolveChord already has for the
// keyboard layer, applied here to the mouse-driven menu's own drill-down.
func (r *Root) closeMenuOrGoBack() {
	if r.menuInSubmenu == nil {
		r.closeMenu()
		return
	}
	r.menuInSubmenu = nil
	r.renderContextMenu()
	x, y, _, _ := r.menuLayout.GetRect()
	r.resizeContextMenu(x, y)
	r.menu.SetCurrentItem(0)
}

// captureContextMenuKey adds Left/Backspace as a second way back out of
// a submenu, alongside Escape and clicking/selecting "◂ Back", and — at
// any level — a plain letter matching one of the currently visible
// entries' own mnemonic (see menuEntry.mnemonic) fires that entry
// directly, the same as arrowing to it and pressing Enter would.
//
// Left/Backspace: tview's own List binds Left to shifting its
// horizontal scroll offset, harmless and unused for labels this short,
// so intercepting it here only changes behavior while a submenu is
// actually showing; at the top level (where there's nothing to go back
// to) it reaches List's own default handling exactly as before.
func (r *Root) captureContextMenuKey(event *tcell.EventKey) *tcell.EventKey {
	if entry, ok := r.menuMnemonicEntry(event); ok {
		entry.action(r)
		return nil
	}

	if r.menuInSubmenu == nil {
		return event
	}
	switch event.Key() {
	case tcell.KeyLeft, tcell.KeyBackspace, tcell.KeyBackspace2:
		r.closeMenuOrGoBack()
		return nil
	}
	return event
}

// menuMnemonicEntry finds the currently visible entry, in whichever
// menu (top level or a drilled-into submenu) is showing right now,
// whose own mnemonic matches event — a plain, unmodified rune key, and
// only ever a leaf (menuEntry.action set): none of the entries this
// exists for today have a submenu of their own, and a mnemonic firing a
// submenu's own action field (nil, for a group) would do nothing
// useful anyway.
func (r *Root) menuMnemonicEntry(event *tcell.EventKey) (menuEntry, bool) {
	if event.Key() != tcell.KeyRune || event.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) != 0 {
		return menuEntry{}, false
	}
	key := event.Rune()
	for _, entry := range r.currentMenuTree() {
		if entry.mnemonic != key || entry.action == nil {
			continue
		}
		if entry.visible != nil && !entry.visible(r) {
			continue
		}
		return entry, true
	}
	return menuEntry{}, false
}

// resizeContextMenu sizes and positions menuLayout for whatever r.menu
// currently holds (see listSize) — called on the initial open and again
// every time drilling in or backing out changes the row count, always
// anchored at (x, y), the menu's own existing top-left corner once
// already open.
func (r *Root) resizeContextMenu(x, y int) {
	width, height := listSize(r.menu)
	height++ // reserved title bar row (see menuLayout)
	x, y, width, height = r.clampToPanel(x, y, width, height)
	r.menuLayout.SetRect(x, y, width, height)
	// r.menu's own rect is also set, to the same full area — a real,
	// live-confirmed gap otherwise (see
	// TestContextMenuBlocksRightDragSelection): captureOutsideClick's
	// own "did this click land on the open overlay" check reads
	// r.activeWidget.GetRect() directly, and r.activeWidget stays r.menu
	// (the real focus target — see showMenu), not menuLayout. Left
	// unset, r.menu's rect would stay at whatever tview.NewBox's own
	// uninitialized default (0, 0, 15, 10) happens to be — which
	// overlaps the panel itself — letting a click meant for the panel
	// underneath be treated as if it had landed on the menu instead.
	r.menu.SetRect(x, y, width, height)
}
