package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestGitBreachFilterMatchesIsPlainCaseInsensitiveSubstring(t *testing.T) {
	cases := []struct {
		filter, text string
		want         bool
	}{
		{"", "anything", true},
		{"foo", "a foo bar", true},
		{"FOO", "a foo bar", true},
		{"foo", "a FOO bar", true},
		{"foo", "bar baz", false},
	}
	for _, c := range cases {
		if got := gitBreachFilterMatches(c.filter, c.text); got != c.want {
			t.Errorf("gitBreachFilterMatches(%q, %q) = %v, want %v", c.filter, c.text, got, c.want)
		}
	}
}

// TestOpenGitBreachFilesFilterShowsOnlyMatchingPathsThenClears is the
// discriminating test for Files: row count drops to the matching
// subset, the matching entry is still there, and a second "/"
// (clearing — the shared prompt overlay's own Enter-on-empty-text
// already means Cancel, see openGitBreachFilter's own doc comment)
// restores the full list.
func TestOpenGitBreachFilesFilterShowsOnlyMatchingPathsThenClears(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	for _, name := range []string{"apple.txt", "banana.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	// 1 non-selectable "Untracked (2)" section header row + 2 file
	// rows (see renderGitBreachFiles) — not just the 2 files alone.
	if got := r.gitBreach.FilesTable.GetRowCount(); got != 3 {
		t.Fatalf("setup: Files has %d rows, want 3 (1 section header + apple.txt + banana.txt)", got)
	}

	r.openGitBreachFilesFilter()
	r.prompt.SetText("apple")
	r.finishPrompt(tcell.KeyEnter)

	if got := r.gitBreach.FilesTable.GetRowCount(); got != 2 {
		t.Fatalf("Files has %d rows after filtering \"apple\", want 2 (1 section header + apple.txt)", got)
	}
	if row := gitBreachTableRowForPath(r, "apple.txt"); row == 0 {
		t.Error("apple.txt not found after filtering for \"apple\"")
	}

	r.openGitBreachFilesFilter() // filter already set — clears it
	if got := r.gitBreach.FilesTable.GetRowCount(); got != 3 {
		t.Errorf("Files has %d rows after clearing the filter, want 3 (restored)", got)
	}
}

// TestOpenGitBreachBranchesOrTagsFilterAppliesToWhicheverSubTabShows
// pins both halves of the shared "/" action: it filters Branches when
// that sub-tab is showing, and Tags once toggled to it — each
// independently of the other's own filter text.
func TestOpenGitBreachBranchesOrTagsFilterAppliesToWhicheverSubTabShows(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	runGitBreach(t, dir, "branch", "feature-x")
	runGitBreach(t, dir, "tag", "v1")
	runGitBreach(t, dir, "tag", "v2")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	if got := len(r.gitBreach.BranchesShown); got != 2 {
		t.Fatalf("setup: BranchesShown = %d, want 2 (main, feature-x)", got)
	}

	r.openGitBreachBranchesOrTagsFilter()
	r.prompt.SetText("feature")
	r.finishPrompt(tcell.KeyEnter)

	if got := len(r.gitBreach.BranchesShown); got != 1 || r.gitBreach.BranchesShown[0].Name != "feature-x" {
		t.Fatalf("BranchesShown after filtering \"feature\" = %+v, want just feature-x", r.gitBreach.BranchesShown)
	}

	r.toggleGitBreachBranchesTab() // Branches -> Tags — its own, still-empty filter
	if got := len(r.gitBreach.TagsShown); got != 2 {
		t.Fatalf("TagsShown = %d, want 2 (v1, v2) — Branches' own filter shouldn't leak into Tags", got)
	}

	r.openGitBreachBranchesOrTagsFilter()
	r.prompt.SetText("v1")
	r.finishPrompt(tcell.KeyEnter)

	if got := len(r.gitBreach.TagsShown); got != 1 || r.gitBreach.TagsShown[0].Name != "v1" {
		t.Fatalf("TagsShown after filtering \"v1\" = %+v, want just v1", r.gitBreach.TagsShown)
	}

	r.reverseToggleGitBreachBranchesTab() // Tags -> Branches
	if got := len(r.gitBreach.BranchesShown); got != 1 || r.gitBreach.BranchesShown[0].Name != "feature-x" {
		t.Errorf("BranchesShown after switching back = %+v, want its own \"feature\" filter still applied", r.gitBreach.BranchesShown)
	}
}

// TestOpenGitBreachStashFilterShowsOnlyMatchingEntries mirrors the
// Files test for Stash, matching against the combined ref+message
// text renderGitBreachStash already shows in one cell.
func TestOpenGitBreachStashFilterShowsOnlyMatchingEntries(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a changed 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "stash", "push", "-q", "-m", "fix the bug")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a changed 2"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "stash", "push", "-q", "-m", "unrelated work")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	if got := len(r.gitBreach.StashShown); got != 2 {
		t.Fatalf("setup: StashShown = %d, want 2", got)
	}

	r.openGitBreachStashFilter()
	r.prompt.SetText("bug")
	r.finishPrompt(tcell.KeyEnter)

	// git itself prepends "On <branch>: " to a stash's own message —
	// Contains, not an exact match against the message alone.
	if got := len(r.gitBreach.StashShown); got != 1 || !strings.Contains(r.gitBreach.StashShown[0].Message, "fix the bug") {
		t.Fatalf("StashShown after filtering \"bug\" = %+v, want just the \"fix the bug\" entry", r.gitBreach.StashShown)
	}

	r.openGitBreachStashFilter() // clears it
	if got := len(r.gitBreach.StashShown); got != 2 {
		t.Errorf("StashShown after clearing the filter = %d, want 2 (restored)", got)
	}
}

// TestGitBreachFilterHintLabelReflectsWhetherAFilterIsActive pins the
// hint bar's own toggle-aware label.
func TestGitBreachFilterHintLabelReflectsWhetherAFilterIsActive(t *testing.T) {
	if got := gitBreachFilterHintLabel(""); got != "filter" {
		t.Errorf("gitBreachFilterHintLabel(\"\") = %q, want %q", got, "filter")
	}
	if got := gitBreachFilterHintLabel("x"); got != "clear filter" {
		t.Errorf("gitBreachFilterHintLabel(%q) = %q, want %q", "x", got, "clear filter")
	}
}
