package sshkeys

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// writeKeyPair writes name and name+".pub" into dir for an ed25519 key,
// encrypted with passphrase when non-empty, and returns the key's own
// SHA256 fingerprint for assertions.
func writeKeyPair(t *testing.T, dir, name string, passphrase string) string {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "test-comment")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "test-comment", []byte(passphrase))
	}
	if err != nil {
		t.Fatalf("MarshalPrivateKey: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	authorizedLine := ssh.MarshalAuthorizedKey(sshPub)
	pubLine := append(authorizedLine[:len(authorizedLine)-1], []byte(" test-comment\n")...)
	if err := os.WriteFile(filepath.Join(dir, name+".pub"), pubLine, 0o644); err != nil {
		t.Fatalf("write public key: %v", err)
	}

	return ssh.FingerprintSHA256(sshPub)
}

// writeRSAKeyPair is the RSA equivalent of writeKeyPair, unencrypted
// only — used to exercise Bits reporting for a non-fixed-size algorithm.
func writeRSAKeyPair(t *testing.T, dir, name string, bits int) string {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("MarshalPrivateKey: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}

	sshPub, err := ssh.NewPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".pub"), ssh.MarshalAuthorizedKey(sshPub), 0o644); err != nil {
		t.Fatalf("write public key: %v", err)
	}

	return ssh.FingerprintSHA256(sshPub)
}

