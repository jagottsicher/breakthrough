package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The plain-letter keyboard layer: breakthrough's primary keyboard
// interface for everything the file panel does.
//
// A plain character always arrives as exactly what was typed, on every
// terminal, every layout, every platform, with no multiplexer
// (tmux/screen/byobu) or window manager ever intercepting it first —
// unlike a Ctrl combination (Ctrl+punctuation in particular depends on
// what a terminal decodes it to, which varies by layout) or a function
// key (unavailable or remapped to something else on plenty of
// keyboards, macOS's own media-key row among them). Modern terminal
// file managers (ranger, nnn, lf, vifm) all reach the same conclusion.
// A tiny handful of Ctrl-letter bindings remain, for actions this layer
// itself genuinely can't cover — see cmd/breakthrough's own dispatch —
// but every function key is gone, and so is every Ctrl-letter binding
// this layer's own plain letters and chords can reach instead.
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
//     exists (d moves to the trash, D deletes for good) rather than an
//     unrelated action parked on a free key — so which is punctual and
//     which is consequential can be guessed rather than memorized. The
//     button bar always shows a curated subset of these (see
//     plainCommand.quick/buildButtonBar).
//   - Chords (chordFamilies) for related groups of weekly-or-rarer
//     actions: "g" for jumping somewhere (gg top, gh home, gr /, gb
//     trash), "p" for permissions (pm chmod, po chown), "z" for display
//     toggles (zs size format, zt time format, zo split orientation, zw
//     swap panes), "o" for Options (oo the screen itself, om mouse
//     reporting — the one Options-adjacent setting worth a direct
//     toggle without opening the screen at all). Each is a plain letter
//     followed, within chordTimeout, by one more — see resolveChord. The
//     button bar marks each family with an ellipsis ("g… go to") to show
//     it leads to more rather than acting on its own.
//   - Everything rarer still (the planned Toolbox, the notification
//     log, archive handling, ...) is meant to live behind its own
//     full-screen entry point instead of costing a keyboard slot at
//     all, the same way Options and Batch Rename already do — a screen
//     can hold an unbounded number of features; a keymap cannot.

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

	// quick marks this command for the button bar's own always-visible
	// legend (see buildButtonBar) — a curated subset, not everything:
	// one line only has room for what a sysadmin reaches for constantly,
	// and every entry is still fully documented in the help text
	// regardless of this flag.
	quick bool

	// short is the button bar's own label for a quick command — label
	// itself is often too long for a one-line bar with a dozen-plus
	// entries on it ("Remove permanently (Empty Trash, while browsing
	// the Trash)" would eat the whole row on its own). Unused, and left
	// empty, on every command that isn't quick.
	short string

	// alsoOverProperties marks a command that must keep working even
	// while Properties specifically is open (see
	// acceptsPropertiesAwareKey) — Look, the Details toggle, and the
	// Properties/Details-aware tool trio (hashes, directory size,
	// metadata) all act on "whichever of Properties/Details currently
	// applies", so Properties being the topmost overlay is one of the
	// states they need, not a reason to stand down the way every other
	// plain letter correctly does.
	alsoOverProperties bool
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
		{key: 'c', label: "Copy", quick: true, short: "Copy", action: func(r *Root) { r.copyCurrentSelection() }},
		{key: 'x', label: "Cut", quick: true, short: "Cut", action: func(r *Root) { r.cutCurrentSelection() }},
		{key: 'v', label: "Paste", quick: true, short: "Paste", action: func(r *Root) { r.pasteClipboard() }},
		{key: 'd', label: "Move to Trash", quick: true, short: "Trash", action: func(r *Root) { r.moveSelectionToTrash() }},
		{key: 'r', label: "Rename (Restore, while browsing the Trash)", action: func(r *Root) {
			if r.inTrash() {
				r.restoreSelectionFromTrash()
				return
			}
			r.renameCurrentEntry()
		}},
		{key: 'e', label: "Edit", action: func(r *Root) { r.editCurrentEntry() }},
		{key: 'f', label: "Find", action: func(r *Root) { r.openSearch() }},
		{key: '/', label: "Filter", action: func(r *Root) { r.openFilterMenu() }},
		{key: '.', label: "Toggle hidden files", quick: true, short: "Hide", action: func(r *Root) { r.toggleHidden() }},
		{key: 'i', label: "Properties", quick: true, short: "Props", action: func(r *Root) { r.propertiesCurrentEntry() }},
		{key: 'm', label: "Context menu", quick: true, short: "Menu", action: func(r *Root) { r.MenuShortcut() }},
		{key: 's', label: "Split view on/off", quick: true, short: "Split", action: func(r *Root) { r.toggleSplit() }},
		{key: 't', label: "Tab switcher", quick: true, short: "Tabs", action: func(r *Root) { r.openTabSwitcher(r.activeTab) }},
		{key: 'n', label: "New tab", action: func(r *Root) { r.newTabHere() }},
		{key: 'w', label: "Close tab", action: func(r *Root) { r.closeCurrentTab() }},
		{key: 'q', label: "Quit", action: func(r *Root) { r.RequestQuit() }},
		{key: 'a', label: "Select all", action: func(r *Root) { r.panel.selectAll() }},
		{key: 'u', label: "Undo last rename (Batch Rename's own undo — the only kind there is yet)", action: func(r *Root) { r.undoLastBatchRename() }},
		{key: 'l', label: "Look", quick: true, short: "Look", alsoOverProperties: true, action: func(r *Root) { r.lookCurrentEntry() }},
		{key: '?', label: "Help", quick: true, short: "Help", action: func(r *Root) { r.openHelp() }},
		{key: ':', label: "Bash command line", action: func(r *Root) { r.app.SetFocus(r.bashLine) }},

		// --- Ebene 2: the bigger sibling of the letter above -----------
		{key: 'D', label: "Remove permanently (Empty Trash, while browsing the Trash)", action: func(r *Root) {
			if r.inTrash() {
				r.openEmptyTrashConfirm()
				return
			}
			r.openRemoveConfirm()
		}},
		// The user's own explicit request: a way to paste a symlink
		// dereferenced — a real copy of whatever it points to, instead of
		// recreating the link itself — for both Copy- and Cut-marked
		// clipboards alike. Not quick (no button-bar slot), the same
		// treatment 'D' above already gets: this is deliberately the
		// rarer, more consequential sibling of the everyday 'v', always
		// gated behind its own confirmation dialog (see
		// pasteClipboardFollowingSymlinks) rather than a single keypress.
		{key: 'V', label: "Paste, following symlinks", action: func(r *Root) { r.pasteClipboardFollowingSymlinks() }},
		// quick despite the narrower row this leaves: the button bar is
		// the only always-present, clickable route to toggling Details
		// *while Properties is open with unsaved changes* — every other
		// button-bar click is swallowed in that state (see
		// captureOutsideClick's own carve-out, keyed on 'I' specifically),
		// so dropping this from the permanent legend would quietly cost a
		// deliberately-built mouse gesture, not just a documented one.
		{key: 'I', label: "Details sidebar", quick: true, short: "Details", alsoOverProperties: true, action: func(r *Root) { r.toggleDetailsSidebar() }},
		// The three Properties/Details-aware tools: each already targets
		// "whichever of Properties/Details currently applies" on its own
		// (see ComputeHashesShortcut/ComputeDirSizeShortcut/
		// FetchMetadataShortcut's own doc comments), and now also opens
		// Details itself first if neither is open yet — so "select
		// something, press the key" works from plain browsing too, not
		// only once one of the two windows already happens to be open.
		{key: 'h', label: "Compute hashes (Properties/Details, whichever applies)", alsoOverProperties: true, action: func(r *Root) { r.ComputeHashesShortcut() }},
		{key: 'k', label: "Compute directory size, recursively (Details)", alsoOverProperties: true, action: func(r *Root) { r.ComputeDirSizeShortcut() }},
		{key: 'M', label: "Load image metadata (Details) — not implemented yet", alsoOverProperties: true, action: func(r *Root) { r.FetchMetadataShortcut() }},
		{key: 'E', label: "Sed Replace", action: func(r *Root) { r.openSedReplace() }},
		{key: 'B', label: "Batch rename", action: func(r *Root) { r.openBatchRename() }},
		{key: 'G', label: "Go to the last row", action: func(r *Root) { r.panel.focusRow(r.panel.table.GetRowCount() - 1) }},
		{key: '+', label: "Select by pattern", action: func(r *Root) { r.openSelectPlus() }},
		{key: '-', label: "Deselect by pattern", action: func(r *Root) { r.openSelectMinus() }},
		{key: '*', label: "Invert selection", action: func(r *Root) { r.panel.invertSelection() }},
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

	// quick marks this family for the button bar's own always-visible
	// legend (see buildButtonBar/plainCommand's own quick field) — false
	// for "y", whose members are all still reserved placeholders (see
	// chordFamilies' own doc comment); advertising it in the one bar
	// that's always on screen would be advertising a feature that isn't
	// there yet.
	quick bool
}

