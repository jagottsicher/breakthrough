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

// BzgrepArgs builds bzgrep's own argument list (not including
// "bzgrep" itself) for a single bzip2-compressed file. Same
// -n/-I/-H/-i/-w/-m passthrough and -r/--recursive restriction as
// ZgrepArgs (verified by reading bzgrep's own script, /usr/bin/bzgrep,
// not guessed — it's literally "Adapted from zgrep of the Debian gzip
// package" per its own header comment) — but with one real difference,
// also only found by reading and running the actual script, not
// documented anywhere and not present in the current zgrep/xzgrep
// (verified — neither has this block at all): this bzgrep build is an
// old (1998–2002) snapshot that still carries a "grep is buggy with -e
// on SVR4" workaround forcing $grep to egrep the moment -e or -f is
// used — which then conflicts with -F/-E's own matcher flag ("grep:
// conflicting matchers specified"), the exact same class of bug
// ZipgrepArgs' own doc comment already documents for zipgrep. Passing
// pattern as a bare positional argument instead of via -e sidesteps
// the trigger entirely (confirmed by running it directly): $grep stays
// plain grep, so -F/-E keep working normally, unlike ZipgrepArgs' own
// case, which had to give up flag-based mode control altogether.
func BzgrepArgs(pattern string, mode Mode, bz2File string, caseSensitive, wholeWords, firstHit bool) []string {
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
	return append(args, pattern, bz2File)
}

// XzgrepArgs builds xzgrep's own argument list (not including
// "xzgrep" itself) for a single xz- or lzma-compressed file —
// structurally identical to ZgrepArgs (verified by reading xzgrep's
// own script, /usr/bin/xzgrep, part of XZ Utils: the exact same
// option-parsing shape as zgrep's own script, same -r/--recursive
// rejection, same -n/-I/-H/-i/-w/-m/-e passthrough).
func XzgrepArgs(pattern string, mode Mode, xzFile string, caseSensitive, wholeWords, firstHit bool) []string {
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
	return append(args, "-e", pattern, xzFile)
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
