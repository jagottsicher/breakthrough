package remotefs

import "fmt"

// defaultSFTPPort is the standard SSH/SFTP port, used whenever a
// Connection's own Port is left at its zero value — the overwhelmingly
// common case, so callers building a Connection from a connection
// dialog's own "Port" field (blank unless the user typed something
// else) don't each need to repeat this fallback themselves.
const defaultSFTPPort = 22

// AuthMethod records *how* a Connection last actually authenticated —
// never itself a secret (see Connection's own doc comment): just which
// of the two broad kinds of method Dial ended up using. See Dial's own
// doc comment (sftp.go) for how it derives this from authMethods'
// own passwordUsed out-parameter.
type AuthMethod string

const (
	// AuthMethodUnknown is a Connection's own zero value: never
	// actually dialed yet (e.g. a freshly typed "Connect" form), or a
	// history entry persisted before this field existed.
	AuthMethodUnknown AuthMethod = ""
	// AuthMethodKeyOrAgent means an SSH agent or a private key file
	// authenticated the connection — reusable non-interactively, since
	// neither one ever needs a human present to type anything.
	AuthMethodKeyOrAgent AuthMethod = "key-or-agent"
	// AuthMethodPassword means an interactively typed password
	// authenticated the connection — see rsync.go's own
	// refuseBackgroundPasswordAuth for why that makes it unsafe to
	// silently reuse for a backgrounded transfer.
	AuthMethodPassword AuthMethod = "password"
)

// Connection identifies a remote endpoint to connect to — everything
// needed to dial it again, but deliberately nothing secret: no
// password, no passphrase, no private key material. That split is
// what makes a Connection safe to persist to disk as connection
// history (see history.go) — only ever "where", never the credentials
// themselves. AuthMethod is the one exception worth persisting despite
// being about "how": it's metadata about the method, never the secret
// itself, and knowing it up front is what lets a future reconnect (or
// internal/ui's own Rsync dialog) warn about a password-only endpoint
// before ever dialing it again.
type Connection struct {
	Host string
	Port int // 0 means defaultSFTPPort
	User string

	AuthMethod AuthMethod
}

// Port22IfZero returns c.Port, or defaultSFTPPort if it's unset.
func (c Connection) Port22IfZero() int {
	if c.Port == 0 {
		return defaultSFTPPort
	}
	return c.Port
}

// Addr returns c's own "host:port" dial target.
func (c Connection) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port22IfZero())
}

// Label renders c the way a shell prompt or `ssh` invocation would —
// "user@host", plus a ":port" suffix only when it isn't the default,
// so the overwhelmingly common case stays exactly as short as typing
// it at a shell would be. This is what the connection dropdown and the
// panel's own header button both show (see internal/ui's
// connectionmenu.go/remoteheader.go) — one shared rendering, so the
// two can never drift apart from each other.
func (c Connection) Label() string {
	label := c.Host
	if c.User != "" {
		label = c.User + "@" + c.Host
	}
	if c.Port != 0 && c.Port != defaultSFTPPort {
		label = fmt.Sprintf("%s:%d", label, c.Port)
	}
	return label
}

// Equal reports whether c and other identify the same endpoint —
// Port22IfZero'd on both sides first, so "no port typed" and an
// explicit "22" compare equal, the same connection either way.
func (c Connection) Equal(other Connection) bool {
	return c.Host == other.Host && c.User == other.User && c.Port22IfZero() == other.Port22IfZero()
}
