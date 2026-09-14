package archive

import (
	"path"
	"sort"
	"strings"
)

// Children returns dir's own direct children out of entries — one
// directory level, the shape internal/ui's Panel needs to show a
// browsable listing (see its own load()/loadArchiveDir split). dir is
// "" for the archive's own root; entries elsewhere in the tree (a
// sibling directory's own contents, say) are simply not returned.
//
// Not every archive format stores an explicit entry for every
// intermediate directory — zip in particular routinely omits one for a
// directory that's never empty, only ever implied by its members' own
// paths (verified directly against a handful of real-world zips, not
// assumed) — so an implied directory with no Entry of its own is
// synthesized here (IsDir true, Size/Mode/ModTime left at their zero
// value — nothing to report for a directory this package never
// actually saw a header for). A real, explicit directory Entry for the
// same path is preferred over a synthesized one if both exist (see the
// two-pass structure below), so its own real ModTime/Mode still show
// when the archive did bother to store one.
//
// Result order: not sorted here — internal/ui applies its own Panel-
// wide sort preference the same way a real directory's own listing
// does (see Panel.load), so sorting twice would be wasted work at best
// and a second, subtly different sort order at worst.
func Children(entries []Entry, dir string) []Entry {
	dir = path.Clean(dir)
	if dir == "." {
		dir = ""
	}

	byPath := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byPath[e.Path] = e
	}

	seen := make(map[string]bool)
	var result []Entry
	addChild := func(childPath string, synth Entry) {
		if seen[childPath] {
			return
		}
		seen[childPath] = true
		if real, ok := byPath[childPath]; ok {
			result = append(result, real)
			return
		}
		result = append(result, synth)
	}

	for _, e := range entries {
		if e.Path == dir {
			continue // dir's own entry, if it has one — not its own child
		}
		rel := e.Path
		if dir != "" {
			if !strings.HasPrefix(rel, dir+"/") {
				continue
			}
			rel = strings.TrimPrefix(rel, dir+"/")
		} else if strings.HasPrefix(rel, "/") {
			continue // a malformed absolute member path — never a child of the root either way
		}

		if slash := strings.IndexByte(rel, '/'); slash >= 0 {
			// e is nested deeper than one level below dir — what belongs
			// in this listing is the implied directory one level down,
			// not e itself.
			name := rel[:slash]
			childPath := name
			if dir != "" {
				childPath = dir + "/" + name
			}
			addChild(childPath, Entry{Path: childPath, IsDir: true})
			continue
		}

		addChild(e.Path, e)
	}
	return result
}

// SortByName sorts entries by base name, directories first — the
// default order a freshly entered archive (or archive subdirectory)
// shows before Panel's own sort preference (if anything other than
// "Name ascending") reorders it, matching a real directory's own
// initial listing order.
func SortByName(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir // directories first
		}
		return path.Base(entries[i].Path) < path.Base(entries[j].Path)
	})
}
