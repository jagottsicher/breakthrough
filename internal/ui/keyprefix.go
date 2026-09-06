package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// The key prefix: Ctrl+_ followed by one letter, the way tmux, screen
// and emacs all reach commands that don't fit on a modifier key of
// their own.
//
// This exists because the single-key namespace is genuinely exhausted.
// Of 26 Ctrl+letter combinations, four are structurally unavailable in
// a terminal (Ctrl+I is Tab, Ctrl+M is Enter, Ctrl+H is Backspace,
// Ctrl+[ is Escape — the same byte, no terminal can tell them apart),
// sixteen are already bound here, and the handful left are all real
// readline bindings the embedded command line needs. Continuing to hunt
// for a spare letter per feature had stopped being a strategy and
// become scavenging.
//
// Ctrl+_ specifically, for three reasons: no terminal multiplexer
// claims it (tmux takes Ctrl+B, screen and byobu take Ctrl+A, dtach and
// abduco take Ctrl+\ — and a multiplexer always wins, since it
// intercepts its own prefix before the application inside ever sees the
// key), tcell decodes it as a distinct key rather than folding it into
// something else (verified against a real terminal, not assumed), and
// it costs no binding this application actually uses.
//
// Two deliberate design choices, both of which the usual criticism of
// modal key handling turns on:
//
//   - The prefix does nothing on its own. That is what makes a timeout
//     unnecessary: there is no "did they mean the prefix alone, or the
//     start of a chord" question to resolve by waiting, which is the
//     single most irritating property of modal terminal UIs (the same
//     ambiguity that makes Escape-versus-Alt so unpleasant elsewhere).
//     Pressing it puts the application in prefix mode until a key
//     arrives, however long that takes.
//   - The button bar becomes a legend the moment prefix mode starts
//     (see prefixHintBar), listing every verb. Without that, a prefix
//     tree is a memorization exercise; with it, it is discoverable in
//     the same way this application's context menu already makes
//     features discoverable. This is not an optional nicety — it is
//     what makes the whole approach defensible.
//
// Nothing is taken away: every F-key and every existing Ctrl binding
// keeps working. This is a second path, added because a MacBook's
// function keys are media keys by default and a Mac user would
// otherwise have to reconfigure their keyboard to reach a feature.

// prefixVerb is one entry in the tree: the key that triggers it, the
// short label the hint bar shows, and what it does.
type prefixVerb struct {
	key    rune
	label  string
	action func(*Root)
}

// prefixGroup is a labelled run of verbs, purely for how the hint bar
// is laid out — the grouping has no behavioural meaning.
type prefixGroup struct {
	verbs []prefixVerb
}

// prefixTree is the whole tree, in hint-bar order.
//
// Deliberately flat — prefix, then exactly one key, never a third
// level. Twenty-six letters is far more room than this will need, and
// every extra level costs the user a keystroke and a decision on every
// single use.
//
// Verb letters are mnemonic where a sensible letter was free, and the
// F-key equivalents are all present, which is the point: this is a
// complete alternative route to everything the function keys reach.
func prefixTree() []prefixGroup {
	return []prefixGroup{
		{verbs: []prefixVerb{
			{'s', "Split", func(r *Root) { r.toggleSplit() }},
			{'o', "Orient", func(r *Root) { r.toggleSplitStacked() }},
			// "Switch pane", not "otherPane": the label is the only
			// thing standing between a user and finding this at all, and
			// the first wording said what the pane *is* rather than what
			// the key *does*. Someone looking for a way to switch panes
			// read straight past it — reported.
			{'p', "Switch pane", func(r *Root) { r.switchPaneOrExplain() }},
			{'d', "Details", func(r *Root) { r.toggleDetailsSidebar() }},
		}},
		{verbs: []prefixVerb{
			{'t', "Tabs", func(r *Root) { r.openTabSwitcher(r.activeTab) }},
			{'n', "New tab", func(r *Root) { r.newTabHere() }},
			{'w', "Close tab", func(r *Root) { r.closeCurrentTab() }},
		}},
		{verbs: []prefixVerb{
			// renameCurrentEntry, not openRename: the latter renames
			// r.target/r.targetRow, which only a right-click sets, so
			// reaching it from the keyboard aimed at whatever was last
			// right-clicked — or, on a fresh start, at row 0, the ".."
			// entry. A real bug; see the regression test.
			{'r', "Rename", func(r *Root) { r.renameCurrentEntry() }},
			{'m', "Mouse", func(r *Root) { r.ToggleMouseShortcut() }},
			{',', "Options", func(r *Root) { r.openOptions() }},
			{'?', "Help", func(r *Root) { r.openHelp() }},
		}},
	}
}

// prefixVerbFor finds the verb bound to key, if any.
//
// Digits are handled separately by the caller (see handlePrefixKey):
// they jump straight to a tab by number, which would mean ten near-
// identical entries in the tree above and ten more cells in an already
// crowded hint bar, for one rule that states itself in four characters.
func prefixVerbFor(key rune) (prefixVerb, bool) {
	for _, group := range prefixTree() {
		for _, verb := range group.verbs {
			if verb.key == key {
				return verb, true
			}
		}
	}
	return prefixVerb{}, false
}

