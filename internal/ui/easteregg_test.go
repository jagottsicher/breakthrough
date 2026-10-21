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
