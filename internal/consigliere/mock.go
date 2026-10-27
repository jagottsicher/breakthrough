package consigliere

import "context"

// mockReplyText is deliberately long enough to wrap across a narrow
// overlay and scroll within a short one — the one thing a single short
// placeholder line would never show, and the actual point of this
// first slice (see doc.go): judging the panel's own layout before any
// real provider exists.
const mockReplyText = "This is a placeholder reply from the AI Consigliere — no real provider call yet, just a preview of how a multi-line reply looks and scrolls in this panel. A real tip would speak to the current view's own visible context (e.g. Log Audit: time range, active filter) and give one short, concrete piece of advice instead of this notice."

// Mock is a Provider that never makes a network call — it always
// answers with mockReplyText, regardless of req. Exists to exercise
// the UI panel's own history/scroll/input mechanics end to end before
// any real provider, API-key storage, or consent-checkbox decision
// (all still open per feature_ideas.txt) is made.
type Mock struct{}

func (Mock) Reply(ctx context.Context, req Request) (string, error) {
	return mockReplyText, nil
}
