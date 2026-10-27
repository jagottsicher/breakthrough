package consigliere

import (
	"context"
	"net/http"
	"time"
)

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

// KeyFunc resolves the API key a real Provider (Anthropic/
// OpenAICompat) needs, called fresh on every Reply rather than once at
// construction — see Anthropic.APIKey's own doc comment for why: this
// package never imports internal/apikey itself (that's a UI/config
// concern, not business logic), so a caller in internal/ui supplies
// one, typically a closure over apikey.Resolve plus whatever env var/
// file path/Session that call needs.
type KeyFunc func() (string, error)

// resolveKey runs key, or reports a clear error if none was supplied
// at all — a Provider built without one (e.g. a test's own zero
// value) fails loudly rather than sending an empty x-api-key/
// Authorization header, which a real server would otherwise reject
// with a confusing 401.
func resolveKey(key KeyFunc) (string, error) {
	if key == nil {
		return "", errNoAPIKey
	}
	return key()
}

// errNoAPIKey is resolveKey's own "no KeyFunc at all" case — distinct
// from whatever KeyFunc itself returns (e.g. apikey.Resolve's own
// TierNone case), which carries a more specific, user-facing message.
var errNoAPIKey = errNoAPIKeyError{}

type errNoAPIKeyError struct{}

func (errNoAPIKeyError) Error() string { return "no API key configured" }

// httpTimeout bounds every real provider's own HTTP call — the
// default http.Client has no timeout at all, which would otherwise
// let one hung request leave the sidebar's "thinking…" state stuck
// indefinitely (ctx's own cancellation handles the user explicitly
// backing out; this handles the network simply never answering).
const httpTimeout = 30 * time.Second

// httpClient returns client if non-nil (a test's own fake transport),
// or a shared default with httpTimeout otherwise.
func httpClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: httpTimeout}
}

// firstNonEmpty returns override if it's non-empty, otherwise
// fallback — BaseURL/Model's own "" means "use the package default"
// contract, shared by Anthropic and OpenAICompat.
func firstNonEmpty(override, fallback string) string {
	if override != "" {
		return override
	}
	return fallback
}
