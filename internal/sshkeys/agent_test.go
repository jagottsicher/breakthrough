package sshkeys

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// startTestAgent runs a real in-memory ssh-agent (agent.NewKeyring, the
// same package's own reference server implementation) behind a Unix
// socket, loads privKey into it, and returns the socket path plus the
// key's own SHA256 fingerprint. The listener is closed on test cleanup.
func startTestAgent(t *testing.T, privKey ed25519.PrivateKey) (socketPath, fingerprint string) {
	t.Helper()

	// A short-named directory straight under os.TempDir(), not
	// t.TempDir()'s own deeply nested (subtest-name-included) path:
	// macOS's sockaddr_un caps sun_path at 104 bytes, and a real CI run
	// on macOS hit exactly that ceiling here ("bind: invalid argument"),
	// confirmed live, not guessed.
	dir, err := os.MkdirTemp("", "sshagent")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	socketPath = filepath.Join(dir, "a.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: privKey}); err != nil {
		t.Fatalf("keyring.Add: %v", err)
	}

	sshPub, err := ssh.NewPublicKey(privKey.Public())
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	fingerprint = ssh.FingerprintSHA256(sshPub)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() { _ = agent.ServeAgent(keyring, conn) }()
		}
	}()

	return socketPath, fingerprint
}

func TestAgentFingerprintsReturnsLoadedIdentity(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	socketPath, want := startTestAgent(t, priv)

	t.Setenv("SSH_AUTH_SOCK", socketPath)
	got := AgentFingerprints()
	if !got[want] {
		t.Errorf("AgentFingerprints() = %v, want it to contain %q", got, want)
	}
}

func TestAgentFingerprintsNoSocketEnvReturnsNil(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	if got := AgentFingerprints(); got != nil {
		t.Errorf("AgentFingerprints() = %v, want nil with no SSH_AUTH_SOCK set", got)
	}
}

func TestAgentFingerprintsUnreachableSocketReturnsNil(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(t.TempDir(), "does-not-exist.sock"))
	if got := AgentFingerprints(); got != nil {
		t.Errorf("AgentFingerprints() = %v, want nil for an unreachable socket", got)
	}
}
