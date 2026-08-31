package fsops

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

func TestResolveUIDNumeric(t *testing.T) {
	uid, err := ResolveUID("1000")
	if err != nil {
		t.Fatalf("ResolveUID(\"1000\"): %v", err)
	}
	if uid != 1000 {
		t.Errorf("uid = %d, want 1000", uid)
	}
}

func TestResolveGIDNumeric(t *testing.T) {
	gid, err := ResolveGID("1000")
	if err != nil {
		t.Fatalf("ResolveGID(\"1000\"): %v", err)
	}
	if gid != 1000 {
		t.Errorf("gid = %d, want 1000", gid)
	}
}

func TestResolveUIDUnknown(t *testing.T) {
	if _, err := ResolveUID("notauser12345"); err == nil {
		t.Error("ResolveUID should fail for a name that's neither a known user nor numeric")
	}
}

func TestParseOwnerGroup(t *testing.T) {
	tests := []struct {
		in      string
		wantUID int
		wantGID int
		wantErr bool
	}{
		{"1000", 1000, -1, false},        // owner only, numeric
		{":1000", -1, 1000, false},       // group only, numeric
		{"1000:1000", 1000, 1000, false}, // both, numeric
		{"", -1, -1, true},               // nothing at all
		{":", -1, -1, true},              // nothing at all, just the separator
		{"notauser12345", 0, 0, true},    // no such user, not numeric either
	}

	for _, tt := range tests {
		uid, gid, err := ParseOwnerGroup(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseOwnerGroup(%q) = (%d, %d), want an error", tt.in, uid, gid)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseOwnerGroup(%q): unexpected error %v", tt.in, err)
			continue
		}
		if uid != tt.wantUID || gid != tt.wantGID {
			t.Errorf("ParseOwnerGroup(%q) = (%d, %d), want (%d, %d)", tt.in, uid, gid, tt.wantUID, tt.wantGID)
		}
	}
}

// TestParseOwnerGroupResolvesCurrentUser checks name resolution (not just
// the numeric fallback TestParseOwnerGroup already covers) against the
// process's own username — the one account guaranteed to exist wherever
// this test runs, without depending on a fixture user.
func TestParseOwnerGroupResolvesCurrentUser(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skipf("could not determine current user: %v", err)
	}

	uid, gid, err := ParseOwnerGroup(u.Username)
	if err != nil {
		t.Fatalf("ParseOwnerGroup(%q): %v", u.Username, err)
	}
	if uid != os.Getuid() {
		t.Errorf("uid = %d, want %d (os.Getuid())", uid, os.Getuid())
	}
	if gid != -1 {
		t.Errorf("gid = %d, want -1 (no group given)", gid)
	}
}

// TestChownNoopToOwnUser pins that Chown reaches os.Chown correctly.
// Changing ownership to anyone else needs root, which this test
// environment may not have; chowning to the process's own current
// uid/gid is the one case guaranteed to succeed regardless of
// privileges.
func TestChownNoopToOwnUser(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Chown(path, os.Getuid(), os.Getgid()); err != nil {
		t.Errorf("Chown to own uid/gid should succeed without special privileges: %v", err)
	}
}

// TestChownRecursiveNoopToOwnUser is TestChownNoopToOwnUser's own
// counterpart for ChownRecursive: same "own uid/gid, no root needed"
// constraint, but over a small tree (top-level file, a subdirectory, and
// a file inside it) to confirm the walk actually reaches every entry,
// not just path itself.
func TestChownRecursiveNoopToOwnUser(t *testing.T) {
	dir := t.TempDir()
	topFile := filepath.Join(dir, "f.txt")
	subDir := filepath.Join(dir, "sub")
	subFile := filepath.Join(subDir, "g.txt")

	if err := os.WriteFile(topFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ChownRecursive(dir, os.Getuid(), os.Getgid()); err != nil {
		t.Errorf("ChownRecursive to own uid/gid should succeed without special privileges: %v", err)
	}

	for _, p := range []string{dir, topFile, subDir, subFile} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("stat %s after ChownRecursive: %v", p, err)
		}
	}
}
