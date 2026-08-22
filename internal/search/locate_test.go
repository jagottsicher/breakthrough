package search

import (
	"reflect"
	"testing"
)

// TestLocateArgsGlobLinuxAddsBasename pins the deliberate consistency
// choice (see LocateArgs' own doc comment): locate's own default match
// scope is the whole path, but Glob/Keyword mode adds -b so it behaves
// like find's -iname (filename-only) instead — matching whichever
// Engine the search dialog has selected shouldn't change what a Glob
// pattern matches against.
func TestLocateArgsGlobLinuxAddsBasename(t *testing.T) {
	got, ok := LocateArgs("linux", "*.go", ModeGlob)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	want := []string{"-0", "-i", "-b", "*.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LocateArgs glob (linux) = %v, want %v", got, want)
	}
}

// TestLocateArgsGlobBSDHasNoBasenameFlag pins the platform difference
// (verified against the FreeBSD locate(1) man page's own full flag
// list, which has no -b at all): BSD locate falls back to its own
// native whole-path matching for Glob/Keyword, since there's no flag
// to ask for anything narrower.
func TestLocateArgsGlobBSDHasNoBasenameFlag(t *testing.T) {
	for _, goos := range []string{"darwin", "freebsd"} {
		got, ok := LocateArgs(goos, "*.go", ModeGlob)
		if !ok {
			t.Fatalf("(%s) ok = false, want true", goos)
		}
		want := []string{"-0", "-i", "*.go"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("LocateArgs glob (%s) = %v, want %v", goos, got, want)
		}
	}
}

func TestLocateArgsKeywordWrapsWithWildcards(t *testing.T) {
	got, ok := LocateArgs("linux", "report", ModeKeyword)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	want := []string{"-0", "-i", "-b", "*report*"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LocateArgs keyword = %v, want %v", got, want)
	}
}

func TestLocateArgsRegexLinux(t *testing.T) {
	got, ok := LocateArgs("linux", `.*\.log$`, ModeRegex)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	want := []string{"-0", "-i", "--regex", `.*\.log$`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LocateArgs regex (linux) = %v, want %v", got, want)
	}
}

// TestLocateArgsRegexBSDUnavailable pins that BSD locate (macOS,
// FreeBSD) has no regex support of any kind (verified against the
// FreeBSD locate(1) man page) — LocateArgs must report that rather
// than build a command that would just fail with a usage error.
func TestLocateArgsRegexBSDUnavailable(t *testing.T) {
	for _, goos := range []string{"darwin", "freebsd"} {
		if _, ok := LocateArgs(goos, `.*\.log$`, ModeRegex); ok {
			t.Errorf("(%s) ok = true, want false — BSD locate has no regex support", goos)
		}
	}
}
