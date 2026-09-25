package multiplex

import (
	"errors"
	"os/exec"
	"regexp"
	"strings"
)

// runScreenList is a package-level swappable var so a test can feed
// parseScreenList a fixed, real-looking `screen -ls` transcript instead
// of ever needing a real screen binary or real sessions.
//
// screen itself exits non-zero both when it genuinely has nothing to
// report (no sessions at all — "No Sockets found...") and, presumably,
// for a real failure; the exit status alone can't tell those apart, and
// stdout is what actually says which happened either way (see
// parseScreenList), so a plain *exec.ExitError here is swallowed
// rather than treated as a hard failure — only a "the binary itself
// couldn't even run" error (already ruled out by ScreenInstalled, but
// possible if a stale $PATH entry disagrees, or a real permission
// problem) is passed through.
var runScreenList = func() (string, error) {
	out, err := exec.Command("screen", "-ls").Output()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return "", err
	}
	return string(out), nil
}

// screenSessionLine matches one session's own line from `screen -ls`,
// e.g. "\t12345.name\t(Detached)" or, with screen's own optional
// per-session timestamp in between (observed directly against a real
// screen 5.0.2 — not assumed from the manual alone), "\t12345.name\t
// (25.09.2026 14:38:56)\t(Detached)". Group 1 is the exact "<pid>.<name>"
// token AttachCommand/CloseCommand feed straight back to screen; group 2
// is every parenthesized group after it, greedily, so parseScreenList
// only ever has to look at the *last* one for the real status — an
// optional middle timestamp never has to be specifically recognized or
// skipped, just not mistaken for the status itself.
var screenSessionLine = regexp.MustCompile(`^\s*(\d+\.\S+)((?:\s*\([^)]*\))+)\s*$`)

// screenParenGroup pulls one "(...)" group's own inner text out —
// applied to screenSessionLine's own second capture group, which can
// hold more than one such group back to back.
var screenParenGroup = regexp.MustCompile(`\(([^)]*)\)`)

// parseScreenList turns `screen -ls`'s own transcript into Sessions,
// tolerant of a header line ("There is/are a/screen(s) on:"), a footer
// line ("N Socket(s) in ..."), and the no-sessions-at-all case ("No
// Sockets found...") — all three read as zero sessions, never an error:
// see runScreenList's own doc comment for why a real failure never
// reaches here with output at all.
func parseScreenList(text string) []Session {
	var sessions []Session
	for _, line := range strings.Split(text, "\n") {
		m := screenSessionLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		groups := screenParenGroup.FindAllStringSubmatch(m[2], -1)
		if len(groups) == 0 {
			continue
		}
		status := groups[len(groups)-1][1]
		sessions = append(sessions, Session{
			Name:     m[1],
			Backend:  BackendScreen,
			Attached: strings.EqualFold(status, "Attached"),
		})
	}
	return sessions
}

func listScreenSessions() ([]Session, error) {
	out, err := runScreenList()
	if err != nil {
		return nil, err
	}
	return parseScreenList(out), nil
}

// screenAttachCommand builds the real `screen -D -r <name>` argv that
// attaches to s — "-D -r" (spelled out, not the "-Dr" some invocations
// merge into one flag, both work identically against a real screen
// binary but the spelled-out form is unambiguous in an argv slice), the
// same "detach it wherever it's already attached, then reattach here"
// semantics `feature_ideas.txt` calls for, covering both an already-
// detached session (plain reattach) and one still attached somewhere
// else (screen's own "-D" forces that other client off first) in a
// single command.
func screenAttachCommand(s Session) []string {
	return []string{"screen", "-D", "-r", s.Name}
}

// screenCloseCommand builds the real command that ends s outright —
// screen's own remote-control "quit" verb against the exact session
// named, never a signal sent to some guessed PID.
func screenCloseCommand(s Session) []string {
	return []string{"screen", "-S", s.Name, "-X", "quit"}
}
