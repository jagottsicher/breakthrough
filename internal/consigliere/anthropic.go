package consigliere

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// AnthropicDefaultModel is used when Anthropic.Model is "" —
// Anthropic's own docs recommend this as the starting point for most
// workloads (platform.claude.com/docs/en/models/overview, checked
// 2026-10-27). Model IDs retire over time; override via the Options
// screen's own "Model" field (consigliere_model) if this one stops
// working.
const AnthropicDefaultModel = "claude-opus-5-5"

// AnthropicDefaultBaseURL is Anthropic's own API host.
const AnthropicDefaultBaseURL = "https://api.anthropic.com"

// anthropicAPIVersion is the Messages API's own required protocol
// version header — unrelated to AnthropicDefaultModel and far less
// likely to ever need updating (platform.claude.com/docs/en/api/
// messages, checked 2026-10-27).
const anthropicAPIVersion = "2023-06-01"

// anthropicServerSideFallbackBeta opts every request into retrying a
// declined reply on another model within the same call — see
// anthropicFallbacks' own doc comment for why this is always sent,
// not an Options toggle. The beta header's own date must match
// exactly (platform.claude.com/docs/en/build-with-claude/
// refusals-and-fallback, checked 2026-11-04) — a different
// server-side-fallback-* value, or none, gets fallbacks rejected with
// a 400 rather than silently ignored.
const anthropicServerSideFallbackBeta = "server-side-fallback-2026-07-01"

// anthropicFallbacks requests Anthropic's own server-side fallback
// routing, mode "default" ("the model Anthropic recommends for its
// refusal category", rather than a fixed, hand-picked list this
// package would then have to keep current). A real, reported case:
// Machiavelli Mode forwarding an ordinary application log's own line
// — which happened to include a third party's name and other
// personal fields — got declined outright (stop_reason "refusal",
// category "cyber") on content that was never attack-adjacent at all.
// Always sent, unconditionally: a refusal otherwise ends the
// question right there with nothing to act on besides rephrasing, and
// this costs nothing extra when no refusal happens in the first place
// (see Reply's own stop_reason handling for what happens when every
// model in the fallback chain still declines).
const anthropicFallbacks = "default"

// anthropicMaxTokens caps a single reply. Claude Opus 5 (and later
// Opus models) think by default even when Effort is left unset — the
// thinking tokens come out of this same budget, ahead of the actual
// reply — so a cap sized for the sidebar's own short, concrete-tip
// text alone was leaving no room for that reasoning on anything but
// the simplest question. A real, reported bug: with the budget
// already spent on thinking before any TextBlock, the loop below
// never found one at all. Raising this alone only shrinks the window
// where that happens — see anthropicDefaultEffort below for the fix
// that actually bounds how much thinking happens in the first place,
// and the stop_reason check in Reply for the case this still doesn't
// cover (a TextBlock that exists but was itself cut off mid-reply).
const anthropicMaxTokens = 4096

// anthropicDefaultEffort is sent whenever Effort is left unset,
// instead of omitting output_config entirely (the model's own
// documented default, "high", which also means "think as much as it
// wants to"). A real, reported bug: thinking tokens come out of
// anthropicMaxTokens ahead of the actual reply, so "high" effort could
// leave only one or two tokens of budget for the visible answer —
// observed as a reply truncated to two words, non-deterministically,
// since how much a given question makes the model think isn't fixed.
// "medium" trades away the depth "high" would have given a genuinely
// hard problem for headroom this sidebar's own short, concrete-tip
// questions rarely need anyway — matching the guidance in
// platform.claude.com/docs/en/build-with-claude/effort (checked
// 2026-11-04) that chat/latency-sensitive routes do well below "high".
// An explicit Effort (including "high" itself) always overrides this.
const anthropicDefaultEffort = "medium"

// Anthropic is a Provider backed by Anthropic's own Messages API. Its
// own request/response shape (x-api-key auth, an anthropic-version
// header, a content-block array in the reply rather than a flat
// string) is why this needs its own implementation rather than
// sharing OpenAICompat's.
type Anthropic struct {
	// BaseURL overrides AnthropicDefaultBaseURL — "" means the default.
	// Exists for tests (httptest.NewServer) rather than any real
	// product need; Anthropic itself only has the one host.
	BaseURL string
	// Model overrides AnthropicDefaultModel — "" means the default.
	Model string
	// Effort sets output_config.effort on the request ("low",
	// "medium", "high", "xhigh", or "max" — see
	// platform.claude.com/docs/en/build-with-claude/effort, checked
	// 2026-10-27) — "" sends anthropicDefaultEffort rather than
	// omitting output_config, see that constant's own doc comment for
	// why "the model's own documented default" turned out to be the
	// wrong choice here.
	Effort string
	// APIKey resolves the current key immediately before the request
	// is sent — see KeyFunc's own doc comment for why this is a
	// function, not a plain string.
	APIKey KeyFunc
	// Client overrides the shared default *http.Client — nil means
	// httpClient's own default, with httpTimeout applied.
	Client *http.Client
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model        string                 `json:"model"`
	MaxTokens    int                    `json:"max_tokens"`
	System       string                 `json:"system,omitempty"`
	Messages     []anthropicMessage     `json:"messages"`
	OutputConfig *anthropicOutputConfig `json:"output_config,omitempty"`
	Fallbacks    string                 `json:"fallbacks,omitempty"`
}

type anthropicOutputConfig struct {
	Effort string `json:"effort"`
}

