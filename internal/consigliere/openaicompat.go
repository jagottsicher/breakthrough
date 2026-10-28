package consigliere

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OpenAIDefaultModel is used when OpenAICompat.Model is "" and
// BaseURL is OpenAIDefaultBaseURL — OpenAI's own docs name this as
// the flagship, "if unsure, start here" model (developers.openai.com/
// api/docs/models, checked 2026-10-27). Model IDs retire over time;
// override via the Options screen's own "Model" field
// (consigliere_model) if this one stops working.
const OpenAIDefaultModel = "gpt-6-astra"

// OpenAIDefaultBaseURL is OpenAI's own API host, including its own
// "/v1" version segment — see the doc comment on Reply's own url
// construction for why that segment now lives in BaseURL itself
// rather than being appended unconditionally.
const OpenAIDefaultBaseURL = "https://api.openai.com/v1"

// DeepSeekDefaultModel is used when OpenAICompat.Model is "" and
// BaseURL is DeepSeekDefaultBaseURL — the higher-capability general-
// purpose tier in DeepSeek's own current lineup (api-docs.deepseek.com,
// checked 2026-10-27), as opposed to deepseek-flash's cheaper, faster
// one. Override via consigliere_model if this changes.
const DeepSeekDefaultModel = "deepseek-v4-pro"

// DeepSeekDefaultBaseURL is DeepSeek's own API host — deliberately
// OpenAI-API-compatible (same endpoint path, same request/response
// shape, same Bearer auth), which is exactly why it shares
// OpenAICompat with OpenAI itself rather than needing its own file.
const DeepSeekDefaultBaseURL = "https://api.deepseek.com/v1"

// GeminiDefaultModel is used when OpenAICompat.Model is "" and
// BaseURL is GeminiDefaultBaseURL — Google's own current flagship
// model (ai.google.dev/gemini-api/docs/models, checked 2026-10-28).
// Override via consigliere_model if this changes.
const GeminiDefaultModel = "gemini-4-argon"

// GeminiDefaultBaseURL is Gemini's own OpenAI-compatible endpoint
// (ai.google.dev/gemini-api/docs/openai, checked 2026-10-28) — unlike
// OpenAI/DeepSeek, its own "openai" compatibility shim already carries
// the version segment itself ("/v1beta/openai", not "/v1"), which is
// exactly why BaseURL now carries its own full prefix up to (not
// including) "/chat/completions"/"/models" instead of Reply assuming
// "/v1/..." for every provider.
const GeminiDefaultBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"

// OllamaDefaultModel is offered as a starting point only — unlike
// every other provider here, Ollama has no universal default that
// just works: it only replies with whatever models have actually
// been pulled locally (`ollama pull <name>`), which this package has
// no way to know in advance. "llama3.1" is Ollama's own
// library's current top listing (ollama.com/library, checked
// 2026-10-28), not a guarantee it's present on this machine — the
// Options screen's own live model list (fetched from this same local
// server) is the real source of truth once Ollama is actually
// running.
const OllamaDefaultModel = "llama3.1"

// OllamaDefaultBaseURL is Ollama's own local server, OpenAI-compatible
// endpoint (docs.ollama.com/api/openai-compatibility, checked
// 2026-10-28) — same URL shape as OpenAI/DeepSeek, and the same
// "api_key required by the client library, but never actually
// checked" contract every local Ollama install has, which is why
// internal/ui supplies a constant placeholder KeyFunc for this
// provider rather than ever consulting internal/apikey for it.
const OllamaDefaultBaseURL = "http://localhost:11434/v1"

// Unlike Anthropic's own max_tokens, this package doesn't set an
// output-length cap on an OpenAICompat request: it's optional there,
// and the exact field name has shifted across API generations (see
// OpenAIDefaultModel's own doc comment on how fast model IDs already
// move) — omitting it risks a slightly longer reply, not a wrong or
// rejected request, which is the safer failure mode of the two.

