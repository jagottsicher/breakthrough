package git

import (
	"context"
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
