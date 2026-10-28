package apikey

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveEnvBeatsFileBeatsEncrypted(t *testing.T) {
	const envVar = "BREAKTHROUGH_TEST_API_KEY"

	storePath := filepath.Join(t.TempDir(), "keys.json")
	session, err := OpenSession(storePath)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := session.Unlock("passphrase"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if err := session.Set("anthropic", "from-encrypted"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	filePath := filepath.Join(t.TempDir(), "key.txt")
	if err := os.WriteFile(filePath, []byte("from-file\n"), 0o600); err != nil {
		t.Fatalf("writing file tier: %v", err)
	}

	// All three configured: env wins.
	t.Setenv(envVar, "from-env")
	key, tier, err := Resolve(envVar, filePath, "anthropic", session)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if tier != TierEnv || key != "from-env" {
		t.Fatalf("got (%q, %v), want (from-env, TierEnv)", key, tier)
	}

	// Env unset: file wins over encrypted.
	t.Setenv(envVar, "")
	key, tier, err = Resolve(envVar, filePath, "anthropic", session)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if tier != TierFile || key != "from-file" {
		t.Fatalf("got (%q, %v), want (from-file, TierFile)", key, tier)
	}

	// Env unset, file not configured: encrypted tier wins.
	key, tier, err = Resolve(envVar, "", "anthropic", session)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if tier != TierEncrypted || key != "from-encrypted" {
		t.Fatalf("got (%q, %v), want (from-encrypted, TierEncrypted)", key, tier)
	}

	// Nothing configured at all: TierNone, no error.
	key, tier, err = Resolve("", "", "openai", session)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if tier != TierNone || key != "" {
		t.Fatalf("got (%q, %v), want (\"\", TierNone)", key, tier)
	}
}

func TestResolveMissingFileFallsThrough(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "keys.json")
	session, err := OpenSession(storePath)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := session.Unlock("passphrase"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if err := session.Set("anthropic", "from-encrypted"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	missing := filepath.Join(t.TempDir(), "does-not-exist.txt")
	key, tier, err := Resolve("", missing, "anthropic", session)
	if err != nil {
		t.Fatalf("Resolve with missing configured file: %v", err)
	}
	if tier != TierEncrypted || key != "from-encrypted" {
		t.Fatalf("got (%q, %v), want fall-through to encrypted tier", key, tier)
	}
}

func TestResolveUnreadableFileIsAnError(t *testing.T) {
	dirAsFile := t.TempDir() // a directory, not a file — os.ReadFile on it fails, but not with IsNotExist
	_, _, err := Resolve("", dirAsFile, "anthropic", nil)
	if err == nil {
		t.Fatalf("Resolve with unreadable configured file: want error, got nil")
	}
}

func TestResolveLockedSessionIsSkipped(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "keys.json")
	session, err := OpenSession(storePath)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	// Never unlocked.
	key, tier, err := Resolve("", "", "anthropic", session)
	if err != nil {
		t.Fatalf("Resolve with locked session: %v", err)
	}
	if tier != TierNone || key != "" {
		t.Fatalf("got (%q, %v), want (\"\", TierNone) for locked session", key, tier)
	}
}
