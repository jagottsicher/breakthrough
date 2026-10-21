package easteregg

// centerPad returns s as runes, padded with spaces to width — split as
// evenly as possible between both sides (one extra space on the right
// when width-len(s) is odd) so a shorter string centers within a
// longer one rather than only padding on one side. Returns s's own
// runes unpadded if it's already at or past width.
func centerPad(s string, width int) []rune {
	r := []rune(s)
	if len(r) >= width {
		return r
	}
	total := width - len(r)
	left := total / 2
	right := total - left
	out := make([]rune, 0, width)
	for i := 0; i < left; i++ {
		out = append(out, ' ')
	}
	out = append(out, r...)
	for i := 0; i < right; i++ {
		out = append(out, ' ')
	}
	return out
}

// MorphLine renders a transition from "from" to "to" — unlike
// GlitchLine (noise resolving into one fixed target), this starts
// showing from's own text cleanly at frame 0, flickers through noise
// per character as each one's own resolve frame approaches, and shows
// to's own text once past it. Both strings are centered (see
// centerPad) within a shared width — max(len(from), len(to)) — before
// comparing position by position, so "SYSTEM FAILURE" morphing into
// the shorter "breakthrough" pads the latter with spaces on both
// sides rather than only growing on the right.
//
// A position where the (padded) characters already agree never shows
// noise at all — nothing to transition there. Reuses glitchResolveFrame/
// glitchNoiseRune (glitch.go) for the per-character timing and noise
// character, the same deterministic-per-(index,frame,seed) contract
// GlitchLine itself relies on.
func MorphLine(from, to string, frame, totalFrames int, seed int64) string {
	width := len([]rune(from))
	if w := len([]rune(to)); w > width {
		width = w
	}
	fromR := centerPad(from, width)
	toR := centerPad(to, width)

	out := make([]rune, width)
	for i := range out {
		if fromR[i] == toR[i] {
			out[i] = toR[i]
			continue
		}
		switch {
		case frame <= 0:
			out[i] = fromR[i]
		case frame >= glitchResolveFrame(i, totalFrames, seed):
			out[i] = toR[i]
		default:
			out[i] = glitchNoiseRune(i, frame, seed)
		}
	}
	return string(out)
}
