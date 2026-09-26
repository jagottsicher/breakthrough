package ui

import (
	"fmt"
	"strings"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/archive"
)

// archiveExtractionFor decides whether clipboard (Copy/Cut's own
// source list — see Root.clipboard) names members inside a single
// archive rather than real files on disk, for pasteInto's own "which
// kind of paste is this" branch. Checked once, from the first item
// alone: a marked selection is always scoped to whichever single
// directory — a real one, or one archive's own internal view — was on
// screen when Copy/Cut was pressed (see Panel.selected's own doc
// comment), never a mix of the two, so there's nothing to gain from
// checking every item just to confirm what the first one already
// answers.
//
// members comes back as the real archive.Entry values (not just the
// clipboard's own path strings) specifically so Extract knows which of
// them are directories — see its own doc comment on why that matters
// for an otherwise-empty one. A clipboard item that no longer matches
// anything in the archive (rare: only if the archive on disk changed
// since Copy was pressed) is silently left out rather than failing the
// whole extraction over it.
func archiveExtractionFor(clipboard []string) (archivePath string, members []archive.Entry, ok bool) {
	if len(clipboard) == 0 {
		return "", nil, false
	}
	var firstInternal string
	archivePath, firstInternal, ok = splitArchivePath(clipboard[0])
	if !ok {
		return "", nil, false
	}
	if firstInternal == "" {
		// clipboard[0] IS the archive file itself (splitArchivePath's own
		// "cur is already a real archive file" case — see its own doc
		// comment), not a member inside one: a real, user-reported bug
		// otherwise, since this is exactly what marking a plain .zip/.tar.gz
		// file for an ordinary Copy/Cut produces. Refusing here falls
		// through to pasteInto's own ordinary startPaste instead, which
		// copies/moves the archive file itself like any other file —
		// there is nothing here for archive.Extract to meaningfully do
		// with zero actual members, which silently produced an empty,
		// no-op "copy" before this check existed.
		return "", nil, false
	}
	listed, err := archive.List(archivePath)
	if err != nil {
		return "", nil, false
	}
	byPath := make(map[string]archive.Entry, len(listed))
	for _, e := range listed {
		byPath[e.Path] = e
	}
	for _, c := range clipboard {
		_, internal, ok := splitArchivePath(c)
		if !ok {
			continue
		}
		if e, found := byPath[internal]; found {
			members = append(members, e)
			continue
		}
		// No explicit entry of its own — many archives never store one
		// for a directory that's only ever implied by its own members'
		// paths (see archive.Children's own doc comment on exactly this,
		// and why load() already has to synthesize one to show it at
		// all). A marked row like that still needs its own Entry here,
		// synthesized the same way, or it would silently drop out of
		// the paste instead of extracting everything nested under it.
		if hasDescendant(listed, internal) {
			members = append(members, archive.Entry{Path: internal, IsDir: true})
		}
	}
	return archivePath, members, true
}

// hasDescendant reports whether any entry in listed lies under dir —
// archiveExtractionFor's own check for whether a directory with no
// explicit entry of its own is still a real, non-empty part of the
// archive (as opposed to a clipboard entry that's simply stale).
func hasDescendant(listed []archive.Entry, dir string) bool {
	prefix := dir + "/"
	for _, e := range listed {
		if strings.HasPrefix(e.Path, prefix) {
			return true
		}
	}
	return false
}

// extractClipboardArchive is pasteInto's own action once
// archiveExtractionFor has confirmed the clipboard names archive
// members rather than real files. archive.Extract runs off the UI
// thread — the same "a paste's real I/O never blocks the event loop"
// contract pasteOne already keeps for an ordinary Copy, just without
// that whole job/progress/conflict machinery behind it (see this
// feature's own doc comment in archivepanel.go on why extraction is
// deliberately simpler: no rename/move/delete inside an archive to
// coordinate with, and no existing-destination conflict dialog yet
// either — a name already present at destDir is silently overwritten,
// same as archive.Extract's own contract). Reports back through
// QueueUpdateDraw once done: any error, then a reload of every open tab
// currently showing destDir, so the extracted files/folders actually
// appear without a manual reload — the same visible-result guarantee
// finishPasteJob already gives a real Paste.
func (r *Root) extractClipboardArchive(archivePath string, members []archive.Entry, destDir string) {
	r.safeGo("archive extract", nil, func() {
		err := archive.Extract(archivePath, members, destDir)
		r.app.QueueUpdateDraw(func() { r.finishExtractClipboardArchive(archivePath, len(members), destDir, err) })
	})
}

// finishExtractClipboardArchive is extractClipboardArchive's own
// QueueUpdateDraw hand-off, split out so it's directly, synchronously
// testable — the same reasoning finishDuplicate's own doc comment
// gives for finishCompressJob/finishRsyncJob's identical shape.
func (r *Root) finishExtractClipboardArchive(archivePath string, memberCount int, destDir string, err error) {
	r.forEachTab(func(p *Panel) {
		if p.path == destDir {
			p.reportError(p.load(p.path))
		}
	})
	if err != nil {
		r.activityLog.Error(activitylog.CategoryArchive, fmt.Sprintf("extract from %s: %v", archivePath, err))
		r.showError(fmt.Errorf("extract from %s: %w", archivePath, err))
		return
	}
	r.activityLog.Action(activitylog.CategoryArchive, fmt.Sprintf("extracted %d item(s) from %s", memberCount, archivePath))
}
