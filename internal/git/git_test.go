package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// requireGit mirrors internal/gitstatus's own identically-named helper
// — every test here needs a real git binary.
func requireGit(t *testing.T) {
	t.Helper()
	if !Available() {
		t.Skip("git not found on $PATH")
	}
}

// gitTestEnv pins author/committer identity AND isolates this test run
// from whatever global/system git config happens to exist on the
// machine actually running it — GIT_CONFIG_GLOBAL/GIT_CONFIG_SYSTEM
// pointing at /dev/null means a developer's own commit.gpgsign=true or
// similar can never make a test here hang waiting on a passphrase
// prompt or fail against a key that only exists on their own machine.
// internal/gitstatus's own tests don't need this isolation (they never
// call `git commit`), which is why it isn't shared from there.
func gitTestEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = gitTestEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// initRepo sets a repo-local identity and disables gpgsign explicitly
// — gitTestEnv's own GIT_AUTHOR_*/GIT_COMMITTER_* vars only cover
// *this test's own setup calls* (runGit), never the code under test:
// Commit builds its own os.Environ()-based Env, which still inherits
// whatever (or no) global git identity happens to exist on the
// machine actually running it. Without this, Commit's own tests pass
// locally (a developer machine has a global identity) and fail on any
// CI runner that doesn't - or worse, hang waiting on a passphrase
// prompt if the runner's own global config happens to force
// commit.gpgsign=true - exactly the class of "works here, not there"
// gap gitstatus_test.go's own gitTestEnv doc comment already warns
// about for its own, narrower set of tests.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRootOnADirectoryThatIsNotARepository(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()

	_, inRepo, err := Root(context.Background(), dir)
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	if inRepo {
		t.Error("inRepo = true for a plain directory with no .git anywhere above it")
	}
}

func TestRootFindsTheTopLevelFromANestedSubdirectory(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	nested := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	root, inRepo, err := Root(context.Background(), nested)
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	if !inRepo {
		t.Fatal("inRepo = false for a real nested subdirectory of a working tree")
	}
	// Resolve both sides through EvalSymlinks: on macOS, t.TempDir()
	// lives under /var, itself a symlink to /private/var — git's own
	// --show-toplevel output is the fully resolved path, which a raw
	// string compare against dir would otherwise spuriously fail.
	wantRoot, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	gotRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if gotRoot != wantRoot {
		t.Errorf("Root = %q, want %q", gotRoot, wantRoot)
	}
}

