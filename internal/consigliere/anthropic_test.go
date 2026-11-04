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

func TestAnthropicReplySendsExpectedRequestAndParsesReply(t *testing.T) {
	var gotPath, gotAPIKey, gotVersion string
	var gotBody anthropicRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"use lsusb to check if the device even shows up"}]}`))
	}))
	defer server.Close()

	p := Anthropic{
		BaseURL: server.URL,
		APIKey:  func() (string, error) { return "sk-ant-test-key", nil },
	}

	got, err := p.Reply(context.Background(), Request{
		History:  []Message{{Role: RoleUser, Text: "mein USB-Geraet geht nicht mehr"}},
		Snapshot: "context: USB troubleshooting",
	})
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if got != "use lsusb to check if the device even shows up" {
		t.Fatalf("Reply = %q", got)
	}

	if gotPath != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", gotPath)
	}
	if gotAPIKey != "sk-ant-test-key" {
		t.Errorf("x-api-key = %q", gotAPIKey)
	}
	if gotVersion != anthropicAPIVersion {
		t.Errorf("anthropic-version = %q, want %q", gotVersion, anthropicAPIVersion)
	}
	if gotBody.Model != AnthropicDefaultModel {
		t.Errorf("model = %q, want default %q", gotBody.Model, AnthropicDefaultModel)
	}
	if gotBody.System != "context: USB troubleshooting" {
		t.Errorf("system = %q", gotBody.System)
	}
	if len(gotBody.Messages) != 1 || gotBody.Messages[0].Role != "user" {
		t.Errorf("messages = %+v", gotBody.Messages)
	}
}

func TestAnthropicReplyUsesOverrideModel(t *testing.T) {
	var gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body anthropicRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel = body.Model
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	p := Anthropic{
		BaseURL: server.URL,
		Model:   "claude-haiku-5-5",
		APIKey:  func() (string, error) { return "k", nil },
	}
	if _, err := p.Reply(context.Background(), Request{}); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if gotModel != "claude-haiku-5-5" {
		t.Errorf("model = %q, want override", gotModel)
	}
}

func TestAnthropicReplySetsOutputConfigEffortWhenSet(t *testing.T) {
	var gotBody anthropicRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	p := Anthropic{BaseURL: server.URL, Effort: "low", APIKey: func() (string, error) { return "k", nil }}
	if _, err := p.Reply(context.Background(), Request{}); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if gotBody.OutputConfig == nil || gotBody.OutputConfig.Effort != "low" {
		t.Errorf("OutputConfig = %+v, want Effort \"low\"", gotBody.OutputConfig)
	}
}

// TestAnthropicReplySendsDefaultEffortWhenUnset pins a deliberately
// changed contract: omitting output_config used to mean "the model's
// own documented default" (unconditional "high", which also means
// unbounded thinking) — a real, reported bug let that consume
// anthropicMaxTokens before any reply text, or cut the reply itself
// off mid-sentence. "" now sends anthropicDefaultEffort explicitly;
// see that constant's own doc comment.
func TestAnthropicReplySendsDefaultEffortWhenUnset(t *testing.T) {
	var gotBody anthropicRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	p := Anthropic{BaseURL: server.URL, APIKey: func() (string, error) { return "k", nil }}
	if _, err := p.Reply(context.Background(), Request{}); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if gotBody.OutputConfig == nil || gotBody.OutputConfig.Effort != anthropicDefaultEffort {
		t.Errorf("OutputConfig = %+v, want Effort %q", gotBody.OutputConfig, anthropicDefaultEffort)
	}
}

func TestAnthropicReplySurfacesServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	defer server.Close()

	p := Anthropic{BaseURL: server.URL, APIKey: func() (string, error) { return "wrong-key", nil }}
	_, err := p.Reply(context.Background(), Request{})
	if err == nil {
		t.Fatal("Reply: want an error for a 401 response")
	}
	if !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Errorf("Reply error = %v, want it to surface the server's own message", err)
	}
}

// TestAnthropicReplySurfacesStopReasonWhenNoTextContent pins a real,
// reported bug: Claude Opus 5 thinks by default even with Effort left
// unset, and those thinking tokens come out of the same MaxTokens
// budget ahead of any TextBlock — on a reply that needed more
// deliberation than anthropicMaxTokens left room for, the loop found
// no text block at all and the resulting error gave the user nothing
// to act on beyond "no text content". stop_reason (almost always
// "max_tokens" in that case) must be in the message.
func TestAnthropicReplySurfacesStopReasonWhenNoTextContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"content":[{"type":"thinking","text":""}],"stop_reason":"max_tokens"}`))
	}))
	defer server.Close()

	p := Anthropic{BaseURL: server.URL, APIKey: func() (string, error) { return "k", nil }}
	_, err := p.Reply(context.Background(), Request{})
	if err == nil {
		t.Fatal("Reply: want an error when no block is text")
	}
	if !strings.Contains(err.Error(), "max_tokens") {
		t.Errorf("Reply error = %v, want it to mention stop_reason max_tokens", err)
	}
}

