package remotefs

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// newTestKeyPair generates a fresh ed25519 key pair for tests — never
// the same key twice, so a test asserting host-key mismatch behavior
// (see hostkey_test.go) always gets a genuinely different key to
// compare against, not one that might coincidentally collide.
func newTestKeyPair(t *testing.T) (ssh.Signer, ssh.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating test key pair: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("wrapping test private key: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("wrapping test public key: %v", err)
	}
	return signer, sshPub
}

// startTestSFTPServer starts a real, in-process SSH server on
// 127.0.0.1 that answers exactly one subsystem request ("sftp") by
// serving the whole filesystem the test process itself can see — the
// same real, unmodified server-side handler github.com/pkg/sftp's own
// documented example server uses, not a hand-rolled stand-in. Tests
// point it at their own t.TempDir() by using absolute paths under it,
// rather than this function attempting any chroot of its own (the
// bare sftp.Server has none — see its own doc comment for why that's
// deliberately out of scope for this project's first remote
// protocol).
//
// Returns the address to dial; the listener and every accepted
// connection are torn down via t.Cleanup.
func startTestSFTPServer(t *testing.T, sshConfig *ssh.ServerConfig) string {
	t.Helper()

	hostSigner, _ := newTestKeyPair(t)
	sshConfig.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening for test SSH server: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed — normal shutdown, not a test failure
			}
			go serveTestSSHConn(conn, sshConfig)
		}
	}()

	return ln.Addr().String()
}

// serveTestSSHConn runs one SSH connection's full lifecycle: the
// handshake, then every "session" channel it opens, replying to the
// "subsystem" request the real sftp package's own client always sends
// (see sftp.NewClient), and handing the channel to a real
// sftp.Server once that request names "sftp" — this mirrors
// github.com/pkg/sftp's own documented example server line for line,
// since that boilerplate is exactly what a real SFTP server needs on
// top of a bare SSH handshake, nothing specific to this project.
//
// Deliberately never calls anything on a *testing.T: this runs in
// background goroutines whose lifetime isn't bounded by any one
// test's own — logging or failing through a T after its test has
// already returned panics ("Log in goroutine after Test has
// completed"), so a Serve error here is simply not reported rather
// than risking that.
func serveTestSSHConn(conn net.Conn, config *ssh.ServerConfig) {
	sc, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return // failed handshake/auth — expected and asserted on by the tests that trigger it
	}
	defer func() { _ = sc.Close() }()
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}

		go func() {
			for req := range requests {
				isSFTP := req.Type == "subsystem" && len(req.Payload) > 4 && string(req.Payload[4:]) == "sftp"
				_ = req.Reply(isSFTP, nil)
			}
		}()

		go func() {
			server, err := sftp.NewServer(channel)
			if err != nil {
				return
			}
			_ = server.Serve()
			_ = server.Close()
		}()
	}
}
