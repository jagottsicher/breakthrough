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

	entries := make([]Entry, len(dirEntries))
	for i, de := range dirEntries {
		entries[i] = Entry{
			Name:  de.Name(),
			IsDir: de.IsDir(),
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir // directories before files
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	return entries, nil
}
