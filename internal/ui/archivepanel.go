package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jagottsicher/breakthrough/internal/archive"
	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// resolveArchiveState decides whether abs (load's own target, already
// filepath.Abs'd) lies inside an archive — and if so, which one, and
// where inside it — so load() can source its rows from archive.List
// instead of fsops.ListDir. See Panel.archivePath's own doc comment for
// what abs looks like in that case.
//
// The already-browsing-this-archive case (p.archivePath != "") is
// resolved by a plain string-prefix check against abs — no filesystem
// access at all: entering a subdirectory or going back up via ".."
// inside the same archive never needs to ask the disk anything, since
// nothing about the *archive itself* changed. Falling out of it (abs no
// longer under p.archivePath — gone up past the archive's own root)
// falls through to the check below instead of just reporting false,
// so a rare future path — jumping directly from one archive's own
// listing into a second, different one — would still resolve
// correctly rather than silently landing in the first archive's own
// stale internalDir.
//
// The not-currently-browsing-one case costs exactly one os.Stat, and
// only when abs's own extension already matches archive.Classify —
// every ordinary directory name fails that cheap string check first
// and never reaches the stat at all. The stat itself is what tells a
// real archive *file* named e.g. "backup.zip" apart from an ordinary
// directory that simply happens to be named that (unusual, but not
// this package's job to forbid) — only the former is ever entered as
// an archive.
func (p *Panel) resolveArchiveState(abs string) (archivePath, internalDir string, inArchive bool) {
	if p.archivePath != "" {
		if abs == p.archivePath {
			return p.archivePath, "", true
		}
		if rel, ok := strings.CutPrefix(abs, p.archivePath+"/"); ok {
			return p.archivePath, rel, true
		}
	}
	return splitArchivePath(abs)
}

// splitArchivePath is resolveArchiveState's own stateless fallback,
// also used directly wherever there's no live Panel to ask (Root's own
// Copy/Paste plumbing, whose r.clipboard entries may well have been
// recorded from a tab, or an archive session, that's since moved on —
// see pasteInto). Unlike resolveArchiveState's own fast path, this
// doesn't assume p is already exactly the archive file or exactly one
// level away from it: it climbs from p all the way up to the
// filesystem root if it has to, checking each ancestor in turn (see
// splitOnce), so a deeply nested clipboard path (several directories
// inside the archive) still resolves correctly from a single call, not
// just the shallow, one-step-at-a-time case load() itself always
// produces internally.
//
// Every step this climbs costs one archive.Classify (a plain string
// check, free) and — only when that matches — one os.Stat; an ordinary
// real path climbs to "/" doing at most one wasted stat per path
// component whose name happens to end in a recognized archive suffix
// (rare), and none at all otherwise. Bounded by the path's own depth,
// so this is never worse than one os.ReadDir would already cost.
func splitArchivePath(p string) (archivePath, internalDir string, ok bool) {
	var suffix []string
	cur := p
	for {
		if archivePath, ok := splitOnce(cur); ok {
			for i, j := 0, len(suffix)-1; i < j; i, j = i+1, j-1 {
				suffix[i], suffix[j] = suffix[j], suffix[i]
			}
			return archivePath, strings.Join(suffix, "/"), true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", "", false // reached the filesystem root without finding one
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
}

// splitOnce reports whether cur itself is a real archive file — the
// single check splitArchivePath repeats at every level as it climbs.
func splitOnce(cur string) (archivePath string, ok bool) {
	if _, classified := archive.Classify(cur); !classified {
		return "", false
	}
	fi, err := os.Stat(cur)
	if err != nil || fi.IsDir() {
		return "", false
	}
	return cur, true
}

// loadArchiveEntries is load's own archive-sourced counterpart to
// fsops.ListDir: archivePath's full member listing (archiveEntries,
// re-read via archive.List only when archivePath itself is new — see
// Panel.archiveEntries' own doc comment), narrowed to internalDir's own
// direct children (archive.Children), turned into the same []fsops.Entry
// shape load()'s existing filter/sort/render pipeline already expects.
// A member's own Mode there is always its stored permission bits with
// no type bits set — Type below is what load()'s row-building actually
// keys off, the same as a real fsops.ListDir result.
//
// Reports the underlying archive.List error, if any (a corrupted
// archive, one deleted out from under an open tab, ...), the same way
// fsops.ListDir's own error already surfaces to load()'s caller.
func (p *Panel) loadArchiveEntries(archivePath, internalDir string) ([]fsops.Entry, error) {
	if p.archivePath != archivePath {
		listed, err := archive.List(archivePath)
		if err != nil {
			return nil, err
		}
		p.archivePath = archivePath
		p.archiveEntries = listed
	}

	children := archive.Children(p.archiveEntries, internalDir)
	archive.SortByName(children)

	entries := make([]fsops.Entry, len(children))
	for i, c := range children {
		typ := fsops.TypeFile
		if c.IsDir {
			typ = fsops.TypeDir
		}
		entries[i] = fsops.Entry{
			Name:    filepathBaseSlash(c.Path),
			Type:    typ,
			IsDir:   c.IsDir,
			Mode:    c.Mode,
			Size:    c.Size,
			ModTime: c.ModTime,
		}
	}
	return entries, nil
}

// filepathBaseSlash is path.Base for an archive member's own
// forward-slash path (see archive.Entry.Path's own doc comment on why
// it's always "/"-separated regardless of host OS) — named apart from
// an ordinary filepath.Base call so nothing here accidentally uses the
// host's own separator on a path that was never one to begin with.
func filepathBaseSlash(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// inArchiveView reports whether this Panel is currently browsing inside
// an archive rather than a real directory — checked by every action
// this feature deliberately doesn't extend into an archive (rename,
// edit, chmod/chown, Cut, Move to Trash/Remove, Batch Rename, Sed
// Replace — see this feature's own scope note at the top of this file)
// before it would otherwise try a real filesystem call against a
// composite path nothing on disk actually answers to.
func (p *Panel) inArchiveView() bool {
	return p.archivePath != ""
}

// errNotSupportedInArchive is what every action guarded by
// inArchiveView reports instead of proceeding — one shared message
// rather than each call site wording its own, since the reason is
// always exactly the same: this feature only ever supports browsing,
// marking, and copying out (see archive.go's own package doc comment).
var errNotSupportedInArchive = errors.New("not supported while browsing inside an archive — only marking and copying out is")

// leavingArchive reports whether load()'s new target abs falls outside
// whatever archive p.archivePath currently names — called once, right
// before p.path is actually overwritten, so the cached
// archivePath/archiveEntries this Panel is holding onto get dropped
// exactly when they stop applying to anything still on screen, the
// same "reset happens as part of load()" contract every other per-
// directory Panel field already follows (see load's own doc comment on
// selected/lastNameClickRow).
func (p *Panel) leavingArchive(abs string) bool {
	if p.archivePath == "" {
		return false
	}
	return abs != p.archivePath && !strings.HasPrefix(abs, p.archivePath+"/")
}
