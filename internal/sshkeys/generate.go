package sshkeys

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// RSABitsChoices/ECDSABitsChoices are the only key sizes offered for a
// new rsa/ecdsa key pair — the same short, conventional lists
// `ssh-keygen` itself and every common guide already treat as the real
// choices (2048 the now-dated but still-accepted floor, 4096 the modern
// default; P-256/P-384/P-521, the only three curves `ssh-keygen -t
// ecdsa` itself accepts) — never a free-text bit count that could
// produce a key too weak to be worth generating at all.
var (
	RSABitsChoices   = []int{2048, 3072, 4096}
	ECDSABitsChoices = []int{256, 384, 521}
)

// GenerateSpec describes a new key pair to create, filled in from the
// "Generate key" form — never raw ssh-keygen syntax typed by hand.
type GenerateSpec struct {
	// Algorithm is "ed25519", "rsa", or "ecdsa" — dsa is deliberately
	// never offered here: reading an existing dsa key back (see
	// ScanKeyPairs) still has to work, generating a new, already-
	// deprecated one deliberately doesn't.
	Algorithm string
	// Bits is meaningful only for rsa/ecdsa (see RSABitsChoices/
	// ECDSABitsChoices) — ignored for ed25519, which has exactly one,
	// fixed size.
	Bits int
	// Filename is a bare filename inside the target directory (see
	// FilenameAvailable), never a path: this app only ever generates
	// into ~/.ssh, the same fixed, conventional location every other
	// key pair here already lives in.
	Filename string
	Comment  string
}

// Validate checks GenerateSpec's own fields in isolation, with no
// filesystem access at all — see FilenameAvailable for the one check
// that needs one (whether Filename already exists), kept separate so
// this can be exercised as a pure function.
func (s GenerateSpec) Validate() error {
	switch s.Algorithm {
	case "ed25519":
		// No Bits to check — every ed25519 key is the same fixed size.
	case "rsa":
		if !intInSlice(s.Bits, RSABitsChoices) {
			return fmt.Errorf("rsa key size must be one of %v, got %d", RSABitsChoices, s.Bits)
		}
	case "ecdsa":
		if !intInSlice(s.Bits, ECDSABitsChoices) {
			return fmt.Errorf("ecdsa curve size must be one of %v, got %d", ECDSABitsChoices, s.Bits)
		}
	default:
		return fmt.Errorf("unsupported algorithm %q", s.Algorithm)
	}

	if s.Filename == "" {
		return errors.New("filename is required")
	}
	if strings.ContainsRune(s.Filename, '/') {
		return errors.New("filename must not contain a path separator")
	}
	if s.Filename == "." || s.Filename == ".." {
		return fmt.Errorf("%q is not a valid filename", s.Filename)
	}
	return nil
}

func intInSlice(v int, choices []int) bool {
	for _, c := range choices {
		if v == c {
			return true
		}
	}
	return false
}

// FilenameAvailable reports whether name is safe to use as a new key
// pair's own filename inside dir: neither the private half (name) nor
// the public half (name+".pub") may already exist, so generating a new
// key pair can never silently overwrite an existing one — the same
// "irreversible action" floor Trash/Remove/Purge already hold, applied
// here to "would silently destroy an existing private key" rather than
// a delete.
func FilenameAvailable(dir, name string) error {
	for _, suffix := range []string{"", ".pub"} {
		path := filepath.Join(dir, name+suffix)
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists", filepath.Base(path))
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// GenerateCommand returns the exact `ssh-keygen` command line that
// would create spec's own key pair inside dir. Deliberately never
// passes a passphrase on the command line at all (no `-N`): a
// passphrase there would sit in this process's own argv, and therefore
// in `ps` output and most shells' own history, for as long as the
// command exists — `ssh-keygen`'s own interactive prompt (asked twice,
// blank for "no passphrase") is the only way to set one that never
// touches either. Callers run this through a real, attached terminal
// (see internal/ui's own runSSHKeysGenerateCommandFullScreen) for
// exactly that reason.
func GenerateCommand(dir string, spec GenerateSpec) (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}

	path := filepath.Join(dir, spec.Filename)
	parts := []string{"ssh-keygen", "-t", spec.Algorithm}
	if spec.Algorithm == "rsa" || spec.Algorithm == "ecdsa" {
		parts = append(parts, "-b", strconv.Itoa(spec.Bits))
	}
	parts = append(parts, "-f", shellQuoteArg(path))
	if spec.Comment != "" {
		parts = append(parts, "-C", shellQuoteArg(spec.Comment))
	}
	return strings.Join(parts, " "), nil
}

// shellQuoteArg wraps s in single quotes for safe use inside a real
// shell command line — the same POSIX sh idiom (and the same escaping)
// internal/ui's own shellQuoteArg (compress.go) and internal/rsync's
// own shellQuote already use for exactly this reason; duplicated here
// rather than exported and shared, since it's a three-line, fully
// self-contained utility and neither package has any other reason to
// depend on the other.
func shellQuoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
