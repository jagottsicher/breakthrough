package ui

import "testing"

// TestDefaultDocSearchIndexIsSharedAcrossRoots pins the real,
// measured fix: every Root used to rebuild the whole documentation
// search index from scratch in NewRoot, which — multiplied across
// this package's own hundreds of NewRoot-driven tests — was slow
// enough to tip a slower CI runner's per-package test timeout over
// the edge. Two separate Root instances must now end up pointing at
// the exact same *docsearch.Index, not two independently built ones.
func TestDefaultDocSearchIndexIsSharedAcrossRoots(t *testing.T) {
	a := newPlainKeyRoot(t)
	b := newPlainKeyRoot(t)

	if a.docSearchIndex == nil || b.docSearchIndex == nil {
		t.Fatal("both roots should have a real index, not nil")
	}
	if a.docSearchIndex != b.docSearchIndex {
		t.Error("the two roots should share the exact same *docsearch.Index instance, not each build their own")
	}
}
