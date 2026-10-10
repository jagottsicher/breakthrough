package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// LogEntry is one entry in the repository's own commit log, newest first
// (git log's own default order) — Subject is the commit message's own
// first line only, the same "short, human-facing summary" Log's own
// --format already limits itself to; the full message body isn't
// needed anywhere a LogEntry value is used today. AuthorEmail is the
// stable identity internal/ui's own author-coloring hashes (the same
// person can change their display name; their email is what actually
// stays constant across commits) — AuthorName is only ever what gets
// shown.
type LogEntry struct {
	Hash        string
	Short       string
	Subject     string
	AuthorName  string
	AuthorEmail string
	When        time.Time
}

// CommitLogLimit is the suggested starting point for limit, Log's own
// second argument — an unbounded `git log` on a large, long-lived
// repository would block reloadGitBreach's own synchronous fetch for
// however long that takes, and the Commits box only ever shows a
// fixed-height list on screen at once anyway. Not a hard ceiling
// anymore: internal/ui's own gitBreachCommitsLimit starts here and
// grows by another CommitLogLimit each time the cursor reaches the
// last loaded row with more still available (see
// maybeLoadMoreGitBreachCommits), rather than this package enforcing
// one fixed cutoff no caller can ever see past.
const CommitLogLimit = 200

// Log lists the limit most recent commits reachable from HEAD. %x00
// between fields mirrors Branches' own reasoning (a commit subject
// could contain almost anything except a literal NUL byte); each
// commit is still its own line, the same default `git log --format`
// behavior for a format string with no embedded %n that for-each-ref's
// own line-per-record output already relies on.
//
// A repository with no commits at all isn't a real error — git log's
// own exit code there is indistinguishable from an actual failure
// except by matching stderr text (locale-dependent, fragile) — so a
// failure is only ever reported as such once `git rev-parse --verify
// HEAD` confirms there's a real commit to have failed to log in the
// first place; otherwise this returns (nil, nil), the same "nothing
// to show yet, not broken" shape Branches' own empty-repo case has via
// for-each-ref's own empty-but-successful output.
func Log(ctx context.Context, root string, limit int) ([]LogEntry, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "log",
		"-n", strconv.Itoa(limit),
		"--format=%H%x00%h%x00%s%x00%cI%x00%an%x00%ae")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if verifyErr := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--verify", "-q", "HEAD").Run(); verifyErr != nil {
			return nil, nil
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("git log: %s", msg)
		}
		return nil, fmt.Errorf("git log: %w", err)
	}

	var commits []LogEntry
	for _, line := range strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\x00", 6)
		if len(fields) != 6 {
			continue
		}
		when, err := time.Parse(time.RFC3339, fields[3])
		if err != nil {
			when = time.Time{}
		}
		commits = append(commits, LogEntry{
			Hash: fields[0], Short: fields[1], Subject: fields[2], When: when,
			AuthorName: fields[4], AuthorEmail: fields[5],
		})
	}
	return commits, nil
}

// CommitCount returns the total number of commits reachable from HEAD
// — internal/ui's own Commits box header uses this against however
// many Log actually returned to show "loaded/total" only while loaded
// is still less than total, so the user knows there's more to scroll
// to rather than silently wondering why the list stops where it does.
// Same empty-repository handling as Log, for the same reason: `git
// rev-list --count` on a repository with no commits at all isn't a
// real error, just nothing to count yet.
func CommitCount(ctx context.Context, root string) (int, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "rev-list", "--count", "HEAD")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if verifyErr := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--verify", "-q", "HEAD").Run(); verifyErr != nil {
			return 0, nil
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return 0, fmt.Errorf("git rev-list: %s", msg)
		}
		return 0, fmt.Errorf("git rev-list: %w", err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(stdout.String()))
	if err != nil {
		return 0, fmt.Errorf("git rev-list: unexpected output %q", stdout.String())
	}
	return count, nil
}

// CommitDiff returns hash's own diff against its parent — `git show
// --format=` (an empty pretty-format suppresses the commit header git
// show would otherwise prepend, leaving just the diff itself, the same
// shape Diff's own `git diff` output already has) rather than `git
// diff <hash>^ <hash>`: the latter fails outright on a repository's
// root commit, which has no parent to diff against at all, while `git
// show` already knows how to render a root commit's own diff (against
// the empty tree) without needing that case handled here separately.
func CommitDiff(ctx context.Context, root, hash string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "show", "--format=", hash)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("git show: %s", msg)
		}
		return "", fmt.Errorf("git show: %w", err)
	}
	return stdout.String(), nil
}
