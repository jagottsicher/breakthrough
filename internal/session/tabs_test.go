package session

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestStateDirUsesXDGStateHome pins StateDir's own resolution — the XDG
// State directory, matching the $XDG_DATA_HOME resolution dataDir already
// does for the persistent trash.
func TestStateDirUsesXDGStateHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	want := filepath.Join(dir, "breakthrough")
	if got := StateDir(); got != want {
		t.Errorf("StateDir() = %q, want %q", got, want)
	}
	if got, want := TabsPath(), filepath.Join(want, "tabs"); got != want {
		t.Errorf("TabsPath() = %q, want %q", got, want)
	}
}

// TestStateDirFallsBackToHome pins the ~/.local/state fallback for a
// system with no $XDG_STATE_HOME set at all — the common case on
// anything that isn't a freshly configured desktop session.
func TestStateDirFallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", home)

	want := filepath.Join(home, ".local", "state", "breakthrough")
	if got := StateDir(); got != want {
		t.Errorf("StateDir() = %q, want %q", got, want)
	}
}

// TestSaveLoadTabsRoundTrip is the core guarantee: whatever layout is
// saved on exit comes back identically on the next start, order included
// — the whole point of the file.
func TestSaveLoadTabsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tabs")
	want := TabState{
		Paths:  []string{"/home/jens", "/etc", "/var/log"},
		Active: 2,
	}

	if err := SaveTabs(path, want); err != nil {
		t.Fatalf("SaveTabs: %v", err)
	}
	got, err := LoadTabs(path)
	if err != nil {
		t.Fatalf("LoadTabs: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

// TestSaveTabsCreatesParentDirectory pins that a first-ever save works on
// a machine where the state directory doesn't exist yet — the case every
// fresh install hits exactly once.
func TestSaveTabsCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "tabs")

	if err := SaveTabs(path, TabState{Paths: []string{"/tmp"}}); err != nil {
		t.Fatalf("SaveTabs: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("saved file missing: %v", err)
	}
}

// TestSaveTabsIsPrivate pins the 0600 mode — a tab layout lists the
// directories this user has been browsing, which shouldn't be readable by
// other users on a shared machine.
func TestSaveTabsIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tabs")
	if err := SaveTabs(path, TabState{Paths: []string{"/tmp"}}); err != nil {
		t.Fatalf("SaveTabs: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("saved file mode = %o, want 600", perm)
	}
}

// TestLoadTabsMissingFileIsNotAnError pins the first-run case: no saved
// state yet is normal, not a failure the caller should have to special-
// case or surface to the user.
func TestLoadTabsMissingFileIsNotAnError(t *testing.T) {
	got, err := LoadTabs(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("LoadTabs on a missing file: %v", err)
	}
	if len(got.Paths) != 0 || got.Active != 0 {
		t.Errorf("LoadTabs on a missing file = %+v, want zero value", got)
	}
}

// TestLoadTabsSkipsMalformedLines pins the degrade-don't-fail contract:
// comments, blank lines, junk without an "=", and unknown keys are all
// ignored, and the real entries around them still load.
func TestLoadTabsSkipsMalformedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tabs")
	content := strings.Join([]string{
		"# a comment",
		"",
		"this line has no equals sign",
		"unknown_key = whatever",
		"active = 1",
		"path = /home/jens",
		"path = /etc",
		"path =",
		"   ",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	got, err := LoadTabs(path)
	if err != nil {
		t.Fatalf("LoadTabs: %v", err)
	}
	want := TabState{Paths: []string{"/home/jens", "/etc"}, Active: 1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadTabs = %+v, want %+v", got, want)
	}
}

// TestLoadTabsClampsOutOfRangeActive pins that a truncated or hand-edited
// file can never hand back an index that would panic the caller when used
// to select a tab.
func TestLoadTabsClampsOutOfRangeActive(t *testing.T) {
	for _, tc := range []struct {
		name   string
		active string
	}{
		{"past the end", "99"},
		{"negative", "-3"},
		{"not a number", "banana"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tabs")
			content := "active = " + tc.active + "\npath = /home/jens\npath = /etc\n"
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatalf("writing fixture: %v", err)
			}
			got, err := LoadTabs(path)
			if err != nil {
				t.Fatalf("LoadTabs: %v", err)
			}
			if got.Active != 0 {
				t.Errorf("Active = %d, want 0 (clamped)", got.Active)
			}
			if len(got.Paths) != 2 {
				t.Errorf("Paths = %v, want both entries kept", got.Paths)
			}
		})
	}
}

// TestSaveTabsSkipsPathsWithNewlines pins that a filename containing a
// newline — legal on POSIX, but unrepresentable in a line-oriented file —
// costs only its own tab instead of corrupting every entry after it.
func TestSaveTabsSkipsPathsWithNewlines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tabs")
	state := TabState{
		Paths: []string{"/home/jens", "/tmp/bad\nname", "/etc"},
	}
	if err := SaveTabs(path, state); err != nil {
		t.Fatalf("SaveTabs: %v", err)
	}

	got, err := LoadTabs(path)
	if err != nil {
		t.Fatalf("LoadTabs: %v", err)
	}
	want := []string{"/home/jens", "/etc"}
	if !reflect.DeepEqual(got.Paths, want) {
		t.Errorf("Paths = %v, want %v (the newline entry dropped, the rest intact)", got.Paths, want)
	}
}

// TestSaveTabsOverwritesPreviousLayout pins that saving replaces the
// previous layout rather than appending to it — otherwise the file would
// grow every run and restore tabs the user closed long ago.
func TestSaveTabsOverwritesPreviousLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tabs")
	if err := SaveTabs(path, TabState{Paths: []string{"/a", "/b", "/c"}, Active: 2}); err != nil {
		t.Fatalf("first SaveTabs: %v", err)
	}
	if err := SaveTabs(path, TabState{Paths: []string{"/only"}}); err != nil {
		t.Fatalf("second SaveTabs: %v", err)
	}

	got, err := LoadTabs(path)
	if err != nil {
		t.Fatalf("LoadTabs: %v", err)
	}
	want := TabState{Paths: []string{"/only"}, Active: 0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after overwrite = %+v, want %+v", got, want)
	}
}

// TestSaveTabsWithNoStateDirectory pins the "" path guard — StateDir
// returns "" on a system with neither $XDG_STATE_HOME nor a resolvable
// home, and neither call should panic there.
func TestSaveTabsWithNoStateDirectory(t *testing.T) {
	if err := SaveTabs("", TabState{Paths: []string{"/tmp"}}); err == nil {
		t.Error("SaveTabs(\"\") = nil, want an error")
	}
	got, err := LoadTabs("")
	if err != nil {
		t.Errorf("LoadTabs(\"\") = %v, want no error", err)
	}
	if len(got.Paths) != 0 {
		t.Errorf("LoadTabs(\"\") = %+v, want zero value", got)
	}
}
