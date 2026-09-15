// Package compare answers "are these two things the same, and if not,
// what's different" for two files or two directory trees — used by the
// Compare screen (internal/ui), never by any panel/copy code directly,
// the same fsops-vs-ui split this project keeps everywhere else.
//
// A file-to-file comparison (CompareFiles) is metadata only: size and
// modification time. A byte-exact answer is a separate, explicit step
// left to the caller — fsops.Hash on each side — since it can be slow
// on a large file and this package has no opinion on which digest to
// use for it. A line-by-line answer is UnifiedDiff, a thin wrapper
// around the system's own diff(1): this project's established
// preference for a real POSIX tool over a reimplementation, the same
// choice internal/replace (sed) and internal/search (grep/find/locate)
// already made for their own external tools.
//
// A directory-to-directory comparison (Walk) descends both trees in
// lock step, side by side, directory by directory. A path that exists
// on only one side is reported once, as itself — its own subtree, if
// it has one, is never descended into, so an old, untouched backup
// folder that only exists on one side is one row, not ten thousand.
package compare
