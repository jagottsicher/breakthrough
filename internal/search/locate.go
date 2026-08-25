package search

// LocateArgs builds locate(1)'s own argument list (not including
// "locate" itself — see Runner) for a filename search matching pattern
// per mode. Unlike find, locate has no directory-scope argument at
// all — it always searches its own whole-system index, built ahead of
// time by updatedb (see internal/ui's search dialog, which still
// applies the chosen scope afterward as a client-side filter over
// locate's results, and warns that the index may not reflect very
// recent changes).
//
// locate's own default match scope is the WHOLE PATH (verified against
// the mlocate/plocate manuals, not guessed) — the opposite of find's
// own -iname, which is filename-only by default. -b/--basename
// restricts it to just the file name, which this always adds for Glob
// and Keyword (but not Regex) specifically so switching the search
// dialog's Engine between find and locate for the same Glob/Keyword
// pattern behaves the same either way, rather than silently widening
// to match on directory names too.
//
// -b is Linux-only (mlocate/plocate) — verified against the FreeBSD
// locate(1) man page's own full flag list, which has no -b at all,
// alongside no regex support either (see the ok=false case below): on
// BSD locate (macOS, FreeBSD), Glob/Keyword mode falls back to
// locate's own native whole-path matching instead, since there's no
// flag to ask for anything narrower.
//
// Regex mode is Linux-only outright (mlocate/plocate's own --regex,
// POSIX ERE, whole path). BSD locate has no regex support of any kind
// — offering it there would be a hard usage error, so LocateArgs
// returns ok=false rather than building a broken command (see
// internal/ui's search dialog, which hides "Regex" as a locate option
// outside Linux for the same reason).
//
// -0 (not the newline-separated default) throughout: null-separated
// output, the same reason FindArgs uses -print0 — see its own doc
// comment.
//
// -i (case-insensitive) is added unless caseSensitive — locate's own
// default (no -i) is case-sensitive, so this is what actually needs
// adding for the common case, the reverse of FindArgs' -iname/-name
// choice below.
func LocateArgs(goos, pattern string, mode Mode, caseSensitive bool) (args []string, ok bool) {
	linux := goos == "linux"
	args = []string{"-0"}
	if !caseSensitive {
		args = append(args, "-i")
	}

	switch mode {
	case ModeRegex:
		if !linux {
			return nil, false
		}
		return append(args, "--regex", pattern), true
	case ModeKeyword:
		if linux {
			args = append(args, "-b")
		}
		return append(args, "*"+pattern+"*"), true
	default: // ModeGlob
		if linux {
			args = append(args, "-b")
		}
		return append(args, pattern), true
	}
}
