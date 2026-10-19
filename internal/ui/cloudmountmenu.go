package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/rclone"
)

// Cloud-mount business logic for the connect menu's own merged table
// (see connectionmenu.go): a cloud mount is listed as a row in the
// exact same connectionMenuTable an SFTP connection is, told apart by
// the always-present Type column, rather than a separate dropdown of
// its own — per the user's own explicit request that both read as one
// list, not two things reached through two different menus with two
// different navigation paths. This file only holds the business logic
// (status text, mount/unmount, remove); the actual row rendering and
// per-cell dispatch live in connectionmenu.go, right alongside the
// SFTP rows they're merged with.

// cloudMountRemoveGlyph mirrors connectionHistoryRemoveGlyph — the
// same "✕" every per-row remove action in this app already uses.
const cloudMountRemoveGlyph = connectionHistoryRemoveGlyph

// cloudMountStatusText reports entry's own live status, by shelling
// out through internal/rclone.IsMounted — or, while a Mount/Unmount
// call for its Target is still running in the background (see
// toggleCloudMount), a transient "mounting…"/"unmounting…" label
// instead of a stale "mounted"/"not mounted" that would otherwise sit
// there, unexplained, for however long that subprocess takes.
func (r *Root) cloudMountStatusText(entry rclone.MountEntry) string {
	if pending, ok := r.cloudMountPending[entry.Target]; ok {
		return pending
	}
	mounted, err := rclone.IsMounted(entry.Target)
	if err != nil {
		return "error"
	}
	if mounted {
		return "mounted"
	}
	return "not mounted"
}

// cloudMountRowColor mirrors connectionHistoryColor's own shape for
// the other kind of row connectionMenuTable now carries: green
// (theme.EntryExecutable) while actually mounted — the same color
// isActive already gets — red (theme.CriticalText) if the IsMounted
// check itself errored (a real, surfaced problem, not just "not
// mounted"), plain theme.Text otherwise. Deliberately no third,
// blended "used to work" shade the way connectionHistoryColor's own
// default case has: a cloud mount's mounted/not-mounted state is a
// live fact checked fresh on every render, never a remembered past
// outcome the way LastFailed/Untested are for SFTP history.
func cloudMountRowColor(theme config.ResolvedTheme, mounted, errored bool) tcell.Color {
	switch {
	case errored:
		return theme.CriticalText
	case mounted:
		return theme.EntryExecutable
	default:
		return theme.Text
	}
}

// toggleCloudMount mounts entry if it isn't currently mounted, or
// unmounts it if it is — run via safeGo so a slow network backend's
// own real Mount/Unmount latency (unlike the local, near-instant case
// internal/rclone's own integration test exercises) never blocks the
// UI thread, the same non-blocking-background-work rule this project
// applies to every other potentially slow external-tool call (see
// runConnect's own identical reasoning for SFTP). Deliberately no
// cancellation yet, unlike runConnect's own animated, cancelable
// progress line — internal/rclone has no way to kill an in-flight
// rclone process yet either, so a cancel button here could only ever
// be decorative; left for whenever that's actually built, rather than
// faked now.
func (r *Root) toggleCloudMount(entry rclone.MountEntry) {
	mounted, err := rclone.IsMounted(entry.Target)
	if err != nil {
		r.showError(err)
		return
	}

	if r.cloudMountPending == nil {
		r.cloudMountPending = map[string]string{}
	}
	if mounted {
		r.cloudMountPending[entry.Target] = "unmounting…"
	} else {
		r.cloudMountPending[entry.Target] = "mounting…"
	}
	r.renderConnectionMenu()

	onPanic := func() {
		delete(r.cloudMountPending, entry.Target)
		r.renderConnectionMenu()
	}
	r.safeGo("cloud mount toggle", onPanic, func() {
		var opErr error
		if mounted {
			opErr = rclone.Unmount(entry.Target)
		} else {
			if err := rclone.EnsureMountDir(entry.Target); err != nil {
				opErr = err
			} else {
				opErr = rclone.Mount(entry.RemoteName+":", entry.Target, rclone.MountOptions{
					CacheDir: rclone.MountBase(r.settings) + "/.cache/" + entry.RemoteName,
				})
			}
		}
		r.app.QueueUpdateDraw(func() {
			delete(r.cloudMountPending, entry.Target)
			r.renderConnectionMenu()
			if opErr != nil {
				r.showError(fmt.Errorf("cloud mount %q: %w", entry.RemoteName, opErr))
			}
		})
	})
}

// removeCloudMount drops entry from the persisted list — refuses
// outright while it's still mounted (unmount first, the same
// "disconnect before removing" shape disconnectConnectionRow/
// removeConnectionHistoryRow already enforce for an active SFTP
// connection) rather than leaving an orphaned live mount nothing in
// breakthrough's own UI can reach anymore.
func (r *Root) removeCloudMount(entry rclone.MountEntry) {
	mounted, err := rclone.IsMounted(entry.Target)
	if err != nil {
		r.showError(err)
		return
	}
	if mounted {
		r.showError(fmt.Errorf("unmount %q before removing it", entry.RemoteName))
		return
	}
	r.openConfirm(
		fmt.Sprintf("Remove %q and delete its cloud credentials permanently?", entry.RemoteName),
		"Yes, delete permanently",
		func() { r.deleteCloudMount(entry) },
	)
}

// deleteCloudMount is removeCloudMount's own confirmed action — see
// deleteCloudRemoteByName's own doc comment for what it actually does
// and why that order matters; this just supplies the MountEntry it
// always has (removeCloudMount is only ever reached for a row that
// already has one) and re-renders the merged list afterwards.
func (r *Root) deleteCloudMount(entry rclone.MountEntry) {
	r.deleteCloudRemoteByName(entry.RemoteName, &entry)
	r.renderConnectionMenu()
}

// deleteCloudRemoteByName drops remoteName's own MountEntry, if
// mountEntry is non-nil (nil means there isn't one — see
// runDeleteSelectedCloudRemote's own doc comment for the case that
// actually needs this), and then the underlying rclone remote itself
// (its real credentials, via DeleteRemote), logging either outcome
// the same way connectdialog.go's own "saved"/"connected to" actions
// already do (activitylog.CategoryRemote) — a real, reported gap:
// there was no way to remove a cloud remote's own credentials from
// rclone's config at all before this, only breakthrough's own
// MountEntry for it.
//
// Order matters: RemoveMountEntry first, DeleteRemote second. If
// DeleteRemote were to fail after RemoveMountEntry already succeeded,
// the remote would be an "orphan" (still in rclone's config, no
// longer reachable as a configured mount) — annoying, but recoverable
// through "+ Create new remote…"'s own dropdown, which lists every
// real ListRemotes entry regardless of MountEntry status. The reverse
// order would risk the opposite: a MountEntry left pointing at a
// remote that no longer exists at all, which every other read of it
// (ListRemotes-backed Type lookups, toggleCloudMount's own Mount call)
// would then have to treat as a dangling reference instead.
func (r *Root) deleteCloudRemoteByName(remoteName string, mountEntry *rclone.MountEntry) {
	if mountEntry != nil {
		if err := rclone.RemoveMountEntry(mountEntry.Target); err != nil {
			r.showError(err)
			return
		}
	}
	if err := rclone.DeleteRemote(remoteName); err != nil {
		r.activityLog.Error(activitylog.CategoryRemote, fmt.Sprintf("removed cloud mount %q but failed to delete its remote: %v", remoteName, err))
		r.showError(err)
		return
	}
	r.activityLog.Action(activitylog.CategoryRemote, fmt.Sprintf("deleted cloud remote %q", remoteName))
}
