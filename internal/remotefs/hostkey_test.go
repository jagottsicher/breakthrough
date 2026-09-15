package remotefs

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// testRemoteAddr stands in for the real net.Addr a live TCP connection
// would supply — the real knownhosts implementation dereferences it
// directly (verified the hard way: a nil literal here panics inside
// golang.org/x/crypto/ssh/knownhosts itself, not this package's own
// code), so every direct hostKeyCallback call in this file needs a
// real, non-nil value here, even though none of these tests care what
// address it actually holds.
var testRemoteAddr = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}

func TestHostKeyCallbackCreatesTheKnownHostsFileIfMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	if _, err := hostKeyCallback(path, nil); err != nil {
		t.Fatalf("hostKeyCallback: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("known_hosts file was not created: %v", err)
	}
}

func TestHostKeyCallbackAcceptsAnUnknownHostViaPromptAndPersistsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	_, pub := newTestKeyPair(t)

	promptCalls := 0
	prompt := func(hostname string, key ssh.PublicKey) (bool, error) {
		promptCalls++
		return true, nil
	}

	cb, err := hostKeyCallback(path, prompt)
	if err != nil {
		t.Fatalf("hostKeyCallback: %v", err)
	}
	if err := cb("example.com:22", testRemoteAddr, pub); err != nil {
		t.Fatalf("cb: %v", err)
	}
	if promptCalls != 1 {
		t.Errorf("promptCalls = %d, want 1", promptCalls)
	}

	// A second, independent callback built fresh against the same file
	// must now trust the key without prompting at all — proves the
	// accepted key was actually persisted, not just accepted in
	// memory for this one call.
	secondPromptCalls := 0
	cb2, err := hostKeyCallback(path, func(string, ssh.PublicKey) (bool, error) {
		secondPromptCalls++
		return true, nil
	})
	if err != nil {
		t.Fatalf("hostKeyCallback (second): %v", err)
	}
	if err := cb2("example.com:22", testRemoteAddr, pub); err != nil {
		t.Errorf("cb2: %v, want the now-known key to be trusted silently", err)
	}
	if secondPromptCalls != 0 {
		t.Errorf("secondPromptCalls = %d, want 0 (already trusted, no prompt needed)", secondPromptCalls)
	}
}

func TestHostKeyCallbackDeclinesAnUnknownHostViaPrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	_, pub := newTestKeyPair(t)

	cb, err := hostKeyCallback(path, func(string, ssh.PublicKey) (bool, error) {
		return false, nil
	})
	if err != nil {
		t.Fatalf("hostKeyCallback: %v", err)
	}
	if err := cb("example.com:22", testRemoteAddr, pub); err == nil {
		t.Error("cb = nil error, want an error when the prompt declines")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading known_hosts: %v", err)
	}
	if strings.Contains(string(data), "example.com") {
		t.Error("known_hosts contains example.com, want nothing persisted after a declined prompt")
	}
}

func TestHostKeyCallbackPropagatesAPromptError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	_, pub := newTestKeyPair(t)
	wantErr := errors.New("boom")

	cb, err := hostKeyCallback(path, func(string, ssh.PublicKey) (bool, error) {
		return false, wantErr
	})
	if err != nil {
		t.Fatalf("hostKeyCallback: %v", err)
	}
	if err := cb("example.com:22", testRemoteAddr, pub); !errors.Is(err, wantErr) {
		t.Errorf("cb error = %v, want it to wrap/equal the prompt's own error", err)
	}
}

func TestHostKeyCallbackRejectsAChangedKeyWithoutEverPrompting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	_, firstKey := newTestKeyPair(t)
	_, secondKey := newTestKeyPair(t)

	cb, err := hostKeyCallback(path, func(string, ssh.PublicKey) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("hostKeyCallback: %v", err)
	}
	if err := cb("example.com:22", testRemoteAddr, firstKey); err != nil {
		t.Fatalf("trusting the first key: %v", err)
	}

	promptCalls := 0
	cb2, err := hostKeyCallback(path, func(string, ssh.PublicKey) (bool, error) {
		promptCalls++
		return true, nil // would wrongly accept the MITM key if this ever ran
	})
	if err != nil {
		t.Fatalf("hostKeyCallback (second): %v", err)
	}
	if err := cb2("example.com:22", testRemoteAddr, secondKey); err == nil {
		t.Error("cb2 = nil error, want a changed host key to always be rejected")
	}
	if promptCalls != 0 {
		t.Errorf("promptCalls = %d, want 0 — a changed key must never reach the trust-on-first-use prompt", promptCalls)
	}
}
