package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/hinshun/vt10x"
	"github.com/rivo/tview"
)

func TestTermMouseBytesNoReportingReturnsNil(t *testing.T) {
	event := tcell.NewEventMouse(0, 0, tcell.ButtonPrimary, tcell.ModNone)
	if got := termMouseBytes(tview.MouseLeftDown, event, 0, 0, 0); got != nil {
		t.Errorf("termMouseBytes with mode 0 = %v, want nil", got)
	}
}

func TestTermMouseBytesSgrPressAndRelease(t *testing.T) {
	mode := vt10x.ModeMouseButton | vt10x.ModeMouseSgr
	for _, tc := range []struct {
		name   string
		action tview.MouseAction
		want   string
	}{
		{"left down", tview.MouseLeftDown, "\x1b[<0;6;4M"},
		{"left up", tview.MouseLeftUp, "\x1b[<0;6;4m"},
		{"middle down", tview.MouseMiddleDown, "\x1b[<1;6;4M"},
		{"right down", tview.MouseRightDown, "\x1b[<2;6;4M"},
		{"scroll up", tview.MouseScrollUp, "\x1b[<64;6;4M"},
		{"scroll down", tview.MouseScrollDown, "\x1b[<65;6;4M"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := tcell.NewEventMouse(5, 3, tcell.ButtonNone, tcell.ModNone)
			got := termMouseBytes(tc.action, event, 5, 3, mode)
			if string(got) != tc.want {
				t.Errorf("termMouseBytes(%v) = %q, want %q", tc.action, got, tc.want)
			}
		})
	}
}

func TestTermMouseBytesSgrAppliesModifiers(t *testing.T) {
	mode := vt10x.ModeMouseButton | vt10x.ModeMouseSgr
	event := tcell.NewEventMouse(0, 0, tcell.ButtonNone, tcell.ModShift|tcell.ModCtrl)
	got := termMouseBytes(tview.MouseLeftDown, event, 0, 0, mode)
	// Shift (+4) and Ctrl (+16) on top of the left-button base code (0).
	want := "\x1b[<20;1;1M"
	if string(got) != want {
		t.Errorf("termMouseBytes(Shift+Ctrl+left down) = %q, want %q", got, want)
	}
}

func TestTermMouseBytesSgrMotionOnlyWhileButtonHeldInButtonEventMode(t *testing.T) {
	mode := vt10x.ModeMouseMotion | vt10x.ModeMouseSgr // 1002, not 1003

	held := tcell.NewEventMouse(0, 0, tcell.ButtonPrimary, tcell.ModNone)
	if got := termMouseBytes(tview.MouseMove, held, 2, 1, mode); got == nil {
		t.Error("termMouseBytes(MouseMove with button held, mode 1002) = nil, want a motion report")
	}

	plain := tcell.NewEventMouse(0, 0, tcell.ButtonNone, tcell.ModNone)
	if got := termMouseBytes(tview.MouseMove, plain, 2, 1, mode); got != nil {
		t.Errorf("termMouseBytes(MouseMove with no button, mode 1002) = %v, want nil", got)
	}
}

func TestTermMouseBytesSgrMotionAlwaysReportedInAnyEventMode(t *testing.T) {
	mode := vt10x.ModeMouseMany | vt10x.ModeMouseSgr // 1003

	plain := tcell.NewEventMouse(0, 0, tcell.ButtonNone, tcell.ModNone)
	got := termMouseBytes(tview.MouseMove, plain, 2, 1, mode)
	want := "\x1b[<35;3;2M" // button 3 ("no button") + 32 for motion
	if string(got) != want {
		t.Errorf("termMouseBytes(MouseMove, no button, mode 1003) = %q, want %q", got, want)
	}
}

func TestTermMouseBytesX10OnlyIgnoresReleaseAndMotion(t *testing.T) {
	mode := vt10x.ModeMouseX10 // mode 9 alone — press-only, per xterm's own ctlseqs

	up := tcell.NewEventMouse(0, 0, tcell.ButtonNone, tcell.ModNone)
	if got := termMouseBytes(tview.MouseLeftUp, up, 0, 0, mode); got != nil {
		t.Errorf("termMouseBytes(left up, X10-only mode) = %v, want nil", got)
	}

	held := tcell.NewEventMouse(0, 0, tcell.ButtonPrimary, tcell.ModNone)
	if got := termMouseBytes(tview.MouseMove, held, 0, 0, mode); got != nil {
		t.Errorf("termMouseBytes(motion, X10-only mode) = %v, want nil", got)
	}

	down := tcell.NewEventMouse(0, 0, tcell.ButtonNone, tcell.ModNone)
	if got := termMouseBytes(tview.MouseLeftDown, down, 0, 0, mode); got == nil {
		t.Error("termMouseBytes(left down, X10-only mode) = nil, want a press report")
	}
}

func TestTermMouseBytesLegacyEncodingUsesSingleBytesAndSaturates(t *testing.T) {
	mode := vt10x.ModeMouseButton // no ModeMouseSgr

	down := tcell.NewEventMouse(0, 0, tcell.ButtonNone, tcell.ModNone)
	got := termMouseBytes(tview.MouseLeftDown, down, 1, 2, mode)
	want := []byte{0x1b, '[', 'M', 0 + 32, 1 + 1 + 32, 2 + 1 + 32}
	if string(got) != string(want) {
		t.Errorf("termMouseBytes(legacy left down at col1,row2) = %v, want %v", got, want)
	}

	// Release in legacy mode can't report which button went up — xterm's
	// own button code 3 ("unknown") is sent regardless.
	up := tcell.NewEventMouse(0, 0, tcell.ButtonNone, tcell.ModNone)
	gotUp := termMouseBytes(tview.MouseRightUp, up, 0, 0, mode)
	wantUp := []byte{0x1b, '[', 'M', 3 + 32, 1 + 32, 1 + 32}
	if string(gotUp) != string(wantUp) {
		t.Errorf("termMouseBytes(legacy right up) = %v, want %v", gotUp, wantUp)
	}

	// Columns/rows beyond 223 saturate at the single-byte encoding's own
	// ceiling (255) rather than wrapping around.
	far := tcell.NewEventMouse(0, 0, tcell.ButtonNone, tcell.ModNone)
	gotFar := termMouseBytes(tview.MouseLeftDown, far, 9000, 9000, mode)
	wantFar := []byte{0x1b, '[', 'M', 0 + 32, 255, 255}
	if string(gotFar) != string(wantFar) {
		t.Errorf("termMouseBytes(legacy, huge coordinates) = %v, want %v", gotFar, wantFar)
	}
}

func TestTermMouseBytesClickAndDoubleClickAreIgnored(t *testing.T) {
	mode := vt10x.ModeMouseButton | vt10x.ModeMouseSgr
	event := tcell.NewEventMouse(0, 0, tcell.ButtonNone, tcell.ModNone)
	for _, action := range []tview.MouseAction{
		tview.MouseLeftClick, tview.MouseLeftDoubleClick,
		tview.MouseMiddleClick, tview.MouseRightClick,
	} {
		if got := termMouseBytes(action, event, 0, 0, mode); got != nil {
			t.Errorf("termMouseBytes(%v) = %v, want nil — Up already reported this click", action, got)
		}
	}
}
