package ui

import "github.com/gdamore/tcell/v2"

// termKeyBytes translates one keyboard event into the byte sequence a
// remote shell/terminal expects on its own stdin — the core, most
// common set: plain runes, Enter, Backspace, Tab, Escape, the four
// arrow keys, and Ctrl+letter. Anything else (F-keys, Home/End/
// PageUp/PageDown/Insert/Delete, Alt-combinations) is a later, purely
// additive extension — see feature_ideas.txt #11's own Phase 2 notes
// — not an architectural decision, so leaving them out now doesn't
// block anything else from working.
//
// Returns nil for a key this doesn't translate at all — the caller
// then simply does nothing with it, rather than guessing at a
// sequence that might be wrong.
//
// Ctrl+letter is checked via tcell.KeyCtrlA..KeyCtrlZ directly, never
// KeyRune combined with ModCtrl — verified directly against tcell's
// own key.go (NewEventKey's own normalization: a rune key event with
// ModCtrl set and a letter rune is rewritten to one of these
// constants before any caller ever sees it, the same real path actual
// terminal input goes through, not just this constructor). An
// earlier version of this function checked KeyRune+ModCtrl instead,
// which this file's own test caught never matching anything at all.
func termKeyBytes(event *tcell.EventKey) []byte {
	if event.Key() >= tcell.KeyCtrlA && event.Key() <= tcell.KeyCtrlZ {
		return []byte{byte(event.Key() - tcell.KeyCtrlA + 1)}
	}
	switch event.Key() {
	case tcell.KeyEnter:
		return []byte("\r")
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		return []byte{0x7f}
	case tcell.KeyTab:
		return []byte("\t")
	case tcell.KeyEscape:
		return []byte{0x1b}
	case tcell.KeyUp:
		return []byte("\x1b[A")
	case tcell.KeyDown:
		return []byte("\x1b[B")
	case tcell.KeyRight:
		return []byte("\x1b[C")
	case tcell.KeyLeft:
		return []byte("\x1b[D")
	case tcell.KeyRune:
		return []byte(string(event.Rune()))
	default:
		return nil
	}
}
