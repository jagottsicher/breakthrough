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
