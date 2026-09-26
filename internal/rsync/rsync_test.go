package rsync

import (
	"os/exec"
	"strings"
	"testing"
)

func TestEndpointStringLocalIsJustThePath(t *testing.T) {
	e := Endpoint{Path: "/home/jens/projects"}
	if got, want := e.String(), "/home/jens/projects"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if e.IsRemote() {
		t.Error("IsRemote() = true for an endpoint with no Host")
	}
}

func TestEndpointStringRemoteWithUser(t *testing.T) {
	e := Endpoint{Host: "example.com", User: "jens", Path: "/srv/data"}
	if got, want := e.String(), "jens@example.com:/srv/data"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if !e.IsRemote() {
		t.Error("IsRemote() = false for an endpoint with a Host")
	}
}

func TestEndpointStringRemoteWithoutUser(t *testing.T) {
	e := Endpoint{Host: "example.com", Path: "/srv/data"}
	if got, want := e.String(), "example.com:/srv/data"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestSourceArgCopyContentsAppendsTrailingSlash pins the exact
// behavior verified directly against a real rsync run (rsync -a src/
// dst/ copies src's own children into dst; rsync -a src dst/ creates
// dst/src instead) — this package's own CopyContents flag exists
// specifically to make that choice explicit rather than depending on
// whether the user happened to type a trailing slash themselves.
func TestSourceArgCopyContentsAppendsTrailingSlash(t *testing.T) {
	j := Job{Source: Endpoint{Path: "/data/src"}, CopyContents: true}
	if got, want := j.sourceArg(), "/data/src/"; got != want {
		t.Errorf("sourceArg() = %q, want %q", got, want)
	}
}

func TestSourceArgWithoutCopyContentsHasNoTrailingSlash(t *testing.T) {
	j := Job{Source: Endpoint{Path: "/data/src"}, CopyContents: false}
	if got, want := j.sourceArg(), "/data/src"; got != want {
		t.Errorf("sourceArg() = %q, want %q", got, want)
	}
}

// TestSourceArgCopyContentsOnAnAlreadySlashedPathDoesNotDouble pins a
// small edge case: a source path someone already typed with its own
// trailing slash must not end up with two.
func TestSourceArgCopyContentsOnAnAlreadySlashedPathDoesNotDouble(t *testing.T) {
	j := Job{Source: Endpoint{Path: "/data/src/"}, CopyContents: true}
	if got, want := j.sourceArg(), "/data/src/"; got != want {
		t.Errorf("sourceArg() = %q, want %q (no doubled slash)", got, want)
	}
}

func TestArgsBuildsFlagsInOrderThenSourceThenDestination(t *testing.T) {
	j := Job{
		Source:      Endpoint{Path: "/src"},
		Destination: Endpoint{Path: "/dst"},
		Archive:     true,
		Compress:    true,
		Progress:    true,
	}
	got := j.Args()
	want := []string{"-a", "-z", "--info=progress2", "/src", "/dst"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("Args() = %v, want %v", got, want)
	}
}

func TestArgsOmitsEveryFlagThatIsOff(t *testing.T) {
	j := Job{Source: Endpoint{Path: "/src"}, Destination: Endpoint{Path: "/dst"}}
	got := j.Args()
	want := []string{"/src", "/dst"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("Args() = %v, want %v (no flags at all)", got, want)
	}
}

func TestArgsIncludesOneExcludeFlagPerPattern(t *testing.T) {
	j := Job{
		Source:      Endpoint{Path: "/src"},
		Destination: Endpoint{Path: "/dst"},
		Excludes:    []string{"*.log", "node_modules"},
	}
	got := j.Args()
	want := []string{"--exclude=*.log", "--exclude=node_modules", "/src", "/dst"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("Args() = %v, want %v", got, want)
	}
}

func TestArgsAddsRshWithPortWhenDestinationHasANonDefaultPort(t *testing.T) {
	j := Job{
		Source:      Endpoint{Path: "/src"},
		Destination: Endpoint{Host: "example.com", Port: 2222, Path: "/dst"},
	}
	got := j.Args()
	want := []string{"-e", "ssh -p 2222", "/src", "example.com:/dst"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("Args() = %v, want %v", got, want)
	}
}

func TestArgsPrefersSourcePortWhenBothEndpointsAreRemoteWithDifferentPorts(t *testing.T) {
	// A real, documented limitation (see rsh's own doc comment): rsync
	// has exactly one -e flag, so remote-to-remote with two different
	// non-standard ports can't be expressed in full — Source's own
	// port wins.
	j := Job{
		Source:      Endpoint{Host: "a.example.com", Port: 2222, Path: "/src"},
		Destination: Endpoint{Host: "b.example.com", Port: 3333, Path: "/dst"},
	}
	got := j.Args()
	want := []string{"-e", "ssh -p 2222", "a.example.com:/src", "b.example.com:/dst"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("Args() = %v, want %v", got, want)
	}
}

func TestArgsOmitsRshWhenNeitherEndpointHasANonDefaultPort(t *testing.T) {
	j := Job{
		Source:      Endpoint{Host: "example.com", Path: "/src"},
		Destination: Endpoint{Path: "/dst"},
	}
	for _, a := range j.Args() {
		if a == "-e" {
			t.Fatalf("Args() = %v, want no -e flag when no port is non-default", j.Args())
		}
	}
}

func TestCommandProducesAShellQuotedLineMatchingArgs(t *testing.T) {
	j := Job{
		Source:      Endpoint{Path: "/data/my src"},
		Destination: Endpoint{Host: "example.com", User: "jens", Path: "/backup"},
		Archive:     true,
		DryRun:      true,
	}
	got := j.Command()
	want := "rsync '-a' '-n' '/data/my src' 'jens@example.com:/backup'"
	if got != want {
		t.Errorf("Command() = %q, want %q", got, want)
	}
}

// TestCommandSplicesExtraArgsVerbatimBetweenFlagsAndPositionals pins
// that a free-typed extra-flags fragment is neither re-quoted nor
// re-split — it's handed to a real shell exactly as the user wrote
// it, the same "sh -c does the parsing, not hand-rolled Go code"
// principle runEditor already follows for $VISUAL/$EDITOR.
func TestCommandSplicesExtraArgsVerbatimBetweenFlagsAndPositionals(t *testing.T) {
	j := Job{
		Source:      Endpoint{Path: "/src"},
		Destination: Endpoint{Path: "/dst"},
		Archive:     true,
		ExtraArgs:   `--exclude='*.log' --bwlimit=500`,
	}
	got := j.Command()
	want := "rsync '-a' --exclude='*.log' --bwlimit=500 '/src' '/dst'"
	if got != want {
		t.Errorf("Command() = %q, want %q", got, want)
	}
}

func TestShellQuoteEscapesEmbeddedSingleQuotes(t *testing.T) {
	got := shellQuote(`it's a test`)
	want := `'it'"'"'s a test'`
	if got != want {
		t.Errorf("shellQuote(%q) = %q, want %q", `it's a test`, got, want)
	}
}

func TestShellQuoteOnAnEmptyStringStillProducesQuotes(t *testing.T) {
	if got, want := shellQuote(""), "''"; got != want {
		t.Errorf("shellQuote(\"\") = %q, want %q", got, want)
	}
}

// FuzzShellQuote is this package's own defense against exactly the kind
// of bug a hand-picked test corpus is worst at catching: an unusual
// filename (embedded backticks, a leading "-", a "$(...)" substitution,
// a literal newline — all real, legal POSIX filenames) that shellQuote
// escapes wrong, turning a Job's own preview/Command() into something a
// real shell doesn't interpret as the same literal string it started
// from. The property under test is round-trip fidelity against a real
// shell, not just "doesn't panic": for any s a real path could ever be,
// decoding shellQuote(s) back through /bin/sh must reproduce s exactly.
// A real path can never contain a NUL byte (no POSIX filesystem allows
// one), so that one input is skipped rather than asserted on — the one
// case shellQuote was never meant to handle.
func FuzzShellQuote(f *testing.F) {
	for _, seed := range []string{
		"simple.txt", "it's a test", "", " leading and trailing ",
		"$(rm -rf /)", "`backticks`", "a\nb", "a\tb", "--flag-looking",
		`"double quotes"`, "\\backslash", "*.log", "~/home",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if strings.ContainsRune(s, 0) {
			t.Skip("a real path can never contain a NUL byte")
		}
		quoted := shellQuote(s)
		out, err := exec.Command("sh", "-c", "printf %s "+quoted).Output()
		if err != nil {
			t.Fatalf("shell rejected shellQuote(%q) = %q: %v", s, quoted, err)
		}
		if string(out) != s {
			t.Errorf("round-trip mismatch: shellQuote(%q) = %q, shell decoded it back to %q", s, quoted, out)
		}
	})
}
