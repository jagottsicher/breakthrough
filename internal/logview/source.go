package logview

import (
	"io"
	"os"
	"path/filepath"
	"time"
)

// SourceEntry is one directory entry as a FileSource reports it —
// deliberately narrower than os.DirEntry/fsops.Entry (just what
// Discover actually needs: name, whether it's a directory or a regular
// file, size and mtime for the selection screen's own columns) so this
// package never has to import fsops or remotefs to know about either
// one's own, much larger concept of a directory entry.
type SourceEntry struct {
	Name      string
	IsDir     bool
	IsRegular bool // false for a symlink, socket, device, ... — Discover skips these, same as before FileSource existed
	Size      int64
	ModTime   time.Time
}

// FileSource abstracts "list a directory, open a file" over either the
// local filesystem or an already-open remote connection — Discover/Open
// (discover.go) go through this instead of calling os.* directly, so
// the exact same logrotate-family grouping, compression handling, and
// format detection work identically regardless of which one backs it.
// Per the user's own explicit request: "jL" used to always browse the
// local filesystem even while a panel was connected to a remote
// session, when the whole point of asking for a directory on that
// session was to read through its own log files, not this machine's.
//
// Join builds a child path the way this source's own paths actually
// work — filepath.Join's OS-native separator for LocalFileSource,
// always "/" for a remote SFTP connection (see remotefs.Client's own
// doc comment: "All paths are POSIX-absolute") — so CandidateFile.Path
// is always built correctly for whichever source produced it, and
// Open (given the same source back) never has to guess.
//
// See internal/ui's own remoteLogFileSource for the real remote
// implementation, built on remotefs.Client — this package itself never
// imports remotefs, keeping local/remote concerns strictly separated
// the way every other part of this app already does.
type FileSource interface {
	ReadDir(dir string) ([]SourceEntry, error)
	Open(path string) (io.ReadCloser, error)
	Join(dir, name string) string
}

// LocalFileSource is the default FileSource, backed directly by the
// local filesystem — exactly what Discover/Open always did before
// FileSource existed.
type LocalFileSource struct{}

func (LocalFileSource) ReadDir(dir string) ([]SourceEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]SourceEntry, 0, len(entries))
	for _, e := range entries {
		var size int64
		var modTime time.Time
		if info, err := e.Info(); err == nil {
			size = info.Size()
			modTime = info.ModTime()
		}
		out = append(out, SourceEntry{
			Name:      e.Name(),
			IsDir:     e.IsDir(),
			IsRegular: e.Type().IsRegular(),
			Size:      size,
			ModTime:   modTime,
		})
	}
	return out, nil
}

func (LocalFileSource) Open(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

func (LocalFileSource) Join(dir, name string) string {
	return filepath.Join(dir, name)
}
