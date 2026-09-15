// Package rsync builds a real rsync(1) invocation from a small, typed
// description of the job — never reimplements any of rsync's own
// transfer logic, matching this project's own established "shell out
// to the real POSIX tool" convention already used for du/df/grep/sed/
// find (see internal/fsops/internal/search/internal/replace) rather
// than reinventing it. Deliberately independent of internal/remotefs:
// rsync always runs as a real, separate process talking to its own
// remote copy over plain ssh, never through this project's own
// golang.org/x/crypto/ssh-based SFTP client — the two connections
// share nothing at the protocol level, only (for a host already
// trusted through the Connect dialog) the same real ssh-agent/default
// keys/~/.ssh/known_hosts a plain `ssh` invocation already uses on its
// own.
package rsync

import (
	"fmt"
	"strings"
)

// Endpoint is one side of a Job — a real local path, or a remote one
// reached over ssh. Host == "" means local.
type Endpoint struct {
	Host string
	Port int // 0 means the default (22) — see Job.Args' own doc comment on why this can't always be expressed
	User string
	Path string
}

// IsRemote reports whether e names a real local path or a remote one.
func (e Endpoint) IsRemote() bool {
	return e.Host != ""
}

// String renders e the way rsync's own CLI expects an argument to
// look — a plain path for a local endpoint, "user@host:path" (or just
// "host:path" with no explicit user) for a remote one. Never includes
// the port: rsync's own compact "host:path" syntax has no room for
// one at all — a non-default port is instead expressed through a
// whole separate flag (see Job.Args' own -e/--rsh handling), which is
// why Port lives on Endpoint but never appears here.
func (e Endpoint) String() string {
	if !e.IsRemote() {
		return e.Path
	}
	if e.User != "" {
		return e.User + "@" + e.Host + ":" + e.Path
	}
	return e.Host + ":" + e.Path
}

// Job describes one rsync invocation, independent of how (or whether)
// it's ever actually run — Args/Command turn it into the real argv, a
// UI's own live command-preview line, or a background task's own
// argument list, all from the exact same description, so those three
// can never drift out of sync with each other the way maintaining
// separate ad hoc string-building for each would risk.
type Job struct {
	Source, Destination Endpoint

	// CopyContents, when Source names a directory, copies its own
	// children directly into Destination (rsync's own well-known but
	// easy-to-miss "trailing slash on the source" convention) rather
	// than creating a new directory named after Source itself inside
	// Destination. Meaningless — and ignored by Args — when Source is
	// a single file: there are no "contents" to copy separately from
	// the file itself either way.
	//
	// Surfaced as an explicit, named choice rather than left to
	// whether the user happened to type a trailing "/" themselves,
	// per the user's own explicit request: a single easy-to-miss
	// character silently deciding between two very different
	// outcomes (does the destination end up with a new subdirectory,
	// or not) is exactly the kind of trap this project's own
	// no-silent-surprises principle exists to avoid.
	CopyContents bool

	Archive  bool // -a: recursive, preserves permissions/times/symlinks/... — rsync's own conventional default for "just copy it properly"
	Delete   bool // --delete: also remove from Destination whatever no longer exists in Source
	DryRun   bool // -n: report what would happen, change nothing
	Compress bool // -z: compress file data in transit — mainly useful over a slow link
	Progress bool // --info=progress2: one running "N% ... (xfr#i, to-chk=j/k)" line rather than rsync's own default silence

	// Excludes is one --exclude=PATTERN flag per entry, in order.
	Excludes []string

	// ExtraArgs is a raw, user-typed fragment of shell syntax, spliced
	// verbatim into Command() right after every flag Job itself
	// already understands — an escape hatch for whichever of rsync's
	// own many other flags this type doesn't model explicitly, without
	// this package having to keep up with all of them. Never re-parsed
	// into discrete arguments here (see Command's own doc comment on
	// why); Args() therefore has no way to represent it at all and
	// simply omits it.
	ExtraArgs string
}

// sourceArg is Source's own argument exactly as it goes on the command
// line — String()'s own rendering, with a trailing "/" appended when
// CopyContents is on and Source actually names a directory-shaped
// path (Source.Path doesn't already end in "/", which would make the
// slash this adds redundant, not wrong, but still worth not doubling
// up).
func (j Job) sourceArg() string {
	s := j.Source.String()
	if j.CopyContents && !strings.HasSuffix(s, "/") {
		s += "/"
	}
	return s
}

