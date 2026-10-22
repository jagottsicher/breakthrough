package mail

import (
	"os"
	"path/filepath"
	"testing"
)

func writeNetrc(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "netrc")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("NETRC", path)
}

func TestLookupNetrcFindsMatchingMachine(t *testing.T) {
	writeNetrc(t, `
machine imap.example.com
login alice
password s3cret
`)
	got, err := lookupNetrc("imap.example.com")
	if err != nil {
		t.Fatalf("lookupNetrc: %v", err)
	}
	if got.Login != "alice" || got.Password != "s3cret" {
		t.Errorf("got %+v, want login=alice password=s3cret", got)
	}
}

func TestLookupNetrcPicksTheRightEntryAmongSeveral(t *testing.T) {
	writeNetrc(t, `
machine other.example.com
login bob
password wrong

machine imap.example.com
login alice
password s3cret

machine third.example.com
login carol
password alsowrong
`)
	got, err := lookupNetrc("imap.example.com")
	if err != nil {
		t.Fatalf("lookupNetrc: %v", err)
	}
	if got.Login != "alice" || got.Password != "s3cret" {
		t.Errorf("got %+v, want login=alice password=s3cret", got)
	}
}

func TestLookupNetrcIsCaseSensitive(t *testing.T) {
	writeNetrc(t, `
machine imap.example.com
login alice
password s3cret
`)
	if _, err := lookupNetrc("IMAP.EXAMPLE.COM"); err == nil {
		t.Error("lookupNetrc matched a different-case machine name, want case-sensitive")
	}
}

func TestLookupNetrcFallsBackToDefault(t *testing.T) {
	writeNetrc(t, `
machine other.example.com
login bob
password wrong

default
login fallback-user
password fallback-pass
`)
	got, err := lookupNetrc("imap.example.com")
	if err != nil {
		t.Fatalf("lookupNetrc: %v", err)
	}
	if got.Login != "fallback-user" || got.Password != "fallback-pass" {
		t.Errorf("got %+v, want the default entry", got)
	}
}

func TestLookupNetrcNoMatchAndNoDefaultIsAnError(t *testing.T) {
	writeNetrc(t, `
machine other.example.com
login bob
password wrong
`)
	if _, err := lookupNetrc("imap.example.com"); err == nil {
		t.Error("lookupNetrc should have failed for an unlisted machine with no default entry")
	}
}

func TestLookupNetrcMissingFileIsAnError(t *testing.T) {
	t.Setenv("NETRC", filepath.Join(t.TempDir(), "does-not-exist"))
	if _, err := lookupNetrc("imap.example.com"); err == nil {
		t.Error("lookupNetrc should have failed for a missing file")
	}
}

// TestLookupNetrcSkipsMacdefBody pins the one real correctness risk in
// a from-scratch .netrc tokenizer: a macdef block's own body is
// arbitrary shell script text, not credential tokens — a line like
// "machine fake" inside one must never be misread as a real entry, and
// the real entry after the macro's trailing blank line must still be
// found correctly.
func TestLookupNetrcSkipsMacdefBody(t *testing.T) {
	writeNetrc(t, `
macdef init
machine fake-from-inside-macro
login should-never-match
password should-never-match

machine imap.example.com
login alice
password s3cret
`)
	got, err := lookupNetrc("imap.example.com")
	if err != nil {
		t.Fatalf("lookupNetrc: %v", err)
	}
	if got.Login != "alice" || got.Password != "s3cret" {
		t.Errorf("got %+v, want login=alice password=s3cret", got)
	}
	if _, err := lookupNetrc("fake-from-inside-macro"); err == nil {
		t.Error("a machine name inside a macdef body should never be a real entry")
	}
}

func TestLookupNetrcSingleLineEntry(t *testing.T) {
	writeNetrc(t, `machine imap.example.com login alice password s3cret`)
	got, err := lookupNetrc("imap.example.com")
	if err != nil {
		t.Fatalf("lookupNetrc: %v", err)
	}
	if got.Login != "alice" || got.Password != "s3cret" {
		t.Errorf("got %+v, want login=alice password=s3cret", got)
	}
}
