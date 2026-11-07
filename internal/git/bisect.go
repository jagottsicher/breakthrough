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

// BisectInProgress reports whether root has a bisect currently
// running — a plain file stat, the same "read straight off the
// filesystem, no git subprocess" shape UntrackedContent already uses:
// git itself creates .git/BISECT_START the moment `git bisect start`
// succeeds and removes it on `git bisect reset`, so its mere
// existence is git's own, authoritative answer. Checked fresh every
// reloadGitBreach rather than tracked in gitBreachState: a bisect
// persists across process restarts (a user can start one, quit
// breakthrough, and come back), so any boolean this package's own
// caller kept itself would go stale the moment that happened.
func BisectInProgress(root string) bool {
	_, err := os.Stat(filepath.Join(root, ".git", "BISECT_START"))
	return err == nil
}

// BisectStart runs `git bisect start bad good` and returns git's own
// stdout — "Bisecting: N revisions left to test after this" plus the
// first commit it checked out, exactly the text a user running this
// by hand would see. That text is the feature: surfaced verbatim by
// the caller (see internal/ui's own openGitBreachBisectStart) rather
// than parsed here for some derived status.
func BisectStart(ctx context.Context, root, bad, good string) (string, error) {
	return runBisect(ctx, root, "start", bad, good)
}

// BisectMark runs `git bisect <verdict>` — verdict is "good", "bad",
// or "skip", git's own three answers to "does this revision have the
// problem" — and returns git's own stdout: either the next revision
// to test, or "<sha> is the first bad commit" (plus its own diffstat)
// once the search concludes.
func BisectMark(ctx context.Context, root, verdict string) (string, error) {
	return runBisect(ctx, root, verdict)
}

// BisectReset runs `git bisect reset` — ends the bisect, returning
// HEAD to whatever branch it was on before BisectStart, and removes
// .git/BISECT_START (see BisectInProgress's own doc comment on why
// that file's existence is this package's whole "is one running"
// check).
func BisectReset(ctx context.Context, root string) (string, error) {
	return runBisect(ctx, root, "reset")
}

// runBisect is every `git bisect <subcommand>` call's own shared
// shape — captures stdout (unlike run(), which discards it: every
// bisect subcommand's own point is the text it prints) and reports
// stderr on failure the same way run() does for everything else in
// this package.
func runBisect(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root, "bisect"}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("git bisect: %s", msg)
		}
		return "", fmt.Errorf("git bisect: %w", err)
	}
	return stdout.String(), nil
}
