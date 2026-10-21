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

// easterEggView is breakthrough's hidden "follow the white rabbit"
// overlay — Phase 1 of feature_ideas.txt's own phased plan: the
// trigger (rabbit button, see panel.go, or the typed phrase, see
// cmd/breakthrough's own SetInputCapture feeding easteregg.
// PhraseDetector) and the intro glitch stage only. Later phases
// (Matrix rain, SYSTEM FAILURE → breakthrough morph, the breakthrough
// reveal) extend this same type and its Draw rather than replacing
// either.
//
// *tview.Box embedded purely for Draw's own per-cell control —
// nothing here tview itself can draw for us, the same reasoning
// verticalScrollbar's own doc comment gives for the identical choice
// (internal/ui/scrollbar.go).
type easterEggView struct {
	*tview.Box

	frame int
	seed  int64
	line  string // the fully-resolved target text, e.g. "Wake up, jens…"
}

func (r *Root) newEasterEggView() *easterEggView {
	v := &easterEggView{Box: tview.NewBox()}
	v.SetInputCapture(r.captureEasterEggKey)
	return v
}

// Draw paints a full black field, then the intro line mid-screen,
// resolving out of noise via easteregg.GlitchLine as frame advances
// (see Root.animateEasterEgg).
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

	text := []rune(easteregg.GlitchLine(v.line, v.frame, easterEggIntroRevealFrames, v.seed))
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

// openEasterEgg is the one entry point both triggers call — see
// panel.go's rabbitBtn click wiring and cmd/breakthrough's own
// phrase-detector feed. A second trigger while the overlay is already
// open (e.g. the phrase completing right as someone also clicks the
// button) is a no-op rather than restarting the animation from
// scratch.
func (r *Root) openEasterEgg() {
	if r.activePage == easterEggPage {
		return
	}
	r.easterEggView.frame = 0
	r.easterEggView.seed = time.Now().UnixNano()
	r.easterEggView.line = "Wake up, " + currentUsername() + "…"
	r.showOverlay(easterEggPage, r.easterEggView)

	ctx, cancel := context.WithCancel(context.Background())
	r.easterEggCancel = cancel
	r.safeGo("easter egg animation", r.closeEasterEgg, func() { r.animateEasterEgg(ctx) })
}

// animateEasterEgg advances easterEggView.frame every
// easterEggFrameInterval until ctx is cancelled — the same ticker-
// driven, context-cancelled shape animateSearchProgress (search.go)
// already establishes for exactly the same reason: nothing else would
// ever stop this ticker on its own, and search.go's own doc comment
// there documents the real bug that shape avoids (a ticker outliving
// the state it updates, silently overwriting what came after it).
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
				r.easterEggView.frame++
			})
		case <-ctx.Done():
			return
		}
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
// trigger's own rolling match (easteregg.PhraseDetector) — exported
// for cmd/breakthrough, the same reason AcceptsGlobalShortcut is (see
// its own doc comment in bottombar.go): main.go's SetInputCapture is
// outside this package.
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
