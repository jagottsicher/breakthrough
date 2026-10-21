package ui

import (
	"context"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/easteregg"
)

const easterEggPage = "easterEgg"

// easterEggSentinelPath is the fake "file" the Easter egg's Find
// trigger uses (see Panel.appendEasterEggSearchResult, Root.openLook,
// Panel.activateRow, and Root.loadDetailsTarget — every place that
// needs to recognize this one synthetic row rather than treat it as a
// real path) — NUL-prefixed, which a real POSIX path can never
// contain (the kernel itself rejects one), so collision with any real
// file is impossible by construction, not merely unlikely.
const easterEggSentinelPath = "\x00easteregg-follow-the-white-rabbit"

// easterEggSentinelLabel is this fake row's own display name in the
// search results list.
const easterEggSentinelLabel = "Follow the white rabbit"

// easterEggDetailsText is what the Details sidebar shows for
// easterEggSentinelPath — a fixed, decorative block rather than a
// failed stat, per the user's own explicit request.
const easterEggDetailsText = "🐇 " + easterEggSentinelLabel + "\n\nThere is no path here — only a choice.\n\nPress \"l\" to see how deep it goes."

// easterEggFrameInterval is the animation's own target frame rate —
// see feature_ideas.txt's "Easter Egg" section, "Laufzeit-
// Eigenschaften" (~50-80ms).
const easterEggFrameInterval = 60 * time.Millisecond

// easterEggIntroRevealFrames is how many frames the intro's own
// "Wake up, <username>…" line takes to fully resolve out of noise —
// at easterEggFrameInterval this is roughly 2.5 seconds.
const easterEggIntroRevealFrames = 42

// easterEggIntroHoldFrames is how much longer the intro stays on
// screen, fully resolved, before the Matrix rain stage begins — about
// 1.2 more seconds at easterEggFrameInterval, long enough to actually
// read "Wake up, <username>…" before it's gone.
const easterEggIntroHoldFrames = 20

// easterEggRainFrames is how long the Matrix rain runs, from entering
// the Rain stage to the abrupt "SYSTEM FAILURE" cutoff — about 4.2
// seconds at easterEggFrameInterval. Originally 150 (~9s); cut after
// the user's own explicit report that it ran far too long, enough that
// "SYSTEM FAILURE" right after it went unnoticed — a shorter rain
// keeps attention through to the stage that actually matters.
const easterEggRainFrames = 70

// easterEggFailureHoldFrames is how long "SYSTEM FAILURE" sits on
// screen, static, before it starts morphing into "breakthrough" —
// exactly 3 seconds, per the user's own explicit request (after an
// earlier, shorter hold already got lengthened once from 1.5s to
// 2.1s, following a report that it read as too brief a beat to
// register at all).
const easterEggFailureHoldFrames = 50

// easterEggMorphFrames is how many frames the "SYSTEM FAILURE" →
// "breakthrough" character morph itself takes — about 2.4 seconds,
// the same pace as the intro's own reveal (easterEggIntroRevealFrames)
// so the two read as the same kind of event.
const easterEggMorphFrames = 40

// easterEggFailurePostMorphHoldFrames is how long the completed,
// fully-green "breakthrough" text sits still after the morph finishes
// — about 1.2 seconds, long enough to actually land as "it worked"
// before the wall itself tears open.
const easterEggFailurePostMorphHoldFrames = 20

// easterEggFailureText/easterEggBreakthroughText are the two anchor
// strings easteregg.MorphLine transitions between.
const (
	easterEggFailureText      = "SYSTEM FAILURE"
	easterEggBreakthroughText = "breakthrough"
)

// easterEggRevealFrames is how long the torn-wall opening takes to
// grow from nothing to covering the entire screen — about 1.9 seconds.
// Originally 50 frames (~3s); shortened after the user's own explicit
// request that the reveal itself feel a bit quicker — a "breakthrough"
// should feel like it arrives suddenly once it starts, more so than
// the first cut already aimed for.
const easterEggRevealFrames = 32

