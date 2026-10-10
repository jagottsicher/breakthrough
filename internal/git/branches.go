package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Branch is one local branch — Current is whichever one HEAD currently
// points to, not necessarily set at all (a detached HEAD matches no
// local branch — see Status.Detached/Status.Branch, already fetched by
// the same Fetch call that produced the Files box's own data, for
// telling that case apart from "nothing is current" rather than
// needing a second, redundant check here).
type Branch struct {
	Name    string
	Current bool
}

// Branches lists every local branch (refs/heads/ only — no remote-
// tracking branches, no tags; those are a later Ausbaustufe's own
// Remotes/Tags tabs, not this first step's job).
//
// `git for-each-ref`, not `git branch --list`: --list's own output is
// meant for a human terminal (locale-sensitive, decorated with "* "/
// "+ " for the current/other-worktree-checked-out branch, a
// parenthetical for detached HEAD mixed into the same stream) — the
// exact same "don't parse a human-facing format" reasoning internal/
// gitstatus and this package's own Fetch (porcelain -z) already apply,
// now to branches instead of file status. %00 between the two fields
// keeps parsing simple without needing to worry about a branch name
// containing a literal space.
func Branches(ctx context.Context, root string) ([]Branch, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "for-each-ref",
		"--format=%(refname:short)%00%(HEAD)", "refs/heads/")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("git for-each-ref: %s", msg)
		}
		return nil, fmt.Errorf("git for-each-ref: %w", err)
	}

	var branches []Branch
	for _, line := range strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\x00", 2)
		if len(fields) != 2 {
			continue
		}
		branches = append(branches, Branch{Name: fields[0], Current: fields[1] == "*"})
	}
	return branches, nil
}

// Checkout runs `git checkout <branch>` — the caller's own job to
// decide whether a dirty working tree needs confirming first (see
// internal/ui's own openGitBreachCheckout): git itself already refuses
// outright when the checkout would actually overwrite a modified
// file, but it stays silent (and succeeds) when the target branch's
// own version of every modified file happens not to conflict, quietly
// carrying uncommitted changes onto a different branch than whoever
// made them was looking at — exactly the surprise a confirmation
// exists to prevent.
func Checkout(ctx context.Context, root, branch string) error {
	return run(ctx, root, "checkout", branch)
}

// DeleteBranch runs `git branch -d <branch>` — the safe form, never
// `-D`: git itself already refuses outright (a real, clean error, not a
// silent no-op) whenever branch has commits not reachable from any
// other branch, which is exactly the "would actually lose work" case a
// confirmation elsewhere in this app would otherwise exist to prevent.
// Force-deleting past that refusal is deliberately not offered here —
// its own explicit second confirmation, if ever added, is its own
// later Ausbaustufe, not a flag silently bolted onto this one. The
// caller's own job to refuse outright on the current branch before
// ever calling this at all (see internal/ui's own
// openGitBreachDeleteBranch): git's own error for that case ("cannot
// delete branch ... used by worktree") is clear enough on its own, but
// matching openGitBreachCheckout's own established "a no-op, not an
// error, for the already-current branch" convention is the more
// consistent user experience here.
func DeleteBranch(ctx context.Context, root, branch string) error {
	return run(ctx, root, "branch", "-d", branch)
}
