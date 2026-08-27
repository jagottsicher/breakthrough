// Package replace implements "Sed Replace" (see internal/ui/sedreplace.go):
// running a real sed(1) substitution against one or more selected files
// and previewing the result before anything is written back.
//
// This never invokes sed's own -i (in-place edit): GNU sed wants
// `-i script file`, while BSD/macOS sed requires a mandatory backup-
// suffix argument right after -i (an empty string for no backup) before
// the script — two incompatible calling conventions for the same intent
// (verified against both sed(1) man pages, not guessed). Instead, sed
// always runs as a plain filter (stdin -> stdout,
// see RunSed) and this package diffs/writes the result itself, atomically
// (temp file plus rename in the same directory — the same pattern
// internal/config's SetKey already uses for settings files), sidestepping
// the -i split entirely. It also means Preview and Apply run the exact
// same command — dry-run is just "don't write the result back", not a
// separate code path that could drift from what actually gets applied.
//
// Scope is deliberately narrow: a fixed list of already-selected files
// (from the panel's checkbox selection or current row — see
// internal/ui's selectedOrCurrentPaths), never a recursive directory
// walk. A directory tree search-and-replace would need the same
// discovery machinery internal/search already has (grep to first find
// which files/lines match) and is left for a later phase if it turns out
// to be wanted — see docs/whitepaper.md.
//
// Two ways to build the sed script that actually runs (see script.go):
// a guided Find/Replace pair with a few common flags (BuildScript), or
// an advanced mode where the user's own raw sed script is used verbatim,
// unlocking anything real sed can do (address ranges, multiple commands,
// hold-space tricks, ...) at the cost of needing to know sed syntax.
//
// Portability note, in the same spirit as internal/search's own grep
// wrappers: only GNU sed (this project's primary target, Linux) has been
// verified directly. BSD/macOS sed is a different, less featureful
// implementation (no \U/\L case conversion, no -z, and — unverified,
// flagged rather than guessed — support for the s///I case-insensitive
// modifier is unconfirmed there). "All of sed's capabilities" in the
// advanced mode means whichever sed is actually first on $PATH, not a
// guaranteed common subset.
package replace
