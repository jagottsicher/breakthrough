package remotefs

import (
	"errors"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// PasswordPrompt is asked for a password only once every other
// configured auth method (agent, key files) has already failed or
// wasn't available at all — the same "try the easy, non-interactive
// ways first" order a real `ssh` client already follows. A non-nil
// error aborts the connection instead of retrying.
type PasswordPrompt func() (string, error)

// PassphrasePrompt decrypts one specific private key file found on
// disk (see defaultIdentityFiles) — called at most once per key,
// immediately before that key is tried as an auth method.
type PassphrasePrompt func(keyPath string) (string, error)

// AuthOptions controls how Dial authenticates — see authMethods for
// the order these are actually tried in.
type AuthOptions struct {
	// AgentSocket is the SSH_AUTH_SOCK path to dial for agent-based
	// auth. Left for the caller to fill in (rather than reading
	// os.Getenv here directly) so a test can point this at its own
	// in-process fake agent instead of whatever the real environment
	// happens to have running.
	AgentSocket string

	// IdentityFiles overrides the private key paths tried, in order;
	// nil means the conventional default list (see
	// defaultIdentityFiles).
	IdentityFiles []string

	Passphrase PassphrasePrompt
	Password   PasswordPrompt
}

// authMethods builds every ssh.AuthMethod worth offering the server,
// in the order they should be tried: agent first, then whichever
// default identity files actually exist, then an interactive password
// as the last resort. None of the first two failing to produce
// anything is itself an error — no agent running and no default key
// files present is the common case for a brand new setup, not a
// problem — Dial only fails once the server has rejected every method
// actually offered.
func authMethods(opts AuthOptions) []ssh.AuthMethod {
	var methods []ssh.AuthMethod

	if am, ok := agentAuthMethod(opts.AgentSocket); ok {
		methods = append(methods, am)
	}

	for _, path := range identityFiles(opts.IdentityFiles) {
		if am, ok := keyFileAuthMethod(path, opts.Passphrase); ok {
			methods = append(methods, am)
		}
	}

	if opts.Password != nil {
		// maxTries=1: a wrong password should surface as a real error
		// back through Dial, not silently re-prompt in a loop the
		// caller never asked for — the connection dialog itself is
		// what offers "try again" (a fresh Dial call), not this.
		methods = append(methods, ssh.RetryableAuthMethod(ssh.PasswordCallback(func() (string, error) {
			return opts.Password()
		}), 1))
	}

	return methods
}

func agentAuthMethod(socketPath string) (ssh.AuthMethod, bool) {
	if socketPath == "" {
		return nil, false
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, false
	}
	client := agent.NewClient(conn)
	return ssh.PublicKeysCallback(client.Signers), true
}

// defaultIdentityFiles is the same short, conventional list `ssh`
// itself tries with no -i flag — ed25519 first (the modern default
// `ssh-keygen` itself now produces), then the two older algorithms
// still common in existing setups. Returns nil (nothing to try) if
// the home directory itself can't be determined.
func defaultIdentityFiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	names := []string{"id_ed25519", "id_ecdsa", "id_rsa"}
	files := make([]string, len(names))
	for i, name := range names {
		files[i] = filepath.Join(home, ".ssh", name)
	}
	return files
}

func identityFiles(configured []string) []string {
	if configured != nil {
		return configured
	}
	return defaultIdentityFiles()
}

// keyFileAuthMethod reads and parses path as a private key, prompting
// for a passphrase only if the key is actually encrypted. Returns
// ok=false for anything that isn't a usable auth method — the file
// not existing (overwhelmingly the common case: most of
// defaultIdentityFiles's own three candidates usually aren't there),
// failing to parse, or a passphrase that turns out to be wrong —
// rather than treating any of those as fatal: an unusable candidate
// key just isn't offered, the same way a real `ssh` client silently
// skips one.
func keyFileAuthMethod(path string, passphrase PassphrasePrompt) (ssh.AuthMethod, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	signer, err := ssh.ParsePrivateKey(data)
	if err == nil {
		return ssh.PublicKeys(signer), true
	}

	var passErr *ssh.PassphraseMissingError
	if !errors.As(err, &passErr) || passphrase == nil {
		return nil, false
	}
	pass, err := passphrase(path)
	if err != nil {
		return nil, false
	}
	signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(pass))
	if err != nil {
		return nil, false
	}
	return ssh.PublicKeys(signer), true
}
