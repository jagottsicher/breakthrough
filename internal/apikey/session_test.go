package apikey

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")

	s, err := OpenSession(path)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if s.IsUnlocked() {
		t.Fatalf("new session should start locked")
	}
	if err := s.Unlock("correct-passphrase"); err != nil {
		t.Fatalf("Unlock on empty store: %v", err)
	}
	if err := s.Set("anthropic", "sk-ant-secret-value"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Reopen as a fresh process would, to prove persistence actually happened.
	s2, err := OpenSession(path)
	if err != nil {
		t.Fatalf("OpenSession (reopen): %v", err)
	}
	if err := s2.Unlock("correct-passphrase"); err != nil {
		t.Fatalf("Unlock with correct passphrase: %v", err)
	}
	got, err := s2.Get("anthropic")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "sk-ant-secret-value" {
		t.Fatalf("got %q, want original plaintext", got)
	}
}

func TestSessionWrongPassphraseDistinguishable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")

	s, err := OpenSession(path)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := s.Unlock("correct-passphrase"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if err := s.Set("anthropic", "sk-ant-secret-value"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	s2, err := OpenSession(path)
	if err != nil {
		t.Fatalf("OpenSession (reopen): %v", err)
	}
	err = s2.Unlock("wrong-passphrase")
	if !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("Unlock with wrong passphrase: got %v, want ErrWrongPassphrase", err)
	}

	// Distinguish from "not found": correct passphrase, different provider.
	s3, err := OpenSession(path)
	if err != nil {
		t.Fatalf("OpenSession (reopen 2): %v", err)
	}
	if err := s3.Unlock("correct-passphrase"); err != nil {
		t.Fatalf("Unlock with correct passphrase: %v", err)
	}
	_, err = s3.Get("openai")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get unknown provider: got %v, want ErrNotFound", err)
	}
}

func TestSessionLockedWithoutUnlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	s, err := OpenSession(path)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if _, err := s.Get("anthropic"); !errors.Is(err, ErrLocked) {
		t.Fatalf("Get while locked: got %v, want ErrLocked", err)
	}
	if err := s.Set("anthropic", "x"); !errors.Is(err, ErrLocked) {
		t.Fatalf("Set while locked: got %v, want ErrLocked", err)
	}
}

func TestSessionLockZeroesKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	s, err := OpenSession(path)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := s.Unlock("passphrase"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	s.Lock()
	if s.IsUnlocked() {
		t.Fatalf("session should be locked after Lock")
	}
	if _, err := s.Get("anthropic"); !errors.Is(err, ErrLocked) {
		t.Fatalf("Get after Lock: got %v, want ErrLocked", err)
	}
}

func TestStoreFileHasNoPlaintext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	const secret = "sk-ant-this-must-never-appear-on-disk"

	s, err := OpenSession(path)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := s.Unlock("passphrase"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if err := s.Set("anthropic", secret); err != nil {
		t.Fatalf("Set: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading store file: %v", err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatalf("store file on disk contains the plaintext secret")
	}
	if strings.Contains(string(raw), "passphrase") {
		t.Fatalf("store file on disk contains the plaintext passphrase")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat store file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("store file mode = %v, want 0600", info.Mode().Perm())
	}
}
