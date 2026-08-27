package viewer

import (
	"fmt"
	"io"
	"os"
)

// DefaultPreviewLimit is how much of a file's content Look reads by
// default (see Load) — generous enough for the overwhelming majority of
// source files, config files, and diffs to show in full, while still
// keeping a multi-gigabyte log file (a real, common case for this app's
// own POSIX-focused audience — see CLAUDE.md) from being pulled entirely
// into memory just to preview it. A file larger than this shows its own
// first DefaultPreviewLimit bytes plus a truncation notice (see Load) —
// internal/ui's separate "Tail -f" action (see runTailFollow) is the way
// to watch a growing log live instead.
const DefaultPreviewLimit = 8 << 20 // 8 MiB

// ReadPreview reads up to limit bytes of path's content — never more,
// regardless of the file's real size — plus one extra probe byte used
// only to decide truncated, which is stripped from data before it's
// returned. An error opening or reading path (it doesn't exist, a
// permission error, path is a directory) is returned as-is, ready to
// show through internal/ui's showError the same way any other
// filesystem-facing error in this app does.
func ReadPreview(path string, limit int64) (data []byte, truncated bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	// Discarded explicitly: this file is only ever read from, never
	// written to, so a Close failure has nothing to report that the read
	// itself wouldn't already have surfaced — unlike config.SetKey's own
	// deferred Close, which does propagate one because it writes.
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	if info.IsDir() {
		return nil, false, fmt.Errorf("%s: is a directory", path)
	}

	buf, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(buf)) > limit {
		return buf[:limit], true, nil
	}
	return buf, false, nil
}
