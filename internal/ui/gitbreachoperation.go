// gitbreachoperation.go wires "C"/"A"/"K" — Continue/Abort/Skip for
// whichever merge/rebase/cherry-pick/revert gitBreach.Operation
// currently reports in progress (see reloadGitBreach, which re-checks
// it on every fetch the same way Status itself is). All three are
// deliberately no-ops with gitBreach.Operation == git.OperationNone,
// the same "self-guarded, always-wired" shape openGitBreachCommitPrompt
// already establishes for "nothing staged to commit" — pressing one of
// these keys never needs a per-box guard the way, say, "o"/"t" do,
// since no row selection is involved at all.
package ui

import (
	"context"
	"fmt"

	"github.com/jagottsicher/breakthrough/internal/git"
)

// openGitBreachOperationContinue is "C" — runs once the dashboard's
// own conflict surface believes every conflicted path has actually
// been resolved and staged; git itself still refuses and names
// whichever path is still unmerged if that belief is wrong, surfaced
// via showTransientErrorOverOverlay (see resolveGitBreachConflictSection's
// own doc comment on why that, not showError, from inside this
// overlay).
func (r *Root) openGitBreachOperationContinue() {
	if r.gitBreach.Operation == git.OperationNone {
		return
	}
	// A conflicting `git flow feature finish` is a plain `git merge`
	// underneath (see git.GitFlowFeatureFinish's own doc comment) —
	// git.ContinueOperation below finishes that merge exactly the same
	// way it would for any other one, but git flow's own cleanup step
	// (deleting the now-fully-merged feature branch) never ran, since
	// it never got the chance to. Captured before ContinueOperation
	// runs: completing the merge consumes .git/MERGE_MSG, the only
	// record of which finish this conflict belonged to.
	pendingFeature := r.gitBreach.PendingGitFlowFinish
	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	if err := git.ContinueOperation(ctx, r.gitBreach.Root, r.gitBreach.Operation); err != nil {
		r.showTransientErrorOverOverlay(fmt.Errorf("git breach: %w", err))
		return
	}
	if pendingFeature != "" {
		// Best-effort: the merge itself already landed on develop
		// either way, so a refusal here (an unrelated worktree still
		// has this branch checked out, say) only ever leaves a stale
		// branch name behind — never a real failure worth its own
		// error overlay over what Continue already accomplished.
		_ = git.DeleteBranch(ctx, r.gitBreach.Root, r.gitBreach.GitFlowFeaturePrefix+pendingFeature)
	}
	r.reloadGitBreach()
}

// openGitBreachOperationAbort is "A" — always confirms first, the same
// "the action itself is what's irreversible" reasoning
// openGitBreachDeleteBranch's own doc comment already gives for an
// identical always-ask choice: aborting throws away every resolution
// made so far in this operation, unlike Continue (the intended, worked-
// toward outcome) or Skip (which keeps the rest of the sequence alive).
func (r *Root) openGitBreachOperationAbort() {
	if r.gitBreach.Operation == git.OperationNone {
		return
	}
	op := r.gitBreach.Operation
	r.openConfirm(
		fmt.Sprintf("Abort the %s in progress? Any conflict resolution made so far is discarded.", op),
		"Yes, abort",
		func() { r.runGitBreachOperationAbort(op) },
	)
}

func (r *Root) runGitBreachOperationAbort(op git.Operation) {
	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	if err := git.AbortOperation(ctx, r.gitBreach.Root, op); err != nil {
		r.showTransientErrorOverOverlay(fmt.Errorf("git breach: %w", err))
		return
	}
	r.reloadGitBreach()
}

// openGitBreachOperationSkip is "K" — git.SkipOperation itself already
// refuses a merge (which has no --skip at all, see its own doc
// comment); every other refusal (most commonly "no operation in
// progress" from this function's own guard just surfacing as git's own
// error text instead, were it ever reachable) comes back the same way.
func (r *Root) openGitBreachOperationSkip() {
	if r.gitBreach.Operation == git.OperationNone {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitBreachFetchTimeout)
	defer cancel()
	if err := git.SkipOperation(ctx, r.gitBreach.Root, r.gitBreach.Operation); err != nil {
		r.showTransientErrorOverOverlay(fmt.Errorf("git breach: %w", err))
		return
	}
	r.reloadGitBreach()
}
