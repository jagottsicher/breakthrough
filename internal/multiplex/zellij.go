package multiplex

import (
	"errors"
	"os/exec"
	"strings"
)

// runZellijList is a package-level swappable var — see runScreenList's
// own doc comment for the identical "swallow a plain exit-status
// failure, pass a real one through" reasoning: zellij also exits
// non-zero when it has nothing to report.
//
// "-n -s" (--no-formatting --short) asks for exactly one session name
// per line, nothing else — verified directly against a real zellij
// 0.45.1 binary, not assumed: the default listing includes ANSI color
// codes and a human-readable "[Created 3m ago]" suffix per line, and
// "-s" alone still keeps the ANSI codes unless "-n" is also given.
var runZellijList = func() (string, error) {
	out, err := exec.Command("zellij", "list-sessions", "-n", "-s").Output()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return "", err
	}
	return string(out), nil
}

// parseZellijList turns runZellijList's own transcript into Sessions —
// one plain session name per line, "No active zellij sessions
// found."/any other unparsable line simply produces nothing rather than
// an error. Status is always StatusUnknown: `zellij list-sessions`
// never reports whether a session is currently attached at all
// (verified directly, not assumed — see Status's own doc comment),
// unlike screen/tmux, which both do.
func parseZellijList(text string) []Session {
	var sessions []Session
	for _, line := range strings.Split(text, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || strings.Contains(name, " ") {
			// A real session name is never empty and (per Zellij's own
			// naming rules) never contains a space — "No active zellij
			// sessions found." and any other stray informational line
			// both fail this the same simple way, without needing to
			// match their exact wording.
			continue
		}
		sessions = append(sessions, Session{Name: name, Backend: BackendZellij, Status: StatusUnknown})
	}
	return sessions
}

func listZellijSessions() ([]Session, error) {
	out, err := runZellijList()
	if err != nil {
		return nil, err
	}
	return parseZellijList(out), nil
}

// zellijAttachCommand builds the real `zellij attach <name>` argv —
// no force-takeover flag needed, unlike screen's "-D"/tmux's "-d": a
// Zellij session natively supports more than one simultaneously
// attached client (verified directly — attaching a second client next
// to an already-attached one just works), so there is nothing to force
// or take over in the first place.
func zellijAttachCommand(s Session) []string {
	return []string{"zellij", "attach", s.Name}
}

// zellijCloseCommand builds the real command that ends s outright —
// Zellij's own "kill-session" (ends a running session), not
// "delete-session" (its own separate verb for clearing an already-
// exited session's still-resurrectable record, a different operation
// entirely).
func zellijCloseCommand(s Session) []string {
	return []string{"zellij", "kill-session", s.Name}
}
