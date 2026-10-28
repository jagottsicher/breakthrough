package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/consigliere"
	"github.com/jagottsicher/breakthrough/internal/docsearch"
)

// fakeConsigliereProvider is a consigliere.Provider test double —
// reply (and/or err) is returned verbatim from Reply, calls is
// incremented so a test can assert whether the fallback-only
// translation path actually ran.
type fakeConsigliereProvider struct {
	reply string
	err   error
	calls *int
}

func (p fakeConsigliereProvider) Reply(ctx context.Context, req consigliere.Request) (string, error) {
	if p.calls != nil {
		*p.calls++
	}
	return p.reply, p.err
}

// neverCalledProvider fails the test outright if Reply is ever
// invoked — used by every case that expects the direct (untranslated)
// search to already succeed, where a translation call would be a
// real, unwanted extra network round trip.
type neverCalledProvider struct{ t *testing.T }

func (p neverCalledProvider) Reply(ctx context.Context, req consigliere.Request) (string, error) {
	p.t.Fatal("provider.Reply should not have been called — the direct search already had a result")
	return "", nil
}

func TestConsigliereDocSnapshotFindsRelevantDocumentation(t *testing.T) {
	root := newPlainKeyRoot(t)

	got := root.consigliereDocSnapshot(context.Background(), neverCalledProvider{t}, "how do I search the last 3 hours in a log file")

	if !strings.Contains(got, "Relevant documentation:") {
		t.Fatalf("snapshot = %q, want it to mention it found documentation", got)
	}
	if !strings.Contains(got, "Log Holepunch") {
		t.Errorf("snapshot = %q, want it to surface the Log Holepunch section", got)
	}
}

// TestConsigliereDocSnapshotTranslatesWhenDirectSearchFindsNothing
// pins the real, reported fix: a question sharing no vocabulary at
// all with the (English-only) corpus falls back to asking the
// provider for an English translation and searching again with that.
func TestConsigliereDocSnapshotTranslatesWhenDirectSearchFindsNothing(t *testing.T) {
	root := newPlainKeyRoot(t)
	calls := 0
	provider := fakeConsigliereProvider{reply: "how do I open a new tab", calls: &calls}

	got := root.consigliereDocSnapshot(context.Background(), provider, "wie kann ich das fenster verschieben")

	if calls != 1 {
		t.Fatalf("provider.Reply called %d times, want exactly 1 (the translation attempt)", calls)
	}
	if !strings.Contains(got, "Relevant documentation:") {
		t.Errorf("snapshot = %q, want the retried, translated search to find something", got)
	}
}

// TestConsigliereDocSnapshotEmptyWhenTranslationFails pins the other
// half: a failed translation attempt (network error) degrades to "no
// documentation found," not a second error surfaced to the user.
func TestConsigliereDocSnapshotEmptyWhenTranslationFails(t *testing.T) {
	root := newPlainKeyRoot(t)
	provider := fakeConsigliereProvider{err: errors.New("network unreachable")}

	got := root.consigliereDocSnapshot(context.Background(), provider, "xenomorph platypus quantum")

	if got != "" {
		t.Errorf("snapshot = %q, want empty when both the direct search and the translation fallback find nothing", got)
	}
}

func TestConsigliereDocSnapshotEmptyForAnUnrelatedQuestionEvenAfterTranslating(t *testing.T) {
	root := newPlainKeyRoot(t)
	provider := fakeConsigliereProvider{reply: "xenomorph platypus quantum"} // "translation" still matches nothing

	got := root.consigliereDocSnapshot(context.Background(), provider, "xenomorph platypus quantum")

	if got != "" {
		t.Errorf("snapshot = %q, want empty for a question matching nothing in the docs even after translating", got)
	}
}

func TestConsigliereDocSnapshotEmptyWithoutAnIndex(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.docSearchIndex = nil

	if got := root.consigliereDocSnapshot(context.Background(), neverCalledProvider{t}, "anything at all"); got != "" {
		t.Errorf("snapshot = %q, want empty when the index itself is unavailable", got)
	}
}

func TestConsigliereDocSnapshotCapsChunkLength(t *testing.T) {
	root := newPlainKeyRoot(t)
	root.docSearchIndex = docsearch.NewIndex([]docsearch.Chunk{
		{Heading: "Long section", Body: strings.Repeat("word ", consigliereMaxDocChunkChars)}, // comfortably over the cap regardless of its current value
	})

	got := root.consigliereDocSnapshot(context.Background(), neverCalledProvider{t}, "word")

	if !strings.Contains(got, "…") {
		t.Errorf("snapshot = %q, want a truncation marker for an oversized chunk", got)
	}
	if len(got) > 2*consigliereMaxDocChunkChars {
		t.Errorf("snapshot is %d chars long, want it bounded near the %d-char cap", len(got), consigliereMaxDocChunkChars)
	}
}

func TestJoinConsigliereSnapshotPartsOmitsEmptyPartsWithoutStrayBlankLines(t *testing.T) {
	cases := []struct {
		name  string
		parts []string
		want  string
	}{
		{"both set", []string{"view info", "doc info"}, "view info\n\ndoc info"},
		{"first only", []string{"view info", ""}, "view info"},
		{"second only", []string{"", "doc info"}, "doc info"},
		{"neither", []string{"", ""}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := joinConsigliereSnapshotParts(c.parts...); got != c.want {
				t.Errorf("joinConsigliereSnapshotParts(%v) = %q, want %q", c.parts, got, c.want)
			}
		})
	}
}
