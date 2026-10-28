package ui

import (
	"context"
	"strings"
	"testing"
)

func TestConsigliereDocSearchViewHintNamesLogAuditAndGitBreach(t *testing.T) {
	root := newPlainKeyRoot(t)

	root.activePage = logAuditViewerPage
	if got := root.consigliereDocSearchViewHint(); got != "Log Holepunch screen" {
		t.Errorf("hint over Log Holepunch = %q, want %q", got, "Log Holepunch screen")
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
	if got := appendConsigliereDocSearchHint("query", "Log Holepunch screen"); got != "query Log Holepunch screen" {
		t.Errorf("got %q", got)
	}
	if got := appendConsigliereDocSearchHint("query", ""); got != "query" {
		t.Errorf("got %q, want the query unchanged when there's no hint", got)
	}
}

// TestConsigliereDocSnapshotDisambiguatesLogAuditFromActionLog pins
// the real, reported fix end to end: a question asked while on Log
// Holepunch now retrieves Log Holepunch's own section rather than the
// similarly-purposed Action Log's — both real sections in the real
// embedded corpus, not a synthetic fixture, since the whole bug was
// specifically about two real sections sharing enough vocabulary to
// be confused for each other.
func TestConsigliereDocSnapshotDisambiguatesLogAuditFromActionLog(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.activePage = logAuditViewerPage

	got := root.consigliereDocSnapshot(context.Background(), neverCalledProvider{t}, "how do I search for exception in the last three days")

	if !strings.Contains(got, "Log Holepunch screen") {
		t.Errorf("snapshot = %q, want the Log Holepunch section surfaced once the view hint disambiguates it from the Action Log", got)
	}
}

// TestConsigliereDocSnapshotPrefersLogHolepunchOverActionLogEvenWhenActionLogIsShorter
// pins a second, real reported case of the same underlying bug: even
// with the view hint appended, a short Action Log chunk's higher term
// density (BM25's own length normalization) could still outrank the
// much longer, but correct, Log Holepunch chunk for an everyday
// phrasing like "how do you show the last 3 hours in a log" — fixed by
// forcing the hinted screen's own chunk to the front (see
// ChunkByHeadingContains/prependDocChunk), not by trying to out-tune
// BM25's own ranking.
func TestConsigliereDocSnapshotPrefersLogHolepunchOverActionLogEvenWhenActionLogIsShorter(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.activePage = logAuditViewerPage

	got := root.consigliereDocSnapshot(context.Background(), neverCalledProvider{t}, "how do you show the last 3 hours in a log")

	idxLog := strings.Index(got, "Log Holepunch screen")
	idxAction := strings.Index(got, "Action Log screen")
	if idxLog == -1 {
		t.Fatalf("snapshot = %q, want the Log Holepunch section present", got)
	}
	if idxAction != -1 && idxAction < idxLog {
		t.Errorf("snapshot = %q, want Log Holepunch ranked above Action Log", got)
	}
}
