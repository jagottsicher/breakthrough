// Package sshshell builds the ssh(1) invocation for breakthrough's own
// "gs" SSH shell dropdown and runs it, classifying the outcome so the
// UI layer can decide how to record it — kept out of internal/ui per
// this project's own architecture rule (CLAUDE.md: "Größere Workflows
// erhalten ein eigenes Fachpaket"), the same separation internal/sshkeys,
// internal/rsync, and internal/firewall already have for their own
// external-tool features.
package sshshell

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"strconv"

	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// DefaultPort mirrors remotefs' own unexported defaultSFTPPort —
// duplicated here rather than exported from remotefs purely to avoid
// growing that package's own public surface for a single int only
// this package needs: ssh itself already defaults to 22 without "-p"
// at all, so this just has to agree with what Connection.Port22IfZero
// already treats as "nothing typed".
const DefaultPort = 22

// ConnectionFailedExitCode is the exit code ssh itself uses for "the
// connection could not be established at all" (refused, unreachable,
// auth failed, ...) — as opposed to the *remote shell's own* exit
// code, which ssh simply passes through unchanged and can be anything.
// Documented in ssh(1); used by Run to tell the two apart before
// classifying the outcome.
const ConnectionFailedExitCode = 255

// Argv builds the argv (after "ssh" itself) that Run execs — split out
// so the exact flag mapping can be pinned by a test without actually
// exec'ing ssh.
func Argv(conn remotefs.Connection) []string {
	var args []string
	if conn.Port != 0 && conn.Port != DefaultPort {
		args = append(args, "-p", strconv.Itoa(conn.Port))
	}
	if conn.ShellAgentForwarding {
		args = append(args, "-A")
	}
	if conn.ShellCompression {
		args = append(args, "-C")
	}
	if conn.ShellVerbose {
		args = append(args, "-v")
	}
	if conn.ShellX11Forwarding {
		args = append(args, "-X")
	}
	user := conn.User
	if user == "" {
		user = currentUsername()
	}
	args = append(args, user+"@"+conn.Host)
	return args
}

// currentUsername duplicates internal/ui's own unexported helper of
// the same name — unexported there, and this package must not import
// internal/ui (internal/ui imports this package, never the reverse),
// so a small, self-contained duplicate is the same deliberate
// trade-off DefaultPort's own doc comment above already makes.
func currentUsername() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// Outcome classifies what happened after Run's own ssh process exits.
type Outcome int

const (
	// Succeeded means ssh itself connected — whatever exit code the
	// *remote shell* ended with (anything at all) is none of this
	// package's business and is never treated as a failure.
	Succeeded Outcome = iota
	// ConnectionFailed means ssh itself never connected — exactly
	// ConnectionFailedExitCode, nothing else.
	ConnectionFailed
	// LocalError means ssh itself never ran at all (missing binary,
	// ...) — a local problem, not a connection attempt.
	LocalError
)

// Run execs a real `ssh` process for conn, with the current process's
// own stdin/stdout/stderr wired straight through — full interactivity,
// including a remote `sudo -i`/`su -` identity switch. Meant to be
// called from inside the caller's own terminal-suspend callback: this
// package has no opinion on how the terminal gets handed over, that
// stays the UI layer's job. Returns the classified Outcome plus, for
// LocalError only, the underlying error.
func Run(conn remotefs.Connection) (Outcome, error) {
	cmd := exec.Command("ssh", Argv(conn)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	runErr := cmd.Run()

	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
		return Succeeded, nil
	case errors.As(runErr, &exitErr):
		if exitErr.ExitCode() == ConnectionFailedExitCode {
			return ConnectionFailed, nil
		}
		return Succeeded, nil
	default:
		return LocalError, runErr
	}
}
