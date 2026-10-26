package ui

import "github.com/gdamore/tcell/v2"

// termKeyBytes translates one keyboard event into the byte sequence a
// remote shell/terminal expects on its own stdin: plain runes, Enter,
// Backspace, Tab, Escape, the four arrow keys, Ctrl+letter, F1–F12,
// and Home/End/PageUp/PageDown/Insert/Delete. Mouse events and
// Alt-combinations are a later, purely additive extension — see
// feature_ideas.txt #11's own Phase 2 notes — not an architectural
// decision, so leaving them out now doesn't block anything else from
// working.
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
//
// The F-key and navigation-key sequences below are the standard xterm
// ones (F1–F4 use the SS3 form \x1bO<letter>, matching every real
// xterm/urxvt/gnome-terminal since the VT220 convention; F5 onward
// use the CSI ...~ form) — the same encoding ssh's own pty allocation
// already negotiates for a TERM=xterm*-class client, so this matches
// what the remote side already expects from a direct (non-embedded)
// connection, no per-TERM table needed.
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
	case tcell.KeyHome:
		return []byte("\x1b[H")
	case tcell.KeyEnd:
		return []byte("\x1b[F")
	case tcell.KeyPgUp:
		return []byte("\x1b[5~")
	case tcell.KeyPgDn:
		return []byte("\x1b[6~")
	case tcell.KeyInsert:
		return []byte("\x1b[2~")
	case tcell.KeyDelete:
		return []byte("\x1b[3~")
	case tcell.KeyF1:
		return []byte("\x1bOP")
	case tcell.KeyF2:
		return []byte("\x1bOQ")
	case tcell.KeyF3:
		return []byte("\x1bOR")
	case tcell.KeyF4:
		return []byte("\x1bOS")
	case tcell.KeyF5:
		return []byte("\x1b[15~")
	case tcell.KeyF6:
		return []byte("\x1b[17~")
	case tcell.KeyF7:
		return []byte("\x1b[18~")
	case tcell.KeyF8:
		return []byte("\x1b[19~")
	case tcell.KeyF9:
		return []byte("\x1b[20~")
	case tcell.KeyF10:
		return []byte("\x1b[21~")
	case tcell.KeyF11:
		return []byte("\x1b[23~")
	case tcell.KeyF12:
		return []byte("\x1b[24~")
	case tcell.KeyRune:
		return []byte(string(event.Rune()))
	default:
		return nil
	}
}
