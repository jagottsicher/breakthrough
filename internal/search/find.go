package search

// FindArgs builds find(1)'s own argument list (not including "find"
// itself — see Runner, which is what actually runs it) for a filename
// search rooted at scope, matching pattern per mode.
//
// Glob and Keyword match just the file name (find's own -iname, or
// -name if caseSensitive — see below), case-insensitively by default —
// the same scope this app's own panel filter and Select+/- already use
// (see internal/ui's filterByText/selectByPattern). Regex instead
// matches the WHOLE PATH (find's own -iregex/-regex) — not a choice
// made here, but how find itself defines these primaries on every
// platform (verified against the GNU findutils manual and the FreeBSD
// find(1) man page, not guessed): -iname only ever sees the last path
// component, -iregex always sees the entire path, so a pattern like
// ".*/vendor/.*\\.go$" can match on directory structure a filename-only
// pattern never could.
//
// caseSensitive switches -iname/-iregex to their case-sensitive
// counterparts, -name/-regex — a real, if lesser-used, GNU/BSD find
// primitive on every platform this app targets, so this needs no
// goos-specific handling the way the regex dialect below does.
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
//
// ignoreDirs, if non-empty, adds find's own standard "-prune" idiom
// ahead of the real test: "( -name D1 -o -name D2 ) -prune -o
// <real test> -print0" — each name matched via -name (not -iname: a
// deliberate, case-sensitive exception here, since directory names to
// skip — .git, node_modules, vendor — are conventionally exact) against
// find's own traversal, so a matching directory is never even descended
// into, rather than merely filtered out of already-produced results
// (an important difference for a large ignored tree: node_modules
// itself never gets walked at all). -prune's own semantics — skip
// descending, produce no output unless followed by an explicit action —
// are POSIX-common to every find implementation this app targets, so
// this needs no goos-specific handling the way -iregex above does.
func FindArgs(goos, scope, pattern string, mode Mode, ignoreDirs []string, caseSensitive bool) []string {
	var args []string
	if mode == ModeRegex && goos != "linux" {
		args = append(args, "-E") // BSD find: must precede the path, see above
	}
	args = append(args, scope)

	if len(ignoreDirs) > 0 {
		args = append(args, "(")
		for i, name := range ignoreDirs {
			if i > 0 {
				args = append(args, "-o")
			}
			args = append(args, "-name", name)
		}
		args = append(args, ")", "-prune", "-o")
	}

	nameFlag, regexFlag := "-iname", "-iregex"
	if caseSensitive {
		nameFlag, regexFlag = "-name", "-regex"
	}

	switch mode {
	case ModeRegex:
		if goos == "linux" {
			args = append(args, "-regextype", "posix-extended", regexFlag, pattern)
		} else {
			args = append(args, regexFlag, pattern)
		}
	case ModeKeyword:
		args = append(args, nameFlag, "*"+pattern+"*")
	default: // ModeGlob
		args = append(args, nameFlag, pattern)
	}
	return append(args, "-print0")
}