// OpenAICompat is a Provider for any backend that speaks the OpenAI
// Chat Completions API shape — OpenAI itself, DeepSeek (which deliber-
// ately cloned that shape), or, per internal/consigliere's own doc
// comment, a local OpenAI-compatible endpoint a user points BaseURL
// at directly. Unlike Anthropic, there's no single sensible built-in
// default for BaseURL/Model here — the caller (internal/ui) always
// supplies both, picking OpenAI's or DeepSeek's own constants above
// based on which provider is currently selected.
type OpenAICompat struct {
	BaseURL string
	Model   string
	// Effort sets reasoning_effort on the request (OpenAI:
	// "none"/"minimal"/"low"/"medium"/"high"/"xhigh"/"max"; DeepSeek:
	// "none"/"low"/"high"/"max" — see OpenAIEffortLevels/ModelInfo's
	// own doc comments) — "" omits the field, the model's own
	// documented default.
	Effort string
	// APIKey resolves the current key immediately before the request
	// is sent — see KeyFunc's own doc comment.
	APIKey KeyFunc
	Client *http.Client
}

type openAICompatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAICompatRequest struct {
	Model           string                `json:"model"`
	Messages        []openAICompatMessage `json:"messages"`
	ReasoningEffort string                `json:"reasoning_effort,omitempty"`
}

type openAICompatChoice struct {
	Message openAICompatMessage `json:"message"`
}

type openAICompatResponse struct {
	Choices []openAICompatChoice   `json:"choices"`
	Error   *openAICompatErrorBody `json:"error,omitempty"`
}

type openAICompatErrorBody struct {
	Message string `json:"message"`
}

// Reply implements Provider. req.System and req.Snapshot (combined
// via combineConsigliereSystem), unlike Anthropic's own separate
// "system" field, become the first message with role "system" — the
// Chat Completions API's own equivalent slot (see
// toOpenAICompatMessages).
func (p OpenAICompat) Reply(ctx context.Context, req Request) (string, error) {
	key, err := resolveKey(p.APIKey)
	if err != nil {
		return "", err
	}

	body := openAICompatRequest{
		Model:           p.Model,
		Messages:        toOpenAICompatMessages(req),
		ReasoningEffort: p.Effort,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("consigliere: encoding request: %w", err)
	}

	// "/chat/completions", not "/v1/chat/completions": BaseURL now
	// carries its own version/compatibility-shim segment (see
	// GeminiDefaultBaseURL's own doc comment on why that can't be a
	// fixed "/v1" assumed here for every provider).
	url := p.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("consigliere: building request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("authorization", "Bearer "+key)

	resp, err := httpClient(p.Client).Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("consigliere: calling provider: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("consigliere: reading response: %w", err)
	}

	var parsed openAICompatResponse
	_ = json.Unmarshal(data, &parsed) // see Anthropic.Reply's identical reasoning for parsing regardless of status

	if resp.StatusCode != http.StatusOK {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return "", fmt.Errorf("consigliere: %s", parsed.Error.Message)
		}
		return "", fmt.Errorf("consigliere: provider returned status %d", resp.StatusCode)
	}

	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("consigliere: provider reply had no content")
	}
	return parsed.Choices[0].Message.Content, nil
}

// toOpenAICompatMessages prepends req.System+req.Snapshot (combined
// via combineConsigliereSystem) as a single "system" message —
// skipped entirely if both are empty, since an empty system message
// is pure noise, not a real instruction — then translates the chat
// history the same way toAnthropicMessages does for Anthropic's own
// messages array.
func toOpenAICompatMessages(req Request) []openAICompatMessage {
	out := make([]openAICompatMessage, 0, len(req.History)+1)
	if sys := combineConsigliereSystem(req.System, req.Snapshot); sys != "" {
		out = append(out, openAICompatMessage{Role: "system", Content: sys})
	}
	for _, m := range req.History {
		role := "user"
		if m.Role == RoleAssistant {
			role = "assistant"
		}
		out = append(out, openAICompatMessage{Role: role, Content: m.Text})
	}
	return out
}
