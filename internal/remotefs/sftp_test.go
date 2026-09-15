package remotefs

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// noPromptHostKeyCallback trusts any host key outright — every test in
// this file dials its own throwaway, freshly generated test server, so
// there is no real host identity worth verifying here; a real Dial
// call from the UI layer always goes through the TOFU-and-persist
// hostkey.go path instead (see hostkey_test.go for that).
func noPromptHostKeyCallback(string, string, string) (bool, error) { return true, nil }

// testKnownHostsFile is a fresh, empty path inside t.TempDir() for
// DialOptions.KnownHostsFile — every test in this file must set this
// explicitly rather than leaving it "" (which defaults to the real
// developer's own ~/.ssh/known_hosts, via DefaultKnownHostsFile): a
// real, previously-unnoticed bug found by actually reading a failure
// message rather than just rerunning it, this whole file was silently
// appending a throwaway 127.0.0.1 entry to that real file on every
// single test run (confirmed directly: 143 of them accumulated there
// before this fix), and once the OS happened to reuse the same
// ephemeral port for two different freshly generated test servers, a
// later Dial's real host-key verification (see hostkey.go's own
// deliberate "no bypass" refusal for a key that changed) failed with a
// hard "knownhosts: key mismatch" against a stale entry from an
// earlier, unrelated test run — the exact flake that surfaced this.
func testKnownHostsFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "known_hosts")
}

// passwordServerConfig builds a *ssh.ServerConfig that accepts exactly
// one user/password pair — the simplest auth a test server can offer,
// used by every test here that isn't specifically exercising key- or
// agent-based auth instead.
func passwordServerConfig(user, password string) *ssh.ServerConfig {
	return &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if conn.User() == user && string(pass) == password {
				return nil, nil
			}
			return nil, errors.New("wrong credentials")
		},
	}
}

func TestDialWithPasswordAuthListsAndReadsFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello, remote world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	addr := startTestSFTPServer(t, passwordServerConfig("tester", "s3cret"))

	client, err := Dial(context.Background(), DialOptions{
		Connection: Connection{Host: mustSplitHost(t, addr), Port: mustSplitPort(t, addr), User: "tester"},
		Auth: AuthOptions{
			IdentityFiles: []string{},
			Password:      func() (string, error) { return "s3cret", nil },
		},
		HostKeyPrompt:  noPromptHostKeyCallback,
		KnownHostsFile: testKnownHostsFile(t),
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	entries, err := client.ListDir(dir)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	byName := map[string]fsops.Entry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2, got %+v", len(entries), entries)
	}
	if !byName["sub"].IsDir || byName["sub"].Type != fsops.TypeDir {
		t.Errorf("sub = %+v, want an IsDir directory entry", byName["sub"])
	}
	if byName["hello.txt"].Type != fsops.TypeFile || byName["hello.txt"].Size != int64(len("hello, remote world")) {
		t.Errorf("hello.txt = %+v, want a plain file of the right size", byName["hello.txt"])
	}

	stat, err := client.Stat(filepath.Join(dir, "hello.txt"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if stat.Size != int64(len("hello, remote world")) {
		t.Errorf("Stat size = %d, want %d", stat.Size, len("hello, remote world"))
	}

	rc, err := client.Open(filepath.Join(dir, "hello.txt"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = rc.Close() }()
	content, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if string(content) != "hello, remote world" {
		t.Errorf("content = %q, want %q", content, "hello, remote world")
	}
}

// TestListDirSortsDirectoriesFirstThenCaseInsensitiveName pins a real,
// user-reported bug: SSH_FXP_READDIR returns entries in whatever order
// the remote server's own filesystem happens to store them, not
// sorted — and internal/ui's own Panel.applySortPreference assumes its
// input already arrives directories-first (see ListDir's own doc
// comment for exactly why), the same precondition fsops.ListDir's
// local implementation already guarantees. Deliberately creates
// entries in an order that would expose the bug immediately if this
// sort were ever removed (a directory created *after* a file that
// sorts earlier by name, and vice versa).
func TestListDirSortsDirectoriesFirstThenCaseInsensitiveName(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"z.txt", "B_dir", "a.txt", "A_dir"} {
		full := filepath.Join(dir, name)
		if strings.HasSuffix(name, "_dir") {
			if err := os.Mkdir(full, 0o755); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	addr := startTestSFTPServer(t, passwordServerConfig("tester", "s3cret"))
	client, err := Dial(context.Background(), DialOptions{
		Connection: Connection{Host: mustSplitHost(t, addr), Port: mustSplitPort(t, addr), User: "tester"},
		Auth: AuthOptions{
			IdentityFiles: []string{},
			Password:      func() (string, error) { return "s3cret", nil },
		},
		HostKeyPrompt:  noPromptHostKeyCallback,
		KnownHostsFile: testKnownHostsFile(t),
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	entries, err := client.ListDir(dir)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name)
	}
	want := []string{"A_dir", "B_dir", "a.txt", "z.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListDir order = %v, want %v (directories first, then case-insensitive name)", got, want)
	}
}

func TestDialWithWrongPasswordFailsWithoutBeingAConnectionRefusedError(t *testing.T) {
	addr := startTestSFTPServer(t, passwordServerConfig("tester", "s3cret"))

	_, err := Dial(context.Background(), DialOptions{
		Connection: Connection{Host: mustSplitHost(t, addr), Port: mustSplitPort(t, addr), User: "tester"},
		Auth: AuthOptions{
			IdentityFiles: []string{},
			Password:      func() (string, error) { return "wrong", nil },
		},
		HostKeyPrompt:  noPromptHostKeyCallback,
		KnownHostsFile: testKnownHostsFile(t),
	})
	if err == nil {
		t.Fatal("Dial succeeded with the wrong password, want an error")
	}
	var refused *ConnectionRefusedError
	if errors.As(err, &refused) {
		t.Errorf("got a ConnectionRefusedError (%v), want an authentication failure — the server was reachable", err)
	}
}

func TestDialWithPublicKeyIdentityFileSucceeds(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := writeTestKeyFile(t, t.TempDir(), "id_ed25519", priv, "")

	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), sshPub.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("unrecognized key")
		},
	}
	addr := startTestSFTPServer(t, config)

	client, err := Dial(context.Background(), DialOptions{
		Connection:     Connection{Host: mustSplitHost(t, addr), Port: mustSplitPort(t, addr), User: "tester"},
		Auth:           AuthOptions{IdentityFiles: []string{keyPath}},
		HostKeyPrompt:  noPromptHostKeyCallback,
		KnownHostsFile: testKnownHostsFile(t),
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = client.Close()
}

func TestDialWithAgentAuthSucceeds(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}

	socketPath := startTestAgent(t, priv)

	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), sshPub.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("unrecognized key")
		},
	}
	addr := startTestSFTPServer(t, config)

	client, err := Dial(context.Background(), DialOptions{
		Connection:     Connection{Host: mustSplitHost(t, addr), Port: mustSplitPort(t, addr), User: "tester"},
		Auth:           AuthOptions{AgentSocket: socketPath, IdentityFiles: []string{}},
		HostKeyPrompt:  noPromptHostKeyCallback,
		KnownHostsFile: testKnownHostsFile(t),
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = client.Close()
}

func TestClientCreateMkdirRenameAndRemoveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	addr := startTestSFTPServer(t, passwordServerConfig("tester", "s3cret"))
	client, err := Dial(context.Background(), DialOptions{
		Connection: Connection{Host: mustSplitHost(t, addr), Port: mustSplitPort(t, addr), User: "tester"},
		Auth: AuthOptions{
			IdentityFiles: []string{},
			Password:      func() (string, error) { return "s3cret", nil },
		},
		HostKeyPrompt:  noPromptHostKeyCallback,
		KnownHostsFile: testKnownHostsFile(t),
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	filePath := filepath.Join(dir, "uploaded.txt")
	w, err := client.Create(filePath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := w.Write([]byte("uploaded content")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading what should have been uploaded: %v", err)
	}
	if string(got) != "uploaded content" {
		t.Errorf("content = %q, want %q", got, "uploaded content")
	}

	subdir := filepath.Join(dir, "newdir")
	if err := client.Mkdir(subdir); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if fi, err := os.Stat(subdir); err != nil || !fi.IsDir() {
		t.Errorf("Mkdir did not create a real directory: %v", err)
	}

	renamedPath := filepath.Join(dir, "renamed.txt")
	if err := client.Rename(filePath, renamedPath); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("old path still exists after Rename")
	}
	if _, err := os.Stat(renamedPath); err != nil {
		t.Errorf("new path missing after Rename: %v", err)
	}

	if err := client.Remove(renamedPath); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(renamedPath); !os.IsNotExist(err) {
		t.Error("file still exists after Remove")
	}

	if err := client.RemoveDirectory(subdir); err != nil {
		t.Fatalf("RemoveDirectory: %v", err)
	}
	if _, err := os.Stat(subdir); !os.IsNotExist(err) {
		t.Error("directory still exists after RemoveDirectory")
	}
}

// TestDiskUsageReturnsRealFiguresFromTheTestServersOwnFilesystem pins
// the real, end-to-end path: pkg/sftp's simple *sftp.Server does
// implement the statvfs@openssh.com extension on Linux (via a real
// syscall.Statfs against whatever local directory it's serving —
// confirmed by reading server_statvfs_impl.go/server_statvfs_linux.go
// directly, not assumed), so this hermetic test server can answer a
// real DiskUsage call the same way an actual OpenSSH sftp-server
// would, without needing anything faked.
func TestDiskUsageReturnsRealFiguresFromTheTestServersOwnFilesystem(t *testing.T) {
	dir := t.TempDir()
	addr := startTestSFTPServer(t, passwordServerConfig("tester", "s3cret"))
	client, err := Dial(context.Background(), DialOptions{
		Connection: Connection{Host: mustSplitHost(t, addr), Port: mustSplitPort(t, addr), User: "tester"},
		Auth: AuthOptions{
			IdentityFiles: []string{},
			Password:      func() (string, error) { return "s3cret", nil },
		},
		HostKeyPrompt:  noPromptHostKeyCallback,
		KnownHostsFile: testKnownHostsFile(t),
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	got, err := client.DiskUsage(dir)
	if err != nil {
		t.Fatalf("DiskUsage: %v", err)
	}
	// The real numbers depend on whatever filesystem the test runner's
	// own temp directory happens to live on — only the shape is worth
	// pinning here, not exact values (see
	// TestDiskUsageFromStatVFSConvertsBlocksAndInodesToBytesAndPercentages
	// for the exact unit-conversion math against known inputs).
	if got.AvailBytes <= 0 {
		t.Errorf("AvailBytes = %d, want a positive figure for a real, writable temp directory", got.AvailBytes)
	}
	if got.UsePercent < 0 || got.UsePercent > 100 {
		t.Errorf("UsePercent = %d, want a value in [0, 100]", got.UsePercent)
	}
}

// TestDiskUsageFromStatVFSConvertsBlocksAndInodesToBytesAndPercentages
// pins diskUsageFromStatVFS's own unit conversion and percentage math
// directly against hand-built values — no real server round trip
// needed at all (see diskUsageFromStatVFS's own doc comment on why).
func TestDiskUsageFromStatVFSConvertsBlocksAndInodesToBytesAndPercentages(t *testing.T) {
	v := &sftp.StatVFS{
		Frsize: 1024,
		Blocks: 1000,
		Bfree:  400, // used = (1000-400)*1024 = 614400
		Bavail: 300, // avail = 300*1024 = 307200; percent = 600/(600+300) = 66%... in blocks: 600/900
		Files:  200,
		Ffree:  120, // used inodes = 80
		Favail: 100, // percent = 80/(80+100) = 44%
	}

	got := diskUsageFromStatVFS(v)

	want := fsops.DiskUsage{
		UsedBytes:    614400,
		AvailBytes:   307200,
		UsedInodes:   80,
		AvailInodes:  100,
		UsePercent:   66,
		InodePercent: 44,
	}
	if got != want {
		t.Errorf("diskUsageFromStatVFS(%+v) = %+v, want %+v", v, got, want)
	}
}

func TestDialFailsWithConnectionRefusedErrorWhenNothingIsListening(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // frees the port; nothing is listening on it anymore

	_, err = Dial(context.Background(), DialOptions{
		Connection: Connection{Host: mustSplitHost(t, addr), Port: mustSplitPort(t, addr), User: "tester"},
		Auth:       AuthOptions{IdentityFiles: []string{}, Password: func() (string, error) { return "x", nil }},
	})
	var refused *ConnectionRefusedError
	if !errors.As(err, &refused) {
		t.Errorf("err = %v, want a *ConnectionRefusedError", err)
	}
}

func TestDialRespectsContextCancellationDuringAHungHandshake(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		// Accepts the TCP connection but never speaks SSH at all —
		// simulates a host that's reachable but then goes silent,
		// exactly the case dialSSHContext's own doc comment describes.
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		time.Sleep(10 * time.Second) // outlives this test's own 5s bound below either way
		_ = conn.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := Dial(ctx, DialOptions{
			Connection: Connection{Host: mustSplitHost(t, ln.Addr().String()), Port: mustSplitPort(t, ln.Addr().String()), User: "tester"},
			Auth:       AuthOptions{IdentityFiles: []string{}, Password: func() (string, error) { return "x", nil }},
		})
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Dial did not return within 5s of its own 200ms context deadline — cancellation isn't actually being honored")
	}
}

// startTestAgent runs a real, in-process ssh-agent (golang.org/x/crypto/
// ssh/agent's own reference implementation, not a stand-in) over a
// fresh unix socket, holding exactly one already-added private key —
// enough for AuthOptions.AgentSocket-based tests to authenticate
// through a real agent protocol round trip instead of asserting
// against agentAuthMethod's own internals directly.
func startTestAgent(t *testing.T, priv ed25519.PrivateKey) (socketPath string) {
	t.Helper()
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatalf("adding key to test agent: %v", err)
	}

	socketPath = filepath.Join(t.TempDir(), "agent.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listening for test agent: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _ = agent.ServeAgent(keyring, conn) }()
		}
	}()

	return socketPath
}

func mustSplitHost(t *testing.T, addr string) string {
	t.Helper()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("splitting %q: %v", addr, err)
	}
	return host
}

func mustSplitPort(t *testing.T, addr string) int {
	t.Helper()
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("splitting %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parsing port %q: %v", portStr, err)
	}
	return port
}
