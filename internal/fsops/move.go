//go:build unix

package fsops

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// MoveOptions configures a single Move call — Copy's own CopyOptions,
// minus FollowSymlinks: moving relocates whatever's actually at src,
// link or not, by design, not something left unimplemented. Deciding
// to instead move whatever a symlink points at would also have to
// decide what happens to the link itself afterward (left dangling?
// removed too?) — a genuinely different operation, not a variant of
// this one, and not something Move offers at all.
type MoveOptions struct {
	// Force allows overwriting an existing dst — refused outright
	// otherwise (see refuseExisting), the same contract Copy and
	// Rename use.
	Force bool
	// Mode selects MergeInto vs ReplaceEntirely once Force allows
	// overwriting an existing dst directory at all — see
	// OverwriteMode's own doc comment for the choice itself, and this
	// function's own doc comment for exactly when and how it applies
	// here specifically.
	Mode OverwriteMode
	// OnFile, if non-nil, is called once for src itself before
	// attempting os.Rename — see this function's own doc comment for
	// why that's the only signal available on Move's own fast path,
	// and Copy's own OnFile doc comment for the fuller contract that
	// applies once a fallback below reaches Copy instead.
	OnFile func(path string)
}

// Move moves src to dst, refusing to overwrite an existing dst unless
// opts.Force is true (the same contract Copy and Rename use).
// opts.Mode is Copy's own OverwriteMode, meaningful here for exactly
// the same reason and under exactly the same conditions as there (a
// directory dst, Force true) — see its own doc comment.
//
// It tries os.Rename first, which is atomic and cheap but only works
// within a single filesystem. If that fails with EXDEV ("cross-device
// link" — src and dst are on different filesystems, e.g. two different
// mounts), it falls back to Copy followed by removing src, since
// os.Rename can never bridge that gap directly. It also falls back the
// same way on ENOTEMPTY/EEXIST when opts.Mode is MergeInto: POSIX
// rename(2) can only ever replace an existing directory outright (it
// refuses a non-empty one entirely — Linux's own man page documents
// both errno values as possible depending on the filesystem, and this
// project's own dev filesystem was confirmed live to actually return
// EEXIST here, not ENOTEMPTY, so both are checked rather than just the
// one the name suggests) — there is no atomic, kernel-level way to
// rename "into" one instead, merging as it goes, so achieving that here
// needs
// the same file-by-file Copy pass the EXDEV case already uses. For
// ReplaceEntirely, this never triggers: dst is removed outright below,
// before os.Rename is even attempted, so it always lands on an absent
// path rather than a non-empty directory in the first place. Any other
// os.Rename failure is returned as-is — there's no reason to believe
// copy+delete would fare any better.
//
// The fallback's Copy call always runs with Force true: the overwrite
// decision was already made above (or by os.Rename, which itself
// overwrites an existing dst atomically once permitted to proceed), so
// there is nothing left to ask about by the time either fallback is
// reached.
func Move(src, dst string, opts MoveOptions) error {
	if Overlaps(src, dst) {
		// os.Rename itself already refuses the "into one of its own
		// subdirectories" shape of this outright (EINVAL — verified
		// directly against Linux's own rename(2) man page, not assumed),
		// and silently no-ops for the exact-match shape rather than
		// erroring at all — renaming a path to itself trivially succeeds,
		// since it's already exactly where it's meant to end up. Checked
		// here anyway, ahead of both: the same clear, explicit refusal
		// Copy's own doc comment gives instead of a cryptic "invalid
		// argument", and an actual error instead of a silent no-op
		// reported back as if something had genuinely happened.
		return fmt.Errorf("fsops: %s and %s are the same, or one is inside the other — refusing to move", src, dst)
	}

	if !opts.Force {
		if err := refuseExisting(dst); err != nil {
			return err
		}
	} else if opts.Mode == ReplaceEntirely {
		if fi, statErr := os.Lstat(src); statErr == nil && fi.IsDir() {
			if err := os.RemoveAll(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}

	if opts.OnFile != nil {
		opts.OnFile(src)
	}

	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	var linkErr *os.LinkError
	isRealLinkErr := errors.As(err, &linkErr)
	isExdev := isRealLinkErr && errors.Is(linkErr.Err, syscall.EXDEV)
	// Linux's own rename(2) man page documents *both* ENOTEMPTY and
	// EEXIST as possible for "newpath is a non-empty directory" —
	// verified live, not just against the docs: this project's own dev
	// filesystem returns EEXIST here, not ENOTEMPTY, so checking for
	// only one of the two would have left this fallback silently dead
	// on exactly the filesystem it was tested on.
	isNotEmpty := isRealLinkErr && (errors.Is(linkErr.Err, syscall.ENOTEMPTY) || errors.Is(linkErr.Err, syscall.EEXIST))
	if !isExdev && (!isNotEmpty || opts.Mode != MergeInto) {
		return err
	}

	copyOpts := CopyOptions{Force: true, Mode: opts.Mode, OnFile: opts.OnFile}
	if err := Copy(src, dst, copyOpts); err != nil {
		return err
	}
	// The copy succeeded, so src's data is safely at dst; if removing the
	// original fails, that leaves a harmless duplicate behind rather than
	// losing anything — safer than the alternative of removing src first.
	return os.RemoveAll(src)
}
