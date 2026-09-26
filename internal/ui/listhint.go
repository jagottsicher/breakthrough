// listhint.go: the shared bottom hint bar shape every full-screen list
// screen (Activity Log, Compare (tree), Firewall, Mounts, Options,
// Sessions, Toolbox) renders — "key: label" segments styled the same
// "background-colored key, label directly after" way chordHintBar/
// buildButtonBar's own highlightKey already render the main button
// bar's own legend and a chord family's own second-level members, per
// the user's own explicit request that these read the same way those
// already do, not as plain, uncolored text.
package ui

import (
	"fmt"
	"strings"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// listHintEntry is one "key: label" segment of a list screen's own
// bottom hint bar — key is exactly what gets pressed ("r", "Esc",
// "↑/↓", ...), label is what it does.
type listHintEntry struct {
	key   string
	label string
}

// buildListHint renders entries in the shared style this file's own
// doc comment describes.
//
// A single-rune key (e.g. "r", "?") gets highlightKey's own padded
// style — one space either side of it inside the colored background,
// the label following with no space of its own (the highlight's own
// trailing space already separates the two). Anything longer — "Esc",
// "Tab", or a compound like "↑/↓" — gets the background wrapped
// directly around it instead, with no padding and no space before its
// own label, the exact same treatment chordHintBar already gives "Esc"
// itself: there is no single-character "key" to pad here, the whole
// text names what's actually pressed.
//
// Every entry is separated from the one before it by a single plain
// space, except one whose own key is exactly "Esc": that one gets the
// heavier " │ " block separator instead — the same one buildButtonBar's
// own blockSep and chordHintBar's own "Esccancel" already use to set an
// exit action apart from the ones before it, since every one of these
// hint bars' own last entry is exactly that.
func buildListHint(theme config.ResolvedTheme, entries []listHintEntry) string {
	keyBG := colorTag(theme.ButtonBackground)
	var b strings.Builder
	b.WriteString(" ")
	for i, e := range entries {
		if i > 0 {
			if e.key == "Esc" {
				b.WriteString(" │ ")
			} else {
				b.WriteString(" ")
			}
		}
		if len([]rune(e.key)) == 1 {
			fmt.Fprintf(&b, "[:%s:] %s [-:-:-]%s", keyBG, e.key, e.label)
		} else {
			fmt.Fprintf(&b, "[:%s:]%s[-:-:-]%s", keyBG, e.key, e.label)
		}
	}
	b.WriteString(" ")
	return b.String()
}