type anthropicResponse struct {
	Model       string                  `json:"model"`
	Content     []anthropicContentBlock `json:"content"`
	StopReason  string                  `json:"stop_reason"`
	StopDetails *anthropicStopDetails   `json:"stop_details,omitempty"`
	Error       *anthropicErrorBody     `json:"error,omitempty"`
}

type anthropicErrorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// anthropicStopDetails is only populated when StopReason is
// "refusal" — a safety classifier declined the request outright
// (HTTP 200, no error) rather than running out of budget. Category is
// an open set ("cyber", "bio", "reasoning_extraction",
// "frontier_llm", ... or "" — platform.claude.com/docs/en/api/
// messages, checked 2026-11-04) and Explanation may also be empty;
// both are surfaced as-is rather than guessed at.
type anthropicStopDetails struct {
	Category    string `json:"category"`
	Explanation string `json:"explanation"`
}

// Reply implements Provider. req.System and req.Snapshot (combined
// via combineConsigliereSystem) go in the Messages API's own
// top-level "system" field (a real system prompt slot, distinct from
// the messages array, which only ever carries user/assistant turns)
// — not folded into the first message, which would make it look like
// something the human said.
func (p Anthropic) Reply(ctx context.Context, req Request) (string, error) {
	key, err := resolveKey(p.APIKey)
	if err != nil {
		return "", err
	}

	body := anthropicRequest{
		Model:        firstNonEmpty(p.Model, AnthropicDefaultModel),
		MaxTokens:    anthropicMaxTokens,
		System:       combineConsigliereSystem(req.System, req.Snapshot),
		Messages:     toAnthropicMessages(req.History),
		OutputConfig: &anthropicOutputConfig{Effort: firstNonEmpty(p.Effort, anthropicDefaultEffort)},
		Fallbacks:    anthropicFallbacks,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("consigliere: encoding Anthropic request: %w", err)
	}

	url := firstNonEmpty(p.BaseURL, AnthropicDefaultBaseURL) + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("consigliere: building Anthropic request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", key)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	httpReq.Header.Set("anthropic-beta", anthropicServerSideFallbackBeta)

	resp, err := httpClient(p.Client).Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("consigliere: calling Anthropic: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("consigliere: reading Anthropic response: %w", err)
	}

	var parsed anthropicResponse
	// Parsed regardless of status: a non-2xx response's own body is
	// exactly where Anthropic puts the actual error message (see
	// parsed.Error below) — far more useful to the user than the bare
	// status code alone.
	_ = json.Unmarshal(data, &parsed)

	if resp.StatusCode != http.StatusOK {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return "", fmt.Errorf("consigliere: Anthropic: %s", parsed.Error.Message)
		}
		return "", fmt.Errorf("consigliere: Anthropic returned status %d", resp.StatusCode)
	}

	for _, block := range parsed.Content {
		if block.Type == "text" && block.Text != "" {
			if parsed.StopReason == "max_tokens" {
				// A real, reported bug: a non-empty TextBlock here does
				// not mean a complete reply — anthropicMaxTokens can run
				// out mid-sentence just as easily as it can run out
				// before any TextBlock at all, and returning that
				// fragment as if it were the whole answer (e.g. "Ich s")
				// is a silent wrong result, worse than the loud error
				// below. Appended rather than discarded: a short, cut-off
				// fragment is still more useful to the user than nothing.
				return block.Text + "\n\n[reply cut off — ran out of reply budget; try asking again, or more narrowly]", nil
			}
			if parsed.Model != "" && parsed.Model != body.Model {
				// anthropicFallbacks served this reply, silently, from a
				// different model than the one configured in Options — a
				// real, reported risk (CLAUDE.md's own "no silent
				// substitution") if that went unmentioned: a noticeably
				// different-sounding answer with no indication why.
				return fmt.Sprintf("%s\n\n[answered by %s after %s declined]", block.Text, parsed.Model, body.Model), nil
			}
			return block.Text, nil
		}
	}
	if parsed.StopReason == "refusal" {
		detail := "no explanation given"
		if parsed.StopDetails != nil {
			switch {
			case parsed.StopDetails.Category != "" && parsed.StopDetails.Explanation != "":
				detail = fmt.Sprintf("%s: %s", parsed.StopDetails.Category, parsed.StopDetails.Explanation)
			case parsed.StopDetails.Category != "":
				detail = parsed.StopDetails.Category
			case parsed.StopDetails.Explanation != "":
				detail = parsed.StopDetails.Explanation
			}
		}
		return "", fmt.Errorf("consigliere: Anthropic declined to answer (%s) — likely the forwarded content itself, not a bug; try rephrasing the question", detail)
	}
	// StopReason ("max_tokens" most likely, per anthropicMaxTokens's
	// own doc comment — the thinking budget ran out before any
	// TextBlock) goes in the message itself: a bare "no text content"
	// left the user with nothing to act on beyond retrying blindly.
	return "", fmt.Errorf("consigliere: Anthropic reply had no text content (stop_reason: %s)", firstNonEmpty(parsed.StopReason, "unknown"))
}

// toAnthropicMessages translates Message.Role (this package's own
// Role enum) into the literal "user"/"assistant" strings the Messages
// API's own messages array expects.
func toAnthropicMessages(history []Message) []anthropicMessage {
	out := make([]anthropicMessage, 0, len(history))
	for _, m := range history {
		role := "user"
		if m.Role == RoleAssistant {
			role = "assistant"
		}
		out = append(out, anthropicMessage{Role: role, Content: m.Text})
	}
	return out
}
