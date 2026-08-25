package search

import (
	"os/exec"
	"runtime"
)

// LocateAvailable reports whether the locate binary can be found on
// $PATH at all — internal/ui's search dialog uses this to decide
// whether to offer "locate" as an Engine choice in the first place.
// This only checks that the binary exists, not that its database has
// ever actually been built (see LocateDatabaseCaveat) — a real,
// separate gap, particularly on macOS, where the binary can be present
// with no database ever populated behind it.
func LocateAvailable() bool {
	_, err := exec.LookPath("locate")
	return err == nil
}

// LocateRegexAvailable reports whether locate's own --regex option is
// available on this platform — Linux (mlocate/plocate) only, verified
// against the FreeBSD locate(1) man page's own full flag list, which
// has none at all (see LocateArgs' own doc comment). internal/ui's
// search dialog uses this to hide "Regex" as a locate option
// elsewhere, rather than let a user pick a combination LocateArgs
// itself would refuse to build a command for.
func LocateRegexAvailable() bool {
	return runtime.GOOS == "linux"
}

// LocateDatabaseCaveat is shown alongside the locate Engine option
// whenever it's offered — locate's own index is only ever as fresh as
// the last updatedb run, which on a typical desktop is a periodic cron/
// systemd-timer job (often not enabled at all on macOS, where locate's
// database frequently doesn't exist until a user explicitly builds it
// via `sudo /usr/libexec/locate.updatedb`) — a search that comes back
// empty, or missing something created moments ago, isn't a bug in this
// app; find is the option that's always current, at the cost of
// walking the filesystem live instead of a prebuilt index.
const LocateDatabaseCaveat = "locate searches a prebuilt index (updatedb) — may not reflect very recent changes"

// ZgrepAvailable reports whether zgrep can be found on $PATH — always
// installed alongside gzip on every platform this app targets, but
// checked rather than assumed, the same as LocateAvailable.
func ZgrepAvailable() bool {
	_, err := exec.LookPath("zgrep")
	return err == nil
}

// ZipgrepAvailable reports whether zipgrep can be found on $PATH —
// part of Info-Zip's unzip package, not guaranteed to be installed
// (unlike zgrep/gzip) even on a system that otherwise has zip/unzip
// themselves.
func ZipgrepAvailable() bool {
	_, err := exec.LookPath("zipgrep")
	return err == nil
}
