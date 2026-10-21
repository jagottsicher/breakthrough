package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/easteregg"
)

func TestOpenEasterEggShowsTheOverlayAndStartsAnimating(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.openEasterEgg()

	if r.activePage != easterEggPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, easterEggPage)
	}
	if r.easterEggCancel == nil {
		t.Error("easterEggCancel should be set once the overlay is open")
	}
	if r.easterEggView.line == "" {
		t.Error("easterEggView.line should be set to the intro text once opened")
	}

	r.closeEasterEgg() // tidy up this test's own background goroutine
}

func TestOpenEasterEggTwiceWhileAlreadyOpenIsANoop(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openEasterEgg()
	firstSeed := r.easterEggView.seed

	r.openEasterEgg() // second trigger while already open

	if r.easterEggView.seed != firstSeed {
		t.Error("a second trigger while already open should not restart the animation (seed changed)")
	}

	r.closeEasterEgg()
}

func TestEscapeClosesEasterEggAndStopsTheAnimation(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openEasterEgg()
	if r.activePage != easterEggPage {
		t.Fatalf("setup: easter egg should be open")
	}

	r.captureEasterEggKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	if r.activePage == easterEggPage {
		t.Error("Escape should close the easter egg overlay")
	}
	if r.easterEggCancel != nil {
		t.Error("easterEggCancel should be nil again after Escape closes the overlay")
	}
}

func TestCaptureEasterEggKeyConsumesEveryKey(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openEasterEgg()
	defer r.closeEasterEgg()

	if got := r.captureEasterEggKey(tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone)); got != nil {
		t.Errorf("captureEasterEggKey should consume every non-Escape key too, got %v", got)
	}
	if r.activePage != easterEggPage {
		t.Error("a non-Escape key should not close the overlay")
	}
}

func TestFeedEasterEggPhraseOpensTheEggOnCompletion(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	for _, ru := range easteregg.TriggerPhrase {
		r.FeedEasterEggPhrase(tcell.NewEventKey(tcell.KeyRune, ru, tcell.ModNone))
	}

	if r.activePage != easterEggPage {
		t.Errorf("activePage = %q, want %q after typing the full trigger phrase", r.activePage, easterEggPage)
	}
	r.closeEasterEgg()
}

func TestFeedEasterEggPhraseIsInertWhileAnOverlayIsOpen(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openHelp() // any unrelated overlay — activePage becomes non-empty

	for _, ru := range easteregg.TriggerPhrase {
		r.FeedEasterEggPhrase(tcell.NewEventKey(tcell.KeyRune, ru, tcell.ModNone))
	}

	if r.activePage == easterEggPage {
		t.Error("typing the trigger phrase while another overlay (Help) is open should not open the easter egg")
	}
}

func TestOpenEasterEggStartsInIntroStage(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openEasterEgg()
	defer r.closeEasterEgg()

	if r.easterEggView.stage != easterEggStageIntro {
		t.Errorf("stage = %d, want easterEggStageIntro", r.easterEggView.stage)
	}
	if r.easterEggView.stageFrame != 0 {
		t.Errorf("stageFrame = %d, want 0 right after opening", r.easterEggView.stageFrame)
	}
}

// TestTickEasterEggFrameStaysInIntroBeforeHoldFramesElapse and
// TestTickEasterEggFrameTransitionsFromIntroToRainAfterHoldFrames pin
// the Intro→Rain transition boundary exactly — tickEasterEggFrame lets
// this simulate N frames instantly rather than waiting on
// easterEggFrameInterval for real (see its own doc comment).
func TestTickEasterEggFrameStaysInIntroBeforeHoldFramesElapse(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openEasterEgg()
	defer r.closeEasterEgg()

	for i := 0; i < easterEggIntroRevealFrames+easterEggIntroHoldFrames-1; i++ {
		r.tickEasterEggFrame()
	}

	if r.easterEggView.stage != easterEggStageIntro {
		t.Errorf("stage = %d, want still easterEggStageIntro one frame before the transition", r.easterEggView.stage)
	}
}

func TestTickEasterEggFrameTransitionsFromIntroToRainAfterHoldFrames(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openEasterEgg()
	defer r.closeEasterEgg()
	r.easterEggView.SetRect(0, 0, 80, 24)

	for i := 0; i < easterEggIntroRevealFrames+easterEggIntroHoldFrames; i++ {
		r.tickEasterEggFrame()
	}

	if r.easterEggView.stage != easterEggStageRain {
		t.Errorf("stage = %d, want easterEggStageRain after %d frames", r.easterEggView.stage, easterEggIntroRevealFrames+easterEggIntroHoldFrames)
	}
	if r.easterEggView.stageFrame != 0 {
		t.Errorf("stageFrame = %d, want reset to 0 on entering the new stage", r.easterEggView.stageFrame)
	}
	if len(r.easterEggView.rainColumns) != 80 {
		t.Errorf("len(rainColumns) = %d, want 80 (the view's own width) once Rain starts", len(r.easterEggView.rainColumns))
	}
}

