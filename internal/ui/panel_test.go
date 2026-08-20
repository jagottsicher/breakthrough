package ui

import "testing"

func TestBuildHeaderSpans(t *testing.T) {
	text, spans := buildHeaderSpans("/a/bb/c")

	wantText := " ~ < >  /a/bb/c"
	if text != wantText {
		t.Fatalf("text = %q, want %q", text, wantText)
	}

	want := []headerSpan{
		{start: 1, end: 2, action: actionHome},
		{start: 3, end: 4, action: actionBack},
		{start: 5, end: 6, action: actionForward},
		{start: 8, end: 9, action: actionNavigate, target: "/"},
		{start: 9, end: 10, action: actionNavigate, target: "/a"},
		{start: 11, end: 13, action: actionNavigate, target: "/a/bb"},
		{start: 14, end: 15, action: actionNavigate, target: "/a/bb/c"},
	}

	if len(spans) != len(want) {
		t.Fatalf("got %d spans, want %d: %+v", len(spans), len(want), spans)
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Errorf("span %d = %+v, want %+v", i, spans[i], want[i])
		}
	}

	// Every path span's slice of text must equal its own last path
	// component (or "/" for the root span) — this is what makes clicking
	// a name actually correspond to what's drawn under the cursor.
	runes := []rune(text)
	for _, s := range spans {
		if s.action != actionNavigate {
			continue
		}
		got := string(runes[s.start:s.end])
		want := s.target
		if s.target != "/" {
			parts := []rune(s.target)
			for i := len(parts) - 1; i >= 0; i-- {
				if parts[i] == '/' {
					want = string(parts[i+1:])
					break
				}
			}
		}
		if got != want {
			t.Errorf("span %+v covers text %q, want %q", s, got, want)
		}
	}
}

func TestBuildHeaderSpansRoot(t *testing.T) {
	text, spans := buildHeaderSpans("/")

	wantText := " ~ < >  /"
	if text != wantText {
		t.Fatalf("text = %q, want %q", text, wantText)
	}

	// 3 buttons + the root span.
	if len(spans) != 4 {
		t.Fatalf("got %d spans, want 4: %+v", len(spans), spans)
	}
	root := spans[len(spans)-1]
	if root != (headerSpan{start: 8, end: 9, action: actionNavigate, target: "/"}) {
		t.Errorf("root span = %+v, want the trailing '/' span", root)
	}
}
