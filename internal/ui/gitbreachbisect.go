package ui

import (
	"context"
	"fmt"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/git"
)

// gitBreachBisectDiffKey is the fixed scroll key bisect's own status
// text lives under in Main — unlike a file/commit/ref diff, there's
// only ever one bisect running at a time, so no per-target key is
// needed the way "F:"/"C:"/"R:" each carry one.
const gitBreachBisectDiffKey = "Z:bisect"

// openGitBreachBisectStart is "B" on the Commits table — only reaches
// its own action while no bisect is already running (see
// gitBreachBisectAvailable). The row under the cursor becomes the
// "good" ref; openGitBreachRefPicker supplies "bad" — the same
// direction openGitBreachDiffAgainstRef's own picker uses, deliberate
// consistency between the two rather than a coincidence: the cursor
// is always "this" commit, whatever gets picked is always the other
// one being compared or tested against it.
func (r *Root) openGitBreachBisectStart() {
	if !r.gitBreachBisectAvailable() {
		return
	}
	row, _ := r.gitBreach.CommitsTable.GetSelection()
	good, ok := r.gitBreachCommitAt(row)
	if !ok {
		return
	}
	r.openGitBreachRefPicker(good.Hash, func(bad string) {
		r.runGitBreachBisect(func(ctx context.Context) (string, error) {
			return git.BisectStart(ctx, r.gitBreach.Root, bad, good.Hash)
		})
	})
}

// openGitBreachBisectMark is "G"/"X" while a bisect is running —
// good/bad, git's own two real verdicts ("skip" isn't wired to a key
// yet, see feature_ideas.txt's own #21 item 7 for that and the other
// explicitly later steps: `git bisect run`, log/replay, visualizing
// the remaining range). Capitalized specifically to stay clear of "g"
// (the AI Consigliere chord, inert inside this exact table otherwise)
// and "c"/"r" (commit/reload, already documented as focus-independent
// everywhere in this dashboard).
func (r *Root) openGitBreachBisectMark(verdict string) {
	if !git.BisectInProgress(r.gitBreach.Root) {
		return
	}
	r.runGitBreachBisect(func(ctx context.Context) (string, error) {
		return git.BisectMark(ctx, r.gitBreach.Root, verdict)
	})
}

// openGitBreachBisectEnd is "E" while a bisect is running — `git
// bisect reset`, returning HEAD to whatever it was on before
// openGitBreachBisectStart and clearing .git/BISECT_START (see
// git.BisectInProgress' own doc comment on why that file's existence
// is this whole feature's "is one running" check).
func (r *Root) openGitBreachBisectEnd() {
	if !git.BisectInProgress(r.gitBreach.Root) {
		return
	}
	r.runGitBreachBisect(func(ctx context.Context) (string, error) {
		return git.BisectReset(ctx, r.gitBreach.Root)
	})
}

// runGitBreachBisect is every bisect action's own shared shape: run
// fn, surface a failure the overlay-safe way (same reasoning as
// openGitBreachCreateTag's own doc comment — Git Breach is itself an
// overlay, and showError's own showOverlay would close it first), or
// on success reload both this dashboard's own state and the active
// panel underneath it (every bisect subcommand can check out a
// different commit, the same "files on disk can change" reasoning
// checkoutGitBreachBranch's own doc comment already gives) and show
// git's own real stdout verbatim in Main — that text (the next
// revision to test, or "<sha> is the first bad commit") is the
// feature, not something to parse into a derived status here.
func (r *Root) runGitBreachBisect(fn func(context.Context) (string, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	out, err := fn(ctx)
	if err != nil {
		r.showTransientErrorOverOverlay(fmt.Errorf("git breach: %w", err))
		return
	}
	r.reloadGitBreach()
	if loadErr := r.panel.load(r.panel.path); loadErr != nil {
		r.showTransientErrorOverOverlay(loadErr)
	}
	r.gitBreachSetDiffText(gitBreachBisectDiffKey, tview.Escape(out), "")
}

// gitBreachBisectAvailable reports whether "B" should do anything
// right now — not while a bisect is already running (BisectStart
// itself would refuse anyway, but with a less specific message than
// this dashboard's own established "do nothing rather than show a
// confusing refusal" convention for an action that's simply not
// reachable from here right now).
func (r *Root) gitBreachBisectAvailable() bool {
	return !git.BisectInProgress(r.gitBreach.Root)
}
