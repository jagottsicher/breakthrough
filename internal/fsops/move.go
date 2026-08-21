//go:build unix

package fsops

import (
	"errors"
	"os"
	"syscall"
)

// Move moves src to dst, refusing to overwrite an existing dst unless
// force is true (the same contract Copy and Rename use).
//
// It tries os.Rename first, which is atomic and cheap but only works
// within a single filesystem. If that fails with EXDEV ("cross-device
// link" — src and dst are on different filesystems, e.g. two different
// mounts), it falls back to Copy followed by removing src, since
// os.Rename can never bridge that gap directly (see CLAUDE.md's
// architecture notes on this). Any other os.Rename failure is returned
// as-is — there's no reason to believe copy+delete would fare any better.
//
// The fallback's Copy call always runs with force=true: the overwrite
// decision was already made above (or by os.Rename, which itself
// overwrites an existing dst atomically once permitted to proceed), so
// there is nothing left to ask about by the time the EXDEV fallback is
// reached.
func Move(src, dst string, force bool) error {
	if !force {
		if err := refuseExisting(dst); err != nil {
			return err
		}
	}

	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	var linkErr *os.LinkError
	if !errors.As(err, &linkErr) || !errors.Is(linkErr.Err, syscall.EXDEV) {
		return err
	}

	if err := Copy(src, dst, true); err != nil {
		return err
	}
	// The copy succeeded, so src's data is safely at dst; if removing the
	// original fails, that leaves a harmless duplicate behind rather than
	// losing anything — safer than the alternative of removing src first.
	return os.RemoveAll(src)
}
