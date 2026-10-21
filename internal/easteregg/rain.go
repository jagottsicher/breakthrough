package easteregg

import "math/rand"

// RainIntensityLevels is how many distinct brightness steps a falling
// stream's own trail has — RainColumn.CellAt reports a cell's level in
// [1, RainIntensityLevels] (never 0; a cell outside any stream reports
// ok=false instead), the brightest, leading character at
// RainIntensityLevels, fading step by step down to 1 at the trail's
// faintest, oldest end.
const RainIntensityLevels = 6

// rainColumnSpread/rainCharSpread/rainCharFrameSpread spread a shared
// seed across independent pseudo-random sequences (per-column shape,
// per-cell character) — different odd multipliers from glitch.go's own
// glitchResolveSpread/glitchFrameSpread on purpose, so a rain column
// and a glitch character never happen to draw from a correlated
// sequence merely for sharing the same outer seed.
const (
	rainColumnSpread    = 0x9E3779B1
	rainCharSpread      = 0x85EBCA77
	rainCharFrameSpread = 0xC2B2AE3D
)

// RainCharChangeInterval is how many frames a still-unresolved rain
// character holds before flickering to a new one — every single frame
// would read as pure noise rather than falling streams; holding too
// long would read as frozen. Tuned by eye once this renders for real
// (see this feature's own PR), not derived from anything else.
const RainCharChangeInterval = 4

// RainColumn is one falling character stream's own deterministic
// shape — speed, trail length, and the full cycle (fall + the blank
// gap before it starts again) all vary per column, computed once from
// (col, height, seed) and independent of frame, so a caller builds one
// per column per screen size and reuses it across every frame rather
// than recomputing it per cell.
//
// CycleLen deliberately varies per column (not just Speed/Phase): a
// shared cycle length would still let every column's own head
// resynchronize into visible bands every few cycles, however random
// Phase/Speed are individually — different, column-specific cycle
// lengths (via each column's own randomized extra gap) keep the whole
// screen desynchronized indefinitely instead.
type RainColumn struct {
	Speed    int
	TrailLen int
	CycleLen int
	Phase    int
}

// NewRainColumn builds col's own deterministic falling-stream shape.
// height only scales the ranges Speed/TrailLen/the extra gap are drawn
// from, so a taller terminal gets proportionally longer trails and
// gaps rather than looking sparse.
func NewRainColumn(col, height int, seed int64) RainColumn {
	if height < 1 {
		height = 1
	}
	r := rand.New(rand.NewSource(seed ^ int64(col)*rainColumnSpread))
	speed := 1 + r.Intn(3) // 1..3 rows per frame
	trailLen := height/3 + r.Intn(height/3+1)
	if trailLen < 1 {
		trailLen = 1
	}
	extraGap := r.Intn(height/2 + 1)
	return RainColumn{
		Speed:    speed,
		TrailLen: trailLen,
		CycleLen: height + trailLen + extraGap,
		Phase:    r.Intn(height + trailLen + extraGap),
	}
}

// HeadRow is the stream's own leading (brightest, most recently fallen
// into) row at frame — can be negative (not yet entered from the top)
// or past height-1 (already exited the bottom, trail still fading out
// above it) by design; CellAt's own bounds check, not this method, is
// what decides whether any given row is actually visible right now.
func (c RainColumn) HeadRow(frame int) int {
	cycle := c.CycleLen
	if cycle < 1 {
		cycle = 1
	}
	pos := (c.Phase + frame*c.Speed) % cycle
	return pos - c.TrailLen
}

// CellAt reports row's own state within this column at frame: ok is
// false for a row outside the current stream entirely (nothing to
// draw — the caller should fall through to a plain background cell).
// When ok, level is this cell's brightness in [1, RainIntensityLevels]
// (see its own doc comment) and ch is the character currently showing
// there, re-rolled every RainCharChangeInterval frames so it flickers
// rather than sitting frozen or changing every single frame. col/seed
// are needed again here (not just in NewRainColumn) purely to seed
// ch's own independent per-cell sequence.
func (c RainColumn) CellAt(col, row, frame int, seed int64) (ch rune, level int, ok bool) {
	head := c.HeadRow(frame)
	if row > head || row <= head-c.TrailLen {
		return 0, 0, false
	}
	dist := head - row // 0 at the head, up to TrailLen-1 at the faintest visible row
	denom := c.TrailLen - 1
	if denom < 1 {
		denom = 1
	}
	level = RainIntensityLevels - (dist*(RainIntensityLevels-1))/denom
	if level < 1 {
		level = 1
	}
	charFrame := frame / RainCharChangeInterval
	r := rand.New(rand.NewSource(seed ^ int64(col)*rainColumnSpread ^ int64(row)*rainCharSpread ^ int64(charFrame)*rainCharFrameSpread))
	ch = rune(glitchNoiseChars[r.Intn(len(glitchNoiseChars))])
	return ch, level, true
}
