package viewer

import (
	"strings"
	"testing"
)

// joinTokens concatenates every token's own text — the round-trip
// Highlight promises (see its own doc comment): classifying never
// rewrites content.
func joinTokens(tokens []Token) string {
	var b strings.Builder
	for _, t := range tokens {
		b.WriteString(t.Text)
	}
	return b.String()
}

// kindsPresent reports which kinds appear at all, for tests that care
// that something was recognized without pinning exactly which run.
func kindsPresent(tokens []Token) map[TokenKind]bool {
	present := map[TokenKind]bool{}
	for _, t := range tokens {
		present[t.Kind] = true
	}
	return present
}

func TestHighlightRoundTripsContentExactly(t *testing.T) {
	cases := map[string]string{
		"main.go":   "package main\n\n// a comment\nfunc main() { println(\"hi\", 42) }\n",
		"conf.yaml": "key: value\n# comment\nlist:\n  - one\n  - 2\n",
		"notes.txt": "just some plain prose, nothing to lex\n",
		"empty.txt": "",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			got := joinTokens(Highlight(name, content))
			if got != content {
				t.Errorf("round trip changed content:\n got %q\nwant %q", got, content)
			}
		})
	}
}

func TestHighlightGoRecognizesKeywordsStringsComments(t *testing.T) {
	content := "package main\n\n// a comment\nfunc main() { println(\"hi\", 42) }\n"
	present := kindsPresent(Highlight("main.go", content))

	for kind, label := range map[TokenKind]string{
		KindKeyword: "keyword",
		KindString:  "string",
		KindComment: "comment",
		KindNumber:  "number",
	} {
		if !present[kind] {
			t.Errorf("no %s token found in Go source", label)
		}
	}
}

func TestHighlightYAMLRecognizesKeysAndComments(t *testing.T) {
	content := "# a comment\nkey: value\ncount: 42\n"
	present := kindsPresent(Highlight("conf.yaml", content))

	if !present[KindComment] {
		t.Error("no comment token found in YAML")
	}
	if !present[KindName] && !present[KindKeyword] {
		t.Error("YAML keys were not classified as anything structural")
	}
}

// TestHighlightDiffMarksAddedAndRemoved pins the one place a chroma
// "Generic" category earns its own color — see classify's own doc
// comment.
func TestHighlightDiffMarksAddedAndRemoved(t *testing.T) {
	content := "--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old line\n+new line\n"
	present := kindsPresent(Highlight("change.diff", content))

	if !present[KindDiffAdd] {
		t.Error("no added-line token found in a diff")
	}
	if !present[KindDiffDelete] {
		t.Error("no removed-line token found in a diff")
	}
}

// TestHighlightUnknownContentIsOnePlainToken pins the fallback every
// caller relies on to need no separate branch (see Highlight's own doc
// comment).
func TestHighlightUnknownContentIsOnePlainToken(t *testing.T) {
	content := "zzz qqq not any known language at all\n"
	tokens := Highlight("mystery.zzzz", content)

	if len(tokens) != 1 || tokens[0].Kind != KindPlain || tokens[0].Text != content {
		t.Errorf("got %+v, want exactly one plain token spanning the whole input", tokens)
	}
}

// TestHighlightOverLimitIsNotLexed pins HighlightLimit: a file past it
// comes back whole, as a single plain token, rather than costing real
// lexing time.
func TestHighlightOverLimitIsNotLexed(t *testing.T) {
	content := strings.Repeat("package main\n", (HighlightLimit/13)+100)
	if len(content) <= HighlightLimit {
		t.Fatalf("setup: content is only %d bytes, need more than %d", len(content), HighlightLimit)
	}

	tokens := Highlight("main.go", content)
	if len(tokens) != 1 || tokens[0].Kind != KindPlain {
		t.Errorf("got %d tokens (first kind %v), want exactly one plain token for content over the limit", len(tokens), tokens[0].Kind)
	}
	if tokens[0].Text != content {
		t.Error("over-limit content was not returned verbatim")
	}
}

// TestHighlightDetectsByContentWithoutExtension pins the Analyse
// fallback: a file with no usable name still gets lexed if its content
// is recognizable.
func TestHighlightDetectsByContentWithoutExtension(t *testing.T) {
	content := "#!/bin/sh\n# a comment\necho \"hello\"\n"
	present := kindsPresent(Highlight("runme", content))

	if !present[KindComment] && !present[KindString] && !present[KindKeyword] {
		t.Error("an extensionless shell script was not classified at all — content analysis fallback did not run")
	}
}

// TestHighlightMergesAdjacentSameKindRuns pins the merging Highlight
// does to keep the rendered tag count down (see its own inline comment)
// — no two neighbouring tokens may share a kind.
func TestHighlightMergesAdjacentSameKindRuns(t *testing.T) {
	tokens := Highlight("main.go", "package main\n\nfunc main() { println(1, 2, 3) }\n")
	for i := 1; i < len(tokens); i++ {
		if tokens[i].Kind == tokens[i-1].Kind {
			t.Fatalf("tokens %d and %d share kind %v — adjacent same-kind runs should have been merged", i-1, i, tokens[i].Kind)
		}
	}
}
