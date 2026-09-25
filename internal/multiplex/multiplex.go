// Package multiplex reads and manages local GNU screen, tmux, and
// Zellij sessions — real external tools, read and driven exactly the
// way a user would from a real shell, never reimplemented, the same
// "shell out to the real program" approach this project's own
// internal/firewall already takes for ufw/iptables/nft and
// internal/rsync takes for rsync(1).
//
// Named multiplex, not session: internal/session already owns this
// project's own unrelated "application session state" (StateDir, tab
// persistence) — a package about terminal multiplexer sessions needs
// its own, unambiguous name rather than colliding with that one in
// spirit even without colliding in the import path.
//
// mosh is deliberately not a fourth backend here: unlike screen/tmux/
// Zellij, a mosh-server instance has no listing command and no way to
// be reattached to at all once the client that started it is gone —
// reconnection needs the one-time secret key mosh-server prints to its
// own stdout at startup (see `man mosh-server`), which is never
// recoverable afterward. There is nothing this package could list or
// attach to that would actually work.
package multiplex

// Backend identifies which terminal multiplexer a Session came from.
type Backend string

const (
	BackendScreen Backend = "screen"
	BackendTmux   Backend = "tmux"
	BackendZellij Backend = "zellij"
)

func (b Backend) String() string { return string(b) }

// Status is whether a Session is currently attached to by a real
// client — Unknown, not just true/false, because not every backend can
// actually report this: Zellij's own `list-sessions` (verified directly
// against a real zellij 0.45.1, not assumed) never distinguishes an
// attached session from a detached one at all, unlike screen/tmux,
// which both report it plainly. A caller has to be able to tell "this
// backend doesn't know" apart from "this backend knows, and the answer
// is no" — collapsing the two into a plain bool would silently invent a
// detached status Zellij itself never actually claimed.
type Status int

const (
	StatusUnknown Status = iota
	StatusAttached
	StatusDetached
)

// Session is one screen, tmux, or Zellij session, normalized across all
// three backends so internal/ui's own Sessions screen never needs
// backend-specific logic beyond building the right command line (see
// AttachCommand/CloseCommand).
type Session struct {
	// Name is the exact identifier the owning backend itself uses to
	// address this session again — screen's own "<pid>.<name>" socket
	// name verbatim from `screen -ls`, or tmux's/Zellij's own session
	// name from `tmux list-sessions`/`zellij list-sessions`. Never
	// reformatted: AttachCommand/CloseCommand feed it straight back to
	// the real backend command, so it has to stay byte-for-byte what
	// that backend itself reported.
	Name    string
	Backend Backend
	Status  Status
}
