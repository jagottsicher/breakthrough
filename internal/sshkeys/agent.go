package sshkeys

import (
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// AgentFingerprints returns the SHA256 fingerprints of every identity
// currently loaded in the running ssh-agent behind SSH_AUTH_SOCK — the
// same environment variable internal/ui's own connect dialog already
// reads for agent-based SFTP auth (see connectdialog.go). It talks to
// the agent over its own wire protocol (golang.org/x/crypto/ssh/agent),
// never the ssh-add binary: ssh-add has no way to list fingerprints
// without also risking an interactive passphrase prompt on a locked
// identity.
//
// Returns nil, with no error, when no agent is reachable at all —
// "nothing is loaded" and "there is no agent to ask" look identical on
// the Keys screen's own Agent column, and forcing every non-agent
// session to show a permanent error would be noise, not information.
func AgentFingerprints() map[string]bool {
	socketPath := os.Getenv("SSH_AUTH_SOCK")
	if socketPath == "" {
		return nil
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil
	}
	defer func() { _ = conn.Close() }()

	keys, err := agent.NewClient(conn).List()
	if err != nil {
		return nil
	}

	fingerprints := make(map[string]bool, len(keys))
	for _, key := range keys {
		pub, err := ssh.ParsePublicKey(key.Blob)
		if err != nil {
			continue
		}
		fingerprints[ssh.FingerprintSHA256(pub)] = true
	}
	return fingerprints
}
