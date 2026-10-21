package easteregg

import (
	"math"
)

// revealAspectCorrection compensates for a terminal cell being roughly
// twice as tall as it is wide — without scaling the vertical distance
// up by this factor, the growing opening would render as a wide
// ellipse instead of a round, torn hole.
const revealAspectCorrection = 2.0

// revealHarmonics/revealHarmonicBaseWeight shape the mask's own
// jagged boundary: revealHarmonics sine waves at increasing
// frequency, summed, so the edge reads as torn rock rather than a
// smooth circle. Weight decreases with frequency (baseWeight/h) so
// higher harmonics add fine roughness on top of the lower ones'
// broader waviness, rather than overriding them.
const (
	revealHarmonics          = 4
	revealHarmonicBaseWeight = 0.15
)

// revealPhaseSpread spreads seed across each harmonic's own
// independent phase (see revealHarmonicPhase) — an odd multiplier
// distinct from every other *Spread constant in this package (glitch.go,
// rain.go) purely so none of their pseudo-random sequences ever
// correlate merely for sharing the same outer seed.
const revealPhaseSpread = 0xA24BAED4

// revealJagSpread is the harmonic sum's own maximum possible magnitude
// — sum of revealHarmonicBaseWeight/h for h=1..revealHarmonics — used
// by RevealMask to size the mask's own effective max radius with
// enough headroom that even the most "pinched in" angle still fully
// covers every corner by the final frame, despite the boundary's own
// jaggedness dipping the radius multiplier below 1.0 in some
// directions.
var revealJagSpread = func() float64 {
	sum := 0.0
	for h := 1; h <= revealHarmonics; h++ {
		sum += revealHarmonicBaseWeight / float64(h)
	}
	return sum
}()

// revealPhaseHash is a splitmix64-style mix, used in place of a full
// math/rand.Source for revealHarmonicPhase: RevealMask calls it once
// per harmonic for every cell on every frame (the Reveal stage's own
// Draw repaints the whole field each tick), and constructing a real
// rand.Source that often — only to draw a single float64 from it and
// throw it away — was expensive enough to stretch each frame's real
// wall-clock time past easterEggFrameInterval, silently lengthening
// "breakthrough" and the signature caption's own holds past their
// intended 3 seconds each. This mix is a handful of arithmetic
// operations, no allocation, same determinism contract.
func revealPhaseHash(x uint64) uint64 {
	x += 0x9E3779B97F4A7C15
	x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
	x = (x ^ (x >> 27)) * 0x94D049BB133111EB
	return x ^ (x >> 31)
}

// revealHarmonicPhase deterministically picks harmonic h's own phase
// offset from seed — different phases per harmonic (and per seed) are
// what keep the jagged boundary looking organic rather than a fixed,
// instantly-recognizable wave pattern every time the egg runs.
func revealHarmonicPhase(h int, seed int64) float64 {
	mixed := revealPhaseHash(uint64(seed) ^ uint64(h)*revealPhaseSpread)
	return (float64(mixed>>11) / float64(uint64(1)<<53)) * 2 * math.Pi
}

// revealJag returns the boundary's own radius multiplier at angle
// (radians, 0 along the positive x-axis) — 1.0 on average, oscillating
// within [1-revealJagSpread, 1+revealJagSpread] via the summed sine
// harmonics.
func revealJag(angle float64, seed int64) float64 {
	jag := 1.0
	for h := 1; h <= revealHarmonics; h++ {
		phase := revealHarmonicPhase(h, seed)
		jag += (revealHarmonicBaseWeight / float64(h)) * math.Sin(angle*float64(h)+phase)
	}
	return jag
}

// RevealEdgeBand is how wide the "near the edge" zone is, as a
// fraction of the local jagged radius — see RevealMask's own doc
// comment on edgeCloseness.
const RevealEdgeBand = 0.12

// RevealMask reports whether (col, row) is inside the growing,
// irregular "torn wall" opening at frame (out of totalFrames, after
// which the mask covers the entire width x height field, corners
// included — guaranteed despite the boundary's own jaggedness, see
// revealJagSpread's own doc comment) and, when inside, how close to
// the boundary it is: edgeCloseness is 0 deep inside (fully revealed,
// nothing rough left to show) up to 1 right at the torn edge itself.
// A cell outside the mask always reports edgeCloseness 0 too — the
// caller only ever reads it when inside is true.
//
// Monotonic in frame for a fixed cell: once inside at some frame, a
// cell stays inside at every later frame too (the boundary only ever
// grows, never shrinks), the same guarantee GlitchLine's own
// per-character resolve frame gives.
func RevealMask(col, row, frame, totalFrames, width, height int, seed int64) (inside bool, edgeCloseness float64) {
	if totalFrames < 1 {
		totalFrames = 1
	}
	cx := float64(width) / 2
	cy := float64(height) / 2
	dx := float64(col) - cx
	dy := (float64(row) - cy) * revealAspectCorrection
	dist := math.Hypot(dx, dy)
	angle := math.Atan2(dy, dx)

	baseMaxRadius := math.Hypot(cx, cy*revealAspectCorrection)
	minJag := 1 - revealJagSpread
	if minJag < 0.01 {
		minJag = 0.01
	}
	effectiveMaxRadius := baseMaxRadius / minJag

	growth := float64(frame) / float64(totalFrames)
	if growth > 1 {
		growth = 1
	}
	threshold := growth * effectiveMaxRadius * revealJag(angle, seed)

	if dist > threshold {
		return false, 0
	}

	band := threshold * RevealEdgeBand
	if band < 0.5 {
		band = 0.5
	}
	ratio := (threshold - dist) / band
	switch {
	case ratio > 1:
		ratio = 1
	case ratio < 0:
		ratio = 0
	}
	return true, 1 - ratio
}
