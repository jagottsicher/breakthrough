package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestTermKeyBytesPlainRunes(t *testing.T) {
	got := termKeyBytes(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone))
	if string(got) != "a" {
		t.Errorf("termKeyBytes('a') = %q, want %q", got, "a")
	}
}

func TestTermKeyBytesEnterBackspaceTabEscape(t *testing.T) {
	for _, tc := range []struct {
		key  tcell.Key
		want string
	}{
		{tcell.KeyEnter, "\r"},
		{tcell.KeyBackspace2, "\x7f"},
		{tcell.KeyTab, "\t"},
		{tcell.KeyEscape, "\x1b"},
	} {
		got := termKeyBytes(tcell.NewEventKey(tc.key, 0, tcell.ModNone))
		if string(got) != tc.want {
			t.Errorf("termKeyBytes(%v) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestTermKeyBytesArrowKeys(t *testing.T) {
	for _, tc := range []struct {
		key  tcell.Key
		want string
	}{
		{tcell.KeyUp, "\x1b[A"},
		{tcell.KeyDown, "\x1b[B"},
		{tcell.KeyRight, "\x1b[C"},
		{tcell.KeyLeft, "\x1b[D"},
	} {
		got := termKeyBytes(tcell.NewEventKey(tc.key, 0, tcell.ModNone))
		if string(got) != tc.want {
			t.Errorf("termKeyBytes(%v) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestTermKeyBytesCtrlLetters(t *testing.T) {
	// Ctrl+C is 0x03, Ctrl+A is 0x01, Ctrl+Z is 0x1a — the standard
	// "letter's own position in the alphabet" mapping every real
	// terminal already uses, case-insensitively (Ctrl+c and Ctrl+C
	// send the identical byte — there's no way to tell them apart at
	// the terminal layer, same as a real one).
	for _, tc := range []struct {
		rune rune
		want byte
	}{
		{'a', 1}, {'c', 3}, {'z', 26},
		{'A', 1}, {'C', 3}, {'Z', 26},
	} {
		got := termKeyBytes(tcell.NewEventKey(tcell.KeyRune, tc.rune, tcell.ModCtrl))
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("termKeyBytes(Ctrl+%c) = %v, want [%d]", tc.rune, got, tc.want)
		}
	}
}

func TestTermKeyBytesUnmappedKeyReturnsNil(t *testing.T) {
	if got := termKeyBytes(tcell.NewEventKey(tcell.KeyF5, 0, tcell.ModNone)); got != nil {
		t.Errorf("termKeyBytes(F5) = %v, want nil (not yet mapped — see its own doc comment)", got)
	}
}

func TestTermKeyBytesPlainRuneIsUnaffectedByUnrelatedModifiers(t *testing.T) {
	// Shift+a arrives as the already-uppercased rune 'A' with
	// ModShift set, the same way every terminal already handles
	// shifted letters — Shift itself is never a separate byte to send.
	got := termKeyBytes(tcell.NewEventKey(tcell.KeyRune, 'A', tcell.ModShift))
	if string(got) != "A" {
		t.Errorf("termKeyBytes(Shift+a as rune 'A') = %q, want %q", got, "A")
	}
}
