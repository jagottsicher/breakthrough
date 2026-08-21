//go:build unix && !darwin

package fsops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestListUsers exercises the real /etc/passwd on whatever machine runs
// this test — every unix system this project targets (other than macOS)
// has at least root in it, so a non-empty, sorted result is a safe thing
// to assert without depending on any other specific account existing.
func TestListUsers(t *testing.T) {
	users, err := ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) == 0 {
		t.Fatal("ListUsers returned no users, want at least root")
	}

	foundRoot := false
	for _, u := range users {
		if u.Name == "root" && u.UID == 0 {
			foundRoot = true
		}
	}
	if !foundRoot {
		t.Error("root (uid 0) should be in the list")
	}

	for i := 1; i < len(users); i++ {
		if strings.ToLower(users[i-1].Name) > strings.ToLower(users[i].Name) {
			t.Fatalf("not sorted at index %d: %q > %q", i, users[i-1].Name, users[i].Name)
		}
	}
}

// TestListGroups is ListUsers' own test, but for /etc/group — every
// system has at least one group with gid 0 (root or wheel, depending on
// the OS), so this checks sortedness and non-emptiness without assuming
// a specific group name.
func TestListGroups(t *testing.T) {
	groups, err := ListGroups()
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(groups) == 0 {
		t.Fatal("ListGroups returned no groups, want at least one")
	}

	foundGID0 := false
	for _, g := range groups {
		if g.GID == 0 {
			foundGID0 = true
		}
	}
	if !foundGID0 {
		t.Error("a group with gid 0 (root/wheel) should be in the list")
	}

	for i := 1; i < len(groups); i++ {
		if strings.ToLower(groups[i-1].Name) > strings.ToLower(groups[i].Name) {
			t.Fatalf("not sorted at index %d: %q > %q", i, groups[i-1].Name, groups[i].Name)
		}
	}
}

func TestParsePasswdLikeSkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "passwd-like")
	content := "good:x:100:200:desc:/home/good:/bin/sh\n" +
		"\n" +
		"# a comment\n" +
		"toofewfields:x\n" +
		"badid:x:notanumber:0::/bin/sh\n" +
		"another:x:50:60::/home/another:/bin/sh\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := parsePasswdLike(path)
	if err != nil {
		t.Fatalf("parsePasswdLike: %v", err)
	}

	want := map[string]int{"good": 100, "another": 50}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(entries), len(want), entries)
	}
	for _, e := range entries {
		if want[e.name] != e.id {
			t.Errorf("entry %q id = %d, want %d", e.name, e.id, want[e.name])
		}
	}
}
