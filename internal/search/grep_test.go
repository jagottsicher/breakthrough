package search

import (
	"reflect"
	"testing"
)

func TestGrepArgsKeywordUsesFixedString(t *testing.T) {
	got := GrepArgs("TODO", "/home/jens/project", ModeKeyword)
	want := []string{"-r", "-n", "-I", "-H", "-F", "-e", "TODO", "/home/jens/project"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GrepArgs keyword = %v, want %v", got, want)
	}
}

// TestGrepArgsGlobTreatedAsKeyword pins that Glob has no meaning for
// file content (see GrepArgs' own doc comment) — treated the same as
// Keyword, a fixed-string match.
func TestGrepArgsGlobTreatedAsKeyword(t *testing.T) {
	got := GrepArgs("TODO", "/home/jens/project", ModeGlob)
	want := []string{"-r", "-n", "-I", "-H", "-F", "-e", "TODO", "/home/jens/project"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GrepArgs glob = %v, want %v", got, want)
	}
}

func TestGrepArgsRegexUsesExtendedFlag(t *testing.T) {
	got := GrepArgs(`func \w+\(`, "/home/jens/project", ModeRegex)
	want := []string{"-r", "-n", "-I", "-H", "-E", "-e", `func \w+\(`, "/home/jens/project"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GrepArgs regex = %v, want %v", got, want)
	}
}

func TestZgrepArgsSingleFileNoRecursion(t *testing.T) {
	got := ZgrepArgs("error", ModeKeyword, "/var/log/syslog.1.gz")
	want := []string{"-n", "-I", "-H", "-F", "-e", "error", "/var/log/syslog.1.gz"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ZgrepArgs = %v, want %v", got, want)
	}
}

// TestZipgrepArgsRegexPassesPatternBare pins zipgrep's own real,
// undocumented constraint (verified by reading and running its script
// directly, not guessed — see ZipgrepArgs' own doc comment): the
// pattern must be a bare positional argument, never preceded by -e —
// its own -e handling is broken and errors out with "conflicting
// matchers specified".
func TestZipgrepArgsRegexPassesPatternBare(t *testing.T) {
	got := ZipgrepArgs("TODO", ModeRegex, "/home/jens/archive.zip")
	want := []string{"-n", "TODO", "/home/jens/archive.zip"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ZipgrepArgs regex = %v, want %v", got, want)
	}
}

// TestZipgrepArgsKeywordEscapesPattern pins the other real zipgrep
// constraint: it always runs the pattern through egrep(1) internally
// (-E implied), so a fixed-string match can't ask for -F (conflicts
// with the implied -E) — escaped as a regex that can only match the
// literal text instead.
func TestZipgrepArgsKeywordEscapesPattern(t *testing.T) {
	got := ZipgrepArgs("a.b*c", ModeKeyword, "/home/jens/archive.zip")
	want := []string{"-n", `a\.b\*c`, "/home/jens/archive.zip"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ZipgrepArgs keyword = %v, want %v", got, want)
	}
}
