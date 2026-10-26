package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/hinshun/vt10x"
	"github.com/rivo/tview"
)

// termMouseBytes translates one mouse event into the byte sequence a
// remote program expects on its stdin, or nil if mode says it hasn't
// asked for mouse reporting at all (vt10x.ModeMouseMask) — the same
// "don't forward a local-only gesture into a program that never
// turned mouse tracking on" decision a real terminal emulator already
// makes, not an unconditional passthrough: a remote vim's own visual
// selection with the mouse would otherwise never work normally
// (text-selection drag sent as mouse-tracking bytes instead), the
// usual reason plain terminals also gate this. col/row are 0-based,
// relative to the virtual terminal's own top-left cell (the same
// space Draw's own GetInnerRect already uses).
//
// Click/DoubleClick (tview's own synthesized actions, fired *in
// addition to* the Up that caused them — see Application.fireMouseActions)
// are deliberately not cases here: handling them too would send a
// second, duplicate release for the same physical click.
func termMouseBytes(action tview.MouseAction, event *tcell.EventMouse, col, row int, mode vt10x.ModeFlag) []byte {
	if mode&vt10x.ModeMouseMask == 0 {
		return nil
	}

	var button int
	release := false
	motion := false
	switch action {
	case tview.MouseLeftDown:
		button = 0
	case tview.MouseMiddleDown:
		button = 1
	case tview.MouseRightDown:
		button = 2
	case tview.MouseLeftUp:
		button, release = 0, true
	case tview.MouseMiddleUp:
		button, release = 1, true
	case tview.MouseRightUp:
		button, release = 2, true
	case tview.MouseScrollUp:
		button = 64
	case tview.MouseScrollDown:
		button = 65
	case tview.MouseScrollLeft:
		button = 66
	case tview.MouseScrollRight:
		button = 67
	case tview.MouseMove:
		motion = true
		switch {
		case event.Buttons()&tcell.ButtonPrimary != 0:
			button = 0
		case event.Buttons()&tcell.ButtonSecondary != 0:
			button = 2
		case event.Buttons()&tcell.ButtonMiddle != 0:
			button = 1
		default:
			button = 3 // xterm's own "no button held" code
		}
		// Button-event tracking (1002) only wants motion while a
		// button is held; any-event tracking (1003) wants it always.
		if mode&vt10x.ModeMouseMany == 0 && (mode&vt10x.ModeMouseMotion == 0 || button == 3) {
			return nil
		}
	default:
		return nil
	}

	// X10 compatibility mode (the one bit vt10x calls ModeMouseX10, set
	// by mode 9 alone) only ever reports presses — never release or
	// motion, per xterm's own ctlseqs documentation.
	x10Only := mode&vt10x.ModeMouseMask == vt10x.ModeMouseX10
	if x10Only && (release || motion) {
		return nil
	}

	if motion {
		button += 32
	}
	mod := event.Modifiers()
	if mod&tcell.ModShift != 0 {
		button += 4
	}
	if mod&tcell.ModAlt != 0 {
		button += 8
	}
	if mod&tcell.ModCtrl != 0 {
		button += 16
	}

	if mode&vt10x.ModeMouseSgr != 0 {
		suffix := byte('M')
		if release {
			suffix = 'm'
		}
		return fmt.Appendf(nil, "\x1b[<%d;%d;%d%c", button, col+1, row+1, suffix)
	}

	// Legacy X10/UTF-8 encoding: single bytes, so coordinates saturate
	// at 255-32=223 rather than wrapping — the real limitation this
	// encoding has always had, not a bug to work around here. A real
	// release can't report which button went up in this encoding
	// either (xterm's own button code 3 for "release, unknown which").
	cb := button + 32
	if release {
		cb = 3 + 32
	}
	cx, cy := col+1+32, row+1+32
	if cx > 255 {
		cx = 255
	}
	if cy > 255 {
		cy = 255
	}
	return []byte{0x1b, '[', 'M', byte(cb), byte(cx), byte(cy)}
}
