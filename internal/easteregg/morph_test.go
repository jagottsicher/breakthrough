package easteregg

import "testing"

func TestCenterPadSplitsEvenlyWithExtraOnTheRight(t *testing.T) {
	got := string(centerPad("ab", 6))
	want := "  ab  "
	if got != want {
		t.Errorf("centerPad(\"ab\", 6) = %q, want %q", got, want)
	}
	got = string(centerPad("abc", 6))
	want = " abc  "
	if got != want {
		t.Errorf("centerPad(\"abc\", 6) = %q, want %q (one extra space on the right for an odd gap)", got, want)
	}
}

func TestCenterPadLeavesAnAlreadyLongEnoughStringAlone(t *testing.T) {
	if got := string(centerPad("hello", 3)); got != "hello" {
		t.Errorf("centerPad(\"hello\", 3) = %q, want \"hello\" unchanged", got)
	}
}

func TestMorphLineFrameZeroIsExactlyFrom(t *testing.T) {
	got := MorphLine("SYSTEM FAILURE", "breakthrough", 0, 40, 42)
	want := string(centerPad("SYSTEM FAILURE", len([]rune("SYSTEM FAILURE"))))
	if got != want {
		t.Errorf("MorphLine(frame=0) = %q, want exactly %q (from, centered)", got, want)
	}
}

func TestMorphLineAtOrPastTotalFramesIsExactlyTo(t *testing.T) {
	width := len([]rune("SYSTEM FAILURE"))
	want := string(centerPad("breakthrough", width))
	for _, frame := range []int{40, 41, 1000} {
		if got := MorphLine("SYSTEM FAILURE", "breakthrough", frame, 40, 42); got != want {
			t.Errorf("MorphLine(frame=%d) = %q, want exactly %q", frame, got, want)
		}
	}
}

func TestMorphLineSameInputsAreIdenticalAcrossCalls(t *testing.T) {
	a := MorphLine("SYSTEM FAILURE", "breakthrough", 15, 40, 42)
	b := MorphLine("SYSTEM FAILURE", "breakthrough", 15, 40, 42)
	if a != b {
		t.Errorf("MorphLine with identical inputs produced different output: %q vs %q", a, b)
	}
}

func TestMorphLineAgreeingPositionsNeverShowNoise(t *testing.T) {
	// Equal-length strings, so centerPad never shifts either one — a
	// shared "break" prefix lands at the same indices in both, which
	// must read as stable letters at every frame, never a noise
	// character, since from/to already agree there.
	for frame := 0; frame <= 20; frame++ {
		got := []rune(MorphLine("breakXXXXX", "breakYYYYY", frame, 20, 7))
		want := []rune("break")
		for i, w := range want {
			if got[i] != w {
				t.Errorf("frame %d: position %d = %q, want stable %q (from/to agree there)", frame, i, got[i], w)
			}
		}
	}
}

func TestMorphLineHandlesEqualLengthStrings(t *testing.T) {
	got := MorphLine("ABCDE", "VWXYZ", 1000, 20, 1)
	if got != "VWXYZ" {
		t.Errorf("MorphLine(frame past total) = %q, want %q", got, "VWXYZ")
	}
	got = MorphLine("ABCDE", "VWXYZ", 0, 20, 1)
	if got != "ABCDE" {
		t.Errorf("MorphLine(frame=0) = %q, want %q", got, "ABCDE")
	}
}
