package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
)

// The plain-letter keyboard layer: breakthrough's primary keyboard
// interface, alongside (not replacing) every existing Ctrl/F-key
// shortcut.
//
// Why this exists at all: Ctrl+_ (the previous prefix key) turned out
// not to work on every keyboard layout — Ctrl+punctuation depends on
// what a terminal decodes Ctrl+Shift+that-key's-unshifted-form to,
// which varies by layout and terminal in a way plain characters never
// do. Looking for a better single Ctrl combination misses the actual
// finding: the panel consumes exactly one bare key today (Space,
// selection) — tview.Table's own h/j/k/l/g/G vim bindings aside — so the
// entire letter alphabet was sitting unused in the one input layer that
// has zero portability risk: a plain character always arrives as
// exactly what was typed, on every terminal, every layout, every
// platform, with no multiplexer (tmux/screen/byobu) or window manager
// ever intercepting it first. Modern terminal file managers (ranger,
// nnn, lf, vifm) all reach the same conclusion.
//
// Only fires while the panel's own table has real keyboard focus (see
// acceptsPlainKeyCommand) — never while typing in the filter box, the
// header's path-edit field, the bash line, or with any overlay open —
// so it costs nothing to anyone editing text at the time.
//
// Three tiers, mirroring how often each thing is actually done:
//
//   - Single letters (plainCommands) for the everyday verbs: copy, cut,
//     paste, rename, edit, and so on. A capital letter is deliberately
//     the "bigger sibling" of its own lowercase counterpart wherever one
//     exists (d moves to the trash, D deletes for good; s splits the
//     view, S swaps the two panes) rather than an unrelated action
//     parked on a free key — so which is punctual and which is
//     consequential can be guessed rather than memorized.
//   - Chords (chordFamilies) for related groups of weekly-or-rarer
//     actions: "g" for jumping somewhere (gg top, gh home, gr /, gb
//     trash), "p" for permissions (pm chmod, po chown), "z" for display
//     toggles (zs size format, zt time format, zo split orientation).
//     Each is a plain letter followed, within chordTimeout, by one more
//     — see resolveChord.
//   - Everything rarer still (the planned Toolbox, the notification
//     log, archive handling, ...) is meant to live behind its own
//     full-screen entry point instead of costing a keyboard slot at
//     all, the same way Options and Batch Rename already do — a screen
//     can hold an unbounded number of features; a keymap cannot.
//
// Nothing existing is removed. Every Ctrl-letter and function-key
// binding in cmd/breakthrough's own dispatch keeps working exactly as
// it did — this is a second, primary layer added alongside it, not a
// replacement, so muscle memory built on the old bindings still works.

// acceptsPlainKeyCommand reports whether a bare letter should be read as
// a command right now.
//
// Deliberately stricter than acceptsGlobalShortcut (which only checks
// activePage and bashLine): the filter box and the header's own
// path-edit field both live *inside* the panel rather than as a
// separate overlay, so activePage alone can't tell them apart from the
// table itself having focus. Typing "budget" into the filter box must
// never have its "d" delete something.
func (r *Root) acceptsPlainKeyCommand() bool {
	return r.acceptsGlobalShortcut() && r.panel.table.HasFocus()
}

// plainCommand is one single-letter entry in the primary keyboard layer.
type plainCommand struct {
	key    rune
	label  string
	action func(r *Root)
}

