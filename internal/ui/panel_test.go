package ui

import "testing"

func TestBuildHeaderSpans(t *testing.T) {
	text, spans := buildHeaderSpans("/a/bb/c")

	wantText := " /a/bb/c"
	if text != wantText {
		t.Fatalf("text = %q, want %q", text, wantText)
	}

	want := []headerSpan{
		{start: 1, end: 2, target: "/"},
		{start: 2, end: 3, target: "/a"},
		{start: 4, end: 6, target: "/a/bb"},
		{start: 7, end: 8, target: "/a/bb/c"},
	}

	if len(spans) != len(want) {
		t.Fatalf("got %d spans, want %d: %+v", len(spans), len(want), spans)
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Errorf("span %d = %+v, want %+v", i, spans[i], want[i])
		}
	}

	// Every span's slice of text must equal its own last path component
	// (or "/" for the root span) — this is what makes clicking a name
	// actually correspond to what's drawn under the cursor.
	runes := []rune(text)
	for _, s := range spans {
		got := string(runes[s.start:s.end])
		want := s.target
		if s.target != "/" {
			parts := []rune(s.target)
			// Last component after the final "/".
			last := ""
			for i := len(parts) - 1; i >= 0; i-- {
				if parts[i] == '/' {
					last = string(parts[i+1:])
					break
				}
			}
			want = last
		}
		if got != want {
			t.Errorf("span %+v covers text %q, want %q", s, got, want)
		}
	}
}

func TestBuildHeaderSpansRoot(t *testing.T) {
	text, spans := buildHeaderSpans("/")

	wantText := " /"
	if text != wantText {
		t.Fatalf("text = %q, want %q", text, wantText)
	}

	if len(spans) != 1 || spans[0] != (headerSpan{start: 1, end: 2, target: "/"}) {
		t.Fatalf("spans = %+v, want a single root span", spans)
	}
}
