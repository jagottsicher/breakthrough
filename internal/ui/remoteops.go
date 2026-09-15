// Package ui's own remoteops.go holds the small, single-item remote
// operations (rename, permanent delete, chmod) that don't need the
// full async/progress/conflict machinery remotepaste.go's own transfer
// engine does — see this project's own phased rollout: Phase 1
// (browsing, viewing) shipped first; this is Phase 2, filling in
// rename/delete/chmod/copy-cut-paste for a remote panel. Still
// deliberately not covered: chown (no remote user/group database to
// resolve a typed name against — see openChown's own doc comment on
// why even the *local* text fallback only works because the local
// account database is right there), Compare, Batch rename, Sed
// Replace, Edit, and Properties as a whole (see openProperties's own
// doc comment) — each still refuses outright via isRemote's own guard,
// unchanged from Phase 1.
package ui

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

var errEmptyName = fmt.Errorf("new name must not be empty")

// renameRemote is fsops.Rename's own remote counterpart — same
// contract (refuses an empty name, a name containing "/", or an
// existing destination; returns the new full path on success), against
// a remote Client instead of the real local filesystem. Uses the
// "path" package throughout, not "path/filepath": a remote path is
// always POSIX-"/"-separated regardless of whatever OS this copy of
// breakthrough itself happens to be running on (see remotefs's own
// package doc comment).
func renameRemote(client remotefs.Client, p, newName string) (string, error) {
	if newName == "" {
		return "", errEmptyName
	}
	if strings.ContainsRune(newName, '/') {
		return "", fmt.Errorf("new name must not contain a path separator: %q", newName)
	}

	dest := path.Join(path.Dir(p), newName)
	if _, err := client.Stat(dest); err == nil {
		return "", fmt.Errorf("%s already exists", dest)
	}
	if err := client.Rename(p, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// removeRemoteRecursive deletes p — a single file/symlink/special file
// outright, or a real directory by first recursively emptying it
// (depth-first: every child before the directory itself, the same
// order a real `rm -r` removes in) — since RemoveDirectory/Remove's
// own SFTP semantics only ever delete one already-empty entry at a
// time, unlike local fsops.PurgeCompletely's own os.RemoveAll.
//
// isDir must be Lstat-true, not resolved-Stat-true: a symlink to a
// directory must always be unlinked on its own, never recursed into
// and have its target's contents deleted — the exact same
// os.Lstat(path).IsDir() distinction fsops.PurgeCompletely's own local
// implementation already makes, for the same reason. The top-level
// caller passes the row's own already-known fsops.Entry.Type == TypeDir
// (see openRemoveConfirmRemote); each recursive step passes
// child.Type == fsops.TypeDir from ListDir's own Lstat-based listing —
// deliberately not child.IsDir, which (per its own doc comment on
// fsops.Entry) is true for a directory *symlink* too, exactly the case
// this must not recurse into.
func removeRemoteRecursive(client remotefs.Client, p string, isDir bool) error {
	if !isDir {
		return client.Remove(p)
	}

	children, err := client.ListDir(p)
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := removeRemoteRecursive(client, path.Join(p, child.Name), child.Type == fsops.TypeDir); err != nil {
			return err
		}
	}
	return client.RemoveDirectory(p)
}

// chmodDirsRecursiveRemote is fsops.ChmodDirsRecursive's own remote
// counterpart: mode applies to p itself and every directory nested
// beneath it. Lstat-based via ListDir's own child.Type, so a symlink
// to a directory is left alone — the same as the local
// implementation's filepath.WalkDir, which never follows one either.
func chmodDirsRecursiveRemote(client remotefs.Client, p string, mode os.FileMode) error {
	if err := client.Chmod(p, mode); err != nil {
		return err
	}
	children, err := client.ListDir(p)
	if err != nil {
		return err
	}
	for _, child := range children {
		if child.Type != fsops.TypeDir {
			continue
		}
		if err := chmodDirsRecursiveRemote(client, path.Join(p, child.Name), mode); err != nil {
			return err
		}
	}
	return nil
}

// chmodFilesRecursiveRemote is fsops.ChmodFilesRecursive's own remote
// counterpart: mode applies to every regular file nested beneath p —
// never p itself (always a directory being recursed into, never the
// file being chmod'd), and never a symlink even one resolving to a
// regular file, matching TypeFile's own exclusion of every symlink
// type.
func chmodFilesRecursiveRemote(client remotefs.Client, p string, mode os.FileMode) error {
	children, err := client.ListDir(p)
	if err != nil {
		return err
	}
	for _, child := range children {
		full := path.Join(p, child.Name)
		switch child.Type {
		case fsops.TypeDir:
			if err := chmodFilesRecursiveRemote(client, full, mode); err != nil {
				return err
			}
		case fsops.TypeFile:
			if err := client.Chmod(full, mode); err != nil {
				return err
			}
		}
	}
	return nil
}
