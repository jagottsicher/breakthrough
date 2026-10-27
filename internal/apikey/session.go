package apikey

import (
	"errors"
	"sync"
)

// ErrLocked is returned by Session.Get/Set when no passphrase has
// been unlocked yet for this process. The Options screen is expected
// to prompt for a passphrase and call Unlock in response, rather than
// treating this as a fatal error.
var ErrLocked = errors.New("apikey: session is locked")

// Session caches a passphrase-derived key in memory for the lifetime
// of the process, the same ssh-agent/gpg-agent UX: prompt once per
// session, not once per call. The derived key is never written to
// disk and never logged; Lock zeroes it out explicitly rather than
// just dropping the reference.
type Session struct {
	mu    sync.Mutex
	store *Store
	key   []byte
}

// OpenSession loads the store at path (creating nothing on disk yet —
// LoadStore treats a missing file as an empty store) and returns a
// locked Session over it.
func OpenSession(path string) (*Session, error) {
	store, err := LoadStore(path)
	if err != nil {
		return nil, err
	}
	return &Session{store: store}, nil
}

// IsUnlocked reports whether a passphrase has already been verified
// and cached for this session.
func (s *Session) IsUnlocked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.key != nil
}

// Unlock derives a key from passphrase and, if the store already has
// at least one entry, verifies it against one of them before caching
// it — a wrong passphrase against an existing store is rejected here,
// not silently accepted and only failing on the next Get. Against a
// brand-new, empty store there is nothing yet to verify against, so
// any passphrase is accepted as the one that will be used going
// forward (the same way setting a new passphrase on an empty pass/age
// store works).
func (s *Session) Unlock(passphrase string) error {
	key, err := s.store.deriveKey(passphrase)
	if err != nil {
		return err
	}
	if err := s.store.verify(key); err != nil {
		return err
	}
	s.mu.Lock()
	s.key = key
	s.mu.Unlock()
	return nil
}

// Lock discards the cached key, zeroing it in memory first.
func (s *Session) Lock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.key {
		s.key[i] = 0
	}
	s.key = nil
}

func (s *Session) cachedKey() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.key
}

// Get returns the decrypted key for provider. Returns ErrLocked if no
// passphrase has been unlocked yet, ErrNotFound if the store has no
// entry for provider, or ErrWrongPassphrase if decryption fails
// (which Unlock's own verify step should already have ruled out for
// any entry present at Unlock time).
func (s *Session) Get(provider string) (string, error) {
	key := s.cachedKey()
	if key == nil {
		return "", ErrLocked
	}
	return s.store.getWithKey(provider, key)
}

// Set encrypts plaintext for provider with the cached key and
// persists the store immediately.
func (s *Session) Set(provider, plaintext string) error {
	key := s.cachedKey()
	if key == nil {
		return ErrLocked
	}
	return s.store.setWithKey(provider, plaintext, key)
}

// HasAnyEntry reports whether the underlying store has at least one
// provider key stored already — usable even while locked, so a caller
// deciding between an "unlock" prompt (a single passphrase field) and
// a "set a passphrase for the first time" one (passphrase + confirm)
// can tell which case it's in before ever deriving a key.
func (s *Session) HasAnyEntry() bool {
	return s.store.HasAnyEntry()
}

// HasProvider reports whether the underlying store already has an
// entry for provider, usable even while locked (e.g. so the Options
// screen can show "a key is stored" without prompting for a
// passphrase just to render that fact).
func (s *Session) HasProvider(provider string) bool {
	_, ok := s.store.file.Entries[provider]
	return ok
}
