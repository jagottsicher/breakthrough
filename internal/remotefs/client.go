package remotefs

import (
	"fmt"
	"io"
	"os"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// Client is one open connection to a remote directory tree. All paths
// are POSIX-absolute against the remote filesystem, exactly like a
// local fsops call — a Client never resolves anything against the
// local machine's own filesystem.
type Client interface {
	// Root is where a freshly opened Client starts browsing — the
	// remote account's own home directory for SFTP, chosen once at
	// Dial time and fixed for the Client's whole lifetime.
	Root() string

	// ListDir lists one directory's immediate children, Lstat'd (not
	// resolved) the same way fsops.ListDir's own local entries are —
	// see this package's own doc comment for which fsops.Entry fields
	// a remote listing can't populate and what they default to instead.
	ListDir(path string) ([]fsops.Entry, error)

	// Stat describes a single path, following a symlink to its target
	// exactly like fsops.Stat's own local, non-Lstat contract.
	Stat(path string) (fsops.Entry, error)

	// Lstat describes a single path *without* following a symlink —
	// the same Lstat-not-Stat distinction this project's own local
	// delete/copy code already depends on to correctly treat a symlink
	// as itself, never silently recurse into whatever directory it
	// happens to point at (see fsops.PurgeCompletely's own os.Lstat
	// call for the local equivalent of exactly this).
	Lstat(path string) (fsops.Entry, error)

	// Open returns a streaming reader over a remote file's content —
	// viewing, previewing, or copying it to the local machine all read
	// through this same call.
	Open(path string) (io.ReadCloser, error)

	// Create truncates (or creates) path and returns a streaming
	// writer over its content — uploading a local file writes through
	// this same call.
	Create(path string) (io.WriteCloser, error)

	// Mkdir creates one new directory; the parent must already exist,
	// the same one-level-at-a-time contract os.Mkdir itself has.
	Mkdir(path string) error

	// Remove deletes a single file. RemoveDirectory deletes a single,
	// already-empty directory. Kept separate, rather than one call
	// that guesses from a cached Entry, because the two map to
	// genuinely different SFTP wire requests (SSH_FXP_REMOVE vs
	// SSH_FXP_RMDIR) with different server-side semantics.
	Remove(path string) error
	RemoveDirectory(path string) error

	// Rename moves or renames a single path, failing outright rather
	// than silently overwriting an existing newPath — the same
	// conservative default os.Rename's own POSIX semantics don't
	// actually give you locally either, callers needing "replace" call
	// Remove first, exactly as this project's own local move/rename
	// code already does.
	Rename(oldPath, newPath string) error

	// Chmod sets a path's own permission bits — only the low 12 bits of
	// mode (the standard rwxrwxrwx + setuid/setgid/sticky bits) are
	// meaningful over SFTP; any other bits in mode are ignored the same
	// way os.Chmod's own doc comment already says they are locally.
	Chmod(path string, mode os.FileMode) error

	// Close ends the underlying connection. Safe to call more than
	// once; a Client is unusable afterward.
	Close() error
}

// ConnectionRefusedError wraps a Dial failure that occurred before any
// authentication was attempted at all (DNS/TCP failure, port closed,
// ...) — distinguished from an authentication or host-key failure so a
// caller can phrase the error shown to the user more usefully than a
// bare wrapped net.OpError would on its own.
type ConnectionRefusedError struct {
	Addr string
	Err  error
}

func (e *ConnectionRefusedError) Error() string {
	return fmt.Sprintf("could not reach %s: %v", e.Addr, e.Err)
}

func (e *ConnectionRefusedError) Unwrap() error { return e.Err }