// plainCommands is the whole single-letter layer, in one place —
// dispatch (see HandlePlainKey), the collision test
// (TestNoDuplicatePlainKeyBindings), and the help text all read this
// same list, so they cannot silently drift apart from each other the
// way five hand-maintained copies of a keymap otherwise do.
//
// Every action here is either an existing method already used by a
// Ctrl-letter/F-key/context-menu equivalent (kept byte-for-byte, so
// behaviour doesn't fork between the old and new path to the same
// feature) or a small new wrapper documented at its own definition.
func plainCommands() []plainCommand {
	return []plainCommand{
		// --- Ebene 1: everyday verbs -----------------------------------
		{'c', "Copy", func(r *Root) { r.copyCurrentSelection() }},
		{'x', "Cut", func(r *Root) { r.cutCurrentSelection() }},
		{'v', "Paste", func(r *Root) { r.pasteClipboard() }},
		{'d', "Move to Trash", func(r *Root) { r.moveSelectionToTrash() }},
		{'r', "Rename (Restore, while browsing the Trash)", func(r *Root) {
			if r.inTrash() {
				r.restoreSelectionFromTrash()
				return
			}
			r.renameCurrentEntry()
		}},
		{'e', "Edit", func(r *Root) { r.editCurrentEntry() }},
		{'f', "Find", func(r *Root) { r.openSearch() }},
		{'/', "Filter", func(r *Root) { r.app.SetFocus(r.panel.filterField) }},
		{'.', "Toggle hidden files", func(r *Root) { r.toggleHidden() }},
		{'i', "Properties", func(r *Root) { r.propertiesCurrentEntry() }},
		{'m', "Context menu", func(r *Root) { r.MenuShortcut() }},
		{'s', "Split view on/off", func(r *Root) { r.toggleSplit() }},
		{'t', "Tab switcher", func(r *Root) { r.openTabSwitcher(r.activeTab) }},
		{'n', "New tab", func(r *Root) { r.newTabHere() }},
		{'w', "Close tab", func(r *Root) { r.closeCurrentTab() }},
		{'q', "Quit", func(r *Root) { r.RequestQuit() }},
		{'a', "Select all", func(r *Root) { r.panel.selectAll() }},
		{'u', "Undo last rename (Batch Rename's own undo — the only kind there is yet)", func(r *Root) { r.undoLastBatchRename() }},
		{'l', "Look", func(r *Root) { r.lookCurrentEntry() }},
		{'?', "Help", func(r *Root) { r.openHelp() }},
		{':', "Bash command line", func(r *Root) { r.app.SetFocus(r.bashLine) }},

		// --- Ebene 2: the bigger sibling of the letter above -----------
		{'D', "Remove permanently (Empty Trash, while browsing the Trash)", func(r *Root) {
			if r.inTrash() {
				r.openEmptyTrashConfirm()
				return
			}
			r.openRemoveConfirm()
		}},
		{'I', "Details sidebar", func(r *Root) { r.toggleDetailsSidebar() }},
		{'E', "Sed Replace", func(r *Root) { r.openSedReplace() }},
		{'S', "Swap panes", func(r *Root) { r.swapPanesOrExplain() }},
		{'B', "Batch rename", func(r *Root) { r.openBatchRename() }},
		{'G', "Go to the last row", func(r *Root) { r.panel.focusRow(r.panel.table.GetRowCount() - 1) }},
		{'+', "Select by pattern", func(r *Root) { r.openSelectPlus() }},
		{'-', "Deselect by pattern", func(r *Root) { r.openSelectMinus() }},
		{'*', "Invert selection", func(r *Root) { r.panel.invertSelection() }},
	}
}

// plainCommandFor finds the command bound to key, if any.
func plainCommandFor(key rune) (plainCommand, bool) {
	for _, c := range plainCommands() {
		if c.key == key {
			return c, true
		}
	}
	return plainCommand{}, false
}

// copyCurrentSelection and cutCurrentSelection sync r.target to the
// cursor before delegating to copyToClipboard/cutToClipboard.
//
// A real gap found while wiring this layer up, not a defensive
// afterthought: those two read the checkbox selection, falling back to
// r.target if nothing is checked (see clipboardTargets) — and until
// now, r.target was only ever set by a right-click or by one of the
// *CurrentEntry functions (which already do this same sync inline, see
// renameCurrentEntry/propertiesCurrentEntry). Neither Copy nor Cut had
// a Ctrl-letter or F-key binding before this layer, so nothing had ever
// called them from a bare keypress with no prior right-click — pressing
// "c" on the very first file of a fresh session would have found
// r.target == "" and copied nothing, silently.
func (r *Root) copyCurrentSelection() {
	if row, path, ok := r.panel.CurrentRowPath(); ok {
		r.target, r.targetRow = path, row
	}
	r.copyToClipboard()
}

