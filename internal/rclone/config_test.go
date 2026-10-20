package rclone

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// withUserDir points config.UserDir (via XDG_CONFIG_HOME) at a fresh
// temp directory for the duration of t — the same isolation
// remotefs's own history tests already rely on, just via Setenv
// instead of a package-level override, since config.UserDir has none.
func withUserDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

// TestLoadMountEntriesMissingFileIsNotAnError pins
// LoadMountEntries' own "nothing configured yet" contract, the same
// one remotefs.LoadHistory already establishes for connections.json.
func TestLoadMountEntriesMissingFileIsNotAnError(t *testing.T) {
	withUserDir(t)
	entries, err := LoadMountEntries()
	if err != nil {
		t.Fatalf("LoadMountEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

// TestAddMountEntryPersistsAndRoundTrips pins the basic write/read
// cycle.
func TestAddMountEntryPersistsAndRoundTrips(t *testing.T) {
	withUserDir(t)
	entry := MountEntry{RemoteName: "mys3", Target: "/home/user/.local/state/breakthrough/mounts/mys3", Identifier: "build bucket"}
	if err := AddMountEntry(entry); err != nil {
		t.Fatalf("AddMountEntry: %v", err)
	}

	got, err := LoadMountEntries()
	if err != nil {
		t.Fatalf("LoadMountEntries: %v", err)
	}
	if len(got) != 1 || !reflect.DeepEqual(got[0], entry) {
		t.Errorf("got %+v, want [%+v]", got, entry)
	}
}

// TestAddMountEntryRefusesDuplicateTarget pins that two cloud mounts
// can never silently share one local mountpoint.
func TestAddMountEntryRefusesDuplicateTarget(t *testing.T) {
	withUserDir(t)
	first := MountEntry{RemoteName: "mys3", Target: "/mnt/shared"}
	if err := AddMountEntry(first); err != nil {
		t.Fatalf("AddMountEntry: %v", err)
	}

	second := MountEntry{RemoteName: "myazure", Target: "/mnt/shared"}
	if err := AddMountEntry(second); err == nil {
		t.Error("AddMountEntry should refuse a duplicate Target")
	}

	got, err := LoadMountEntries()
	if err != nil {
		t.Fatalf("LoadMountEntries: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d entries after refused duplicate, want 1 (unchanged)", len(got))
	}
}

// TestRemoveMountEntryAbsentIsNotAnError mirrors
// remotefs.RemoveFromHistory's own "absence isn't an error" contract.
func TestRemoveMountEntryAbsentIsNotAnError(t *testing.T) {
	withUserDir(t)
	if err := RemoveMountEntry("/not/configured"); err != nil {
		t.Errorf("RemoveMountEntry on an absent target should not error, got %v", err)
	}
}

// TestRemoveMountEntryDropsOnlyTheMatchingTarget pins that removal is
// scoped to one entry, not the whole list.
func TestRemoveMountEntryDropsOnlyTheMatchingTarget(t *testing.T) {
	withUserDir(t)
	if err := AddMountEntry(MountEntry{RemoteName: "mys3", Target: "/mnt/a"}); err != nil {
		t.Fatalf("AddMountEntry: %v", err)
	}
	if err := AddMountEntry(MountEntry{RemoteName: "myazure", Target: "/mnt/b"}); err != nil {
		t.Fatalf("AddMountEntry: %v", err)
	}

	if err := RemoveMountEntry("/mnt/a"); err != nil {
		t.Fatalf("RemoveMountEntry: %v", err)
	}

	got, err := LoadMountEntries()
	if err != nil {
		t.Fatalf("LoadMountEntries: %v", err)
	}
	if len(got) != 1 || got[0].Target != "/mnt/b" {
		t.Errorf("got %+v, want only /mnt/b left", got)
	}
}

// TestMountBasePrefersExplicitSetting pins MountBase's own precedence:
// an explicit config.Settings.CloudMountBaseDir always wins over the
// computed default.
func TestMountBasePrefersExplicitSetting(t *testing.T) {
	got := MountBase(config.Settings{CloudMountBaseDir: "/srv/breakthrough-mounts"})
	if got != "/srv/breakthrough-mounts" {
		t.Errorf("MountBase = %q, want the explicit setting", got)
	}
}

// TestMountBaseFallsBackToComputedDefault pins that a blank setting
// falls back to DefaultMountBase, never to a literal empty path.
func TestMountBaseFallsBackToComputedDefault(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	got := MountBase(config.Settings{CloudMountBaseDir: ""})
	want := DefaultMountBase()
	if got != want || got == "" {
		t.Errorf("MountBase(blank) = %q, want %q (non-empty)", got, want)
	}
}

// TestMountDirJoinsBaseAndName pins the one place a remote's own
// mountpoint path is computed, so every caller agrees on it.
func TestMountDirJoinsBaseAndName(t *testing.T) {
	got := MountDir("/base", "mys3")
	want := filepath.Join("/base", "mys3")
	if got != want {
		t.Errorf("MountDir = %q, want %q", got, want)
	}
}

// TestEnsureMountDirCreatesAFreshDirectory pins the common case: the
// target doesn't exist yet, so it's created outright.
func TestEnsureMountDirCreatesAFreshDirectory(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "fresh")
	if err := EnsureMountDir(target); err != nil {
		t.Fatalf("EnsureMountDir: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		t.Errorf("EnsureMountDir should have created %q as a directory", target)
	}
}

// TestEnsureMountDirAcceptsAnAlreadyEmptyDirectory pins the
// pre-created (e.g. root-owned, under /mnt) case: already existing
// and empty is accepted as is, never recreated or modified.
func TestEnsureMountDirAcceptsAnAlreadyEmptyDirectory(t *testing.T) {
	target := t.TempDir()
	if err := EnsureMountDir(target); err != nil {
		t.Errorf("EnsureMountDir should accept an existing empty directory, got %v", err)
	}
}

// TestEnsureMountDirRefusesANonEmptyDirectory pins the core safety
// property: never silently mount over a directory that already has
// real files in it.
func TestEnsureMountDirRefusesANonEmptyDirectory(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "existing-file.txt"), []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := EnsureMountDir(target); err == nil {
		t.Error("EnsureMountDir should refuse a non-empty existing directory")
	}
}

// TestEnsureMountDirRefusesAFile pins that a target that exists but is
// a plain file (not a directory at all) is reported, not silently
// treated as usable.
func TestEnsureMountDirRefusesAFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(target, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := EnsureMountDir(target); err == nil {
		t.Error("EnsureMountDir should refuse a target that already exists as a plain file")
	}
}

// TestCreateStaticRemoteReturnsErrNotAvailableWithoutRclone mirrors
// TestListRemotesReturnsErrNotAvailableWithoutRclone for this
// function.
func TestCreateStaticRemoteReturnsErrNotAvailableWithoutRclone(t *testing.T) {
	old := lookPath
	defer func() { lookPath = old }()
	lookPath = func(string) (string, error) { return "", errors.New("not found") }

	if err := CreateStaticRemote("mys3", "s3", nil); !errors.Is(err, ErrNotAvailable) {
		t.Errorf("CreateStaticRemote() error = %v, want ErrNotAvailable", err)
	}
}

// TestCreateStaticRemoteRefusesAnExistingName pins the "no silent
// overwrite of rclone's own config" rule — ListRemotes already
// reporting the name is enough to refuse, no config create call ever
// attempted.
func TestCreateStaticRemoteRefusesAnExistingName(t *testing.T) {
	oldLookPath, oldRunListRemotes, oldRunConfigCreate := lookPath, runListRemotes, runConfigCreate
	defer func() { lookPath, runListRemotes, runConfigCreate = oldLookPath, oldRunListRemotes, oldRunConfigCreate }()
	lookPath = func(string) (string, error) { return "/usr/bin/rclone", nil }
	runListRemotes = func() ([]byte, error) {
		return []byte(`[{"name":"mys3","type":"s3","source":"file","description":""}]`), nil
	}
	calledConfigCreate := false
	runConfigCreate = func(args []string) error {
		calledConfigCreate = true
		return nil
	}

	if err := CreateStaticRemote("mys3", "s3", []string{"access_key_id=AKIA"}); err == nil {
		t.Error("CreateStaticRemote should refuse an already-existing remote name")
	}
	if calledConfigCreate {
		t.Error("CreateStaticRemote should never reach rclone config create for an existing name")
	}
}

// TestCreateStaticRemotePassesArgsInOrder pins the exact argv shape
// for a brand-new remote, including that params are appended verbatim
// and in order (not re-sorted as a map would).
func TestCreateStaticRemotePassesArgsInOrder(t *testing.T) {
	oldLookPath, oldRunListRemotes, oldRunConfigCreate := lookPath, runListRemotes, runConfigCreate
	defer func() { lookPath, runListRemotes, runConfigCreate = oldLookPath, oldRunListRemotes, oldRunConfigCreate }()
	lookPath = func(string) (string, error) { return "/usr/bin/rclone", nil }
	runListRemotes = func() ([]byte, error) { return []byte(`[]`), nil }

	var gotArgs []string
	runConfigCreate = func(args []string) error {
		gotArgs = args
		return nil
	}

	err := CreateStaticRemote("mys3", "s3", []string{"access_key_id=AKIA", "secret_access_key=secret", "endpoint=https://minio.example.com"})
	if err != nil {
		t.Fatalf("CreateStaticRemote: %v", err)
	}

	want := []string{"config", "create", "mys3", "s3", "--non-interactive", "access_key_id=AKIA", "secret_access_key=secret", "endpoint=https://minio.example.com"}
	if len(gotArgs) != len(want) {
		t.Fatalf("got args %v, want %v", gotArgs, want)
	}
	for i, w := range want {
		if gotArgs[i] != w {
			t.Errorf("arg %d = %q, want %q (full: %v)", i, gotArgs[i], w, gotArgs)
		}
	}
}

// TestCreateStaticRemoteRealRcloneRoundTrip exercises CreateStaticRemote
// end to end against a real rclone binary, skipped where it isn't
// installed — not simulated, same reasoning
// TestIsMountedDetectsARealRcloneFUSEMount already gives for its own
// real mount. The "local" backend needs no credentials at all, so this
// stays fast and fully offline while still confirming the real
// subprocess, the real config file write, and ListRemotes' own
// round-trip all actually work together.
func TestCreateStaticRemoteRealRcloneRoundTrip(t *testing.T) {
	requireCommand(t, "rclone")

	confDir := t.TempDir()
	t.Setenv("RCLONE_CONFIG", confDir+"/rclone.conf")

	if err := CreateStaticRemote("breakthrough-test-local", "local", nil); err != nil {
		t.Fatalf("CreateStaticRemote: %v", err)
	}

	remotes, err := ListRemotes()
	if err != nil {
		t.Fatalf("ListRemotes: %v", err)
	}
	found := false
	for _, r := range remotes {
		if r.Name == "breakthrough-test-local" {
			found = true
			if r.Type != "local" {
				t.Errorf("remote type = %q, want %q", r.Type, "local")
			}
		}
	}
	if !found {
		t.Errorf("ListRemotes = %+v, want it to include the just-created remote", remotes)
	}

	// A second CreateStaticRemote for the same name must refuse, not
	// overwrite — the exact behavior TestCreateStaticRemoteRefusesAnExistingName
	// already pins with a mocked ListRemotes; this confirms the same
	// contract holds against the real one too.
	if err := CreateStaticRemote("breakthrough-test-local", "local", nil); err == nil {
		t.Error("CreateStaticRemote should refuse a name that already exists, even against the real rclone config")
	}
}
