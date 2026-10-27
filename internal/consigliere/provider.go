package consigliere

import "context"

// Role distinguishes the human's own turns from the Consigliere's own
// replies within a Session's history — see Message.
type Role int

const (
	RoleUser Role = iota
	RoleAssistant
)

// Message is one turn of a chat session, kept verbatim in Session.History
// so a follow-up question ("und kann man das jetzt besser erkennen?")
// can refer back to it — see the spec's own "Chat-Historie/Gedächtnis"
// section.
type Message struct {
	Role Role
	Text string
}

// Request is everything one call to Provider.Reply needs: the
// conversation so far, plus the current view's own curated context
// snapshot (see feature_ideas.txt's "AdvisorContext()") — rebuilt fresh
// from the live view state on every new question, never the one
// captured when the overlay was first opened.
type Request struct {
	History  []Message
	Snapshot string
}

// Provider answers one Consigliere question. A real implementation
// (Claude/ChatGPT/DeepSeek/a local OpenAI-compatible endpoint) sends
// Request over HTTP and returns the model's own reply text; Mock
// (mock.go) answers without any network call at all.
type Provider interface {
	Reply(ctx context.Context, req Request) (string, error)
}