func (r *Root) cutCurrentSelection() {
	if row, path, ok := r.panel.CurrentRowPath(); ok {
		r.target, r.targetRow = path, row
	}
	r.cutToClipboard()
}

// chordMember is one second key within a chord family.
type chordMember struct {
	key    rune
	label  string
	action func(r *Root)
}

// chordFamily is a prefix letter and everything it can be followed by.
type chordFamily struct {
	prefix  rune
	name    string
	members []chordMember
}

// chordFamilies is every chord, in the fixed order the hint bar shows
// them.
//
// The "y" (yank) family is deliberately reserved rather than absent:
// copying a path or name to the *system* clipboard needs its own design
// (X11/Wayland/OSC-52-over-SSH all differ) that doesn't exist yet — see
// the project's own longer-term notes on this. Each member reports that
// plainly, the same way placeholderMenuAction already does for a
// context-menu entry ahead of its real feature, rather than silently
// doing nothing or not appearing in the hint bar at all — an absent key
// looks like an oversight; a key that explains itself does not.
func chordFamilies() []chordFamily {
	return []chordFamily{
		{prefix: 'g', name: "go", members: []chordMember{
			{'g', "Top", func(r *Root) { r.panel.focusRow(0) }},
			{'h', "Home", func(r *Root) { r.showError(r.panel.navigate(userHomeDir())) }},
			{'r', "Root /", func(r *Root) { r.showError(r.panel.navigate("/")) }},
			{'b', "Trash", func(r *Root) { r.openTrash() }},
		}},
		{prefix: 'p', name: "permissions", members: []chordMember{
			{'m', "chmod", func(r *Root) { r.openChmod() }},
			{'o', "chown", func(r *Root) { r.openChown() }},
		}},
		{prefix: 'z', name: "display", members: []chordMember{
			{'s', "Size format", func(r *Root) { r.toggleSizeBytes() }},
			{'t', "Time format", func(r *Root) { r.toggleMtimeUnix() }},
			{'o', "Split orientation", func(r *Root) { r.toggleSplitStacked() }},
		}},
		{prefix: 'y', name: "yank (reserved — no system clipboard yet)", members: []chordMember{
			{'p', "Copy full path", reservedYankMember("Copy full path")},
			{'n', "Copy name", reservedYankMember("Copy name")},
			{'a', "Copy all selected paths", reservedYankMember("Copy all selected paths")},
		}},
	}
}

// reservedYankMember is a placeholder for one of the "y" family's
// members — see chordFamilies' own doc comment on why the family exists
// before the feature behind it does. Takes no receiver itself (it's a
// package-level helper building a closure), since chordFamilies() is
// itself a plain function with no *Root of its own to call a method on.
func reservedYankMember(name string) func(r *Root) {
	return func(r *Root) {
		r.showError(fmt.Errorf("%s: not implemented yet — reserved for a future system-clipboard feature", name))
	}
}

// chordFamilyFor finds the chord family led by prefix, if any.
func chordFamilyFor(prefix rune) (chordFamily, bool) {
	for _, f := range chordFamilies() {
		if f.prefix == prefix {
			return f, true
		}
	}
	return chordFamily{}, false
}

// userHomeDir is os.UserHomeDir with its error swallowed into an empty
// string — navigate("") then fails on its own and the resulting error
// reaches the user via showError exactly the same way any other
// navigation failure does (see the "g h" chord member above), so there
// is nothing this needs to do differently just because the failure
// happened one function earlier.
func userHomeDir() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return dir
}

// chordTimeout is how long a chord's second key stays live for.
//
// Long enough that reaching for it doesn't feel rushed, short enough
// that an abandoned chord (the user got distracted, or simply changed
// their mind) doesn't sit waiting indefinitely for a keystroke that
// might arrive minutes later and mean something completely different by
// then.
const chordTimeout = 2500 * time.Millisecond

