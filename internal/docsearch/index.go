package docsearch

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// bm25K1/bm25B are the standard Okapi BM25 tuning constants —
// Robertson/Zaragoza's own commonly-cited defaults (k1 in [1.2, 2.0],
// b = 0.75), not tuned against this specific corpus: the point here
// is "noticeably better than a plain substring match," not a
// precisely optimized ranking function.
const (
	bm25K1 = 1.5
	bm25B  = 0.75
)

// Index is a BM25-ranked search over a fixed set of Chunks, built
// once (see NewIndex/NewDefaultIndex) and queried many times —
// building it tokenizes and counts term frequencies across every
// chunk, work worth doing once rather than per question.
type Index struct {
	chunks []Chunk
	// terms[i] is chunks[i]'s own tokenized Heading+Body, as a
	// term->count map — kept rather than re-tokenizing Body on every
	// Search call.
	terms []map[string]int
	// docFreq[term] is how many chunks contain term at least once —
	// BM25's own IDF input.
	docFreq map[string]int
	avgLen  float64
}

// NewIndex builds an Index over chunks.
func NewIndex(chunks []Chunk) *Index {
	idx := &Index{
		chunks:  chunks,
		terms:   make([]map[string]int, len(chunks)),
		docFreq: map[string]int{},
	}

	var totalLen int
	for i, c := range chunks {
		counts := map[string]int{}
		tokens := tokenize(c.Heading + " " + c.Body)
		for _, t := range tokens {
			counts[t]++
		}
		idx.terms[i] = counts
		totalLen += len(tokens)
		for t := range counts {
			idx.docFreq[t]++
		}
	}
	if len(chunks) > 0 {
		idx.avgLen = float64(totalLen) / float64(len(chunks))
	}
	return idx
}

// Search returns the topN chunks best matching query, ranked by BM25
// score, highest first — chunks with a score of exactly 0 (no query
// term appears in them at all) are excluded outright rather than
// padding the result out to topN with irrelevant ones: a question
// with nothing to do with the documentation should retrieve nothing,
// not whatever happened to rank "least bad."
func (idx *Index) Search(query string, topN int) []Chunk {
	queryTerms := tokenize(query)
	if len(queryTerms) == 0 || len(idx.chunks) == 0 {
		return nil
	}

	type scored struct {
		chunk Chunk
		score float64
	}
	results := make([]scored, 0, len(idx.chunks))

	for i, counts := range idx.terms {
		docLen := 0
		for _, n := range counts {
			docLen += n
		}
		var score float64
		for _, qt := range queryTerms {
			tf := counts[qt]
			if tf == 0 {
				continue
			}
			df := idx.docFreq[qt]
			idf := math.Log(1 + (float64(len(idx.chunks))-float64(df)+0.5)/(float64(df)+0.5))
			denom := float64(tf) + bm25K1*(1-bm25B+bm25B*float64(docLen)/idx.avgLen)
			score += idf * (float64(tf) * (bm25K1 + 1) / denom)
		}
		if score > 0 {
			results = append(results, scored{idx.chunks[i], score})
		}
	}

	sort.Slice(results, func(a, b int) bool { return results[a].score > results[b].score })

	if len(results) > topN {
		results = results[:topN]
	}
	out := make([]Chunk, len(results))
	for i, r := range results {
		out[i] = r.chunk
	}
	return out
}

// ChunkByHeadingContains returns the first chunk whose Heading
// contains substr (case-insensitive), and true — or a zero Chunk and
// false if substr is "" or nothing matches. Exists for the one case
// plain BM25 ranking isn't reliable enough for on its own: when the
// caller already knows, with certainty, which section names the
// screen the question was actually asked from (see
// Root.consigliereDocSearchViewHint), a short chunk that only
// mentions that section's name once in passing can still outrank the
// actual, longer section for a query sharing little else with it —
// BM25's own length normalization rewards the shorter chunk's higher
// term density over the stronger, but diluted-by-length, heading
// match. Call sites that know the current screen should prepend this
// result rather than trust ranking alone to surface it.
func (idx *Index) ChunkByHeadingContains(substr string) (Chunk, bool) {
	if substr == "" {
		return Chunk{}, false
	}
	lower := strings.ToLower(substr)
	for _, c := range idx.chunks {
		if strings.Contains(strings.ToLower(c.Heading), lower) {
			return c, true
		}
	}
	return Chunk{}, false
}

// stopwords are dropped from every tokenize call, both when indexing
// and when querying — a real, measured gap this list closes: without
// it, a natural-language question like "how do I search the last 3
// hours in a log file" let ordinary function words ("how", "do", "i",
// "the", "in", "a") win the ranking purely because they're
// comparatively rare *within this specific, terse, specialized
// corpus* (BM25's own IDF rewards rarity, not real relevance), burying
// the chunks that actually matter ("hours", "relative", "log") under
// ones that just happen to contain more stopwords. A short, standard
// English function-word list, not a generic NLP stopword corpus —
// this documentation is English prose, nothing more exotic.
var stopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "can": true, "do": true, "does": true, "for": true,
	"from": true, "how": true, "i": true, "if": true, "in": true, "into": true,
	"is": true, "it": true, "its": true, "of": true, "on": true, "or": true,
	"that": true, "the": true, "this": true, "to": true, "was": true, "what": true,
	"when": true, "where": true, "which": true, "with": true, "you": true, "your": true,
}

// tokenize lowercases s and splits it into runs of letters/digits,
// dropping everything else (punctuation, Markdown syntax like "`"/
// "|"/"*") and every stopword — deliberately no stemming beyond that:
// this corpus's own vocabulary (chord names, setting keys) is
// specific enough that the small recall gain stemming would add
// isn't worth the complexity.
func tokenize(s string) []string {
	var tokens []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			if !stopwords[b.String()] {
				tokens = append(tokens, b.String())
			}
			b.Reset()
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		} else {
			flush()
		}
	}
	flush()
	return tokens
}
