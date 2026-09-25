package multiplex

import "fmt"

// ListSessions reads every current screen and tmux session — both
// backends at once, never "prefer one": unlike internal/firewall's own
// single active backend (UFW/nftables/iptables really are three
// different views of the same underlying rules), a screen session and a
// tmux session are two entirely independent, simultaneously usable
// things, so both always show together.
//
// Only refuses outright if *neither* binary is even installed — see
// ScreenInstalled/TmuxInstalled — the one case feature_ideas.txt calls
// out as a real, reportable error rather than "zero sessions": a
// missing binary is a different situation from a present one that
// simply has nothing running right now.
func ListSessions() ([]Session, error) {
	screenOK := ScreenInstalled()
	tmuxOK := TmuxInstalled()
	if !screenOK && !tmuxOK {
		return nil, fmt.Errorf("multiplex: neither screen nor tmux is installed")
	}

	var sessions []Session
	if screenOK {
		s, err := listScreenSessions()
		if err != nil {
			return nil, fmt.Errorf("screen -ls: %w", err)
		}
		sessions = append(sessions, s...)
	}
	if tmuxOK {
		s, err := listTmuxSessions()
		if err != nil {
			return nil, fmt.Errorf("tmux list-sessions: %w", err)
		}
		sessions = append(sessions, s...)
	}
	return sessions, nil
}

// AttachCommand returns the exact argv that attaches to (and, if
// necessary, takes over) s — see screenAttachCommand/tmuxAttachCommand
// for each backend's own semantics. internal/ui's own Sessions screen
// runs this directly (no shell involved: a session's own name is
// whatever the real backend already reported, never something a shell
// would need to interpret), the same way firewall.AddRuleCommand hands
// internal/ui an exact command instead of that screen building one
// itself.
func AttachCommand(s Session) []string {
	switch s.Backend {
	case BackendScreen:
		return screenAttachCommand(s)
	case BackendTmux:
		return tmuxAttachCommand(s)
	default:
		return nil
	}
}

// CloseCommand returns the exact argv that ends s outright.
func CloseCommand(s Session) []string {
	switch s.Backend {
	case BackendScreen:
		return screenCloseCommand(s)
	case BackendTmux:
		return tmuxCloseCommand(s)
	default:
		return nil
	}
}
