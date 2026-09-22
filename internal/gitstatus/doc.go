// Package gitstatus answers "is this directory part of a git working
// tree, and if so, what's its status" — a thin wrapper around the
// system's own git(1), this project's established preference for a
// real POSIX/well-known tool over a reimplementation (the same choice
// already made for sed, grep/find/locate, du/df, uname, who — see
// internal/replace, internal/search, internal/fsops.diskusage.go,
// internal/ui's own kernelVersionText/loggedInSessions). Parsing git's
// own on-disk format (refs, the index, packed-refs, ...) from scratch
// would be a large, fragile undertaking for something git itself
// already answers correctly in one call.
package gitstatus
