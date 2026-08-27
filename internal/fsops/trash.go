package fsops

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// TrashItem is one entry currently sitting in a trash directory: its
// on-disk id (the basename under files/, and the stem of its info/
// sidecar file) plus the metadata needed to restore it — see ListTrash.
type TrashItem struct {
	ID           string
	OriginalPath string
	DeletedAt    time.Time
}

// Path returns item's payload location inside trashDir — what actually
// needs to exist on disk (e.g. as a Panel row target while the trash
// directory itself is what's being browsed).
func (item TrashItem) Path(trashDir string) string {
	return filepath.Join(trashFilesDir(trashDir), item.ID)
}

// FilesDir returns the subdirectory of trashDir that actually holds
// trashed payloads (see TrashItem.Path) — what a file browser navigates
// into to browse or restore trash contents; trashDir itself only ever
// contains this and info/, never a trashed item directly.
func FilesDir(trashDir string) string { return trashFilesDir(trashDir) }

func trashFilesDir(trashDir string) string { return filepath.Join(trashDir, "files") }
func trashInfoDir(trashDir string) string  { return filepath.Join(trashDir, "info") }
func trashInfoPath(trashDir, id string) string {
	return filepath.Join(trashInfoDir(trashDir), id+".trashinfo")
}

// ensureTrashSkeleton makes sure trashDir, its files/ and info/
// subdirectories exist (mode 0700 — trash contents are as private as the
// files that were in them). Deleting the trash directory (in whole or in
// part) outside breakthrough is expected and harmless: every trash
// operation below calls this first and treats "had to recreate it"
// exactly like "it was already there and empty" — nothing here trusts
// that the directory survived since the last call.
func ensureTrashSkeleton(trashDir string) error {
	for _, dir := range []string{trashDir, trashFilesDir(trashDir), trashInfoDir(trashDir)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// randomTrashID returns eight random hex characters — the same shape and
// source (crypto/rand) as internal/session's own session ID. Good enough
// uniqueness for one trash directory's own filenames without needing a
// persistent counter; MoveToTrash still checks for (and steps around) a
// collision rather than assuming one can never happen.
func randomTrashID() (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

// MoveToTrash moves src into trashDir (see internal/session.TrashDir),
// recording its original absolute path so RestoreFromTrash can put it
// back later. Reuses Move — cross-device fallback included, the same as
// every other move in this codebase (see Move's own doc comment) — so
// trashing a file from a different filesystem than the trash directory
// itself still works, which matters here more than for a typical move:
// a session trash under $XDG_RUNTIME_DIR is very often on a different
// filesystem (tmpfs) than whatever is being trashed.
//
// No confirmation is asked here, nor by any caller — moving to the trash
// is the reversible action by design (see PurgeCompletely for the
// irreversible one, always gated by a confirmation in internal/ui). A
// directory goes in whole, recursively, exactly the way a plain move
// always has — there is nothing to warn about since nothing is actually
// being destroyed yet.
func MoveToTrash(src, trashDir string) error {
	if err := ensureTrashSkeleton(trashDir); err != nil {
		return err
	}

	absSrc, err := filepath.Abs(src)
	if err != nil {
		return err
	}

	base := filepath.Base(src)
	var id, target string
	for attempt := 0; ; attempt++ {
		id, err = randomTrashID()
		if err != nil {
			return err
		}
		target = filepath.Join(trashFilesDir(trashDir), id+"_"+base)
		if _, err := os.Lstat(target); os.IsNotExist(err) {
			break
		}
		if attempt > 100 {
			return fmt.Errorf("fsops: could not find a free trash slot for %s", src)
		}
	}
	slug := id + "_" + base

	if err := Move(src, target, false); err != nil {
		return err
	}

	// The move already succeeded at this point — if writing the sidecar
	// below fails, the file still safely exists in trash/files/, just
	// without a restorable record. That's the same trade-off ListTrash's
	// own self-healing already assumes: an info/ record with no match
	// gets dropped, and (symmetrically) a files/ entry with no matching
	// record just doesn't show up as a *restorable* item, which is far
	// safer than trying to move the source back and risking a second
	// failure compounding the first.
	values := map[string]string{
		"path": absSrc,
		// Nanosecond resolution, not just RFC3339's default seconds:
		// several files trashed via one multi-select "Move to Trash" can
		// easily land in the same wall-clock second, and ListTrash's
		// oldest-first ordering needs to actually distinguish them rather
		// than falling back to directory-read order (os.ReadDir sorts by
		// filename, which starts with a random id here — see
		// randomTrashID — and has nothing to do with deletion order).
		"deleted_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	return writeTrashInfo(trashInfoPath(trashDir, slug), values)
}

// writeTrashInfo reuses config.SetKey (atomic temp-file-plus-rename
// write, same as every other on-disk settings write in this project)
// rather than a second, trash-specific file writer — one call per key,
// which for a brand-new two-line file just means two small atomic
// rewrites in a row, a fine trade for not maintaining a parallel format.
func writeTrashInfo(path string, values map[string]string) error {
	for _, key := range [...]string{"path", "deleted_at"} {
		if err := config.SetKey(path, key, values[key]); err != nil {
			return err
		}
	}
	return nil
}

// ListTrash returns everything currently in trashDir, oldest first. An
// info/ record with no matching files/ entry, or one that fails to parse
// (missing/malformed path or deleted_at), is dropped rather than reported
// as an error and its stale sidecar file removed — an external "rm -rf"
// on part of the trash, outside breakthrough, is expected to look exactly
// like "that item just isn't there any more" the next time anything reads
// this directory (see ensureTrashSkeleton's own doc comment for the same
// principle applied to the directory itself).
func ListTrash(trashDir string) ([]TrashItem, error) {
	if err := ensureTrashSkeleton(trashDir); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(trashInfoDir(trashDir))
	if err != nil {
		return nil, err
	}

	const suffix = ".trashinfo"
	var items []TrashItem
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || len(name) <= len(suffix) || name[len(name)-len(suffix):] != suffix {
			continue
		}
		id := name[:len(name)-len(suffix)]
		infoPath := filepath.Join(trashInfoDir(trashDir), name)

		if _, err := os.Lstat(filepath.Join(trashFilesDir(trashDir), id)); err != nil {
			_ = os.Remove(infoPath) // self-heal: the payload is gone, so is this record
			continue
		}

		values, _, err := config.ParseFile(infoPath)
		if err != nil {
			continue
		}
		deletedAt, err := time.Parse(time.RFC3339Nano, values["deleted_at"])
		if err != nil || values["path"] == "" {
			_ = os.Remove(infoPath) // malformed record, same self-healing as above
			continue
		}

		items = append(items, TrashItem{ID: id, OriginalPath: values["path"], DeletedAt: deletedAt})
	}

	sort.Slice(items, func(i, j int) bool { return items[i].DeletedAt.Before(items[j].DeletedAt) })
	return items, nil
}

// RestoreFromTrash moves item back to its OriginalPath. Baseline
// behaviour only, via Move's own force=false contract: refuses rather
// than overwriting if something already occupies OriginalPath, and fails
// outright if OriginalPath's parent directory no longer exists — this
// project's own feature notes flag richer conflict handling here (rename,
// merge into an existing same-named directory, ...) as something to
// think through separately, not decided yet, so this deliberately stays
// at the safe, minimal baseline rather than guessing at that UX.
//
// Uses Move rather than a raw os.Rename for the same cross-device reason
// MoveToTrash does — a session trash under $XDG_RUNTIME_DIR restoring
// back to a real filesystem path is realistically a cross-device move as
// often as not.
func RestoreFromTrash(item TrashItem, trashDir string) error {
	if err := Move(item.Path(trashDir), item.OriginalPath, false); err != nil {
		return err
	}
	// If removing the sidecar fails, ListTrash's own self-healing (the
	// files/ entry is gone now, so this record no longer matches
	// anything) cleans it up on the next call — see its own doc comment.
	return os.Remove(trashInfoPath(trashDir, item.ID))
}

// PurgeCompletely permanently removes path: os.Remove for a file or
// symlink, os.RemoveAll for a directory tree regardless of whether it's
// empty. Bypasses the trash entirely — unrelated to whatever
// MoveToTrash has already put there. Always gated by a confirmation
// dialog in internal/ui; never called directly from a keybinding without
// one.
func PurgeCompletely(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

// EmptyTrash permanently removes everything currently in trashDir, same
// confirmation requirement as PurgeCompletely. Returns how many items
// were removed; keeps going past a single item's failure so one stuck
// file doesn't block emptying the rest, but still reports the first
// error encountered.
func EmptyTrash(trashDir string) (removed int, err error) {
	items, err := ListTrash(trashDir)
	if err != nil {
		return 0, err
	}

	var firstErr error
	for _, item := range items {
		if err := purgeTrashItem(item, trashDir); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed++
	}
	return removed, firstErr
}

func purgeTrashItem(item TrashItem, trashDir string) error {
	if err := os.RemoveAll(item.Path(trashDir)); err != nil {
		return err
	}
	return os.Remove(trashInfoPath(trashDir, item.ID))
}

// CountEntries returns how many filesystem entries exist inside path, not
// counting path itself — used to word a Remove confirmation with a real
// count ("... and 42 items inside it?") instead of a vague "and its
// contents". Returns 0, nil for a plain file/symlink or an empty
// directory.
func CountEntries(path string) (int, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	if !fi.IsDir() {
		return 0, nil
	}

	count := 0
	err = filepath.WalkDir(path, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != path {
			count++
		}
		return nil
	})
	return count, err
}
