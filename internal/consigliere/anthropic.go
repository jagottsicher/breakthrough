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

// anthropicMaxTokens caps a single reply — generous enough for the
// sidebar's own short, concrete-tip use case without risking an
// unbounded, slow-to-render response.
const anthropicMaxTokens = 1024

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
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
	Error   *anthropicErrorBody     `json:"error,omitempty"`
}

type anthropicErrorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Reply implements Provider. req.Snapshot goes in the Messages API's
// own top-level "system" field (a real system prompt slot, distinct
// from the messages array, which only ever carries user/assistant
// turns) — not folded into the first message, which would make it
// look like something the human said.
func (p Anthropic) Reply(ctx context.Context, req Request) (string, error) {
	key, err := resolveKey(p.APIKey)
	if err != nil {
		return "", err
	}

	body := anthropicRequest{
		Model:     firstNonEmpty(p.Model, AnthropicDefaultModel),
		MaxTokens: anthropicMaxTokens,
		System:    req.Snapshot,
		Messages:  toAnthropicMessages(req.History),
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

	resp, err := httpClient(p.Client).Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("consigliere: calling Anthropic: %w", err)
	}
	defer resp.Body.Close()

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
			return block.Text, nil
		}
	}
	return "", fmt.Errorf("consigliere: Anthropic reply had no text content")
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
