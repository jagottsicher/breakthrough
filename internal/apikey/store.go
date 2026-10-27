package apikey

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/scrypt"
)

// formatVersion identifies the on-disk layout. Bumped whenever the
// KDF or cipher choice changes, so a future breakthrough can tell an
// old file apart from a corrupt one instead of just failing to parse it.
const formatVersion = 1

// Default scrypt cost parameters for newly created stores. Stored in
// the file itself (not just read from these constants) so raising the
// cost later doesn't orphan keys written under the old cost.
const (
	defaultScryptN = 1 << 15 // 32768
	defaultScryptR = 8
	defaultScryptP = 1
	scryptKeyLen   = 32 // AES-256
	saltLen        = 16
)

// Sentinel errors Resolve/Session rely on to tell the caller exactly
// what went wrong, per CLAUDE.md's "keine stillen Fehler" rule.
var (
	ErrWrongPassphrase = errors.New("apikey: wrong passphrase")
	ErrNotFound        = errors.New("apikey: no key stored for this provider")
)

type entry struct {
	Nonce      string `json:"nonce"`      // base64
	Ciphertext string `json:"ciphertext"` // base64
}

// fileFormat is the exact JSON shape persisted to disk. Every field
// needed to reproduce the derived key (salt, scrypt cost) travels with
// the file, never from a constant that might later change.
type fileFormat struct {
	Version int              `json:"version"`
	Salt    string           `json:"salt"` // base64
	ScryptN int              `json:"scrypt_n"`
	ScryptR int              `json:"scrypt_r"`
	ScryptP int              `json:"scrypt_p"`
	Entries map[string]entry `json:"entries"`
}

// Store is the encrypted-at-rest tier of the three-tier API key chain
// (see doc.go). It holds ciphertext only; it never sees a passphrase
// itself — that derivation and the plaintext in-memory handling live
// in Session, which is the only intended caller for most uses.
type Store struct {
	path string
	file fileFormat
}

// LoadStore reads the store at path. A missing file is not an error —
// it is treated as a brand-new, empty store, mirroring how
// internal/config treats an absent user config file as "nothing
// configured" rather than a failure. A file that exists but can't be
// parsed is a real error: that distinction matters so a corrupt store
// is never mistaken for an empty one.
func LoadStore(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Store{path: path, file: fileFormat{Entries: map[string]entry{}}}, nil
		}
		return nil, fmt.Errorf("apikey: reading store: %w", err)
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("apikey: parsing store: %w", err)
	}
	if f.Entries == nil {
		f.Entries = map[string]entry{}
	}
	return &Store{path: path, file: f}, nil
}

// HasAnyEntry reports whether the store has at least one provider key,
// used by Session to decide whether a passphrase can be verified
// against existing ciphertext or is merely being set for the first time.
func (s *Store) HasAnyEntry() bool {
	return len(s.file.Entries) > 0
}

// save writes the store atomically (via a temp file + rename) with
// mode 0600, matching the github-token.txt precedent already set
// elsewhere in this project for sensitive on-disk files.
func (s *Store) save() error {
	if s.path == "" {
		return fmt.Errorf("apikey: store has no path to save to")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("apikey: creating store directory: %w", err)
	}
	data, err := json.MarshalIndent(s.file, "", "  ")
	if err != nil {
		return fmt.Errorf("apikey: encoding store: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("apikey: writing store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("apikey: committing store: %w", err)
	}
	return nil
}

// ensureSalt fills in salt and KDF parameters on a brand-new store the
// first time a key is ever derived from it. A no-op once set, so the
// same salt/cost keeps being used for every later Set/Unlock on this
// store — changing them on an existing file would silently make all
// its existing entries undecryptable.
func (s *Store) ensureSalt() error {
	if s.file.Salt != "" {
		return nil
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("apikey: generating salt: %w", err)
	}
	s.file.Version = formatVersion
	s.file.Salt = base64.StdEncoding.EncodeToString(salt)
	s.file.ScryptN = defaultScryptN
	s.file.ScryptR = defaultScryptR
	s.file.ScryptP = defaultScryptP
	return nil
}

// deriveKey runs scrypt over passphrase using this store's own salt
// and cost parameters (generating them first if this is a fresh
// store). Two calls with the same passphrase against the same store
// always yield the same key, by construction.
func (s *Store) deriveKey(passphrase string) ([]byte, error) {
	if err := s.ensureSalt(); err != nil {
		return nil, err
	}
	salt, err := base64.StdEncoding.DecodeString(s.file.Salt)
	if err != nil {
		return nil, fmt.Errorf("apikey: decoding salt: %w", err)
	}
	key, err := scrypt.Key([]byte(passphrase), salt, s.file.ScryptN, s.file.ScryptR, s.file.ScryptP, scryptKeyLen)
	if err != nil {
		return nil, fmt.Errorf("apikey: deriving key: %w", err)
	}
	return key, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("apikey: initializing cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

// setWithKey encrypts plaintext for provider using an already-derived
// key and persists the store. Separated from Set (on Session) so
// Session can derive the key once per Unlock instead of once per call.
func (s *Store) setWithKey(provider, plaintext string, key []byte) error {
	gcm, err := newGCM(key)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("apikey: generating nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	if s.file.Entries == nil {
		s.file.Entries = map[string]entry{}
	}
	s.file.Entries[provider] = entry{
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}
	return s.save()
}

// getWithKey decrypts provider's entry using an already-derived key.
// Returns ErrNotFound if no entry exists for provider at all, or
// ErrWrongPassphrase if an entry exists but authentication fails —
// callers (notably the Options screen) must be able to tell those
// two situations apart.
func (s *Store) getWithKey(provider string, key []byte) (string, error) {
	e, ok := s.file.Entries[provider]
	if !ok {
		return "", ErrNotFound
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce, err := base64.StdEncoding.DecodeString(e.Nonce)
	if err != nil {
		return "", ErrWrongPassphrase
	}
	ciphertext, err := base64.StdEncoding.DecodeString(e.Ciphertext)
	if err != nil {
		return "", ErrWrongPassphrase
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrWrongPassphrase
	}
	return string(plaintext), nil
}

// verify checks a derived key against one arbitrary existing entry,
// used by Session.Unlock to reject a wrong passphrase immediately
// rather than only on the first real Get.
func (s *Store) verify(key []byte) error {
	for provider := range s.file.Entries {
		_, err := s.getWithKey(provider, key)
		return err
	}
	return nil // nothing to verify against yet — any passphrase is accepted for a fresh store
}
