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

// StashApply runs `git stash apply <ref>` — deliberately never drops
// the stash either way, success or conflict, unlike StashPop's own
// apply-then-drop. Dropping the ref afterward, if desired, is the
// caller's own separate, explicit StashDrop call — never implicit
// here. (Originally the only way to apply a stash this package
// offered at all, back when Git breach had no conflict-resolution UI
// yet and StashPop's own "keeps the stash anyway on a conflicting
// apply" behavior would have stranded the user with no way to resolve
// it from inside this dashboard — see StashPop's own doc comment now
// that one exists alongside this.)
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

// StashPop runs `git stash pop <ref>` — apply-then-drop in one git
// call, now offered alongside StashApply/StashDrop since
// internal/ui's own conflict surface (conflict.go/
// gitbreachoperation.go) exists to recover from the one gap this was
// originally withheld for (see this file's own git history): a
// conflicting pop keeps the stash entry anyway, exactly like a
// conflicting StashApply already does, but previously left the user
// with no way to resolve it from inside this dashboard at all.
//
// A conflicting pop is deliberately not reported as err, the same
// "git's own non-zero exit covers both an outright failure and an
// expected conflict" situation Merge already has — except stash pop
// has no equivalent of InProgressOperation to disambiguate with:
// unlike a merge/rebase/cherry-pick/revert, git tracks no "operation in
// progress" ref file for a conflicting stash apply at all. Comparing
// Status.Conflicts before and after instead — more of it after than
// before means this call is what caused it — correctly tells a new
// conflict apart from a pop that refused outright while unrelated
// conflicts already existed (git itself refuses a pop/apply immediately
// in that case, with its own distinct error), which simply comparing
// the conflict count's own non-zero-ness afterward would get wrong.
func StashPop(ctx context.Context, root, ref string) error {
	before, beforeErr := Fetch(ctx, root)
	err := run(ctx, root, "stash", "pop", ref)
	if err != nil && beforeErr == nil {
		if after, afterErr := Fetch(ctx, root); afterErr == nil && len(after.Conflicts) > len(before.Conflicts) {
			return nil
		}
	}
	return err
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
