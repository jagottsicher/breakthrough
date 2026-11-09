// Package github wraps the optional `gh` CLI for Git breach's own PR
// status icon — see feature_ideas.txt's own item 20: “nur falls `gh`
// bereits installiert ist, sonst schlicht kein Icon”, the same
// "external tool used optionally, never a hard dependency" pattern
// rsync/zstd already follow elsewhere in this codebase. Deliberately
// its own package, not internal/git: this talks to GitHub's own API
// through `gh`, not to the local git repository itself.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// State is a pull request's own state, collapsed from gh's "state"
// (OPEN/MERGED/CLOSED) plus "isDraft" into the four states Git
// breach's own icon actually distinguishes.
type State int

const (
	StateOpen State = iota
	StateDraft
	StateMerged
	StateClosed
)

// PullRequest is one branch's own pull request — Number/URL come
// straight from `gh`, State is this package's own collapsed form.
type PullRequest struct {
	Number int
	URL    string
	State  State
}

// Available reports whether `gh` is on $PATH at all — callers must
// check this before calling PullRequests/OpenPullRequest and treat a
// false result as "no icon, no error", never a failure: a missing
// optional tool is not this feature's problem to report.
func Available() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// cmdEnvironWithoutPrompts mirrors internal/git's own helper of the
// same name — GH_PROMPT_DISABLED stops `gh` from ever trying an
// interactive prompt (an unauthenticated machine, or one lacking a
// scope `gh pr list` needs) from blocking this call.
func cmdEnvironWithoutPrompts() []string {
	return append(os.Environ(), "GH_PROMPT_DISABLED=1")
}

type rawPullRequest struct {
	HeadRefName string `json:"headRefName"`
	Number      int    `json:"number"`
	URL         string `json:"url"`
	State       string `json:"state"`
	IsDraft     bool   `json:"isDraft"`
}

// PullRequests fetches every pull request this repository's own
// GitHub remote has, in one single `gh pr list` call — not one call
// per branch: Git breach's own Branches box can list dozens of local
// branches, so a per-branch round trip would turn one dashboard
// reload into dozens of sequential network calls. --state all
// because the default is open-only, and a merged/closed PR's own
// icon matters here too; --limit 100 caps how far back that reaches,
// trading a stale icon on an ancient branch for reload latency.
// Keyed on headRefName since that's the only thing Git breach's own
// Branch already has to look one up by.
func PullRequests(ctx context.Context, root string) (map[string]PullRequest, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "list", "--state", "all", "--limit", "100",
		"--json", "number,state,isDraft,url,headRefName")
	cmd.Dir = root
	cmd.Env = cmdEnvironWithoutPrompts()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("gh pr list: %s", msg)
		}
		return nil, fmt.Errorf("gh pr list: %w", err)
	}

	return parsePullRequests(stdout.Bytes())
}

// parsePullRequests is PullRequests' own parsing half, pulled out so
// a test can exercise it against captured JSON instead of shelling
// out to the real `gh` — see github_test.go's own tests, which are
// this function's only real coverage (the network call above has
// none, deliberately: nothing to assert against other than "didn't
// crash" without a live GitHub remote).
func parsePullRequests(data []byte) (map[string]PullRequest, error) {
	var raw []rawPullRequest
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("gh pr list: parsing its own JSON output: %w", err)
	}

	prs := make(map[string]PullRequest, len(raw))
	for _, r := range raw {
		pr := PullRequest{Number: r.Number, URL: r.URL}
		switch {
		case r.State == "MERGED":
			pr.State = StateMerged
		case r.State == "CLOSED":
			pr.State = StateClosed
		case r.IsDraft:
			pr.State = StateDraft
		default:
			pr.State = StateOpen
		}
		prs[r.HeadRefName] = pr
	}
	return prs, nil
}

// OpenPullRequest shells out to `gh pr view --web <branch>` — gh
// itself resolves the branch to its own pull request and opens the
// system browser, so this package never has to parse a remote URL
// (SSH vs. HTTPS, github.com vs. an Enterprise host) or pick a
// platform-specific browser opener itself.
func OpenPullRequest(ctx context.Context, root, branch string) error {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", "--web", branch)
	cmd.Dir = root
	cmd.Env = cmdEnvironWithoutPrompts()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("gh pr view: %s", msg)
		}
		return fmt.Errorf("gh pr view: %w", err)
	}
	return nil
}
