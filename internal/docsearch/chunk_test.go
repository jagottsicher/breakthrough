package docsearch

import "testing"

func TestParseMarkdownChunksSplitsOnEveryHeadingLevel(t *testing.T) {
	md := `# Title

Title's own intro text.

## Section one

Body one.

### Subsection

Body sub.

## Section two

Body two.
`
	chunks := ParseMarkdownChunks(md, "test.md")

	want := []struct {
		heading string
		body    string
	}{
		{"Title", "Title's own intro text."},
		{"Section one", "Body one."},
		{"Subsection", "Body sub."},
		{"Section two", "Body two."},
	}
	if len(chunks) != len(want) {
		t.Fatalf("got %d chunks, want %d: %+v", len(chunks), len(want), chunks)
	}
	for i, w := range want {
		if chunks[i].Heading != w.heading {
			t.Errorf("chunk %d heading = %q, want %q", i, chunks[i].Heading, w.heading)
		}
		if chunks[i].Body != w.body {
			t.Errorf("chunk %d body = %q, want %q", i, chunks[i].Body, w.body)
		}
		if chunks[i].Source != "test.md" {
			t.Errorf("chunk %d source = %q, want %q", i, chunks[i].Source, "test.md")
		}
	}
}

// TestParseMarkdownChunksKeepsTextBeforeTheFirstHeadingAsItsOwnChunk
// pins the other half of ParseMarkdownChunks' own doc comment: text
// that precedes even the first heading (no document in this corpus
// actually opens this way, but nothing guarantees that stays true)
// becomes its own chunk with an empty Heading, rather than being
// silently dropped.
func TestParseMarkdownChunksKeepsTextBeforeTheFirstHeadingAsItsOwnChunk(t *testing.T) {
	md := "Unheaded intro.\n\n# Title\n\nBody.\n"
	chunks := ParseMarkdownChunks(md, "test.md")

	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2: %+v", len(chunks), chunks)
	}
	if chunks[0].Heading != "" || chunks[0].Body != "Unheaded intro." {
		t.Errorf("chunk 0 = %+v, want the unheaded intro", chunks[0])
	}
	if chunks[1].Heading != "Title" || chunks[1].Body != "Body." {
		t.Errorf("chunk 1 = %+v, want the Title section", chunks[1])
	}
}

func TestParseMarkdownChunksIgnoresHeadingLikeTextInsideAFence(t *testing.T) {
	// Deliberately NOT special-cased (see isMarkdownHeading's own doc
	// comment): this corpus's own docs never put a "#" inside a code
	// fence at the start of a line, so this test pins the simpler,
	// current behavior rather than a requirement — a `# comment` line
	// inside a fenced block would be (mis)parsed as a heading. If that
	// ever becomes a real problem for this specific corpus, fix it
	// then, not speculatively now.
	md := "## Real heading\n\n```\n# not a heading, but parsed as one\n```\n"
	chunks := ParseMarkdownChunks(md, "test.md")
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2 (known limitation): %+v", len(chunks), chunks)
	}
}

func TestIsMarkdownHeadingRejectsNonHeadingHashUsage(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"# Title", true},
		{"## Section", true},
		{"###### Deep", true},
		{"####### TooDeep", false},
		{"#NoSpace", false},
		{"not a heading", false},
		{"#", true}, // bare "#" with nothing after it
	}
	for _, c := range cases {
		if got := isMarkdownHeading(c.line); got != c.want {
			t.Errorf("isMarkdownHeading(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}
