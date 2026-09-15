package ui

import (
	"testing"
	"time"

	"github.com/jagottsicher/breakthrough/internal/config"
)

func TestConnectionGlowColorIsMutedWhenNotConnectedRegardlessOfTime(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	for _, ms := range []int64{0, 500, 1500, 3000, 123456} {
		got := connectionGlowColor(theme, false, time.UnixMilli(ms))
		if got != theme.MutedTextColor {
			t.Errorf("connectionGlowColor(false, %dms) = %v, want theme.MutedTextColor", ms, got)
		}
	}
}

// TestConnectionGlowColorAtItsDimmestPointEqualsTheBaseColor pins the
// exact instant within connectionGlowPeriod where the sine wave's own
// brightness term bottoms out at 0 — sin(2*pi*0.75) = -1, so
// brightness = (1-1)/2 = 0 and blendTowardWhite is a no-op. 2250ms is
// 0.75 of a 3-second period.
func TestConnectionGlowColorAtItsDimmestPointEqualsTheBaseColor(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	got := connectionGlowColor(theme, true, time.UnixMilli(2250))
	if got != theme.EntryExecutable {
		t.Errorf("connectionGlowColor at its dimmest = %v, want theme.EntryExecutable unblended", got)
	}
}

// TestConnectionGlowColorAtItsBrightestIsLighterThanTheBaseColor pins
// the opposite extreme: sin(2*pi*0.25) = 1, brightness = 1, the
// brightest point of the cycle — 750ms is 0.25 of a 3-second period.
func TestConnectionGlowColorAtItsBrightestIsLighterThanTheBaseColor(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	base := theme.EntryExecutable
	bright := connectionGlowColor(theme, true, time.UnixMilli(750))

	br, bg, bb := base.RGB()
	pr, pg, pb := bright.RGB()
	if pr < br || pg < bg || pb < bb {
		t.Errorf("brightest color %v is not lighter than the base color %v in every channel", bright, base)
	}
	if bright == base {
		t.Error("brightest color equals the base color unchanged — the glow isn't actually animating")
	}
}

// TestConnectionGlowColorIsPeriodic confirms the breathing motion
// actually repeats every connectionGlowPeriod rather than drifting.
func TestConnectionGlowColorIsPeriodic(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	t0 := time.UnixMilli(400)
	t1 := time.UnixMilli(400 + connectionGlowPeriod.Milliseconds())
	if got, want := connectionGlowColor(theme, true, t0), connectionGlowColor(theme, true, t1); got != want {
		t.Errorf("color one full period later = %v, want the same as at t0 (%v)", got, want)
	}
}

func TestBlendTowardWhiteAtZeroReturnsTheColorUnchanged(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	if got := blendTowardWhite(theme.EntryExecutable, 0); got != theme.EntryExecutable {
		t.Errorf("blendTowardWhite(c, 0) = %v, want c unchanged", got)
	}
}

func TestBlendTowardWhiteAtOneReturnsPureWhite(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	got := blendTowardWhite(theme.EntryExecutable, 1)
	r, g, b := got.RGB()
	if r != 255 || g != 255 || b != 255 {
		t.Errorf("blendTowardWhite(c, 1) = %v, want pure white", got)
	}
}

func TestBlendTowardWhiteClampsOutOfRangeFractions(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	if got, want := blendTowardWhite(theme.EntryExecutable, -1), theme.EntryExecutable; got != want {
		t.Errorf("blendTowardWhite(c, -1) = %v, want c unchanged (clamped to 0)", got)
	}
	r, g, b := blendTowardWhite(theme.EntryExecutable, 2).RGB()
	if r != 255 || g != 255 || b != 255 {
		t.Error("blendTowardWhite(c, 2) did not clamp to pure white")
	}
}
