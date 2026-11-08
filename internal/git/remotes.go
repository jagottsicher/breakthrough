package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// cmdEnvironWithoutPrompts is os.Environ() plus the two overrides
// FetchRemote's own doc comment explains — shared here rather than
// duplicated once Push/Pull (later Phase D patches) need the
// identical "never block on an interactive credential prompt"
// guarantee for their own network calls.
func cmdEnvironWithoutPrompts() []string {
	return append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
	)
}

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

// FetchRemote runs `git fetch <remote>` — a real network call, unlike
// everything else this package exposes to internal/ui, so the caller
// is expected to run it off the UI goroutine with a much longer
// timeout than this package's other operations get (see internal/ui's
// own gitBreachNetworkTimeout).
//
// Two environment overrides, both there for the same reason Commit's
// own GIT_EDITOR=true is: nothing here can ever answer an interactive
// prompt. GIT_TERMINAL_PROMPT=0 makes git itself fail fast instead of
// prompting for an HTTPS username/password with no terminal reading
// for it to use. GIT_SSH_COMMAND with BatchMode=yes is the SSH-side
// equivalent — without it, ssh still tries to prompt for a
// passphrase on this process's own controlling terminal (the same one
// the TUI itself has in raw mode), which neither git's own flag above
// nor a background goroutine can prevent on its own; verified directly
// against a real SSH remote that refuses access, not guessed.
// PushRemote runs `git push <remote> <branch>` — explicit about which
// local branch, rather than a bare `git push <remote>` relying on
// whatever upstream tracking (or lack of it) happens to already be
// configured: the caller (openGitBreachPushRemote) shows this exact
// command in its own confirm dialog first, so what actually runs has
// to match that literally, not depend on config this dialog never
// showed. Same GIT_TERMINAL_PROMPT/GIT_SSH_COMMAND overrides as
// FetchRemote, for the identical reason — a push is every bit as much
// a real network call as a fetch is.
//
// Unlike CherryPick/Merge, a rejected push (most commonly: the remote
// has commits this branch doesn't, "non-fast-forward") is always a
// real, reportable failure here, never silently reinterpreted as
// something else — there is no local conflict state for this package
// to resolve the way there is for those two; the user's own next step
// is a Pull first, a separate later patch.
func PushRemote(ctx context.Context, root, remote, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "push", remote, branch)
	cmd.Env = cmdEnvironWithoutPrompts()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("git push: %s", msg)
		}
		return fmt.Errorf("git push: %w", err)
	}
	return nil
}

// PullRemote runs `git pull --no-rebase <remote> <branch>` — a real
// fetch followed by a real merge, in one step. --no-rebase is
// deliberate, not the default: `git pull` otherwise follows whatever
// pull.rebase this repository or the user's own global config
// happens to have set, which would make this dashboard's own
// conflict handling depend on config it never shows or asks about.
// Forcing the merge strategy keeps this predictable and keeps a
// conflicting pull landing in exactly the same conflict surface a
// conflicting Merge already does (OperationMerge, resolved with
// "o"/"t" then "C"/"A"/"K") — the same disambiguation Merge's own doc
// comment already explains, reused verbatim here since `git pull`
// without --rebase is, underneath, exactly a fetch plus a merge.
func PullRemote(ctx context.Context, root, remote, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "pull", "--no-rebase", remote, branch)
	cmd.Env = cmdEnvironWithoutPrompts()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if InProgressOperation(root) == OperationMerge {
			return nil
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("git pull: %s", msg)
		}
		return fmt.Errorf("git pull: %w", err)
	}
	return nil
}

func FetchRemote(ctx context.Context, root, remote string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "fetch", remote)
	cmd.Env = cmdEnvironWithoutPrompts()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("git fetch: %s", msg)
		}
		return fmt.Errorf("git fetch: %w", err)
	}
	return nil
}
