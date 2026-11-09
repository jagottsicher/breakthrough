package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/github"
)

func TestRenderGitBreachBranchesShowsThePullRequestIcon(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")
	runGitBreach(t, dir, "branch", "feature-x")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.gitBreach.PullRequests = map[string]github.PullRequest{
		"feature-x": {Number: 42, State: github.StateOpen},
	}
	r.renderGitBreachBranches()

	row, ok := gitBreachBranchRow(r, "feature-x")
	if !ok {
		t.Fatal("feature-x not found in the Branches table")
	}
	if text := r.gitBreach.BranchesTable.GetCell(row, 0).Text; !strings.Contains(text, "●") {
		t.Errorf("row text = %q, want it to contain the PR icon", text)
	}
}

func TestRenderGitBreachBranchesShowsNoIconForABranchWithoutAPullRequest(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")
	runGitBreach(t, dir, "branch", "feature-x")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.gitBreach.PullRequests = map[string]github.PullRequest{}
	r.renderGitBreachBranches()

	row, ok := gitBreachBranchRow(r, "feature-x")
	if !ok {
		t.Fatal("feature-x not found in the Branches table")
	}
	if text := r.gitBreach.BranchesTable.GetCell(row, 0).Text; strings.Contains(text, "●") || strings.Contains(text, "○") {
		t.Errorf("row text = %q, want no PR icon", text)
	}
}

func TestApplyGitBreachPullRequestsDiscardsAStaleResult(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	r.applyGitBreachPullRequests("/some/other/repo/entirely", map[string]github.PullRequest{"main": {Number: 1}}, nil)

	if r.gitBreach.PullRequests != nil {
		t.Errorf("PullRequests = %+v after a stale apply, want it untouched (nil)", r.gitBreach.PullRequests)
	}
}

func TestApplyGitBreachPullRequestsOnFailureLeavesPullRequestsUntouched(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.gitBreach.PullRequests = map[string]github.PullRequest{"main": {Number: 1}}

	r.applyGitBreachPullRequests(dir, nil, fmt.Errorf("gh: not authenticated"))

	if r.activePage == errorPage {
		t.Error("activePage = errorPage after a failed gh call, want it silently discarded (cosmetic icon only)")
	}
	if _, ok := r.gitBreach.PullRequests["main"]; !ok {
		t.Error("PullRequests cleared on a failed apply, want the previous result left alone")
	}
}

func TestApplyGitBreachPullRequestsOnSuccessUpdatesAndRenders(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	runGitBreach(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")
	runGitBreach(t, dir, "branch", "feature-x")
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	r.applyGitBreachPullRequests(dir, map[string]github.PullRequest{"feature-x": {Number: 7, State: github.StateMerged}}, nil)

	row, ok := gitBreachBranchRow(r, "feature-x")
	if !ok {
		t.Fatal("feature-x not found in the Branches table")
	}
	if text := r.gitBreach.BranchesTable.GetCell(row, 0).Text; !strings.Contains(text, "●") {
		t.Errorf("row text = %q, want it to contain the PR icon after apply", text)
	}
}

func TestOpenGitBreachPullRequestIsANoOpOffTheBranchesSubTab(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.gitBreach.PullRequests = map[string]github.PullRequest{"main": {Number: 1}}
	r.toggleGitBreachBranchesTab() // -> Tags

	r.openGitBreachPullRequest()

	if r.activePage == errorPage {
		t.Error("activePage = errorPage, want a silent no-op off the Branches sub-tab")
	}
}

func TestOpenGitBreachPullRequestIsANoOpOnABranchWithoutAPullRequest(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	r.gitBreach.PullRequests = map[string]github.PullRequest{}

	r.openGitBreachPullRequest()

	if r.activePage == errorPage {
		t.Error("activePage = errorPage, want a silent no-op on a branch with no known PR")
	}
}

func TestApplyGitBreachOpenPullRequestOnFailureShowsAnError(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	r.applyGitBreachOpenPullRequest(dir, "main", fmt.Errorf("gh: no pull requests found"))

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want %q after a failed open", r.activePage, errorPage)
	}
}

func TestApplyGitBreachOpenPullRequestDiscardsAStaleResult(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()

	r.applyGitBreachOpenPullRequest("/some/other/repo/entirely", "main", fmt.Errorf("boom"))

	if r.activePage == errorPage {
		t.Error("activePage = errorPage for a stale result, want it discarded silently")
	}
}
