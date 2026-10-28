package ui

import (
	"context"
	"strings"
	"testing"
)

func TestConsigliereDocSearchViewHintNamesLogAuditAndGitBreach(t *testing.T) {
	root := newPlainKeyRoot(t)

	root.activePage = logAuditViewerPage
	if got := root.consigliereDocSearchViewHint(); got != "Log Audit screen" {
		t.Errorf("hint over Log Audit = %q, want %q", got, "Log Audit screen")
	}

	root.activePage = gitBreachPage
	if got := root.consigliereDocSearchViewHint(); got != "Git Breach" {
		t.Errorf("hint over Git Breach = %q, want %q", got, "Git Breach")
	}

	root.activePage = ""
	if got := root.consigliereDocSearchViewHint(); got != "" {
		t.Errorf("hint over the bare panel = %q, want \"\" (no disambiguation needed)", got)
	}
}

func TestAppendConsigliereDocSearchHint(t *testing.T) {
	if got := appendConsigliereDocSearchHint("query", "Log Audit screen"); got != "query Log Audit screen" {
		t.Errorf("got %q", got)
	}
	if got := appendConsigliereDocSearchHint("query", ""); got != "query" {
		t.Errorf("got %q, want the query unchanged when there's no hint", got)
	}
}

// TestConsigliereDocSnapshotDisambiguatesLogAuditFromActionLog pins
// the real, reported fix end to end: a question asked while on Log
// Audit now retrieves Log Audit's own section rather than the
// similarly-purposed Action Log's — both real sections in the real
// embedded corpus, not a synthetic fixture, since the whole bug was
// specifically about two real sections sharing enough vocabulary to
// be confused for each other.
func TestConsigliereDocSnapshotDisambiguatesLogAuditFromActionLog(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.activePage = logAuditViewerPage

	got := root.consigliereDocSnapshot(context.Background(), neverCalledProvider{t}, "how do I search for exception in the last three days")

	if !strings.Contains(got, "Log Audit screen") {
		t.Errorf("snapshot = %q, want the Log Audit section surfaced once the view hint disambiguates it from the Action Log", got)
	}
}
