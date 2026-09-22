package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/gitstatus"
)

func requireGit(t *testing.T) {
	t.Helper()
	if !gitstatus.Available() {
		t.Skip("git not found on $PATH")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")
	return dir
}

func TestGitStatusColorSeverityOrder(t *testing.T) {
	theme := config.DefaultTheme().Resolve()
	tests := []struct {
		name string
		st   gitstatus.Status
		want tcell.Color
	}{
		{"clean", gitstatus.Status{Branch: "main"}, theme.EntryExecutable},
		{"staged", gitstatus.Status{Branch: "main", Staged: 1}, theme.WarningText},
		{"unstaged", gitstatus.Status{Branch: "main", Unstaged: 1}, theme.WarningText},
		{"untracked", gitstatus.Status{Branch: "main", Untracked: 1}, theme.WarningText},
		{"conflict outranks dirty", gitstatus.Status{Branch: "main", Staged: 1, Conflicts: 1}, theme.CriticalText},
	}
	for _, tt := range tests {
		if got := gitStatusColor(tt.st, theme); got != tt.want {
			t.Errorf("%s: gitStatusColor = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestGitStatusTextOmitsZeroFieldsButKeepsRealOnes(t *testing.T) {
	theme := config.DefaultTheme().Resolve()

	clean := gitStatusText(gitstatus.Status{Branch: "main"}, theme)
	if !strings.Contains(clean, "git:(main)") {
		t.Errorf("clean text = %q, missing branch", clean)
	}
	for _, glyph := range []string{"⇡", "⇣", "+", "!", "?", "="} {
		if strings.Contains(clean, glyph) {
			t.Errorf("clean text = %q, should show no figures at all, found %q", clean, glyph)
		}
	}

	full := gitStatusText(gitstatus.Status{
		Branch: "feature", Ahead: 2, Behind: 1,
		Staged: 3, Unstaged: 4, Untracked: 5, Conflicts: 6,
	}, theme)
	for _, want := range []string{"git:(feature)", "⇡2", "⇣1", "+3", "!4", "?5", "=6"} {
		if !strings.Contains(full, want) {
			t.Errorf("full text = %q, missing %q", full, want)
		}
	}
}

func TestGitStatusForStatusBarOnARealRepository(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	theme := config.DefaultTheme().Resolve()

	got, ok := gitStatusForStatusBar(theme, dir)
	if !ok {
		t.Fatal("gitStatusForStatusBar ok = false for a real, clean repository")
	}
	if !strings.Contains(got, "git:(main)") {
		t.Errorf("got %q, want it to name the branch", got)
	}
}

func TestGitStatusForStatusBarOutsideARepository(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	theme := config.DefaultTheme().Resolve()

	if _, ok := gitStatusForStatusBar(theme, dir); ok {
		t.Error("gitStatusForStatusBar ok = true for a plain directory with no git repository")
	}
}

func TestBuildStatusBarShowsGitSegmentAndItToggles(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	text := r.buildStatusBar()
	if !strings.Contains(text, "git:(main)") {
		t.Errorf("status bar text should contain the git segment, got:\n%s", text)
	}

	r.settings.ShowGitStatus = false
	text = r.buildStatusBar()
	if strings.Contains(text, "git:(") {
		t.Errorf("status bar text should not contain a git segment once toggled off, got:\n%s", text)
	}
}

// finishDetailsGitStatus bypasses startDetailsGitStatus's own
// background goroutine (its result lands via r.app.QueueUpdateDraw,
// which nothing here drains without a running Application — the same
// constraint properties_test.go's own isolateHashFile documents) and
// just fetches and applies the result directly, the same shortcut
// finishCompareTreeWalk already takes for the tree-compare walk.
func finishDetailsGitStatus(t *testing.T, r *Root, path string) {
	t.Helper()
	st, inRepo, err := gitstatus.Fetch(context.Background(), path)
	if err != nil {
		t.Fatalf("gitstatus.Fetch: %v", err)
	}
	if inRepo {
		r.detailsGitStatus = &st
	}
	r.renderDetailsSidebar()
}

func TestDetailsShowsGitSectionForADirectoryInsideARepository(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	_, err = os.Stat(sub) // sanity: sub really exists before selecting it
	if err != nil {
		t.Fatal(err)
	}
	r.target = sub
	r.loadDetailsTarget(sub)
	finishDetailsGitStatus(t, r, sub)

	text := r.detailsSidebar.GetText(true)
	if !strings.Contains(text, "git:(main)") {
		t.Errorf("Details text should show the git section for a directory inside a repository, got:\n%s", text)
	}
}

func TestDetailsOmitsGitSectionForAPlainFile(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	file := filepath.Join(dir, "a.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.loadDetailsTarget(file)

	if r.detailsGitCancel != nil {
		t.Error("startDetailsGitStatus should not even start for a plain file")
	}
	text := r.detailsSidebar.GetText(true)
	if strings.Contains(text, "git:(") {
		t.Errorf("Details text should have no git section for a plain file (only directories, per the user's own request), got:\n%s", text)
	}
}

func TestDetailsOmitsGitSectionOutsideARepository(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.loadDetailsTarget(sub)
	finishDetailsGitStatus(t, r, sub)

	text := r.detailsSidebar.GetText(true)
	if strings.Contains(text, "git:(") {
		t.Errorf("Details text should have no git section outside a git repository, got:\n%s", text)
	}
}

func TestDetailsGitStatusIsNotStartedWhenTheSettingIsOff(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.settings.ShowGitStatus = false
	r.SetRect(0, 0, 100, 40)
	r.loadDetailsTarget(sub)

	if r.detailsGitCancel != nil {
		t.Error("startDetailsGitStatus should not start anything while the setting is off")
	}
}