// prefixHintBar renders the legend shown while prefix mode is active —
// what replaces the ordinary button bar (see refreshButtonBar).
//
// Groups are separated the same way the button bar's own entries are,
// so the row still reads as one bar rather than as a different kind of
// thing that happened to appear in the same place.
func prefixHintBar() string {
	var groups []string
	for _, group := range prefixTree() {
		var cells []string
		for _, verb := range group.verbs {
			cells = append(cells, fmt.Sprintf("%c %s", verb.key, verb.label))
		}
		groups = append(groups, strings.Join(cells, "  "))
	}
	// The digit rule and the way out, both stated rather than left to be
	// discovered: a mode with no visible exit is the other half of what
	// makes modal interfaces unpleasant.
	groups = append(groups, "1-0 Tab N", "Esc cancel")
	return " ^_  " + strings.Join(groups, " │ ")
}

// startPrefix enters prefix mode: the next key is read as a verb.
//
// Refuses while the command line has focus — that line needs its own
// keys for readline-style editing, and every other global shortcut here
// stands down in the same situation. Also refuses while an overlay is
// open: the verbs act on the panel and its layout, and half of them
// would make no sense layered under a dialog.
func (r *Root) startPrefix() {
	if !r.acceptsGlobalShortcut() {
		return
	}
	r.prefixActive = true
	r.showPrefixHint()
}

// cancelPrefix leaves prefix mode without running anything, restoring
// the ordinary button bar.
func (r *Root) cancelPrefix() {
	if !r.prefixActive {
		return
	}
	r.prefixActive = false
	r.refreshButtonBar()
}

// showPrefixHint swaps the button bar for the verb legend. Goes
// straight to the widget rather than through refreshButtonBar, whose
// job is the opposite one — rebuilding the ordinary bar from the
// application's own state.
//
// Clears buttonBarSpans at the same time, so a click on the legend
// can't land on whatever button happened to occupy those columns a
// moment ago (see handleButtonBarClick).
func (r *Root) showPrefixHint() {
	r.buttonBarSpans = nil
	r.buttonBar.SetText(prefixHintBar())
}

// handlePrefixKey is offered every key while prefix mode might be
// active, before the ordinary shortcut dispatch sees it (see
// cmd/breakthrough). Reports whether it consumed the key.
//
// Consuming *every* key in prefix mode, not just the bound ones, is
// deliberate: a verb that also happens to be a global shortcut must not
// fire both, and an unrecognized key should leave the mode rather than
// silently falling through to do something else entirely — which would
// be the same key meaning two different things depending on state the
// user may have forgotten they were in.
func (r *Root) handlePrefixKey(event *tcell.EventKey) bool {
	if !r.prefixActive {
		return false
	}

	// Leaving the mode first, in every branch: a verb can open an
	// overlay, and the hint bar must already be gone by then rather than
	// still advertising keys that no longer apply.
	r.prefixActive = false
	r.refreshButtonBar()

	if event.Key() == tcell.KeyEscape {
		return true
	}
	if event.Key() != tcell.KeyRune {
		// A non-printing key (an arrow, a function key, Enter) is not a
		// verb and never will be; treat it as a cancel rather than
		// guessing.
		return true
	}

	key := event.Rune()
	if key >= '0' && key <= '9' {
		n := int(key - '0')
		if n == 0 {
			n = 10 // ...0 is the tenth tab, continuing the row of digits
		}
		r.switchToTab(n - 1)
		return true
	}

	verb, ok := prefixVerbFor(key)
	if !ok {
		// Named rather than silent: at this point the user pressed the
		// prefix on purpose and then something that isn't in the tree,
		// which is worth one line of feedback — the hint bar they were
		// just looking at has already gone.
		r.showError(fmt.Errorf("%s is not a prefix command — press Ctrl+_ again to see the list", string(key)))
		return true
	}
	verb.action(r)
	return true
}

// PrefixShortcut is Ctrl+_'s own action (see cmd/breakthrough).
//
// Pressing it while already in prefix mode leaves the mode, so the same
// key is both the way in and a way back out — the same "press it again"
// behaviour the tab switcher's own key already has.
func (r *Root) PrefixShortcut() {
	if r.prefixActive {
		r.cancelPrefix()
		return
	}
	r.startPrefix()
}

// HandlePrefixKey is handlePrefixKey's exported form, called by
// cmd/breakthrough's own input capture before anything else.
func (r *Root) HandlePrefixKey(event *tcell.EventKey) bool {
	return r.handlePrefixKey(event)
}

// PrefixActive reports whether prefix mode is currently waiting for a
// verb — exported for cmd/breakthrough, which has to know not to treat
// Ctrl+_ itself as an ordinary shortcut mid-chord.
func (r *Root) PrefixActive() bool { return r.prefixActive }
