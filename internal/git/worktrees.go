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

// Worktree is one entry from `git worktree list` — Path is always the
// worktree's own absolute root (including the main one, the original
// checkout "jg" itself was opened on). Branch is empty for a detached
// worktree — see Detached, not an empty Branch, for telling that case
// apart from a real failure to parse one; a branch literally named
// nothing is not a thing git allows.
type Worktree struct {
	Path       string
	Head       string
	Branch     string
	Detached   bool
	Locked     bool
	LockReason string
}

// Worktrees lists every worktree this repository has — the main one
// first (always git's own first record), every linked one after,
// order otherwise unspecified beyond that.
//
// `git worktree list --porcelain`, not the human-facing plain-table
// form: the same "don't parse a format meant for a terminal, ask git
// for the real data instead" reasoning Branches'/Remotes' own doc
// comments already give for for-each-ref/config over `git branch
// --list`/`git remote -v`. Records are separated by a blank line;
// within one, "worktree <path>" and "HEAD <hash>" always come first,
// then exactly one of "branch <ref>"/"detached"/"bare", then zero or
// more of "locked[ <reason>]"/"prunable <reason>" — see
// git-worktree(1)'s own "PORCELAIN FORMAT" section for the full
// grammar; this parser only extracts the fields Worktree itself
// carries, silently skipping any other line gracefully rather than
// erroring on it (a future git version adding a new line kind here
// should degrade, not break this dashboard).
func Worktrees(ctx context.Context, root string) ([]Worktree, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "worktree", "list", "--porcelain")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("git worktree: %s", msg)
		}
		return nil, fmt.Errorf("git worktree: %w", err)
	}

	var worktrees []Worktree
	var cur Worktree
	flush := func() {
		if cur.Path != "" {
			worktrees = append(worktrees, cur)
		}
		cur = Worktree{}
	}
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		if line == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			cur.Detached = true
		case line == "locked":
			cur.Locked = true
		case strings.HasPrefix(line, "locked "):
			cur.Locked = true
			cur.LockReason = strings.TrimPrefix(line, "locked ")
		}
	}
	flush()
	return worktrees, nil
}

// WorktreePath proposes a sibling directory for a new worktree on
// branch — "<repo>-<branch>" next to root itself, slashes in branch
// flattened to "-" since a branch like "feature/x" would otherwise
// nest a directory no one asked for. If that path already exists, a
// numeric suffix is appended ("-2", "-3", ...) until a free one is
// found — git itself refuses to add a worktree at an existing
// non-empty path, so this check happens before ever shelling out.
func WorktreePath(root, branch string) (string, error) {
	root = filepath.Clean(root)
	parent := filepath.Dir(root)
	base := filepath.Base(root) + "-" + strings.ReplaceAll(branch, "/", "-")
	candidate := filepath.Join(parent, base)
	for i := 2; ; i++ {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
		candidate = filepath.Join(parent, fmt.Sprintf("%s-%d", base, i))
	}
}

// AddWorktree runs `git worktree add <path> <branch>`, checking out
// branch into a brand-new linked worktree at path — path's own parent
// must already exist, but path itself must not (git's own
// requirement, not one this function adds).
func AddWorktree(ctx context.Context, root, path, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "worktree", "add", path, branch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("git worktree add: %s", msg)
		}
		return fmt.Errorf("git worktree add: %w", err)
	}
	return nil
}
