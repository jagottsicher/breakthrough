package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// Extract writes members (each one Children/List already reported —
// either a single file, or a directory to extract recursively along
// with everything nested under it) out of archivePath into destDir.
// Each member lands at destDir/<its own base name> — a file becomes
// one file there, a directory becomes a directory there containing
// everything nested under it in the archive — exactly the way copying
// a real file or directory into destDir names its own destination.
// That specific shape is deliberate, not incidental: it's what lets
// "copy marked archive entries out" (see internal/ui's own archive-
// paste wiring) reuse the very same Copy/Paste keys, clipboard, and
// destination-picker a real file copy already uses, with archive
// members standing in for real ones.
//
// Every problem extracting one member is collected rather than
// stopping at the first (this project's own established convention for
// a multi-item action — see e.g. Root's own paste-error summary) — a
// partial failure among several marked entries still extracts
// everything it could.
func Extract(archivePath string, members []Entry, destDir string) error {
	kind, ok := Classify(archivePath)
	if !ok {
		return &UnsupportedFormatError{Path: archivePath}
	}

	memberPaths := make([]string, len(members))
	var errs []error
	for i, m := range members {
		memberPaths[i] = m.Path
		if !m.IsDir {
			continue
		}
		// Pre-create every requested directory member's own destination,
		// even one with no descendants at all in the archive — an empty
		// directory has no file of its own for the loops below to ever
		// create it as a side effect of, the same reason a real
		// recursive Copy of an empty directory still creates it at the
		// destination.
		if err := ensureDir(destDir, path.Base(path.Clean(m.Path))); err != nil {
			errs = append(errs, err)
		}
	}

	var extractErr error
	switch kind {
	case KindZip:
		extractErr = extractZip(archivePath, memberPaths, destDir)
	case KindTar:
		extractErr = extractTar(archivePath, memberPaths, destDir)
	case KindSevenZip:
		extractErr = extractSevenZip(archivePath, members, destDir)
	case KindRar:
		extractErr = extractRar(archivePath, members, destDir)
	}
	if extractErr != nil {
		errs = append(errs, extractErr)
	}
	return errors.Join(errs...)
}

// resolveDest joins name (a "/"-joined path already known to come from
// inside the archive, never from anything else) onto base, refusing to
// ever land outside it — the zip-slip guard every write in this file
// needs, factored out once here rather than duplicated per format.
func resolveDest(base, name string) (string, error) {
	full := filepath.Join(base, filepath.FromSlash(name))
	rel, err := filepath.Rel(base, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s: refusing to extract outside the destination directory", name)
	}
	return full, nil
}

// destFile resolves name to its safe destination path (see
// resolveDest) and ensures that path's own parent directory exists,
// ready for an O_CREATE file write right at the returned path — what
// extractZipFile/extractTarFile both need before opening their own
// destination file.
func destFile(base, name string) (string, error) {
	full, err := resolveDest(base, name)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	return full, nil
}

// ensureDir resolves name to its safe destination path (see
// resolveDest) and creates it as a directory outright — unlike
// destFile, which only ensures a *file*'s own parent exists. Used both
// for a directory Entry's own destPath and (via relativeDest's own
// intermediate-directory results) for a zip that never stored an
// explicit entry for some intermediate directory a deeper member
// implies.
func ensureDir(base, name string) error {
	full, err := resolveDest(base, name)
	if err != nil {
		return err
	}
	return os.MkdirAll(full, 0o755)
}

// relativeDest reports where entryPath (a raw Entry.Path from the
// archive) belongs under destDir, given the set of member paths the
// caller actually asked for: entryPath itself, or entryPath found
// nested under one of members (see Extract's own doc comment on why
// the destination is named after each member's own base name, not its
// full internal path). ok is false for an entry that matches no
// requested member at all — every other member of the archive, left
// untouched.
func relativeDest(entryPath string, members []string) (rel string, ok bool) {
	for _, m := range members {
		m := path.Clean(m)
		base := path.Base(m)
		if entryPath == m {
			return base, true
		}
		if strings.HasPrefix(entryPath, m+"/") {
			return base + "/" + strings.TrimPrefix(entryPath, m+"/"), true
		}
	}
	return "", false
}

