package consigliere

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAICompatReplySendsExpectedRequestAndParsesReply(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody openAICompatRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"check dmesg for the device"}}]}`))
	}))
	defer server.Close()

	p := OpenAICompat{
		BaseURL: server.URL,
		Model:   OpenAIDefaultModel,
		APIKey:  func() (string, error) { return "sk-oa-test-key", nil },
	}

	got, err := p.Reply(context.Background(), Request{
		History:  []Message{{Role: RoleUser, Text: "mein USB-Geraet geht nicht mehr"}},
		Snapshot: "context: USB troubleshooting",
	})
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if got != "check dmesg for the device" {
		t.Fatalf("Reply = %q", got)
	}

	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer sk-oa-test-key" {
		t.Errorf("authorization = %q", gotAuth)
	}
	if gotBody.Model != OpenAIDefaultModel {
		t.Errorf("model = %q", gotBody.Model)
	}
	if len(gotBody.Messages) != 2 || gotBody.Messages[0].Role != "system" || gotBody.Messages[1].Role != "user" {
		t.Errorf("messages = %+v, want [system, user]", gotBody.Messages)
	}
}

func TestOpenAICompatReplyOmitsEmptySnapshot(t *testing.T) {
	var gotBody openAICompatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	p := OpenAICompat{BaseURL: server.URL, Model: "m", APIKey: func() (string, error) { return "k", nil }}
	if _, err := p.Reply(context.Background(), Request{History: []Message{{Role: RoleUser, Text: "hi"}}}); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if len(gotBody.Messages) != 1 || gotBody.Messages[0].Role != "user" {
		t.Errorf("messages = %+v, want just [user] with no empty system message", gotBody.Messages)
	}
}

func TestOpenAICompatReplySetsReasoningEffortWhenSet(t *testing.T) {
	var gotBody openAICompatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	p := OpenAICompat{BaseURL: server.URL, Model: "m", Effort: "low", APIKey: func() (string, error) { return "k", nil }}
	if _, err := p.Reply(context.Background(), Request{}); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if gotBody.ReasoningEffort != "low" {
		t.Errorf("reasoning_effort = %q, want %q", gotBody.ReasoningEffort, "low")
	}
}

func TestOpenAICompatReplyOmitsReasoningEffortWhenUnset(t *testing.T) {
	var gotRaw map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotRaw)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	p := OpenAICompat{BaseURL: server.URL, Model: "m", APIKey: func() (string, error) { return "k", nil }}
	if _, err := p.Reply(context.Background(), Request{}); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if _, ok := gotRaw["reasoning_effort"]; ok {
		t.Errorf("request body has reasoning_effort = %v, want the field omitted entirely", gotRaw["reasoning_effort"])
	}
}

func TestOpenAICompatReplySurfacesServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided"}}`))
	}))
	defer server.Close()

	p := OpenAICompat{BaseURL: server.URL, Model: "m", APIKey: func() (string, error) { return "wrong-key", nil }}
	_, err := p.Reply(context.Background(), Request{})
	if err == nil {
		t.Fatal("Reply: want an error for a 401 response")
	}
	if !strings.Contains(err.Error(), "Incorrect API key provided") {
		t.Errorf("Reply error = %v, want it to surface the server's own message", err)
	}
}

func TestOpenAICompatReplyWithoutAPIKeyFails(t *testing.T) {
	p := OpenAICompat{BaseURL: "http://unused.invalid", Model: "m"}
	_, err := p.Reply(context.Background(), Request{})
	if !errors.Is(err, errNoAPIKey) {
		t.Fatalf("Reply without APIKey = %v, want errNoAPIKey", err)
	}
}
