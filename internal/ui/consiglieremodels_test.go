package ui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/apikey"
	"github.com/jagottsicher/breakthrough/internal/consigliere"
)

// withFakeConsigliereModelList installs fn as listConsigliereModelsFunc
// for the duration of the test, restoring TestMain's own "no fake
// installed" default afterwards — the same t.Cleanup-scoped-override
// shape every other swappable var in this package's tests already
// uses.
func withFakeConsigliereModelList(t *testing.T, fn func(ctx context.Context, providerName, key, ollamaBaseURLOverride string) ([]consigliere.ModelInfo, error)) {
	t.Helper()
	previous := listConsigliereModelsFunc
	listConsigliereModelsFunc = fn
	t.Cleanup(func() { listConsigliereModelsFunc = previous })
}

// rootWithConsigliereKey returns a Root whose "anthropic" provider
// already has a usable key — via the env var tier, the simplest one
// to set up in a test — so refreshConsigliereModelCache actually
// attempts a fetch instead of recording errConsigliereNoAPIKey.
func rootWithConsigliereKey(t *testing.T) *Root {
	t.Helper()
	r := newPlainKeyRoot(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")
	return r
}

// TestConsigliereProviderNeedsNoKeyOnlyTrueForOllama pins the one
// deliberate exception to the usual three-tier key chain — Ollama's
// own local server never checks a key at all (see
// OllamaDefaultBaseURL's own doc comment); every other provider still
// needs a real one.
func TestConsigliereProviderNeedsNoKeyOnlyTrueForOllama(t *testing.T) {
	for _, provider := range []string{"anthropic", "openai", "deepseek", "gemini"} {
		if consigliereProviderNeedsNoKey(provider) {
			t.Errorf("consigliereProviderNeedsNoKey(%q) = true, want false", provider)
		}
	}
	if !consigliereProviderNeedsNoKey("ollama") {
		t.Error(`consigliereProviderNeedsNoKey("ollama") = false, want true`)
	}
}

// TestRefreshConsigliereModelCacheFetchesForOllamaWithoutAnyKeyConfigured
// pins the real bug the usual apikey.Resolve gate would otherwise
// cause: Ollama has nothing to configure in Options at all, so the
// fetch must still be attempted (not skipped as "no API key") with
// the placeholder key every Ollama OpenAI-compat client is documented
// to need.
func TestRefreshConsigliereModelCacheFetchesForOllamaWithoutAnyKeyConfigured(t *testing.T) {
	r := newPlainKeyRoot(t)
	r.settings.ConsigliereProvider = "ollama"
	type call struct{ providerName, key string }
	calls := make(chan call, 1)
	withFakeConsigliereModelList(t, func(ctx context.Context, providerName, key, ollamaBaseURLOverride string) ([]consigliere.ModelInfo, error) {
		calls <- call{providerName, key}
		return nil, nil
	})

	r.refreshConsigliereModelCache()

	select {
	case got := <-calls:
		if got.providerName != "ollama" {
			t.Errorf("fetch called with provider %q, want %q", got.providerName, "ollama")
		}
		if got.key == "" {
			t.Error("fetch called with an empty key — want the documented placeholder, not a blank Authorization header")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fetch was never called — want it attempted even though nothing is configured in Options")
	}
	if err := r.consigliereModelFetchErr["ollama"]; err != nil {
		t.Errorf("consigliereModelFetchErr[ollama] = %v, want none", err)
	}
}

// TestRefreshConsigliereModelCachePassesTheConfiguredOllamaBaseURL
// pins a real, reported gap: the model list has to come from the
// same server Reply itself will actually talk to, not always the
// built-in localhost default.
func TestRefreshConsigliereModelCachePassesTheConfiguredOllamaBaseURL(t *testing.T) {
	r := newPlainKeyRoot(t)
	r.settings.ConsigliereProvider = "ollama"
	r.settings.ConsigliereOllamaBaseURL = "http://192.168.1.50:11434/v1"
	calls := make(chan string, 1)
	withFakeConsigliereModelList(t, func(ctx context.Context, providerName, key, ollamaBaseURLOverride string) ([]consigliere.ModelInfo, error) {
		calls <- ollamaBaseURLOverride
		return nil, nil
	})

	r.refreshConsigliereModelCache()

	select {
	case got := <-calls:
		if want := "http://192.168.1.50:11434/v1"; got != want {
			t.Errorf("fetch called with ollamaBaseURLOverride = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fetch was never called")
	}
}

// TestRefreshConsigliereModelCachePassesTheConfiguredCustomBaseURL is
// TestRefreshConsigliereModelCachePassesTheConfiguredOllamaBaseURL's
// own counterpart for "custom" — the same real, reported gap, just for
// the provider that has no built-in default of its own at all, rather
// than one that falls back to localhost.
// TestListOllamaModelsWithNativeFallbackUsesNativeTagsWhenV1ModelsFails
// pins the user's own explicit report: a real Ollama-compatible
// server that answers chat questions and lists models through its own
// native /api/tags, but 404s on the OpenAI-compatible GET /v1/models
// permanently — the real listConsigliereModelsFunc dispatch (not the
// TestMain-overridden package var, which never reaches this), against
// a local httptest server that behaves exactly that way.
func TestListOllamaModelsWithNativeFallbackUsesNativeTagsWhenV1ModelsFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusNotFound)
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"qwen2.5:1.5b"},{"name":"llama3.2:1b"}]}`))
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	models, err := listOllamaModelsWithNativeFallback(context.Background(), "", server.URL+"/v1")
	if err != nil {
		t.Fatalf("listOllamaModelsWithNativeFallback: %v", err)
	}
	if len(models) != 2 || models[0].ID != "qwen2.5:1.5b" || models[1].ID != "llama3.2:1b" {
		t.Errorf("models = %+v, want the native /api/tags result", models)
	}
}

// TestListOllamaModelsWithNativeFallbackPrefersV1ModelsWhenItWorks
// pins the other half: a real Ollama server's own /v1/models is tried
// first and used as-is — the fallback never fires, so an extra round
// trip only ever happens for a server that actually needs it.
func TestListOllamaModelsWithNativeFallbackPrefersV1ModelsWhenItWorks(t *testing.T) {
	tagsCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"llama3.1"}]}`))
		case "/api/tags":
			tagsCalled = true
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	models, err := listOllamaModelsWithNativeFallback(context.Background(), "", server.URL+"/v1")
	if err != nil {
		t.Fatalf("listOllamaModelsWithNativeFallback: %v", err)
	}
	if len(models) != 1 || models[0].ID != "llama3.1" {
		t.Errorf("models = %+v, want the /v1/models result", models)
	}
	if tagsCalled {
		t.Error("/api/tags should never be called when /v1/models already succeeded")
	}
}

