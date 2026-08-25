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
//
// -i (case-insensitive) is added unless caseSensitive, matching
// FindArgs/LocateArgs' own default — content search here used to
// always be case-sensitive regardless of this flag even existing
// (there was no toggle at all), inconsistent with filename search's
// own case-insensitive default; this brings the two in line rather
// than leaving that inconsistency in place now that a real toggle
// exists to opt back into case-sensitive matching.
//
// wholeWords adds -w (match only whole words); firstHit adds -m 1
// (stop after the first match in each file) — both real extensions on
// every grep this app targets, not POSIX-mandated but confirmed
// present in GNU grep and BSD/macOS/FreeBSD grep alike (verified
// against the FreeBSD/macOS grep(1) man pages, not guessed).
//
// ignoreDirs adds one --exclude-dir=NAME per entry — a real user
// report: a plain content search (no Filename term — see runContentSearch's
// own doc comment on why only that one case reaches GrepArgs with a
// whole directory tree still left to walk, rather than one already-
// approved file at a time) ran through Ignore dirs' own value entirely
// unfiltered, since nothing here ever passed it to grep at all.
// --exclude-dir matches a directory's own base name — the same
// component-only contract Request.IgnoreDirs already documents for
// FindArgs' -prune and locate's own client-side underIgnoredDir filter
// — confirmed present under that exact flag name in GNU grep (this
// package's own primary target) and, via its own bsdgrep(1) man page,
// FreeBSD grep; macOS shipped the same bsdgrep-derived implementation
// starting with macOS 12 (Monterey, 2021) — well before any macOS this
// app's own CI, or a realistic install, would still be running.
func GrepArgs(pattern, scope string, mode Mode, ignoreDirs []string, caseSensitive, wholeWords, firstHit bool) []string {
	args := []string{"-r", "-n", "-I", "-H"}
	for _, name := range ignoreDirs {
		args = append(args, "--exclude-dir="+name)
	}
	if !caseSensitive {
		args = append(args, "-i")
	}
	if wholeWords {
		args = append(args, "-w")
	}
	if firstHit {
		args = append(args, "-m", "1")
	}
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
func ZgrepArgs(pattern string, mode Mode, gzFile string, caseSensitive, wholeWords, firstHit bool) []string {
	args := []string{"-n", "-I", "-H"}
	if !caseSensitive {
		args = append(args, "-i")
	}
	if wholeWords {
		args = append(args, "-w")
	}
	if firstHit {
		args = append(args, "-m", "1")
	}
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
func ZipgrepArgs(pattern string, mode Mode, zipFile string, caseSensitive, wholeWords, firstHit bool) []string {
	if mode != ModeRegex {
		pattern = regexp.QuoteMeta(pattern)
	}
	args := []string{"-n"}
	if !caseSensitive {
		args = append(args, "-i") // egrep's own -i, passed through — see this func's own doc comment
	}
	if wholeWords {
		args = append(args, "-w")
	}
	if firstHit {
		args = append(args, "-m", "1")
	}
	return append(args, pattern, zipFile)
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
