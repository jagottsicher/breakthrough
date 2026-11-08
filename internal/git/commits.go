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
	// Parents holds this commit's own parent hashes, in the order git
	// itself lists them (first parent first) — zero for a root commit,
	// one for an ordinary commit, two or more for a merge, including an
	// octopus merge. internal/ui's own lane-assignment algorithm walks
	// this list; nothing else in this package interprets it.
	Parents []string
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
		"--format=%H%x00%h%x00%s%x00%cI%x00%an%x00%ae%x00%P")
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
		fields := strings.SplitN(line, "\x00", 7)
		if len(fields) != 7 {
			continue
		}
		when, err := time.Parse(time.RFC3339, fields[3])
		if err != nil {
			when = time.Time{}
		}
		var parents []string
		if fields[6] != "" {
			parents = strings.Split(fields[6], " ")
		}
		commits = append(commits, LogEntry{
			Hash: fields[0], Short: fields[1], Subject: fields[2], When: when,
			AuthorName: fields[4], AuthorEmail: fields[5], Parents: parents,
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

// DiffRefs diffs two arbitrary refs against each other directly — any
// commit hash, branch, or tag name git itself can resolve — rather
// than CommitDiff's own "this commit against its one parent" shape.
// `git diff a b` (not `a..b`, though both mean the same thing to
// `diff` specifically — unlike `log`, where they'd mean something
// completely different): a plain two-tree comparison, oldest-named
// argument first, exactly as the caller named them. Swapping the two
// arguments at the call site is the whole of a reverse diff; no
// separate flag or function for it.
func DiffRefs(ctx context.Context, root, a, b string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "diff", a, b)
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

// CherryPick runs `git cherry-pick ref` onto whatever HEAD currently
// points at — the single-commit, onto-HEAD case feature_ideas.txt's
// own Phase C item asks for first ("Commit(s) markieren, in einen
// anderen Branch einfügen" is noted as a later remainder: picking onto
// a branch other than the current one needs its own checkout step
// first, and multi-select doesn't exist on any table in this dashboard
// yet). ref is anything git itself can resolve to a single commit — a
// hash, branch name, or tag name all work identically.
//
// A conflicting pick is deliberately not reported as err — but unlike
// Merge, checking InProgressOperation(root) == OperationCherryPick
// alone is not enough to tell a real conflict apart from every other
// failure here: a real, live-caught bug picking an already-applied
// ref (git's own cherry-pick sequencer calls this "empty", not a
// conflict at all) still leaves CHERRY_PICK_HEAD behind with zero
// actual conflicted paths, which InProgressOperation alone can't
// distinguish from a genuine conflict waiting to be resolved. Instead,
// comparing Status.Conflicts before and after — the same technique
// StashPop's own doc comment already uses, for the identical reason
// (stash apply/pop has no ref-file state to check at all): more
// conflicts afterward means this pick caused them, unchanged means a
// real failure (empty pick, an unknown ref, a dirty tree it refuses).
func CherryPick(ctx context.Context, root, ref string) error {
	before, beforeErr := Fetch(ctx, root)
	err := run(ctx, root, "cherry-pick", ref)
	if err != nil && beforeErr == nil {
		if after, afterErr := Fetch(ctx, root); afterErr == nil && len(after.Conflicts) > len(before.Conflicts) {
			return nil
		}
	}
	return err
}
