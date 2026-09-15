package remotefs

import "fmt"

// defaultSFTPPort is the standard SSH/SFTP port, used whenever a
// Connection's own Port is left at its zero value — the overwhelmingly
// common case, so callers building a Connection from a connection
// dialog's own "Port" field (blank unless the user typed something
// else) don't each need to repeat this fallback themselves.
const defaultSFTPPort = 22

// Connection identifies a remote endpoint to connect to — everything
// needed to dial it again, but deliberately nothing secret: no
// password, no passphrase, no private key material. That split is
// what makes a Connection safe to persist to disk as connection
// history (see history.go) — only ever "where", never "how
// authenticated".
type Connection struct {
	Host string
	Port int // 0 means defaultSFTPPort
	User string
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
