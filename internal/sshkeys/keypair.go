package sshkeys

import (
	"crypto/dsa" //nolint:staticcheck // ssh-dss is deprecated but still a real, existing key type this screen must still report on, not silently drop.
	"crypto/ecdsa"
	"crypto/rsa"
	"io/fs"

	"golang.org/x/crypto/ssh"
)

// KeyPair describes one local SSH key pair found under a scanned
// directory (see ScanKeyPairs) — one row in the Keys screen.
type KeyPair struct {
	// Name is the private key's own filename, e.g. "id_ed25519" — the
	// same base name ssh/scp/ssh-copy-id would take with -i.
	Name string

	HasPrivate bool
	HasPublic  bool

	// Type is the algorithm's short conventional name (ed25519, rsa,
	// ecdsa, dsa), or the raw ssh.PublicKey.Type() string for anything
	// unrecognized, or "" if it couldn't be determined at all (an
	// encrypted legacy-PEM private key with no accompanying ".pub").
	Type string
	// Bits is the key size in bits (256 for ed25519, always fixed-size,
	// same as `ssh-keygen -lf` itself reports); 0 when unknown.
	Bits int

	Fingerprint string
	Comment     string

	// Encrypted is whether the private key itself is passphrase-
	// protected. Meaningless (false) when HasPrivate is false.
	Encrypted bool

	// PrivateMode is the private key file's own permission bits.
	// PrivatePermissiveWarning is true when group or other has any
	// permission at all on it — sshd itself refuses to use such a key.
	PrivateMode              fs.FileMode
	PrivatePermissiveWarning bool

	// ParseError holds a private key's own parse failure that is not
	// simply "needs a passphrase" — a corrupt or unsupported file, shown
	// to the user instead of silently dropped from the list.
	ParseError string

	// AgentLoaded is filled in separately by the caller (see
	// AgentFingerprints, matched by Fingerprint) — scanning itself never
	// talks to the agent.
	AgentLoaded bool
}

// publicKeyDetails maps a parsed public key to this package's own short,
// conventional type name and bit size, the same pair `ssh-keygen -lf`
// itself prints (e.g. "256 SHA256:… (ED25519)"). Falls back to the raw
// wire type name with no bit size for anything not one of the four
// well-known algorithms.
func publicKeyDetails(pub ssh.PublicKey) (typ string, bits int) {
	switch pub.Type() {
	case ssh.KeyAlgoED25519:
		return "ed25519", 256
	case ssh.KeyAlgoRSA:
		typ = "rsa"
	case ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
		typ = "ecdsa"
	case ssh.KeyAlgoDSA: //nolint:staticcheck // deprecated, but a real, existing key type this read-only inventory must still report on.
		typ = "dsa"
	default:
		return pub.Type(), 0
	}

	cryptoPub, ok := pub.(ssh.CryptoPublicKey)
	if !ok {
		return typ, 0
	}
	switch key := cryptoPub.CryptoPublicKey().(type) {
	case *rsa.PublicKey:
		bits = key.N.BitLen()
	case *ecdsa.PublicKey:
		bits = key.Curve.Params().BitSize
	case *dsa.PublicKey:
		bits = key.P.BitLen()
	}
	return typ, bits
}