func TestScanKeyPairsUnencrypted(t *testing.T) {
	dir := t.TempDir()
	want := writeKeyPair(t, dir, "id_ed25519", "")

	pairs, err := ScanKeyPairs(dir)
	if err != nil {
		t.Fatalf("ScanKeyPairs: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("len(pairs) = %d, want 1", len(pairs))
	}
	kp := pairs[0]
	if kp.Name != "id_ed25519" {
		t.Errorf("Name = %q", kp.Name)
	}
	if !kp.HasPrivate || !kp.HasPublic {
		t.Errorf("HasPrivate = %v, HasPublic = %v, want both true", kp.HasPrivate, kp.HasPublic)
	}
	if kp.Encrypted {
		t.Error("Encrypted = true, want false")
	}
	if kp.Type != "ed25519" || kp.Bits != 256 {
		t.Errorf("Type/Bits = %q/%d, want ed25519/256", kp.Type, kp.Bits)
	}
	if kp.Fingerprint != want {
		t.Errorf("Fingerprint = %q, want %q", kp.Fingerprint, want)
	}
	if kp.Comment != "test-comment" {
		t.Errorf("Comment = %q, want test-comment", kp.Comment)
	}
	if kp.PrivatePermissiveWarning {
		t.Error("PrivatePermissiveWarning = true for a 0600 key, want false")
	}
}

func TestScanKeyPairsEncryptedStillReportsFingerprintFromPublicHalf(t *testing.T) {
	dir := t.TempDir()
	want := writeKeyPair(t, dir, "id_ed25519", "correct-horse")

	pairs, err := ScanKeyPairs(dir)
	if err != nil {
		t.Fatalf("ScanKeyPairs: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("len(pairs) = %d, want 1", len(pairs))
	}
	kp := pairs[0]
	if !kp.Encrypted {
		t.Error("Encrypted = false, want true")
	}
	if kp.ParseError != "" {
		t.Errorf("ParseError = %q, want empty (a passphrase requirement is not a parse error)", kp.ParseError)
	}
	// Fingerprint comes from the ".pub" file here (the normal case); the
	// PassphraseMissingError.PublicKey fallback is exercised separately
	// below with no ".pub" file present at all.
	if kp.Fingerprint != want {
		t.Errorf("Fingerprint = %q, want %q", kp.Fingerprint, want)
	}
}

func TestScanKeyPairsEncryptedWithoutPublicFileUsesEmbeddedPublicKey(t *testing.T) {
	dir := t.TempDir()
	want := writeKeyPair(t, dir, "id_ed25519", "correct-horse")
	if err := os.Remove(filepath.Join(dir, "id_ed25519.pub")); err != nil {
		t.Fatalf("remove .pub: %v", err)
	}

	pairs, err := ScanKeyPairs(dir)
	if err != nil {
		t.Fatalf("ScanKeyPairs: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("len(pairs) = %d, want 1", len(pairs))
	}
	kp := pairs[0]
	if kp.HasPublic {
		t.Error("HasPublic = true, want false: no .pub file was written")
	}
	if !kp.Encrypted {
		t.Error("Encrypted = false, want true")
	}
	if kp.Fingerprint != want {
		t.Errorf("Fingerprint = %q, want %q (from the embedded, still-cleartext public half)", kp.Fingerprint, want)
	}
	if kp.Type != "ed25519" {
		t.Errorf("Type = %q, want ed25519", kp.Type)
	}
}

func TestScanKeyPairsRSAReportsBits(t *testing.T) {
	dir := t.TempDir()
	writeRSAKeyPair(t, dir, "id_rsa", 2048)

	pairs, err := ScanKeyPairs(dir)
	if err != nil {
		t.Fatalf("ScanKeyPairs: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("len(pairs) = %d, want 1", len(pairs))
	}
	if pairs[0].Type != "rsa" || pairs[0].Bits != 2048 {
		t.Errorf("Type/Bits = %q/%d, want rsa/2048", pairs[0].Type, pairs[0].Bits)
	}
}

func TestScanKeyPairsFlagsPermissiveMode(t *testing.T) {
	dir := t.TempDir()
	writeKeyPair(t, dir, "id_ed25519", "")
	if err := os.Chmod(filepath.Join(dir, "id_ed25519"), 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	pairs, err := ScanKeyPairs(dir)
	if err != nil {
		t.Fatalf("ScanKeyPairs: %v", err)
	}
	if !pairs[0].PrivatePermissiveWarning {
		t.Error("PrivatePermissiveWarning = false for a 0644 key, want true")
	}
}

func TestScanKeyPairsSkipsKnownNonKeyFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"authorized_keys", "known_hosts", "config"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("not a key\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	pairs, err := ScanKeyPairs(dir)
	if err != nil {
		t.Fatalf("ScanKeyPairs: %v", err)
	}
	if len(pairs) != 0 {
		t.Fatalf("len(pairs) = %d, want 0: known non-key files must never be reported as orphan private keys", len(pairs))
	}
}

func TestScanKeyPairsFindsOrphanPrivateKeyWithNoPublicFile(t *testing.T) {
	dir := t.TempDir()
	writeKeyPair(t, dir, "id_ed25519", "")
	if err := os.Remove(filepath.Join(dir, "id_ed25519.pub")); err != nil {
		t.Fatalf("remove .pub: %v", err)
	}

	pairs, err := ScanKeyPairs(dir)
	if err != nil {
		t.Fatalf("ScanKeyPairs: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("len(pairs) = %d, want 1", len(pairs))
	}
	if !pairs[0].HasPrivate || pairs[0].HasPublic {
		t.Errorf("HasPrivate = %v, HasPublic = %v, want true/false", pairs[0].HasPrivate, pairs[0].HasPublic)
	}
}

func TestScanKeyPairsReportsCorruptPrivateKeyAsParseError(t *testing.T) {
	dir := t.TempDir()
	corrupt := "-----BEGIN OPENSSH PRIVATE KEY-----\nbm90IGEgcmVhbCBrZXk=\n-----END OPENSSH PRIVATE KEY-----\n"
	if err := os.WriteFile(filepath.Join(dir, "id_broken"), []byte(corrupt), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	pairs, err := ScanKeyPairs(dir)
	if err != nil {
		t.Fatalf("ScanKeyPairs: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("len(pairs) = %d, want 1", len(pairs))
	}
	if pairs[0].ParseError == "" {
		t.Error("ParseError = \"\", want a real parse failure reported")
	}
}

func TestScanKeyPairsMissingDirReturnsError(t *testing.T) {
	if _, err := ScanKeyPairs(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Error("ScanKeyPairs on a missing directory: want error, got nil")
	}
}

func TestScanKeyPairsSortedByName(t *testing.T) {
	dir := t.TempDir()
	writeKeyPair(t, dir, "id_zzz", "")
	writeKeyPair(t, dir, "id_aaa", "")

	pairs, err := ScanKeyPairs(dir)
	if err != nil {
		t.Fatalf("ScanKeyPairs: %v", err)
	}
	if len(pairs) != 2 || pairs[0].Name != "id_aaa" || pairs[1].Name != "id_zzz" {
		t.Fatalf("pairs = %+v, want [id_aaa, id_zzz]", pairs)
	}
}

func TestDefaultDir(t *testing.T) {
	dir := DefaultDir()
	if dir == "" {
		t.Skip("no home directory available in this environment")
	}
	if filepath.Base(dir) != ".ssh" {
		t.Errorf("DefaultDir() = %q, want a path ending in .ssh", dir)
	}
}