// easterEggCaptionDelayFrames is how long after "breakthrough" itself
// appears (stageFrame reaching easterEggRevealFrames) the signature
// line fades in beneath it — exactly 3 seconds at
// easterEggFrameInterval, per the user's own explicit request.
const easterEggCaptionDelayFrames = 50

// easterEggCaptionHoldFrames is how long the signature line stays up
// once it appears before the whole overlay closes — exactly 3 seconds,
// per the user's own explicit request, the same duration as
// "breakthrough" itself gets (easterEggCaptionDelayFrames) and
// "SYSTEM FAILURE" gets (easterEggFailureHoldFrames): every beat in
// the closing sequence reads for the same length now.
const easterEggCaptionHoldFrames = 50

// easterEggRevealHoldFrames is how long the fully-revealed, bright
// field holds — "breakthrough", then the signature line two rows below
// it once easterEggCaptionDelayFrames has passed — before the whole
// overlay closes on its own — the only stage with a real ending (see
// easterEggStageReveal's own doc comment): Escape still skips out at
// any point, same as every other stage, but this one doesn't need it
// to finish.
const easterEggRevealHoldFrames = easterEggCaptionDelayFrames + easterEggCaptionHoldFrames

// easterEggCaptionText is the signature line shown beneath
// "breakthrough" once the reveal settles — per the user's own explicit
// request.
const easterEggCaptionText = "written with ♥ in Go by Jens Schendel"

// Stages of the Easter egg's own animation, in order — see
// easterEggView.Draw's own per-stage branch and Root.animateEasterEgg's
// own stage-transition logic. All four are defined now, even though
// this phase implements all four — Reveal, the last of them, ends by
// closing the overlay on its own (see easterEggRevealHoldFrames)
// rather than transitioning onward.
const (
	easterEggStageIntro = iota
	easterEggStageRain
	easterEggStageFailure
	easterEggStageReveal
)

// easterEggView is breakthrough's hidden "follow the white rabbit"
// overlay — the complete, four-phase animation from feature_ideas.txt's
// own "Easter Egg" section: trigger, intro glitch stage, Matrix rain,
// the SYSTEM FAILURE → breakthrough morph, and finally the torn-wall
// breakthrough reveal.
//
// *tview.Box embedded purely for Draw's own per-cell control —
// nothing here tview itself can draw for us, the same reasoning
// verticalScrollbar's own doc comment gives for the identical choice
// (internal/ui/scrollbar.go).
type easterEggView struct {
	*tview.Box

	seed int64
	line string // the intro's fully-resolved target text, e.g. "Wake up, jens…"

	stage      int
	frame      int // advances every tick for as long as the overlay is open, never reset between stages — rainColumns' own HeadRow needs a continuously advancing frame, not one that jumps back to 0 at every stage change
	stageFrame int // frames spent in the current stage alone — what Draw's own per-stage branch actually reads

	// rainColumns is built once, the moment the Rain stage begins (see
	// Root.animateEasterEgg), and reused every frame after that —
	// NewRainColumn is pure and independent of frame, so there's
	// nothing to gain from recomputing it per Draw call, only CPU to
	// waste. Rebuilt on the fly by Draw itself if the screen's own
	// width no longer matches (a live terminal resize mid-animation),
	// which keeps Draw a pure function of current state rather than
	// needing a resize to also route through the ticker.
	rainColumns []easteregg.RainColumn
}

func (r *Root) newEasterEggView() *easterEggView {
	v := &easterEggView{Box: tview.NewBox()}
	v.SetInputCapture(r.captureEasterEggKey)
	return v
}

