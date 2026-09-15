// remotetrash.go gives a remote connection its own trash — a mirror
// of this project's own local one (see internal/fsops/trash.go), just
// executed via remotefs.Client calls (Mkdir/Create/Open/Rename)
// instead of local os ones, so "d" (Move to Trash) is reversible on a
// remote panel too, instead of always meaning permanent deletion the
// way it used to (per the user's own explicit "think of something"
// request). Lives at ".breakthrough-trash" directly under the
// connection's own Root() — one fixed, predictable location per
// connection, the same "one trash, not one per directory" shape the
// local trash already has, and cheap to move into: Client.Rename is a
// single request, no data transfer, exactly the same "really just one
// rename" case runRemotePaste's own same-connection move already
// relies on.
//
// Deliberately simpler than the local trash in two ways, both
// explicitly out of scope for this first version rather than
// overlooked:
//   - No automatic age/quota-based pruning at startup (see
//     fsops.PruneTrash) — that would mean connecting to every
//     previously-used remote host on every local startup just to
//     check, which this project has no business doing on its own.
//   - Restore refuses outright on a conflict rather than local
//     Restore's own full Paste-conflict-dialog treatment — the same
//     "skip/fail rather than negotiate" simplicity remotepaste.go's own
//     transfer engine already settled on for an ordinary remote paste,
//     for the identical reason (see its own package doc comment).
package ui

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// remoteTrashRootName is the hidden directory every remote trash lives
// under — dot-prefixed so it doesn't clutter an ordinary directory
// listing any more than the local trash's own hidden location does.
const remoteTrashRootName = ".breakthrough-trash"

func remoteTrashDir(client remotefs.Client) string {
	return path.Join(client.Root(), remoteTrashRootName)
}

func remoteTrashFilesDir(client remotefs.Client) string {
	return path.Join(remoteTrashDir(client), "files")
}

func remoteTrashInfoDir(client remotefs.Client) string {
	return path.Join(remoteTrashDir(client), "info")
}

// ensureRemoteTrashSkeleton is ensureTrashSkeleton's own remote
// counterpart: Mkdir on each of the three directories in
// parent-before-child order, "already exists" not treated as a
// failure — every remote trash operation calls this first, the same
// "never trust it survived since last time" reasoning
// ensureTrashSkeleton's own doc comment already gives (an external
// sftp/scp session touching part of the trash outside breakthrough is
// exactly as expected here as it is locally). Client.Mkdir has no
// portable "already exists" sentinel to check via errors.Is (a real
// SFTP server's own failure status varies), so a failed Mkdir is only
// treated as fatal once a follow-up Lstat also fails to find the
// directory already there.
func ensureRemoteTrashSkeleton(client remotefs.Client) error {
	for _, dir := range []string{remoteTrashDir(client), remoteTrashFilesDir(client), remoteTrashInfoDir(client)} {
		if err := client.Mkdir(dir); err != nil {
			if _, statErr := client.Lstat(dir); statErr == nil {
				continue
			}
			return err
		}
	}
	return nil
}

// randomRemoteTrashSlug mirrors fsops's own local slug shape exactly
// (an eight-hex-character collision-avoidance prefix, crypto/rand,
// joined to the original basename with "_") so a raw listing of
// files/ — from a plain sftp/scp session, say — still shows what each
// item actually was, the same reason the local trash's own on-disk
// names do.
func randomRemoteTrashSlug(originalPath string) (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]) + "_" + path.Base(originalPath), nil
}

// moveToTrashRemote moves target into client's own remote trash.
// Writes the sidecar recording target's real original path and the
// deletion time *before* moving the payload itself — the same order
// fsops.MoveToTrash's own doc comment explains locally: a move that
// then fails to also get its sidecar written is recoverable (the
// payload just won't show up with its real name/time until it's
// cleaned up by hand); a sidecar for a payload that was never actually
// moved would be worse, a phantom restore target. If the rename
// itself fails, the just-written sidecar is removed again so it never
// outlives the item it claims to describe.
func moveToTrashRemote(client remotefs.Client, target string) error {
	if err := ensureRemoteTrashSkeleton(client); err != nil {
		return err
	}

	slug, err := randomRemoteTrashSlug(target)
	if err != nil {
		return err
	}
	infoPath := path.Join(remoteTrashInfoDir(client), slug+".trashinfo")
	filesPath := path.Join(remoteTrashFilesDir(client), slug)

	if err := writeRemoteTrashInfo(client, infoPath, target, time.Now().UTC()); err != nil {
		return err
	}
	if err := client.Rename(target, filesPath); err != nil {
		_ = client.Remove(infoPath)
		return err
	}
	return nil
}