func TestRefreshConsigliereModelCachePassesTheConfiguredCustomBaseURL(t *testing.T) {
	r := newPlainKeyRoot(t)
	t.Setenv("BREAKTHROUGH_CUSTOM_API_KEY", "custom-test-key")
	r.settings.ConsigliereProvider = "custom"
	r.settings.ConsigliereCustomBaseURL = "https://api.publicai.co/v1"
	calls := make(chan string, 1)
	withFakeConsigliereModelList(t, func(ctx context.Context, providerName, key, ollamaBaseURLOverride string) ([]consigliere.ModelInfo, error) {
		calls <- ollamaBaseURLOverride
		return nil, nil
	})

	r.refreshConsigliereModelCache()

	select {
	case got := <-calls:
		if want := "https://api.publicai.co/v1"; got != want {
			t.Errorf("fetch called with baseURLOverride = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fetch was never called")
	}
}

// TestRefreshConsigliereModelCacheCallsFetchWithResolvedKey pins the
// synchronous half: a key is actually available, so the fetch is
// attempted (not skipped) with the right provider/key pair, and
// marked in-flight. Doesn't wait for the goroutine's own completion —
// see TestFinishConsigliereModelFetchPopulatesCacheAndRerenders for
// that half, called directly for the same reason finishConnect's own
// doc comment gives.
func TestRefreshConsigliereModelCacheCallsFetchWithResolvedKey(t *testing.T) {
	r := rootWithConsigliereKey(t)
	type call struct{ providerName, key string }
	calls := make(chan call, 1)
	withFakeConsigliereModelList(t, func(ctx context.Context, providerName, key, ollamaBaseURLOverride string) ([]consigliere.ModelInfo, error) {
		calls <- call{providerName, key}
		return nil, nil
	})

	r.refreshConsigliereModelCache()

	select {
	case got := <-calls:
		if got.providerName != "anthropic" || got.key != "sk-ant-test-key" {
			t.Errorf("fetch called with (%q, %q)", got.providerName, got.key)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fetch was never called")
	}
}

func TestFinishConsigliereModelFetchPopulatesCacheAndRerenders(t *testing.T) {
	r := newPlainKeyRoot(t)
	r.consigliereModelFetchInFlight["anthropic"] = true
	want := []consigliere.ModelInfo{{ID: "claude-opus-5-5", EffortLevels: []string{"low", "high"}}}

	r.finishConsigliereModelFetch("anthropic", want, nil)

	if cached, ok := r.consigliereModelCache["anthropic"]; !ok || len(cached) != 1 || cached[0].ID != want[0].ID {
		t.Errorf("consigliereModelCache[anthropic] = %+v, want %+v", cached, want)
	}
	if r.consigliereModelFetchInFlight["anthropic"] {
		t.Error("fetch should no longer be marked in-flight once it completes")
	}
}

func TestFinishConsigliereModelFetchRecordsErrorWithoutTouchingCache(t *testing.T) {
	r := newPlainKeyRoot(t)
	r.consigliereModelFetchInFlight["anthropic"] = true
	wantErr := errors.New("network unreachable")

	r.finishConsigliereModelFetch("anthropic", nil, wantErr)

	if _, ok := r.consigliereModelCache["anthropic"]; ok {
		t.Error("a failed fetch should not populate the cache")
	}
	if !errors.Is(r.consigliereModelFetchErr["anthropic"], wantErr) {
		t.Errorf("consigliereModelFetchErr[anthropic] = %v, want %v", r.consigliereModelFetchErr["anthropic"], wantErr)
	}
}

func TestRefreshConsigliereModelCacheSkipsWithoutAKey(t *testing.T) {
	r := newPlainKeyRoot(t) // no ANTHROPIC_API_KEY set — TestMain blanks it
	called := false
	withFakeConsigliereModelList(t, func(ctx context.Context, providerName, key, ollamaBaseURLOverride string) ([]consigliere.ModelInfo, error) {
		called = true
		return nil, nil
	})

	r.refreshConsigliereModelCache()

	if called {
		t.Error("should never call the fetch func when no API key is available at all")
	}
	if err := r.consigliereModelFetchErr["anthropic"]; !errors.Is(err, errConsigliereNoAPIKey) {
		t.Errorf("consigliereModelFetchErr[anthropic] = %v, want errConsigliereNoAPIKey", err)
	}
}

func TestRefreshConsigliereModelCacheSkipsWhenAlreadyCached(t *testing.T) {
	r := rootWithConsigliereKey(t)
	r.consigliereModelCache["anthropic"] = []consigliere.ModelInfo{{ID: "already-cached"}}
	called := false
	withFakeConsigliereModelList(t, func(ctx context.Context, providerName, key, ollamaBaseURLOverride string) ([]consigliere.ModelInfo, error) {
		called = true
		return nil, nil
	})

	r.refreshConsigliereModelCache()

	if called {
		t.Error("should never fetch when this provider is already cached")
	}
}

// TestRefreshConsigliereModelCacheSkipsWhenAlreadyInFlight pins the
// other dedup guard: a second call while the first fetch's own
// background goroutine (see refreshConsigliereModelCache's own doc
// comment on why that's a real goroutine, not something this test can
// just wait on with a fixed sleep) is still running must not start a
// second, redundant fetch for the same provider. Synchronized via a
// channel the fake itself closes once it has actually started,
// avoiding any sleep-based race.
func TestRefreshConsigliereModelCacheSkipsWhenAlreadyInFlight(t *testing.T) {
	r := rootWithConsigliereKey(t)
	started := make(chan struct{})
	unblock := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	withFakeConsigliereModelList(t, func(ctx context.Context, providerName, key, ollamaBaseURLOverride string) ([]consigliere.ModelInfo, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		close(started)
		<-unblock
		return nil, nil
	})

	r.refreshConsigliereModelCache()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first fetch never started")
	}

	r.refreshConsigliereModelCache() // should be a no-op: still in flight

	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 1 {
		t.Errorf("fetch called %d times while the first was still running, want exactly 1", got)
	}
	close(unblock) // let the first fetch's own goroutine finish instead of leaking it
}

func TestConsigliereModelChoicesAlwaysIncludesStoredOverrideEvenWithoutACache(t *testing.T) {
	r := newPlainKeyRoot(t)
	r.settings.ConsigliereAnthropicModel = "claude-haiku-5-5"

	choices := r.consigliereModelChoices()

	var found bool
	for _, c := range choices {
		if c.value == "claude-haiku-5-5" {
			found = true
		}
	}
	if !found {
		t.Errorf("choices = %+v, want the stored override included even though nothing was ever fetched", choices)
	}
	if choices[0].value != "" {
		t.Errorf("choices[0] = %+v, want the \"(default: ...)\" sentinel first", choices[0])
	}
}

func TestConsigliereModelChoicesFromCacheIncludeEveryFetchedModel(t *testing.T) {
	r := newPlainKeyRoot(t)
	r.consigliereModelCache["anthropic"] = []consigliere.ModelInfo{
		{ID: "claude-opus-5-5"},
		{ID: "claude-haiku-5-5"},
	}

	choices := r.consigliereModelChoices()

	if len(choices) != 4 { // default sentinel + two fetched models + typed-entry sentinel
		t.Fatalf("choices = %+v, want 4 entries", choices)
	}
	if choices[1].value != "claude-opus-5-5" || choices[2].value != "claude-haiku-5-5" {
		t.Errorf("choices = %+v", choices)
	}
}

// TestConsigliereModelChoicesDefaultLabelIsNotAvailableBeforeFetch
// pins the user's own explicit report: claiming a specific default
// model name before the live list has actually been fetched overstates
// what breakthrough really knows — "(not available)", not a model
// name, until a fetch for this provider has actually succeeded.
func TestConsigliereModelChoicesDefaultLabelIsNotAvailableBeforeFetch(t *testing.T) {
	r := newPlainKeyRoot(t)

	choices := r.consigliereModelChoices()

	if choices[0].value != "" || choices[0].label != "(not available)" {
		t.Errorf("choices[0] = %+v, want {value: \"\", label: \"(not available)\"}", choices[0])
	}
}

// TestConsigliereModelChoicesDefaultLabelShowsRealDefaultOnceFetched
// is the other half: once a fetch for this provider has actually
// succeeded, the real built-in default model name is shown.
func TestConsigliereModelChoicesDefaultLabelShowsRealDefaultOnceFetched(t *testing.T) {
	r := newPlainKeyRoot(t)
	r.consigliereModelCache["anthropic"] = []consigliere.ModelInfo{{ID: "claude-opus-5-5"}}

	choices := r.consigliereModelChoices()

	if want := "(default: " + consigliere.AnthropicDefaultModel + ")"; choices[0].label != want {
		t.Errorf("choices[0].label = %q, want %q", choices[0].label, want)
	}
}

// TestConsigliereModelChoicesDefaultLabelStaysNotAvailableForCustom
// pins the bug this was reported for directly: "custom" has no
// built-in default model name of its own at all (unlike every other
// provider), so its own default choice must never claim some other
// provider's own default — "(not available)" even once a fetch has
// succeeded, since there genuinely is nothing to report.
// TestConsigliereModelChoicesDefaultLabelDistinguishesLockedFromNoKey
// pins the user's own explicit report: a key stored but still locked
// behind an unentered passphrase used to look identical to "nothing is
// configured here at all" — both just "(not available)". Now
// distinct: "(enter passphrase to see)" for the first, "(no API key
// configured)" for the second.
func TestConsigliereModelChoicesDefaultLabelDistinguishesLockedFromNoKey(t *testing.T) {
	r := newPlainKeyRoot(t)
	session, err := apikey.OpenSession(filepath.Join(t.TempDir(), "apikeys.json"))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	r.apiKeySession = session

	r.refreshConsigliereModelCache()
	choices := r.consigliereModelChoices()
	if want := "(no API key configured)"; choices[0].label != want {
		t.Errorf("choices[0].label = %q, want %q (no key stored anywhere)", choices[0].label, want)
	}

	if err := session.Unlock("a passphrase"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if err := session.Set("anthropic", "sk-ant-stored"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	session.Lock()
	delete(r.consigliereModelFetchErr, "anthropic")

	r.refreshConsigliereModelCache()
	choices = r.consigliereModelChoices()
	if want := "(enter passphrase to see)"; choices[0].label != want {
		t.Errorf("choices[0].label = %q, want %q (a key is stored, just locked)", choices[0].label, want)
	}
}

// TestConsigliereModelRowBlankCommitResetsToDefault pins the user's
// own explicit report: typing something into Model and then clearing
// the field again used to do nothing at all (editOptionValue's own
// ordinary "blank commit is a no-op" rule — see
// optionSpec.emptyMeansDefault's own doc comment) — the only way back
// to "use the built-in default" was "Reset category", which also
// discards every other AI Consigliere setting (Provider, API key
// file, ...) along with it.
func TestConsigliereModelRowBlankCommitResetsToDefault(t *testing.T) {
	r, _ := newOptionsRoot(t)
	selectOptionCategory(t, r, "AI Consigliere")
	r.settings.ConsigliereAnthropicModel = "some-garbage-the-user-typed"

	row, ok := optionRowByKey(r, "consigliere_anthropic_model")
	if !ok {
		t.Fatal("no consigliere_anthropic_model row")
	}
	opt, _ := r.optionAtRow(row)

	r.editOptionValue(opt)
	r.optionsInput.SetText("")
	r.optionsInput.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if r.settings.ConsigliereAnthropicModel != "" {
		t.Errorf("ConsigliereAnthropicModel = %q, want a blank commit to reset it to the built-in default", r.settings.ConsigliereAnthropicModel)
	}
}

// TestConsigliereModelRowApplyRetriggersTheFetch pins the other half
// of the same report: once Model/Effort has just been typed into (or
// reset), a fetch that was never attempted — or previously failed —
// deserves another try, the same "a value changed, retry" reasoning
// the base-URL rows' own apply already follows (see
// refreshConsigliereModelCacheIfSelected).
func TestConsigliereModelRowApplyRetriggersTheFetch(t *testing.T) {
	r := rootWithConsigliereKey(t)
	for i, cat := range optionCategories() {
		if cat.name == "AI Consigliere" {
			r.optionsCategory = i
		}
	}
	calls := make(chan struct{}, 2)
	withFakeConsigliereModelList(t, func(ctx context.Context, providerName, key, baseURLOverride string) ([]consigliere.ModelInfo, error) {
		calls <- struct{}{}
		return []consigliere.ModelInfo{{ID: "claude-opus-5-5"}}, nil
	})

	row, ok := optionRowByKey(r, "consigliere_anthropic_model")
	if !ok {
		t.Fatal("no consigliere_anthropic_model row")
	}
	opt, _ := r.optionAtRow(row)
	opt.apply(r, "a-typed-in-model")

	select {
	case <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("typing a new model should have retried the model fetch")
	}
}

func TestConsigliereModelChoicesDefaultLabelStaysNotAvailableForCustom(t *testing.T) {
	r := newPlainKeyRoot(t)
	r.settings.ConsigliereProvider = "custom"
	r.consigliereModelCache["custom"] = []consigliere.ModelInfo{{ID: "some-model"}}

	choices := r.consigliereModelChoices()

	if choices[0].value != "" || choices[0].label != "(not available)" {
		t.Errorf("choices[0] = %+v, want {value: \"\", label: \"(not available)\"}", choices[0])
	}
}

// TestConsigliereEffortChoicesDefaultLabelIsNotAvailableBeforeFetch is
// TestConsigliereModelChoicesDefaultLabelIsNotAvailableBeforeFetch's
// own counterpart for the Effort row.
func TestConsigliereEffortChoicesDefaultLabelIsNotAvailableBeforeFetch(t *testing.T) {
	r := newPlainKeyRoot(t)

	choices := r.consigliereEffortChoices()

	if choices[0].value != "" || choices[0].label != "(not available)" {
		t.Errorf("choices[0] = %+v, want {value: \"\", label: \"(not available)\"}", choices[0])
	}
}

// TestConsigliereEffortChoicesDefaultLabelShowsModelDefaultOnceFetched
// is the other half: once a fetch for this provider has actually
// succeeded, the usual "(model default)" sentinel is shown again.
func TestConsigliereEffortChoicesDefaultLabelShowsModelDefaultOnceFetched(t *testing.T) {
	r := newPlainKeyRoot(t)
	r.consigliereModelCache["anthropic"] = []consigliere.ModelInfo{{ID: consigliere.AnthropicDefaultModel}}

	choices := r.consigliereEffortChoices()

	if want := "(model default)"; choices[0].label != want {
		t.Errorf("choices[0].label = %q, want %q", choices[0].label, want)
	}
}

func TestConsigliereEffortChoicesUsesPerModelLevelsForAnthropic(t *testing.T) {
	r := newPlainKeyRoot(t)
	r.settings.ConsigliereAnthropicModel = "claude-opus-5-5"
	r.consigliereModelCache["anthropic"] = []consigliere.ModelInfo{
		{ID: "claude-opus-5-5", EffortLevels: []string{"low", "medium", "high"}},
		{ID: "claude-haiku-5-5", EffortLevels: []string{"medium"}},
	}

	choices := r.consigliereEffortChoices()

	want := []string{"", "low", "medium", "high", consigliereTypedEntrySentinel}
	if len(choices) != len(want) {
		t.Fatalf("choices = %+v, want %v", choices, want)
	}
	for i, v := range want {
		if choices[i].value != v {
			t.Errorf("choices[%d] = %q, want %q", i, choices[i].value, v)
		}
	}
}

func TestConsigliereEffortChoicesUsesFixedSetForOpenAI(t *testing.T) {
	r := newPlainKeyRoot(t)
	r.settings.ConsigliereProvider = "openai"

	choices := r.consigliereEffortChoices()

	if len(choices) != len(consigliere.OpenAIEffortLevels)+2 { // +1 "(model default)" sentinel, +1 typed-entry sentinel
		t.Fatalf("choices = %+v, want the fixed OpenAIEffortLevels set regardless of any cache", choices)
	}
}

func TestConsigliereEffortChoicesIncludesStoredOverrideEvenIfNotInLevels(t *testing.T) {
	r := newPlainKeyRoot(t)
	r.settings.ConsigliereAnthropicEffort = "some-future-level"
	r.consigliereModelCache["anthropic"] = []consigliere.ModelInfo{
		{ID: consigliere.AnthropicDefaultModel, EffortLevels: []string{"low", "high"}},
	}

	choices := r.consigliereEffortChoices()

	var found bool
	for _, c := range choices {
		if c.value == "some-future-level" {
			found = true
		}
	}
	if !found {
		t.Errorf("choices = %+v, want the stored override preserved even though it's not among the fetched levels", choices)
	}
}

// TestConsigliereModelChoicesCycleNeverSilentlyDiscardsAnUnmatchedOverride
// is the regression the advisor flagged: cycleOptionChoice wraps to
// choices[0] when the current value isn't found among them — if a
// stored override ever disappeared from choices(), the very next
// Enter press on this row would silently replace it with the
// provider's own default instead of what the user actually configured.
func TestConsigliereModelChoicesCycleNeverSilentlyDiscardsAnUnmatchedOverride(t *testing.T) {
	r := newPlainKeyRoot(t)
	r.settings.ConsigliereAnthropicModel = "a-manually-typed-model-not-in-any-fetch"
	r.consigliereModelCache["anthropic"] = []consigliere.ModelInfo{{ID: "claude-opus-5-5"}}

	choices := r.consigliereModelChoices()

	for _, c := range choices {
		if c.value == r.settings.ConsigliereAnthropicModel {
			return // found — cycling from here will not silently reset to the default
		}
	}
	t.Fatalf("choices = %+v, want the manually-typed override present", choices)
}

// TestOptionDefaultHintSurfacesAModelFetchFailure pins a real, reported
// bug: a key good enough for Reply but rejected (or erroring) by the
// provider's own "list models" endpoint left Model/Effort showing
// just "(default: ...)"/"(model default)" with absolutely no
// indication anything had failed — indistinguishable from a provider
// that genuinely has nothing beyond its own default, since nothing
// else in this screen ever read consigliereModelFetchErr.
func TestOptionDefaultHintSurfacesAModelFetchFailure(t *testing.T) {
	r := newPlainKeyRoot(t)
	for i, cat := range optionCategories() {
		if cat.name == "AI Consigliere" {
			r.optionsCategory = i
		}
	}
	r.consigliereModelFetchInFlight["anthropic"] = true
	r.finishConsigliereModelFetch("anthropic", nil, errors.New("Anthropic: invalid x-api-key"))

	modelRow, ok := optionRowByKey(r, "consigliere_anthropic_model")
	if !ok {
		t.Fatal("no consigliere_anthropic_model row")
	}
	opt, _ := r.optionAtRow(modelRow)
	if got := r.optionDefaultHint(opt); !strings.Contains(got, "invalid x-api-key") {
		t.Errorf("optionDefaultHint(consigliere_anthropic_model) = %q, want it to surface the fetch error", got)
	}

	effortRow, ok := optionRowByKey(r, "consigliere_anthropic_effort")
	if !ok {
		t.Fatal("no consigliere_anthropic_effort row")
	}
	opt, _ = r.optionAtRow(effortRow)
	if got := r.optionDefaultHint(opt); !strings.Contains(got, "invalid x-api-key") {
		t.Errorf("optionDefaultHint(consigliere_anthropic_effort) = %q, want it to surface the fetch error", got)
	}
}

// TestOptionDefaultHintTruncatesALongModelFetchFailure pins a real,
// reported regression: an uncapped provider error (a real one can run
// well past a normal "default: ..." hint's own length) overflowed the
// Default column, pushing the Info column — and everything visually
// to its right — well past where it belongs. showOptionInfo's own "?"
// popup gets the full text instead (see TestShowOptionInfoIncludesTheFullModelFetchFailure).
func TestOptionDefaultHintTruncatesALongModelFetchFailure(t *testing.T) {
	r := newPlainKeyRoot(t)
	for i, cat := range optionCategories() {
		if cat.name == "AI Consigliere" {
			r.optionsCategory = i
		}
	}
	long := "consigliere: Anthropic: the parameter name is `lifecycle[]`, not `lifecycle` — a much longer message than any ordinary default hint"
	r.consigliereModelFetchInFlight["anthropic"] = true
	r.finishConsigliereModelFetch("anthropic", nil, errors.New(long))

	modelRow, ok := optionRowByKey(r, "consigliere_anthropic_model")
	if !ok {
		t.Fatal("no consigliere_anthropic_model row")
	}
	opt, _ := r.optionAtRow(modelRow)
	got := r.optionDefaultHint(opt)
	if len(got) >= len(long) {
		t.Errorf("optionDefaultHint(consigliere_anthropic_model) = %q (%d chars), want it capped well below the raw error's own %d chars", got, len(got), len(long))
	}
}

// TestShowOptionInfoIncludesTheFullModelFetchFailure pins the other
// half: the "?" info popup — the one place in this screen that
// already has room for a long explanation — gets the untruncated
// error, not just the capped hint optionDefaultHint shows inline.
func TestShowOptionInfoIncludesTheFullModelFetchFailure(t *testing.T) {
	r := newPlainKeyRoot(t)
	for i, cat := range optionCategories() {
		if cat.name == "AI Consigliere" {
			r.optionsCategory = i
		}
	}
	long := "consigliere: Anthropic: the parameter name is `lifecycle[]`, not `lifecycle` — a much longer message than any ordinary default hint"
	r.consigliereModelFetchInFlight["anthropic"] = true
	r.finishConsigliereModelFetch("anthropic", nil, errors.New(long))

	modelRow, ok := optionRowByKey(r, "consigliere_anthropic_model")
	if !ok {
		t.Fatal("no consigliere_anthropic_model row")
	}
	opt, _ := r.optionAtRow(modelRow)
	r.showOptionInfo(opt)

	if got := r.optionsInfo.GetText(true); !strings.Contains(got, long) {
		t.Errorf("optionsInfo text = %q, want it to include the full, untruncated error %q", got, long)
	}
}

// TestOptionDefaultHintSurfacesAnEmptyModelList is the other silent
// path the advisor flagged: a fetch that returns no error but also no
// models (a malformed success body, or a provider that genuinely
// listed nothing) is just as invisible as a real error unless called
// out specifically — finishConsigliereModelFetch treats a nil error
// as success regardless of how many models came back.
func TestOptionDefaultHintSurfacesAnEmptyModelList(t *testing.T) {
	r := newPlainKeyRoot(t)
	for i, cat := range optionCategories() {
		if cat.name == "AI Consigliere" {
			r.optionsCategory = i
		}
	}
	r.consigliereModelFetchInFlight["anthropic"] = true
	r.finishConsigliereModelFetch("anthropic", []consigliere.ModelInfo{}, nil)

	modelRow, ok := optionRowByKey(r, "consigliere_anthropic_model")
	if !ok {
		t.Fatal("no consigliere_anthropic_model row")
	}
	opt, _ := r.optionAtRow(modelRow)
	if got := r.optionDefaultHint(opt); !strings.Contains(got, "empty") {
		t.Errorf("optionDefaultHint(consigliere_anthropic_model) = %q, want it to mention the empty list", got)
	}
}
