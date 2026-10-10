package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// run executes a git subcommand against root, returning stderr's own
// text (trimmed) as the error on failure rather than exec.Cmd's own
// opaque exit-status error — every caller below surfaces this straight
// to the dashboard's own error display, so it needs to actually say
// what git objected to.
func run(ctx context.Context, root string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("git %s: %s", args[0], msg)
		}
		return fmt.Errorf("git %s: %w", args[0], err)
	}
	return nil
}

// Stage runs `git add --` against paths (relative to root, exactly as
// Status's own FileChange.Path already reports them) — a no-op, not an
// error, for an empty paths.
func Stage(ctx context.Context, root string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	return run(ctx, root, append([]string{"add", "--"}, paths...)...)
}

// Unstage runs `git reset -q HEAD --` against paths — the portable
// spelling: `git restore --staged` reads better but only exists from
// git 2.23 (Aug 2019) onward, and this project makes no documented
// minimum-git-version promise to rely on that. -q suppresses the
// "Unstaged changes after reset" hint `git reset` prints to stdout by
// default, which would otherwise look like a file list in whatever
// surfaces this command's own output.
func Unstage(ctx context.Context, root string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	return run(ctx, root, append([]string{"reset", "-q", "HEAD", "--"}, paths...)...)
}

// Commit runs `git commit -m message` against root — GIT_EDITOR=true
// guarantees this never blocks the UI goroutine's own caller waiting
// on an interactive editor, even against a repository whose
// core.editor is misconfigured to something that would otherwise hang
// waiting for input nothing here can ever provide. A commit with
// nothing staged fails with git's own "nothing to commit" message,
// surfaced as-is rather than swallowed — the dashboard's own caller is
// expected to show it, not treat an empty commit as having silently
// succeeded.
func Commit(ctx context.Context, root, message string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "commit", "-m", message)
	cmd.Env = append(os.Environ(), "GIT_EDITOR=true")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("git commit: %s", msg)
		}
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

// Diff returns path's own diff text — against the index (staged ==
// true, `git diff --cached`) or against the worktree (staged == false,
// plain `git diff`). Either call returns "" for an untracked path:
// neither form of `git diff` has anything to compare it against, since
// it isn't in the index at all yet — see UntrackedContent for what the
// dashboard actually shows in that case instead.
func Diff(ctx context.Context, root, path string, staged bool) (string, error) {
	args := []string{"diff"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("git diff: %s", msg)
		}
		return "", fmt.Errorf("git diff: %w", err)
	}
	return stdout.String(), nil
}

// UntrackedContent reads path's own raw file content directly off disk
// (root-relative, same as every other path this package deals in) — an
// untracked file was never added to the index, so there is no git
// object to diff it against at all; showing its own content is this
// dashboard's own stand-in for "diff" in that one case, the same
// "nothing staged to compare against yet" situation a brand new file
// in any other git UI is in.
func UntrackedContent(root, path string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return "", err
	}
	return string(data), nil
}
