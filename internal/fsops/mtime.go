package fsops

import (
	"os"
	"time"
)

// SetModTime sets path's modification time to t. It also updates the
// access time to match, since os.Chtimes requires both and there's no
// portable way to read the current atime back to pass through unchanged
// — atime isn't part of fs.FileInfo, only the platform-specific stat
// structure has it, and under a different field name on Linux (Atim)
// than on Darwin/BSD (Atimespec). touch(1) does the same by default
// (without -m or -a): setting one sets both.
func SetModTime(path string, t time.Time) error {
	return os.Chtimes(path, t, t)
}