// extractZip opens archivePath once (its own central directory, the
// same as List) and writes out every entry relativeDest resolves
// against members.
func extractZip(archivePath string, members []string, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }() // read-only; nothing to do if this fails

	var errs []error
	for _, f := range r.File {
		entryPath := path.Clean(strings.TrimSuffix(f.Name, "/"))
		rel, ok := relativeDest(entryPath, members)
		if !ok {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := ensureDir(destDir, rel); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if err := extractZipFile(f, destDir, rel); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", entryPath, err))
		}
	}
	return errors.Join(errs...)
}

// extractZipFile writes one regular file member to destDir/rel,
// preserving its own stored file mode (a zip's executable bit, chiefly —
// verified against archive/zip's own FileInfo().Mode(), which decodes
// exactly that from the Unix external-attributes field zip already
// carries when the archive was made on a Unix system; falls back to a
// plain 0o644 otherwise, the same default os.Create itself would use).
func extractZipFile(f *zip.File, destDir, rel string) error {
	dest, err := destFile(destDir, rel)
	if err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }() // read-only; nothing to do if this fails

	mode := f.FileInfo().Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	out, err := createOverwriting(dest, mode)
	if err != nil {
		return err
	}
	// The explicit Close below is what actually reports a write/flush
	// failure; this deferred one is only a safety net for the copy error
	// path, so its own result is deliberately discarded rather than
	// shadowing whichever error got there first (the same convention
	// fsops.Copy's own doc comment already establishes).
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, rc); err != nil {
		return err
	}
	return out.Close()
}

// createOverwriting opens dest for writing, freshly created with mode
// — silently replacing whatever was there before, matching this
// package's own already-documented "a name already present at destDir
// is silently overwritten" contract (see Extract's own doc comment).
//
// Removes an existing dest first rather than opening with O_TRUNC on
// top of it: a member's own stored mode is often read-only (no owner
// write bit — common for a file an archive deliberately marks
// non-writable, e.g. a license or a build artifact), and open() only
// skips the requested-access check against a file's own mode when
// that file is genuinely new — reopening an *existing* file for
// O_WRONLY still has to satisfy its current permissions first. A real,
// reported bug: extracting such a member a second time onto its own
// previous copy failed with "permission denied" for exactly this
// reason, even though overwriting it at all was already this
// package's own intended behavior.
func createOverwriting(dest string, mode os.FileMode) (*os.File, error) {
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
}

// extractTar streams once through archivePath's own tar body (see
// openTarStream — the same up-front cost List already pays, since tar
// has no separate index to extract selectively from instead).
func extractTar(archivePath string, members []string, destDir string) error {
	f, closeAll, err := openTarStream(archivePath)
	if err != nil {
		return err
	}
	defer closeAll()

	var errs []error
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			errs = append(errs, err)
			break
		}
		if hdr.Typeflag != tar.TypeDir && hdr.Typeflag != tar.TypeReg {
			continue // see listTar's own doc comment on why these are the only two kinds tracked at all
		}
		entryPath := path.Clean(strings.TrimSuffix(hdr.Name, "/"))
		rel, ok := relativeDest(entryPath, members)
		if !ok {
			continue
		}
		if hdr.Typeflag == tar.TypeDir {
			if err := ensureDir(destDir, rel); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if err := extractTarFile(tr, hdr, destDir, rel); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", entryPath, err))
		}
	}
	return errors.Join(errs...)
}

// extractTarFile writes one regular file member, currently positioned
// at tr, to destDir/rel — preserving hdr's own stored permission bits
// the same reason extractZipFile preserves a zip member's.
func extractTarFile(tr *tar.Reader, hdr *tar.Header, destDir, rel string) error {
	dest, err := destFile(destDir, rel)
	if err != nil {
		return err
	}
	mode := hdr.FileInfo().Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	out, err := createOverwriting(dest, mode)
	if err != nil {
		return err
	}
	// See extractZipFile's own doc comment on why this deferred Close's
	// own result is discarded rather than the explicit one below it.
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, tr); err != nil {
		return err
	}
	return out.Close()
}

