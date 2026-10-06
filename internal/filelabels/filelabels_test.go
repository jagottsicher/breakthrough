package filelabels

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "labels.json")
	return NewWithPersistence(path), path
}

func TestSetGetRoundTrip(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Set("/a/b", 3); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := s.Get("/a/b"); got != 3 {
		t.Fatalf("Get = %d, want 3", got)
	}
	if got := s.Get("/a/unset"); got != 0 {
		t.Fatalf("Get unset = %d, want 0", got)
	}
}

func TestSetZeroClears(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Set("/a/b", 5); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Set("/a/b", 0); err != nil {
		t.Fatalf("Set 0: %v", err)
	}
	if got := s.Get("/a/b"); got != 0 {
		t.Fatalf("Get after clear = %d, want 0", got)
	}
}

func TestSetInvalidID(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Set("/a/b", 10); err == nil {
		t.Fatal("Set(10): want error, got nil")
	}
	if err := s.Set("/a/b", -1); err == nil {
		t.Fatal("Set(-1): want error, got nil")
	}
}

func TestCleanPathNormalization(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Set("/a//b/", 2); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := s.Get("/a/b"); got != 2 {
		t.Fatalf("Get normalized = %d, want 2", got)
	}
}

func TestStoreRoundTripAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "labels.json")
	s1 := NewWithPersistence(path)
	if err := s1.SetMany([]string{"/x/1", "/x/2"}, 4); err != nil {
		t.Fatalf("SetMany: %v", err)
	}

	s2 := NewWithPersistence(path)
	if got := s2.Get("/x/1"); got != 4 {
		t.Fatalf("Get /x/1 after reload = %d, want 4", got)
	}
	if got := s2.Get("/x/2"); got != 4 {
		t.Fatalf("Get /x/2 after reload = %d, want 4", got)
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist", "labels.json")
	s := NewWithPersistence(path)
	if got := s.Get("/anything"); got != 0 {
		t.Fatalf("Get on fresh store = %d, want 0", got)
	}
}

func TestLoadSkipsOutOfRangeEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "labels.json")
	data, err := json.Marshal(persistedFile{Version: currentVersion, Labels: map[string]int{
		"/good": 5,
		"/bad":  42,
	}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s := NewWithPersistence(path)
	if got := s.Get("/good"); got != 5 {
		t.Fatalf("Get /good = %d, want 5", got)
	}
	if got := s.Get("/bad"); got != 0 {
		t.Fatalf("Get /bad = %d, want 0 (out-of-range entry should be skipped)", got)
	}
}

func TestRehomeExactMatch(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Set("/old/file.txt", 2); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Rehome("/old/file.txt", "/new/file.txt"); err != nil {
		t.Fatalf("Rehome: %v", err)
	}
	if got := s.Get("/old/file.txt"); got != 0 {
		t.Fatalf("Get old path = %d, want 0", got)
	}
	if got := s.Get("/new/file.txt"); got != 2 {
		t.Fatalf("Get new path = %d, want 2", got)
	}
}

func TestRehomeDirectoryPrefix(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Set("/a/dir", 1); err != nil {
		t.Fatalf("Set dir: %v", err)
	}
	if err := s.Set("/a/dir/inner.txt", 3); err != nil {
		t.Fatalf("Set nested: %v", err)
	}
	if err := s.Set("/a/dir/sub/deep.txt", 4); err != nil {
		t.Fatalf("Set deep: %v", err)
	}

	if err := s.Rehome("/a/dir", "/b/renamed"); err != nil {
		t.Fatalf("Rehome: %v", err)
	}

	if got := s.Get("/b/renamed"); got != 1 {
		t.Fatalf("Get /b/renamed = %d, want 1", got)
	}
	if got := s.Get("/b/renamed/inner.txt"); got != 3 {
		t.Fatalf("Get /b/renamed/inner.txt = %d, want 3", got)
	}
	if got := s.Get("/b/renamed/sub/deep.txt"); got != 4 {
		t.Fatalf("Get /b/renamed/sub/deep.txt = %d, want 4", got)
	}
	if got := s.Get("/a/dir/inner.txt"); got != 0 {
		t.Fatalf("Get old nested path = %d, want 0", got)
	}
}

// TestRehomePrefixVsSibling pins the exact bug the feature spec calls
// out by name: a plain strings.HasPrefix(p, dir) would wrongly also
// match "/a/foo" against an oldPath of "/a/fo" — only a dir-plus-
// separator prefix is a real descendant.
func TestRehomePrefixVsSibling(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Set("/a/fo", 1); err != nil {
		t.Fatalf("Set /a/fo: %v", err)
	}
	if err := s.Set("/a/foo", 2); err != nil {
		t.Fatalf("Set /a/foo: %v", err)
	}
	if err := s.Set("/a/foo/inner.txt", 3); err != nil {
		t.Fatalf("Set /a/foo/inner.txt: %v", err)
	}

	if err := s.Rehome("/a/fo", "/a/renamed"); err != nil {
		t.Fatalf("Rehome: %v", err)
	}

	if got := s.Get("/a/renamed"); got != 1 {
		t.Fatalf("Get /a/renamed = %d, want 1", got)
	}
	// "/a/foo" and everything under it must be completely untouched.
	if got := s.Get("/a/foo"); got != 2 {
		t.Fatalf("Get /a/foo = %d, want 2 (must survive sibling rename untouched)", got)
	}
	if got := s.Get("/a/foo/inner.txt"); got != 3 {
		t.Fatalf("Get /a/foo/inner.txt = %d, want 3", got)
	}
}

func TestTrashRestoreRoundTrip(t *testing.T) {
	s, _ := newTestStore(t)
	original := "/home/user/docs/report.txt"
	trashed := "/home/user/.local/state/breakthrough/trash/files/ab12cd_report.txt"

	if err := s.Set(original, 6); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Move to Trash: the label follows the file to its real location
	// inside the trash directory.
	if err := s.Rehome(original, trashed); err != nil {
		t.Fatalf("Rehome to trash: %v", err)
	}
	if got := s.Get(original); got != 0 {
		t.Fatalf("Get original after trashing = %d, want 0", got)
	}
	if got := s.Get(trashed); got != 6 {
		t.Fatalf("Get trashed = %d, want 6", got)
	}

	// Restore: the label follows it back to TrashItem.OriginalPath.
	if err := s.Rehome(trashed, original); err != nil {
		t.Fatalf("Rehome from trash: %v", err)
	}
	if got := s.Get(original); got != 6 {
		t.Fatalf("Get original after restore = %d, want 6", got)
	}
	if got := s.Get(trashed); got != 0 {
		t.Fatalf("Get trashed after restore = %d, want 0", got)
	}
}

func TestDeleteExactAndPrefix(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Set("/a/dir", 1); err != nil {
		t.Fatalf("Set dir: %v", err)
	}
	if err := s.Set("/a/dir/inner.txt", 2); err != nil {
		t.Fatalf("Set nested: %v", err)
	}
	if err := s.Set("/a/dirsibling", 3); err != nil {
		t.Fatalf("Set sibling: %v", err)
	}

	if err := s.Delete("/a/dir"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if got := s.Get("/a/dir"); got != 0 {
		t.Fatalf("Get /a/dir = %d, want 0", got)
	}
	if got := s.Get("/a/dir/inner.txt"); got != 0 {
		t.Fatalf("Get /a/dir/inner.txt = %d, want 0", got)
	}
	// "/a/dirsibling" starts with "/a/dir" but is not a descendant —
	// must survive, the same prefix-vs-sibling distinction Rehome needs.
	if got := s.Get("/a/dirsibling"); got != 3 {
		t.Fatalf("Get /a/dirsibling = %d, want 3 (must survive)", got)
	}
}

func TestSetManySingleSave(t *testing.T) {
	s, path := newTestStore(t)
	if err := s.SetMany([]string{"/x", "/y", "/z"}, 7); err != nil {
		t.Fatalf("SetMany: %v", err)
	}
	for _, p := range []string{"/x", "/y", "/z"} {
		if got := s.Get(p); got != 7 {
			t.Fatalf("Get %s = %d, want 7", p, got)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var pf persistedFile
	if err := json.Unmarshal(data, &pf); err != nil {
		t.Fatalf("Unmarshal persisted file: %v", err)
	}
	if pf.Version != currentVersion {
		t.Fatalf("persisted version = %d, want %d", pf.Version, currentVersion)
	}
	if len(pf.Labels) != 3 {
		t.Fatalf("persisted labels = %d, want 3", len(pf.Labels))
	}
}

func TestNilStoreIsSafe(t *testing.T) {
	var s *Store
	if got := s.Get("/anything"); got != 0 {
		t.Fatalf("nil Get = %d, want 0", got)
	}
	if err := s.Set("/anything", 1); err != nil {
		t.Fatalf("nil Set: %v", err)
	}
	if err := s.SetMany([]string{"/a", "/b"}, 1); err != nil {
		t.Fatalf("nil SetMany: %v", err)
	}
	if err := s.Rehome("/a", "/b"); err != nil {
		t.Fatalf("nil Rehome: %v", err)
	}
	if err := s.Delete("/a"); err != nil {
		t.Fatalf("nil Delete: %v", err)
	}
}

func TestEmptyPersistencePathDisablesPersistence(t *testing.T) {
	s := NewWithPersistence("")
	if err := s.Set("/a", 2); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := s.Get("/a"); got != 2 {
		t.Fatalf("Get = %d, want 2 (in-memory should still work)", got)
	}
}

func TestFindOrphansDetectsRemovedFile(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present.txt")
	if err := os.WriteFile(present, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(dir, "gone.txt") // never actually created

	s, _ := newTestStore(t)
	if err := s.SetMany([]string{present, gone}, 3); err != nil {
		t.Fatalf("SetMany: %v", err)
	}

	orphans := s.FindOrphans(context.Background())
	if len(orphans) != 1 || orphans[0] != gone {
		t.Errorf("FindOrphans = %v, want [%s]", orphans, gone)
	}
}

func TestFindOrphansSkipsUnreachableParent(t *testing.T) {
	dir := t.TempDir()
	missingParent := filepath.Join(dir, "does-not-exist", "file.txt")

	s, _ := newTestStore(t)
	if err := s.Set(missingParent, 1); err != nil {
		t.Fatalf("Set: %v", err)
	}

	orphans := s.FindOrphans(context.Background())
	if len(orphans) != 0 {
		t.Errorf("FindOrphans = %v, want none — an unreachable parent must be skipped, not flagged orphaned", orphans)
	}
}

func TestFindOrphansEmptyWhenEverythingPresent(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s, _ := newTestStore(t)
	if err := s.SetMany([]string{a, b}, 4); err != nil {
		t.Fatalf("SetMany: %v", err)
	}

	if orphans := s.FindOrphans(context.Background()); len(orphans) != 0 {
		t.Errorf("FindOrphans = %v, want none", orphans)
	}
}

func TestFindOrphansRespectsCancelledContext(t *testing.T) {
	dir := t.TempDir()
	gone := filepath.Join(dir, "gone.txt")

	s, _ := newTestStore(t)
	if err := s.Set(gone, 1); err != nil {
		t.Fatalf("Set: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	orphans := s.FindOrphans(ctx)
	if len(orphans) != 0 {
		t.Errorf("FindOrphans with an already-cancelled context = %v, want none found", orphans)
	}
}

func TestFindOrphansNilStoreIsSafe(t *testing.T) {
	var s *Store
	if got := s.FindOrphans(context.Background()); got != nil {
		t.Errorf("nil Store FindOrphans = %v, want nil", got)
	}
}
