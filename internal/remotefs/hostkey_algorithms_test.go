package remotefs

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// TestPreferredHostKeyAlgorithmsReturnsRecordedTypesForAKnownHost pins
// preferredHostKeyAlgorithms' own core logic directly, independent of
// any real SSH handshake: given a known_hosts file with an existing
// entry for a host, it returns that entry's own key algorithm; for a
// host with no entry at all, it returns nil (no preference) rather
// than erroring.
func TestPreferredHostKeyAlgorithmsReturnsRecordedTypesForAKnownHost(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh.NewPublicKey: %v", err)
	}

	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(sshKnownHostsLine(t, "example.test:22", sshPub)), 0o600); err != nil {
		t.Fatalf("writing known_hosts: %v", err)
	}

	got := preferredHostKeyAlgorithms(path, "example.test:22")
	if len(got) != 1 || got[0] != sshPub.Type() {
		t.Errorf("preferredHostKeyAlgorithms = %v, want [%q]", got, sshPub.Type())
	}

	if got := preferredHostKeyAlgorithms(path, "never-seen.test:22"); got != nil {
		t.Errorf("preferredHostKeyAlgorithms for an unrecorded host = %v, want nil", got)
	}
}

// TestDialSucceedsWhenRecordedAlgorithmIsNotTheLibraryDefaultsFirstChoice
// is the real, end-to-end regression for the bug this file's other
// test's own production code fixes: a real, user-reported case of a
// plain `ssh user@host` connecting instantly while this project's own
// Connect dialog refused the identical host outright with "knownhosts:
// key mismatch".
//
// Root cause: a server offering more than one host key type lets the
// client's own HostKeyAlgorithms preference decide which one it
// presents — and golang.org/x/crypto/ssh's own default order (see
// supportedHostKeyAlgos in its common.go) prefers RSA over Ed25519,
// the *opposite* of OpenSSH's own default, which is exactly how two
// different SSH clients against the same server end up trusting two
// different host keys for it. Reproduced here directly: the test
// server offers both an RSA and an Ed25519 host key (AddHostKey takes
// either, called twice), known_hosts has only the Ed25519 one recorded
// (what a plain `ssh` would have pinned, by OpenSSH's own preference),
// and Dial is called with no explicit HostKeyAlgorithms override —
// exactly how internal/ui's own connect flow calls it. Before this
// fix, the library's own RSA-first default meant the server would
// present RSA, found no match for the recorded Ed25519 entry, and
// knownhosts correctly (if, here, wrongly in effect) reported a
// mismatch. With preferredHostKeyAlgorithms wired into Dial, it
// detects the recorded Ed25519 entry and asks for exactly that
// algorithm, matching what a real `ssh` already trusts.
func TestDialSucceedsWhenRecordedAlgorithmIsNotTheLibraryDefaultsFirstChoice(t *testing.T) {
	ed25519Signer, ed25519Pub := newTestKeyPair(t)
	rsaSigner := newTestRSAKeyPair(t)

	config := passwordServerConfig("tester", "s3cret")
	config.AddHostKey(rsaSigner)
	config.AddHostKey(ed25519Signer)
	// startTestSFTPServer (testserver_test.go) always adds its own
	// extra Ed25519 host key on top of whatever sshConfig already
	// has — fine for every other test in this package, which never
	// cares which exact key gets presented, but it would silently
	// overwrite/shadow this test's own Ed25519 key for the "ssh-
	// ed25519" algorithm slot and defeat the entire point here, so
	// this test starts its own minimal listener instead, offering
	// exactly these two keys and no others.
	addr := startTestServerWithExactlyTheseHostKeys(t, config)

	knownHosts := testKnownHostsFile(t)
	if err := os.WriteFile(knownHosts, []byte(sshKnownHostsLine(t, addr, ed25519Pub)), 0o600); err != nil {
		t.Fatalf("writing known_hosts: %v", err)
	}

	client, err := Dial(context.Background(), DialOptions{
		Connection: Connection{Host: mustSplitHost(t, addr), Port: mustSplitPort(t, addr), User: "tester"},
		Auth: AuthOptions{
			IdentityFiles: []string{},
			Password:      func() (string, error) { return "s3cret", nil },
		},
		HostKeyPrompt:  noPromptHostKeyCallback,
		KnownHostsFile: knownHosts,
	})
	if err != nil {
		t.Fatalf("Dial: %v (the server's own RSA host key should never have been offered instead of the recorded Ed25519 one)", err)
	}
	defer func() { _ = client.Close() }()
}

// startTestServerWithExactlyTheseHostKeys is startTestSFTPServer's own
// twin, minus its automatic extra Ed25519 AddHostKey call — needed
// here so this file's own regression test controls precisely which
// host keys the server offers, down to the exact key bytes, rather
// than one more generated on top.
func startTestServerWithExactlyTheseHostKeys(t *testing.T, sshConfig *ssh.ServerConfig) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening for test SSH server: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveTestSSHConn(conn, sshConfig)
		}
	}()

	return ln.Addr().String()
}

// newTestRSAKeyPair generates a fresh 2048-bit RSA key pair for
// TestDialSucceedsWhenRecordedAlgorithmIsNotTheLibraryDefaultsFirstChoice's
// own second host key — ssh.ServerConfig.AddHostKey accepts either
// signer type, the server offers both to a connecting client exactly
// the way a real sshd with multiple HostKey directives configured
// does.
func newTestRSAKeyPair(t *testing.T) ssh.Signer {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("wrapping test RSA private key: %v", err)
	}
	return signer
}

// sshKnownHostsLine renders one known_hosts line for hostWithPort/key,
// the same construction this package's own appendKnownHost uses — used
// directly here, in tests, to seed a starting known_hosts file by hand
// rather than going through a real Dial's own trust-on-first-use path.
func sshKnownHostsLine(t *testing.T, hostWithPort string, key ssh.PublicKey) string {
	t.Helper()
	return knownhosts.Line([]string{knownhosts.Normalize(hostWithPort)}, key) + "\n"
}