// easterEggRainPalette maps a RainColumn.CellAt level (1..RainIntensityLevels)
// to the color it renders in — index 0 is unused (CellAt never returns
// level 0; see its own doc comment), brightest near-white-green at the
// head down to a dim, near-background green at the trail's faintest
// end. Plain tcell.Color constants, not config.ResolvedTheme fields:
// this overlay is deliberately its own fixed black-and-green world,
// the same way the Matrix's own visual language always is, independent
// of whichever color scheme the rest of the app is currently using.
var easterEggRainPalette = [easteregg.RainIntensityLevels + 1]tcell.Color{
	1: tcell.NewRGBColor(0, 40, 0),
	2: tcell.NewRGBColor(0, 70, 0),
	3: tcell.NewRGBColor(0, 110, 0),
	4: tcell.NewRGBColor(20, 160, 20),
	5: tcell.NewRGBColor(120, 220, 120),
	6: tcell.NewRGBColor(220, 255, 220),
}

// easterEggFailureColor/easterEggBreakthroughColor are drawFailure's
// own two endpoint colors — "SYSTEM FAILURE" in alarm red, morphing
// through blendToward (panel.go) into "breakthrough" in the same
// bright green easterEggRainPalette's own head color already uses, so
// the morph's own finishing color reads as a continuation of the rain
// that just stopped, not an unrelated third color introduced only here.
var (
	easterEggFailureColor      = tcell.NewRGBColor(255, 50, 50)
	easterEggBreakthroughColor = easterEggRainPalette[easteregg.RainIntensityLevels]
)

// easterEggRockColor/easterEggAmberColor/easterEggWhiteColor are
// drawReveal's own palette — dark, unlit rock outside the growing
// opening, warming from amber to near-white moving in from its own
// jagged edge, the same "torn wall, sunlit landscape beyond it" image
// the README's own hero picture (docs/images/) shows, reduced to what
// a character grid can actually render.
var (
	easterEggRockColor  = tcell.NewRGBColor(32, 28, 26)
	easterEggAmberColor = tcell.NewRGBColor(214, 140, 40)
	easterEggWhiteColor = tcell.NewRGBColor(255, 248, 230)
)

// easterEggEdgeGlyphs are drawReveal's own half-block shading
// characters for the mask's own jagged boundary band — from mostly
// rock (sparse shading, still reads as the dark field behind it)
// closest to the edge, to mostly light (dense shading) just inside it,
// a torn rather than a smooth, anti-aliased-looking edge. The same
// kind of half-block technique detailsiconbox.go's own diagonal fold
// already establishes for this project, applied to an irregular
// boundary instead of a straight diagonal one.
var easterEggEdgeGlyphs = []rune{'░', '▒', '▓'}

// Draw paints the current stage only — a full base field first (black
// for Intro/Rain/Failure; drawReveal paints its own dark-rock base
// instead, see its own doc comment), then whichever stage-specific
// content stageFrame/frame select. Reads state only, never advances
// it (see Root.animateEasterEgg's own doc comment on why): a live
// terminal resize mid-animation must repaint the current frame at the
// new size, not accidentally skip frames.
func (v *easterEggView) Draw(screen tcell.Screen) {
	v.DrawForSubclass(screen, v)
	x, y, width, height := v.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}

	blank := tcell.StyleDefault.Background(tcell.ColorBlack)
	if v.stage != easterEggStageReveal {
		for row := y; row < y+height; row++ {
			for col := x; col < x+width; col++ {
				screen.SetContent(col, row, ' ', nil, blank)
			}
		}
	}

	switch v.stage {
	case easterEggStageIntro:
		v.drawIntro(screen, x, y, width, height, blank)
	case easterEggStageRain:
		v.drawRain(screen, x, y, width, height, blank)
	case easterEggStageFailure:
		v.drawFailure(screen, x, y, width, height, blank)
	case easterEggStageReveal:
		v.drawReveal(screen, x, y, width, height)
	}
}

