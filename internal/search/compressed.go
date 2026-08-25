package search

import (
	"context"
	"sync"
)

// compressedTools pairs one grep-supported compression's own file
// glob with the wrapper tool that greps it directly — the "Stufe
// Content" set the user's own design settled on: exactly the formats
// grep itself has a dedicated wrapper for (zgrep/bzgrep/xzgrep/
// zipgrep, all verified present and flag-compatible with GrepArgs' own
// shape — see BzgrepArgs/XzgrepArgs/ZgrepArgs/ZipgrepArgs' own doc
// comments), not archive *containers* like tar.gz/tar.bz2/tar.xz —
// none of those four tools can meaningfully search inside one (see
// Request.IncludeCompressed's own doc comment for why that's a
// deliberate scope decision, not an oversight).
var compressedTools = []struct {
	glob string
	tool string
	args func(pattern string, mode Mode, file string, caseSensitive, wholeWords, firstHit bool) []string
}{
	{"*.gz", "zgrep", ZgrepArgs},
	{"*.bz2", "bzgrep", BzgrepArgs},
	{"*.xz", "xzgrep", XzgrepArgs},
	{"*.zip", "zipgrep", ZipgrepArgs},
}

// startCompressedContentSearch kicks off one listThenGrep call per
// compressedTools entry, all running concurrently with each other (and
// with the caller's own plain grep -r call — see runContentSearch) —
// each format's own candidate list is found and grepped independently,
// so a slow, large .zip full of members doesn't hold up .gz results
// arriving in the meantime, the same reasoning startDirectoryProgress's
// own doc comment gives for not serializing independent walks. A no-op
// when req.IncludeCompressed is false (the common case) or Content
// isn't ContentGrep.
//
// Returns a func that blocks until every one of the four searches is
// actually done — meant to be called via defer right where it's
// returned (see runContentSearch), so that func never returns, and
// Run's own results channel never closes, while one of the four is
// still sending on it. A no-op wait func when there's nothing to wait
// for, so the caller never needs its own separate branch for that case
// — the same shape startArchiveSearch already has for the identical
// reason.
func startCompressedContentSearch(ctx context.Context, req Request, results chan<- Result) (wait func()) {
	if !req.IncludeCompressed || req.Content != ContentGrep {
		return func() {}
	}

	var wg sync.WaitGroup
	for _, ct := range compressedTools {
		ct := ct
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = listThenGrep(ctx, req, ct.glob, ModeGlob, false, ct.tool, func(f string) []string {
				return ct.args(req.Pattern, req.Mode, f, req.CaseSensitive, req.WholeWords, req.FirstHit)
			}, results)
		}()
	}
	return wg.Wait
}
