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
// needed anywhere a LogEntry value is used today.
type LogEntry struct {
	Hash    string
	Short   string
	Subject string
	When    time.Time
}

// CommitLogLimit bounds how many commits Log ever fetches in one call —
// an unbounded `git log` on a large, long-lived repository would block
// reloadGitBreach's own synchronous fetch for however long that takes,
// and the Commits box only ever shows a fixed-height list on screen
// anyway (see internal/ui's own gitBreachFirstDataRow-style scrolling,
// not a "load more" affordance this first Ausbaustufe doesn't have).
const CommitLogLimit = 200

// Log lists the CommitLogLimit most recent commits reachable from HEAD.
// %x00 between fields mirrors Branches' own reasoning (a commit subject
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
func Log(ctx context.Context, root string) ([]LogEntry, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "log",
		"-n", strconv.Itoa(CommitLogLimit),
		"--format=%H%x00%h%x00%s%x00%cI")
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
		fields := strings.SplitN(line, "\x00", 4)
		if len(fields) != 4 {
			continue
		}
		when, err := time.Parse(time.RFC3339, fields[3])
		if err != nil {
			when = time.Time{}
		}
		commits = append(commits, LogEntry{Hash: fields[0], Short: fields[1], Subject: fields[2], When: when})
	}
	return commits, nil
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