// extractSevenZip extracts members out of archivePath via the real 7z
// binary (see SevenZipBinary) into a fresh temp directory — 7z's own
// `x` command recreates each requested member's full internal path
// under whatever `-o<dir>` names (verified directly: asking for a bare
// directory member name, e.g. "sub", correctly recurses into
// everything nested under it — no wildcard needed, unlike unrar; see
// extractRar's own doc comment) — then relocates exactly the
// requested members from there into destDir via moveExtractedTree,
// which is what actually rewrites each one onto its own base name
// (see Extract's own doc comment on that contract, and
// relativeDest for the shared logic zip/tar already use for it).
func extractSevenZip(archivePath string, members []Entry, destDir string) error {
	bin, err := SevenZipBinary()
	if err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "breakthrough-7z-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }() // best-effort temp-dir cleanup

	args := []string{"x", "-y", "-o" + tmpDir, archivePath}
	for _, m := range members {
		args = append(args, m.Path)
	}
	if out, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", bin, err, bytes.TrimSpace(out))
	}

	return moveExtractedTree(tmpDir, destDir, memberPathsOf(members))
}

// extractRar extracts members out of archivePath via the real unrar
// binary into a fresh temp directory, then relocates them into destDir
// the same way extractSevenZip does (see moveExtractedTree).
//
// Unlike 7z, unrar does *not* recurse into a directory member's own
// contents when given its bare name (verified directly: asking unrar
// to extract just "sub" instead silently extracted the *entire*
// archive, sibling files included — a real, observed difference from
// 7z's own behavior, not an assumption) — it needs an explicit
// "sub/*" glob instead, which this appends only for a member Extract's
// own caller already marked IsDir.
func extractRar(archivePath string, members []Entry, destDir string) error {
	if _, err := exec.LookPath("unrar"); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "breakthrough-rar-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }() // best-effort temp-dir cleanup

	args := []string{"x", "-y", archivePath}
	for _, m := range members {
		if m.IsDir {
			args = append(args, m.Path+"/*")
		} else {
			args = append(args, m.Path)
		}
	}
	args = append(args, tmpDir+string(filepath.Separator))
	if out, err := exec.Command("unrar", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("unrar: %w: %s", err, bytes.TrimSpace(out))
	}

	return moveExtractedTree(tmpDir, destDir, memberPathsOf(members))
}

// memberPathsOf is extractSevenZip/extractRar's own small shared step:
// their real work (building the tool's own CLI args) needs the full
// Entry (specifically IsDir, for extractRar's own wildcard — see its
// doc comment), but moveExtractedTree afterward, like extractZip/
// extractTar's own relativeDest calls, only ever needs the plain paths.
func memberPathsOf(members []Entry) []string {
	paths := make([]string, len(members))
	for i, m := range members {
		paths[i] = m.Path
	}
	return paths
}

// moveExtractedTree relocates whatever extractSevenZip/extractRar
// actually produced under tmpDir (the *tool's* own layout: each
// requested member's full internal archive path, recreated as real
// files/directories) into destDir, renamed onto each member's own base
// name — precisely the same destination contract extractZip/extractTar
// already give a Copy/Paste-sourced request (see Extract's own doc
// comment), just reached by moving already-extracted real files
// instead of streaming straight out of the archive itself the way
// those two do, since neither 7z nor unrar offers this package a
// stream to read from directly.
//
// fsops.Move (not a plain os.Rename) is what actually relocates each
// file: tmpDir is wherever the system's own default temp directory
// lives, which is not guaranteed to share a filesystem with an
// arbitrary user-chosen destDir — see fsops.Move's own doc comment on
// the EXDEV fallback this project's own file operations already
// require everywhere else (see this project's own documented rule to
// always handle EXDEV on a file move).
func moveExtractedTree(tmpDir, destDir string, members []string) error {
	var errs []error
	err := filepath.WalkDir(tmpDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == tmpDir {
			return nil
		}
		rel, err := filepath.Rel(tmpDir, p)
		if err != nil {
			return err
		}
		entryPath := filepath.ToSlash(rel)
		destRel, ok := relativeDest(entryPath, members)
		if !ok {
			// Shouldn't happen — both extractSevenZip and extractRar only
			// ever ask the tool for exactly the requested members — but
			// tolerated defensively rather than failing the whole
			// extraction over one unexpected extra entry.
			return nil
		}
		if d.IsDir() {
			if err := ensureDir(destDir, destRel); err != nil {
				errs = append(errs, err)
			}
			return nil
		}
		dest, err := destFile(destDir, destRel)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", entryPath, err))
			return nil
		}
		if err := fsops.Move(p, dest, fsops.MoveOptions{Force: true}); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", entryPath, err))
		}
		return nil
	})
	if err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
