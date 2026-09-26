// listhint.go: the shared bottom hint bar shape every full-screen list
// screen (Activity Log, Compare (tree), Firewall, Mounts, Options,
// Sessions, Toolbox) renders — "key: label" segments styled the same
// "background-colored key, label directly after" way chordHintBar/
// buildButtonBar's own highlightKey already render the main button
// bar's own legend and a chord family's own second-level members, per
// the user's own explicit request that these read the same way those
// already do, not as plain, uncolored text — and, per the user's own
// further explicit request, every one of these colored keys is a real,
// clickable button too, including a compound entry's own individual
// members ("↑" and "↓" each clickable on their own, not just the pair
// as one region).
package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// listHintKey is one clickable key within a listHintEntry — key is
// exactly what gets pressed ("r", "Esc", "↑", ...), run is what a click
// on it actually does. Several keys sharing one listHintEntry (e.g.
// "↑"/"↓" both under "move") are rendered adjacent, separated by a
// plain "/", each keeping its own run — a group describes one label,
// not one action, so each member still needs its own.
type listHintKey struct {
	key string
	run func(r *Root)
}

// listHintEntry is one "key(s): label" segment of a list screen's own
// bottom hint bar. suffix, when non-empty, is plain, uncolored text
// appended directly after the last key and before label — for
// something the hint text has to mention that isn't actually a key at
// all (Sessions' own "/click", describing that a plain mouse click on
// the row already does the same thing every key here does).
type listHintEntry struct {
	keys   []listHintKey
	suffix string
	label  string
}

// hintKey builds the common case: a listHintEntry with exactly one key.
func hintKey(key, label string, run func(r *Root)) listHintEntry {
	return listHintEntry{keys: []listHintKey{{key, run}}, label: label}
}

// simulateKeyOnFocused returns a run func that dispatches a synthetic
// key event to whichever primitive currently has real keyboard focus —
// used by a list hint's own clickable Tab/arrow-key buttons, whose
// real-key behavior already depends on which of several widgets on the
// same screen currently has focus (Tab's own multi-way cycle, a
// List/Table's own native cursor movement, Options' own captureOptions
// PaneArrows) rather than one single, focus-independent action to call
// directly the way every other button here can. This deliberately
// mirrors a real keypress exactly, unlike this file's own other run
// funcs (see hintKey's own callers), which call the underlying action
// directly regardless of focus — a click is always an unambiguous,
// explicit request (the same reasoning captureButtonBarMouse's own doc
// comment gives), but "move" genuinely has no single meaning
// independent of what's currently focused.
func simulateKeyOnFocused(key tcell.Key) func(r *Root) {
	return func(r *Root) {
		focused := r.app.GetFocus()
		if focused == nil {
			return
		}
		if h := focused.InputHandler(); h != nil {
			h(tcell.NewEventKey(key, 0, tcell.ModNone), func(tview.Primitive) {})
		}
	}
}

// renderHintKey renders key with the shared "background-colored key"
// style this file's own package doc comment describes — highlightKey's
// own padded style (one space either side, inside the colored
// background) for a single rune (e.g. "r", "?"), or chordHintBar's own
// direct "Esc" treatment (no padding at all) for anything longer ("Esc",
// "Tab", one member of a compound like "↑"): there is no single-
// character "key" to pad in that case, the whole text names what's
// actually pressed. width is the rendered result's own display width —
// the click target's own size, in columns, starting wherever the
// caller places this text.
func renderHintKey(theme config.ResolvedTheme, key string) (rendered string, width int) {
	keyBG := colorTag(theme.ButtonBackground)
	if len([]rune(key)) == 1 {
		return fmt.Sprintf("[:%s:] %s [-:-:-]", keyBG, key), 3
	}
	return fmt.Sprintf("[:%s:]%s[-:-:-]", keyBG, key), len([]rune(key))
}