// drawIntro paints the glitching, then fully-resolved, "Wake up,
// <username>…" line — stageFrame-driven (not frame), so the reveal
// timing is always relative to when the Intro stage itself began.
func (v *easterEggView) drawIntro(screen tcell.Screen, x, y, width, height int, blank tcell.Style) {
	text := []rune(easteregg.GlitchLine(v.line, v.stageFrame, easterEggIntroRevealFrames, v.seed))
	if len(text) == 0 {
		return
	}
	textX := x + (width-len(text))/2
	textY := y + height/2
	style := blank.Foreground(tcell.ColorGreen)
	for i, ch := range text {
		col := textX + i
		if col < x || col >= x+width {
			continue
		}
		screen.SetContent(col, textY, ch, nil, style)
	}
}

// drawRain paints the falling-stream field — rainColumns is rebuilt
// here (not just in animateEasterEgg) whenever its own length no
// longer matches width, so a live terminal resize mid-rain is
// reflected immediately rather than drawing against a stale column
// count or leaving newly-exposed columns blank.
func (v *easterEggView) drawRain(screen tcell.Screen, x, y, width, height int, blank tcell.Style) {
	if len(v.rainColumns) != width {
		v.rainColumns = newRainColumns(width, height, v.seed)
	}
	for col := 0; col < width; col++ {
		for row := 0; row < height; row++ {
			ch, level, ok := v.rainColumns[col].CellAt(col, row, v.frame, v.seed)
			if !ok {
				continue
			}
			style := blank.Foreground(easterEggRainPalette[level])
			screen.SetContent(x+col, y+row, ch, nil, style)
		}
	}
}

// drawFailure paints the Failure stage's own two beats: "SYSTEM
// FAILURE" held static in red for easterEggFailureHoldFrames, then
// morphing character by character into "breakthrough" over
// easterEggMorphFrames (easteregg.MorphLine), its color sliding from
// red to green across that same span (blendToward, panel.go). Once
// the morph completes, stageFrame keeps advancing but both the text
// and the color stay at their final values — "breakthrough", fully
// green — since nothing beyond this stage exists yet (see
// easterEggStageReveal's own doc comment on the struct): the egg
// simply holds there until Escape, the same honest "nothing further
// to show" boundary Phase 1 ended the Intro stage on.
func (v *easterEggView) drawFailure(screen tcell.Screen, x, y, width, height int, blank tcell.Style) {
	morphFrame := v.stageFrame - easterEggFailureHoldFrames
	var text string
	var color tcell.Color
	switch {
	case morphFrame < 0:
		text = easterEggFailureText
		color = easterEggFailureColor
	default:
		text = easteregg.MorphLine(easterEggFailureText, easterEggBreakthroughText, morphFrame, easterEggMorphFrames, v.seed)
		t := float64(morphFrame) / float64(easterEggMorphFrames)
		color = blendToward(easterEggFailureColor, easterEggBreakthroughColor, t)
	}

	runes := []rune(text)
	if len(runes) == 0 {
		return
	}
	textX := x + (width-len(runes))/2
	textY := y + height/2
	style := blank.Foreground(color)
	for i, ch := range runes {
		col := textX + i
		if col < x || col >= x+width || ch == ' ' {
			continue
		}
		screen.SetContent(col, textY, ch, nil, style)
	}
}

