package search

import "regexp"

// GrepArgs builds grep(1)'s own argument list (not including "grep"
// itself — see Runner) for a recursive content search rooted at scope.
// ModeGlob has no meaning for file content — there's no glob concept
// for what's INSIDE a file — so it's treated the same as ModeKeyword
// here; internal/ui's search dialog itself doesn't offer Glob once
// content search is turned on, for exactly this reason.
//
// Keyword is a fixed string (-F): matched literally, not as a pattern
// — the same "no syntax to learn" contract ModeKeyword has for
// find/locate. Regex is POSIX extended (-E) — verified supported
// identically across GNU grep and BSD/macOS/FreeBSD grep, unlike -P
// (Perl-compatible), which only GNU grep has — deliberately not used
// here for exactly that reason, so a regex content search behaves the
// same on every platform this app targets.
//
// -r (recurse), -n (line numbers, so a result can later be opened
// straight to the matching line), -I (skip binary files — a content
// search shouldn't surface binary garbage as a "match"), and -H
// (always print the file name, even if scope turns out to be a single
// file) are all verified supported identically across GNU and BSD/
// macOS/FreeBSD grep.
func GrepArgs(pattern, scope string, mode Mode) []string {
	args := []string{"-r", "-n", "-I", "-H"}
	args = append(args, matchModeFlag(mode))
	return append(args, "-e", pattern, scope)
}

// ZgrepArgs builds zgrep's own argument list (not including "zgrep"
// itself) for a single gzip-compressed file. zgrep has no -r/
// --recursive at all — a real, permanent limitation (verified against
// gzip's own bug tracker, not guessed: extending zgrep to cover the
// whole of grep's directory-recursion behavior was explicitly rejected
// upstream) — so a content search across a directory tree of .gz files
// runs one zgrep invocation per file the caller already found via
// FindArgs (see internal/search's own package doc and the executor
// that drives this), not a single recursive zgrep call.
func ZgrepArgs(pattern string, mode Mode, gzFile string) []string {
	args := []string{"-n", "-I", "-H"}
	args = append(args, matchModeFlag(mode))
	return append(args, "-e", pattern, gzFile)
}

// ZipgrepArgs builds zipgrep's own argument list (not including
// "zipgrep" itself) for a single zip archive. Like zgrep, zipgrep
// takes exactly one archive per invocation — verified against its own
// man page: the zipfile argument is singular, with any further
// arguments taken as archive MEMBER filters, not additional archives
// — so a content search across several zip files runs one zipgrep
// invocation per file the caller already found (same pattern as
// ZgrepArgs).
//
// Two real quirks here, both verified by reading zipgrep's own script
// (/usr/bin/zipgrep, Info-Zip) and running it directly — not guessed,
// and not documented in its man page:
//
//   - It always runs the pattern through egrep(1) internally (-E
//     always implied), so passing -F for a fixed-string (Keyword/Glob)
//     match conflicts with that ("grep: conflicting matchers
//     specified"). ModeRegex passes pattern through unchanged (egrep's
//     default already is what -E would ask for); anything else escapes
//     it with regexp.QuoteMeta first, so it still matches literally
//     without ever passing a matcher flag egrep would reject.
//   - Its own "-e pattern" handling is broken: the script reuses
//     pattern as both -e's argument AND a second, separate positional
//     pattern, which is the exact same "conflicting matchers" error
//     again. The pattern must always be passed as a bare positional
//     argument instead — never preceded by -e.
func ZipgrepArgs(pattern string, mode Mode, zipFile string) []string {
	if mode != ModeRegex {
		pattern = regexp.QuoteMeta(pattern)
	}
	return []string{"-n", pattern, zipFile}
}

// matchModeFlag is GrepArgs/ZgrepArgs/ZipgrepArgs' shared Mode
// translation: -F (fixed string) for anything but Regex, -E (POSIX
// extended regex) for Regex.
func matchModeFlag(mode Mode) string {
	if mode == ModeRegex {
		return "-E"
	}
	return "-F"
}
