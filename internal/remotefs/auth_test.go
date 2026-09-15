package remotefs

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// writeTestKeyFile marshals signer's own private key to an OpenSSH-
// format PEM file (optionally passphrase-encrypted) at dir/name —
// ssh.MarshalPrivateKey[WithPassphrase] produces the exact format a
// real ~/.ssh/id_ed25519 already has, so this exercises the same
// parsing path keyFileAuthMethod uses against a real key file, not a
// hand-rolled stand-in format.
func writeTestKeyFile(t *testing.T, dir, name string, priv ed25519.PrivateKey, passphrase string) string {
	t.Helper()
	var block *pem.Block
	var err error
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatalf("marshaling test private key: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("writing test private key: %v", err)
	}
	return path
}

func TestKeyFileAuthMethodOnAMissingFileIsSilentlyUnavailable(t *testing.T) {
	if _, ok := keyFileAuthMethod(filepath.Join(t.TempDir(), "nope"), nil); ok {
		t.Error("ok = true for a nonexistent key file, want false (not an error — the common case)")
	}
}

func TestKeyFileAuthMethodOnAnUnencryptedKeySucceedsWithoutAPassphrasePrompt(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := writeTestKeyFile(t, t.TempDir(), "id_ed25519", priv, "")

	promptCalls := 0
	_, ok := keyFileAuthMethod(path, func(string) (string, error) {
		promptCalls++
		return "", nil
	})
	if !ok {
		t.Fatal("ok = false, want true for a valid unencrypted key")
	}
	if promptCalls != 0 {
		t.Errorf("promptCalls = %d, want 0 (an unencrypted key needs no passphrase)", promptCalls)
	}
}

func TestKeyFileAuthMethodOnAnEncryptedKeyUsesThePassphrasePrompt(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := writeTestKeyFile(t, t.TempDir(), "id_ed25519", priv, "correct horse")

	_, ok := keyFileAuthMethod(path, func(string) (string, error) {
		return "correct horse", nil
	})
	if !ok {
		t.Error("ok = false, want true when the prompt returns the right passphrase")
	}
}

func TestKeyFileAuthMethodOnAnEncryptedKeyWithTheWrongPassphraseFails(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := writeTestKeyFile(t, t.TempDir(), "id_ed25519", priv, "correct horse")

	_, ok := keyFileAuthMethod(path, func(string) (string, error) {
		return "wrong passphrase", nil
	})
	if ok {
		t.Error("ok = true, want false for a wrong passphrase")
	}
}

func TestKeyFileAuthMethodOnAnEncryptedKeyWithNoPassphraseCallbackIsUnavailable(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := writeTestKeyFile(t, t.TempDir(), "id_ed25519", priv, "correct horse")

	if _, ok := keyFileAuthMethod(path, nil); ok {
		t.Error("ok = true, want false — an encrypted key with nothing to ask for a passphrase can't be used")
	}
}

func TestAgentAuthMethodWithNoSocketConfiguredIsUnavailable(t *testing.T) {
	if _, ok := agentAuthMethod(""); ok {
		t.Error("ok = true for an empty socket path, want false")
	}
}

func TestAgentAuthMethodWithAnUnreachableSocketIsUnavailable(t *testing.T) {
	if _, ok := agentAuthMethod(filepath.Join(t.TempDir(), "no-agent-here.sock")); ok {
		t.Error("ok = true for a socket nothing is listening on, want false")
	}
}

func TestIdentityFilesHonorsAnExplicitEmptyListInsteadOfFallingBackToDefaults(t *testing.T) {
	// A test asking for "no identity files" must actually get none —
	// not silently fall back to whatever the real machine running this
	// test happens to have under ~/.ssh, which would make every other
	// Dial-level test non-hermetic (see sftp_test.go).
	if got := identityFiles([]string{}); len(got) != 0 {
		t.Errorf("identityFiles([]string{}) = %v, want an empty slice, not the defaults", got)
	}
}

func TestAuthMethodsReturnsNothingWhenEverythingIsExplicitlyDisabled(t *testing.T) {
	methods := authMethods(AuthOptions{IdentityFiles: []string{}})
	if len(methods) != 0 {
		t.Errorf("len(methods) = %d, want 0", len(methods))
	}
}
