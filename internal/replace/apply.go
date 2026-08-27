package replace

import (
	"bytes"
	"os"
	"path/filepath"
)

// FileChange is one file Preview found sed would actually alter.
type FileChange struct {
	Path   string
	Before []byte
	After  []byte
}

// binarySniffLen mirrors grep -I's own general approach (used throughout
// internal/search) of only looking at a leading chunk, not the whole
// file, to decide whether something looks like binary content.
const binarySniffLen = 8000

// looksBinary reports whether data contains a NUL byte in its first
// binarySniffLen bytes — the same heuristic grep -I uses to decide what
// counts as text. Running sed against a real binary file could corrupt
// it unpredictably, so Preview skips anything this reports true for
// entirely rather than attempting it.
func looksBinary(data []byte) bool {
	n := len(data)
	if n > binarySniffLen {
		n = binarySniffLen
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}

// Preview runs script (see BuildScript) against each of paths and
// returns one FileChange per file sed actually altered — a directory, an
// unreadable path, or a file that looks binary is left out of changes
// and instead recorded (by path) in skipped, so the caller can still
// tell the user about those rather than have them silently vanish from
// the review list; a file sed left byte-for-byte unchanged is left out
// of both, the same way a content search only reports real matches.
//
// A bad sed script (e.g. a syntax error) fails identically for every
// file, so the first such failure stops Preview immediately (returned as
// err) rather than repeating the same message once per file.
func Preview(paths []string, script string, extendedRegex bool) (changes []FileChange, skipped map[string]string, err error) {
	skipped = map[string]string{}

	for _, path := range paths {
		fi, statErr := os.Lstat(path)
		if statErr != nil {
			skipped[path] = statErr.Error()
			continue
		}
		if fi.IsDir() {
			skipped[path] = "is a directory"
			continue
		}

		before, readErr := os.ReadFile(path)
		if readErr != nil {
			skipped[path] = readErr.Error()
			continue
		}
		if looksBinary(before) {
			skipped[path] = "looks like a binary file"
			continue
		}

		after, sedErr := RunSed(script, extendedRegex, before)
		if sedErr != nil {
			return nil, nil, sedErr
		}

		if !bytes.Equal(before, after) {
			changes = append(changes, FileChange{Path: path, Before: before, After: after})
		}
	}

	return changes, skipped, nil
}

// Apply writes each change's After back to its Path, atomically (temp
// file plus rename in the same directory, preserving the original
// file's mode — the same pattern internal/config's SetKey already uses
// for settings files). If backup is true, the original content is
// written to Path+".bak" first; a failure to write that backup aborts
// that one file's apply entirely rather than silently proceeding without
// the safety net the caller asked for.
//
// Keeps going past one file's failure rather than stopping the whole
// batch, so one stuck/permission-denied file doesn't block the rest;
// returns how many files were actually written and the first error
// encountered, if any.
func Apply(changes []FileChange, backup bool) (applied int, err error) {
	var firstErr error
	for _, c := range changes {
		if applyErr := applyOne(c, backup); applyErr != nil {
			if firstErr == nil {
				firstErr = applyErr
			}
			continue
		}
		applied++
	}
	return applied, firstErr
}

func applyOne(c FileChange, backup bool) error {
	fi, err := os.Stat(c.Path)
	if err != nil {
		return err
	}

	if backup {
		if err := os.WriteFile(c.Path+".bak", c.Before, fi.Mode()); err != nil {
			return err
		}
	}

	dir := filepath.Dir(c.Path)
	tmp, err := os.CreateTemp(dir, ".breakthrough-sed-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(c.After); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, fi.Mode()); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, c.Path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