func TestTickEasterEggFrameTransitionsFromRainToFailureAfterRainFrames(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openEasterEgg()
	defer r.closeEasterEgg()
	r.easterEggView.SetRect(0, 0, 80, 24)

	for i := 0; i < easterEggIntroRevealFrames+easterEggIntroHoldFrames+easterEggRainFrames; i++ {
		r.tickEasterEggFrame()
	}

	if r.easterEggView.stage != easterEggStageFailure {
		t.Errorf("stage = %d, want easterEggStageFailure", r.easterEggView.stage)
	}
	if r.easterEggView.stageFrame != 0 {
		t.Errorf("stageFrame = %d, want reset to 0 on entering Failure", r.easterEggView.stageFrame)
	}
}

func TestDrawFailureShowsSystemFailureInRedBeforeTheHoldEnds(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openEasterEgg()
	defer r.closeEasterEgg()
	r.easterEggView.stage = easterEggStageFailure
	r.easterEggView.stageFrame = 0
	r.easterEggView.SetRect(0, 0, 80, 24)

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	screen.SetSize(80, 24)
	r.easterEggView.Draw(screen)

	textX := (80 - len(easterEggFailureText)) / 2
	textY := 24 / 2
	ch, style, _ := screen.Get(textX, textY)
	if ch != string(easterEggFailureText[0]) {
		t.Errorf("first char at (%d,%d) = %q, want %q (\"SYSTEM FAILURE\" held, not yet morphing)", textX, textY, ch, string(easterEggFailureText[0]))
	}
	fg, _, _ := style.Decompose()
	if fg != easterEggFailureColor {
		t.Errorf("color = %v, want the alarm-red easterEggFailureColor", fg)
	}
}

func TestDrawFailureShowsBreakthroughInGreenAfterTheMorphCompletes(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openEasterEgg()
	defer r.closeEasterEgg()
	r.easterEggView.stage = easterEggStageFailure
	r.easterEggView.stageFrame = easterEggFailureHoldFrames + easterEggMorphFrames + 10
	r.easterEggView.SetRect(0, 0, 80, 24)

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	screen.SetSize(80, 24)
	r.easterEggView.Draw(screen)

	textX := (80 - len(easterEggFailureText)) / 2 // same width as the morph's own padded field
	textY := 24 / 2
	// "breakthrough" is centered within that same padded width — find
	// its own first non-space column rather than assuming an exact
	// offset.
	found := false
	for i := 0; i < len(easterEggFailureText); i++ {
		ch, style, _ := screen.Get(textX+i, textY)
		if ch == "b" {
			found = true
			fg, _, _ := style.Decompose()
			if fg != easterEggBreakthroughColor {
				t.Errorf("color = %v, want easterEggBreakthroughColor once the morph has fully completed", fg)
			}
			break
		}
	}
	if !found {
		t.Error("expected to find \"breakthrough\"'s own leading 'b' somewhere in the padded field after the morph completes")
	}
}

func TestTickEasterEggFrameTransitionsFromFailureToRevealAfterPostMorphHold(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openEasterEgg()
	defer r.closeEasterEgg()
	r.easterEggView.SetRect(0, 0, 80, 24)
	r.easterEggView.stage = easterEggStageFailure
	r.easterEggView.stageFrame = 0

	for i := 0; i < easterEggFailureHoldFrames+easterEggMorphFrames+easterEggFailurePostMorphHoldFrames; i++ {
		r.tickEasterEggFrame()
	}

	if r.easterEggView.stage != easterEggStageReveal {
		t.Errorf("stage = %d, want easterEggStageReveal", r.easterEggView.stage)
	}
	if r.easterEggView.stageFrame != 0 {
		t.Errorf("stageFrame = %d, want reset to 0 on entering Reveal", r.easterEggView.stageFrame)
	}
}

// TestTickEasterEggFrameClosesTheOverlayAfterTheRevealHoldEnds pins
// Reveal's own real ending — the only stage that closes itself rather
// than holding indefinitely (see easterEggStageReveal's own doc
// comment).
func TestTickEasterEggFrameClosesTheOverlayAfterTheRevealHoldEnds(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openEasterEgg()
	r.easterEggView.SetRect(0, 0, 80, 24)
	r.easterEggView.stage = easterEggStageReveal
	r.easterEggView.stageFrame = 0

	for i := 0; i < easterEggRevealFrames+easterEggRevealHoldFrames; i++ {
		r.tickEasterEggFrame()
	}

	if r.activePage == easterEggPage {
		t.Error("the overlay should have closed itself once the Reveal hold ended")
	}
	if r.easterEggCancel != nil {
		t.Error("easterEggCancel should be nil after the self-close")
	}
}

