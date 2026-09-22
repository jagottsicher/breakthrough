package compare

import (
	"fmt"
	"os"
	"time"
)

// FileMeta is one side's own metadata for a CompareFiles result.
type FileMeta struct {
	Size    int64
	ModTime time.Time
	IsDir   bool
}

// FileCompare is what CompareFiles reports about two paths — metadata
// only. SizeEqual/ModTimeEqual are the same "quick check" heuristic
// rsync's own default sync mode uses: if both agree, the two are
// almost certainly the same content without ever reading either one —
// a caller that wants certainty instead of a heuristic still has to
// hash both sides itself (see the package doc for why that's not done
// here), and a line-by-line answer is UnifiedDiff.
type FileCompare struct {
	A, B FileMeta

	SizeEqual, ModTimeEqual bool
}

// CompareFiles stats a and b and reports their metadata side by side.
// Neither path has to actually be a plain file — comparing a
// directory's own metadata against a file's is a legitimate (if
// probably uninteresting) result, not an error; Walk is what a caller
// wants for descending into two directories instead of just stat'ing
// them.
//
// Uses Lstat, not Stat: a symlink is compared as itself, not as
// whatever it points to — consistent with Walk (see its own doc
// comment) and with fsops.Stat's own IsSymlink handling elsewhere in
// this app, and it avoids ever following a symlink into a cycle.
func CompareFiles(a, b string) (FileCompare, error) {
	infoA, err := os.Lstat(a)
	if err != nil {
		return FileCompare{}, fmt.Errorf("stat %s: %w", a, err)
	}
	infoB, err := os.Lstat(b)
	if err != nil {
		return FileCompare{}, fmt.Errorf("stat %s: %w", b, err)
	}

	fc := FileCompare{
		A: FileMeta{Size: infoA.Size(), ModTime: infoA.ModTime(), IsDir: infoA.IsDir()},
		B: FileMeta{Size: infoB.Size(), ModTime: infoB.ModTime(), IsDir: infoB.IsDir()},
	}
	fc.SizeEqual = fc.A.Size == fc.B.Size
	fc.ModTimeEqual = fc.A.ModTime.Equal(fc.B.ModTime)
	return fc, nil
}
