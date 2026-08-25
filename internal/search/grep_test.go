package search

import (
	"reflect"
	"testing"
)

func TestGrepArgsKeywordUsesFixedString(t *testing.T) {
	got := GrepArgs("TODO", "/home/jens/project", ModeKeyword, nil, false, false, false)
	want := []string{"-r", "-n", "-I", "-H", "-i", "-F", "-e", "TODO", "/home/jens/project"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GrepArgs keyword = %v, want %v", got, want)
	}
}

// TestGrepArgsGlobTreatedAsKeyword pins that Glob has no meaning for
// file content (see GrepArgs' own doc comment) — treated the same as
// Keyword, a fixed-string match.
func TestGrepArgsGlobTreatedAsKeyword(t *testing.T) {
	got := GrepArgs("TODO", "/home/jens/project", ModeGlob, nil, false, false, false)
	want := []string{"-r", "-n", "-I", "-H", "-i", "-F", "-e", "TODO", "/home/jens/project"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GrepArgs glob = %v, want %v", got, want)
	}
}

func TestGrepArgsRegexUsesExtendedFlag(t *testing.T) {
	got := GrepArgs(`func \w+\(`, "/home/jens/project", ModeRegex, nil, false, false, false)
	want := []string{"-r", "-n", "-I", "-H", "-i", "-E", "-e", `func \w+\(`, "/home/jens/project"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GrepArgs regex = %v, want %v", got, want)
	}
}

func TestZgrepArgsSingleFileNoRecursion(t *testing.T) {
	got := ZgrepArgs("error", ModeKeyword, "/var/log/syslog.1.gz", false, false, false)
	want := []string{"-n", "-I", "-H", "-i", "-F", "-e", "error", "/var/log/syslog.1.gz"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ZgrepArgs = %v, want %v", got, want)
	}
}

// TestBzgrepArgsOmitsDashEForSVR4Workaround pins BzgrepArgs' one real
// difference from ZgrepArgs (see its own doc comment): pattern is a
// bare positional argument, never preceded by -e — this bzgrep build's
// own "-e forces egrep" workaround would otherwise conflict with -F/-E.
func TestBzgrepArgsOmitsDashEForSVR4Workaround(t *testing.T) {
	got := BzgrepArgs("error", ModeKeyword, "/var/log/syslog.1.bz2", false, false, false)
	want := []string{"-n", "-I", "-H", "-i", "-F", "error", "/var/log/syslog.1.bz2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BzgrepArgs = %v, want %v", got, want)
	}
}

// TestXzgrepArgsMatchesZgrepShape pins that XzgrepArgs is structurally
// identical to ZgrepArgs (see its own doc comment on why — verified
// against xzgrep's real script, not guessed).
func TestXzgrepArgsMatchesZgrepShape(t *testing.T) {
	got := XzgrepArgs("error", ModeKeyword, "/var/log/syslog.1.xz", false, false, false)
	want := []string{"-n", "-I", "-H", "-i", "-F", "-e", "error", "/var/log/syslog.1.xz"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("XzgrepArgs = %v, want %v", got, want)
	}
}

// TestZipgrepArgsRegexPassesPatternBare pins zipgrep's own real,
// undocumented constraint (verified by reading and running its script
// directly, not guessed — see ZipgrepArgs' own doc comment): the
// pattern must be a bare positional argument, never preceded by -e —
// its own -e handling is broken and errors out with "conflicting
// matchers specified".
func TestZipgrepArgsRegexPassesPatternBare(t *testing.T) {
	got := ZipgrepArgs("TODO", ModeRegex, "/home/jens/archive.zip", false, false, false)
	want := []string{"-n", "-i", "TODO", "/home/jens/archive.zip"}
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
	got := ZipgrepArgs("a.b*c", ModeKeyword, "/home/jens/archive.zip", false, false, false)
	want := []string{"-n", "-i", `a\.b\*c`, "/home/jens/archive.zip"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ZipgrepArgs keyword = %v, want %v", got, want)
	}
}

// TestGrepArgsCaseSensitiveOmitsIFlag pins that caseSensitive drops -i
// — unlike find/locate, grep's own default (no -i) is already
// case-sensitive, so caseSensitive=false is what adds -i here (see
// GrepArgs' own doc comment on why that's now the default, matching
// find/locate, rather than leaving grep as the one case-sensitive-by-
// default outlier).
func TestGrepArgsCaseSensitiveOmitsIFlag(t *testing.T) {
	got := GrepArgs("TODO", "/home/jens/project", ModeKeyword, nil, true, false, false)
	want := []string{"-r", "-n", "-I", "-H", "-F", "-e", "TODO", "/home/jens/project"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GrepArgs case-sensitive = %v, want %v", got, want)
	}
}

// TestGrepArgsWholeWordsAndFirstHit pins MC's own "Whole words"/"First
// hit" checkboxes: -w and -m 1, both real extensions confirmed present
// on every grep this app targets (see GrepArgs' own doc comment).
func TestGrepArgsWholeWordsAndFirstHit(t *testing.T) {
	got := GrepArgs("TODO", "/home/jens/project", ModeKeyword, nil, false, true, true)
	want := []string{"-r", "-n", "-I", "-H", "-i", "-w", "-m", "1", "-F", "-e", "TODO", "/home/jens/project"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GrepArgs whole-words+first-hit = %v, want %v", got, want)
	}
}

// TestGrepArgsIgnoreDirsAddsExcludeDirFlags pins a real user report: a
// plain content search (no Filename term to narrow the file list
// first) used to run through Ignore dirs' own value entirely
// unfiltered — GrepArgs never had anywhere to put it at all. One
// --exclude-dir=NAME per entry, ahead of the case-sensitivity/mode
// flags (order doesn't matter to grep itself, but matching where
// FindArgs' own -prune sits — right after the options that shape
// *how* the walk happens, before the ones shaping *what* counts as a
// match — keeps the two functions reading the same way).
func TestGrepArgsIgnoreDirsAddsExcludeDirFlags(t *testing.T) {
	got := GrepArgs("TODO", "/home/jens/project", ModeKeyword, []string{"node_modules", ".git"}, false, false, false)
	want := []string{"-r", "-n", "-I", "-H", "--exclude-dir=node_modules", "--exclude-dir=.git", "-i", "-F", "-e", "TODO", "/home/jens/project"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GrepArgs ignoreDirs = %v, want %v", got, want)
	}
}

// TestGrepArgsNilIgnoreDirsAddsNoExcludeFlags pins that a plain search
// (Ignore dirs left off, the common case) draws no --exclude-dir flags
// at all — nil is a safe, ordinary "nothing to exclude" rather than
// something GrepArgs needs special-cased.
func TestGrepArgsNilIgnoreDirsAddsNoExcludeFlags(t *testing.T) {
	got := GrepArgs("TODO", "/home/jens/project", ModeKeyword, nil, false, false, false)
	want := []string{"-r", "-n", "-I", "-H", "-i", "-F", "-e", "TODO", "/home/jens/project"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GrepArgs nil ignoreDirs = %v, want %v", got, want)
	}
}
