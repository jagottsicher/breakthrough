package search

import (
	"reflect"
	"testing"
)

func TestFindArgsGlob(t *testing.T) {
	got := FindArgs("linux", "/home/jens", "*.go", ModeGlob, nil, false, false, false)
	want := []string{"/home/jens", "-iname", "*.go", "-print0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindArgs glob = %v, want %v", got, want)
	}
}

func TestFindArgsKeywordWrapsWithWildcards(t *testing.T) {
	got := FindArgs("linux", "/home/jens", "report", ModeKeyword, nil, false, false, false)
	want := []string{"/home/jens", "-iname", "*report*", "-print0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindArgs keyword = %v, want %v", got, want)
	}
}

// TestFindArgsRegexLinuxUsesRegextype pins the GNU-specific requirement
// (verified against the findutils manual, not guessed): GNU find's own
// default regex dialect, with no -regextype given at all, is Emacs
// syntax — not POSIX ERE — so -regextype posix-extended must always be
// given explicitly for a "regex" toggle to mean what it says.
func TestFindArgsRegexLinuxUsesRegextype(t *testing.T) {
	got := FindArgs("linux", "/var/log", `.*\.log$`, ModeRegex, nil, false, false, false)
	want := []string{"/var/log", "-regextype", "posix-extended", "-iregex", `.*\.log$`, "-print0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindArgs regex (linux) = %v, want %v", got, want)
	}
}

// TestFindArgsRegexBSDUsesEFlag pins BSD find's own syntax (verified
// against the FreeBSD find(1) man page): -E, not -regextype, and
// placed before the search path, since BSD find has no -regextype at
// all — passing it would be a hard usage error there.
func TestFindArgsRegexBSDUsesEFlag(t *testing.T) {
	for _, goos := range []string{"darwin", "freebsd"} {
		got := FindArgs(goos, "/var/log", `.*\.log$`, ModeRegex, nil, false, false, false)
		want := []string{"-E", "/var/log", "-iregex", `.*\.log$`, "-print0"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FindArgs regex (%s) = %v, want %v", goos, got, want)
		}
	}
}

// TestFindArgsIgnoreDirsAddsPruneClause pins the -prune idiom (see
// FindArgs' own doc comment): one -name per ignored directory, OR'd
// together, pruned before the real test runs.
func TestFindArgsIgnoreDirsAddsPruneClause(t *testing.T) {
	got := FindArgs("linux", "/home/jens", "*.go", ModeGlob, []string{".git", "node_modules"}, false, false, false)
	want := []string{
		"/home/jens",
		"(", "-name", ".git", "-o", "-name", "node_modules", ")", "-prune", "-o",
		"-iname", "*.go", "-print0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindArgs with ignoreDirs = %v, want %v", got, want)
	}
}

// TestFindArgsIgnoreDirsSingleEntry pins the single-name case doesn't
// grow a pointless "-o" — just "( -name D )".
func TestFindArgsIgnoreDirsSingleEntry(t *testing.T) {
	got := FindArgs("linux", "/home/jens", "*.go", ModeGlob, []string{".git"}, false, false, false)
	want := []string{
		"/home/jens",
		"(", "-name", ".git", ")", "-prune", "-o",
		"-iname", "*.go", "-print0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindArgs with one ignoreDirs entry = %v, want %v", got, want)
	}
}

// TestFindArgsIgnoreDirsWithRegexBSDKeepsEFlagFirst pins that the
// -prune clause is inserted after -E/scope, not before — -E must stay
// the very first argument on BSD find (see FindArgs' own doc comment).
func TestFindArgsIgnoreDirsWithRegexBSDKeepsEFlagFirst(t *testing.T) {
	got := FindArgs("darwin", "/var/log", `.*\.log$`, ModeRegex, []string{"archive"}, false, false, false)
	want := []string{
		"-E", "/var/log",
		"(", "-name", "archive", ")", "-prune", "-o",
		"-iregex", `.*\.log$`, "-print0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindArgs with ignoreDirs (BSD regex) = %v, want %v", got, want)
	}
}

// TestFindArgsCaseSensitiveSwitchesFlags pins that caseSensitive swaps
// -iname/-iregex for -name/-regex, both modes, both regex dialects.
func TestFindArgsCaseSensitiveSwitchesFlags(t *testing.T) {
	got := FindArgs("linux", "/home/jens", "*.go", ModeGlob, nil, true, false, false)
	want := []string{"/home/jens", "-name", "*.go", "-print0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindArgs glob, case-sensitive = %v, want %v", got, want)
	}

	got = FindArgs("linux", "/var/log", `.*\.log$`, ModeRegex, nil, true, false, false)
	want = []string{"/var/log", "-regextype", "posix-extended", "-regex", `.*\.log$`, "-print0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindArgs regex, case-sensitive = %v, want %v", got, want)
	}
}

// TestFindArgsNonRecursiveAddsMaxdepth pins MC's own "Find recursively"
// checkbox, inverted (see Request.NonRecursive's own doc comment on
// why): -maxdepth 1 right after scope, before any -prune clause.
func TestFindArgsNonRecursiveAddsMaxdepth(t *testing.T) {
	got := FindArgs("linux", "/home/jens", "*.go", ModeGlob, nil, false, true, false)
	want := []string{"/home/jens", "-maxdepth", "1", "-iname", "*.go", "-print0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindArgs non-recursive = %v, want %v", got, want)
	}
}

// TestFindArgsFollowSymlinksAddsLFlagFirst pins MC's own "Follow
// symlinks" checkbox: -L, which — like -E for BSD regex mode — must
// precede the scope path.
func TestFindArgsFollowSymlinksAddsLFlagFirst(t *testing.T) {
	got := FindArgs("linux", "/home/jens", "*.go", ModeGlob, nil, false, false, true)
	want := []string{"-L", "/home/jens", "-iname", "*.go", "-print0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindArgs follow-symlinks = %v, want %v", got, want)
	}
}
