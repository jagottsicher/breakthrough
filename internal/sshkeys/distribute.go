package sshkeys

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultPort is the port ssh itself falls back to whenever none is
// given explicitly — the same default internal/rsync's own Endpoint
// already assumes for the identical reason.
const DefaultPort = 22

// ReadPublicKeyLine returns name+".pub"'s own exact content, trimmed of
// its trailing newline — the literal line InstallScript embeds into the
// remote authorized_keys file, never reconstructed from the parsed
// KeyPair fields (type/comment/fingerprint): a reconstruction could
// drift from byte-for-byte what the actual file holds (extra options
// before the key type, an unusual comment with embedded whitespace),
// and authorized_keys is exactly the file where "close enough" already
// means "does not authenticate".
func ReadPublicKeyLine(dir, name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, name+".pub"))
	if err != nil {
		return "", err
	}
	line := strings.TrimRight(string(data), "\n")
	if line == "" {
		return "", fmt.Errorf("%s.pub is empty", name)
	}
	if strings.Contains(line, "\n") {
		return "", fmt.Errorf("%s.pub has more than one line", name)
	}
	return line, nil
}

// InstallScript returns the POSIX sh script (no ssh wrapper of its own)
// that installs pubKeyLine into the *remote* user's own
// ~/.ssh/authorized_keys — the same end result `ssh-copy-id` produces,
// built explicitly here instead so this app controls every one of its
// own three safety properties directly:
//
//   - Never a duplicate: grep -qxF checks for the exact line first,
//     the same idempotency `ssh-copy-id` itself provides.
//   - Never silently overwritten: an existing authorized_keys is
//     copied to a timestamped backup right beside it before anything
//     is appended.
//   - Never a locked-out remote account: ~/.ssh and authorized_keys
//     itself get their required 700/600 permissions explicitly —
//     sshd refuses a key installed with looser ones.
//
// Prints exactly one of two words on its own last line ("ADDED" or
// "ALREADY_PRESENT") — checked by InstallCommand's own caller-facing
// contract, not scraped from prose, so a locale-dependent message from
// any of the plain POSIX utilities used here can never break that
// check.
func InstallScript(pubKeyLine string) string {
	quoted := shellQuoteArg(pubKeyLine)
	return "mkdir -p ~/.ssh && chmod 700 ~/.ssh && " +
		"touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys && " +
		"if grep -qxF " + quoted + " ~/.ssh/authorized_keys; then " +
		"echo ALREADY_PRESENT; " +
		"else " +
		"cp -p ~/.ssh/authorized_keys ~/.ssh/authorized_keys.bak.\"$(date +%Y%m%d%H%M%S)\" && " +
		"printf '%s\\n' " + quoted + " >> ~/.ssh/authorized_keys && " +
		"echo ADDED; " +
		"fi"
}

// InstallCommand returns the exact `ssh` command line that runs
// InstallScript's own script on host, through a real, attached
// terminal (see internal/ui's own runSSHKeysCommandFullScreen):
// whichever auth method the target actually needs — agent, a
// passphrase-protected key, or a password — needs one regardless of
// which it turns out to be, the same reasoning sudo's own prompt
// already requires one for the Firewall screen's Add-rule form.
func InstallCommand(host string, port int, user, pubKeyLine string) (string, error) {
	if err := validateTarget(host, user); err != nil {
		return "", err
	}
	if pubKeyLine == "" {
		return "", errors.New("no public key to install")
	}
	return sshCommand(host, port, user, nil, InstallScript(pubKeyLine)), nil
}

// TestCommand returns the exact `ssh` command line that checks whether
// user@host now actually accepts privateKeyPath with no password or
// passphrase prompt at all — the literal meaning of "passwordless
// access", not just "the key is now listed in authorized_keys" (a
// locked private key, or a server-side sshd policy, could still block
// it). -o BatchMode=yes never falls back to an interactive prompt;
// -o IdentitiesOnly=yes -i privateKeyPath forces exactly this key, not
// whichever one an already-loaded agent might offer instead — a false
// "it works" from a *different*, already-authorized key would defeat
// the entire point of this check.
func TestCommand(host string, port int, user, privateKeyPath string) (string, error) {
	if err := validateTarget(host, user); err != nil {
		return "", err
	}
	if privateKeyPath == "" {
		return "", errors.New("no private key to test")
	}
	opts := []string{
		"-o", "BatchMode=yes",
		"-o", "IdentitiesOnly=yes",
		"-o", "PasswordAuthentication=no",
		"-o", "ConnectTimeout=10",
		"-i", shellQuoteArg(privateKeyPath),
	}
	return sshCommand(host, port, user, opts, "true"), nil
}

func validateTarget(host, user string) error {
	if host == "" {
		return errors.New("host is required")
	}
	if user == "" {
		return errors.New("user is required")
	}
	return nil
}

// sshCommand assembles "ssh [extraOpts] [-p port] user@host remoteCommand"
// — one shared builder so InstallCommand/TestCommand can never drift
// apart on quoting or on how a non-default port is spelled.
func sshCommand(host string, port int, user string, extraOpts []string, remoteCommand string) string {
	if port == 0 {
		port = DefaultPort
	}
	parts := []string{"ssh"}
	parts = append(parts, extraOpts...)
	if port != DefaultPort {
		parts = append(parts, "-p", strconv.Itoa(port))
	}
	parts = append(parts, shellQuoteArg(user+"@"+host), shellQuoteArg(remoteCommand))
	return strings.Join(parts, " ")
}
