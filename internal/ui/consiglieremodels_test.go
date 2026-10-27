package ui

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jagottsicher/breakthrough/internal/consigliere"
)

// withFakeConsigliereModelList installs fn as listConsigliereModelsFunc
// for the duration of the test, restoring TestMain's own "no fake
// installed" default afterwards — the same t.Cleanup-scoped-override
// shape every other swappable var in this package's tests already
// uses.
func withFakeConsigliereModelList(t *testing.T, fn func(ctx context.Context, providerName, key string) ([]consigliere.ModelInfo, error)) {
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
	withFakeConsigliereModelList(t, func(ctx context.Context, providerName, key string) ([]consigliere.ModelInfo, error) {
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
	withFakeConsigliereModelList(t, func(ctx context.Context, providerName, key string) ([]consigliere.ModelInfo, error) {
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
	withFakeConsigliereModelList(t, func(ctx context.Context, providerName, key string) ([]consigliere.ModelInfo, error) {
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
	withFakeConsigliereModelList(t, func(ctx context.Context, providerName, key string) ([]consigliere.ModelInfo, error) {
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
	r.settings.ConsigliereModel = "claude-haiku-5-5"

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

	if len(choices) != 3 { // default sentinel + two fetched models
		t.Fatalf("choices = %+v, want 3 entries", choices)
	}
	if choices[1].value != "claude-opus-5-5" || choices[2].value != "claude-haiku-5-5" {
		t.Errorf("choices = %+v", choices)
	}
}

func TestConsigliereEffortChoicesUsesPerModelLevelsForAnthropic(t *testing.T) {
	r := newPlainKeyRoot(t)
	r.settings.ConsigliereModel = "claude-opus-5-5"
	r.consigliereModelCache["anthropic"] = []consigliere.ModelInfo{
		{ID: "claude-opus-5-5", EffortLevels: []string{"low", "medium", "high"}},
		{ID: "claude-haiku-5-5", EffortLevels: []string{"medium"}},
	}

	choices := r.consigliereEffortChoices()

	want := []string{"", "low", "medium", "high"}
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

	if len(choices) != len(consigliere.OpenAIEffortLevels)+1 { // +1 for the "(model default)" sentinel
		t.Fatalf("choices = %+v, want the fixed OpenAIEffortLevels set regardless of any cache", choices)
	}
}

func TestConsigliereEffortChoicesIncludesStoredOverrideEvenIfNotInLevels(t *testing.T) {
	r := newPlainKeyRoot(t)
	r.settings.ConsigliereEffort = "some-future-level"
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
	r.settings.ConsigliereModel = "a-manually-typed-model-not-in-any-fetch"
	r.consigliereModelCache["anthropic"] = []consigliere.ModelInfo{{ID: "claude-opus-5-5"}}

	choices := r.consigliereModelChoices()

	for _, c := range choices {
		if c.value == r.settings.ConsigliereModel {
			return // found — cycling from here will not silently reset to the default
		}
	}
	t.Fatalf("choices = %+v, want the manually-typed override present", choices)
}
