package easteregg

import "testing"

func TestGlitchLineFrameZeroIsAllNoise(t *testing.T) {
	target := "Wake up, jens..."
	got := []rune(GlitchLine(target, 0, 30, 42))
	want := []rune(target)
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i, r := range got {
		if r == want[i] {
			t.Errorf("rune %d already resolved at frame 0: got %q, target rune %q", i, r, want[i])
		}
	}
}

func TestGlitchLineAtOrPastRevealFramesIsExactlyTarget(t *testing.T) {
	target := "Wake up, jens..."
	for _, frame := range []int{30, 31, 1000} {
		if got := GlitchLine(target, frame, 30, 42); got != target {
			t.Errorf("GlitchLine(frame=%d) = %q, want exactly %q", frame, got, target)
		}
	}
}

func TestGlitchLineSameInputsAreIdenticalAcrossCalls(t *testing.T) {
	target := "Wake up, jens..."
	a := GlitchLine(target, 7, 30, 42)
	b := GlitchLine(target, 7, 30, 42)
	if a != b {
		t.Errorf("GlitchLine with identical inputs produced different output: %q vs %q", a, b)
	}
}

func TestGlitchLineDifferentSeedsCanDiffer(t *testing.T) {
	target := "Wake up, jens..."
	a := GlitchLine(target, 3, 30, 1)
	b := GlitchLine(target, 3, 30, 2)
	if a == b {
		t.Error("two different seeds produced identical mid-animation output — suspiciously coincidental for this target/frame")
	}
}

func TestGlitchLineSurvivesANonASCIIUsername(t *testing.T) {
	target := "Wake up, jörg..."
	got := GlitchLine(target, 1000, 30, 42)
	if got != target {
		t.Errorf("GlitchLine(frame past reveal) = %q, want exactly %q (non-ASCII intact)", got, target)
	}
	// Also check an early frame doesn't panic or mangle the rune count
	// on a multi-byte-but-single-rune character ('ö').
	early := []rune(GlitchLine(target, 0, 30, 42))
	if len(early) != len([]rune(target)) {
		t.Errorf("GlitchLine(frame=0) rune count = %d, want %d (same as target, non-ASCII included)", len(early), len([]rune(target)))
	}
}

func TestGlitchLineEachCharacterResolvesNoEarlierThanItsOwnFrame(t *testing.T) {
	// Once a character has resolved at some frame f, it must stay
	// resolved at every later frame too (glitchResolveFrame is a fixed
	// point per character, not something frame can un-resolve).
	target := "breakthrough"
	revealFrames := 20
	resolvedAt := make([]int, len([]rune(target)))
	for i := range resolvedAt {
		resolvedAt[i] = -1
	}
	want := []rune(target)
	for frame := 0; frame <= revealFrames; frame++ {
		got := []rune(GlitchLine(target, frame, revealFrames, 7))
		for i, r := range got {
			if r == want[i] {
				if resolvedAt[i] == -1 {
					resolvedAt[i] = frame
				}
			} else if resolvedAt[i] != -1 {
				t.Errorf("character %d resolved at frame %d but un-resolved again at frame %d", i, resolvedAt[i], frame)
			}
		}
	}
	for i, f := range resolvedAt {
		if f == -1 {
			t.Errorf("character %d never resolved by frame %d", i, revealFrames)
		}
	}
}
