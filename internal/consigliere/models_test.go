package consigliere

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestListAnthropicModelsParsesEffortLevelsAndFiltersLifecycle(t *testing.T) {
	var gotQuery url.Values
	var gotAPIKey, gotVersion string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		_, _ = w.Write([]byte(`{
			"data": [
				{
					"id": "claude-opus-5-5",
					"lifecycle": "active",
					"capabilities": {"effort": {
						"low": {"supported": true},
						"medium": {"supported": true},
						"high": {"supported": true},
						"xhigh": {"supported": true},
						"max": {"supported": true}
					}}
				},
				{
					"id": "claude-opus-4-5-legacy",
					"lifecycle": "retired",
					"capabilities": {"effort": {"high": {"supported": true}}}
				}
			]
		}`))
	}))
	defer server.Close()

	models, err := ListAnthropicModels(context.Background(), "sk-ant-test-key", server.URL, nil)
	if err != nil {
		t.Fatalf("ListAnthropicModels: %v", err)
	}

	if gotAPIKey != "sk-ant-test-key" {
		t.Errorf("x-api-key = %q", gotAPIKey)
	}
	if gotVersion != anthropicAPIVersion {
		t.Errorf("anthropic-version = %q, want %q", gotVersion, anthropicAPIVersion)
	}
	if gotQuery.Get("lifecycle") != "active" {
		t.Errorf("lifecycle query param = %q, want %q", gotQuery.Get("lifecycle"), "active")
	}
	if gotQuery.Get("limit") != "1000" {
		t.Errorf("limit query param = %q, want %q", gotQuery.Get("limit"), "1000")
	}

	if len(models) != 1 {
		t.Fatalf("models = %+v, want exactly the one active model (retired one filtered client-side too)", models)
	}
	if models[0].ID != "claude-opus-5-5" {
		t.Errorf("models[0].ID = %q", models[0].ID)
	}
	want := []string{"low", "medium", "high", "xhigh", "max"}
	if len(models[0].EffortLevels) != len(want) {
		t.Fatalf("EffortLevels = %v, want %v", models[0].EffortLevels, want)
	}
	for i, lvl := range want {
		if models[0].EffortLevels[i] != lvl {
			t.Errorf("EffortLevels[%d] = %q, want %q (order matters)", i, models[0].EffortLevels[i], lvl)
		}
	}
}

func TestListAnthropicModelsSurfacesServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	defer server.Close()

	_, err := ListAnthropicModels(context.Background(), "wrong-key", server.URL, nil)
	if err == nil {
		t.Fatal("want an error for a 401 response")
	}
}

func TestListOpenAICompatModelsParsesIDsAndOptionalEffort(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("authorization")
		_, _ = w.Write([]byte(`{
			"data": [
				{"id": "gpt-6-astra"},
				{"id": "deepseek-v4-pro", "effort": {"supported_levels": ["none", "low", "high", "max"]}}
			]
		}`))
	}))
	defer server.Close()

	models, err := ListOpenAICompatModels(context.Background(), "sk-test-key", server.URL, nil)
	if err != nil {
		t.Fatalf("ListOpenAICompatModels: %v", err)
	}

	if gotAuth != "Bearer sk-test-key" {
		t.Errorf("authorization = %q", gotAuth)
	}
	if len(models) != 2 {
		t.Fatalf("models = %+v", models)
	}
	if models[0].ID != "gpt-6-astra" || models[0].EffortLevels != nil {
		t.Errorf("models[0] = %+v, want no effort info (OpenAI never reports it)", models[0])
	}
	if models[1].ID != "deepseek-v4-pro" {
		t.Errorf("models[1].ID = %q", models[1].ID)
	}
	want := []string{"none", "low", "high", "max"}
	if len(models[1].EffortLevels) != len(want) {
		t.Fatalf("models[1].EffortLevels = %v, want %v", models[1].EffortLevels, want)
	}
	for i, lvl := range want {
		if models[1].EffortLevels[i] != lvl {
			t.Errorf("EffortLevels[%d] = %q, want %q", i, models[1].EffortLevels[i], lvl)
		}
	}
}

func TestListOpenAICompatModelsSurfacesServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided"}}`))
	}))
	defer server.Close()

	_, err := ListOpenAICompatModels(context.Background(), "wrong-key", server.URL, nil)
	if err == nil {
		t.Fatal("want an error for a 401 response")
	}
}