// chordFamilies is every chord, in the fixed order the hint bar shows
// them.
//
// The "y" (yank) family is deliberately reserved rather than absent:
// copying a path or name to the *system* clipboard needs its own design
// (X11/Wayland/OSC-52-over-SSH all differ) that doesn't exist yet — see
// the project's own longer-term notes on this. Each member reports that
// plainly (see reservedYankMember) rather than silently doing nothing or
// not appearing in the hint bar at all — an absent key looks like an
// oversight; a key that explains itself does not.
func chordFamilies() []chordFamily {
	return []chordFamily{
		// Ordered to match the header row's own five nav buttons
		// (∎~↑<> — Start/Home/Up/Back/Forward, see buildHeaderSpans),
		// per the user's own explicit request to sort this family
		// properly once it grew past the original four: gg/gh/gu/gp/gn
		// step along the *same* axis those buttons do (start, home, then
		// three ways to move relative to where you already are), before
		// gr/gb — jumps to a fixed, unrelated place — close it out.
		{prefix: 'g', name: "go to", quick: true, members: []chordMember{
			{'g', "Top", func(r *Root) { r.panel.focusRow(0) }},
			{'h', "Home", func(r *Root) { r.showError(r.panel.navigate(userHomeDir())) }},
			// Mirrors actionUp's own filepath.Dir(p.path) — see its own
			// doc comment on why filepath.Dir("/") == "/" (a harmless
			// no-op at the filesystem root) needs no special-casing here
			// either.
			{'u', "Up", func(r *Root) { r.showError(r.panel.navigate(filepath.Dir(r.panel.path))) }},
			// "p"/"n" (previous/next), not "b"/"f" — "b" was already
			// spoken for by Trash below, and "back"/"forward" as
			// abbreviations read no more naturally than "previous"/
			// "next" once one of the two obvious pairs is unavailable —
			// the user's own explicit choice of wording ("go prev und go
			// next") settled it either way.
			{'p', "Back", func(r *Root) { r.panel.back() }},
			{'n', "Forward", func(r *Root) { r.panel.forward() }},
			// "/ (root)", not "Root /": a label ending in "/" sat right
			// against the single-space separator before the next member
			// (see chordHintBar), reading as if the "/" were part of that
			// separator rather than this label's own content — the user's
			// own explicit report. Leading "/" instead doesn't have that
			// problem: it's preceded by "r"'s own highlight box, never by
			// a bare separator space, so there's nothing for it to be
			// mistaken for. Matches the help text's own existing phrasing
			// ("gr / (root)" — see help.go).
			{'r', "/ (root)", func(r *Root) { r.showError(r.panel.navigate("/")) }},
			{'b', "Trash", func(r *Root) { r.openTrash() }},
		}},
		{prefix: 'p', name: "perms", quick: true, members: []chordMember{
			{'m', "chmod", func(r *Root) { r.openChmod() }},
			{'o', "chown", func(r *Root) { r.openChown() }},
		}},
		{prefix: 'z', name: "display", quick: true, members: []chordMember{
			{'s', "Size format", func(r *Root) { r.toggleSizeBytes() }},
			{'t', "Time format", func(r *Root) { r.toggleMtimeUnix() }},
			{'o', "Split orientation", func(r *Root) { r.toggleSplitStacked() }},
			{'w', "Swap panes", func(r *Root) { r.swapPanesOrExplain() }},
			{'r', "Reload", func(r *Root) { r.reloadCurrentTab() }},
		}},
		{prefix: 'y', name: "yank (reserved — no system clipboard yet)", members: []chordMember{
			{'p', "Copy full path", reservedYankMember("Copy full path")},
			{'n', "Copy name", reservedYankMember("Copy name")},
			{'a', "Copy all selected paths", reservedYankMember("Copy all selected paths")},
		}},
		// "oo" doubles the prefix for "the family's own main destination",
		// the same shape "gg" (go to » top) already established — opening
		// the Options screen itself. "om" is the one Options-adjacent
		// setting worth a direct toggle without opening the screen at all
		// (see toggleMouseReporting's own doc comment on why "z" —
		// display — wasn't the right fit for it either).
		{prefix: 'o', name: "options", quick: true, members: []chordMember{
			{'o', "Options screen", func(r *Root) { r.openOptions() }},
			{'m', "Mouse reporting", func(r *Root) { r.toggleMouseReporting() }},
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

// chordTimeout is how long a chord's second key stays live for —
// settings.ChordTimeoutMS (see its own doc comment in config/settings.go),
// a live setting rather than the fixed constant this used to be, so a
// change under Options takes effect on the very next chord without a
// restart. 4000ms by default, the same value that was hardcoded before
// this setting existed: long enough to actually read the button bar's
// own legend first — a family like "g" lists four members with a label
// each, and the first value tried here (2.5s) turned out too short to
// read that before choosing, never mind reach for the key — short
// enough that an abandoned chord (the user got distracted, or simply
// changed their mind) doesn't sit waiting indefinitely for a keystroke
// that might arrive minutes later and mean something completely
// different by then.
func (r *Root) chordTimeout() time.Duration {
	return time.Duration(r.settings.ChordTimeoutMS) * time.Millisecond
}

// chordTickInterval is how often the status bar's own countdown
// indicator (see chordIndicatorText) is repainted while a chord is
// pending — frequent enough that the shrinking bar reads as continuous
// motion, infrequent enough that it costs nothing over a slow SSH link
// (the same tradeoff this app's other progress animations already
// make; see hashAnimationInterval).
const chordTickInterval = 150 * time.Millisecond

// HandlePlainKey is offered every key before cmd/breakthrough's own
// switch sees it — reports whether it consumed the key.
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

	key := event.Rune()

	// Checked before the ordinary gate below, not as a fallback after
	// it fails: a command marked alsoOverProperties is meant to fire in
	// either state, and acceptsPropertiesAwareKey's own first branch is
	// exactly acceptsPlainKeyCommand again, so nothing is skipped by
	// checking this one first.
	if cmd, ok := plainCommandFor(key); ok && cmd.alsoOverProperties && r.acceptsPropertiesAwareKey() {
		cmd.action(r)
		return true
	}

	if !r.acceptsPlainKeyCommand() {
		return false
	}
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

// acceptsPropertiesAwareKey reports whether a plainCommand marked
// alsoOverProperties should act right now: the ordinary
// acceptsPlainKeyCommand rule (a command marked this way still needs to
// fire from plain browsing too, not only from inside Properties), or,
// in addition, while Properties specifically is open and its own
// text-editing field doesn't currently have real keyboard focus —
// typing "h" into the Name field must insert a letter, not launch hash
// computation. The same "Properties is exactly one of the states this
// needs to keep working in" reasoning ComputeHashesShortcut and
// ToggleDetailsSidebarShortcut's own doc comments already established
// for Ctrl+K/Ctrl+D, before either had a plain-letter home.
func (r *Root) acceptsPropertiesAwareKey() bool {
	if r.acceptsPlainKeyCommand() {
		return true
	}
	return r.activePage == propertiesPage && !r.propertiesEditField.HasFocus()
}

// startChord puts the application into chord mode: the button bar
// becomes family's own legend (see chordHintBar) and the status bar
// starts counting the timeout down.
func (r *Root) startChord(family chordFamily) {
	r.pendingChord = family.prefix
	r.chordDeadline = time.Now().Add(r.chordTimeout())

	ctx, cancel := context.WithCancel(context.Background())
	r.chordCancel = cancel

	text, spans := r.chordHintBar(family)
	r.buttonBarSpans = spans
	r.buttonBar.SetText(text)
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
	elapsedFrac := 1 - float64(remaining)/float64(r.chordTimeout())
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
// overlay, is what's on screen the moment a chord starts — and, per the
// user's own explicit request, makes every member (and "Esc cancel")
// clickable with the mouse too, exactly like the ordinary button bar
// (see buildButtonBar/captureButtonBarMouse, which reads whichever
// spans are currently in r.buttonBarSpans without caring whether they
// came from here or there).
//
// Each member's own resolving key is set off with a space on either
// side, colored with the same ButtonBackground every real button in
// this app already uses (see styleButton) — a visual echo of "this is
// the key you press", making the letter-to-action mapping easier to
// scan than plain text alongside a label would be. The label itself
// follows directly after that trailing space with no space of its own
// — the same "exactly one space between key and label" rule
// buildButtonBar's own highlightKey follows, per the user's own
// explicit request that both read the same way.
//
// "Esc cancel" gets the same highlight-then-label idea on "Esc" itself,
// per the user's own explicit follow-up requests — not just single
// letters: "Esc" is still "the key you press" in exactly the same sense
// a single letter is, three characters or not. Unlike the members
// above, though, "Esc" carries no padding space of its own at all
// (colored background directly against "Esc", then straight into
// "cancel" with nothing between them) — the user's own explicit,
// separate request to drop even that: the color alone is enough to set
// "Esc" apart from "cancel" without also needing a space to do it.
//
// A single plain space separates the prefix's own "…" from the first
// member, and one member from the next — per the user's own explicit
// clarification, deliberately its own separator, not the same thing as
// each member's own leading highlight space (which the user considers
// part of that member's own highlight, not inter-member spacing at
// all), the same "one space after, always" rule buildButtonBar's own
// quick legend follows. "Esccancel" gets the heavier " │ " (matching
// buildButtonBar's own blockSep) instead of that plain space, marking it
// as behaving differently from an ordinary member: it cancels the whole
// chord rather than resolving it, the same distinction blockSep draws
// around a chord-family cascade cell on the row above this one.
func (r *Root) chordHintBar(family chordFamily) (text string, spans []buttonBarSpan) {
	var b strings.Builder
	col := 0
	write := func(s string) {
		b.WriteString(s)
		col += tview.TaggedStringWidth(s)
	}

	write(fmt.Sprintf("%c… ", family.prefix))

	keyBG := colorTag(r.theme.ButtonBackground)
	for i, m := range family.members {
		if i > 0 {
			write(" ")
		}
		start := col
		write(fmt.Sprintf("[:%s:] %c [-:-:-]%s", keyBG, m.key, m.label))
		member := m // per-iteration copy; Go 1.22+ already gives range vars
		// this, but explicit here since the closure outlives the loop
		spans = append(spans, buttonBarSpan{
			startCol: start, endCol: col, key: member.key,
			run: func(r *Root) {
				r.cancelChord() // same "clear state before running" order resolveChord uses
				member.action(r)
			},
		})
	}

	write(" │ ")
	escStart := col
	write(fmt.Sprintf("[:%s:]Esc[-:-:-]cancel", keyBG))
	spans = append(spans, buttonBarSpan{
		startCol: escStart, endCol: col,
		run: func(r *Root) { r.cancelChord() },
	})

	return b.String(), spans
}
