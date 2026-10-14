// Package compare answers "are these two things the same, and if not,
// what's different" for two files or two directory trees — used by the
// Compare screen (internal/ui), never by any panel/copy code directly,
// the same fsops-vs-ui split this project keeps everywhere else.
//
// A file-to-file comparison (CompareFiles) is metadata only: size and
// modification time. A byte-exact answer is a separate, explicit step
// left to the caller — fsops.Hash on each side — since it can be slow
// on a large file and this package has no opinion on which digest to
// use for it. A line-by-line answer is SideBySide, built in-process on
// github.com/aymanbagabas/go-udiff rather than the system's own
// diff(1) — a deliberate reversal of this package's earlier preference
// for a real POSIX tool (still internal/replace's and internal/search's
// own choice for sed/grep/find/locate, which have no equivalent
// in-process substitute here). The side-by-side view (internal/ui) needs
// each pair of lines as structured data — which line number on which
// side, same/changed/only-one-side — not a block of text to reparse;
// go-udiff already computes exactly that (OpKind per line), where
// shelling out to diff(1) and parsing its unified-diff text back into
// the same shape would only add a round trip through a format designed
// for patch(1), not for driving two side-by-side panes. It also drops
// the previous diff(1)-on-$PATH precondition entirely: the feature no
// longer disables itself on a system without diff(1) installed.
//
// This supersedes the compare.go package header's own older note on the
// same subject (unified-diff-via-Look reasoning) — that whole approach
// was replaced by the side-by-side view this comment now describes.
//
// A directory-to-directory comparison (Walk) descends both trees in
// lock step, side by side, directory by directory. A path that exists
// on only one side is reported once, as itself — its own subtree, if
// it has one, is never descended into, so an old, untouched backup
// folder that only exists on one side is one row, not ten thousand.
package compare
