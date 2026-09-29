package sshkeys

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/crypto/ssh"
)

// DefaultDir returns the user's own ~/.ssh — the same directory
// internal/remotefs's own defaultIdentityFiles (auth.go) already
// searches for its default identity files — or "" if the home directory
// can't be determined.
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh")
}

// knownNonKeyFiles are ~/.ssh's own well-known non-key-pair residents —
// never mistaken for an orphan private key even though they, too, hold
// no matching ".pub" sibling.
var knownNonKeyFiles = map[string]bool{
	"authorized_keys":  true,
	"authorized_keys2": true,
	"known_hosts":      true,
	"known_hosts.old":  true,
	"config":           true,
	"environment":      true,
	"rc":               true,
}

// ScanKeyPairs inventories every SSH key pair found directly inside dir
// (never recursing into subdirectories such as per-connection
// control-master sockets some setups keep there).
//
// Discovery starts from every ".pub" file — the normal case — then adds
// any remaining private-looking file (a PEM/OpenSSH private key with no
// ".pub" sibling at all) as its own, incomplete entry, so a key pair
// missing its public half is reported rather than silently skipped.
func ScanKeyPairs(dir string) ([]KeyPair, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(entries))
	var pairs []KeyPair

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pub") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".pub")
		seen[name] = true
		pairs = append(pairs, scanOnePair(dir, name))
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || seen[name] || knownNonKeyFiles[name] || strings.HasSuffix(name, ".pub") {
			continue
		}
		if looksLikePrivateKey(filepath.Join(dir, name)) {
			seen[name] = true
			pairs = append(pairs, scanOnePair(dir, name))
		}
	}

	sort.Slice(pairs, func(i, j int) bool { return pairs[i].Name < pairs[j].Name })
	return pairs, nil
}

// looksLikePrivateKey is a cheap, sufficient discriminator between a
// private key file and everything else ~/.ssh might hold (sockets,
// per-host notes, stray files) — every PEM and OpenSSH private key
// format shares the literal "PRIVATE KEY" substring on its very first
// line, so reading a small header is enough; no need to parse the whole
// file just to decide whether it is worth parsing at all.
func looksLikePrivateKey(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 64)
	n, _ := f.Read(buf)
	return bytes.Contains(buf[:n], []byte("PRIVATE KEY"))
}

// scanOnePair fills in one KeyPair from whichever of name+".pub" and
// name actually exist in dir — either half may be missing.
func scanOnePair(dir, name string) KeyPair {
	kp := KeyPair{Name: name}

	if data, err := os.ReadFile(filepath.Join(dir, name+".pub")); err == nil {
		kp.HasPublic = true
		if pub, comment, _, _, err := ssh.ParseAuthorizedKey(data); err == nil {
			kp.Type, kp.Bits = publicKeyDetails(pub)
			kp.Fingerprint = ssh.FingerprintSHA256(pub)
			kp.Comment = comment
		}
	}

	privPath := filepath.Join(dir, name)
	info, err := os.Stat(privPath)
	if err != nil || info.IsDir() {
		return kp
	}
	kp.HasPrivate = true
	kp.PrivateMode = info.Mode().Perm()
	kp.PrivatePermissiveWarning = kp.PrivateMode&0o077 != 0

	data, err := os.ReadFile(privPath)
	if err != nil {
		return kp
	}
	if _, err := ssh.ParseRawPrivateKey(data); err != nil {
		missing, ok := err.(*ssh.PassphraseMissingError)
		if !ok {
			kp.ParseError = err.Error()
			return kp
		}
		kp.Encrypted = true
		// A modern OpenSSH-format encrypted key still carries its own
		// public half in the clear — exactly what lets `ssh-keygen -lf`
		// show a fingerprint for an encrypted key without ever asking
		// for its passphrase. Prefer the already-read ".pub" file above
		// when present; only fall back to this when there was none.
		if missing.PublicKey != nil && kp.Fingerprint == "" {
			kp.Type, kp.Bits = publicKeyDetails(missing.PublicKey)
			kp.Fingerprint = ssh.FingerprintSHA256(missing.PublicKey)
		}
	}
	return kp
}
