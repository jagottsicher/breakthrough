package consigliere

import "context"

// mockReplyText is deliberately long enough to wrap across a narrow
// overlay and scroll within a short one — the one thing a single short
// placeholder line would never show, and the actual point of this
// first slice (see doc.go): judging the panel's own layout before any
// real provider exists.
const mockReplyText = "This is a placeholder reply from the AI Consigliere — no real provider call yet, just a preview of how a multi-line reply looks and scrolls in this panel. A real tip would speak to the current view's own visible context (e.g. Log Audit: time range, active filter) and give one short, concrete piece of advice instead of this notice."

// Mock is a Provider that never makes a network call — it always
// answers with mockReplyText, regardless of req. Nothing in
// internal/ui constructs this anymore (see buildConsigliereProvider),
// now that Anthropic/OpenAICompat exist — kept as a standalone,
// dependency-free Provider for exercising the UI panel's own history/
// scroll/input mechanics without a network call or API key.
type Mock struct{}

func (Mock) Reply(ctx context.Context, req Request) (string, error) {
	return mockReplyText, nil
}
