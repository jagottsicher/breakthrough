package fsops

import (
	"os"
	"sort"
	"strings"
)

// Entry describes one item in a directory listing.
type Entry struct {
	Name  string
	IsDir bool
}

// ListDir returns the entries of the directory at path, sorted with
// directories first, then alphabetically (case-insensitive) within each
// group. It does not recurse.
//
// IsDir reflects what os.ReadDir's DirEntry reports for the entry itself,
// not the target of a symlink — a symlink to a directory is therefore not
// currently reported as a directory. This is a known simplification for
// Phase 0 and may need revisiting once symlink handling matters (e.g. for
// copy/move).
func ListDir(path string) ([]Entry, error) {
	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	// keyedEntry pairs each Entry with its lowercase sort key, computed
	// once up front rather than inside the sort.Slice comparator below
	// (which runs O(n log n) times) — for a directory with tens of
	// thousands of entries, recomputing strings.ToLower per comparison
	// means millions of redundant allocations for no benefit. Sorting a
	// slice of this combined type (instead of Entry plus a parallel key
	// slice) keeps each key attached to its Entry through every swap;
	// sort.Slice only knows how to swap elements of the one slice it's
	// given, so a separate parallel slice would silently desync.
	type keyedEntry struct {
		Entry
		key string
	}

	keyed := make([]keyedEntry, len(dirEntries))
	for i, de := range dirEntries {
		name := de.Name()
		keyed[i] = keyedEntry{
			Entry: Entry{Name: name, IsDir: de.IsDir()},
			key:   strings.ToLower(name),
		}
	}

	sort.Slice(keyed, func(i, j int) bool {
		if keyed[i].IsDir != keyed[j].IsDir {
			return keyed[i].IsDir // directories before files
		}
		return keyed[i].key < keyed[j].key
	})

	entries := make([]Entry, len(keyed))
	for i, k := range keyed {
		entries[i] = k.Entry
	}

	return entries, nil
}
