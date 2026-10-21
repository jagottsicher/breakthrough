package easteregg

import "testing"

func feedString(d *PhraseDetector, s string) (completed bool) {
	for _, r := range s {
		if d.Feed(r) {
			completed = true
		}
	}
	return completed
}

func TestPhraseDetectorCompletesOnTheExactPhrase(t *testing.T) {
	var d PhraseDetector
	if !feedString(&d, TriggerPhrase) {
		t.Fatal("Feed should report true once the full phrase has been typed")
	}
}

func TestPhraseDetectorResetsOnAMismatchedRune(t *testing.T) {
	var d PhraseDetector
	feedString(&d, "follow")
	d.Feed('x') // mismatch mid-phrase, not TriggerPhrase's own first rune
	if feedString(&d, TriggerPhrase[1:]) {
		t.Error("completing the phrase's own remainder after a mismatch should not report true")
	}
	// A real, fresh attempt from the top must still work afterward.
	if !feedString(&d, TriggerPhrase) {
		t.Error("a full, fresh phrase after a mismatch should still complete")
	}
}

func TestPhraseDetectorRestartsOnAMismatchThatIsItselfTheFirstRune(t *testing.T) {
	var d PhraseDetector
	feedString(&d, "foll")
	d.Feed('f') // mismatch, but 'f' is also TriggerPhrase's own first rune
	if !feedString(&d, TriggerPhrase[1:]) {
		t.Error("a mismatch that is itself the phrase's first rune should count as a fresh start, not a hard reset")
	}
}

func TestPhraseDetectorResetsAfterCompletingSoASecondAttemptWorks(t *testing.T) {
	var d PhraseDetector
	if !feedString(&d, TriggerPhrase) {
		t.Fatal("setup: first attempt should complete")
	}
	if !feedString(&d, TriggerPhrase) {
		t.Error("a second, independent attempt right after the first should also complete")
	}
}

func TestPhraseDetectorIgnoresUnrelatedTypingInterspersed(t *testing.T) {
	var d PhraseDetector
	feedString(&d, "ls -la")
	if feedString(&d, TriggerPhrase) == false {
		t.Error("unrelated typing beforehand should not prevent a subsequent real attempt from completing")
	}
}
