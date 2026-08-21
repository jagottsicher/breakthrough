package fsops

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Copy copies src (a file, a directory recursively, or a symlink, which is
// recreated pointing at the same target rather than followed) to dst. It
// refuses to overwrite an existing dst unless force is true — the same
// contract Rename uses, so the caller (a confirmation dialog) decides
// about overwrites explicitly rather than this silently clobbering
// something.
//
// For a directory whose dst already exists (only possible with
// force=true), Copy merges into it rather than replacing it wholesale:
// MkdirAll on an existing directory is a no-op, and each child underneath
// is then copied with the same force setting, overwriting same-named
// files as it goes. That single "force" decision, made once by the
// caller, is treated as covering everything nested under src — Copy
// doesn't ask again per file.
func Copy(src, dst string, force bool) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}

	if !force {
		if err := refuseExisting(dst); err != nil {
			return err
		}
	}

	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		return copySymlink(src, dst)
	case fi.IsDir():
		return copyDir(src, dst, fi.Mode(), force)
	default:
		return copyFile(src, dst, fi.Mode(), force)
	}
}

// refuseExisting errors if dst already exists (as anything — file,
// directory, symlink, ...), the shared "don't overwrite without asking"
// check Copy, Move, and Rename all use.
func refuseExisting(dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("fsops: %s already exists", dst)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// copyFile copies one regular file's content and permission bits. If dst
// already exists (only reached with force=true — the caller already
// checked otherwise), it's removed first so the copy starts clean rather
// than potentially leaving stale bytes behind a shorter new file.
func copyFile(src, dst string, mode os.FileMode, force bool) error {
	if force {
		if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }() // read-only; nothing to do if this fails

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return err
	}
	// The explicit Close below is what actually reports a write/flush
	// failure; this deferred one is only a safety net for the error paths
	// above it, so its own result is deliberately discarded rather than
	// shadowing whichever error got there first.
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// copyDir recursively copies a directory's contents into dst, creating
// dst itself (or reusing it, if force allowed proceeding with an existing
// one — see Copy's doc comment on merge semantics).
func copyDir(src, dst string, mode os.FileMode, force bool) error {
	if err := os.MkdirAll(dst, mode.Perm()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		childSrc := filepath.Join(src, entry.Name())
		childDst := filepath.Join(dst, entry.Name())

		fi, err := os.Lstat(childSrc)
		if err != nil {
			return err
		}

		if !force {
			if err := refuseExisting(childDst); err != nil {
				return err
			}
		}

		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			err = copySymlink(childSrc, childDst)
		case fi.IsDir():
			err = copyDir(childSrc, childDst, fi.Mode(), force)
		default:
			err = copyFile(childSrc, childDst, fi.Mode(), force)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// copySymlink recreates src's link (whatever it points at, even if that
// target doesn't exist or lives outside src's own tree) at dst, rather
// than copying the file it points to.
func copySymlink(src, dst string) error {
	target, err := os.Readlink(src)
	if err != nil {
		return err
	}
	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Symlink(target, dst)
}
