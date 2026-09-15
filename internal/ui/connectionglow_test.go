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

// TestConnectionGlowColorAtRestEqualsTheBaseColor pins the one instant
// within connectionGlowPeriod where (1-cos(x))/2 is exactly 0 — phase
// 0 of a 3-second period, i.e. 0ms (and, equivalently, a full period
// later). Unlike the two-rest-point sine this replaced, there is now
// only one rest point per cycle — the halfway point is the peak
// instead (see TestConnectionGlowColorAtItsBrightestIsLighterThanTheBaseColor).
func TestConnectionGlowColorAtRestEqualsTheBaseColor(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	for _, ms := range []int64{0, connectionGlowPeriod.Milliseconds()} {
		got := connectionGlowColor(theme, true, time.UnixMilli(ms))
		if got != theme.EntryExecutable {
			t.Errorf("connectionGlowColor at %dms = %v, want theme.EntryExecutable unblended", ms, got)
		}
	}
}

// TestConnectionGlowColorAtItsBrightestIsLighterThanTheBaseColor pins
// the brightest point of the cycle: (1-cos(pi))/2 = 1 at phase 0.5 —
// 1500ms is half of a 3-second period.
func TestConnectionGlowColorAtItsBrightestIsLighterThanTheBaseColor(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	base := theme.EntryExecutable
	bright := connectionGlowColor(theme, true, time.UnixMilli(1500))

	br, bg, bb := base.RGB()
	pr, pg, pb := bright.RGB()
	if pr < br || pg < bg || pb < bb {
		t.Errorf("brightest color %v is not lighter than the base color %v in every channel", bright, base)
	}
	if bright == base {
		t.Error("brightest color equals the base color unchanged — the glow isn't actually animating")
	}
}

// TestConnectionGlowColorNeverDipsBelowTheBaseColor pins the user's
// own explicit correction: a connected glow that darkens toward black
// on part of its cycle reads as a modem/router's own "still searching
// for a signal" light, not a settled, already-alive connection — so
// the color at every phase of the cycle must be at least as bright as
// the base color in every channel, never a darker shade of it.
func TestConnectionGlowColorNeverDipsBelowTheBaseColor(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	base := theme.EntryExecutable
	br, bg, bb := base.RGB()

	period := connectionGlowPeriod.Milliseconds()
	for ms := int64(0); ms < period; ms += 50 {
		got := connectionGlowColor(theme, true, time.UnixMilli(ms))
		gr, gg, gb := got.RGB()
		if gr < br || gg < bg || gb < bb {
			t.Fatalf("connectionGlowColor at %dms = %v is darker than the base color %v in at least one channel", ms, got, base)
		}
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