// singleKeyHint renders one "key" immediately followed by label, with
// no surrounding padding of its own — for a one-off "key to do this"
// hint that sits at column 0 of its own line inside a larger block of
// otherwise plain text (Properties'/Details' own hash and directory-
// size compute hints — "'h' to compute...", "'k' to compute this
// directory's total size..."), rather than one of buildListHint's own
// standalone hint bars. keyWidth is the key's own click target width,
// always starting at column 0 — the caller's own mouse capture checks
// a click's column against it directly (see e.g. hashesMouseCapture),
// deliberately never falling back to "click anywhere on this line/
// section" the way this app used to, per the user's own explicit
// request that a real button, not a whole line or area, be the actual
// click target everywhere a key is highlighted this way.
func singleKeyHint(theme config.ResolvedTheme, key, label string) (text string, keyWidth int) {
	rendered, width := renderHintKey(theme, key)
	return rendered + label, width
}

// buildListHint renders entries in the shared style this file's own
// doc comment describes, and returns every key's own clickable region
// alongside it (see listHintActionAt/captureListHintMouse).
//
// Every entry is separated from the one before it by a single plain
// space, except one whose own first key is exactly "Esc": that one gets
// the heavier " │ " block separator instead — the same one
// buildButtonBar's own blockSep and chordHintBar's own "Esccancel"
// already use to set an exit action apart from the ones before it,
// since every one of these hint bars' own last entry is exactly that.
func buildListHint(theme config.ResolvedTheme, entries []listHintEntry) (text string, spans []listHintSpan) {
	var b strings.Builder
	col := 0
	write := func(s string) {
		b.WriteString(s)
		col += tview.TaggedStringWidth(s)
	}

	write(" ")
	for i, e := range entries {
		if i > 0 {
			if len(e.keys) > 0 && e.keys[0].key == "Esc" {
				write(" │ ")
			} else {
				write(" ")
			}
		}
		for j, k := range e.keys {
			if j > 0 {
				write("/")
			}
			start := col
			rendered, _ := renderHintKey(theme, k.key)
			write(rendered)
			if k.run != nil {
				spans = append(spans, listHintSpan{startCol: start, endCol: col, run: k.run})
			}
		}
		if e.suffix != "" {
			// Unlike a colored key (whose own background already marks
			// where it ends), plain suffix text needs a real space of
			// its own before the label — otherwise "click" and
			// "activate" visually run together into one word.
			write(e.suffix + " ")
		}
		write(e.label)
	}
	write(" ")

	return b.String(), spans
}

// listHintSpan is one clickable region within a list screen's own
// bottom hint bar — buttonBarSpan's own idea (see bottombar.go), but
// key-less: nothing here ever needs to read which key a span was for
// after the fact, only run it.
type listHintSpan struct {
	startCol, endCol int
	run              func(r *Root)
}

// listHintActionAt returns the span at screen position (x, y) within
// hint's own rect, if any — listHintSpan's own counterpart to
// buttonBarActionAt.
func listHintActionAt(hint *tview.TextView, spans []listHintSpan, x, y int) (listHintSpan, bool) {
	if !hint.InRect(x, y) {
		return listHintSpan{}, false
	}
	rectX, _, _, _ := hint.GetInnerRect()
	col := x - rectX
	for _, s := range spans {
		if col >= s.startCol && col < s.endCol {
			return s, true
		}
	}
	return listHintSpan{}, false
}

// captureListHintMouse builds hint's own mouse-capture handler, reading
// *spans fresh on every call (a pointer to the Root field the caller
// stores its own listHintEntry's spans in, e.g. &r.activityLogHintSpans)
// rather than a snapshot taken once at construction — applyTheme
// rebuilds that field's own value on every live theme switch (see
// buildListHint), and a stale, once-captured slice would otherwise keep
// running whichever theme was active when this handler was first
// attached. The same "InRect gated before the action-type gate,
// anything but a left click unconditionally consumed" shape
// captureButtonBarMouse already establishes, for the same reason: a
// click landing inside this row must never fall through to TextView's
// own default MouseHandler and steal real keyboard focus onto a hint
// bar that has nothing to focus for.
func (r *Root) captureListHintMouse(hint *tview.TextView, spans *[]listHintSpan) func(tview.MouseAction, *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	return func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if !hint.InRect(event.Position()) {
			return action, event
		}
		if action != tview.MouseLeftClick {
			return tview.MouseConsumed, nil
		}
		x, y := event.Position()
		if span, ok := listHintActionAt(hint, *spans, x, y); ok && span.run != nil {
			span.run(r)
		}
		return tview.MouseConsumed, nil
	}
}
