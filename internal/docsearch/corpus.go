package docsearch

import (
	"fmt"

	// docsembed lives at the module root, not under internal/ or
	// cmd/ — see its own doc comment for why its import path has no
	// further path segment after the module path itself.
	docsembed "github.com/jagottsicher/breakthrough"
)

// userGuideFilename is docsembed.FS's own single embedded file.
const userGuideFilename = "docs/user-guide.md"

// NewDefaultIndex builds an Index over breakthrough's own embedded
// user guide (see docsembed's own doc comment on why that embed has
// to live at the module root), plus whatever extra chunks the caller
// supplies — internal/ui's own NewRoot passes in internal/ui/help.go's
// helpText this way (parsed via ParseTviewBoldHeadingChunks), since
// docsearch can't import internal/ui itself without an import cycle
// (internal/ui already imports docsearch). The only error path is
// docsembed.FS.ReadFile failing, which would mean the embed itself is
// broken — a build-time problem, not something a normal run should
// ever actually hit.
func NewDefaultIndex(extra ...Chunk) (*Index, error) {
	data, err := docsembed.FS.ReadFile(userGuideFilename)
	if err != nil {
		return nil, fmt.Errorf("docsearch: reading embedded %s: %w", userGuideFilename, err)
	}
	chunks := ParseMarkdownChunks(string(data), "user-guide.md")
	chunks = dropChunk(chunks, "Contents")
	chunks = append(chunks, extra...)
	return NewIndex(chunks), nil
}

// dropChunk removes every chunk whose Heading exactly matches
// heading — used to exclude the user guide's own "## Contents" table
// of contents, which echoes every other section's own heading text
// as link labels and would otherwise match almost any query purely by
// containing a fragment of everything else in the document, not
// because it actually answers anything itself.
func dropChunk(chunks []Chunk, heading string) []Chunk {
	out := make([]Chunk, 0, len(chunks))
	for _, c := range chunks {
		if c.Heading != heading {
			out = append(out, c)
		}
	}
	return out
}
