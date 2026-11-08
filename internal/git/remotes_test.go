package git

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestRemotesOnARepositoryWithNoRemotes(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)

	remotes, err := Remotes(context.Background(), dir)
	if err != nil {
		t.Fatalf("Remotes: %v", err)
	}
	if len(remotes) != 0 {
		t.Errorf("Remotes = %+v, want none", remotes)
	}
}

func TestRemotesListsEveryConfiguredRemote(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	runGit(t, dir, "remote", "add", "origin", "https://example.com/repo.git")
	runGit(t, dir, "remote", "add", "upstream", "git@example.com:other/repo.git")

	remotes, err := Remotes(context.Background(), dir)
	if err != nil {
		t.Fatalf("Remotes: %v", err)
	}
	if len(remotes) != 2 {
		t.Fatalf("Remotes = %+v, want 2", remotes)
	}
	if remotes[0].Name != "origin" || remotes[0].FetchURL != "https://example.com/repo.git" {
		t.Errorf("remotes[0] = %+v, want origin/https://example.com/repo.git", remotes[0])
	}
	if remotes[1].Name != "upstream" || remotes[1].FetchURL != "git@example.com:other/repo.git" {
		t.Errorf("remotes[1] = %+v, want upstream/git@example.com:other/repo.git", remotes[1])
	}
}

// TestRemotesFallsBackToFetchURLForPushWithNoOverride pins the common
// case: PushURL is never left empty, even though git itself only
// ever stores a real `pushurl` config key for the rare remote that
// actually has a separate one.
func TestRemotesFallsBackToFetchURLForPushWithNoOverride(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	runGit(t, dir, "remote", "add", "origin", "https://example.com/repo.git")

	remotes, err := Remotes(context.Background(), dir)
	if err != nil {
		t.Fatalf("Remotes: %v", err)
	}
	if len(remotes) != 1 {
		t.Fatalf("Remotes = %+v, want 1", remotes)
	}
	if remotes[0].PushURL != remotes[0].FetchURL {
		t.Errorf("PushURL = %q, want it to fall back to FetchURL %q", remotes[0].PushURL, remotes[0].FetchURL)
	}
}

func TestRemotesReportsASeparatePushURLOverride(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	runGit(t, dir, "remote", "add", "origin", "https://example.com/repo.git")
	runGit(t, dir, "remote", "set-url", "--push", "origin", "ssh://push.example.com/repo.git")

	remotes, err := Remotes(context.Background(), dir)
	if err != nil {
		t.Fatalf("Remotes: %v", err)
	}
	if len(remotes) != 1 {
		t.Fatalf("Remotes = %+v, want 1", remotes)
	}
	if remotes[0].FetchURL != "https://example.com/repo.git" {
		t.Errorf("FetchURL = %q, want the original url", remotes[0].FetchURL)
	}
	if remotes[0].PushURL != "ssh://push.example.com/repo.git" {
		t.Errorf("PushURL = %q, want the pushurl override", remotes[0].PushURL)
	}
}

// TestFetchRemoteFetchesNewCommitsFromARealRemote uses a local bare
// repository as the remote — a real push/fetch round trip, not a
// mocked one, the same "test the real git subprocess" approach every
// other test in this package already takes, just against a bare repo
// instead of the network.
func TestFetchRemoteFetchesNewCommitsFromARealRemote(t *testing.T) {
	requireGit(t)
	bareDir := t.TempDir()
	runGit(t, bareDir, "init", "-q", "--bare", "-b", "main")

	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "base")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "base")
	runGit(t, dir, "remote", "add", "origin", bareDir)
	runGit(t, dir, "push", "-q", "origin", "main")

	// A second clone of the same bare repo, pushes a commit dir's own
	// remote-tracking ref doesn't know about yet.
	otherDir := t.TempDir()
	runGit(t, otherDir, "clone", "-q", bareDir, otherDir)
	runGit(t, otherDir, "config", "user.name", "Test")
	runGit(t, otherDir, "config", "user.email", "test@example.com")
	writeFile(t, otherDir, "b.txt", "from the other clone")
	runGit(t, otherDir, "add", "b.txt")
	runGit(t, otherDir, "commit", "-q", "-m", "second")
	runGit(t, otherDir, "push", "-q", "origin", "main")

	if err := FetchRemote(context.Background(), dir, "origin"); err != nil {
		t.Fatalf("FetchRemote: %v", err)
	}

	logCmd := exec.Command("git", "-C", dir, "log", "-1", "--format=%s", "origin/main")
	logCmd.Env = gitTestEnv()
	out, err := logCmd.Output()
	if err != nil {
		t.Fatalf("log origin/main: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "second" {
		t.Errorf("origin/main's own latest subject = %q, want %q (the fetch should have updated it)", got, "second")
	}
}

func TestFetchRemoteOnAnUnknownRemoteFails(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)

	if err := FetchRemote(context.Background(), dir, "no-such-remote"); err == nil {
		t.Error("FetchRemote(no-such-remote): err = nil, want an error")
	}
}
