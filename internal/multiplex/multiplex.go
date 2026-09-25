// Package multiplex reads and manages local GNU screen and tmux
// sessions — real external tools, read and driven exactly the way a
// user would from a real shell, never reimplemented, the same "shell
// out to the real program" approach this project's own internal/firewall
// already takes for ufw/iptables/nft and internal/rsync takes for
// rsync(1).
//
// Named multiplex, not session: internal/session already owns this
// project's own unrelated "application session state" (StateDir, tab
// persistence) — a package about screen/tmux terminal multiplexer
// sessions needs its own, unambiguous name rather than colliding with
// that one in spirit even without colliding in the import path.
package multiplex

// Backend identifies which terminal multiplexer a Session came from.
type Backend string

const (
	BackendScreen Backend = "screen"
	BackendTmux   Backend = "tmux"
)

func (b Backend) String() string { return string(b) }

// Session is one screen or tmux session, normalized across both
// backends so internal/ui's own Sessions screen never needs
// backend-specific logic beyond building the right command line (see
// AttachCommand/CloseCommand).
type Session struct {
	// Name is the exact identifier the owning backend itself uses to
	// address this session again — screen's own "<pid>.<name>" socket
	// name verbatim from `screen -ls`, or tmux's own session name from
	// `tmux list-sessions`. Never reformatted: AttachCommand/
	// CloseCommand feed it straight back to the real backend command,
	// so it has to stay byte-for-byte what that backend itself reported.
	Name     string
	Backend  Backend
	Attached bool
}
