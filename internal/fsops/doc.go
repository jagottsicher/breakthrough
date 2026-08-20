// Package fsops implements breakthrough's filesystem operations: listing,
// copy, move, rename, and trash handling. This is the highest-risk part of
// the codebase (data loss on bugs), so every exported function here should
// ship with tests, and comments should err on the side of over-explaining
// edge cases (e.g. cross-device moves, symlink handling, partial failures).
package fsops
