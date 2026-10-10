package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Stash is one entry on the repository's own stash stack — Ref is
// git's own reflog-style selector ("stash@{0}", "stash@{1}", ...),
// always usable as-is against `git stash show`/`git stash apply`/`git
// stash drop` without this package reconstructing it from an index.
type Stash struct {
	Ref     string
	Message string
}

// StashList lists every entry on the stash stack, newest first (git's
// own default order) — %gd%x00%s mirrors Log's own reasoning: a stash
// message could contain almost anything except a literal NUL byte, and
// each entry is still its own line, the same default `git log`-family
// --format behavior for a format string with no embedded %n that
// for-each-ref's and Log's own line-per-record output already rely on.
// An empty stash stack isn't an error at all here (unlike Log on a
// repository with no commits yet) — `git stash list` simply succeeds
// with no output, so there's no equivalent special case needed.
func StashList(ctx context.Context, root string) ([]Stash, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "stash", "list", "--format=%gd%x00%s")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("git stash list: %s", msg)
		}
		return nil, fmt.Errorf("git stash list: %w", err)
	}

	var stashes []Stash
	for _, line := range strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\x00", 2)
		if len(fields) != 2 {
			continue
		}
		stashes = append(stashes, Stash{Ref: fields[0], Message: fields[1]})
	}
	return stashes, nil
}

// StashApply runs `git stash apply <ref>` — deliberately apply, never
// `pop`: pop is apply-then-drop, but on a conflicting apply it *keeps*
// the stash anyway, leaving the working tree conflicted with no way for
// this dashboard to resolve it (Git breach has no conflict-resolution
// UI at all yet — see CommitDiff's own sibling, the Files box's own
// Conflicts row, which still just says resolving conflicts isn't
// supported here). Apply alone keeps the stash either way, so a
// conflict is always recoverable by hand outside this dashboard rather
// than something this one call could silently strand the user in.
// Dropping the ref afterward, if desired, is the caller's own separate,
// explicit StashDrop call — never implicit here.
func StashApply(ctx context.Context, root, ref string) error {
	return run(ctx, root, "stash", "apply", ref)
}

// StashDrop runs `git stash drop <ref>` — the one truly irreversible
// Stash action (unlike Apply, which always leaves the stash itself
// intact): once dropped, the stashed changes are gone for good, no
// different in kind from `git branch -d`'s own "this permanently
// removes something" shape DeleteBranch already documents, just with
// no equivalent "unmerged" safety net git itself can refuse on here —
// a stash is either dropped or it isn't. The caller's own job to
// confirm first (see internal/ui's own openGitBreachStashDrop), always,
// not conditionally — unlike checkout's own "only ask if there's
// something to lose" restraint, dropping a stash is itself the
// irreversible action, not something that's only risky in some states.
func StashDrop(ctx context.Context, root, ref string) error {
	return run(ctx, root, "stash", "drop", ref)
}

// StashDiff returns ref's own diff against the commit it was stashed
// from — `git stash show -p`, the same shape CommitDiff's own `git
// show --format=` output already has (gitBreachColorizeDiff's own
// per-file "diff --git" header tracking applies unchanged to a stash
// spanning several files, exactly as it already does for a commit).
func StashDiff(ctx context.Context, root, ref string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "stash", "show", "-p", ref)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("git stash show: %s", msg)
		}
		return "", fmt.Errorf("git stash show: %w", err)
	}
	return stdout.String(), nil
}
