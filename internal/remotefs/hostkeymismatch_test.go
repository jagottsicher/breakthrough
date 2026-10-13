package remotefs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestHostKeyCallbackReportsAChangedKeyAsHostKeyMismatchError pins the
// richer error hostKeyCallback now returns for a changed key — the
// same "always rejected, never prompted" behavior
// TestHostKeyCallbackRejectsAChangedKeyWithoutEverPrompting already
// pins, but checked here for the actual fingerprints a caller (the
// Connect dialog's own "connect anyway" retry) needs to show a real
// old-vs-new comparison instead of knownhosts.KeyError's bare string.
func TestHostKeyCallbackReportsAChangedKeyAsHostKeyMismatchError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	_, oldKey := newTestKeyPair(t)
	_, newKey := newTestKeyPair(t)

	cb, err := hostKeyCallback(path, func(string, string, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("hostKeyCallback: %v", err)
	}
	if err := cb("example.com:22", testRemoteAddr, oldKey); err != nil {
		t.Fatalf("trusting the first key: %v", err)
	}

	cb2, err := hostKeyCallback(path, nil)
	if err != nil {
		t.Fatalf("hostKeyCallback (second): %v", err)
	}
	verifyErr := cb2("example.com:22", testRemoteAddr, newKey)

	var mismatchErr *HostKeyMismatchError
	if !errors.As(verifyErr, &mismatchErr) {
		t.Fatalf("err = %v (%T), want a *HostKeyMismatchError", verifyErr, verifyErr)
	}
	if mismatchErr.NewFingerprint != ssh.FingerprintSHA256(newKey) {
		t.Errorf("NewFingerprint = %q, want %q", mismatchErr.NewFingerprint, ssh.FingerprintSHA256(newKey))
	}
	if len(mismatchErr.OldFingerprints) != 1 || mismatchErr.OldFingerprints[0] != ssh.FingerprintSHA256(oldKey) {
		t.Errorf("OldFingerprints = %v, want [%q]", mismatchErr.OldFingerprints, ssh.FingerprintSHA256(oldKey))
	}
}

// TestDialWithSkipHostKeyVerificationSucceedsDespiteAChangedKey pins
// the one-off "connect anyway" escape hatch end to end: a host whose
// recorded key no longer matches what the server presents — the exact
// shape a user hits after a legitimate host key rotation (see
// internal/ui's own Connect dialog retry flow) — still refuses by
// default, but succeeds when the caller explicitly opts in via
// DialOptions.SkipHostKeyVerification.
func TestDialWithSkipHostKeyVerificationSucceedsDespiteAChangedKey(t *testing.T) {
	_, oldKey := newTestKeyPair(t)
	serverSigner, _ := newTestKeyPair(t) // the server's own, different, real key
	config := passwordServerConfig("tester", "s3cret")
	config.AddHostKey(serverSigner)
	addr := startTestSFTPServer(t, config)

	knownHosts := testKnownHostsFile(t)
	if err := os.WriteFile(knownHosts, []byte(sshKnownHostsLine(t, addr, oldKey)), 0o600); err != nil {
		t.Fatalf("writing known_hosts: %v", err)
	}

	dialOpts := DialOptions{
		Connection: Connection{Host: mustSplitHost(t, addr), Port: mustSplitPort(t, addr), User: "tester"},
		Auth: AuthOptions{
			IdentityFiles: []string{},
			Password:      func() (string, error) { return "s3cret", nil },
		},
		HostKeyPrompt:  noPromptHostKeyCallback,
		KnownHostsFile: knownHosts,
	}

	if _, err := Dial(context.Background(), dialOpts); err == nil {
		t.Fatal("Dial without SkipHostKeyVerification succeeded despite a changed key, want it to refuse")
	} else {
		var mismatchErr *HostKeyMismatchError
		if !errors.As(err, &mismatchErr) {
			t.Fatalf("err = %v (%T), want a *HostKeyMismatchError", err, err)
		}
	}

	dialOpts.SkipHostKeyVerification = true
	client, err := Dial(context.Background(), dialOpts)
	if err != nil {
		t.Fatalf("Dial with SkipHostKeyVerification: %v", err)
	}
	defer func() { _ = client.Close() }()

	before, err := os.ReadFile(knownHosts)
	if err != nil {
		t.Fatalf("reading known_hosts: %v", err)
	}
	if string(before) != sshKnownHostsLine(t, addr, oldKey) {
		t.Error("SkipHostKeyVerification must never write to known_hosts — the stale entry should be untouched")
	}
}
