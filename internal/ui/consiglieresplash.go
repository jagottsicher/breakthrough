// consiglieresplash.go renders a small, original half-block portrait
// into the AI Consigliere sidebar's own chat history (see
// renderConsigliereHistory in consigliere.go) — the archetypal
// fedora-and-suit silhouette the name evokes, not a frame from any
// specific film: an original pixel-art grid, drawn here from scratch,
// never a reproduction of copyrighted artwork or a likeness of any
// real or fictional actor. Prepended to the history text on every
// render rather than stored as a chat message, so it simply scrolls
// out of view as the conversation grows — per the user's own explicit
// request ("das dann einfach wegscrollt").
package ui

import (
	"strings"

	"github.com/rivo/tview"
)

// consigliereSplashWidth is every row of consigliereSplashGrid's own
// fixed column count.
const consigliereSplashWidth = 16

// consigliereSplashGrid is the portrait's own pixel data, two rows per
// terminal row (see consigliereSplashArt's own half-block pairing):
// hat (H) with its band (B) over a face (S), collar (C) and tie (T)
// below, widening into jacket shoulders (J). "." is background — see
// consigliereSplashColors' own doc comment on why it never actually
// paints anything. Built with strings.Repeat rather than hand-typed
// literals so each row's own total width is visibly correct by
// arithmetic (the repeat counts in each line sum to
// consigliereSplashWidth) instead of by manually counting characters.
var consigliereSplashGrid = func() []string {
	rep := strings.Repeat
	return []string{
		rep(".", 6) + rep("H", 4) + rep(".", 6),
		rep(".", 5) + rep("H", 6) + rep(".", 5),
		rep(".", 4) + rep("H", 8) + rep(".", 4),
		rep(".", 2) + rep("H", 12) + rep(".", 2),
		rep("H", 16),
		rep(".", 1) + rep("B", 14) + rep(".", 1),
		rep(".", 3) + rep("S", 10) + rep(".", 3),
		rep(".", 3) + rep("S", 10) + rep(".", 3),
		rep(".", 3) + rep("S", 10) + rep(".", 3),
		rep(".", 4) + rep("S", 8) + rep(".", 4),
		rep(".", 5) + rep("S", 6) + rep(".", 5),
		rep(".", 5) + rep("C", 6) + rep(".", 5),
		rep(".", 4) + rep("C", 3) + rep("T", 2) + rep("C", 3) + rep(".", 4),
		rep(".", 2) + rep("J", 3) + rep("C", 2) + rep("T", 2) + rep("C", 2) + rep("J", 3) + rep(".", 2),
		rep(".", 2) + rep("J", 4) + rep("C", 1) + rep("T", 2) + rep("C", 1) + rep("J", 4) + rep(".", 2),
		rep(".", 2) + rep("J", 5) + rep("T", 2) + rep("J", 5) + rep(".", 2),
		rep("J", 16),
		rep("J", 16),
	}
}()

// consigliereSplashColors maps consigliereSplashGrid's own non-"."
// letter codes to hex colors — tview's dynamic-color tag syntax
// accepts "#rrggbb" directly, so no tcell.Color round-trip is needed.
// "." deliberately has no entry: per the user's own explicit request
// ("darf keinen HG haben"), background pixels render as a plain,
// untagged space instead of any fill color — see
// consigliereSplashArt — so the portrait shows through to whatever
// consigliereHistory's own real background already is (SurfaceBackground,
// see theme.go), rather than sitting in a separate colored box.
var consigliereSplashColors = map[byte]string{
	'H': "#16161b", // hat
	'B': "#3a1f1f", // hat band
	'S': "#c89b6d", // skin
	'C': "#e8e6e2", // shirt collar
	'T': "#4a0f12", // tie
	'J': "#1c1c24", // jacket
}

// consigliereSplashCaption sits one blank line under the portrait —
// what the Consigliere is actually there for, not a line from any
// film.
const consigliereSplashCaption = "Explain the problem, then ask for a solution."

// consigliereSplashLines is consigliereSplashArt's own content, before
// centering: the half-block portrait (see consigliereSplashGrid) two
// source rows per rendered line, then a blank line, then the caption.
func consigliereSplashLines() []string {
	lines := make([]string, 0, len(consigliereSplashGrid)/2+2)
	for i := 0; i+1 < len(consigliereSplashGrid); i += 2 {
		top, bottom := consigliereSplashGrid[i], consigliereSplashGrid[i+1]
		var b strings.Builder
		for col := 0; col < consigliereSplashWidth; col++ {
			t, bo := top[col], bottom[col]
			topColor, topOK := consigliereSplashColors[t]
			botColor, botOK := consigliereSplashColors[bo]
			switch {
			case !topOK && !botOK:
				// Neither half has a real pixel — a plain space paints
				// no ink at all, so it's always safe regardless of
				// color, unlike the two cases below.
				b.WriteString("[-:-] ")
			case topOK && botOK:
				// ▀ (upper half block) paints its own top half with
				// the foreground color and its bottom half with the
				// background color — both real here.
				b.WriteString("[" + topColor + ":" + botColor + "]▀[-:-]")
			case topOK && !botOK:
				// Only the top half is real. ▀'s own background slot
				// is where no ink is drawn — "-" (the TextView's own
				// real background, SurfaceBackground, not a fixed
				// fill color) is correct there specifically because
				// nothing paints over it. Setting the FOREGROUND to
				// "-" instead — the mistake this fixes — would still
				// draw ▀'s own solid upper half in the terminal's
				// default foreground (typically a light/white tone,
				// not transparent at all), which is exactly the
				// stray-white-pixel artifact at the hat/shoulder edges
				// the user reported.
				b.WriteString("[" + topColor + ":-]▀[-:-]")
			default:
				// Only the bottom half is real — ▄ (lower half block)
				// paints ITS OWN bottom half with the foreground color,
				// so the real pixel goes in foreground here instead,
				// for the identical reason the case above keeps it out
				// of ▀'s own foreground slot.
				b.WriteString("[" + botColor + ":-]▄[-:-]")
			}
		}
		lines = append(lines, b.String())
	}
	return lines
}

// consigliereSplashArt renders the portrait (consigliereSplashLines)
// plus consigliereSplashCaption, every line individually centered
// within width columns (tview.TaggedStringWidth, so the art's own
// color tags don't count against their visible width) — per the
// user's own explicit request that both the portrait and the caption
// sit in the middle of the panel, not flush against its left edge.
// width <= 0 (not laid out yet — see showConsigliere's own call
// order) falls back to consigliereSplashWidth, i.e. no centering,
// rather than a negative or division-by-garbage pad.
//
// The caption is wrapped (via wrapText, see errors.go) to width
// *before* centering, each wrapped line on its own: centering the
// full, unwrapped caption string once and letting consigliereHistory's
// own SetWrap break it afterwards — the original version of this —
// only ever centers the first wrapped line; every further line a long
// caption needs starts flush left instead, a real, visible bug the
// user reported ("weder Bild noch Text mittig").
func consigliereSplashArt(width int) string {
	if width <= 0 {
		width = consigliereSplashWidth
	}
	lines := consigliereSplashLines()
	lines = append(lines, "")
	lines = append(lines, wrapText(consigliereSplashCaption, width)...)
	for i, line := range lines {
		pad := (width - tview.TaggedStringWidth(line)) / 2
		if pad > 0 {
			lines[i] = strings.Repeat(" ", pad) + line
		}
	}
	return strings.Join(lines, "\n") + "\n"
}
