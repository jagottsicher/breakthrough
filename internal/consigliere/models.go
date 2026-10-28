package consigliere

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// ModelInfo is one model a provider's own "list models" endpoint
// reports as currently available — just enough to populate the
// Options screen's own Model/Effort dropdowns (see internal/ui's
// refreshConsigliereModelCache).
//
// Deliberately not part of the Provider interface: Mock implements
// Provider and has no API to list anything against, and listing
// models is an Options-screen concern, not something Reply itself
// ever needs.
type ModelInfo struct {
	ID string
	// EffortLevels is the set of valid "effort"/"reasoning_effort"
	// values this specific model supports, in the provider's own
	// documented order. Anthropic and DeepSeek both report this per
	// model in their own "list models" response (capabilities.effort,
	// effort.supported_levels respectively); nil here means the
	// provider's response didn't say (OpenAI never does — see
	// OpenAIEffortLevels for its fixed, hardcoded fallback instead).
	EffortLevels []string
}

// OpenAIEffortLevels is the fixed, documented reasoning_effort enum
// (developers.openai.com/api/docs/api-reference/chat/create, checked
// 2026-10-27) — offered for every OpenAI model regardless, since
// OpenAI's own GET /v1/models response never exposes this per model
// the way Anthropic's/DeepSeek's do (see ModelInfo.EffortLevels).
// An unsupported value for a specific model is rejected by the
// request itself, surfaced through the usual error path, rather than
// filtered out here.
var OpenAIEffortLevels = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

// anthropicModelsResponse is GET /v1/models' own response shape
// (platform.claude.com/docs/en/api/models/list, checked 2026-10-27) —
// only the fields this package actually reads; every other
// documented field (max_input_tokens, created_at, line, ...) is
// irrelevant to populating a Model dropdown and deliberately omitted.
type anthropicModelsResponse struct {
	Data []anthropicModelEntry `json:"data"`
}

type anthropicCapabilitySupport struct {
	Supported bool `json:"supported"`
}

type anthropicModelEntry struct {
	ID           string `json:"id"`
	Lifecycle    string `json:"lifecycle"`
	Capabilities struct {
		Effort struct {
			Low    anthropicCapabilitySupport `json:"low"`
			Medium anthropicCapabilitySupport `json:"medium"`
			High   anthropicCapabilitySupport `json:"high"`
			Max    anthropicCapabilitySupport `json:"max"`
			XHigh  anthropicCapabilitySupport `json:"xhigh"`
		} `json:"effort"`
	} `json:"capabilities"`
}

// effortLevels collects whichever of low/medium/high/xhigh/max this
// entry's own capabilities.effort reports as supported, in that fixed
// order — the same order the Effort documentation itself lists them
// in (platform.claude.com/docs/en/build-with-claude/effort).
func (e anthropicModelEntry) effortLevels() []string {
	var out []string
	if e.Capabilities.Effort.Low.Supported {
		out = append(out, "low")
	}
	if e.Capabilities.Effort.Medium.Supported {
		out = append(out, "medium")
	}
	if e.Capabilities.Effort.High.Supported {
		out = append(out, "high")
	}
	if e.Capabilities.Effort.XHigh.Supported {
		out = append(out, "xhigh")
	}
	if e.Capabilities.Effort.Max.Supported {
		out = append(out, "max")
	}
	return out
}

// ListAnthropicModels calls Anthropic's own GET /v1/models and
// returns every currently "active" model (lifecycle=active is passed
// as a query parameter, and double-checked client-side — a
// deprecated or retired model would otherwise still work today but
// warn or fail the next time this list is refreshed, exactly the
// "currently available" promise the caller asked for), each with its
// own supported effort levels.
//
// limit=1000 is passed explicitly: the endpoint's own default is 20
// with cursor pagination (has_more/last_id) — silently truncating the
// list at 20 models would be a real, easy-to-miss bug as Anthropic's
// own lineup grows.
func ListAnthropicModels(ctx context.Context, apiKey, baseURL string, client *http.Client) ([]ModelInfo, error) {
	url := firstNonEmpty(baseURL, AnthropicDefaultBaseURL) + "/v1/models?limit=1000&lifecycle=active"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("consigliere: building Anthropic models request: %w", err)
	}
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := httpClient(client).Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("consigliere: calling Anthropic models endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("consigliere: reading Anthropic models response: %w", err)
	}

	var parsed anthropicModelsResponse
	_ = json.Unmarshal(data, &parsed)

	if resp.StatusCode != http.StatusOK {
		var errBody anthropicResponse
		_ = json.Unmarshal(data, &errBody)
		if errBody.Error != nil && errBody.Error.Message != "" {
			return nil, fmt.Errorf("consigliere: Anthropic: %s", errBody.Error.Message)
		}
		return nil, fmt.Errorf("consigliere: Anthropic models endpoint returned status %d", resp.StatusCode)
	}

	out := make([]ModelInfo, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		if m.Lifecycle != "" && m.Lifecycle != "active" {
			continue
		}
		out = append(out, ModelInfo{ID: m.ID, EffortLevels: m.effortLevels()})
	}
	return out, nil
}

// openAICompatModelsResponse is GET /v1/models' own response shape,
// shared by OpenAI and DeepSeek (OpenAI's own own version never
// populates Effort at all — see ModelInfo.EffortLevels — DeepSeek's
// own does, via this exact field).
type openAICompatModelsResponse struct {
	Data []openAICompatModelEntry `json:"data"`
}

type openAICompatModelEntry struct {
	ID     string `json:"id"`
	Effort *struct {
		SupportedLevels []string `json:"supported_levels"`
	} `json:"effort"`
}

// ListOpenAICompatModels calls GET /v1/models against baseURL — used
// for both OpenAI and DeepSeek, the same split Anthropic/OpenAICompat
// already draw for the Reply path itself.
func ListOpenAICompatModels(ctx context.Context, apiKey, baseURL string, client *http.Client) ([]ModelInfo, error) {
	// "/models", not "/v1/models" — see OpenAICompat.Reply's own doc
	// comment on why baseURL already carries its own version segment.
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("consigliere: building models request: %w", err)
	}
	httpReq.Header.Set("authorization", "Bearer "+apiKey)

	resp, err := httpClient(client).Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("consigliere: calling models endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("consigliere: reading models response: %w", err)
	}

	var parsed openAICompatModelsResponse
	_ = json.Unmarshal(data, &parsed)

	if resp.StatusCode != http.StatusOK {
		var errBody openAICompatResponse
		_ = json.Unmarshal(data, &errBody)
		if errBody.Error != nil && errBody.Error.Message != "" {
			return nil, fmt.Errorf("consigliere: %s", errBody.Error.Message)
		}
		return nil, fmt.Errorf("consigliere: models endpoint returned status %d", resp.StatusCode)
	}

	out := make([]ModelInfo, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		info := ModelInfo{ID: m.ID}
		if m.Effort != nil {
			info.EffortLevels = m.Effort.SupportedLevels
		}
		out = append(out, info)
	}
	return out, nil
}
