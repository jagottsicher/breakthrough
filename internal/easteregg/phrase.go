package easteregg

// TriggerPhrase is the secret phrase that opens the Easter egg from
// anywhere the caller's own "safe to accept a global shortcut right
// now" guard allows (see internal/ui's own wiring in cmd/breakthrough's
// SetInputCapture) — deliberately lowercase-only ASCII, so PhraseDetector
// never has to worry about case folding or multi-byte runes on its own
// matching side (GlitchLine, not this type, is the one that has to
// handle a non-ASCII username — see its own doc comment).
const TriggerPhrase = "followthewhiterabbit"

// PhraseDetector is a rolling, stateful matcher against TriggerPhrase.
// Feed is meant to be called with every candidate rune in sequence as
// it arrives — it never consumes or claims the key itself (see this
// package's own doc comment and feature_ideas.txt's "Trigger" section
// for why: every rune must still reach its normal handler regardless
// of what Feed reports), it only ever watches and reports back once
// the full phrase has just completed.
type PhraseDetector struct {
	matched int // how many leading characters of TriggerPhrase match so far
}

// Feed advances the match by one rune, returning true exactly once,
// the instant the full phrase completes — and resets itself
// immediately after, matched or not, so a second, later attempt
// (completed or abandoned partway) always starts clean rather than
// carrying leftover state from the first.
//
// A mismatched rune doesn't necessarily mean "start over empty": if r
// itself happens to be TriggerPhrase's own first character, that's a
// valid fresh start right there (e.g. typing the phrase twice in a
// row, or recovering from a typo on the very first letter) — handled
// explicitly below rather than only ever resetting to zero on any
// non-match.
func (d *PhraseDetector) Feed(r rune) bool {
	if r == rune(TriggerPhrase[d.matched]) {
		d.matched++
		if d.matched == len(TriggerPhrase) {
			d.matched = 0
			return true
		}
		return false
	}
	if r == rune(TriggerPhrase[0]) {
		d.matched = 1
	} else {
		d.matched = 0
	}
	return false
}
