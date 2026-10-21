package ui

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
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

func TestRabbitButtonClickOpensEasterEgg(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.rabbitBtn.SetRect(0, 0, rabbitButtonWidth, 1)

	_, _ = r.panel.rabbitBtn.MouseHandler()(tview.MouseLeftClick, tcell.NewEventMouse(1, 0, tcell.Button1, tcell.ModNone), func(tview.Primitive) {})

	if r.activePage != easterEggPage {
		t.Errorf("activePage = %q, want %q after clicking the rabbit button", r.activePage, easterEggPage)
	}
	r.closeEasterEgg()
}

func TestHideRabbitButtonClearsItsText(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.panel.hideRabbitButton()

	if got := r.panel.rabbitBtn.GetText(false); got != "" {
		t.Errorf("rabbitBtn text after hideRabbitButton = %q, want empty", got)
	}
	if r.panel.rabbitHideTimer != nil {
		t.Error("rabbitHideTimer should be nil after hideRabbitButton")
	}
}

// TestNewPanelHidesTheRabbitButtonWhenTheDeadlineAlreadyPassed pins the
// "a tab opened after the five-minute window already closed never
// shows it at all" contract — rabbitButtonDeadline is a package var
// set once at process start; this test moves it into the past for its
// own duration only, restoring the real value afterward so no other
// test in this package is affected.
func TestNewPanelHidesTheRabbitButtonWhenTheDeadlineAlreadyPassed(t *testing.T) {
	original := rabbitButtonDeadline
	rabbitButtonDeadline = time.Now().Add(-time.Minute)
	defer func() { rabbitButtonDeadline = original }()

	p, err := NewPanel(tview.NewApplication(), fixtureDir(t), config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	if got := p.rabbitBtn.GetText(false); got != "" {
		t.Errorf("rabbitBtn text = %q, want empty for a panel created after the deadline", got)
	}
}