// rsh, if non-"", is what Args puts into a leading "-e" flag: the real
// ssh(1) binary, plus "-p PORT" if either endpoint asks for a
// non-default one. rsync itself has no way to express two different
// non-standard ports for a source and a destination that are both
// remote at once (its own "-e" is one global flag, not one per side)
// — Source's own port wins in that specific, rare case, which is a
// real, deliberate limitation, not an oversight: getting that far
// already means both ends are remote *and* both use a non-default
// port, a narrow combination not worth a whole ProxyCommand-based
// workaround for a first version of this feature.
func (j Job) rsh() string {
	port := j.Destination.Port
	if j.Source.IsRemote() && j.Source.Port != 0 {
		port = j.Source.Port
	}
	if port == 0 {
		return ""
	}
	return fmt.Sprintf("ssh -p %d", port)
}

// flagArgs is every discrete flag Job's own typed fields (everything
// except ExtraArgs — see its own doc comment) produce, in a fixed,
// readable order — the shared core both Args() and Command() build on,
// so the two can never quietly disagree about what a given set of
// toggles actually turns into.
func (j Job) flagArgs() []string {
	var args []string
	add := func(a ...string) { args = append(args, a...) }

	if j.Archive {
		add("-a")
	}
	if j.Compress {
		add("-z")
	}
	if j.DryRun {
		add("-n")
	}
	if j.Delete {
		add("--delete")
	}
	if j.Progress {
		add("--info=progress2")
	}
	for _, pattern := range j.Excludes {
		add("--exclude=" + pattern)
	}
	if rsh := j.rsh(); rsh != "" {
		add("-e", rsh)
	}
	return args
}

// Args builds rsync's own real, discrete argv, excluding the leading
// "rsync" program name itself (exec.Command's own first argument
// already covers that) and ExtraArgs (a raw shell fragment has no
// single correct discrete-argument form without re-parsing shell
// syntax by hand — see Command's own doc comment on why this package
// deliberately never attempts that) — flagArgs() first, then Source,
// then Destination last, matching rsync's own documented SRC... DEST
// order.
func (j Job) Args() []string {
	args := j.flagArgs()
	return append(args, j.sourceArg(), j.Destination.String())
}

// Command renders Job as one real, executable shell command line — a
// live preview so the user sees exactly what they're about to launch
// before it does anything, per this project's own "never run something
// consequential without showing what it is first" principle (see e.g.
// the remote-archive confirm dialog's own identical reasoning), and
// also literally what gets run (via `sh -c`), so the preview can never
// drift from reality the way maintaining two separate representations
// would risk.
//
// Every flagArgs() entry is individually shell-quoted; ExtraArgs is
// then spliced in completely verbatim, never re-quoted or re-split —
// it's already exactly what the user typed, meant for a real shell to
// interpret exactly as intended, the same "hand it to sh -c, don't
// re-parse shell syntax by hand" principle this project's own
// runEditor already follows for $VISUAL/$EDITOR (see its own doc
// comment) — a hand-rolled shell-word-splitter is a well-known place
// to get subtly wrong. Source and Destination, both quoted, come last
// either way, matching rsync's own SRC... DEST order.
func (j Job) Command() string {
	flags := j.flagArgs()
	parts := make([]string, 0, len(flags)+3)
	parts = append(parts, "rsync")
	for _, a := range flags {
		parts = append(parts, shellQuote(a))
	}
	if j.ExtraArgs != "" {
		parts = append(parts, j.ExtraArgs)
	}
	parts = append(parts, shellQuote(j.sourceArg()), shellQuote(j.Destination.String()))
	return strings.Join(parts, " ")
}

// shellQuote wraps s in single quotes, the one quoting style that
// needs no escaping at all for every character except a single quote
// itself (POSIX sh has no way to include one inside a single-quoted
// string directly) — closes the quote, emits an escaped literal quote
// via a separate double-quoted segment, then reopens it, the standard
// sh idiom for this. An empty string still renders as "”" rather
// than vanishing entirely, so an empty --exclude= pattern or similar
// stays visible in the preview instead of silently disappearing.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