// TestAnthropicReplyMarksTruncatedTextInsteadOfReturningItAsComplete
// pins a real, reported bug: raising anthropicMaxTokens didn't
// eliminate the thinking-budget problem, it just moved the failure
// mode — a TextBlock that exists but was itself cut off mid-sentence
// (observed as a two-word reply, "Ich s") used to come back from
// Reply looking like any other complete answer, silently wrong rather
// than loudly failed. stop_reason "max_tokens" alongside real text
// must still be visible to the caller.
func TestAnthropicReplyMarksTruncatedTextInsteadOfReturningItAsComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"Ich s"}],"stop_reason":"max_tokens"}`))
	}))
	defer server.Close()

	p := Anthropic{BaseURL: server.URL, APIKey: func() (string, error) { return "k", nil }}
	got, err := p.Reply(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if !strings.HasPrefix(got, "Ich s") {
		t.Errorf("Reply = %q, want the fragment itself kept, not discarded", got)
	}
	if !strings.Contains(got, "cut off") {
		t.Errorf("Reply = %q, want an explicit truncation marker", got)
	}
}

// TestAnthropicReplyExplainsRefusal pins the other new stop_reason
// case a real session hit under Machiavelli Mode on an ordinary
// syslog question: a safety classifier declining outright (HTTP 200,
// stop_reason "refusal") used to come back as the same generic "no
// text content" message as a budget problem, giving no hint that the
// forwarded content itself was the reason, not a bug.
func TestAnthropicReplyExplainsRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"content":[],"stop_reason":"refusal","stop_details":{"category":"cyber","explanation":"repeated authentication failures"}}`))
	}))
	defer server.Close()

	p := Anthropic{BaseURL: server.URL, APIKey: func() (string, error) { return "k", nil }}
	_, err := p.Reply(context.Background(), Request{})
	if err == nil {
		t.Fatal("Reply: want an error for a refusal")
	}
	if !strings.Contains(err.Error(), "cyber") || !strings.Contains(err.Error(), "repeated authentication failures") {
		t.Errorf("Reply error = %v, want it to mention the stop_details category and explanation", err)
	}
}

func TestAnthropicReplyWithoutAPIKeyFails(t *testing.T) {
	p := Anthropic{}
	_, err := p.Reply(context.Background(), Request{})
	if !errors.Is(err, errNoAPIKey) {
		t.Fatalf("Reply without APIKey = %v, want errNoAPIKey", err)
	}
}

func TestAnthropicReplyPropagatesKeyFuncError(t *testing.T) {
	wantErr := errors.New("locked")
	p := Anthropic{APIKey: func() (string, error) { return "", wantErr }}
	_, err := p.Reply(context.Background(), Request{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Reply = %v, want %v", err, wantErr)
	}
}

func TestAnthropicReplyCombinesSystemAndSnapshot(t *testing.T) {
	cases := []struct {
		name     string
		system   string
		snapshot string
		want     string
	}{
		{"both set", "Be terse.", "dir: /tmp", "Be terse.\n\ndir: /tmp"},
		{"system only", "Be terse.", "", "Be terse."},
		{"snapshot only", "", "dir: /tmp", "dir: /tmp"},
		{"neither set", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotBody anthropicRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
			}))
			defer server.Close()

			p := Anthropic{BaseURL: server.URL, APIKey: func() (string, error) { return "k", nil }}
			if _, err := p.Reply(context.Background(), Request{System: c.system, Snapshot: c.snapshot}); err != nil {
				t.Fatalf("Reply: %v", err)
			}
			if gotBody.System != c.want {
				t.Errorf("system = %q, want %q", gotBody.System, c.want)
			}
		})
	}
}
