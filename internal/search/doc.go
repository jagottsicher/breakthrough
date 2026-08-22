// Package search builds and runs the shell commands behind breakthrough's
// search dialog (see internal/ui/search.go): filename search via find or
// locate, and optional file-content search via grep, zgrep (gzip
// contents), or zipgrep (zip contents) — real POSIX tools, shelled out
// to exactly like this project already does for df (internal/ui's
// dfSummary) and the bash line itself, rather than a reimplemented Go
// directory walker. That's also what gives locate its own indexed
// speed for free, and grep/zgrep/zipgrep their own already-correct
// content-matching behavior.
//
// Every argv-building function here is a pure function returning
// []string, deliberately kept separate from actually running anything
// (see Runner) — command construction is where the real cross-platform
// risk lives (see find.go/locate.go/grep.go's own doc comments for the
// GNU/BSD differences involved, each verified against the real,
// current manuals rather than guessed, per this project's own
// standing rule for exactly this kind of tool-behavior assumption),
// and keeping it pure makes that risk fully unit-testable without
// executing anything.
package search
