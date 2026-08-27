// Package session manages breakthrough's per-run session identity and the
// trash path derived from it. A session ID is generated once per process
// (not just the PID, which the OS recycles) and used to build a
// session-scoped trash location such as
// $XDG_RUNTIME_DIR/breakthrough/trash/<user>/<session-id>/, the default
// for a trash that does not survive past the session — see TrashDir for
// the persistent alternative.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"
)

var (
	idOnce sync.Once
	id     string
)

// ID returns this process's session identifier: eight random hex
// characters, generated the first time ID is called and cached for the
// rest of the process's lifetime. Deliberately not the PID — PIDs are
// recycled by the OS, which would let an unrelated later process collide
// with an old session's still-present trash directory.
func ID() string {
	idOnce.Do(func() {
		var buf [4]byte
		if _, err := rand.Read(buf[:]); err != nil {
			// crypto/rand's own doc: reading from it "should never fail,
			// and callers should not typically handle errors returned
			// from it" outside of extremely unusual system-level
			// failures. Falling back to a timestamp keeps breakthrough
			// running with a merely non-cryptographically-random (but
			// still per-process-unique in practice) ID, rather than
			// crashing the whole application over a trash directory name.
			id = fmt.Sprintf("t%d", time.Now().UnixNano())
			return
		}
		id = hex.EncodeToString(buf[:])
	})
	return id
}

// isRoot is a var, not a plain inline os.Geteuid() call, so tests can
// override it to exercise TrashDir's root-only behaviour without
// actually needing to run as root.
var isRoot = func() bool { return os.Geteuid() == 0 }

// username mirrors internal/ui's own currentUsername(): os/user.Current
// first, falling back to $USER if that fails (e.g. no matching /etc/passwd
// entry in some container setups).
func username() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// runtimeDir returns $XDG_RUNTIME_DIR, falling back to os.TempDir() when
// it's unset — e.g. macOS and some minimal Linux setups have no
// systemd-managed per-login runtime directory.
func runtimeDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return d
	}
	return os.TempDir()
}

// dataDir mirrors internal/config.UserDir's own XDG resolution
// ($XDG_DATA_HOME, falling back to ~/.local/share) for the persistent
// trash's own base directory — see TrashDir.
func dataDir() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "breakthrough"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "breakthrough"), nil
}

// TrashDir returns the trash directory to use right now.
//
// persistent=false (the default — see internal/config's trash_persistent
// setting) returns the session-scoped path,
// $XDG_RUNTIME_DIR/breakthrough/trash/<user>/<session-id>/. On a systemd
// system XDG_RUNTIME_DIR is tmpfs-backed and cleared on logout, so this
// disappears on its own once the session ends — nothing has to remember
// to clean it up. Falls back to a subdirectory of os.TempDir() (still
// scoped by user and session ID) where XDG_RUNTIME_DIR isn't set.
//
// persistent=true returns $XDG_DATA_HOME/breakthrough/trash/<user>/ (no
// session ID: a persistent trash is meant to accumulate and survive
// across runs, not start over each time).
//
// Running as root (euid 0 — typically via sudo) always gets the
// persistent path, regardless of the persistent argument: sudo's default
// env_reset drops XDG_RUNTIME_DIR entirely unless a sudoers config
// explicitly keeps it, and even where it is kept, it names the original
// user's runtime directory, not root's — root interactively has no real
// systemd login session of its own to tie a "session-scoped" trash to in
// the first place. Landing unpredictably in whatever os.TempDir()
// fallback that produces would be worse than just always using root's
// own well-known, predictable home directory.
func TrashDir(persistent bool) (string, error) {
	if isRoot() {
		persistent = true
	}
	u := username()
	if persistent {
		base, err := dataDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(base, "trash", u), nil
	}
	return filepath.Join(runtimeDir(), "breakthrough", "trash", u, ID()), nil
}