// writeRemoteTrashInfo writes originalPath/deletedAt as a small
// "key = value" sidecar — this project's own established plain-text
// config shape (see internal/config's own package doc comment) rather
// than freedesktop.org's .trashinfo INI format, the same choice
// fsops.MoveToTrash's own writeTrashInfo already made locally via
// config.SetKey; written directly here instead of through that
// function since it works against a local path, not a remotefs.Client.
func writeRemoteTrashInfo(client remotefs.Client, infoPath, originalPath string, deletedAt time.Time) error {
	w, err := client.Create(infoPath)
	if err != nil {
		return err
	}
	content := fmt.Sprintf("path = %s\ndeleted_at = %s\n", originalPath, deletedAt.Format(time.RFC3339Nano))
	if _, err := io.WriteString(w, content); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

// remoteTrashItem mirrors fsops.TrashItem for one connection's own
// remote trash — slug is the on-disk name under both files/ and info/
// (see moveToTrashRemote), everything relativeDest/Path below need to
// find either half of an item again.
type remoteTrashItem struct {
	slug         string
	OriginalPath string
	DeletedAt    time.Time
}

// Path is item's own payload location inside client's remote trash —
// remoteTrashItem's own counterpart to fsops.TrashItem.Path.
func (item remoteTrashItem) Path(client remotefs.Client) string {
	return path.Join(remoteTrashFilesDir(client), item.slug)
}

func (item remoteTrashItem) infoPath(client remotefs.Client) string {
	return path.Join(remoteTrashInfoDir(client), item.slug+".trashinfo")
}

// listRemoteTrash mirrors fsops.ListTrash: every well-formed sidecar
// whose own payload still exists under files/, oldest first. A
// sidecar with no matching payload, or one that fails to parse, is
// dropped rather than reported as an error, and its own stale file
// removed — the same self-healing ListTrash's own doc comment
// explains for the identical local case.
func listRemoteTrash(client remotefs.Client) ([]remoteTrashItem, error) {
	if err := ensureRemoteTrashSkeleton(client); err != nil {
		return nil, err
	}

	entries, err := client.ListDir(remoteTrashInfoDir(client))
	if err != nil {
		return nil, err
	}

	const suffix = ".trashinfo"
	var items []remoteTrashItem
	for _, e := range entries {
		if e.Type == fsops.TypeDir || !strings.HasSuffix(e.Name, suffix) {
			continue
		}
		slug := strings.TrimSuffix(e.Name, suffix)
		infoPath := path.Join(remoteTrashInfoDir(client), e.Name)

		if _, err := client.Lstat(path.Join(remoteTrashFilesDir(client), slug)); err != nil {
			_ = client.Remove(infoPath) // payload gone — so is this record
			continue
		}

		originalPath, deletedAt, ok := readRemoteTrashInfo(client, infoPath)
		if !ok {
			_ = client.Remove(infoPath) // malformed record, same self-healing as above
			continue
		}
		items = append(items, remoteTrashItem{slug: slug, OriginalPath: originalPath, DeletedAt: deletedAt})
	}

	sort.Slice(items, func(i, j int) bool { return items[i].DeletedAt.Before(items[j].DeletedAt) })
	return items, nil
}

func readRemoteTrashInfo(client remotefs.Client, infoPath string) (originalPath string, deletedAt time.Time, ok bool) {
	f, err := client.Open(infoPath)
	if err != nil {
		return "", time.Time{}, false
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		return "", time.Time{}, false
	}
	return parseRemoteTrashInfo(string(data))
}

// parseRemoteTrashInfo is writeRemoteTrashInfo's own inverse — a tiny,
// self-contained "key = value" reader rather than reusing
// internal/config's own parser, which reads from a local path, not
// bytes already fetched over a connection.
func parseRemoteTrashInfo(data string) (originalPath string, deletedAt time.Time, ok bool) {
	values := map[string]string{}
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	deletedAt, err := time.Parse(time.RFC3339Nano, values["deleted_at"])
	if err != nil || values["path"] == "" {
		return "", time.Time{}, false
	}
	return values["path"], deletedAt, true
}

// restoreFromRemoteTrash moves item back to its own OriginalPath and
// removes its sidecar. Baseline behavior only: refuses rather than
// overwriting if something already occupies OriginalPath, or if its
// parent directory no longer exists — see this file's own package doc
// comment on why a remote Restore doesn't attempt local Restore's own
// richer conflict handling.
func restoreFromRemoteTrash(client remotefs.Client, item remoteTrashItem) error {
	if err := client.Rename(item.Path(client), item.OriginalPath); err != nil {
		return err
	}
	return client.Remove(item.infoPath(client))
}

// emptyRemoteTrash permanently deletes every item currently in
// client's own remote trash, and its own sidecars — Empty Trash's
// remote counterpart. Collects every problem rather than stopping at
// the first, this project's own established convention for a
// multi-item action (see e.g. archive.Extract's own doc comment).
func emptyRemoteTrash(client remotefs.Client) error {
	items, err := listRemoteTrash(client)
	if err != nil {
		return err
	}
	var errs []error
	for _, item := range items {
		payloadPath := item.Path(client)
		entry, statErr := client.Lstat(payloadPath)
		isDir := statErr == nil && entry.Type == fsops.TypeDir
		if err := removeRemoteRecursive(client, payloadPath, isDir); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := client.Remove(item.infoPath(client)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// describeRemoteTrashRows is describeTrashRows' own remote half (see
// its own doc comment on the panel-vs-r.panel distinction that makes
// this a package-level function taking client explicitly, rather than
// a Root method reaching for r.panel.remote itself).
func describeRemoteTrashRows(client remotefs.Client, dir string) (map[string]rowDescription, bool) {
	if path.Clean(dir) != path.Clean(remoteTrashFilesDir(client)) {
		return nil, false
	}

	items, err := listRemoteTrash(client)
	if err != nil {
		return nil, true // still the trash — just couldn't read its own contents
	}

	descriptions := make(map[string]rowDescription, len(items))
	for _, item := range items {
		descriptions[item.Path(client)] = rowDescription{name: item.OriginalPath, modTime: item.DeletedAt}
	}
	return descriptions, true
}
