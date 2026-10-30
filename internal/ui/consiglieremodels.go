package ui

import (
	"context"
	"errors"
	"time"

	"github.com/jagottsicher/breakthrough/internal/apikey"
	"github.com/jagottsicher/breakthrough/internal/consigliere"
)

// consigliereModelFetchTimeout bounds the Options screen's own
// "list models" background fetch — the same httpTimeout reasoning
// internal/consigliere's own Reply calls already follow, just a
// separate constant since this is a different call entirely.
const consigliereModelFetchTimeout = 15 * time.Second

// consigliereTypedEntrySentinel is a reserved optionChoice.value real
// model/effort IDs can never collide with (a NUL byte never appears in
// one) — appended to the Model/Effort rows' own choices (see
// consigliereModelChoices/consigliereEffortChoices) so cycling onto it
// opens a free-text entry instead of applying it literally (see
// cycleOptionChoice's own doc comment on this one exception). A real,
// reported gap without it: those two rows otherwise only ever offer
// what a live fetch returned or what's already stored, with no way to
// type a brand new ID — e.g. while the fetch itself is locked behind
// an unentered passphrase, or failed outright.
const consigliereTypedEntrySentinel = "\x00consigliere-typed-entry\x00"

// listConsigliereModelsFunc is the real "list this provider's
// available models" call — a package-level var, like dialFunc in
// connectdialog.go, so a test can fake it without a real network
// call. Dispatches to the right internal/consigliere function for
// whichever provider is asked for, the same switch
// buildConsigliereProvider's own doc comment establishes for Reply
// itself.
var listConsigliereModelsFunc = func(ctx context.Context, providerName, key, baseURLOverride string) ([]consigliere.ModelInfo, error) {
	switch providerName {
	case "openai":
		return consigliere.ListOpenAICompatModels(ctx, key, consigliere.OpenAIDefaultBaseURL, nil)
	case "deepseek":
		return consigliere.ListOpenAICompatModels(ctx, key, consigliere.DeepSeekDefaultBaseURL, nil)
	case "gemini":
		return consigliere.ListOpenAICompatModels(ctx, key, consigliere.GeminiDefaultBaseURL, nil)
	case "ollama":
		// baseURLOverride, not the bare constant: see
		// ConsigliereOllamaBaseURL's own doc comment
		// (internal/config/settings.go) — the model list has to come
		// from the same server Reply itself will actually talk to.
		return listOllamaModelsWithNativeFallback(ctx, key, firstNonEmptyConsigliereModel(baseURLOverride, consigliere.OllamaDefaultBaseURL))
	case "custom":
		// No built-in fallback at all — see ConsigliereCustomBaseURL's
		// own doc comment: this provider exists specifically for an
		// endpoint this app has no default for in the first place.
		return consigliere.ListOpenAICompatModels(ctx, key, baseURLOverride, nil)
	default:
		return consigliere.ListAnthropicModels(ctx, key, consigliere.AnthropicDefaultBaseURL, nil)
	}
}

