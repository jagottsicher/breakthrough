package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gitBreachRemoteRow mirrors gitBreachBranchRow/gitBreachTagRow
// exactly, for the Remotes sub-tab sharing the same BranchesTable
// widget.
func gitBreachRemoteRow(r *Root, name string) (int, bool) {
	for row := 0; row < r.gitBreach.BranchesTable.GetRowCount(); row++ {
		if rm, ok := r.gitBreachRemoteAt(row); ok && rm.Name == name {
			return row, true
		}
	}
	return 0, false
}

func TestToggleToRemotesShowsEveryConfiguredRemote(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")
	runGitBreach(t, dir, "remote", "add", "origin", "https://example.com/repo.git")
	runGitBreach(t, dir, "remote", "add", "upstream", "git@example.com:other/repo.git")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	if len(r.gitBreach.Remotes) != 2 {
		t.Fatalf("setup: Remotes = %+v, want 2", r.gitBreach.Remotes)
	}

	r.toggleGitBreachBranchesTab() // Branches -> Tags
	r.toggleGitBreachBranchesTab() // Tags -> Remotes
	if r.gitBreach.BranchesTab != gitBreachBranchesTabRemotes {
		t.Fatalf("BranchesTab = %v, want Remotes", r.gitBreach.BranchesTab)
	}

	row, ok := gitBreachRemoteRow(r, "origin")
	if !ok {
		t.Fatal("origin not found in the table after switching to Remotes")
	}
	if text := r.gitBreach.BranchesTable.GetCell(row, 0).Text; !strings.Contains(text, "https://example.com/repo.git") {
		t.Errorf("origin row text = %q, want it to contain its own URL", text)
	}
	if _, ok := gitBreachRemoteRow(r, "upstream"); !ok {
		t.Error("upstream not found in the table after switching to Remotes")
	}
}

func TestRenderGitBreachRemotesOnNoRemotesShowsAPlaceholder(t *testing.T) {
	requireGitForBreach(t)
	dir := initGitBreachRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitBreach(t, dir, "add", "a.txt")
	runGitBreach(t, dir, "commit", "-q", "-m", "initial")

	r := newTestRootForGitBreachDir(t, dir)
	r.openGitBreach()
	if len(r.gitBreach.Remotes) != 0 {
		t.Fatalf("setup: Remotes = %+v, want none", r.gitBreach.Remotes)
	}

	r.toggleGitBreachBranchesTab()
	r.toggleGitBreachBranchesTab()

	if got, want := r.gitBreach.BranchesTable.GetCell(1, 0).Text, "No remotes configured."; !strings.Contains(got, want) {
		t.Errorf("placeholder text = %q, want it to contain %q", got, want)
	}
}
