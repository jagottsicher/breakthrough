package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Remote is one configured remote — Name is whatever the user (or
// `git clone`) called it ("origin", "upstream", ...), FetchURL is
// always set, PushURL only differs from it when a separate
// `remote.<name>.pushurl` override exists (git itself falls back to
// FetchURL for pushing otherwise — see Remotes' own doc comment on
// why this package still always fills PushURL in rather than leaving
// it empty for that common case).
type Remote struct {
	Name     string
	FetchURL string
	PushURL  string
}

// Remotes lists every configured remote, in the order git's own
// config file lists them. `git config --get-regexp`, not `git remote
// -v`: the latter's own output is meant for a human terminal (a
// trailing " (fetch)"/" (push)" literal, no stable field separator
// between name and URL) — the same "don't parse a human-facing
// format, ask git for the real data instead" reasoning Branches' own
// doc comment already gives for for-each-ref over `git branch
// --list`. Two records per remote with a distinct pushurl override
// (one for "url", one for "pushurl") land as a single Remote via name
// — Go map iteration order is deliberately never relied on here:
// order is tracked separately so remotes come back in the same order
// git's own config file has them, not an arbitrary one.
func Remotes(ctx context.Context, root string) ([]Remote, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "config", "--get-regexp", `^remote\..*\.(url|pushurl)$`)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// No remote configured at all is `git config --get-regexp`'s
		// own "no matching key" exit status, not a real error — empty
		// stderr either way is how the two are told apart (a genuine
		// failure, e.g. a corrupt config file, always has something to
		// say).
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("git config: %s", msg)
		}
		return nil, nil
	}

	// byName holds a separately heap-allocated *Remote per name —
	// never a pointer into a slice being built alongside it, which a
	// later append could reallocate out from under an already-stored
	// pointer. order tracks first-seen position separately so the
	// final result still comes back in git's own config-file order,
	// a plain map range over byName would not guarantee that on its
	// own.
	byName := map[string]*Remote{}
	var order []string
	for _, line := range strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		key, url, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		key = strings.TrimPrefix(key, "remote.")
		isPush := strings.HasSuffix(key, ".pushurl")
		name := strings.TrimSuffix(strings.TrimSuffix(key, ".pushurl"), ".url")

		r, exists := byName[name]
		if !exists {
			r = &Remote{Name: name}
			byName[name] = r
			order = append(order, name)
		}
		if isPush {
			r.PushURL = url
		} else {
			r.FetchURL = url
		}
	}

	remotes := make([]Remote, len(order))
	for i, name := range order {
		r := *byName[name]
		// A pushurl override is rare — fall back to FetchURL for the
		// common case, matching git's own behavior for a remote with
		// no separate pushurl configured at all, rather than leaving
		// PushURL empty and making every caller re-derive this same
		// fallback.
		if r.PushURL == "" {
			r.PushURL = r.FetchURL
		}
		remotes[i] = r
	}
	return remotes, nil
}
