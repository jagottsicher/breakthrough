package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// octalDigitCapture returns an InputCapture for a shared inline edit
// field that edits a 4-character octal permission value — Chmod's own
// edit field is always one (active is a constant true there), while
// Properties' shared field is only sometimes one (active reports
// whether the field currently being edited is fieldPermOctal — every
// other field type there is free text and must keep its normal
// insert-at-cursor behavior).
//
// Every rune keystroke is restricted to '0'..'7', the only valid octal
// digits — anything else is rejected outright, never reaching the
// field's text at all. An accepted digit first forwards a synthetic
// Delete keystroke through the field's own normal input handling,
// removing whatever character already sits under the cursor, before
// letting the original keystroke fall through to the field's own
// normal insertion logic. Net effect: typing a digit always overwrites
// the one that was already there and advances the cursor by one,
// instead of appending after it — so the field can be pre-filled with
// its current value (see activateInlineTextField/activateChmodTextField)
// and the user can just type over whichever digits they mean to change,
// leaving the rest exactly as they were, per the user's own explicit
// request.
//
// The one case this can't prevent structurally — the cursor already
// sitting after the 4th digit, with nothing left to forward-delete, so
// letting the keystroke through unchanged would append a 5th character
// instead of overwriting one — is caught by the field's own
// SetAcceptanceFunc instead (still capping the result at 4 characters —
// see activateInlineTextField's own doc comment): rejected there,
// before it ever reaches the text, so a stray keystroke past the 4th
// digit has no effect at all rather than needing to be trimmed off
// again at commit time.
func octalDigitCapture(field *tview.InputField, active func() bool) func(*tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() != tcell.KeyRune || !active() {
			return event
		}
		if ch := event.Rune(); ch < '0' || ch > '7' {
			return nil // not an octal digit — reject outright, don't even reach the field
		}
		field.InputHandler()(tcell.NewEventKey(tcell.KeyDelete, 0, tcell.ModNone), func(tview.Primitive) {})
		return event
	}
}

// resetInlineFieldCursorToStart moves the shared inline edit field's
// cursor to column 0. Needed because SetText's own effect on the
// cursor — verified directly against tview's own textarea.go:
// InputField.SetText calls TextArea.Replace, which always parks the
// cursor at the end of the newly-set text — is the wrong place to
// start from when the whole point of prefilling with the current value
// (see octalDigitCapture's own doc comment) is to let the user
// immediately start overwriting it from the left.
//
// There's no direct "set cursor column" call on InputField itself
// (only its own private, unexported *TextArea has one), so this walks
// it back with the same synthetic-keystroke approach this package's own
// tests already use to drive InputHandler directly. Four Left presses
// for a fixed 4-character value is exact, not just "enough" — further
// presses past column 0 are already harmless no-ops (TextArea clamps
// there), so there's no need to special-case a shorter prefill.
func resetInlineFieldCursorToStart(field *tview.InputField) {
	for i := 0; i < 4; i++ {
		field.InputHandler()(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone), func(tview.Primitive) {})
	}
}
