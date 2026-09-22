package fsops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CreateFile creates a new, empty file called name inside dir — the
// local counterpart to a bare `touch` on a name that doesn't exist yet.
// Refuses an empty name, a name containing a path separator (this only
// ever creates directly inside dir, the same one-level restriction
// Rename's own newName already has), and an existing destination —
// O_EXCL makes that check atomic against a concurrent creator, rather
// than a separate Lstat that could race with one. On success it returns
// the new file's full path.
func CreateFile(dir, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("fsops: name must not be empty")
	}
	if strings.ContainsRune(name, os.PathSeparator) {
		return "", fmt.Errorf("fsops: name must not contain a path separator: %q", name)
	}

	dest := filepath.Join(dir, name)
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("fsops: %s already exists", dest)
		}
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return dest, nil
}

// CreateDir creates a new, empty directory called name inside dir — the
// local counterpart to a bare `mkdir` on a name that doesn't exist yet.
// Same refusals as CreateFile (empty name, a path separator in name, an
// existing destination); on success it returns the new directory's full
// path.
func CreateDir(dir, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("fsops: name must not be empty")
	}
	if strings.ContainsRune(name, os.PathSeparator) {
		return "", fmt.Errorf("fsops: name must not contain a path separator: %q", name)
	}

	dest := filepath.Join(dir, name)
	if err := os.Mkdir(dest, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("fsops: %s already exists", dest)
		}
		return "", err
	}
	return dest, nil
}
