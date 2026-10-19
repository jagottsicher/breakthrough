package rclone

import (
	"errors"
	"os/exec"
	"testing"
	"time"
)

// requireCommand skips t unless name is on $PATH — the same small
// helper internal/mounts/internal/fsops/internal/ui already keep their
// own copy of.
func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available in this environment: %v", name, err)
	}
}

// TestParseListRemotesJSONReadsNameAndType pins the shape this package
// depends on — captured verbatim from a real `rclone listremotes
// --long --json` run (rclone v1.75.2) against a throwaway config with
// an s3, a webdav, and an ftp remote, not guessed from documentation
// (rclone's own docs show no example output for this command at all).
func TestParseListRemotesJSONReadsNameAndType(t *testing.T) {
	const data = `[{"name":"mys3","type":"s3","source":"file","description":""},{"name":"mywebdav","type":"webdav","source":"file","description":""},{"name":"myftp","type":"ftp","source":"file","description":""}]`

	got, err := parseListRemotesJSON([]byte(data))
	if err != nil {
		t.Fatalf("parseListRemotesJSON: %v", err)
	}

	want := []Remote{
		{Name: "mys3", Type: "s3"},
		{Name: "mywebdav", Type: "webdav"},
		{Name: "myftp", Type: "ftp"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d remotes, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("remote %d = %+v, want %+v", i, got[i], w)
		}
	}
}

// TestParseListRemotesJSONEmptyConfig pins that no remotes configured
// at all — rclone's own output is a bare "[]" — reads back as an empty
// slice, not an error or a nil/empty ambiguity.
func TestParseListRemotesJSONEmptyConfig(t *testing.T) {
	got, err := parseListRemotesJSON([]byte(`[]`))
	if err != nil {
		t.Fatalf("parseListRemotesJSON: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d remotes, want 0", len(got))
	}
}

// TestParseListRemotesJSONInvalidReturnsError pins that malformed
// input is reported rather than silently producing an empty (and
// therefore misleadingly "no remotes configured") list.
func TestParseListRemotesJSONInvalidReturnsError(t *testing.T) {
	if _, err := parseListRemotesJSON([]byte("not json")); err == nil {
		t.Error("parseListRemotesJSON should report an error for invalid JSON")
	}
}

// TestAvailableFalseWhenRcloneMissing pins Available's own degrade-
// gracefully contract: a PATH lookup failure means false, never a
// panic or a surfaced error — the caller just checks a bool.
func TestAvailableFalseWhenRcloneMissing(t *testing.T) {
	old := lookPath
	defer func() { lookPath = old }()
	lookPath = func(string) (string, error) { return "", errors.New("not found") }

	if Available() {
		t.Error("Available should be false when the rclone binary can't be found")
	}
}

// TestListRemotesReturnsErrNotAvailableWithoutRclone pins that every
// exported function here fails fast with ErrNotAvailable rather than
// attempting a command that could only ever fail — see this package's
// own doc comment on ErrNotAvailable.
func TestListRemotesReturnsErrNotAvailableWithoutRclone(t *testing.T) {
	old := lookPath
	defer func() { lookPath = old }()
	lookPath = func(string) (string, error) { return "", errors.New("not found") }

	if _, err := ListRemotes(); !errors.Is(err, ErrNotAvailable) {
		t.Errorf("ListRemotes() error = %v, want ErrNotAvailable", err)
	}
	if err := Mount("myremote:", "/some/target", MountOptions{}); !errors.Is(err, ErrNotAvailable) {
		t.Errorf("Mount() error = %v, want ErrNotAvailable", err)
	}
	if err := Unmount("/some/target"); !errors.Is(err, ErrNotAvailable) {
		t.Errorf("Unmount() error = %v, want ErrNotAvailable", err)
	}
}

