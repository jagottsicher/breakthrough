package ui

import (
	"context"
	"time"

	"github.com/jagottsicher/breakthrough/internal/apikey"
	"github.com/jagottsicher/breakthrough/internal/consigliere"
)

// consigliereModelFetchTimeout bounds the Options screen's own
// "list models" background fetch — the same httpTimeout reasoning
// internal/consigliere's own Reply calls already follow, just a
// separate constant since this is a different call entirely.
const consigliereModelFetchTimeout = 15 * time.Second

// listConsigliereModelsFunc is the real "list this provider's
// available models" call — a package-level var, like dialFunc in
// connectdialog.go, so a test can fake it without a real network
// call. Dispatches to the right internal/consigliere function for
// whichever provider is asked for, the same switch
// buildConsigliereProvider's own doc comment establishes for Reply
// itself.
var listConsigliereModelsFunc = func(ctx context.Context, providerName, key string) ([]consigliere.ModelInfo, error) {
	switch providerName {
	case "openai":
		return consigliere.ListOpenAICompatModels(ctx, key, consigliere.OpenAIDefaultBaseURL, nil)
	case "deepseek":
		return consigliere.ListOpenAICompatModels(ctx, key, consigliere.DeepSeekDefaultBaseURL, nil)
	default:
		return consigliere.ListAnthropicModels(ctx, key, consigliere.AnthropicDefaultBaseURL, nil)
	}
}

// refreshConsigliereModelCache fetches the currently selected
// provider's own model list in the background, if it isn't already
// cached or being fetched — called only from a real user action
// (opening/re-entering the "AI Consigliere" category, or changing the
// Provider row — see openOptions/renderOptionCategories/the Provider
// row's own apply), never from consigliereModelChoices/
// consigliereEffortChoices themselves: those are called from
// renderOptionValue on every single table render (and from hundreds
// of existing tests via openOptions), so a network call there would
// put a provider API call on the render path.
//
// A key that isn't available yet (locked, or genuinely unconfigured)
// is recorded as the fetch's own error rather than attempted at all —
// there's no point making a request certain to fail with a 401, and
// doing so would also be indistinguishable from the real DNS gateway
// of "there's no key" the user actually needs to fix.
func (r *Root) refreshConsigliereModelCache() {
	provider := r.settings.ConsigliereProvider
	if r.consigliereModelFetchInFlight[provider] {
		return
	}
	if _, cached := r.consigliereModelCache[provider]; cached {
		return
	}

	key, tier, err := apikey.Resolve(consigliereEnvVarForProvider(provider), r.settings.ConsigliereAPIKeyFile, provider, r.apiKeySession)
	if err == nil && tier == apikey.TierNone {
		err = errConsigliereNoAPIKey
	}
	if err != nil {
		r.consigliereModelFetchErr[provider] = err
		return
	}

	r.consigliereModelFetchInFlight[provider] = true
	delete(r.consigliereModelFetchErr, provider)

	ctx, cancel := context.WithTimeout(context.Background(), consigliereModelFetchTimeout)
	r.safeGo("AI Consigliere model list", func() {
		cancel()
		r.consigliereModelFetchInFlight[provider] = false
	}, func() {
		models, err := listConsigliereModelsFunc(ctx, provider, key)
		cancel()
		r.app.QueueUpdateDraw(func() {
			r.finishConsigliereModelFetch(provider, models, err)
		})
	})
}

// finishConsigliereModelFetch applies listConsigliereModelsFunc's own
// outcome — split out from refreshConsigliereModelCache specifically
// so it's callable directly, without needing a real Application event
// loop to drain QueueUpdateDraw first (the same reasoning
// finishConnect's/showSedPreviewResult's own identical doc comments
// give for the identical split).
func (r *Root) finishConsigliereModelFetch(provider string, models []consigliere.ModelInfo, err error) {
	r.consigliereModelFetchInFlight[provider] = false
	if err != nil {
		r.consigliereModelFetchErr[provider] = err
		return
	}
	r.consigliereModelCache[provider] = models
	r.renderOptions()
}

// consigliereDefaultModelFor is the literal each provider's own
// Default*Model constant maps to — the same switch
// buildConsigliereProvider/listConsigliereModelsFunc already use,
// pulled out once since consigliereModelChoices needs it too.
func consigliereDefaultModelFor(provider string) string {
	switch provider {
	case "openai":
		return consigliere.OpenAIDefaultModel
	case "deepseek":
		return consigliere.DeepSeekDefaultModel
	default:
		return consigliere.AnthropicDefaultModel
	}
}

// consigliereModelChoices is the "Model" row's own choices — reads
// consigliereModelCache only, never fetches (see
// refreshConsigliereModelCache's own doc comment on why). Always
// offers "" (the provider's own built-in default) first, and always
// includes whatever's currently stored even when the live list isn't
// available yet or doesn't contain it anymore — a real, reported risk
// otherwise: cycleOptionChoice falls back to index 0 when the current
// value isn't among the choices, which would silently replace a
// deliberate override with "default" the moment Enter is pressed.
func (r *Root) consigliereModelChoices() []optionChoice {
	provider := r.settings.ConsigliereProvider
	choices := []optionChoice{{value: "", label: "(default: " + consigliereDefaultModelFor(provider) + ")"}}

	cached, ok := r.consigliereModelCache[provider]
	if !ok {
		if v := r.settings.ConsigliereModel; v != "" {
			choices = append(choices, optionChoice{value: v, label: v})
		}
		return choices
	}
	for _, m := range cached {
		choices = append(choices, optionChoice{value: m.ID, label: m.ID})
	}
	return ensureConsigliereChoiceIncludes(choices, r.settings.ConsigliereModel)
}

// consigliereEffortLevelsFor returns model's own supported effort
// levels: OpenAI's fixed, hardcoded set (its /models response never
// says — see consigliere.OpenAIEffortLevels's own doc comment), or
// whichever cached ModelInfo matches model's own ID for Anthropic/
// DeepSeek, both of which do report this per model. nil when the
// cache isn't populated yet, or doesn't contain this exact model.
func (r *Root) consigliereEffortLevelsFor(provider, model string) []string {
	if provider == "openai" {
		return consigliere.OpenAIEffortLevels
	}
	for _, m := range r.consigliereModelCache[provider] {
		if m.ID == model {
			return m.EffortLevels
		}
	}
	return nil
}

// consigliereEffortChoices is the "Effort" row's own choices — see
// consigliereModelChoices' own doc comment for the identical
// render-path-must-never-fetch and never-silently-discard-a-stored-
// override reasoning, applied here to effort levels instead of models.
func (r *Root) consigliereEffortChoices() []optionChoice {
	provider := r.settings.ConsigliereProvider
	model := r.settings.ConsigliereModel
	if model == "" {
		model = consigliereDefaultModelFor(provider)
	}

	choices := []optionChoice{{value: "", label: "(model default)"}}
	for _, lvl := range r.consigliereEffortLevelsFor(provider, model) {
		choices = append(choices, optionChoice{value: lvl, label: lvl})
	}
	return ensureConsigliereChoiceIncludes(choices, r.settings.ConsigliereEffort)
}

// ensureConsigliereChoiceIncludes appends a {value, label: value}
// choice for current if it's non-empty and not already present —
// shared by consigliereModelChoices/consigliereEffortChoices.
func ensureConsigliereChoiceIncludes(choices []optionChoice, current string) []optionChoice {
	if current == "" {
		return choices
	}
	for _, c := range choices {
		if c.value == current {
			return choices
		}
	}
	return append(choices, optionChoice{value: current, label: current})
}
