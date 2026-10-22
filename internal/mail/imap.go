package mail

import (
	"fmt"
	"net"
	"time"

	imap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// imapDialTimeout bounds how long a single UnreadCount call can spend
// just connecting, before even reaching Login/Status — this runs on
// its own background poll (see internal/ui's own IMAP polling ticker,
// never on the once-a-second status-bar tick Maildir/mbox share), so a
// slow or unreachable server delays the next badge update, not the UI
// itself, but an unbounded default (imapclient.Options' own 30s) would
// still eat most of a short poll interval for nothing once a server is
// simply down.
const imapDialTimeout = 10 * time.Second

// IMAPDefaultPort/IMAPDefaultStartTLSPort are the two conventional IMAP
// ports — 993 for implicit TLS (the connection itself is TLS from the
// first byte), 143 for a plaintext connection that then upgrades via
// STARTTLS. Either is "the right default" only in the matching mode —
// see IMAPConfig.StartTLS.
const (
	IMAPDefaultPort         = 993
	IMAPDefaultStartTLSPort = 143
)

// IMAPDefaultMailbox is STATUS's own target when Mailbox is left
// unset — "INBOX" is the one mailbox name every IMAP server is
// required to have (RFC 9051), unlike any other folder name, which
// varies by server/client convention (Sent, Gesendet, [Gmail]/Sent
// Mail, ...).
const IMAPDefaultMailbox = "INBOX"

// IMAPConfig is everything IMAPSource needs beyond the password itself
// — which it never holds; see lookupNetrc's own doc comment on why.
// Host doubles as both the server address to dial and the netrc
// "machine" name to look the password up under — the same assumption
// curl/git/etc. already make for their own netrc use, and the reason
// there's no separate "netrc machine name" field here.
type IMAPConfig struct {
	Host     string
	Port     int    // 0 means IMAPDefaultPort or IMAPDefaultStartTLSPort, matching StartTLS
	Mailbox  string // "" means IMAPDefaultMailbox
	StartTLS bool   // false: implicit TLS (port 993 default); true: plaintext then STARTTLS (port 143 default)
}

func (c IMAPConfig) port() int {
	if c.Port != 0 {
		return c.Port
	}
	if c.StartTLS {
		return IMAPDefaultStartTLSPort
	}
	return IMAPDefaultPort
}

func (c IMAPConfig) mailbox() string {
	if c.Mailbox != "" {
		return c.Mailbox
	}
	return IMAPDefaultMailbox
}

// IMAPSource is one IMAP mailbox — see feature_ideas.txt's own "0f.
// E-Mail-Client-Integration" entry. Unlike MaildirSource/MboxSource,
// this one talks to the network: breakthrough never stores the
// password itself (lookupNetrc reads it fresh from ~/.netrc every
// call, per the user's own explicit decision), and every call here
// opens, authenticates, and tears down its own connection — no
// long-lived IMAP session kept open in the background. That's a
// deliberate simplicity choice over IDLE (feature_ideas.txt's own
// other option): one STATUS command per poll is cheap enough on any
// real server that a persistent connection's own reconnect/keepalive
// lifecycle isn't worth the complexity yet.
type IMAPSource struct {
	cfg IMAPConfig
}

// NewIMAPSource returns an IMAPSource for cfg.
func NewIMAPSource(cfg IMAPConfig) IMAPSource {
	return IMAPSource{cfg: cfg}
}

// Name returns the configured host, verbatim.
func (s IMAPSource) Name() string {
	return s.cfg.Host
}

// imapDial is UnreadCount's own dial step, indirected the same way
// internal/ui's own mailUnreadCount/mailMboxUnreadCount wrap their
// Source constructors — swapped out in tests to connect to a local,
// plaintext fake IMAP server (imapserver, with InsecureAuth) instead
// of real TLS against real DNS, the one part of this Source a unit
// test can't exercise through the real client/server implementation
// otherwise.
var imapDial = func(cfg IMAPConfig) (*imapclient.Client, error) {
	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.port()))
	options := &imapclient.Options{Dialer: &net.Dialer{Timeout: imapDialTimeout}}
	if cfg.StartTLS {
		return imapclient.DialStartTLS(addr, options)
	}
	return imapclient.DialTLS(addr, options)
}

// UnreadCount dials the server (see imapDial), logs in (password from
// ~/.netrc, keyed by Host — see lookupNetrc), and runs STATUS
// (mailbox()) (UNSEEN), the lightest-weight way to ask "how many
// unread" without SELECTing the mailbox (which would also change the
// server's own idea of which mailbox this connection has open,
// irrelevant busywork for a one-shot count). Every exit path tears the
// connection down again before returning — there is deliberately
// nothing left running in the background from a single call.
func (s IMAPSource) UnreadCount() (int, error) {
	creds, err := lookupNetrc(s.cfg.Host)
	if err != nil {
		return 0, fmt.Errorf("imap %s: %w", s.cfg.Host, err)
	}

	client, err := imapDial(s.cfg)
	if err != nil {
		return 0, fmt.Errorf("imap %s: dial: %w", s.cfg.Host, err)
	}
	defer func() { _ = client.Close() }() // tearing down regardless; nothing left to do if this itself fails

	if err := client.Login(creds.Login, creds.Password).Wait(); err != nil {
		return 0, fmt.Errorf("imap %s: login: %w", s.cfg.Host, err)
	}
	// Logout is a courtesy, not a correctness requirement — the
	// deferred Close above tears the connection down regardless of
	// whether the server ever acknowledges it, so a failed Logout
	// here is deliberately not itself an error.
	defer client.Logout()

	data, err := client.Status(s.cfg.mailbox(), &imap.StatusOptions{NumUnseen: true}).Wait()
	if err != nil {
		return 0, fmt.Errorf("imap %s: status %s: %w", s.cfg.Host, s.cfg.mailbox(), err)
	}
	if data.NumUnseen == nil {
		return 0, nil
	}
	return int(*data.NumUnseen), nil
}