func TestFetchOnACleanRepository(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	st, err := Fetch(context.Background(), dir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if st.Branch != "main" {
		t.Errorf("Branch = %q, want main", st.Branch)
	}
	if len(st.Staged) != 0 || len(st.Unstaged) != 0 || len(st.Untracked) != 0 {
		t.Errorf("clean repo has changes: staged=%v unstaged=%v untracked=%v", st.Staged, st.Unstaged, st.Untracked)
	}
}

func TestFetchReportsStagedUnstagedAndUntracked(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	writeFile(t, dir, "b.txt", "b")
	runGit(t, dir, "add", "a.txt", "b.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	// a.txt: staged only. b.txt: staged, then modified again (MM).
	// c.txt: untracked.
	writeFile(t, dir, "a.txt", "a changed")
	runGit(t, dir, "add", "a.txt")
	writeFile(t, dir, "b.txt", "b staged")
	runGit(t, dir, "add", "b.txt")
	writeFile(t, dir, "b.txt", "b staged then changed again")
	writeFile(t, dir, "c.txt", "c")

	st, err := Fetch(context.Background(), dir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	stagedPaths := changePaths(st.Staged)
	unstagedPaths := changePaths(st.Unstaged)

	if !containsPath(stagedPaths, "a.txt") {
		t.Errorf("Staged = %v, want a.txt staged", stagedPaths)
	}
	if !containsPath(stagedPaths, "b.txt") {
		t.Errorf("Staged = %v, want b.txt staged (MM)", stagedPaths)
	}
	if !containsPath(unstagedPaths, "b.txt") {
		t.Errorf("Unstaged = %v, want b.txt also unstaged (MM)", unstagedPaths)
	}
	if containsPath(unstagedPaths, "a.txt") {
		t.Errorf("Unstaged = %v, want a.txt NOT unstaged (staged-only)", unstagedPaths)
	}
	if len(st.Untracked) != 1 || st.Untracked[0] != "c.txt" {
		t.Errorf("Untracked = %v, want [c.txt]", st.Untracked)
	}
}

func TestFetchReportsARenameWithItsOriginalPath(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "old.txt", "same content, long enough for git to detect a rename instead of a delete+add pair reliably across versions")
	runGit(t, dir, "add", "old.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	runGit(t, dir, "mv", "old.txt", "new.txt")

	st, err := Fetch(context.Background(), dir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	found := false
	for _, fc := range st.Staged {
		if fc.Path == "new.txt" {
			found = true
			if fc.OrigPath != "old.txt" {
				t.Errorf("OrigPath = %q, want old.txt", fc.OrigPath)
			}
		}
	}
	if !found {
		t.Errorf("Staged = %v, want new.txt present", st.Staged)
	}
}

func TestFetchReportsAConflict(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "base")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "base")

	runGit(t, dir, "switch", "-q", "-c", "side")
	writeFile(t, dir, "a.txt", "side change")
	runGit(t, dir, "commit", "-q", "-am", "side change")

	runGit(t, dir, "switch", "-q", "main")
	writeFile(t, dir, "a.txt", "main change")
	runGit(t, dir, "commit", "-q", "-am", "main change")

	// Merge is expected to fail with a conflict — ignore its own error.
	mergeCmd := exec.Command("git", "-C", dir, "merge", "-q", "--no-edit", "side")
	mergeCmd.Env = gitTestEnv()
	_ = mergeCmd.Run()

	st, err := Fetch(context.Background(), dir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(st.Conflicts) != 1 || st.Conflicts[0] != "a.txt" {
		t.Errorf("Conflicts = %v, want [a.txt]", st.Conflicts)
	}
}

func TestStageAndUnstageRoundTrip(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")

	if err := Stage(context.Background(), dir, []string{"a.txt"}); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	st, err := Fetch(context.Background(), dir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !containsPath(changePaths(st.Staged), "a.txt") {
		t.Fatalf("Staged = %v, want a.txt staged after Stage", st.Staged)
	}

	if err := Unstage(context.Background(), dir, []string{"a.txt"}); err != nil {
		t.Fatalf("Unstage: %v", err)
	}
	st, err = Fetch(context.Background(), dir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(st.Staged) != 0 {
		t.Errorf("Staged = %v, want empty after Unstage", st.Staged)
	}
	if len(st.Untracked) != 1 || st.Untracked[0] != "a.txt" {
		t.Errorf("Untracked = %v, want [a.txt] after Unstage (back to untracked)", st.Untracked)
	}
}

func TestCommitCreatesARealCommit(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	if err := Stage(context.Background(), dir, []string{"a.txt"}); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	if err := Commit(context.Background(), dir, "add a.txt"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	st, err := Fetch(context.Background(), dir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(st.Staged) != 0 || len(st.Unstaged) != 0 {
		t.Errorf("working tree not clean after commit: staged=%v unstaged=%v", st.Staged, st.Unstaged)
	}
}

func TestCommitWithNothingStagedFails(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	if err := Commit(context.Background(), dir, "nothing to commit"); err == nil {
		t.Error("Commit with a clean working tree: err = nil, want an error")
	}
}

func TestCommitIsCancellable(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "a")
	if err := Stage(context.Background(), dir, []string{"a.txt"}); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Commit(ctx, dir, "cancelled"); err == nil {
		t.Error("Commit against an already-cancelled context: err = nil, want context.Canceled")
	}
}

func TestDiffUnstagedVsStaged(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "a.txt", "line one\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	writeFile(t, dir, "a.txt", "line one\nline two\n")

	unstagedDiff, err := Diff(context.Background(), dir, "a.txt", false)
	if err != nil {
		t.Fatalf("Diff(unstaged): %v", err)
	}
	if unstagedDiff == "" {
		t.Error("Diff(unstaged) = \"\", want the worktree change")
	}

	stagedDiff, err := Diff(context.Background(), dir, "a.txt", true)
	if err != nil {
		t.Fatalf("Diff(staged): %v", err)
	}
	if stagedDiff != "" {
		t.Errorf("Diff(staged) = %q, want empty — nothing staged yet", stagedDiff)
	}

	runGit(t, dir, "add", "a.txt")
	stagedDiff, err = Diff(context.Background(), dir, "a.txt", true)
	if err != nil {
		t.Fatalf("Diff(staged) after add: %v", err)
	}
	if stagedDiff == "" {
		t.Error("Diff(staged) after add = \"\", want the staged change")
	}
}

// TestFetchHandlesPathsWithSpacesAndNonASCIIBytes pins the actual
// reason Fetch passes -z to `git status` at all (see its own doc
// comment): without it, git shell-quotes a path containing a space or
// a non-ASCII byte (core.quotePath), which would otherwise either
// corrupt this parser's own SplitN-based field split or leave the
// path wrapped in literal quote characters nobody asked for.
func TestFetchHandlesPathsWithSpacesAndNonASCIIBytes(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	const name = "a file with spaces and ümlaut.txt"
	writeFile(t, dir, name, "content")

	st, err := Fetch(context.Background(), dir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(st.Untracked) != 1 || st.Untracked[0] != name {
		t.Fatalf("Untracked = %v, want [%q]", st.Untracked, name)
	}

	if err := Stage(context.Background(), dir, []string{name}); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	st, err = Fetch(context.Background(), dir)
	if err != nil {
		t.Fatalf("Fetch after Stage: %v", err)
	}
	if !containsPath(changePaths(st.Staged), name) {
		t.Fatalf("Staged = %v, want %q staged", st.Staged, name)
	}

	diff, err := Diff(context.Background(), dir, name, true)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if diff == "" {
		t.Error("Diff for the newly staged file = \"\", want a real diff")
	}
}

func TestUntrackedContentReadsTheFileDirectly(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	writeFile(t, dir, "new.txt", "brand new content")

	got, err := UntrackedContent(dir, "new.txt")
	if err != nil {
		t.Fatalf("UntrackedContent: %v", err)
	}
	if got != "brand new content" {
		t.Errorf("UntrackedContent = %q, want %q", got, "brand new content")
	}
}

func changePaths(changes []FileChange) []string {
	paths := make([]string, len(changes))
	for i, c := range changes {
		paths[i] = c.Path
	}
	return paths
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}
