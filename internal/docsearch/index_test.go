package docsearch

import (
	"strings"
	"testing"
)

func TestSearchRanksTheMostRelevantChunkFirst(t *testing.T) {
	idx := NewIndex([]Chunk{
		{Heading: "Compress", Body: "Compress a file or directory into an archive format."},
		{Heading: "Log Holepunch screen", Body: "The Time field accepts a relative expression, such as 2 hours ago or 7 days, as well as absolute moments."},
		{Heading: "Rsync", Body: "Synchronize files between two directories or hosts."},
	})

	got := idx.Search("how do I search the last few hours in a log file", 1)

	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	if got[0].Heading != "Log Holepunch screen" {
		t.Errorf("top result = %q, want %q", got[0].Heading, "Log Holepunch screen")
	}
}

func TestSearchReturnsNothingForAnUnrelatedQuery(t *testing.T) {
	idx := NewIndex([]Chunk{
		{Heading: "Compress", Body: "Compress a file or directory into an archive format."},
		{Heading: "Rsync", Body: "Synchronize files between two directories or hosts."},
	})

	got := idx.Search("xenomorph platypus quantum", 5)

	if len(got) != 0 {
		t.Errorf("got %+v, want no results for a query sharing no terms with the corpus", got)
	}
}

func TestSearchRespectsTopN(t *testing.T) {
	idx := NewIndex([]Chunk{
		{Heading: "A", Body: "filter filter filter"},
		{Heading: "B", Body: "filter filter"},
		{Heading: "C", Body: "filter"},
	})

	got := idx.Search("filter", 2)

	if len(got) != 2 {
		t.Fatalf("got %d results, want exactly topN=2", len(got))
	}
	if got[0].Heading != "A" || got[1].Heading != "B" {
		t.Errorf("got %+v, want [A, B] in descending relevance order", got)
	}
}

func TestSearchOnEmptyIndexReturnsNothing(t *testing.T) {
	idx := NewIndex(nil)
	if got := idx.Search("anything", 5); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

func TestSearchOnEmptyQueryReturnsNothing(t *testing.T) {
	idx := NewIndex([]Chunk{{Heading: "A", Body: "some text"}})
	if got := idx.Search("   ", 5); got != nil {
		t.Errorf("got %+v, want nil for a query with no real tokens", got)
	}
}

func TestTokenizeLowercasesDropsPunctuationAndStopwords(t *testing.T) {
	got := tokenize("Chord `gC` — opens the AI Consigliere!")
	want := []string{"chord", "gc", "opens", "ai", "consigliere"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("tokenize = %v, want %v", got, want)
	}
}

func TestNewDefaultIndexBuildsFromTheRealEmbeddedUserGuide(t *testing.T) {
	idx, err := NewDefaultIndex()
	if err != nil {
		t.Fatalf("NewDefaultIndex: %v", err)
	}
	if len(idx.chunks) == 0 {
		t.Fatal("NewDefaultIndex produced an empty index")
	}

	got := idx.Search("relative time expression log holepunch", 3)
	if len(got) == 0 {
		t.Fatal("search for a documented topic returned nothing")
	}
	var found bool
	for _, c := range got {
		if strings.Contains(c.Heading, "Log Holepunch") {
			found = true
		}
	}
	if !found {
		t.Errorf("results = %+v, want the Log Holepunch section among the top matches", got)
	}
}