// TestMountPassesVFSCacheModeFullAndCacheDir pins Mount's own
// non-negotiable write-support flag (see Mount's own doc comment on
// why) and that an explicit CacheDir is actually forwarded, by
// inspecting the args runMount was called with rather than running a
// real rclone.
func TestMountPassesVFSCacheModeFullAndCacheDir(t *testing.T) {
	oldLookPath, oldRunMount := lookPath, runMount
	defer func() { lookPath, runMount = oldLookPath, oldRunMount }()
	lookPath = func(string) (string, error) { return "/usr/bin/rclone", nil }

	var gotArgs []string
	runMount = func(args []string) error {
		gotArgs = args
		return nil
	}

	if err := Mount("mys3:", "/mnt/mys3", MountOptions{CacheDir: "/home/user/.cache/breakthrough-rclone"}); err != nil {
		t.Fatalf("Mount: %v", err)
	}

	wantArgs := []string{"mount", "mys3:", "/mnt/mys3", "--daemon", "--vfs-cache-mode", "full", "--cache-dir", "/home/user/.cache/breakthrough-rclone"}
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("got args %v, want %v", gotArgs, wantArgs)
	}
	for i, w := range wantArgs {
		if gotArgs[i] != w {
			t.Errorf("arg %d = %q, want %q (full: %v)", i, gotArgs[i], w, gotArgs)
		}
	}
}

// TestMountExtraArgsAppendedAfterFixedFlags pins that ExtraArgs is an
// addition, never a replacement, for the fixed flags above it.
func TestMountExtraArgsAppendedAfterFixedFlags(t *testing.T) {
	oldLookPath, oldRunMount := lookPath, runMount
	defer func() { lookPath, runMount = oldLookPath, oldRunMount }()
	lookPath = func(string) (string, error) { return "/usr/bin/rclone", nil }

	var gotArgs []string
	runMount = func(args []string) error {
		gotArgs = args
		return nil
	}

	if err := Mount("mys3:", "/mnt/mys3", MountOptions{ExtraArgs: []string{"--vfs-cache-max-size", "1G"}}); err != nil {
		t.Fatalf("Mount: %v", err)
	}

	wantTail := []string{"--vfs-cache-max-size", "1G"}
	if len(gotArgs) < len(wantTail) {
		t.Fatalf("args too short: %v", gotArgs)
	}
	gotTail := gotArgs[len(gotArgs)-len(wantTail):]
	for i, w := range wantTail {
		if gotTail[i] != w {
			t.Errorf("tail arg %d = %q, want %q", i, gotTail[i], w)
		}
	}
}

// TestIsMountedDetectsARealRcloneFUSEMount exercises IsMounted end to
// end against a real mount, skipped where rclone/fusermount/findmnt
// aren't all installed — not simulated, because the whole point is
// confirming internal/mounts.Read actually surfaces an rclone FUSE
// mount (fstype "fuse.rclone") the same way it does any other real
// mount, rather than assuming it from documentation alone.
func TestIsMountedDetectsARealRcloneFUSEMount(t *testing.T) {
	requireCommand(t, "rclone")
	requireCommand(t, "findmnt")
	requireCommand(t, unmountCommand())

	dir := t.TempDir()
	confDir := t.TempDir()
	t.Setenv("RCLONE_CONFIG", confDir+"/rclone.conf")

	srcDir := t.TempDir()
	if out, err := exec.Command("rclone", "config", "create", "testlocal", "local", "--non-interactive").CombinedOutput(); err != nil {
		t.Fatalf("rclone config create: %v\n%s", err, out)
	}

	target := dir + "/mnt"
	if err := exec.Command("mkdir", "-p", target).Run(); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	if err := Mount("testlocal:"+srcDir, target, MountOptions{}); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	defer func() {
		if err := Unmount(target); err != nil {
			t.Logf("cleanup Unmount: %v", err)
		}
	}()

	// --daemon forks after the mount itself is already established, but
	// a few retries with a short, bounded sleep absorb any remaining
	// scheduling slack rather than assuming the very first check always
	// wins the race.
	for attempt := 0; attempt < 20; attempt++ {
		mounted, err := IsMounted(target)
		if err != nil {
			t.Fatalf("IsMounted: %v", err)
		}
		if mounted {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("IsMounted never reported the real mount as mounted")
}
