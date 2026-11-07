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

// BlameLine is one line of a file's own blame annotation — which
// commit last touched it, who, when, and the line's own content.
// AuthorEmail mirrors LogEntry's own reasoning: the stable identity
// internal/ui's own author-coloring hashes, not the display name,
// which can change across commits for the same person.
type BlameLine struct {
	Hash        string
	Short       string
	AuthorName  string
	AuthorEmail string
	When        time.Time
	LineNo      int
	Content     string
}

// Blame annotates path (root-relative, same as every other path this
// package deals in) line by line via `git blame --line-porcelain` —
// repeats every commit's full header on every line, rather than only
// the first time it appears the way plain `--porcelain` does, trading
// some output size for a parser that never has to remember a previous
// line's own commit. An uncommitted, newly-created file blames every
// line to the "Not Committed Yet" placeholder commit git itself
// invents for it — surfaced as-is, not specially detected, since it
// already reads correctly to a user (zero hash, that exact name).
func Blame(ctx context.Context, root, path string) ([]BlameLine, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "blame", "--line-porcelain", "--", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("git blame: %s", msg)
		}
		return nil, fmt.Errorf("git blame: %w", err)
	}
	return parseBlamePorcelain(stdout.String()), nil
}

// parseBlamePorcelain walks --line-porcelain's own repeated-header
// shape: a "<sha> <orig-line> <final-line>[ <num-lines>]" line,
// several "key value" header lines in no fixed subset (boundary/
// previous/filename are skipped here, not needed by BlameLine), then
// exactly one line starting with a tab — that line's content,
// everything after the tab verbatim (so just the leading tab is
// stripped, not TrimSpace, since real code can start with a space).
func parseBlamePorcelain(output string) []BlameLine {
	var lines []BlameLine
	var cur BlameLine
	var authorTime int64
	for raw := range strings.SplitSeq(output, "\n") {
		switch {
		case strings.HasPrefix(raw, "\t"):
			cur.Content = raw[1:]
			cur.When = time.Unix(authorTime, 0)
			lines = append(lines, cur)
			cur = BlameLine{}
		case strings.HasPrefix(raw, "author "):
			cur.AuthorName = strings.TrimPrefix(raw, "author ")
		case strings.HasPrefix(raw, "author-mail "):
			cur.AuthorEmail = strings.Trim(strings.TrimPrefix(raw, "author-mail "), "<>")
		case strings.HasPrefix(raw, "author-time "):
			authorTime, _ = strconv.ParseInt(strings.TrimPrefix(raw, "author-time "), 10, 64)
		case isBlameShaLine(raw):
			fields := strings.Fields(raw)
			cur.Hash = fields[0]
			cur.Short = blameShortHash(fields[0])
			if len(fields) >= 3 {
				cur.LineNo, _ = strconv.Atoi(fields[2])
			}
		}
	}
	return lines
}

// isBlameShaLine reports whether raw is --line-porcelain's own
// "<sha> <orig-line> <final-line>[ <num-lines>]" line rather than one
// of the several "key value" header lines or the final tab-prefixed
// content line — the only one whose first field is a full 40-character
// hex object ID.
func isBlameShaLine(raw string) bool {
	fields := strings.Fields(raw)
	if len(fields) < 3 || len(fields[0]) != 40 {
		return false
	}
	for _, c := range fields[0] {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

// blameShortHash mirrors git's own default abbreviation length for
// display — Blame has no equivalent of `git log --format=%h` to ask
// git itself, since --line-porcelain only ever gives the full object
// ID.
func blameShortHash(hash string) string {
	const shortLen = 7
	if len(hash) < shortLen {
		return hash
	}
	return hash[:shortLen]
}
