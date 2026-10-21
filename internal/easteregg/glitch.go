package easteregg

import "math/rand"

// glitchNoiseChars is the pool GlitchLine draws unresolved characters
// from — a deliberately narrow, Matrix-flavored set (uppercase Latin,
// digits, a handful of ASCII symbols), not every printable rune: a
// wall of truly random Unicode would read as mojibake, not "glitching
// text".
const glitchNoiseChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%&*"

// glitchResolveSpread/glitchFrameSpread are odd multipliers used only
// to spread a shared seed across independent per-character and
// per-frame pseudo-random sequences (see glitchResolveFrame/
// glitchNoiseRune) — arbitrary beyond being odd and large enough to
// avoid short-period collisions between nearby character indices or
// frame numbers; not cryptographic, just decorrelation.
const (
	glitchResolveSpread = 2654435761
	glitchFrameSpread   = 40503
)

// GlitchLine renders target "resolving" from noise into real text
// over revealFrames: frame 0 is pure noise the same rune-length as
// target, frame >= revealFrames is target verbatim. Each character
// resolves at its own deterministic frame within [0, revealFrames) —
// see glitchResolveFrame — rather than every character flipping over
// in lockstep on the same frame, which is what actually reads as
// "glitching" rather than a single instant swap.
//
// target is read as runes, not bytes: a non-ASCII username (the first
// real caller — see feature_ideas.txt's own "Wake up, <username>…"
// stage) must survive this unchanged once resolved, which indexing
// target's raw bytes would not guarantee.
//
// Deterministic in (target, frame, revealFrames, seed) alone — never
// math/rand's own global source — so the exact same animation frame
// replays identically given the same inputs, which is what makes this
// testable at all (see glitchResolveFrame/glitchNoiseRune's own doc
// comments for how each seeds its own independent rand.Source).
func GlitchLine(target string, frame, revealFrames int, seed int64) string {
	runes := []rune(target)
	out := make([]rune, len(runes))
	for i, want := range runes {
		if frame >= glitchResolveFrame(i, revealFrames, seed) {
			out[i] = want
			continue
		}
		out[i] = glitchNoiseRune(i, frame, seed)
	}
	return string(out)
}

// glitchResolveFrame deterministically spreads each character's own
// resolve point across [1, revealFrames) — never 0, so "frame 0 is
// pure noise" (see GlitchLine's own doc comment) is a guarantee for
// every character, not just the common case plain Intn(revealFrames)
// would have left to chance. A separate seeded source per character
// index, not one shared sequence consumed left to right, so prepending
// or appending characters to target never shifts which frame an
// already-placed character resolves on.
func glitchResolveFrame(charIndex, revealFrames int, seed int64) int {
	if revealFrames <= 1 {
		return 0
	}
	r := rand.New(rand.NewSource(seed ^ int64(charIndex)*glitchResolveSpread))
	return 1 + r.Intn(revealFrames-1)
}

// glitchNoiseRune picks this frame's still-unresolved display
// character for charIndex — reseeded per (charIndex, frame), so the
// same pair always yields the same glyph (a test can assert an exact
// frame's output byte for byte), while neighboring frames differ from
// each other, which is what makes it read as flickering rather than
// frozen noise.
func glitchNoiseRune(charIndex, frame int, seed int64) rune {
	r := rand.New(rand.NewSource(seed ^ int64(charIndex)*glitchResolveSpread ^ int64(frame)*glitchFrameSpread))
	return rune(glitchNoiseChars[r.Intn(len(glitchNoiseChars))])
}
