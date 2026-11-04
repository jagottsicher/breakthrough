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

	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
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

func TestOpenAICompatReplyCombinesSystemAndSnapshot(t *testing.T) {
	cases := []struct {
		name     string
		system   string
		snapshot string
		want     []string // expected system-message contents, "" if none expected at all
	}{
		{"both set", "Be terse.", "dir: /tmp", []string{"Be terse.\n\ndir: /tmp"}},
		{"system only", "Be terse.", "", []string{"Be terse."}},
		{"snapshot only", "", "dir: /tmp", []string{"dir: /tmp"}},
		{"neither set", "", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotBody openAICompatRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
			}))
			defer server.Close()

			p := OpenAICompat{BaseURL: server.URL, Model: "m", APIKey: func() (string, error) { return "k", nil }}
			if _, err := p.Reply(context.Background(), Request{System: c.system, Snapshot: c.snapshot}); err != nil {
				t.Fatalf("Reply: %v", err)
			}

			var gotSystem []string
			for _, m := range gotBody.Messages {
				if m.Role == "system" {
					gotSystem = append(gotSystem, m.Content)
				}
			}
			if len(gotSystem) != len(c.want) {
				t.Fatalf("system messages = %v, want %v", gotSystem, c.want)
			}
			for i := range c.want {
				if gotSystem[i] != c.want[i] {
					t.Errorf("system message %d = %q, want %q", i, gotSystem[i], c.want[i])
				}
			}
		})
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

// TestOpenAICompatReplyExplainsContentFilterRefusal pins the
// precautionary counterpart to Anthropic's own refusal handling:
// finish_reason "content_filter" with empty content is this shape's
// own equivalent of a safety-classifier decline — not yet actually
// reported against this specific provider shape, built ahead of a
// report rather than after one.
func TestOpenAICompatReplyExplainsContentFilterRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":""},"finish_reason":"content_filter"}]}`))
	}))
	defer server.Close()

	p := OpenAICompat{BaseURL: server.URL, Model: "m", APIKey: func() (string, error) { return "k", nil }}
	_, err := p.Reply(context.Background(), Request{})
	if err == nil {
		t.Fatal("Reply: want an error for a content_filter decline")
	}
	if !strings.Contains(err.Error(), "content_filter") {
		t.Errorf("Reply error = %v, want it to mention content_filter", err)
	}
}

// TestOpenAICompatReplySurfacesFinishReasonWhenContentEmpty pins the
// other empty-content case: no finish_reason-specific message exists
// for it, but the reason itself must still be visible rather than a
// bare "no content" the same way every cause used to get.
func TestOpenAICompatReplySurfacesFinishReasonWhenContentEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":""},"finish_reason":"tool_calls"}]}`))
	}))
	defer server.Close()

	p := OpenAICompat{BaseURL: server.URL, Model: "m", APIKey: func() (string, error) { return "k", nil }}
	_, err := p.Reply(context.Background(), Request{})
	if err == nil {
		t.Fatal("Reply: want an error for empty content")
	}
	if !strings.Contains(err.Error(), "tool_calls") {
		t.Errorf("Reply error = %v, want it to mention the finish_reason", err)
	}
}

// TestOpenAICompatReplyMarksTruncatedTextInsteadOfReturningItAsComplete
// mirrors Anthropic.Reply's identical fix: finish_reason "length"
// alongside real content means the reply was cut off, not complete —
// this package never sets its own output-length cap (see the doc
// comment above OpenAICompat), but the provider's own default limit
// can still truncate, and a silently-incomplete reply is the same
// risk either way.
func TestOpenAICompatReplyMarksTruncatedTextInsteadOfReturningItAsComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"check d"},"finish_reason":"length"}]}`))
	}))
	defer server.Close()

	p := OpenAICompat{BaseURL: server.URL, Model: "m", APIKey: func() (string, error) { return "k", nil }}
	got, err := p.Reply(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if !strings.HasPrefix(got, "check d") {
		t.Errorf("Reply = %q, want the fragment itself kept, not discarded", got)
	}
	if !strings.Contains(got, "cut off") {
		t.Errorf("Reply = %q, want an explicit truncation marker", got)
	}
}

func TestOpenAICompatReplyWithoutAPIKeyFails(t *testing.T) {
	p := OpenAICompat{BaseURL: "http://unused.invalid", Model: "m"}
	_, err := p.Reply(context.Background(), Request{})
	if !errors.Is(err, errNoAPIKey) {
		t.Fatalf("Reply without APIKey = %v, want errNoAPIKey", err)
	}
}
