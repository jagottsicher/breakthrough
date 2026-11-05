package logdiskcache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ValidateNewRoot checks whether newRoot is safe to relocate a cache
// to, without actually moving anything — the Options screen's own
// "Change disk cache location" action calls this before even asking
// the user to confirm, so a bad path is caught immediately rather
// than after a possibly-long copy has already started. Creates
// newRoot if it doesn't exist yet (so "a brand new, empty directory
// on a bigger disk" — the whole point of this feature — always
// succeeds), otherwise applies the exact same isSafeRoot rule Open's
// own fresh-start fallback does: empty, this cache's own manifest, or
// this cache's own identity subdirectories, nothing else. One rule,
// not two that could drift apart.
func ValidateNewRoot(newRoot string) error {
	if err := os.MkdirAll(newRoot, dirMode); err != nil {
		return fmt.Errorf("logdiskcache: creating %s: %w", newRoot, err)
	}
	safe, err := isSafeRoot(newRoot)
	if err != nil {
		return fmt.Errorf("logdiskcache: checking %s: %w", newRoot, err)
	}
	if !safe {
		return fmt.Errorf("logdiskcache: %s already contains files that don't look like a breakthrough disk cache — refusing to use it", newRoot)
	}
	return nil
}

// Move relocates this cache's entire on-disk root to newRoot — the
// Options screen's own "Change disk cache location" action, called
// only after ValidateNewRoot and a user confirmation. Old root stays
// authoritative (untouched, fully readable) until the very last
// step: a same-filesystem os.Rename succeeds or fails atomically with
// nothing in between, and the EXDEV copy fallback below only removes
// the old root after copyDir has already finished copying everything
// into newRoot — a cancellation or error during that copy leaves
// newRoot's own partial copy removed and oldRoot exactly as it was,
// never a state where neither root has everything.
//
// Held for the whole call: nothing else may read or write through
// this *Cache while a move is in progress. That's correct, not
// accidental serialization — callers are expected to additionally
// refuse to even start a move while a progressive load might be
// writing through this same Cache (see internal/ui's own
// startLogAuditDiskCacheMove), since blocking that load for however
// long a multi-gigabyte copy takes would be a bad experience even
// though it wouldn't corrupt anything.
func (c *Cache) Move(ctx context.Context, newRoot string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	oldAbs, err := filepath.Abs(c.root)
	if err != nil {
		return fmt.Errorf("logdiskcache: resolving current root: %w", err)
	}
	newAbs, err := filepath.Abs(newRoot)
	if err != nil {
		return fmt.Errorf("logdiskcache: resolving new root: %w", err)
	}
	if oldAbs == newAbs {
		return nil
	}
	if rel, err := filepath.Rel(oldAbs, newAbs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("logdiskcache: new location must not be inside the current cache root")
	}

	if err := ValidateNewRoot(newRoot); err != nil {
		return err
	}
	// ValidateNewRoot already confirmed newRoot is empty or
	// orphaned-cache-only — removing it lets Rename target a
	// genuinely non-existent path (simpler than relying on POSIX's
	// own "rename onto an empty directory" allowance, which not every
	// platform/filesystem combination honors identically) and gives
	// copyDir below a single, consistent "create it fresh" contract
	// either way.
	if err := os.RemoveAll(newRoot); err != nil {
		return fmt.Errorf("logdiskcache: clearing %s before moving into it: %w", newRoot, err)
	}

	oldRoot := c.root
	if err := os.Rename(oldRoot, newRoot); err == nil {
		c.root = newRoot
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return fmt.Errorf("logdiskcache: moving cache: %w", err)
	}

	// EXDEV: oldRoot and newRoot are on different filesystems, so the
	// whole-directory rename above can never work no matter how many
	// times it's retried — copy every byte across instead. oldRoot is
	// still the only complete copy until copyDir returns nil.
	if err := copyDir(ctx, oldRoot, newRoot); err != nil {
		_ = os.RemoveAll(newRoot) // partial copy — oldRoot is untouched and still authoritative
		return err
	}
	// Point of no return: newRoot is now a complete, verified copy of
	// everything oldRoot had. A failure removing oldRoot from here on
	// is wasted disk, never data loss — still switch over to newRoot.
	c.root = newRoot
	if err := os.RemoveAll(oldRoot); err != nil {
		return fmt.Errorf("logdiskcache: moved to %s, but could not remove the old location %s: %w", newRoot, oldRoot, err)
	}
	return nil
}

// copyDir recursively copies every file and directory under src into
// dst, preserving each entry's own mode (so the 0700/0600 permissions
// slice 1 established for this cache's own content survive the EXDEV
// fallback, not whatever the destination filesystem's default umask
// would otherwise produce). Checked for ctx cancellation once per
// file (not mid-file) — the same per-file, not sub-file, granularity
// this app's own existing background loads already use (see
// internal/ui's startLogAuditProgressiveLoad), simple and sufficient
// since individual cached files are at most a few hundred megabytes,
// not the whole budget at once.
//
// A separate, directly callable function (not inlined into Move)
// specifically so a test can exercise the actual byte-copying and
// cancellation behavior without needing two real filesystems to force
// the EXDEV path Move itself only reaches that way — see this
// package's own tests for the resulting split: Move's own EXDEV
// trigger is untested (nothing short of two filesystems can force it
// in a unit test), but what it does once triggered is fully covered
// here. No progress reporting — the one real caller (internal/ui's
// startLogAuditDiskCacheMove) doesn't show byte-level progress for
// this action, and threading a callback nothing uses yet through
// every recursive call would be speculative, not something anything
// actually needs right now.
func copyDir(ctx context.Context, src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("logdiskcache: statting %s: %w", src, err)
	}
	if err := os.MkdirAll(dst, srcInfo.Mode().Perm()); err != nil {
		return fmt.Errorf("logdiskcache: creating %s: %w", dst, err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("logdiskcache: reading %s: %w", src, err)
	}
	for _, e := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(ctx, srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		if _, err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies one regular file's own bytes and mode — a plain
// io.Copy, not os.Link/os.Rename, since the whole reason copyDir is
// reached at all is that src and dst are on different filesystems
// (EXDEV), where a hard link is exactly as impossible as the rename
// that got us here.
func copyFile(src, dst string) (int64, error) {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return 0, fmt.Errorf("logdiskcache: statting %s: %w", src, err)
	}
	in, err := os.Open(src)
	if err != nil {
		return 0, fmt.Errorf("logdiskcache: opening %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, srcInfo.Mode().Perm())
	if err != nil {
		return 0, fmt.Errorf("logdiskcache: creating %s: %w", dst, err)
	}
	n, err := io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		return n, fmt.Errorf("logdiskcache: copying %s to %s: %w", src, dst, err)
	}
	if closeErr != nil {
		return n, fmt.Errorf("logdiskcache: closing %s: %w", dst, closeErr)
	}
	return n, nil
}
