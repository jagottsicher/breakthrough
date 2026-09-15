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

// TestConnectionGlowColorAtRestEqualsTheBaseColor pins the two
// instants within connectionGlowPeriod where the sine wave itself is
// exactly 0 (phase 0 and phase 0.5 of a 3-second period — 0ms and
// 1500ms) — brightness 0 means blendToward's own fraction is 0 on
// either branch, a no-op either way.
func TestConnectionGlowColorAtRestEqualsTheBaseColor(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	for _, ms := range []int64{0, 1500} {
		got := connectionGlowColor(theme, true, time.UnixMilli(ms))
		if got != theme.EntryExecutable {
			t.Errorf("connectionGlowColor at %dms = %v, want theme.EntryExecutable unblended", ms, got)
		}
	}
}

// TestConnectionGlowColorAtItsBrightestIsLighterThanTheBaseColor pins
// the brightest point of the cycle: sin(2*pi*0.25) = 1 — 750ms is 0.25
// of a 3-second period.
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

// TestConnectionGlowColorAtItsDimmestIsDarkerThanTheBaseColor is the
// opposite extreme: sin(2*pi*0.75) = -1 — 2250ms is 0.75 of a
// 3-second period. Per the user's own explicit report that the
// original, brighten-only design was invisible on their own terminal
// (almost certainly a non-truecolor one, where every step of a
// narrower swing quantized down to the same nearest palette color),
// the glow now swings below the base color too, not just above it.
func TestConnectionGlowColorAtItsDimmestIsDarkerThanTheBaseColor(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	base := theme.EntryExecutable
	dim := connectionGlowColor(theme, true, time.UnixMilli(2250))

	br, bg, bb := base.RGB()
	dr, dg, db := dim.RGB()
	if dr > br || dg > bg || db > bb {
		t.Errorf("dimmest color %v is not darker than the base color %v in every channel", dim, base)
	}
	if dim == base {
		t.Error("dimmest color equals the base color unchanged — the glow isn't actually animating")
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

func TestBlendTowardAtZeroReturnsTheColorUnchanged(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	if got := blendToward(theme.EntryExecutable, colorWhite, 0); got != theme.EntryExecutable {
		t.Errorf("blendToward(c, white, 0) = %v, want c unchanged", got)
	}
}

func TestBlendTowardAtOneReturnsExactlyTheTarget(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	if got := blendToward(theme.EntryExecutable, colorWhite, 1); got != colorWhite {
		t.Errorf("blendToward(c, white, 1) = %v, want pure white", got)
	}
	if got := blendToward(theme.EntryExecutable, colorBlack, 1); got != colorBlack {
		t.Errorf("blendToward(c, black, 1) = %v, want pure black", got)
	}
}

func TestBlendTowardClampsOutOfRangeFractions(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	if got, want := blendToward(theme.EntryExecutable, colorWhite, -1), theme.EntryExecutable; got != want {
		t.Errorf("blendToward(c, white, -1) = %v, want c unchanged (clamped to 0)", got)
	}
	if got := blendToward(theme.EntryExecutable, colorWhite, 2); got != colorWhite {
		t.Error("blendToward(c, white, 2) did not clamp to the target")
	}
}
