package sshkeys

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestPublicKeyDetailsECDSA(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pub, err := ssh.NewPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}

	typ, bits := publicKeyDetails(pub)
	if typ != "ecdsa" || bits != 256 {
		t.Errorf("publicKeyDetails() = %q/%d, want ecdsa/256", typ, bits)
	}
}
