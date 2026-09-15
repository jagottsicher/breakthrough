package gitstatus

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// requireGit skips the test outright if git itself isn't on $PATH —
// every test in this file needs a real git binary to run real commands
// against, the same "skip rather than fail on a minimal CI image"
// convention internal/search's own tests already use for grep/find.
func requireGit(t *testing.T) {
	t.Helper()
	if !Available() {
		t.Skip("git not found on $PATH")
	}
}

// gitTestEnv is the author/committer identity every git invocation in
// this file runs with — CI runners have no global git identity
// configured (unlike a real developer machine), so anything that skips
// this and shells out to git directly risks "fatal: empty ident name",
// a failure mode that looks nothing like the git behavior it's
// actually testing.
func gitTestEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
}

// runGit runs a real git command against dir, failing the test
// immediately if it doesn't succeed — the test's own setup helper, not
// the thing under test (that's Fetch, which shells out to git itself
// too, but through this package's own code, not directly like this).
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = gitTestEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	return dir
}

func TestFetchOnADirectoryThatIsNotARepository(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()

	_, inRepo, err := Fetch(context.Background(), dir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if inRepo {
		t.Error("inRepo = true for a plain directory with no .git anywhere above it")
	}
}

func TestFetchOnACleanRepository(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	st, inRepo, err := Fetch(context.Background(), dir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !inRepo {
		t.Fatal("inRepo = false for a real git repository")
	}
	if st.Branch != "main" {
		t.Errorf("Branch = %q, want %q", st.Branch, "main")
	}
	if st.Dirty() || st.Conflicts != 0 {
		t.Errorf("status = %+v, want completely clean", st)
	}
}

func TestFetchFromANestedSubdirectoryReportsTheWholeRepository(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	sub := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "root.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "root.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "changed.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "changed.txt")

	st, inRepo, err := Fetch(context.Background(), sub)
	if err != nil {
		t.Fatalf("Fetch(%q): %v", sub, err)
	}
	if !inRepo {
		t.Fatal("a directory nested inside a git working tree should still report inRepo=true")
	}
	if st.Staged != 1 {
		t.Errorf("Staged = %d, want 1 (git walks up to the real repository root on its own)", st.Staged)
	}
}

func TestFetchCountsStagedUnstagedUntrackedAndConflictsSeparately(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	for _, name := range []string{"staged.txt", "both.txt", "unstaged.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("v1"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "both.txt"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "staged.txt", "both.txt")
	// both.txt is staged, then modified again -- git's own "MM": counts
	// as staged AND unstaged, not one merged "changed" total.
	if err := os.WriteFile(filepath.Join(dir, "both.txt"), []byte("v3"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unstaged.txt"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, inRepo, err := Fetch(context.Background(), dir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !inRepo {
		t.Fatal("expected inRepo = true")
	}
	if st.Staged != 2 {
		t.Errorf("Staged = %d, want 2 (staged.txt, both.txt)", st.Staged)
	}
	if st.Unstaged != 2 {
		t.Errorf("Unstaged = %d, want 2 (both.txt, unstaged.txt)", st.Unstaged)
	}
	if st.Untracked != 1 {
		t.Errorf("Untracked = %d, want 1 (new.txt)", st.Untracked)
	}
	if !st.Dirty() {
		t.Error("Dirty() = false, want true")
	}
}

func TestFetchReportsAheadAndBehindAgainstUpstream(t *testing.T) {
	requireGit(t)
	upstream := initRepo(t)
	if err := os.WriteFile(filepath.Join(upstream, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, upstream, "add", "a.txt")
	runGit(t, upstream, "commit", "-q", "-m", "initial")

	clone := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", upstream, clone).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}

	// The clone gets two local commits (ahead 2) while upstream gets
	// one of its own (behind 1) -- deliberately asymmetric, not just
	// diverging by the same count on each side, so a test that
	// happened to swap the two fields (a real, caught-by-sabotage bug
	// during development) would actually fail instead of passing by
	// coincidence.
	for i, name := range []string{"local1.txt", "local2.txt"} {
		if err := os.WriteFile(filepath.Join(clone, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, clone, "add", name)
		runGit(t, clone, "commit", "-q", "-m", fmt.Sprintf("local commit %d", i+1))
	}

	if err := os.WriteFile(filepath.Join(upstream, "upstream.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, upstream, "add", "upstream.txt")
	runGit(t, upstream, "commit", "-q", "-m", "upstream commit")
	runGit(t, clone, "fetch", "-q")

	st, inRepo, err := Fetch(context.Background(), clone)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !inRepo {
		t.Fatal("expected inRepo = true")
	}
	if st.Ahead != 2 || st.Behind != 1 {
		t.Errorf("Ahead/Behind = %d/%d, want 2/1", st.Ahead, st.Behind)
	}
}

func TestFetchReportsConflicts(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "f.txt")
	runGit(t, dir, "commit", "-q", "-m", "base")
	runGit(t, dir, "checkout", "-q", "-b", "other")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-q", "-am", "other change")
	runGit(t, dir, "checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-q", "-am", "main change")

	// Expected to fail with a real conflict left in the working tree --
	// exactly the state being tested for, not a setup error. "-c
	// merge.ff=false" overrides any global merge.ff a CI runner's own
	// git config might set (a real, observed CI failure otherwise: a
	// runner with merge.ff=only refuses the whole merge outright --
	// "Not possible to fast-forward, aborting" -- leaving main
	// completely untouched). The explicit identity env is just as load-
	// bearing here as it is in runGit: without it, a CI runner with no
	// global git identity configured bails out of the merge immediately
	// with "Committer identity unknown", *before* it ever attempts the
	// actual content merge -- which also leaves the working tree
	// untouched and reads exactly like a real conflict never happened,
	// the same observable symptom as the merge.ff=only case above but
	// for a completely different reason. Asserted below rather than
	// just ignored, so a future case like either of these fails with a
	// clear message instead of the oblique "Conflicts = 0, want 1" both
	// first surfaced as.
	mergeCmd := exec.Command("git", "-c", "merge.ff=false", "-C", dir, "merge", "-q", "other")
	mergeCmd.Env = gitTestEnv()
	mergeOut, mergeErr := mergeCmd.CombinedOutput()
	if mergeErr == nil {
		t.Fatalf("setup: expected the merge to conflict, but it succeeded cleanly, output:\n%s", mergeOut)
	}

	st, inRepo, err := Fetch(context.Background(), dir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !inRepo {
		t.Fatal("expected inRepo = true")
	}
	if st.Conflicts != 1 {
		raw, _ := exec.Command("git", "-C", dir, "status", "--porcelain=v2", "--branch").CombinedOutput()
		t.Errorf("Conflicts = %d, want 1\nmerge output:\n%s\nraw git status:\n%s", st.Conflicts, mergeOut, raw)
	}
}

func TestFetchOnARepositoryWithNoCommitsYet(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)

	st, inRepo, err := Fetch(context.Background(), dir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !inRepo {
		t.Fatal("a freshly-initialized repository is still a git working tree")
	}
	if st.Branch != "main" {
		t.Errorf("Branch = %q, want %q even before the first commit", st.Branch, "main")
	}
}

func TestFetchOnADetachedHead(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")
	head, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	runGit(t, dir, "checkout", "-q", "--detach", strings.TrimSpace(string(head)))

	st, inRepo, err := Fetch(context.Background(), dir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !inRepo {
		t.Fatal("expected inRepo = true")
	}
	if !st.Detached {
		t.Error("Detached = false, want true")
	}
	if len(st.Branch) != 7 {
		t.Errorf("Branch = %q, want a 7-character short commit hash", st.Branch)
	}
}

func TestShortOID(t *testing.T) {
	if got := shortOID("abcdef0123456789"); got != "abcdef0" {
		t.Errorf("shortOID(long) = %q, want %q", got, "abcdef0")
	}
	if got := shortOID("abc"); got != "abc" {
		t.Errorf("shortOID(short) = %q, want it left as-is", got)
	}
}