// drawReveal paints the growing, jagged "torn wall" opening
// (easteregg.RevealMask) — dark rock everywhere outside it, warming
// from amber (right at the ragged edge, rendered with a half-block
// shading glyph — easterEggEdgeGlyphs) to near-white (deep inside,
// a solid colored cell, nothing left to shade) the further a cell
// sits from the boundary. Once the mask has grown to cover the whole
// field (stageFrame >= easterEggRevealFrames), "breakthrough" is
// stenciled across the middle in the same dark rock color, the one
// dark shape left against all that light — and, easterEggCaptionDelayFrames
// later, easterEggCaptionText fades in the same way two rows below it.
func (v *easterEggView) drawReveal(screen tcell.Screen, x, y, width, height int) {
	rock := tcell.StyleDefault.Background(easterEggRockColor)
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			inside, edge := easteregg.RevealMask(col, row, v.stageFrame, easterEggRevealFrames, width, height, v.seed)
			if !inside {
				screen.SetContent(x+col, y+row, ' ', nil, rock)
				continue
			}
			switch {
			case edge >= 0.3:
				// Near the boundary: a half-block shading glyph in the
				// warmed foreground color, on the dark rock background
				// still showing through it — bucketed into
				// easterEggEdgeGlyphs' own three steps across the
				// remaining [0.3, 1.0] edge range.
				bucket := int((edge - 0.3) / 0.7 * float64(len(easterEggEdgeGlyphs)))
				if bucket >= len(easterEggEdgeGlyphs) {
					bucket = len(easterEggEdgeGlyphs) - 1
				}
				glyph := easterEggEdgeGlyphs[len(easterEggEdgeGlyphs)-1-bucket]
				color := blendToward(easterEggAmberColor, easterEggWhiteColor, 1-edge/0.3)
				screen.SetContent(x+col, y+row, glyph, nil, rock.Foreground(color))
			default:
				depth := 1 - edge/0.3
				color := blendToward(easterEggAmberColor, easterEggWhiteColor, depth)
				screen.SetContent(x+col, y+row, ' ', nil, tcell.StyleDefault.Background(color))
			}
		}
	}

	if v.stageFrame < easterEggRevealFrames {
		return
	}
	textY := y + height/2
	style := tcell.StyleDefault.Background(easterEggWhiteColor).Foreground(easterEggRockColor)
	drawStencilLine(screen, easterEggBreakthroughText, x, textY, width, style)

	if v.stageFrame < easterEggRevealFrames+easterEggCaptionDelayFrames {
		return
	}
	drawStencilLine(screen, easterEggCaptionText, x, textY+2, width, style)
}

// drawStencilLine centers text on row, the same dark-rock-on-bright
// "stencil" look drawReveal's own "breakthrough" text and
// easterEggCaptionText share — split out once there were two lines
// using it rather than duplicating the centering/clipping math.
func drawStencilLine(screen tcell.Screen, text string, x, row, width int, style tcell.Style) {
	runes := []rune(text)
	textX := x + (width-len(runes))/2
	for i, ch := range runes {
		col := textX + i
		if col < x || col >= x+width {
			continue
		}
		screen.SetContent(col, row, ch, nil, style)
	}
}

func newRainColumns(width, height int, seed int64) []easteregg.RainColumn {
	cols := make([]easteregg.RainColumn, width)
	for i := range cols {
		cols[i] = easteregg.NewRainColumn(i, height, seed)
	}
	return cols
}

// openEasterEgg is the one entry point the Find trigger calls — see
// Panel.activateRow (Enter) and Root.openLook ("l"), both checking
// against easterEggSentinelPath. A second trigger while the overlay is
// already open is a no-op rather than restarting the animation from
// scratch.
func (r *Root) openEasterEgg() {
	if r.activePage == easterEggPage {
		return
	}
	r.easterEggView.stage = easterEggStageIntro
	r.easterEggView.frame = 0
	r.easterEggView.stageFrame = 0
	r.easterEggView.seed = time.Now().UnixNano()
	r.easterEggView.line = "Wake up, " + currentUsername() + "…"
	r.easterEggView.rainColumns = nil
	r.showOverlay(easterEggPage, r.easterEggView)

	ctx, cancel := context.WithCancel(context.Background())
	r.easterEggCancel = cancel
	r.safeGo("easter egg animation", r.closeEasterEgg, func() { r.animateEasterEgg(ctx) })
}

// animateEasterEgg advances easterEggView.frame/stageFrame every
// easterEggFrameInterval until ctx is cancelled, and owns every stage
// transition (never Draw — see its own doc comment on why): a resize
// forces a redraw at any time, but must never itself advance the
// animation or skip a stage. The same ticker-driven, context-cancelled
// shape animateSearchProgress (search.go) already establishes, for
// exactly the same reason: nothing else would ever stop this ticker on
// its own, and search.go's own doc comment there documents the real
// bug that shape avoids (a ticker outliving the state it updates,
// silently overwriting what came after it).
func (r *Root) animateEasterEgg(ctx context.Context) {
	ticker := time.NewTicker(easterEggFrameInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			r.app.QueueUpdateDraw(func() {
				if ctx.Err() != nil {
					return
				}
				r.tickEasterEggFrame()
			})
		case <-ctx.Done():
			return
		}
	}
}

