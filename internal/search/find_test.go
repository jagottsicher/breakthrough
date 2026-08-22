package search

import (
	"reflect"
	"testing"
)

func TestFindArgsGlob(t *testing.T) {
	got := FindArgs("linux", "/home/jens", "*.go", ModeGlob)
	want := []string{"/home/jens", "-iname", "*.go", "-print0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindArgs glob = %v, want %v", got, want)
	}
}

func TestFindArgsKeywordWrapsWithWildcards(t *testing.T) {
	got := FindArgs("linux", "/home/jens", "report", ModeKeyword)
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
	got := FindArgs("linux", "/var/log", `.*\.log$`, ModeRegex)
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
		got := FindArgs(goos, "/var/log", `.*\.log$`, ModeRegex)
		want := []string{"-E", "/var/log", "-iregex", `.*\.log$`, "-print0"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FindArgs regex (%s) = %v, want %v", goos, got, want)
		}
	}
}
