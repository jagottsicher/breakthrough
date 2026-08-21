package fsops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Rename renames the file or directory at path to newName, keeping it in
// the same parent directory. It refuses to rename across directories
// (newName must not contain a path separator — use a future Move
// operation for that) and refuses to silently overwrite an existing
// entry at the destination; the caller (or a future confirmation dialog)
// has to decide about overwrites explicitly. On success it returns the
// new full path.
func Rename(path, newName string) (string, error) {
	if newName == "" {
		return "", fmt.Errorf("fsops: new name must not be empty")
	}
	if strings.ContainsRune(newName, os.PathSeparator) {
		return "", fmt.Errorf("fsops: new name must not contain a path separator: %q", newName)
	}

	dest := filepath.Join(filepath.Dir(path), newName)

	if _, err := os.Lstat(dest); err == nil {
		return "", fmt.Errorf("fsops: %s already exists", dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if err := os.Rename(path, dest); err != nil {
		return "", err
	}
	return dest, nil
}