func TestDrawRevealPaintsRockOutsideAndTextOnceFullyGrown(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openEasterEgg()
	defer r.closeEasterEgg()
	r.easterEggView.stage = easterEggStageReveal
	r.easterEggView.stageFrame = easterEggRevealFrames + 5 // fully grown, text should be stenciled on
	r.easterEggView.SetRect(0, 0, 80, 24)

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	screen.SetSize(80, 24)
	r.easterEggView.Draw(screen)

	// A point well off-center but still inside the field (not a corner,
	// which stays within the jagged edge's own shading band even at
	// full growth by design — see RevealMask's own doc comment on
	// edgeCloseness) should be fully revealed: solid, non-rock
	// background.
	_, style, _ := screen.Get(10, 12)
	_, bg, _ := style.Decompose()
	if bg == easterEggRockColor {
		t.Error("(10,12) is still rock-colored once the mask is fully grown, want it revealed")
	}

	// Find "breakthrough"'s own leading 'b' somewhere on the middle row.
	found := false
	for col := 0; col < 80; col++ {
		ch, _, _ := screen.Get(col, 24/2)
		if ch == "b" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find \"breakthrough\"'s own leading 'b' stenciled across the middle row once fully grown")
	}
}

func TestDrawRevealOmitsTheCaptionBeforeItsOwnDelayElapses(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openEasterEgg()
	defer r.closeEasterEgg()
	r.easterEggView.stage = easterEggStageReveal
	// Fully grown, but still short of easterEggCaptionDelayFrames —
	// "breakthrough" should show, the caption line should not yet.
	r.easterEggView.stageFrame = easterEggRevealFrames + easterEggCaptionDelayFrames - 1
	r.easterEggView.SetRect(0, 0, 80, 24)

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	screen.SetSize(80, 24)
	r.easterEggView.Draw(screen)

	row := 24/2 + 2
	for col := 0; col < 80; col++ {
		ch, _, _ := screen.Get(col, row)
		if ch == "w" {
			t.Fatalf("found the caption's own leading 'w' at (%d,%d) before easterEggCaptionDelayFrames elapsed", col, row)
		}
	}
}

func TestDrawRevealShowsTheCaptionTwoRowsBelowBreakthroughAfterItsDelay(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openEasterEgg()
	defer r.closeEasterEgg()
	r.easterEggView.stage = easterEggStageReveal
	r.easterEggView.stageFrame = easterEggRevealFrames + easterEggCaptionDelayFrames
	r.easterEggView.SetRect(0, 0, 80, 24)

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	screen.SetSize(80, 24)
	r.easterEggView.Draw(screen)

	row := 24/2 + 2
	found := false
	for col := 0; col < 80; col++ {
		ch, _, _ := screen.Get(col, row)
		if ch == "w" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find the caption's own leading 'w' (\"written with ♥ in Go...\") two rows below \"breakthrough\" once easterEggCaptionDelayFrames has elapsed")
	}
}

func TestDrawRevealDoesNotPanicPartwayThroughGrowth(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openEasterEgg()
	defer r.closeEasterEgg()
	r.easterEggView.stage = easterEggStageReveal
	r.easterEggView.SetRect(0, 0, 80, 24)

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	screen.SetSize(80, 24)
	for _, frame := range []int{0, 1, 10, 25, 49, 50, 60} {
		r.easterEggView.stageFrame = frame
		r.easterEggView.Draw(screen) // must not panic at any point in the growth
	}
}

func TestDrawRainRebuildsColumnsOnWidthMismatch(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.openEasterEgg()
	defer r.closeEasterEgg()
	r.easterEggView.stage = easterEggStageRain
	r.easterEggView.SetRect(0, 0, 80, 24)
	r.easterEggView.rainColumns = make([]easteregg.RainColumn, 40) // stale — simulates a resize since Rain started

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	screen.SetSize(80, 24)
	r.easterEggView.Draw(screen)

	if len(r.easterEggView.rainColumns) != 80 {
		t.Errorf("len(rainColumns) after Draw = %d, want rebuilt to 80 to match the current width", len(r.easterEggView.rainColumns))
	}
}

func TestEasterEggRainPaletteCoversEveryIntensityLevel(t *testing.T) {
	for level := 1; level <= easteregg.RainIntensityLevels; level++ {
		if easterEggRainPalette[level] == tcell.ColorDefault {
			t.Errorf("easterEggRainPalette[%d] is unset (ColorDefault)", level)
		}
	}
}

func TestFeedEasterEggPhraseIgnoresNonRuneAndModifiedKeys(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	// Feed the phrase but interrupt it with a non-rune key (an arrow) in
	// the middle — FeedEasterEggPhrase must leave the in-progress match
	// alone rather than resetting it, so the phrase still completes.
	phrase := []rune(easteregg.TriggerPhrase)
	mid := len(phrase) / 2
	for _, ru := range phrase[:mid] {
		r.FeedEasterEggPhrase(tcell.NewEventKey(tcell.KeyRune, ru, tcell.ModNone))
	}
	r.FeedEasterEggPhrase(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	r.FeedEasterEggPhrase(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModCtrl))
	for _, ru := range phrase[mid:] {
		r.FeedEasterEggPhrase(tcell.NewEventKey(tcell.KeyRune, ru, tcell.ModNone))
	}

	if r.activePage != easterEggPage {
		t.Error("a non-rune or modified key in the middle of typing the phrase should not reset the match")
	}
	r.closeEasterEgg()
}
