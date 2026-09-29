// Package sshkeys inventories the user's own local SSH key pairs
// (~/.ssh/id_* and any other private/public key pairs found there) into
// one normalized KeyPair shape, for internal/ui's own read-only Keys
// screen (Stage 1 of feature_ideas.txt's SSH-Key-Verwaltung entry).
//
// Every key is parsed with the already-vendored golang.org/x/crypto/ssh
// (already a dependency for internal/remotefs's own SFTP client), not a
// second, hand-rolled implementation of OpenSSH's key formats: it is the
// same library that already tells a *ssh.PassphraseMissingError apart
// from every other parse failure, and — for the current OpenSSH private
// key format — still hands back the key's own cleartext public half
// (PassphraseMissingError.PublicKey) even when the private half itself
// is encrypted, exactly like `ssh-keygen -lf` can show a fingerprint for
// an encrypted key without ever asking for its passphrase.
//
// ScanKeyPairs never shells out; AgentFingerprints is the only place
// here that talks to a running process at all (the ssh-agent behind
// SSH_AUTH_SOCK, over its own wire protocol via
// golang.org/x/crypto/ssh/agent — never the ssh-add binary, which has no
// non-interactive-safe way to list fingerprints without also risking a
// passphrase prompt on a locked identity).
package sshkeys
