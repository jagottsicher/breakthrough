package remotefs

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// HostKeyPrompt is asked whenever a host's key isn't yet in the
// known_hosts file — trust-on-first-use, the same thing a real `ssh`
// client's own "The authenticity of host ... can't be established ...
// Are you sure you want to continue connecting (yes/no)?" asks
// interactively. Returning true accepts and permanently records the
// key (see hostKeyCallback); false (or a non-nil error) aborts the
// connection. Never called for a host whose key *changed* — see
// hostKeyCallback's own doc comment for why that case is always
// rejected outright, with no prompt and no bypass.
//
// keyType/fingerprint (ssh.PublicKey's own Type() and
// ssh.FingerprintSHA256(key), computed once here) are passed as plain
// strings rather than the ssh.PublicKey itself, deliberately: a UI
// layer implementing this only ever needs to display them, and
// keeping the ssh package's own types out of that signature means
// nothing above internal/remotefs needs to import it just to show a
// confirmation dialog.
type HostKeyPrompt func(hostname, keyType, fingerprint string) (accept bool, err error)

// DefaultKnownHostsFile is where Dial reads/appends trusted host keys
// unless told otherwise — the exact same file and line format
// `ssh`/`scp`/`sftp` themselves already read and write, so a host
// already trusted from a terminal session is trusted here too, and a
// host accepted here needs no second confirmation from a plain `ssh`
// afterward.
func DefaultKnownHostsFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "known_hosts"), nil
}

// hostKeyCallback builds an ssh.HostKeyCallback backed by the
// known_hosts file at path, creating an empty one first if none
// exists yet — a fresh `~/.ssh` with no known_hosts at all is the
// ordinary state before a machine's very first remote connection, not
// an error condition.
//
// An unknown host (knownhosts.KeyError with an empty Want) is offered
// to prompt for trust-on-first-use. A host whose key *changed* (Want
// non-empty — a previously trusted key no longer matches) is always
// rejected outright, without ever calling prompt at all: that's
// exactly the shape a real machine-in-the-middle attack produces, so
// there is no safe "accept anyway" to offer here the way there is for
// a merely-unknown host.
func hostKeyCallback(path string, prompt HostKeyPrompt) (ssh.HostKeyCallback, error) {
	if err := ensureKnownHostsFile(path); err != nil {
		return nil, fmt.Errorf("preparing %s: %w", path, err)
	}
	base, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		verifyErr := base(hostname, remote, key)
		if verifyErr == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if !errors.As(verifyErr, &keyErr) || len(keyErr.Want) > 0 {
			return verifyErr
		}
		if prompt == nil {
			return verifyErr
		}
		accept, promptErr := prompt(hostname, key.Type(), ssh.FingerprintSHA256(key))
		if promptErr != nil {
			return promptErr
		}
		if !accept {
			return verifyErr
		}
		return appendKnownHost(path, hostname, key)
	}, nil
}

// preferredHostKeyAlgorithms inspects the known_hosts file at path for
// any entries already trusted for hostWithPort ("host:port", the same
// string Dial already passes as both the known_hosts hostname and the
// ssh.NewClientConn addr) and returns their key algorithms
// (ssh-ed25519, rsa-sha2-512, ...), in the order knownhosts' own
// lookup returns them — so Dial's own ssh.ClientConfig can ask the
// server specifically for one of those, rather than letting
// golang.org/x/crypto/ssh's own default negotiation order pick a
// different, equally valid host key the server also offers but that
// was simply never recorded under this algorithm.
//
// Without this, hostKeyCallback's own deliberate "no bypass" refusal
// for a key that changed (see its own doc comment) fires on a false
// positive exactly that way: a real server offering both an RSA and
// an Ed25519 host key, already trusted via Ed25519 by a plain `ssh`
// (OpenSSH's own default preference when both are offered), but this
// project's client negotiating RSA instead (golang.org/x/crypto/ssh's
// own default order, which this project never used to override) and
// finding no RSA entry recorded — reported as "knownhosts: key
// mismatch", indistinguishable from an actual machine-in-the-middle
// from knownhosts' own perspective, even though the host was never
// untrusted at all. A real, user-reported case of exactly this: `ssh
// user@host` connecting instantly while this project's own Connect
// dialog refused the same host outright.
//
// Returns nil (no preference, golang.org/x/crypto/ssh's own default
// order) if the host has no entries yet — the ordinary state before
// its first connection, not an error — or knownHosts can't be read at
// all; Dial's own hostKeyCallback already handles both of those cases
// correctly on its own.
func preferredHostKeyAlgorithms(knownHosts, hostWithPort string) []string {
	base, err := knownhosts.New(knownHosts)
	if err != nil {
		return nil
	}

	probeKey, err := newProbeKey()
	if err != nil {
		return nil
	}

	// probeAddr's own String() is all knownhosts' own HostKeyCallback
	// ever reads from it (see its own db.check: hostWithPort itself is
	// passed as "address", always preferred over "remote" when
	// non-empty) — never dialed, never read any other way. probeKey is
	// likewise never presented to any real server: both exist purely to
	// ask "what, if anything, do you already have recorded for this
	// host", via the *knownhosts.KeyError.Want a guaranteed mismatch
	// against a key nothing could ever actually have recorded is
	// guaranteed to produce.
	verifyErr := base(hostWithPort, probeAddr(hostWithPort), probeKey)
	var keyErr *knownhosts.KeyError
	if !errors.As(verifyErr, &keyErr) {
		return nil
	}

	var algos []string
	seen := make(map[string]bool, len(keyErr.Want))
	for _, want := range keyErr.Want {
		t := want.Key.Type()
		if !seen[t] {
			seen[t] = true
			algos = append(algos, t)
		}
	}
	return algos
}

// probeAddr adapts a plain "host:port" string into the net.Addr
// knownhosts' own HostKeyCallback signature requires — see
// preferredHostKeyAlgorithms' own doc comment on why its String() is
// all that's ever actually read.
type probeAddr string

func (a probeAddr) Network() string { return "tcp" }
func (a probeAddr) String() string  { return string(a) }

// newProbeKey generates a fresh, never-reused Ed25519 key pair purely
// to serve as preferredHostKeyAlgorithms' own throwaway probe —
// Ed25519 only because key generation is cheap and synchronous, not
// because the algorithm itself matters to the probe.
func newProbeKey() (ssh.PublicKey, error) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return ssh.NewPublicKey(pub)
}

func ensureKnownHostsFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

func appendKnownHost(path, hostname string, key ssh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key) + "\n"
	_, err = f.WriteString(line)
	return err
}