// chordTickInterval is how often the status bar's own countdown
// indicator (see chordIndicatorText) is repainted while a chord is
// pending — frequent enough that the shrinking bar reads as continuous
// motion, infrequent enough that it costs nothing over a slow SSH link
// (the same tradeoff this app's other progress animations already
// make; see hashAnimationInterval).
const chordTickInterval = 150 * time.Millisecond

// HandlePlainKey is offered every key before cmd/breakthrough's own
// switch sees it (the same position Root.HandlePrefixKey already has
// for the legacy Ctrl+_ system) — reports whether it consumed the key.
//
// Two entirely different jobs depending on state: if a chord is
// currently waiting on its second key, every key that arrives resolves
// or cancels it (see resolveChord) — nothing falls through, since a
// stray keystroke reaching the panel underneath while a chord thinks
// it's still listening would be far more surprising than a single
// consumed keypress. Otherwise, only a bare, unmodified letter is
// looked up at all, and only while acceptsPlainKeyCommand allows it;
// anything else (Ctrl/Alt combinations, function keys, arrows, Tab, or
// simply a letter with no binding) is left untouched for the rest of
// the dispatch chain exactly as it already was before this layer
// existed.
func (r *Root) HandlePlainKey(event *tcell.EventKey) bool {
	if r.pendingChord != 0 {
		return r.resolveChord(event)
	}

	if event.Key() != tcell.KeyRune || event.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) != 0 {
		return false
	}
	if !r.acceptsPlainKeyCommand() {
		return false
	}

	key := event.Rune()
	if family, ok := chordFamilyFor(key); ok {
		r.startChord(family)
		return true
	}
	if cmd, ok := plainCommandFor(key); ok {
		cmd.action(r)
		return true
	}
	return false
}

// startChord puts the application into chord mode: the button bar
// becomes family's own legend (see chordHintBar, the same "swap the
// button row for a legend" shape the Ctrl+_ prefix's own
// showPrefixHint already established) and the status bar starts
// counting the timeout down.
func (r *Root) startChord(family chordFamily) {
	r.pendingChord = family.prefix
	r.chordDeadline = time.Now().Add(chordTimeout)

	ctx, cancel := context.WithCancel(context.Background())
	r.chordCancel = cancel

	r.buttonBarSpans = nil
	r.buttonBar.SetText(chordHintBar(family))
	r.refreshStatusBar()

	// safeGo, the same as every other background animation in this
	// package: a panic in the ticker should end the chord cleanly, not
	// take the whole process down silently having left the button bar
	// stuck showing a legend for a chord that no longer exists.
	r.safeGo("chord timeout indicator", func() { r.cancelChord() }, func() {
		r.animateChordCountdown(ctx)
	})
}

// resolveChord is HandlePlainKey's own body while a chord is pending —
// see its own doc comment for why every key is consumed here, none
// falls through.
//
// Escape and an unrecognized second key both cancel, but say different
// things about it: Escape is a deliberate "never mind" and gets no
// further comment, the same as it silently closes every other overlay
// in this app; a real but unbound key is worth naming, the same
// "explain rather than fail silently" rule the Ctrl+_ prefix's own
// unknown-verb case already follows — a key that looked like it should
// have done something and didn't is exactly the situation that
// deserves an explanation instead of nothing at all.
func (r *Root) resolveChord(event *tcell.EventKey) bool {
	family, _ := chordFamilyFor(r.pendingChord)
	r.cancelChord() // clear the pending state and its own UI first, so
	// whatever the member action does next (most open an overlay of
	// their own) isn't fighting leftover chord chrome.

	switch {
	case event.Key() == tcell.KeyEscape:
		return true
	case event.Key() != tcell.KeyRune || event.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) != 0:
		return true // not a letter at all — treat it the same as Escape
	}

	for _, m := range family.members {
		if m.key == event.Rune() {
			m.action(r)
			return true
		}
	}
	r.showError(fmt.Errorf("%c%c is not a command — see \"?\" for help", family.prefix, event.Rune()))
	return true
}

