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

// Stages of the Easter egg's own animation, in order — see
// easterEggView.Draw's own per-stage branch and Root.animateEasterEgg's
// own stage-transition logic. All four are defined now, even though
// this phase only implements Intro and Rain: later phases add Draw
// cases and a transition out of Rain, never restructure this list.
const (
	easterEggStageIntro = iota
	easterEggStageRain
	easterEggStageFailure
	easterEggStageReveal
)

// easterEggView is breakthrough's hidden "follow the white rabbit"
// overlay — Phase 1+2 of feature_ideas.txt's own phased plan: trigger,
// intro glitch stage, and Matrix rain. SYSTEM FAILURE → breakthrough
// morph and the breakthrough reveal are later phases, extending this
// same type and its Draw rather than replacing either.
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

// Draw paints the current stage only — a full black field first
// (every stage sits on the same black background), then whichever
// stage-specific content stageFrame/frame select. Reads state only,
// never advances it (see Root.animateEasterEgg's own doc comment on
// why): a live terminal resize mid-animation must repaint the current
// frame at the new size, not accidentally skip frames.
func (v *easterEggView) Draw(screen tcell.Screen) {
	v.DrawForSubclass(screen, v)
	x, y, width, height := v.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}

	blank := tcell.StyleDefault.Background(tcell.ColorBlack)
	for row := y; row < y+height; row++ {
		for col := x; col < x+width; col++ {
			screen.SetContent(col, row, ' ', nil, blank)
		}
	}

	switch v.stage {
	case easterEggStageIntro:
		v.drawIntro(screen, x, y, width, height, blank)
	case easterEggStageRain:
		v.drawRain(screen, x, y, width, height, blank)
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
	if v.stage == easterEggStageIntro && v.stageFrame >= easterEggIntroRevealFrames+easterEggIntroHoldFrames {
		v.stage = easterEggStageRain
		v.stageFrame = 0
		_, _, width, height := v.GetInnerRect()
		v.rainColumns = newRainColumns(width, height, v.seed)
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
