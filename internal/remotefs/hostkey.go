package remotefs

import (
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
