// docsearchindex.go caches the AI Consigliere's own documentation
// search index (see internal/docsearch) at the package level instead
// of rebuilding it on every single NewRoot call. The corpus behind it
// — the embedded user guide plus helpText — never changes within a
// single process, so there is nothing to invalidate; this exists
// purely so the hundreds of test-driven NewRoot calls across this
// package's own test suite (and any real repeated construction)
// don't each pay NewIndex's real CPU cost again from scratch. A real,
// measured case: this exact redundant rebuild, multiplied across
// nearly every test in this package, tipped a slower CI runner's own
// per-package test timeout over the edge once this feature's corpus
// grew large enough to notice.
package ui

import (
	"sync"

	"github.com/jagottsicher/breakthrough/internal/docsearch"
)

var (
	defaultDocSearchIndexOnce sync.Once
	defaultDocSearchIndexVal  *docsearch.Index
	defaultDocSearchIndexErr  error
)

// defaultDocSearchIndex builds the shared Index on its first call,
// then returns the same already-built result on every later one —
// safe to share across every Root instance since Index.Search never
// mutates its own receiver.
func defaultDocSearchIndex() (*docsearch.Index, error) {
	defaultDocSearchIndexOnce.Do(func() {
		defaultDocSearchIndexVal, defaultDocSearchIndexErr =
			docsearch.NewDefaultIndex(docsearch.ParseTviewBoldHeadingChunks(helpText, "help")...)
	})
	return defaultDocSearchIndexVal, defaultDocSearchIndexErr
}