// tickEasterEggFrame advances easterEggView by exactly one frame and
// runs any stage transition that frame triggers — split out from
// animateEasterEgg's own ticker loop so a test can call it directly,
// instantly simulating N frames, instead of waiting on
// easterEggFrameInterval N times for real (see this file's own test
// for exactly that).
func (r *Root) tickEasterEggFrame() {
	v := r.easterEggView
	v.frame++
	v.stageFrame++
	switch {
	case v.stage == easterEggStageIntro && v.stageFrame >= easterEggIntroRevealFrames+easterEggIntroHoldFrames:
		v.stage = easterEggStageRain
		v.stageFrame = 0
		_, _, width, height := v.GetInnerRect()
		v.rainColumns = newRainColumns(width, height, v.seed)
	case v.stage == easterEggStageRain && v.stageFrame >= easterEggRainFrames:
		v.stage = easterEggStageFailure
		v.stageFrame = 0
	case v.stage == easterEggStageFailure && v.stageFrame >= easterEggFailureHoldFrames+easterEggMorphFrames+easterEggFailurePostMorphHoldFrames:
		v.stage = easterEggStageReveal
		v.stageFrame = 0
	case v.stage == easterEggStageReveal && v.stageFrame >= easterEggRevealFrames+easterEggRevealHoldFrames:
		// The only stage with a real ending (see easterEggStageReveal's
		// own doc comment) — closes itself rather than holding
		// indefinitely like every stage before it.
		r.closeEasterEgg()
	}
}

// closeEasterEgg is the single place that ever cancels the animation
// context — called on Escape (captureEasterEggKey), and as safeGo's
// own onPanic if animateEasterEgg itself ever somehow panics, so the
// overlay never gets left showing a frozen frame either way.
func (r *Root) closeEasterEgg() {
	if r.easterEggCancel != nil {
		r.easterEggCancel()
		r.easterEggCancel = nil
	}
	r.hideOverlay()
}

// FeedEasterEggPhrase feeds one candidate rune to the global typed-
// trigger's own rolling match (easteregg.PhraseDetector) — not wired
// into cmd/breakthrough's own input capture (see feature_ideas.txt's
// "Easter Egg" section: the typed phrase turned out to collide with
// "f" opening Find and was replaced by the Find-based trigger), kept
// here because it's pure, tested, and cheap, in case a non-colliding
// phrase is chosen later.
//
// Inert whenever AcceptsGlobalShortcut itself would refuse (any
// overlay open, or the bash line focused) — not just as a courtesy:
// without this, typing the phrase into the filter field, a dialog's
// own text field, or the bash line would silently open the Easter egg
// as a side effect of typing something else entirely. Only a plain,
// unmodified rune is ever fed; every other key (arrows, function keys,
// Ctrl/Alt combinations) leaves the detector's own partial match
// alone rather than resetting it — matches PhraseDetector's own
// contract of only ever reacting to runes that could plausibly be
// part of the phrase itself.
func (r *Root) FeedEasterEggPhrase(event *tcell.EventKey) {
	if !r.AcceptsGlobalShortcut() {
		return
	}
	if event.Key() != tcell.KeyRune || event.Modifiers() != tcell.ModNone {
		return
	}
	if r.easterEggPhrase.Feed(event.Rune()) {
		r.openEasterEgg()
	}
}

// captureEasterEggKey consumes every key while the overlay is open —
// Escape closes it, everything else is simply swallowed: a full-screen
// animation has nothing underneath it to usefully forward a keystroke
// to.
func (r *Root) captureEasterEggKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeEasterEgg()
	}
	return nil
}
