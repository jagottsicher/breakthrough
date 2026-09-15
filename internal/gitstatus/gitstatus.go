package gitstatus

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Status is one directory's own git state — everything a zsh-style
// prompt segment typically shows, gathered from a single
// `git status --porcelain=v2 --branch` call rather than several
// separate ones (branch name, ahead/behind, and every file's own
// index/worktree state all come back in one invocation's output).
type Status struct {
	// Branch is the current branch name, or a short commit hash while
	// in detached-HEAD state (see Detached) — never both.
	Branch   string
	Detached bool

	// Ahead/Behind are how many commits the current branch is ahead
	// of/behind its own upstream — both 0 with no upstream configured,
	// indistinguishable from "up to date"; callers that need to tell
	// the two apart don't currently need to (there's no "no upstream"
	// indicator in the rendered text either).
	Ahead, Behind int

	// Staged/Unstaged/Untracked/Conflicts are file counts: Staged and
	// Unstaged can both be true for the same file (staged, then
	// modified again — git's own "MM" status) and are counted
	// independently here for exactly that reason, rather than one
	// "changed" total that would hide it.
	Staged, Unstaged, Untracked, Conflicts int
}

// Dirty reports whether st has anything not yet committed — staged,
// unstaged, or untracked. Conflicts are deliberately not included:
// they're their own, more urgent state (see Text's own severity
// ordering in internal/ui/gitstatus.go), not merely "dirty".
func (st Status) Dirty() bool {
	return st.Staged > 0 || st.Unstaged > 0 || st.Untracked > 0
}

// Available reports whether git itself can be found on $PATH — the
// same LookPath check this project's other external-tool integrations
// already make (see internal/search's own ZgrepAvailable/
// LocateAvailable) — so a caller can skip Fetch entirely rather than
// have it fail on every single call.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// Fetch runs `git status` against dir and reports its state. inRepo is
// false — not an error — for the overwhelmingly common case of a
// directory that simply isn't part of any git working tree; err is
// reserved for a git invocation that genuinely failed (git itself
// missing, ctx expiring, a corrupt repository, ...).
//
// dir doesn't have to be a repository's own root: git itself walks up
// looking for a .git directory the same way it does for a real `git
// status` typed in a shell, so a deeply nested subdirectory reports
// the very same repository-wide status a checkout at its root would.
//
// --ignore-submodules keeps a submodule's own dirty state from
// counting against the *containing* repository's status — a plain
// `git status` already treats an submodule with uncommitted changes as
// itself "modified" in the parent repo, which would otherwise show up
// here as a change to a path that isn't a source file at all.
func Fetch(ctx context.Context, dir string) (status Status, inRepo bool, err error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain=v2", "--branch", "--ignore-submodules")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr != nil {
		if ctx.Err() != nil {
			return Status{}, false, ctx.Err()
		}
		if strings.Contains(stderr.String(), "not a git repository") {
			return Status{}, false, nil
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return Status{}, false, fmt.Errorf("git status: %s", msg)
		}
		return Status{}, false, fmt.Errorf("git status: %w", runErr)
	}
	return parsePorcelainV2(stdout.String()), true, nil
}

// parsePorcelainV2 reads git status --porcelain=v2 --branch's own
// stable, script-friendly output — see git-status(1)'s own "Porcelain
// Format Version 2" section for the exact grammar this follows.
func parsePorcelainV2(output string) Status {
	var st Status
	var oid, head string

	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			oid = strings.TrimPrefix(line, "# branch.oid ")
		case strings.HasPrefix(line, "# branch.head "):
			head = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.ab "):
			// "+<ahead> -<behind>", only present with an upstream configured.
			for _, field := range strings.Fields(strings.TrimPrefix(line, "# branch.ab ")) {
				n, err := strconv.Atoi(strings.TrimLeft(field, "+-"))
				if err != nil {
					continue
				}
				if strings.HasPrefix(field, "+") {
					st.Ahead = n
				} else {
					st.Behind = n
				}
			}
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "):
			// Ordinary changed ("1 <XY> ...") and renamed/copied
			// ("2 <XY> ...") entries share the same leading <XY> field —
			// X is the index (staged) state, Y the worktree (unstaged)
			// one, either or both '.' meaning "unchanged there".
			fields := strings.SplitN(line, " ", 3)
			if len(fields) < 2 || len(fields[1]) != 2 {
				continue
			}
			xy := fields[1]
			if xy[0] != '.' {
				st.Staged++
			}
			if xy[1] != '.' {
				st.Unstaged++
			}
		case strings.HasPrefix(line, "u "):
			st.Conflicts++
		case strings.HasPrefix(line, "? "):
			st.Untracked++
		}
	}

	switch {
	case head == "(detached)":
		st.Detached = true
		st.Branch = shortOID(oid)
	case head != "":
		st.Branch = head
	}
	return st
}

// shortOID renders a commit hash the same 7-character length `git log
// --oneline`/`git rev-parse --short` default to — long enough to be
// unambiguous in any real repository, short enough to fit a status
// line alongside everything else there. Returns oid as-is if it's
// already no longer than that.
func shortOID(oid string) string {
	const shortLen = 7
	if len(oid) <= shortLen {
		return oid
	}
	return oid[:shortLen]
}
