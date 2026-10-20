package rclone

import (
	"errors"
	"os"
	"os/exec"
	"strings"
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

// TestQuoteRemoteParamsEscapesEveryValue pins quoteRemoteParams' own
// contract: every value is single-quoted unconditionally, and an
// embedded single quote is doubled, not escaped some other way —
// confirmed against a real run, not guessed.
func TestQuoteRemoteParamsEscapesEveryValue(t *testing.T) {
	got := quoteRemoteParams([]string{
		"url=https://example.com/remote.php/webdav/",
		"description=it's a test",
		"noequalssign",
	})
	want := []string{
		"url='https://example.com/remote.php/webdav/'",
		"description='it''s a test'",
		"noequalssign",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("quoteRemoteParams()[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// TestObscurePasswordRealRoundTrip pins ObscurePassword against a
// real rclone binary, skipped where it isn't installed — confirmed by
// actually reversing it with `rclone reveal`, not just asserting it
// returns some string.
func TestObscurePasswordRealRoundTrip(t *testing.T) {
	requireCommand(t, "rclone")

	obscured, err := ObscurePassword("my-test-password")
	if err != nil {
		t.Fatalf("ObscurePassword: %v", err)
	}
	if obscured == "" || obscured == "my-test-password" {
		t.Fatalf("ObscurePassword returned %q, want a real obscured value", obscured)
	}

	revealed, err := exec.Command("rclone", "reveal", obscured).Output()
	if err != nil {
		t.Fatalf("rclone reveal: %v", err)
	}
	if got := strings.TrimSpace(string(revealed)); got != "my-test-password" {
		t.Errorf("rclone reveal(ObscurePassword(x)) = %q, want %q", got, "my-test-password")
	}
}

// TestTestRemoteRealRoundTrip exercises TestRemote against a real
// rclone binary: a "local" backend (no credentials needed at all)
// must succeed, and never creates a config file — confirmed the same
// way TestCreateStaticRemoteRealRcloneRoundTrip already confirms
// CreateStaticRemote's own side effects, here confirming the absence
// of one. A bogus FTP host must fail, and fail within the bounded
// timeout this function itself enforces, not hang.
func TestTestRemoteRealRoundTrip(t *testing.T) {
	requireCommand(t, "rclone")

	confPath := t.TempDir() + "/rclone.conf"
	t.Setenv("RCLONE_CONFIG", confPath)

	if err := TestRemote("local", nil, 5*time.Second); err != nil {
		t.Errorf("TestRemote(local) = %v, want success", err)
	}
	if _, err := os.Stat(confPath); err == nil {
		t.Error("TestRemote should never create a config file")
	}

	start := time.Now()
	err := TestRemote("ftp", []string{"host=203.0.113.1", "user=nobody", "pass=" + mustObscure(t, "nobody")}, 3*time.Second)
	if err == nil {
		t.Error("TestRemote against an unreachable host should fail")
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Errorf("TestRemote took %v, want it bounded by its own timeout, not rclone's default retry/backoff", elapsed)
	}
}

// mustObscure is TestTestRemoteRealRoundTrip's own small helper —
// requireCommand(t, "rclone") is already guaranteed by its only
// caller, so no need to repeat that guard here.
func mustObscure(t *testing.T, plaintext string) string {
	t.Helper()
	obscured, err := ObscurePassword(plaintext)
	if err != nil {
		t.Fatalf("ObscurePassword: %v", err)
	}
	return obscured
}
