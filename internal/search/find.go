package search

// FindArgs builds find(1)'s own argument list (not including "find"
// itself — see Runner, which is what actually runs it) for a filename
// search rooted at scope, matching pattern per mode.
//
// Glob and Keyword match just the file name (find's own -iname),
// case-insensitively — the same scope this app's own panel filter and
// Select+/- already use (see internal/ui's filterByText/selectByPattern).
// Regex instead matches the WHOLE PATH (find's own -iregex) — not a
// choice made here, but how find itself defines these primaries on
// every platform (verified against the GNU findutils manual and the
// FreeBSD find(1) man page, not guessed): -iname only ever sees the
// last path component, -iregex always sees the entire path, so a
// pattern like ".*/vendor/.*\\.go$" can match on directory structure a
// filename-only pattern never could.
//
// goos picks the regex dialect and flag placement, per real,
// verified differences between GNU and BSD find — not one shared
// syntax:
//
//   - "linux" (GNU find): -regextype posix-extended is required
//     explicitly. GNU find's own default, with no -regextype given at
//     all, is Emacs-style regex syntax, not POSIX ERE — silently
//     different matching behavior from what a "regex" toggle should
//     mean here if this were left out.
//   - anything else (darwin/freebsd — BSD find): -E switches -regex/
//     -iregex to POSIX ERE. GNU's -regextype doesn't exist on BSD find
//     at all and would be a hard usage error there.
//
// -print0 (not -print) throughout: null-separated output, so a result
// containing a newline — a real, if rare, possibility in a file name —
// can never be mistaken for two separate results by whatever reads it
// (see Runner).
func FindArgs(goos, scope, pattern string, mode Mode) []string {
	switch mode {
	case ModeRegex:
		if goos == "linux" {
			return []string{scope, "-regextype", "posix-extended", "-iregex", pattern, "-print0"}
		}
		return []string{"-E", scope, "-iregex", pattern, "-print0"}
	case ModeKeyword:
		return []string{scope, "-iname", "*" + pattern + "*", "-print0"}
	default: // ModeGlob
		return []string{scope, "-iname", pattern, "-print0"}
	}
}
