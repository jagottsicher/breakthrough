package fsops

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// OverwriteMode selects what Copy/Move do about a directory whose dst
// already exists, once Force allows proceeding with the overwrite at
// all — irrelevant for a plain file or symlink dst, which only ever has
// one sensible meaning for "overwrite" (replace its content/target
// entirely) regardless of this.
type OverwriteMode int

const (
	// MergeInto keeps whatever's already at dst that src doesn't also
	// have, copying src's own tree over it and only replacing entries
	// both sides share by name — everything else already there
	// survives untouched. The right choice for "add these files into
	// an existing folder" (installing new files alongside a project's
	// existing ones, say). This was Copy's only behavior before
	// OverwriteMode existed, and remains available as an explicit
	// choice rather than the implicit, only one.
	MergeInto OverwriteMode = iota
	// ReplaceEntirely removes dst first (if it exists at all), so the
	// result is *exactly* src's own tree afterward — nothing left over
	// from whatever was there before. The right choice whenever
	// "overwrite" needs to mean "make this identical to the source",
	// not "patch it": replacing a WordPress install's own core
	// directory from a known-clean copy, say, is worthless if a
	// leftover attacker-planted file a merge never touches survives
	// sitting right there afterward regardless — a real, user-reported
	// concern, not a hypothetical one.
	ReplaceEntirely
)

// CopyOptions configures a single Copy call. Introduced once a fourth
// positional parameter was about to be added on top of dst/force —
// this project's own convention past that point, rather than
// accumulating further same-typed bool/enum arguments a call site could
// silently transpose. The zero value (Force false, Mode MergeInto,
// FollowSymlinks false, OnFile nil) is Copy's own original behavior
// exactly, from before CopyOptions existed at all: refuse an existing
// dst, and recreate a symlink as a symlink rather than copying whatever
// it points at.
type CopyOptions struct {
	// Force allows overwriting an existing dst — refused outright
	// otherwise (see refuseExisting), the same contract Rename uses.
	Force bool
	// Mode selects MergeInto vs ReplaceEntirely once Force allows
	// overwriting an existing dst directory at all — see
	// OverwriteMode's own doc comment for the choice itself, and
	// Copy's for exactly when and how it's applied.
	Mode OverwriteMode
	// FollowSymlinks copies whatever a symlink resolves to instead of
	// recreating the link itself — see copySymlink's own doc comment
	// for the two behaviors in full, and why recreating the link is
	// the default. Applies throughout a directory copy, not just to
	// src itself: every symlink copyDir encounters underneath it is
	// resolved and followed the same way.
	FollowSymlinks bool
	// OnFile, if non-nil, is called with a real file or symlink's own
	// path just before that one starts being copied — for a directory,
	// once per entry found underneath it, recursively, in
	// os.ReadDir's own order; for a single file or symlink, once for
	// src itself (or, with FollowSymlinks, for whatever it resolved
	// to). Never called for a directory itself (MkdirAll is instant —
	// there's nothing to report progress on), only for the actual
	// file/symlink work, so a caller can show which file a paste
	// currently has open. Same "cheap, synchronous, no rate-limiting
	// done here" contract Hash's own onProgress already follows (see
	// its own doc comment) — a caller wanting to sample this on its
	// own schedule instead (see animatePasteProgress in internal/ui)
	// is free to.
	OnFile func(path string)
}

