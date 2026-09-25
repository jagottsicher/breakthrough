package multiplex

import (
	"errors"
	"os/exec"
	"strings"
)

// tmuxListFormat asks tmux for exactly two fields per session, tab-
// separated — a real tab byte in this Go string literal, not the two-
// character "\t" some shells would otherwise need escaping (confirmed
// directly against a real tmux 3.7c that a literal tab in the format
// string comes back verbatim, live-tested rather than assumed) —
// robust, machine-parsable output instead of tmux's own human-readable
// default listing (`created ... [80x24]`), the same "ask for a script-
// friendly format instead of parsing a pretty-printed one" preference
// this project's own internal/firewall already shows for `nft -j` over
// plain `nft list`.
const tmuxListFormat = "#{session_name}\t#{session_attached}"

// runTmuxList is a package-level swappable var — see runScreenList's
// own doc comment for the identical reasoning: tmux also exits non-zero
// for "no server running at all" (the common "zero sessions" case), so
// only a genuine "couldn't run the binary" error is passed through here
// rather than treated as a real failure.
var runTmuxList = func() (string, error) {
	out, err := exec.Command("tmux", "list-sessions", "-F", tmuxListFormat).Output()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return "", err
	}
	return string(out), nil
}

// parseTmuxList turns runTmuxList's own tab-separated transcript into
// Sessions — one line per session, "no server running..."/any other
// unparsable line simply produces nothing rather than an error, the
// same "absence isn't an error" contract parseScreenList's own no-
// sessions case already follows.
func parseTmuxList(text string) []Session {
	var sessions []Session
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 2 {
			continue
		}
		name := fields[0]
		if name == "" {
			continue
		}
		sessionStatus := StatusDetached
		if fields[1] == "1" {
			sessionStatus = StatusAttached
		}
		sessions = append(sessions, Session{
			Name:    name,
			Backend: BackendTmux,
			Status:  sessionStatus,
		})
	}
	return sessions
}

func listTmuxSessions() ([]Session, error) {
	out, err := runTmuxList()
	if err != nil {
		return nil, err
	}
	return parseTmuxList(out), nil
}

// tmuxAttachCommand builds the real `tmux attach -d -t <name>` argv
// that attaches to s — "-d" detaches every other client already
// attached to it first, tmux's own equivalent of screen's "-D" (see
// screenAttachCommand's own doc comment), so taking over a session
// already attached somewhere else works the same way regardless of
// which backend it came from.
func tmuxAttachCommand(s Session) []string {
	return []string{"tmux", "attach", "-d", "-t", s.Name}
}

// tmuxCloseCommand builds the real command that ends s outright.
func tmuxCloseCommand(s Session) []string {
	return []string{"tmux", "kill-session", "-t", s.Name}
}
