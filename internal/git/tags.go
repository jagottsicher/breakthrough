package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Tag is one local tag — Target is the commit it ultimately points
// at: for a lightweight tag that's its own object name directly, for
// an annotated tag (a real object of its own, with a message and its
// own creation date) it's the commit the tag object itself points to,
// not the tag object's own hash. Subject is empty for a lightweight
// tag, which carries no message of its own at all.
type Tag struct {
	Name      string
	Target    string
	Annotated bool
	Subject   string
	When      time.Time
}

// Tags lists every local tag (refs/tags/ only), in `for-each-ref`'s
// own default order for this refspec (alphabetical by name). Needs
// two different fields for the commit a tag points to, not just one
// the way Branches does for its own refs: %(objectname) is the tag's
// own object for an annotated tag (its message/date/tagger, a real
// git object of its own) but the commit directly for a lightweight
// one, while %(*objectname) dereferences an annotated tag down to
// that same commit and is simply empty for a lightweight tag — Target
// picks whichever of the two is actually the commit.
func Tags(ctx context.Context, root string) ([]Tag, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "for-each-ref",
		"--format=%(refname:short)%00%(objecttype)%00%(objectname)%00%(*objectname)%00%(creatordate:iso-strict)%00%(contents:subject)",
		"refs/tags/")
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

	var tags []Tag
	for line := range strings.SplitSeq(strings.TrimRight(stdout.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\x00", 6)
		if len(fields) != 6 {
			continue
		}
		target := fields[2]
		if fields[3] != "" {
			target = fields[3]
		}
		var when time.Time
		if fields[4] != "" {
			when, _ = time.Parse(time.RFC3339, fields[4])
		}
		annotated := fields[1] == "tag"
		// %(contents:subject) reads whatever object the ref directly
		// points to — the tag object's own message for an annotated
		// tag, but the *commit's* own subject line for a lightweight
		// one (it points straight at the commit, nothing else to
		// read) — not actually empty the way a naive reading of this
		// field's name might suggest. Zeroed here rather than passed
		// through, so Subject keeps one consistent meaning regardless
		// of tag kind: the tag's own message, never the commit's.
		subject := fields[5]
		if !annotated {
			subject = ""
		}
		tags = append(tags, Tag{
			Name: fields[0], Target: target, Annotated: annotated,
			Subject: subject, When: when,
		})
	}
	return tags, nil
}

// CreateTag makes a new lightweight tag named name at target (a
// commit hash, branch name, or "HEAD") — never annotated: an
// annotated tag's own message is its whole point, and typing one
// blind through a single-line prompt would produce a worse tag than
// not asking for a message at all. Fails outright, the same as `git
// tag` itself, if name already exists — never silently moves an
// existing tag to a new target.
func CreateTag(ctx context.Context, root, name, target string) error {
	return run(ctx, root, "tag", name, target)
}

// DeleteTag runs `git tag -d <name>` — unlike DeleteBranch, there is
// no "unmerged" concept for a tag to safely refuse on; the caller's
// own job to confirm first, the same as every other irreversible
// action in this app.
func DeleteTag(ctx context.Context, root, name string) error {
	return run(ctx, root, "tag", "-d", name)
}
