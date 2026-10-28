package apikey

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Tier identifies which of the three fixed tiers (see doc.go)
// actually supplied a resolved key, so callers such as the Options
// screen can show the user where a given provider's key is coming
// from (the same idea as config.Origin, reused here for keys).
type Tier int

const (
	TierNone Tier = iota
	TierEnv
	TierFile
	TierEncrypted
)

func (t Tier) String() string {
	switch t {
	case TierEnv:
		return "environment variable"
	case TierFile:
		return "file"
	case TierEncrypted:
		return "encrypted store"
	default:
		return "none"
	}
}

// Resolve tries the three tiers in their fixed order — env var, then
// configured plaintext file, then the encrypted session — and returns
// whichever one actually supplies a key, along with which tier that
// was. Any of envVar, filePath or session may be empty/nil, meaning
// that tier is simply not configured and is skipped.
//
// A configured file that doesn't exist yet is treated the same as
// "not configured" and falls through to the next tier — the user may
// simply not have created it yet, e.g. while env var or encrypted
// storage is their actual current source. A configured file that
// exists but can't be read for another reason (permissions, is a
// directory, ...) is a real misconfiguration and is surfaced as an
// error instead of silently falling through, per CLAUDE.md's "keine
// stillen Fehler" rule.
func Resolve(envVar, filePath, provider string, session *Session) (key string, tier Tier, err error) {
	if envVar != "" {
		if v := os.Getenv(envVar); v != "" {
			return v, TierEnv, nil
		}
	}

	if filePath != "" {
		data, readErr := os.ReadFile(filePath)
		switch {
		case readErr == nil:
			if v := strings.TrimSpace(string(data)); v != "" {
				return v, TierFile, nil
			}
		case os.IsNotExist(readErr):
			// Not yet created — fall through to the next tier.
		default:
			return "", TierNone, fmt.Errorf("apikey: reading configured key file %q: %w", filePath, readErr)
		}
	}

	if session != nil && session.IsUnlocked() {
		v, getErr := session.Get(provider)
		if getErr == nil {
			return v, TierEncrypted, nil
		}
		if !errors.Is(getErr, ErrNotFound) {
			return "", TierNone, getErr
		}
	}

	return "", TierNone, nil
}