// listOllamaModelsWithNativeFallback is listConsigliereModelsFunc's
// own "ollama" case, pulled out as a named function — unlike that
// package-level var, never overridden by TestMain's own
// no-real-network-calls default, so a test can exercise its real
// fallback logic directly against a local httptest server instead of
// only ever seeing the fake.
//
// A real, reported case this fallback exists for: some
// Ollama-compatible servers (not Ollama's own reference
// implementation) answer real chat questions and list models through
// their own native API, but never implement the OpenAI-compatible GET
// /v1/models at all, 404ing on it permanently. Tried only as a
// fallback, not the first attempt, since Ollama's own real server
// already supports /v1/models directly and shouldn't need the extra
// round trip.
func listOllamaModelsWithNativeFallback(ctx context.Context, key, baseURL string) ([]consigliere.ModelInfo, error) {
	models, err := consigliere.ListOpenAICompatModels(ctx, key, baseURL, nil)
	if err != nil {
		if native, nativeErr := consigliere.ListOllamaNativeModels(ctx, baseURL, nil); nativeErr == nil {
			return native, nil
		}
	}
	return models, err
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

	// Ollama's own local server never checks a key at all (see
	// OllamaDefaultBaseURL's own doc comment) — the usual
	// apikey.Resolve gate would otherwise always report
	// errConsigliereNoAPIKey for it and never even attempt the fetch,
	// since nothing is ever configured in Options for a provider that
	// needs nothing there.
	key := "ollama"
	if !consigliereProviderNeedsNoKey(provider) {
		var tier apikey.Tier
		var err error
		key, tier, err = apikey.Resolve(consigliereEnvVarForProvider(provider), r.settings.ConsigliereAPIKeyFileFor(provider), provider, r.apiKeySession)
		if err == nil && tier == apikey.TierNone {
			// Resolve itself treats a locked encrypted tier exactly
			// like "not configured at all" (see its own doc comment)
			// — distinguished here instead, same as
			// buildConsigliereProvider's own keyFunc, so the Options
			// screen can tell "nothing is configured anywhere" (a real
			// gap) apart from "a key exists, just needs the
			// passphrase" (nothing to fix, just unlock).
			if r.apiKeySession != nil && r.apiKeySession.HasProvider(provider) && !r.apiKeySession.IsUnlocked() {
				err = errConsigliereLocked
			} else {
				err = errConsigliereNoAPIKey
			}
		}
		if err != nil {
			r.consigliereModelFetchErr[provider] = err
			return
		}
	}

	r.consigliereModelFetchInFlight[provider] = true
	delete(r.consigliereModelFetchErr, provider)

	ctx, cancel := context.WithTimeout(context.Background(), consigliereModelFetchTimeout)
	r.safeGo("AI Consigliere model list", func() {
		cancel()
		r.consigliereModelFetchInFlight[provider] = false
	}, func() {
		models, err := listConsigliereModelsFunc(ctx, provider, key, r.consigliereBaseURLOverride(provider))
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
// pulled out once since consigliereModelChoices needs it too. ""
// means this provider has no built-in default model name of its own
// at all — currently only "custom" (see ConsigliereCustomBaseURL's
// own doc comment on why), and any provider name this switch doesn't
// otherwise recognize, which must never silently claim Anthropic's
// own default the way falling through a bare "default:" case here
// once did.
func consigliereDefaultModelFor(provider string) string {
	switch provider {
	case "anthropic":
		return consigliere.AnthropicDefaultModel
	case "openai":
		return consigliere.OpenAIDefaultModel
	case "deepseek":
		return consigliere.DeepSeekDefaultModel
	case "gemini":
		return consigliere.GeminiDefaultModel
	case "ollama":
		return consigliere.OllamaDefaultModel
	default:
		return ""
	}
}

// consigliereDefaultChoiceLabel is the "" choice's own label for both
// consigliereModelChoices and consigliereEffortChoices — once a real,
// non-empty defaultText is actually backed by a successful fetch for
// this provider, shown as "(<defaultText>)"; otherwise one of three
// distinct reasons why not, rather than a single catch-all "(not
// available)" that used to make "there's a key stored, just enter the
// passphrase to use it" indistinguishable from "nothing is configured
// here at all" — a real, reported gap:
//
//   - fetchErr is errConsigliereLocked: a key is stored encrypted but
//     the passphrase hasn't been entered yet this run — nothing to
//     fix, just unlock it (e.g. by asking the sidebar a question, or
//     "API key (encrypted)" below).
//   - fetchErr is errConsigliereNoAPIKey: no tier has anything at all
//     for this provider — something to actually configure.
//   - any other fetchErr, or none yet (still fetching, or this
//     provider needs no key — e.g. Ollama — but the fetch itself
//     hasn't completed/succeeded for some other reason): the generic
//     "(not available)".
//
// defaultText == "" (currently only "custom", which has no built-in
// default model of its own at all — see ConsigliereCustomBaseURL's
// own doc comment) always takes the generic case too, regardless of
// fetchErr: there's nothing to confirm via a fetch in the first place.
func consigliereDefaultChoiceLabel(fetchErr error, fetched bool, defaultText string) string {
	switch {
	case errors.Is(fetchErr, errConsigliereLocked):
		return "(enter passphrase to see)"
	case errors.Is(fetchErr, errConsigliereNoAPIKey):
		return "(no API key configured)"
	case !fetched || defaultText == "":
		return "(not available)"
	default:
		return "(" + defaultText + ")"
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
	cached, ok := r.consigliereModelCache[provider]

	defaultText := ""
	if d := consigliereDefaultModelFor(provider); d != "" {
		defaultText = "default: " + d
	}
	choices := []optionChoice{{value: "", label: consigliereDefaultChoiceLabel(r.consigliereModelFetchErr[provider], ok, defaultText)}}

	if !ok {
		if v := r.settings.ConsigliereModelFor(provider); v != "" {
			choices = append(choices, optionChoice{value: v, label: v})
		}
		return appendConsigliereTypedEntryChoice(choices)
	}
	for _, m := range cached {
		choices = append(choices, optionChoice{value: m.ID, label: m.ID})
	}
	choices = ensureConsigliereChoiceIncludes(choices, r.settings.ConsigliereModelFor(provider))
	return appendConsigliereTypedEntryChoice(choices)
}

// appendConsigliereTypedEntryChoice adds consigliereTypedEntrySentinel
// as the last choice — shared by consigliereModelChoices/
// consigliereEffortChoices, both of whose every other choice comes
// from a live fetch or an already-stored override (see the sentinel's
// own doc comment on why that's not enough on its own).
func appendConsigliereTypedEntryChoice(choices []optionChoice) []optionChoice {
	return append(choices, optionChoice{value: consigliereTypedEntrySentinel, label: "(enter manually...)"})
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
	model := r.settings.ConsigliereModelFor(provider)
	if model == "" {
		model = consigliereDefaultModelFor(provider)
	}
	_, fetched := r.consigliereModelCache[provider]

	defaultText := ""
	if fetched {
		defaultText = "model default"
	}
	choices := []optionChoice{{value: "", label: consigliereDefaultChoiceLabel(r.consigliereModelFetchErr[provider], fetched, defaultText)}}
	for _, lvl := range r.consigliereEffortLevelsFor(provider, model) {
		choices = append(choices, optionChoice{value: lvl, label: lvl})
	}
	choices = ensureConsigliereChoiceIncludes(choices, r.settings.ConsigliereEffortFor(provider))
	return appendConsigliereTypedEntryChoice(choices)
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
