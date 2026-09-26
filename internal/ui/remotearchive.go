// remotearchive.go is how a zip/tar/... living on a remote connection
// ever gets entered at all: internal/archive has no notion of a
// remotefs.Client, and a zip's own central directory sits at the end
// of the file regardless — there's no way to browse one without the
// whole thing local first, unlike every other remote operation in this
// project, which streams or stats directly over the connection. See
// archivepanel.go's own resolveRemoteArchiveState for the other half
// of this feature: how Panel.load recognizes an archive this file has
// already staged.
package ui

import (
	"fmt"
	"path"
	"strings"

	"github.com/jagottsicher/breakthrough/internal/archive"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// enterRemoteArchive is Panel.onEnterRemoteArchive's own action —
// activateRow has already confirmed the cursor sits on a recognized
// archive file (see archive.Classify) on a remote panel before this
// ever runs. Stats it for its real size and, at or above
// remote_archive_confirm_size (see config.Settings' own doc comment on
// why this is the one setting in this app with a KB/MB/GB unit
// suffix), asks first, naming that real size, rather than silently
// starting what could be a large, slow download; below it, the
// download just starts on its own, the same proactively-transparent
// way every other remote operation in this project already works.
func (r *Root) enterRemoteArchive() {
	panel := r.panel
	remote := panel.remote
	if remote == nil {
		return
	}
	_, archivePath, ok := panel.CurrentRowPath()
	if !ok {
		return
	}

	entry, err := remote.Stat(archivePath)
	if err != nil {
		r.showError(fmt.Errorf("open %s: %w", archivePath, err))
		return
	}

	if needsRemoteArchiveConfirm(entry.Size, r.settings.RemoteArchiveConfirmSize) {
		message := fmt.Sprintf("%s is %s — download it to open?", path.Base(archivePath), humanSize(entry.Size))
		r.openConfirm(message, "Yes, download and open", func() {
			r.startRemoteArchiveDownload(panel, remote, archivePath)
		})
		return
	}
	r.startRemoteArchiveDownload(panel, remote, archivePath)
}

// needsRemoteArchiveConfirm is enterRemoteArchive's own threshold
// compare, split out into a small, directly testable predicate rather
// than left inline — >=, not >, so a size *exactly* equal to the
// configured threshold still asks first, matching how a person reading
// "confirm above 10MB" would expect a file that's exactly 10MB to
// behave.
func needsRemoteArchiveConfirm(size, threshold int64) bool {
	return size >= threshold
}

// startRemoteArchiveDownload runs in the background (see safeGo) so a
// slow network transfer never freezes the rest of the app — the same
// "genuinely async, no visible progress indicator yet, just a result
// once it's done" scope remotepaste.go's own transfer engine already
// accepts for the identical reason (see its own package doc comment).
// Only once the download has actually landed does it stash the local
// temp copy on panel itself (archiveLocalPath/archiveRemoteClient) and
// navigate in — load()'s own resolveRemoteArchiveState is what
// recognizes this exact hand-off (see its own doc comment on why
// archivePath being "" going into that first call is expected, not a
// bug).
func (r *Root) startRemoteArchiveDownload(panel *Panel, remote remotefs.Client, archivePath string) {
	r.safeGo("remote archive download", nil, func() {
		localPath, cleanup, err := downloadRemoteToTemp(remote, archivePath)
		r.app.QueueUpdateDraw(func() {
			r.finishRemoteArchiveDownload(panel, remote, archivePath, localPath, cleanup, err)
		})
	})
}

// finishRemoteArchiveDownload is startRemoteArchiveDownload's own
// synchronous finish, split out so a test can call it directly with a
// download it staged itself — the same "callable without a real
// Application event loop" reasoning connectdialog.go's own
// finishConnect doc comment gives for its identical split from
// runConnect: r.app.QueueUpdateDraw blocks forever with nothing
// actually running the event loop to drain it, which every test here
// runs without.
//
// Stashes the downloaded copy on panel (archiveLocalPath/
// archiveRemoteClient) before navigating in — load()'s own
// resolveRemoteArchiveState is what recognizes this exact hand-off
// (see its own doc comment). Rolls that staging back out, and removes
// the temp file, if navigate then fails after all — a corrupted
// archive, most likely — so this panel doesn't look like it's still
// browsing one it never actually entered.
func (r *Root) finishRemoteArchiveDownload(panel *Panel, remote remotefs.Client, archivePath, localPath string, cleanup func(), err error) {
	if err != nil {
		r.showError(fmt.Errorf("open %s: %w", archivePath, err))
		return
	}
	if !r.hasTab(panel) {
		// The tab this download was meant for closed while it was
		// still in flight — nothing left to enter it into.
		cleanup()
		return
	}

	panel.archiveLocalPath = localPath
	panel.archiveRemoteClient = remote
	if err := panel.navigate(archivePath); err != nil {
		panel.archiveLocalPath = ""
		panel.archiveRemoteClient = nil
		cleanup()
		r.showError(err)
	}
}

// remoteArchiveExtractionFor is archiveExtractionFor's own remote
// counterpart, for pasteInto's identical "is this an archive
// extraction, or an ordinary paste" branch whenever either side of a
// paste is remote. It can't work from paths[0] alone via
// splitArchivePath's own os.Stat climb the way the local version
// does — a remote archive's virtual "archive/member" clipboard path
// was never a real local path to stat in the first place — so it
// instead scans every open tab for one whose own archiveRemoteClient
// is set and whose archivePath is a prefix of paths[0] (the same scan
// this function's own predecessor, remoteArchiveMemberOrigin, used
// back when this could only ever refuse the paste), and reads the
// archive's real member listing from that tab's own already-
// downloaded archiveLocalPath instead of the (unopenable) remote
// archivePath itself.
//
// Returns that local temp copy's own path, not archivePath — it's
// what archive.List/archive.Extract actually need to read real bytes
// from. Member resolution mirrors archiveExtractionFor's own logic
// exactly (an exact listed entry, or a synthesized directory Entry for
// one only ever implied by its own descendants), just keyed off
// archivePath as the shared prefix every clipboard entry here is
// stripped against instead of splitArchivePath's own return value.
func (r *Root) remoteArchiveExtractionFor(paths []string) (archiveLocalPath string, members []archive.Entry, ok bool) {
	if len(paths) == 0 {
		return "", nil, false
	}
	var source *Panel
	r.forEachTab(func(p *Panel) {
		if source != nil || p.archiveRemoteClient == nil || p.archivePath == "" {
			return
		}
		// strings.HasPrefix(paths[0], p.archivePath+"/") alone, not
		// paths[0] == p.archivePath too: the latter would match the
		// archive file's own path with no member suffix at all — the
		// same real bug archiveExtractionFor's own identical check
		// guards against (see its own doc comment): marking the archive
		// file itself for an ordinary Copy/Cut, not any of its own
		// members, so there is no member here for archive.Extract to act
		// on either.
		if strings.HasPrefix(paths[0], p.archivePath+"/") {
			source = p
		}
	})
	if source == nil {
		return "", nil, false
	}

	listed, err := archive.List(source.archiveLocalPath)
	if err != nil {
		return "", nil, false
	}
	byPath := make(map[string]archive.Entry, len(listed))
	for _, e := range listed {
		byPath[e.Path] = e
	}
	for _, p := range paths {
		internal, ok := strings.CutPrefix(p, source.archivePath+"/")
		if !ok {
			continue
		}
		if e, found := byPath[internal]; found {
			members = append(members, e)
			continue
		}
		if hasDescendant(listed, internal) {
			members = append(members, archive.Entry{Path: internal, IsDir: true})
		}
	}
	return source.archiveLocalPath, members, true
}
