package fsops

import (
	"context"
	"os"
	"path/filepath"
)

// TotalBytes sums every regular file's own size found across paths —
// each one either counted directly (a plain file) or walked
// recursively (a directory, summing everything underneath it) —
// matching exactly what a non-following Copy/Move actually streams,
// byte for byte. A symlink anywhere in the walk, including a
// top-level path that is one itself, contributes 0: recreating a link
// is a single name-and-target write, never a byte stream, the same
// reason CopyOptions.OnBytes is never called for one either (see
// copySymlink's own doc comment). Assumes FollowSymlinks isn't in
// play — there's no such option here at all, matching that paste (the
// one caller so far) doesn't currently expose one either; a future
// caller that does would need its own followed-aware variant.
//
// Best-effort, not a correctness check: a path this can't stat or list
// (permission denied, removed mid-scan, ...) simply contributes 0
// rather than aborting the whole scan or returning an error — this
// exists purely to feed an upfront progress estimate (see
// internal/ui's own paste progress bar), and a real Copy/Move of the
// same paths already surfaces the *actual* failure through its own
// ordinary error-reporting path regardless of what this saw or missed
// along the way.
//
// ctx is checked between top-level paths and, within a directory,
// between its own entries — a caller abandoning a long scan over a
// very large tree (a cancelled paste, say) gets an early, partial
// return rather than this walking something nobody will ever read the
// result of.
func TotalBytes(ctx context.Context, paths []string) int64 {
	var total int64
	for _, p := range paths {
		if ctx.Err() != nil {
			return total
		}
		total += totalBytesOne(ctx, p)
	}
	return total
}

func totalBytesOne(ctx context.Context, path string) int64 {
	fi, err := os.Lstat(path)
	if err != nil {
		return 0
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		return 0
	case fi.IsDir():
		entries, err := os.ReadDir(path)
		if err != nil {
			return 0
		}
		var total int64
		for _, entry := range entries {
			if ctx.Err() != nil {
				return total
			}
			total += totalBytesOne(ctx, filepath.Join(path, entry.Name()))
		}
		return total
	default:
		return fi.Size()
	}
}