// cancelChord ends chord mode, however it ended: resolved, explicitly
// cancelled (Escape or an unbound key), or timed out. Restores the
// ordinary button bar and clears the status bar's own countdown.
func (r *Root) cancelChord() {
	if r.chordCancel != nil {
		r.chordCancel()
		r.chordCancel = nil
	}
	r.pendingChord = 0
	r.chordDeadline = time.Time{}
	r.refreshButtonBar()
	r.refreshStatusBar()
}

// animateChordCountdown repaints the status bar every chordTickInterval
// until the chord resolves, is cancelled, or its own deadline passes —
// the same ticker-driven animation shape as
// animateDetailsHashProgress/animateSedPreviewProgress, just measured
// against a deadline instead of counting up indefinitely.
//
// A timeout found here — time.Now().After(r.chordDeadline) — ends the
// chord silently (see cancelChord), deliberately without a message:
// letting a chord lapse is "I changed my mind", not a mistake worth
// commenting on, and the countdown visibly running out is itself the
// only feedback that decision needs.
func (r *Root) animateChordCountdown(ctx context.Context) {
	ticker := time.NewTicker(chordTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			r.app.QueueUpdateDraw(func() {
				if ctx.Err() != nil {
					return
				}
				if time.Now().After(r.chordDeadline) {
					r.cancelChord()
					return
				}
				r.refreshStatusBar()
			})
		case <-ctx.Done():
			return
		}
	}
}

// chordCountdownBlocks are the eight levels chordIndicatorText steps
// through as a pending chord's own time runs out — full block down to
// the thinnest sliver (U+2588 through U+2581), the same "meter draining
// downward" glyph set a battery or signal indicator uses, per the
// user's own explicit request.
var chordCountdownBlocks = []rune{'█', '▇', '▆', '▅', '▄', '▃', '▂', '▁'}

// chordIndicatorText renders the status bar's own leading segment while
// a chord is pending — "" the rest of the time, so buildStatusBar's own
// call site can simply skip a separator when this is empty rather than
// needing its own branch.
//
// Named together with the waiting prefix letter ("g▆"), not the block
// alone: the block by itself says "something is running out" without
// saying what, which is the detail that actually matters here — the
// button bar's own legend (see chordHintBar) already answers "what can
// I press", so this only has to answer "how long do I still have".
func (r *Root) chordIndicatorText() string {
	if r.pendingChord == 0 {
		return ""
	}
	remaining := time.Until(r.chordDeadline)
	if remaining <= 0 {
		return ""
	}
	// elapsedFrac, not remaining/chordTimeout directly: chordCountdownBlocks
	// runs full-to-empty (index 0 is '█'), and the block should read
	// "full" the instant the chord starts (elapsed ≈ 0) and "empty" as
	// the deadline approaches (elapsed ≈ chordTimeout) — the inverse of
	// what remaining time would index into. Using remaining directly
	// here was a real bug, caught live: the indicator showed nearly
	// empty right after pressing the prefix key, and nearly full right
	// before it expired.
	elapsedFrac := 1 - float64(remaining)/float64(chordTimeout)
	idx := int(elapsedFrac * float64(len(chordCountdownBlocks)))
	if idx >= len(chordCountdownBlocks) {
		idx = len(chordCountdownBlocks) - 1
	}
	if idx < 0 {
		idx = 0
	}
	return fmt.Sprintf("%c%c", r.pendingChord, chordCountdownBlocks[idx])
}

// chordHintBar renders the button bar's own legend for one chord family
// — what "?" cannot show here since the button row, not the help
// overlay, is what's on screen the moment a chord starts.
func chordHintBar(family chordFamily) string {
	cells := make([]string, 0, len(family.members))
	for _, m := range family.members {
		cells = append(cells, fmt.Sprintf("%c %s", m.key, m.label))
	}
	return fmt.Sprintf(" %c…  %s  │  Esc cancel", family.prefix, strings.Join(cells, "   "))
}
