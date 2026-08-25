package search

// Mode selects how a search pattern is interpreted — shared by every
// backend this package builds a command for (find, locate, grep), each
// applying it to whatever it's matching against (a file name for
// find/locate, a line of file content for grep).
type Mode int

const (
	// ModeGlob is shell-pattern matching ("*"/"?"/"[...]") — the same
	// syntax this app's own panel filter and Select+/- already use (see
	// internal/ui's filterByText/selectByPattern), for consistency.
	ModeGlob Mode = iota
	// ModeKeyword is a plain substring: the typed text wrapped in "*...*"
	// for find/locate (an anchored glob match would otherwise require the
	// whole name to equal it), or passed as a fixed string to grep
	// (see GrepArgs) — no pattern syntax to learn for either.
	ModeKeyword
	// ModeRegex is POSIX extended regular expression syntax (ERE) —
	// find/locate match the whole path with it (see FindArgs/LocateArgs's
	// own doc comments on why that's a real, tool-inherent difference
	// from Glob/Keyword's filename-only matching, not a choice made
	// here), grep matches within each line.
	ModeRegex
)
