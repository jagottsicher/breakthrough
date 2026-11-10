package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// gitFlowConfigTimeout bounds PendingGitFlowFinish's own two `git
// config --get` calls — always local, no network, so this is purely
// a "never hang forever" ceiling, not a real expected duration.
const gitFlowConfigTimeout = 3 * time.Second

// GitFlowAvailable reports whether `git flow` can actually run here —
// two independent conditions, both required (feature_ideas.txt's own
// item 21: "nur falls git flow/git-flow-next tatsächlich installiert
// ist, sonst nicht sichtbar", the same opt-in pattern GitHub's own PR
// status already follows): the "git-flow" binary itself on $PATH, and
// this specific repository already having run `git flow init` — an
// uninitialized repo has none of the "gitflow.*" config keys below at
// all, and every `git flow feature ...` there fails with a confusing
// error rather than a clean "not set up for this". Callers must treat
// a false result as "feature invisible", never an error row, same as
// a missing `gh`.
func GitFlowAvailable(ctx context.Context, root string) bool {
	if _, err := exec.LookPath("git-flow"); err != nil {
		return false
	}
	_, _, ok := GitFlowConfig(ctx, root)
	return ok
}

// GitFlowConfig reads this repository's own `git flow init` choices —
// develop is gitflow.branch.develop, featurePrefix is
// gitflow.prefix.feature (e.g. "feature/"). ok is false if either key
// is missing, meaning this repository was never `git flow init`-ed at
// all.
func GitFlowConfig(ctx context.Context, root string) (develop, featurePrefix string, ok bool) {
	develop, err1 := configValue(ctx, root, "gitflow.branch.develop")
	featurePrefix, err2 := configValue(ctx, root, "gitflow.prefix.feature")
	if err1 != nil || err2 != nil || develop == "" || featurePrefix == "" {
		return "", "", false
	}
	return develop, featurePrefix, true
}

func configValue(ctx context.Context, root, key string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "config", "--get", key)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", nil // unset key: git config --get exits 1, not a real error here
	}
	return strings.TrimSpace(stdout.String()), nil
}

// GitFlowFeatureStart runs `git flow feature start <name>` — creates
// and checks out a new feature/<name> branch off develop.
func GitFlowFeatureStart(ctx context.Context, root, name string) error {
	return runGitFlow(ctx, root, "feature", "start", name)
}

// GitFlowFeatureFinish runs `git flow feature finish <name>` — merges
// feature/<name> into develop and deletes it. A conflicting finish
// (git flow itself has no special handling for this: it's a plain
// `git merge` underneath) is deliberately not reported as err here,
// the same reasoning Merge's own doc comment already gives — git
// flow's own stderr already names the conflicted path, and
// InProgressOperation picks up the real state (MERGE_HEAD) on the
// very next reload regardless of what this function returns.
func GitFlowFeatureFinish(ctx context.Context, root, name string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "flow", "feature", "finish", name)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// A real conflict, not a genuine failure — see
	// resolvePendingGitFlowFinish's own doc comment for how this gets
	// completed once the conflict is actually resolved.
	if _, ok := PendingGitFlowFinish(root); ok {
		return nil
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return fmt.Errorf("git flow feature finish: %s", msg)
	}
	return fmt.Errorf("git flow feature finish: %w", err)
}

func runGitFlow(ctx context.Context, root string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root, "flow"}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("git flow %s: %s", strings.Join(args, " "), msg)
		}
		return fmt.Errorf("git flow %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// gitFlowMergeMsgPattern matches `git flow`'s own merge-commit message
// for a feature finish — "Merge branch 'feature/<name>' into
// develop" — against whatever MERGE_MSG actually says once a finish
// conflicts. The feature prefix/develop branch name are inserted at
// match time (both configurable via `git flow init`), not hardcoded.
func gitFlowMergeMsgPattern(featurePrefix, develop string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^Merge branch '` + regexp.QuoteMeta(featurePrefix) + `([^']+)' into ` + regexp.QuoteMeta(develop) + `$`)
}

// PendingGitFlowFinish reports the feature name a `git flow feature
// finish` left conflicted mid-merge, if any — read straight out of
// .git/MERGE_MSG, not tracked as separate UI state: the message git
// itself already wrote there ("Merge branch 'feature/<name>' into
// develop") is the only record of which finish this merge conflict
// actually belongs to, and it would otherwise be lost the moment the
// dashboard is closed and reopened, or breakthrough itself restarted,
// while the conflict is still unresolved. ok is false the moment
// MERGE_MSG stops existing (not a merge in progress at all) or its
// content doesn't match a git-flow feature finish's own exact
// wording — an ordinary merge of some other branch never confuses the
// two. The config lookup it needs (develop/featurePrefix) is cheap
// enough — two `git config --get` calls — to redo on every call
// rather than threading it through from somewhere else.
func PendingGitFlowFinish(root string) (name string, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gitFlowConfigTimeout)
	defer cancel()
	develop, featurePrefix, configOK := GitFlowConfig(ctx, root)
	if !configOK {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(root, ".git", "MERGE_MSG"))
	if err != nil {
		return "", false
	}
	m := gitFlowMergeMsgPattern(featurePrefix, develop).FindSubmatch(data)
	if m == nil {
		return "", false
	}
	return string(m[1]), true
}