// Copy copies src (a file, a directory recursively, or a symlink — see
// CopyOptions.FollowSymlinks for the one thing that changes how a
// symlink specifically is handled) to dst. It refuses to overwrite an
// existing dst unless opts.Force is true — the same contract Rename
// uses, so the caller (a confirmation dialog) decides about overwrites
// explicitly rather than this silently clobbering something.
//
// opts.Mode only matters once Force allows an existing dst directory to
// be overwritten at all (see OverwriteMode's own doc comment on the two
// choices) — meaningless, and ignored, for anything else. Its
// ReplaceEntirely case is handled once, right here, before any
// recursion starts: os.RemoveAll on dst, then the same MergeInto-style
// walk below proceeds into what's now empty space, so nothing in
// copyDir/copyFile themselves needs to know which mode is in effect —
// there's nothing left at any nested path for them to collide with
// either way once the top-level directory is already gone.
func Copy(src, dst string, opts CopyOptions) error {
	if Overlaps(src, dst) {
		return fmt.Errorf("fsops: %s and %s are the same, or one is inside the other — refusing to copy", src, dst)
	}

	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}

	if !opts.Force {
		if err := refuseExisting(dst); err != nil {
			return err
		}
	} else if opts.Mode == ReplaceEntirely && fi.IsDir() {
		if err := os.RemoveAll(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		return copySymlink(src, dst, opts)
	case fi.IsDir():
		return copyDir(src, dst, fi.Mode(), opts)
	default:
		return copyFile(src, dst, fi.Mode(), opts)
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

// Overlaps reports whether dst is src itself, or lives somewhere inside
// src's own directory tree — copying or moving src there would mean
// writing into (or over) the very thing being read from. Checked once,
// at Copy/Move's own entry point, not per recursive step: dst is always
// built as src's own path plus a suffix as copyDir recurses, so once
// the top-level pair is confirmed not to overlap, no path derived from
// it that way ever can either. copySymlink's own FollowSymlinks branch
// checks this a second time, against the resolved target rather than
// the link's own path — a symlink pointing into dst's own tree
// overlaps only once actually followed, not as the bare link itself.
//
// Two real failure modes this exists to catch, both found live rather
// than assumed, neither ever reaching a caller's own confirmation
// dialog first — by the time anything asks "overwrite?", the damage
// described below already happened:
//
//   - dst == src exactly (pasting into the very directory an item is
//     already in): copyFile's own force path removes dst — which here
//     is the only copy there ever was — before it can open src to
//     recreate it, permanently losing the file for nothing.
//   - dst somewhere inside src (copying a directory into one of its
//     own subdirectories): copyDir's own MkdirAll creates that
//     destination as a new, empty entry physically underneath src
//     itself, which a still-pending recursive call over that same
//     subtree then discovers as if it were original content and copies
//     again — recursing without any built-in bound, exactly the
//     "cp: cannot copy a directory into itself" case cp(1) itself
//     refuses outright rather than ever attempting.
//
// filepath.Rel resolves both to the same relationship regardless of
// trailing slashes, "." components, or which one is given relative —
// only somewhere strictly outside src's own tree ever needs to walk
// back "up" out of it first, so that's the one shape excluded here:
// "." (dst is src) and anything not starting with ".." (dst is a
// descendant) both count as overlapping; anything starting with ".."
// (dst is elsewhere, including src's own parent) does not.
func Overlaps(src, dst string) bool {
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return false // can't resolve either — let the real operation surface whatever's actually wrong with the path instead
	}
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absSrc, absDst)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

// copyFile copies one regular file's content and permission bits. If dst
// already exists (only reached with opts.Force — the caller already
// checked otherwise), it's removed first so the copy starts clean rather
// than potentially leaving stale bytes behind a shorter new file.
func copyFile(src, dst string, mode os.FileMode, opts CopyOptions) error {
	if opts.OnFile != nil {
		opts.OnFile(src)
	}
	if opts.Force {
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
// dst itself (or reusing it, if opts.Force allowed proceeding with an
// existing one — see Copy's own doc comment on merge semantics).
func copyDir(src, dst string, mode os.FileMode, opts CopyOptions) error {
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

		if !opts.Force {
			if err := refuseExisting(childDst); err != nil {
				return err
			}
		}

		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			err = copySymlink(childSrc, childDst, opts)
		case fi.IsDir():
			err = copyDir(childSrc, childDst, fi.Mode(), opts)
		default:
			err = copyFile(childSrc, childDst, fi.Mode(), opts)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// copySymlink handles one symlink src according to opts.FollowSymlinks:
//
//   - false (the default): recreates src's own link — whatever it
//     points at, even if that target doesn't exist or lives outside
//     src's own tree — at dst, rather than touching the file it points
//     to at all. Matches cp(1)'s own default for a recursive copy, and
//     what this function has always done, from before FollowSymlinks
//     existed as a choice at all.
//   - true: dereferences src instead, copying whatever it actually
//     resolves to (a file or a directory, recursively, via
//     copyFile/copyDir) as if src had named that path directly — dst
//     ends up a real file or directory of its own, not a link. Matches
//     cp(1)'s own -L/--dereference. filepath.EvalSymlinks resolves the
//     whole chain at once, so this is never reached again for whatever
//     it finds (a further symlink from a symlink target is not
//     possible on the result of EvalSymlinks) — but a directory found
//     this way is still walked by the ordinary copyDir above with the
//     same opts, so a further symlink found *underneath* it is still
//     resolved and followed the same way, all the way down.
//
// A broken link (EvalSymlinks failing to resolve it) is a real error
// with FollowSymlinks — there is nothing to copy — rather than quietly
// falling back to recreating the link instead.
func copySymlink(src, dst string, opts CopyOptions) error {
	if opts.OnFile != nil {
		opts.OnFile(src)
	}

	if !opts.FollowSymlinks {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return os.Symlink(target, dst)
	}

	resolved, err := filepath.EvalSymlinks(src)
	if err != nil {
		return err
	}
	// See Overlaps' own doc comment: src itself (the link) was already
	// checked against dst by Copy's own entry point, but resolved is a
	// different path that check never saw — a symlink whose own target
	// lives inside dst's own tree only actually overlaps once followed.
	if Overlaps(resolved, dst) {
		return fmt.Errorf("fsops: %s (via %s) and %s are the same, or one is inside the other — refusing to copy", resolved, src, dst)
	}

	fi, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return copyDir(resolved, dst, fi.Mode(), opts)
	}
	return copyFile(resolved, dst, fi.Mode(), opts)
}
